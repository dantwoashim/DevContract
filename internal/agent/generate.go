// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/dantwoashim/Env_sync/internal/contract"
)

type GeneratedFile struct {
	Path    string
	Content []byte
}

func Generate(spec *contract.Contract, agentName string) ([]GeneratedFile, error) {
	if spec == nil {
		return nil, fmt.Errorf("nil contract")
	}
	spec.Normalize()

	target, ok := spec.EffectiveAgentTarget(agentName)
	if !ok {
		return nil, fmt.Errorf("agent %q is not defined in contract", agentName)
	}

	files := []GeneratedFile{{
		Path:    filepath.Clean(target.Output),
		Content: []byte(renderInstructions(spec, agentName, target)),
	}}

	if len(spec.MCP.Servers) > 0 {
		mcpPath := target.MCPOutput
		if mcpPath == "" {
			mcpPath = filepath.Join(".envsync", "generated", agentName+".mcp.json")
		}
		payload, err := renderMCP(spec)
		if err != nil {
			return nil, err
		}
		files = append(files, GeneratedFile{
			Path:    filepath.Clean(mcpPath),
			Content: payload,
		})
	}

	return files, nil
}

func renderInstructions(spec *contract.Contract, agentName string, target contract.AgentTarget) string {
	var b strings.Builder
	title := target.Header
	if title == "" {
		title = titleCaseASCII(agentName) + " instructions"
	}

	switch agentName {
	case "cursor":
		b.WriteString("---\n")
		b.WriteString("description: EnvSync-generated agent rules\n")
		b.WriteString("globs: [\"**/*\"]\n")
		b.WriteString("alwaysApply: false\n")
		b.WriteString("---\n\n")
	}

	fmt.Fprintf(&b, "# %s\n\n", title)
	if spec.Project.Name != "" {
		fmt.Fprintf(&b, "Project: `%s`\n\n", spec.Project.Name)
	}
	if spec.Project.Summary != "" {
		fmt.Fprintf(&b, "%s\n\n", spec.Project.Summary)
	}

	b.WriteString("## Workflow\n\n")
	b.WriteString("- Run `envsync bootstrap` on a fresh clone.\n")
	b.WriteString("- Run `envsync doctor` before major edits.\n")
	b.WriteString("- Run `envsync guard scan` before commits touching generated instruction files or config.\n")
	if spec.Run.Default != "" {
		fmt.Fprintf(&b, "- Use `envsync run %s` for the default development workflow.\n", spec.Run.Default)
	}

	b.WriteString("\n## Secret Safety\n\n")
	b.WriteString("- Never inline secrets in markdown instructions, JSON config, or logs.\n")
	b.WriteString("- Use environment variables only when configuring tools or MCP servers.\n")
	b.WriteString("- If you touch `.env`, `WORKSPACE.md`, `.github/copilot-instructions.md`, `.cursor/`, `.claude/`, or MCP config, run `envsync guard scan`.\n")

	if envNames := spec.AllEnvNames(); len(envNames) > 0 {
		b.WriteString("\n## Environment Variables\n\n")
		for _, name := range envNames {
			fmt.Fprintf(&b, "- `%s`\n", name)
		}
	}

	if len(spec.MCP.Servers) > 0 {
		b.WriteString("\n## MCP Servers\n\n")
		for _, server := range spec.MCP.Servers {
			fmt.Fprintf(&b, "- `%s`: command `%s`\n", server.Name, server.Command)
		}
		b.WriteString("- Use the generated MCP JSON file. Do not replace `${ENV_VAR}` placeholders with literal secrets.\n")
	}

	if len(target.Instructions) > 0 {
		b.WriteString("\n## Repo-Specific Rules\n\n")
		for _, line := range target.Instructions {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(line))
		}
	}

	if len(spec.Services) > 0 {
		b.WriteString("\n## Local Services\n\n")
		for _, svc := range spec.Services {
			host := svc.Host
			if host == "" {
				host = "127.0.0.1"
			}
			if svc.Port > 0 {
				fmt.Fprintf(&b, "- `%s` on `%s:%d`\n", svc.Name, host, svc.Port)
			} else {
				fmt.Fprintf(&b, "- `%s`\n", svc.Name)
			}
		}
	}

	return b.String()
}

func renderMCP(spec *contract.Contract) ([]byte, error) {
	type server struct {
		Command string            `json:"command"`
		Args    []string          `json:"args,omitempty"`
		Env     map[string]string `json:"env,omitempty"`
	}

	payload := struct {
		Servers map[string]server `json:"servers"`
	}{
		Servers: map[string]server{},
	}

	for _, serverSpec := range spec.MCP.Servers {
		envMap := map[string]string{}
		for _, envVar := range serverSpec.Env {
			envMap[envVar] = "${" + envVar + "}"
		}
		payload.Servers[serverSpec.Name] = server{
			Command: serverSpec.Command,
			Args:    serverSpec.Args,
			Env:     envMap,
		}
	}

	keys := make([]string, 0, len(payload.Servers))
	for key := range payload.Servers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	ordered := struct {
		Servers map[string]server `json:"servers"`
	}{
		Servers: map[string]server{},
	}
	for _, key := range keys {
		ordered.Servers[key] = payload.Servers[key]
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(ordered); err != nil {
		return nil, fmt.Errorf("encoding MCP config: %w", err)
	}
	return buf.Bytes(), nil
}

func titleCaseASCII(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(strings.ToLower(value))
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
