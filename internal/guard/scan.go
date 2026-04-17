// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package guard

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dantwoashim/devcontract/internal/contract"
)

type Severity string

const (
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

type Rule struct {
	Name     string
	Category string
	Severity Severity
	Message  string
	Pattern  *regexp.Regexp
}

type Finding struct {
	Path     string   `json:"path"`
	Line     int      `json:"line"`
	Rule     string   `json:"rule"`
	Category string   `json:"category"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Match    string   `json:"match,omitempty"`
}

type SkippedFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type Report struct {
	FilesScanned       int            `json:"files_scanned"`
	FilesSkipped       int            `json:"files_skipped"`
	FindingsByCategory map[string]int `json:"findings_by_category,omitempty"`
	SkippedByReason    map[string]int `json:"skipped_by_reason,omitempty"`
	Findings           []Finding      `json:"findings"`
	Skipped            []SkippedFile  `json:"skipped,omitempty"`
}

func (r Report) HasSeverity(min Severity) bool {
	for _, finding := range r.Findings {
		if severityRank(finding.Severity) >= severityRank(min) {
			return true
		}
	}
	return false
}

type Options struct {
	Root          string
	Paths         []string
	BlockPatterns []string
}

const (
	textChunkWindowBytes = 256 * 1024
	archiveEntryLimit    = 128
	archiveReadLimit     = 8 << 20
	archiveEntryBytes    = 1 << 20
)

func ScanContractAware(root string, spec *contract.Contract, paths []string) (Report, error) {
	opts := Options{Root: root, Paths: paths}
	if spec != nil {
		opts.BlockPatterns = spec.Policies.BlockPatterns
		if len(opts.Paths) == 0 {
			opts.Paths = append(opts.Paths, defaultScanPaths()...)
			opts.Paths = append(opts.Paths, contractScanPaths(spec)...)
		}
	}
	if len(opts.Paths) == 0 {
		opts.Paths = defaultScanPaths()
	}
	return Scan(opts)
}

func Scan(opts Options) (Report, error) {
	if opts.Root == "" {
		opts.Root = "."
	}

	rules, err := compileRules(opts.BlockPatterns)
	if err != nil {
		return Report{}, err
	}

	files, err := expandPaths(opts.Root, opts.Paths)
	if err != nil {
		return Report{}, err
	}

	report := Report{FilesScanned: len(files), Findings: []Finding{}}
	for _, filePath := range files {
		findings, skipped, err := scanFile(filePath, opts.Root, rules)
		if err != nil {
			return report, err
		}
		report.Findings = append(report.Findings, findings...)
		if skipped != nil {
			report.Skipped = append(report.Skipped, *skipped)
		}
	}

	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Path == report.Findings[j].Path {
			return report.Findings[i].Line < report.Findings[j].Line
		}
		return report.Findings[i].Path < report.Findings[j].Path
	})
	sort.Slice(report.Skipped, func(i, j int) bool {
		return report.Skipped[i].Path < report.Skipped[j].Path
	})

	report.FilesSkipped = len(report.Skipped)
	if len(report.Findings) > 0 {
		report.FindingsByCategory = map[string]int{}
		for _, finding := range report.Findings {
			report.FindingsByCategory[finding.Category]++
		}
	}
	if len(report.Skipped) > 0 {
		report.SkippedByReason = map[string]int{}
		for _, skipped := range report.Skipped {
			report.SkippedByReason[skipped.Reason]++
		}
	}

	return report, nil
}

func compileRules(extraPatterns []string) ([]Rule, error) {
	rules := []Rule{
		{
			Name:     "openai_api_key",
			Category: "provider_key",
			Severity: SeverityError,
			Message:  "Provider-style API key found",
			Pattern:  regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
		},
		{
			Name:     "anthropic_api_key",
			Category: "provider_key",
			Severity: SeverityError,
			Message:  "Provider API key found",
			Pattern:  regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{10,}\b`),
		},
		{
			Name:     "github_pat",
			Category: "access_token",
			Severity: SeverityError,
			Message:  "GitHub personal access token found",
			Pattern:  regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`),
		},
		{
			Name:     "bearer_token",
			Category: "access_token",
			Severity: SeverityError,
			Message:  "Bearer token found",
			Pattern:  regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{20,}`),
		},
		{
			Name:     "pem_private_key",
			Category: "private_key",
			Severity: SeverityError,
			Message:  "Private key material found",
			Pattern:  regexp.MustCompile(`-----BEGIN (RSA|EC|OPENSSH|DSA|PRIVATE) KEY-----`),
		},
		{
			Name:     "inline_secret_assignment",
			Category: "inline_secret",
			Severity: SeverityWarn,
			Message:  "Suspicious inline credential assignment found in text content",
			Pattern:  regexp.MustCompile(`(?i)(api[_ -]?key|token|secret|password)\s*[:=]\s*['"]?[A-Za-z0-9._/\-]{12,}`),
		},
	}

	for idx, pattern := range extraPatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compiling block pattern %q: %w", pattern, err)
		}
		rules = append(rules, Rule{
			Name:     fmt.Sprintf("custom_block_%d", idx+1),
			Category: "custom_block",
			Severity: SeverityError,
			Message:  "Matched custom block pattern",
			Pattern:  re,
		})
	}

	return rules, nil
}

