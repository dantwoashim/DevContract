package guard

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dantwoashim/devcontract/internal/contract"
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

func TestScanReportsCategoriesAndSkippedCoverage(t *testing.T) {
	dir := t.TempDir()

	largePath := filepath.Join(dir, "logs", "audit.log")
	if err := os.MkdirAll(filepath.Dir(largePath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	largeContent := strings.Repeat("safe-line\n", 150000) + "bearer verylongtokenthatshouldtriggerdetection123456789\n"
	if err := os.WriteFile(largePath, []byte(largeContent), 0644); err != nil {
		t.Fatalf("write large file: %v", err)
	}

	binaryPath := filepath.Join(dir, "logs", "artifact.log")
	if err := os.WriteFile(binaryPath, []byte{0xff, 0xfe, 0x00, 0x01}, 0644); err != nil {
		t.Fatalf("write binary file: %v", err)
	}

	report, err := Scan(Options{
		Root:  dir,
		Paths: []string{"logs"},
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(report.Findings) == 0 {
		t.Fatal("expected findings in large text file")
	}
	if report.FindingsByCategory["access_token"] == 0 {
		t.Fatalf("expected access_token category, got %#v", report.FindingsByCategory)
	}
	if report.FilesSkipped == 0 {
		t.Fatal("expected skipped coverage metadata")
	}
	if report.SkippedByReason["binary_or_non_utf8"] == 0 {
		t.Fatalf("expected binary skip reason, got %#v", report.SkippedByReason)
	}
	if report.SkippedByReason["large_text_scanned_in_chunks"] == 0 {
		t.Fatalf("expected large text skip reason, got %#v", report.SkippedByReason)
	}
}

func TestScanUsesBinaryHeuristicsForNonUTF8Files(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "crash.log")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	payload := append([]byte{0xff, 0xfe, 0x00}, []byte("Bearer token_abcdefghijklmnopqrstuvwxyz123456")...)
	payload = append(payload, 0x00, 0x01, 0x02)
	if err := os.WriteFile(path, payload, 0644); err != nil {
		t.Fatalf("write binary log: %v", err)
	}

	report, err := Scan(Options{
		Root:  dir,
		Paths: []string{"logs"},
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(report.Findings) == 0 {
		t.Fatal("expected binary heuristic finding")
	}
	if report.SkippedByReason["binary_scanned_with_heuristics"] == 0 {
		t.Fatalf("expected heuristic coverage metadata, got %#v", report.SkippedByReason)
	}
}

func TestScanInspectsZipArchivesForSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "artifacts.zip")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zipWriter := zip.NewWriter(file)
	entry, err := zipWriter.Create("nested/secret.env")
	if err != nil {
		t.Fatalf("create archive entry: %v", err)
	}
	if _, err := entry.Write([]byte("GITHUB_PAT=ghp_supersecretvalue1234567890")); err != nil {
		t.Fatalf("write archive entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	report, err := Scan(Options{
		Root:  dir,
		Paths: []string{"logs"},
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(report.Findings) == 0 {
		t.Fatal("expected archive finding")
	}
	foundArchivePath := false
	for _, finding := range report.Findings {
		if finding.Path == "logs/artifacts.zip!nested/secret.env" {
			foundArchivePath = true
			break
		}
	}
	if !foundArchivePath {
		t.Fatalf("expected nested archive finding path, got %#v", report.Findings)
	}
}
