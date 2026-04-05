package agent

import (
	"strings"
	"testing"

	"github.com/envsync/envsync/internal/contract"
)

func TestGenerateIncludesEnvPlaceholdersInMCP(t *testing.T) {
	spec := &contract.Contract{
		Version: 1,
		Project: contract.Project{Name: "Payments API", Slug: "payments-api"},
		Agents: map[string]contract.AgentTarget{
			"copilot": {
				Output:    ".github/copilot-instructions.md",
				MCPOutput: ".vscode/mcp.json",
			},
		},
		MCP: contract.MCPConfig{
			Servers: []contract.MCPServer{
				{
					Name:    "repo-docs",
					Command: "node",
					Args:    []string{"scripts/mcp-docs.js"},
					Env:     []string{"GITHUB_TOKEN"},
				},
			},
		},
	}
	files, err := Generate(spec, "copilot")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("expected at least 2 generated files, got %d", len(files))
	}

	mcpContent := string(files[1].Content)
	if !strings.Contains(mcpContent, "${GITHUB_TOKEN}") {
		t.Fatalf("expected env placeholder in MCP config, got %s", mcpContent)
	}
	if strings.Contains(mcpContent, "sk-") {
		t.Fatalf("did not expect inline secrets in MCP config")
	}
}