func expandPaths(root string, candidates []string) ([]string, error) {
	seen := map[string]struct{}{}
	var files []string

	addFile := func(path string) {
		if path == "" {
			return
		}
		abs := filepath.Clean(path)
		if _, ok := seen[abs]; ok {
			return
		}
		seen[abs] = struct{}{}
		files = append(files, abs)
	}

	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}

		full := candidate
		if !filepath.IsAbs(candidate) {
			full = filepath.Join(root, candidate)
		}

		if hasGlob(candidate) {
			matches, err := filepath.Glob(full)
			if err != nil {
				return nil, err
			}
			for _, match := range matches {
				if err := walkScanPath(match, addFile); err != nil {
					return nil, err
				}
			}
			continue
		}

		if _, err := os.Stat(full); err != nil {
			continue
		}
		if err := walkScanPath(full, addFile); err != nil {
			return nil, err
		}
	}

	sort.Strings(files)
	return files, nil
}

func walkScanPath(path string, addFile func(string)) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if shouldScanFile(path) {
			addFile(path)
		}
		return nil
	}

	return filepath.WalkDir(path, func(entryPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldScanFile(entryPath) {
			addFile(entryPath)
		}
		return nil
	})
}

func scanFile(path, root string, rules []Rule) ([]Finding, *SkippedFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	relPath, relErr := filepath.Rel(root, path)
	if relErr != nil {
		relPath = path
	}
	normalizedPath := filepath.ToSlash(relPath)

	return scanContent(normalizedPath, path, data, rules, true)
}

type scanSegmentText struct {
	startLine int
	text      string
}

func buildSegments(data []byte) []scanSegmentText {
	if len(data) <= 1<<20 {
		return []scanSegmentText{{startLine: 1, text: string(data)}}
	}

	headEnd := min(len(data), textChunkWindowBytes)
	tailStart := max(0, len(data)-textChunkWindowBytes)
	if tailStart <= headEnd {
		return []scanSegmentText{{startLine: 1, text: string(data)}}
	}

	return []scanSegmentText{
		{startLine: 1, text: string(data[:headEnd])},
		{startLine: 1 + strings.Count(string(data[:tailStart]), "\n"), text: string(data[tailStart:])},
	}
}

func scanSegment(path, text string, startLine int, rules []Rule) ([]Finding, error) {
	var findings []Finding
	scanner := bufio.NewScanner(strings.NewReader(text))
	lineNumber := startLine - 1
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		for _, rule := range rules {
			match := rule.Pattern.FindString(line)
			if match == "" {
				continue
			}
			findings = append(findings, Finding{
				Path:     path,
				Line:     lineNumber,
				Rule:     rule.Name,
				Category: rule.Category,
				Severity: rule.Severity,
				Message:  rule.Message,
				Match:    truncateMatch(match),
			})
		}
	}
	return findings, scanner.Err()
}

func scanContent(reportPath, sourcePath string, data []byte, rules []Rule, allowArchive bool) ([]Finding, *SkippedFile, error) {
	if utf8.Valid(data) {
		findings, err := scanSegments(reportPath, buildSegments(data), rules)
		if err != nil {
			return nil, nil, err
		}
		if len(data) > 1<<20 {
			return findings, &SkippedFile{Path: reportPath, Reason: "large_text_scanned_in_chunks"}, nil
		}
		return findings, nil, nil
	}

	if allowArchive {
		if archiveKind := detectArchiveKind(sourcePath, data); archiveKind != "" {
			findings, partial, err := scanArchive(reportPath, archiveKind, data, rules)
			if err == nil {
				findings = dedupeFindings(findings)
				if partial {
					return findings, &SkippedFile{Path: reportPath, Reason: "archive_partial_scan"}, nil
				}
				return findings, nil, nil
			}
		}
	}

	segments := buildBinarySegments(data)
	if len(segments) == 0 {
		return nil, &SkippedFile{Path: reportPath, Reason: "binary_or_non_utf8"}, nil
	}

	findings, err := scanSegments(reportPath, segments, rules)
	if err != nil {
		return nil, nil, err
	}
	return dedupeFindings(findings), &SkippedFile{Path: reportPath, Reason: "binary_scanned_with_heuristics"}, nil
}

