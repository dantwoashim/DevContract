// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"

	"github.com/envsync/envsync/internal/ui"
	"github.com/spf13/cobra"
)

var runDryRun bool
var runTrustContract bool
var runRestricted bool

var runCmd = &cobra.Command{
	Use:   "run [target]",
	Short: "Run a named workflow target from the repo contract",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runRun,
}

func runRun(cmd *cobra.Command, args []string) error {
	ctx, err := requireContractContext()
	if err != nil {
		return err
	}

	targetName := ctx.Contract.Run.Default
	if len(args) > 0 {
		targetName = args[0]
	}
	if targetName == "" {
		return fmt.Errorf("no run target selected and run.default is not configured")
	}

	target, ok := ctx.Contract.Run.Targets[targetName]
	if !ok {
		return fmt.Errorf("run target %q is not defined in %s", targetName, ctx.Path)
	}

	ui.Header("EnvSync Run")
	ui.Line(fmt.Sprintf("  Target: %s", targetName))
	ui.Line(fmt.Sprintf("  Command: %s", renderShellCommand("", target.Command)))
	ui.Blank()

	if runDryRun {
		ui.Warning("Dry run enabled; command was not executed.")
		return nil
	}
	if runRestricted {
		ui.Warning("Restricted mode blocked repo-defined shell execution.")
		return nil
	}
	if err := ensureContractTrust(ctx, []string{renderShellCommand("", target.Command)}, runTrustContract); err != nil {
		return err
	}

	if err := runShellCommand(ctx.Root, "", target.Command); err != nil {
		return fmt.Errorf("run target %s failed: %w", targetName, err)
	}

	ui.Success(fmt.Sprintf("Run target %s completed", targetName))
	return nil
}

func init() {
	runCmd.Flags().BoolVar(&runDryRun, "dry-run", false, "Show the resolved command without executing it")
	runCmd.Flags().BoolVar(&runTrustContract, "trust-contract", false, "Trust the current repo contract and persist that decision for this contract revision")
	runCmd.Flags().BoolVar(&runRestricted, "restricted", false, "Block repo-defined shell execution and only show the resolved command")
	rootCmd.AddCommand(runCmd)
}
