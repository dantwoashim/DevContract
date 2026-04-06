// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"os"

	"github.com/envsync/envsync/internal/ui"
	"github.com/spf13/cobra"
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Create an encrypted backup of your .env",
	Long:  "Encrypts the current .env file and saves it to the version store.",
	RunE:  runBackup,
}

func runBackup(cmd *cobra.Command, args []string) error {
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

	data, err := os.ReadFile(targetFile)
	if err != nil {
		ui.RenderError(ui.ErrEnvFileNotFound(targetFile))
		return fmt.Errorf("file not found: %s", targetFile)
	}

	key, err := atRestKey(kp)
	if err != nil {
		return err
	}

	vStore, err := openProjectStore(cfg, project.ProjectID, key)
	if err != nil {
		return err
	}

	version, err := vStore.Append(project.ProjectID, data, key)
	if err != nil {
		return fmt.Errorf("saving backup: %w", err)
	}

	ui.Header("Backup Created")
	ui.Success(fmt.Sprintf("Encrypted %s (%d bytes) -> version #%d", targetFile, len(data), version.Sequence))
	ui.Blank()

	return nil
}

func init() {
	rootCmd.AddCommand(backupCmd)
}
