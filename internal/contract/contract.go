// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package contract

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/dantwoashim/devcontract/internal/config"
	yaml "gopkg.in/yaml.v3"
)

var envNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

type Contract struct {
	Version   int                    `yaml:"version"`
	Project   Project                `yaml:"project"`
	Env       EnvSpec                `yaml:"env"`
	Runtimes  []Runtime              `yaml:"runtimes"`
	Services  []Service              `yaml:"services"`
	Bootstrap Bootstrap              `yaml:"bootstrap"`
	Doctor    Doctor                 `yaml:"doctor"`
	Agents    map[string]AgentTarget `yaml:"agents"`
	MCP       MCPConfig              `yaml:"mcp"`
	Policies  Policies               `yaml:"policies"`
	Run       RunConfig              `yaml:"run"`
}

type Project struct {
	Slug    string `yaml:"slug"`
	Name    string `yaml:"name,omitempty"`
	Summary string `yaml:"summary,omitempty"`
}

type EnvSpec struct {
	Required []EnvVar `yaml:"required"`
	Optional []EnvVar `yaml:"optional"`
	Public   []string `yaml:"public"`
}

type EnvVar struct {
	Name        string `yaml:"name"`
	Source      string `yaml:"source,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type Runtime struct {
	Name       string   `yaml:"name"`
	Binary     string   `yaml:"binary,omitempty"`
	VersionArg []string `yaml:"version_args,omitempty"`
	Required   bool     `yaml:"required,omitempty"`
}

type Service struct {
	Name        string `yaml:"name"`
	Host        string `yaml:"host,omitempty"`
	Port        int    `yaml:"port"`
	Required    bool   `yaml:"required,omitempty"`
	Start       string `yaml:"start,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type Bootstrap struct {
	Steps   []BootstrapStep   `yaml:"steps"`
	Outputs []BootstrapOutput `yaml:"outputs,omitempty"`
}

