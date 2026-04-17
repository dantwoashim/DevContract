// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"

	"github.com/dantwoashim/devcontract/internal/ui"
	"github.com/spf13/cobra"
)

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore a .env from encrypted backup",
	Long:  "Lists available versions and restores the selected one.",
	RunE:  runRestore,
}

var restoreVersion int

func runRestore(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	kp, err := loadIdentity()
	if err != nil {
		return err
	}

	project, err := requireProjectContext()
	if err != nil {
		return err
	}

	targetFile, _ := cmd.Flags().GetString("file")
	targetFile = projectTargetFile(targetFile, cmd.Flags().Changed("file"), project, cfg)

	key, err := atRestKey(kp)
	if err != nil {
		return err
	}

	vStore, err := openProjectStore(cfg, project.ProjectID, key)
	if err != nil {
		return err
	}

	versions, err := vStore.List(project.ProjectID)
	if err != nil || len(versions) == 0 {
		ui.Header("Restore")
		ui.Line("No backups found. Run 'devcontract backup' first.")
		ui.Blank()
		return nil
	}

	ui.Header("Available Backups")

	table := ui.NewTable("Version", "Timestamp", "Size")
	for _, v := range versions {
		ts := v.Timestamp.Format("2006-01-02 15:04:05")
		table.AddRow(
			fmt.Sprintf("#%d", v.Sequence),
			ts,
			fmt.Sprintf("%d bytes", v.SizeBytes),
		)
	}
	fmt.Print(table.Render())
	ui.Blank()

	target := versions[0].Sequence
	if restoreVersion > 0 {
		target = restoreVersion
	}

	data, err := vStore.Restore(project.ProjectID, target, key)
	if err != nil {
		return fmt.Errorf("loading version #%d: %w", target, err)
	}

	if !ui.ConfirmAction(fmt.Sprintf("Restore version #%d to %s?", target, targetFile), true) {
		ui.Line("Cancelled.")
		return nil
	}

	if err := writeEnvFile(targetFile, data); err != nil {
		return fmt.Errorf("writing %s: %w", targetFile, err)
	}

	ui.Success(fmt.Sprintf("Restored version #%d -> %s (%d bytes)", target, targetFile, len(data)))
	ui.Blank()

	return nil
}

func init() {
	restoreCmd.Flags().IntVar(&restoreVersion, "version", 0, "Specific version number to restore")
	rootCmd.AddCommand(restoreCmd)
}
