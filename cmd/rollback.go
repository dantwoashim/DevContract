// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"

	"github.com/dantwoashim/Env_sync/internal/audit"
	"github.com/dantwoashim/Env_sync/internal/ui"
	"github.com/spf13/cobra"
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Restore the most recent encrypted backup",
	Long:  "Restores the latest encrypted backup for the current project to the target .env file.",
	RunE:  runRollback,
}

func runRollback(cmd *cobra.Command, args []string) error {
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

	latest, err := vStore.Latest(project.ProjectID)
	if err != nil {
		return err
	}
	if latest == nil {
		ui.Header("Rollback")
		ui.Line("No encrypted backups are available yet.")
		ui.Blank()
		return nil
	}

	data, err := vStore.Restore(project.ProjectID, latest.Sequence, key)
	if err != nil {
		return fmt.Errorf("restoring latest backup #%d: %w", latest.Sequence, err)
	}

	if !ui.ConfirmAction(fmt.Sprintf("Restore backup #%d to %s?", latest.Sequence, targetFile), true) {
		ui.Line("Cancelled.")
		return nil
	}

	if err := writeEnvFile(targetFile, data); err != nil {
		return fmt.Errorf("writing %s: %w", targetFile, err)
	}

	logger, _ := audit.NewLogger()
	if logger != nil {
		_ = logger.Log(audit.Entry{
			Event:   audit.EventRestore,
			File:    targetFile,
			Details: fmt.Sprintf("rollback to version #%d", latest.Sequence),
		})
	}

	ui.Success(fmt.Sprintf("Rolled back %s to version #%d", targetFile, latest.Sequence))
	ui.Blank()
	return nil
}

func init() {
	rootCmd.AddCommand(rollbackCmd)
}