type BootstrapStep struct {
	Name        string `yaml:"name,omitempty"`
	Run         string `yaml:"run"`
	Shell       string `yaml:"shell,omitempty"`
	Optional    bool   `yaml:"optional,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type BootstrapOutput struct {
	Path      string `yaml:"path"`
	Kind      string `yaml:"kind,omitempty"`
	Gitignore bool   `yaml:"gitignore,omitempty"`
	Header    string `yaml:"header,omitempty"`
	Mode      string `yaml:"mode,omitempty"`
}

type Doctor struct {
	Checks []DoctorCheck `yaml:"checks,omitempty"`
}

type DoctorCheck struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Target   string `yaml:"target,omitempty"`
	Required bool   `yaml:"required,omitempty"`
}

type AgentTarget struct {
	Output       string   `yaml:"output"`
	MCPOutput    string   `yaml:"mcp_output,omitempty"`
	Header       string   `yaml:"header,omitempty"`
	Instructions []string `yaml:"instructions,omitempty"`
}

type MCPConfig struct {
	Servers []MCPServer `yaml:"servers"`
}

type MCPServer struct {
	Name        string   `yaml:"name"`
	Command     string   `yaml:"command"`
	Args        []string `yaml:"args,omitempty"`
	Env         []string `yaml:"env,omitempty"`
	Description string   `yaml:"description,omitempty"`
}

type Policies struct {
	RedactPaths   []string `yaml:"redact_paths,omitempty"`
	BlockPatterns []string `yaml:"block_patterns,omitempty"`
}

type RunConfig struct {
	Default string               `yaml:"default,omitempty"`
	Targets map[string]RunTarget `yaml:"targets,omitempty"`
}

type RunTarget struct {
	Command     string `yaml:"command"`
	Description string `yaml:"description,omitempty"`
}

type ValidationReport struct {
	Errors   []string
	Warnings []string
}

func (r *ValidationReport) AddError(format string, args ...any) {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
}

func (r *ValidationReport) AddWarning(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

func (r *ValidationReport) OK() bool {
	return len(r.Errors) == 0
}

func (e *EnvVar) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		e.Name = strings.TrimSpace(node.Value)
	default:
		type plain EnvVar
		if err := node.Decode((*plain)(e)); err != nil {
			return err
		}
	}
	if e.Source == "" {
		e.Source = "shared"
	}
	return nil
}

func (r *Runtime) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*r = defaultRuntime(strings.TrimSpace(node.Value))
	default:
		type plain Runtime
		if err := node.Decode((*plain)(r)); err != nil {
			return err
		}
	}
	if r.Binary == "" {
		r.Binary = r.Name
	}
	if len(r.VersionArg) == 0 {
		r.VersionArg = defaultRuntime(r.Name).VersionArg
	}
	return nil
}

func (s *BootstrapStep) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		s.Run = strings.TrimSpace(node.Value)
	default:
		type plain BootstrapStep
		if err := node.Decode((*plain)(s)); err != nil {
			return err
		}
	}
	if s.Name == "" {
		s.Name = slugify(firstToken(s.Run))
	}
	return nil
}

func defaultRuntime(name string) Runtime {
	r := Runtime{
		Name:       name,
		Binary:     name,
		VersionArg: []string{"--version"},
		Required:   true,
	}
	switch strings.ToLower(name) {
	case "node":
		r.Binary = "node"
	case "python":
		if runtime.GOOS == "windows" {
			r.Binary = "python"
		} else {
			r.Binary = "python3"
		}
	case "npm":
		r.Binary = "npm"
	case "pnpm":
		r.Binary = "pnpm"
	case "uv":
		r.Binary = "uv"
	case "go":
		r.Binary = "go"
		r.VersionArg = []string{"version"}
	case "docker":
		r.Binary = "docker"
		r.VersionArg = []string{"version", "--format", "{{.Client.Version}}"}
	}
	return r
}

func Default(projectName string) *Contract {
	if projectName == "" {
		projectName = filepath.Base(mustWd())
	}
	slug := slugify(projectName)
	return &Contract{
		Version: 1,
		Project: Project{
			Slug:    slug,
			Name:    projectName,
			Summary: "Repository setup contract",
		},
		Env:      EnvSpec{Public: []string{"PORT", "NODE_ENV"}},
		Runtimes: []Runtime{},
		Bootstrap: Bootstrap{
			Steps: []BootstrapStep{
				{
					Name:        "install-dependencies",
					Run:         "echo \"Define your install command in .devcontract/contract.yaml\"",
					Description: "Replace this placeholder with your real setup step",
				},
			},
			Outputs: []BootstrapOutput{
				{Path: ".env.local", Kind: "env", Gitignore: true, Header: "Developer-local environment overrides"},
			},
		},
		Agents: map[string]AgentTarget{},
		MCP:    MCPConfig{},
		Policies: Policies{
			RedactPaths: []string{".env.local"},
		},
		Run: RunConfig{
			Default: "dev",
			Targets: map[string]RunTarget{
				"dev": {Command: "echo \"Define your default dev command in .devcontract/contract.yaml\"", Description: "Start the default development workflow"},
			},
		},
	}
}

func Load(path string) (*Contract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	contract := &Contract{}
	if err := yaml.Unmarshal(data, contract); err != nil {
		return nil, fmt.Errorf("parsing contract: %w", err)
	}
	contract.Normalize()
	return contract, nil
}

func LoadProject() (*Contract, string, error) {
	path, err := config.FindContractFile()
	if err != nil {
		return nil, "", err
	}
	contract, err := Load(path)
	if err != nil {
		return nil, "", err
	}
	return contract, path, nil
}

func Save(path string, contract *Contract) error {
	contract.Normalize()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating contract directory: %w", err)
	}
	data, err := yaml.Marshal(contract)
	if err != nil {
		return fmt.Errorf("encoding contract: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func EnsureDefault(path, projectName string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	return true, Save(path, Default(projectName))
}

func (c *Contract) Normalize() {
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Project.Slug == "" {
		c.Project.Slug = slugify(c.Project.Name)
	}
	if c.Project.Name == "" {
		c.Project.Name = c.Project.Slug
	}
	if c.Agents == nil {
		c.Agents = map[string]AgentTarget{}
	}
	for name, target := range c.Agents {
		if target.Output == "" {
			target.Output = defaultAgentOutput(name)
		}
		if target.MCPOutput == "" && len(c.MCP.Servers) > 0 {
			target.MCPOutput = defaultMCPOutput(name)
		}
		c.Agents[name] = target
	}
}

func (c *Contract) Validate() *ValidationReport {
	c.Normalize()
	report := &ValidationReport{}
	if c.Version != 1 {
		report.AddError("unsupported contract version %d", c.Version)
	}
	if c.Project.Slug == "" {
		report.AddError("project.slug is required")
	}

	seenEnv := map[string]struct{}{}
	validateEnvList := func(label string, values []EnvVar) {
		for _, envVar := range values {
			if envVar.Name == "" {
				report.AddError("%s contains an empty env var entry", label)
				continue
			}
			if !envNamePattern.MatchString(envVar.Name) {
				report.AddError("%s env var %q must be uppercase snake_case", label, envVar.Name)
			}
			if _, exists := seenEnv[envVar.Name]; exists {
				report.AddError("env var %q is declared more than once", envVar.Name)
			}
			seenEnv[envVar.Name] = struct{}{}
			switch envVar.Source {
			case "", "shared", "developer-local", "ci-only", "manual":
			default:
				report.AddError("env var %q has unknown source %q", envVar.Name, envVar.Source)
			}
		}
	}
	validateEnvList("env.required", c.Env.Required)
	validateEnvList("env.optional", c.Env.Optional)
	for _, name := range c.Env.Public {
		if !envNamePattern.MatchString(name) {
			report.AddError("env.public entry %q must be uppercase snake_case", name)
		}
	}
	if len(c.Runtimes) == 0 {
		report.AddWarning("no runtimes configured")
	}
	for _, runtime := range c.Runtimes {
		if runtime.Name == "" {
			report.AddError("runtime name is required")
		}
		if runtime.Binary == "" {
			report.AddError("runtime %q is missing a binary", runtime.Name)
		}
	}
	for _, service := range c.Services {
		if service.Name == "" {
			report.AddError("service name is required")
		}
		if service.Port < 0 || service.Port > 65535 {
			report.AddError("service %q has invalid port %d", service.Name, service.Port)
		}
	}
	for _, step := range c.Bootstrap.Steps {
		if strings.TrimSpace(step.Run) == "" {
			report.AddError("bootstrap step %q is missing a run command", step.Name)
		}
	}
	for _, output := range c.Bootstrap.Outputs {
		if strings.TrimSpace(output.Path) == "" {
			report.AddError("bootstrap output path is required")
		}
		if filepath.IsAbs(output.Path) {
			report.AddError("bootstrap output %q must be relative to the repo root", output.Path)
		}
		switch output.Mode {
		case "", "create-if-missing", "refresh-managed", "manual":
		default:
			report.AddError("bootstrap output %q uses unsupported mode %q", output.Path, output.Mode)
		}
	}
	validAgents := map[string]struct{}{"copilot": {}, "assistant": {}, "cursor": {}, "claude": {}}
	for name, target := range c.Agents {
		if _, ok := validAgents[name]; !ok {
			report.AddWarning("agent target %q is not one of copilot/assistant/cursor/claude", name)
		}
		if strings.TrimSpace(target.Output) == "" {
			report.AddError("agent %q is missing an output path", name)
		}
	}
	serverNames := map[string]struct{}{}
	for _, server := range c.MCP.Servers {
		if server.Name == "" {
			report.AddError("mcp server name is required")
			continue
		}
		if _, exists := serverNames[server.Name]; exists {
			report.AddError("mcp server %q is declared more than once", server.Name)
		}
		serverNames[server.Name] = struct{}{}
		if server.Command == "" {
			report.AddError("mcp server %q is missing a command", server.Name)
		}
		for _, envVar := range server.Env {
			if !envNamePattern.MatchString(envVar) {
				report.AddError("mcp server %q references invalid env var %q", server.Name, envVar)
			}
		}
	}
	for _, pattern := range c.Policies.BlockPatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			report.AddError("invalid policy block pattern %q: %v", pattern, err)
		}
	}
	for _, check := range c.Doctor.Checks {
		if strings.TrimSpace(check.Name) == "" {
			report.AddError("doctor check name is required")
		}
		if !isSupportedDoctorCheckType(check.Type) {
			report.AddError("doctor check %q uses unsupported type %q", check.Name, check.Type)
		}
		if strings.TrimSpace(check.Target) == "" {
			report.AddError("doctor check %q is missing a target", check.Name)
		}
	}
	if c.Run.Default != "" {
		if _, ok := c.Run.Targets[c.Run.Default]; !ok {
			report.AddError("run.default %q is not declared in run.targets", c.Run.Default)
		}
	}
	for name, target := range c.Run.Targets {
		if strings.TrimSpace(target.Command) == "" {
			report.AddError("run target %q is missing a command", name)
		}
	}
	return report
}

func (c *Contract) AllEnvNames() []string {
	seen := map[string]struct{}{}
	var names []string
	appendNames := func(values []EnvVar) {
		for _, item := range values {
			if item.Name == "" {
				continue
			}
			if _, exists := seen[item.Name]; exists {
				continue
			}
			seen[item.Name] = struct{}{}
			names = append(names, item.Name)
		}
	}
	appendNames(c.Env.Required)
	appendNames(c.Env.Optional)
	for _, name := range c.Env.Public {
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *Contract) EffectiveAgentTarget(name string) (AgentTarget, bool) {
	c.Normalize()
	target, ok := c.Agents[name]
	return target, ok
}

func defaultAgentOutput(name string) string {
	switch name {
	case "copilot":
		return ".github/copilot-instructions.md"
	case "assistant":
		return "WORKSPACE.md"
	case "cursor":
		return filepath.Join(".cursor", "rules", "devcontract.mdc")
	case "claude":
		return filepath.Join(".claude", "DEVCONTRACT.md")
	default:
		return filepath.Join(".devcontract", "generated", name+".md")
	}
}

func defaultMCPOutput(name string) string {
	switch name {
	case "copilot":
		return filepath.Join(".vscode", "mcp.json")
	case "assistant":
		return filepath.Join(".devcontract", "generated", "assistant.mcp.json")
	case "cursor":
		return filepath.Join(".cursor", "mcp.json")
	case "claude":
		return filepath.Join(".claude", "mcp.json")
	default:
		return filepath.Join(".devcontract", "generated", name+".mcp.json")
	}
}

func firstToken(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "step"
	}
	return fields[0]
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "devcontract-project"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "devcontract-project"
	}
	return result
}

func mustWd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "devcontract-project"
	}
	return wd
}

func isSupportedDoctorCheckType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "file_exists", "json_valid", "tcp":
		return true
	default:
		return false
	}
}
