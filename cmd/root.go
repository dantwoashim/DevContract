// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"os"

	"github.com/dantwoashim/devcontract/internal/ui"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	verbose bool
	quiet   bool
	noColor bool
	cfgFile string
	envFile string

	// Build info (set via ldflags)
	Version   = "dev"
	GitCommit = "none"
	BuildDate = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "devcontract",
	Short: "Secure project environment sync and setup",
	Long: `DevContract helps developers share local .env files and standardize repository setup.

It uses existing SSH keys for identity, encrypts shared values end to end,
and keeps local setup instructions in a repo-owned contract.

DevContract is for development environments. It is not a production secrets manager
or a hosted control plane by itself.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		ui.SetQuiet(quiet)
		ui.SetNoColor(noColor)
	},
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "  x %s\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Suppress all output except errors")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "Use alternate config file")
	rootCmd.PersistentFlags().StringVar(&envFile, "file", ".env", "Target specific .env file")
}
