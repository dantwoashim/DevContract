// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/dantwoashim/Env_sync/internal/agent"
	"github.com/dantwoashim/Env_sync/internal/ui"
	"github.com/spf13/cobra"
)

var (
	agentName string
	agentAll  bool
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Generate repo-scoped instruction and tool config files",
}

var agentInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Generate instruction files and JSON tool config from the repo contract",
	RunE:  runAgentInstall,
}

func runAgentInstall(cmd *cobra.Command, args []string) error {
	ctx, err := requireContractContext()
	if err != nil {
		return err
	}

	targets := []string{}
	if agentAll || agentName == "all" || agentName == "" {
		for name := range ctx.Contract.Agents {
			targets = append(targets, name)
		}
		if len(targets) == 0 {
			return fmt.Errorf("no agents are defined in %s", ctx.Path)
		}
	} else {
		targets = append(targets, agentName)
	}
	sort.Strings(targets)

	ui.Header("EnvSync Agent Install")
	for _, target := range targets {
		files, err := agent.Generate(ctx.Contract, target)
		if err != nil {
			return err
		}
		for _, file := range files {
			path, err := safeRepoPath(ctx.Root, file.Path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(path, file.Content, 0644); err != nil {
				return err
			}
			ui.Success(fmt.Sprintf("Wrote %s", path))
		}
	}

	ui.Blank()
	return nil
}

func init() {
	agentInstallCmd.Flags().StringVar(&agentName, "agent", "all", "Agent target: copilot, assistant, cursor, claude, or all")
	agentInstallCmd.Flags().BoolVar(&agentAll, "all", false, "Generate files for every configured agent")
	agentCmd.AddCommand(agentInstallCmd)
	rootCmd.AddCommand(agentCmd)
}
