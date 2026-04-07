package guard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dantwoashim/Env_sync/internal/contract"
)

func TestScanContractAwareFindsSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKSPACE.md")
	if err := os.WriteFile(path, []byte("Never paste sk-proj-supersecretkey1234567890 into docs."), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	spec := &contract.Contract{
		Version: 1,
		Policies: contract.Policies{
			RedactPaths: []string{"WORKSPACE.md"},
		},
	}
	report, err := ScanContractAware(dir, spec, nil)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(report.Findings) == 0 {
		t.Fatal("expected findings")
	}
	if !report.HasSeverity(SeverityError) {
		t.Fatal("expected error severity finding")
	}
}

func TestScanCustomPatterns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".github", "copilot-instructions.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("INTERNAL_AUDIT_SECRET=abc123456789abcd"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	report, err := Scan(Options{
		Root:          dir,
		Paths:         []string{".github"},
		BlockPatterns: []string{`INTERNAL_AUDIT_SECRET=\w+`},
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(report.Findings) < 1 {
		t.Fatalf("expected at least 1 finding, got %d", len(report.Findings))
	}
}