func scanSegments(path string, segments []scanSegmentText, rules []Rule) ([]Finding, error) {
	var findings []Finding
	for _, segment := range segments {
		segmentFindings, err := scanSegment(path, segment.text, segment.startLine, rules)
		if err != nil {
			return nil, err
		}
		findings = append(findings, segmentFindings...)
	}
	return dedupeFindings(findings), nil
}

func buildBinarySegments(data []byte) []scanSegmentText {
	samples := buildBinarySamples(data)
	segments := make([]scanSegmentText, 0, len(samples))
	for _, sample := range samples {
		text := strings.TrimSpace(binarySampleText(sample))
		if text == "" {
			continue
		}
		segments = append(segments, scanSegmentText{
			startLine: 1,
			text:      text,
		})
	}
	return segments
}

func buildBinarySamples(data []byte) [][]byte {
	if len(data) <= 1<<20 {
		return [][]byte{data}
	}
	headEnd := min(len(data), textChunkWindowBytes)
	tailStart := max(0, len(data)-textChunkWindowBytes)
	if tailStart <= headEnd {
		return [][]byte{data}
	}
	return [][]byte{
		data[:headEnd],
		data[tailStart:],
	}
}

func binarySampleText(data []byte) string {
	var builder strings.Builder
	builder.Grow(len(data))
	for _, b := range data {
		switch {
		case b == '\n' || b == '\r':
			builder.WriteByte('\n')
		case b == '\t':
			builder.WriteByte(' ')
		case b >= 32 && b <= 126:
			builder.WriteByte(b)
		default:
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func detectArchiveKind(path string, data []byte) string {
	lowerPath := strings.ToLower(filepath.Base(path))
	switch {
	case len(data) >= 4 && bytes.Equal(data[:4], []byte("PK\x03\x04")):
		return "zip"
	case strings.HasSuffix(lowerPath, ".zip"), strings.HasSuffix(lowerPath, ".jar"), strings.HasSuffix(lowerPath, ".vsix"):
		return "zip"
	case len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b:
		return "gzip"
	case strings.HasSuffix(lowerPath, ".tgz"), strings.HasSuffix(lowerPath, ".tar.gz"), strings.HasSuffix(lowerPath, ".gz"):
		return "gzip"
	case len(data) >= 262 && string(data[257:262]) == "ustar":
		return "tar"
	case strings.HasSuffix(lowerPath, ".tar"):
		return "tar"
	default:
		return ""
	}
}

func scanArchive(reportPath, kind string, data []byte, rules []Rule) ([]Finding, bool, error) {
	switch kind {
	case "zip":
		return scanZipArchive(reportPath, data, rules)
	case "gzip":
		return scanGzipArchive(reportPath, data, rules)
	case "tar":
		return scanTarArchive(reportPath, bytes.NewReader(data), rules)
	default:
		return nil, false, fmt.Errorf("unsupported archive kind %q", kind)
	}
}

func scanZipArchive(reportPath string, data []byte, rules []Rule) ([]Finding, bool, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, false, err
	}

	var (
		findings   []Finding
		totalBytes int64
		partial    bool
	)

	for index, file := range reader.File {
		if index >= archiveEntryLimit {
			partial = true
			break
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > archiveEntryBytes {
			partial = true
			continue
		}
		remainingBudget := uint64(archiveReadLimit - totalBytes)
		if file.UncompressedSize64 > remainingBudget {
			partial = true
			break
		}

		rc, err := file.Open()
		if err != nil {
			partial = true
			continue
		}
		entryData, readErr := io.ReadAll(io.LimitReader(rc, archiveEntryBytes+1))
		_ = rc.Close()
		if readErr != nil {
			partial = true
			continue
		}
		if len(entryData) > archiveEntryBytes {
			partial = true
			continue
		}
		totalBytes += int64(len(entryData))

		entryFindings, skipped, err := scanContent(reportPath+"!"+filepath.ToSlash(file.Name), file.Name, entryData, rules, false)
		if err != nil {
			return nil, false, err
		}
		findings = append(findings, entryFindings...)
		if skipped != nil {
			partial = true
		}
	}

	return findings, partial, nil
}

func scanGzipArchive(reportPath string, data []byte, rules []Rule) ([]Finding, bool, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, false, err
	}
	defer reader.Close()

	payload, err := io.ReadAll(io.LimitReader(reader, archiveReadLimit+1))
	if err != nil {
		return nil, false, err
	}
	if len(payload) > archiveReadLimit {
		payload = payload[:archiveReadLimit]
	}

	if detectArchiveKind(strings.TrimSuffix(reportPath, filepath.Ext(reportPath)), payload) == "tar" {
		findings, partial, err := scanTarArchive(reportPath, bytes.NewReader(payload), rules)
		return findings, partial || len(payload) >= archiveReadLimit, err
	}

	findings, skipped, err := scanContent(strings.TrimSuffix(reportPath, ".gz"), strings.TrimSuffix(reportPath, ".gz"), payload, rules, false)
	if err != nil {
		return nil, false, err
	}
	return findings, skipped != nil || len(payload) >= archiveReadLimit, nil
}

func scanTarArchive(reportPath string, reader io.Reader, rules []Rule) ([]Finding, bool, error) {
	tr := tar.NewReader(reader)
	var (
		findings   []Finding
		totalBytes int64
		entries    int
		partial    bool
	)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, err
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		entries++
		if entries > archiveEntryLimit {
			partial = true
			break
		}
		if header.Size > archiveEntryBytes {
			partial = true
			continue
		}
		if totalBytes+header.Size > archiveReadLimit {
			partial = true
			break
		}

		entryData, err := io.ReadAll(io.LimitReader(tr, archiveEntryBytes+1))
		if err != nil {
			partial = true
			continue
		}
		if len(entryData) > archiveEntryBytes {
			partial = true
			continue
		}
		totalBytes += int64(len(entryData))

		entryFindings, skipped, err := scanContent(reportPath+"!"+filepath.ToSlash(header.Name), header.Name, entryData, rules, false)
		if err != nil {
			return nil, false, err
		}
		findings = append(findings, entryFindings...)
		if skipped != nil {
			partial = true
		}
	}

	return findings, partial, nil
}

func dedupeFindings(findings []Finding) []Finding {
	if len(findings) < 2 {
		return findings
	}

	seen := make(map[string]struct{}, len(findings))
	deduped := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		key := fmt.Sprintf("%s|%d|%s|%s", finding.Path, finding.Line, finding.Rule, finding.Match)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, finding)
	}
	return deduped
}

