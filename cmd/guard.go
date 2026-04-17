// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dantwoashim/Env_sync/internal/config"
	"github.com/dantwoashim/Env_sync/internal/contract"
	"github.com/dantwoashim/Env_sync/internal/guard"
	"github.com/dantwoashim/Env_sync/internal/ui"
	"github.com/spf13/cobra"
)

var (
	guardJSON   bool
	guardPaths  []string
	guardStaged bool
	guardFailOn string
)

var guardCmd = &cobra.Command{
	Use:   "guard",
	Short: "Safety checks for instruction files and configs",
}

var guardScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan for secrets and unsafe inline credentials",
	RunE:  runGuardScan,
}

var guardHookInstallCmd = &cobra.Command{
	Use:   "hook install",
	Short: "Install a pre-commit hook for guard scan",
	RunE:  runGuardHookInstall,
}

func runGuardScan(cmd *cobra.Command, args []string) error {
	root, err := config.FindProjectRoot()
	if err != nil {
		root = mustGetwd()
	}

	var scanPaths []string
	if guardStaged {
		scanPaths, err = stagedFiles(root)
		if err != nil {
			return err
		}
	} else {
		scanPaths = append(scanPaths, guardPaths...)
	}

	spec, _, _ := contract.LoadProject()
	report, err := guard.ScanContractAware(root, spec, scanPaths)
	if err != nil {
		return err
	}

	if guardJSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
	} else {
		ui.Header("EnvSync Guard")
		ui.Line(fmt.Sprintf("  Files scanned: %d", report.FilesScanned))
		ui.Line(fmt.Sprintf("  Files skipped: %d", report.FilesSkipped))
		if len(report.FindingsByCategory) > 0 {
			ui.Line(fmt.Sprintf("  Categories: %s", formatGuardCategoryCounts(report.FindingsByCategory)))
		}
		if len(report.SkippedByReason) > 0 {
			ui.Line(fmt.Sprintf("  Skipped reasons: %s", formatGuardCategoryCounts(report.SkippedByReason)))
		}
		if len(report.Findings) == 0 {
			ui.Success("No unsafe secrets or inline credentials detected")
		} else {
			for _, finding := range report.Findings {
				ui.Line(fmt.Sprintf("  [%s/%s] %s:%d %s (%s)", finding.Severity, finding.Category, finding.Path, finding.Line, finding.Message, finding.Match))
			}
		}
		ui.Blank()
	}

	switch strings.ToLower(guardFailOn) {
	case "warn":
		if len(report.Findings) > 0 {
			return fmt.Errorf("guard scan found %d issue(s)", len(report.Findings))
		}
	case "error":
		if report.HasSeverity(guard.SeverityError) {
			return fmt.Errorf("guard scan found blocking secret exposures")
		}
	}

	return nil
}

func formatGuardCategoryCounts(counts map[string]int) string {
	parts := make([]string, 0, len(counts))
	for key, value := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", key, value))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func runGuardHookInstall(cmd *cobra.Command, args []string) error {
	root, err := config.FindProjectRoot()
	if err != nil {
		root = mustGetwd()
	}

	hooksDir := filepath.Join(root, ".git", "hooks")
	if _, err := os.Stat(hooksDir); err != nil {
		return fmt.Errorf("git hooks directory not found at %s", hooksDir)
	}

	hookPath := filepath.Join(hooksDir, "pre-commit")
	content := "#!/bin/sh\nenvsync guard scan --staged --fail-on error\n"
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		return err
	}

	ui.Success(fmt.Sprintf("Installed guard pre-commit hook at %s", hookPath))
	return nil
}

func stagedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-only", "--diff-filter=ACM")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("listing staged files: %w", err)
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func init() {
	guardScanCmd.Flags().BoolVar(&guardJSON, "json", false, "Print guard findings as JSON")
	guardScanCmd.Flags().StringArrayVar(&guardPaths, "path", nil, "Limit scan to a relative path or directory")
	guardScanCmd.Flags().BoolVar(&guardStaged, "staged", false, "Scan only staged git changes")
	guardScanCmd.Flags().StringVar(&guardFailOn, "fail-on", "error", "Failure threshold: error, warn, or never")

	guardCmd.AddCommand(guardScanCmd)
	guardCmd.AddCommand(guardHookInstallCmd)
	rootCmd.AddCommand(guardCmd)
}
