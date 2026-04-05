package agent

import (
	"strings"
	"testing"

	"github.com/envsync/envsync/internal/contract"
)

func TestGenerateIncludesEnvPlaceholdersInMCP(t *testing.T) {
	spec := contract.Default("AI App")
	files, err := Generate(spec, "copilot")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("expected at least 2 generated files, got %d", len(files))
	}

	mcpContent := string(files[1].Content)
	if !strings.Contains(mcpContent, "${OPENAI_API_KEY}") {
		t.Fatalf("expected env placeholder in MCP config, got %s", mcpContent)
	}
	if strings.Contains(mcpContent, "sk-") {
		t.Fatalf("did not expect inline secrets in MCP config")
	}
}