func truncateMatch(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 24 {
		return value
	}
	return value[:8] + "..." + value[len(value)-8:]
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityError:
		return 2
	case SeverityWarn:
		return 1
	default:
		return 0
	}
}

func defaultScanPaths() []string {
	return []string{"WORKSPACE.md", ".github/copilot-instructions.md", ".cursor", ".claude", ".vscode/mcp.json", "mcp.json", ".devcontract/generated", "prompts", "prompt", "logs"}
}

func contractScanPaths(spec *contract.Contract) []string {
	if spec == nil {
		return nil
	}

	paths := make([]string, 0, len(spec.Agents)*2+len(spec.Policies.RedactPaths))
	for _, target := range spec.Agents {
		if strings.TrimSpace(target.Output) != "" {
			paths = append(paths, target.Output)
		}
		if strings.TrimSpace(target.MCPOutput) != "" {
			paths = append(paths, target.MCPOutput)
		}
	}

	for _, path := range spec.Policies.RedactPaths {
		if isEnvLikePath(path) {
			continue
		}
		paths = append(paths, path)
	}

	return paths
}

func hasGlob(value string) bool {
	return strings.ContainsAny(value, "*?[")
}

func isEnvLikePath(path string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	return base == ".env" || strings.HasPrefix(base, ".env.")
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "out", "coverage", "vendor", ".next":
		return true
	default:
		return false
	}
}

func shouldScanFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".mdx", ".txt", ".json", ".jsonc", ".yaml", ".yml", ".toml", ".env", ".log", ".prompt", ".mdc", ".ini", ".cfg", ".zip", ".jar", ".gz", ".tgz", ".tar", ".vsix":
		return true
	}
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "WORKSPACE.md", "mcp.json", "copilot-instructions.md":
		return true
	}
	return ext == "" && strings.HasPrefix(base, ".env")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
