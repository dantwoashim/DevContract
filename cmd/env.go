// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/dantwoashim/devcontract/internal/envfile"
	"github.com/spf13/cobra"
)

var (
	envExportSource       string
	envExportFormat       string
	envExportWrite        string
	envExportGitHubOutput string
	envExportMaskValues   bool
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Utilities for working with .env files",
}

var envExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export .env content in machine-readable formats",
	RunE:  runEnvExport,
}

func runEnvExport(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(envExportSource)
	if err != nil {
		return fmt.Errorf("reading env file: %w", err)
	}
	parsed, err := envfile.Parse(string(data))
	if err != nil {
		return fmt.Errorf("parsing env file: %w", err)
	}

	values := parsed.ToMap()
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	switch strings.ToLower(strings.TrimSpace(envExportFormat)) {
	case "json":
		encoded, err := json.MarshalIndent(values, "", "  ")
		if err != nil {
			return err
		}
		if envExportWrite == "" {
			fmt.Println(string(encoded))
		} else {
			if err := os.WriteFile(envExportWrite, encoded, 0600); err != nil {
				return err
			}
		}
	case "github-actions":
		content := renderGitHubEnv(keys, values)
		if envExportWrite == "" {
			fmt.Print(content)
		} else {
			f, err := os.OpenFile(envExportWrite, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				return err
			}
			if _, err := f.WriteString(content); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}

		if envExportMaskValues {
			for _, key := range keys {
				if values[key] != "" {
					fmt.Printf("::add-mask::%s\n", values[key])
				}
			}
		}
	default:
		return fmt.Errorf("unsupported export format %q", envExportFormat)
	}

	if envExportGitHubOutput != "" {
		f, err := os.OpenFile(envExportGitHubOutput, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(f, "env_count=%d\n", len(keys)); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}

	return nil
}

func renderGitHubEnv(keys []string, values map[string]string) string {
	var b strings.Builder
	for _, key := range keys {
		value := values[key]
		delimiter := githubDelimiter(value)
		fmt.Fprintf(&b, "%s<<%s\n%s\n%s\n", key, delimiter, value, delimiter)
	}
	return b.String()
}

func githubDelimiter(value string) string {
	delimiter := "DEVCONTRACT_EOF"
	for strings.Contains(value, delimiter) {
		delimiter += "_X"
	}
	return delimiter
}

func init() {
	envExportCmd.Flags().StringVar(&envExportSource, "source", ".env", "Source .env file")
	envExportCmd.Flags().StringVar(&envExportFormat, "format", "json", "Export format: json or github-actions")
	envExportCmd.Flags().StringVar(&envExportWrite, "write", "", "Optional output file path")
	envExportCmd.Flags().StringVar(&envExportGitHubOutput, "github-output", "", "Optional GitHub output file path")
	envExportCmd.Flags().BoolVar(&envExportMaskValues, "mask-values", false, "Emit GitHub add-mask commands for exported values")

	envCmd.AddCommand(envExportCmd)
	rootCmd.AddCommand(envCmd)
}
