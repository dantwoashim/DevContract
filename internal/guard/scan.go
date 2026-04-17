// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package guard

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dantwoashim/Env_sync/internal/contract"
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
	FilesScanned        int            `json:"files_scanned"`
	FilesSkipped        int            `json:"files_skipped"`
	FindingsByCategory  map[string]int `json:"findings_by_category,omitempty"`
	SkippedByReason     map[string]int `json:"skipped_by_reason,omitempty"`
	Findings            []Finding      `json:"findings"`
	Skipped             []SkippedFile  `json:"skipped,omitempty"`
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

	if !utf8.Valid(data) {
		return nil, &SkippedFile{Path: normalizedPath, Reason: "binary_or_non_utf8"}, nil
	}

	segments := buildSegments(data)
	var findings []Finding
	for _, segment := range segments {
		segmentFindings, err := scanSegment(normalizedPath, segment.text, segment.startLine, rules)
		if err != nil {
			return nil, nil, err
		}
		findings = append(findings, segmentFindings...)
	}
	if len(data) > 1<<20 {
		return findings, &SkippedFile{Path: normalizedPath, Reason: "large_text_scanned_in_chunks"}, nil
	}
	return findings, nil, nil
}

type scanSegmentText struct {
	startLine int
	text      string
}

func buildSegments(data []byte) []scanSegmentText {
	if len(data) <= 1<<20 {
		return []scanSegmentText{{startLine: 1, text: string(data)}}
	}

	const windowSize = 256 * 1024
	headEnd := min(len(data), windowSize)
	tailStart := max(0, len(data)-windowSize)
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
	return []string{"WORKSPACE.md", ".github/copilot-instructions.md", ".cursor", ".claude", ".vscode/mcp.json", "mcp.json", ".envsync/generated", "prompts", "prompt", "logs"}
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
	case ".md", ".mdx", ".txt", ".json", ".jsonc", ".yaml", ".yml", ".toml", ".env", ".log", ".prompt", ".mdc", ".ini", ".cfg":
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
