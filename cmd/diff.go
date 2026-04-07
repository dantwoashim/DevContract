// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"os"

	"github.com/dantwoashim/Env_sync/internal/envfile"
	"github.com/dantwoashim/Env_sync/internal/ui"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff [file]",
	Short: "Show differences in .env files",
	Long: `Compares the current .env file against the last synced version from the store.

  envsync diff                  Diff current .env vs last backup
  envsync diff --against other  Diff current .env vs another file`,
	RunE: runDiff,
}

var diffAgainst string

func runDiff(cmd *cobra.Command, args []string) error {
	cfg, _ := loadConfig()
	project, _ := loadProjectContext()

	targetFile, _ := cmd.Flags().GetString("file")
	targetFile = projectTargetFile(targetFile, cmd.Flags().Changed("file"), project, cfg)

	currentData, err := os.ReadFile(targetFile)
	if err != nil {
		ui.RenderError(ui.ErrEnvFileNotFound(targetFile))
		return fmt.Errorf("file not found: %s", targetFile)
	}

	currentEnv, err := envfile.Parse(string(currentData))
	if err != nil {
		return fmt.Errorf("parsing %s: %w", targetFile, err)
	}

	var compareEnv *envfile.EnvFile
	var compareLabel string

	if diffAgainst != "" || len(args) > 0 {
		compareFile := diffAgainst
		if len(args) > 0 {
			compareFile = args[0]
		}

		compareData, err := os.ReadFile(compareFile)
		if err != nil {
			ui.RenderError(ui.StructuredError{
				Category:   ui.ErrFile,
				Message:    fmt.Sprintf("Cannot read %s", compareFile),
				Cause:      err.Error(),
				Suggestion: "Check the file path",
			})
			return err
		}

		compareEnv, err = envfile.Parse(string(compareData))
		if err != nil {
			return fmt.Errorf("parsing %s: %w", compareFile, err)
		}
		compareLabel = compareFile
	} else if project != nil {
		kp, kpErr := loadIdentity()
		if kpErr == nil {
			key, keyErr := atRestKey(kp)
			if keyErr == nil {
				vStore, storeErr := openProjectStore(cfg, project.ProjectID, key)
				if storeErr == nil {
					latest, latestErr := vStore.Latest(project.ProjectID)
					if latestErr == nil && latest != nil {
						restoredData, restoreErr := vStore.Restore(project.ProjectID, latest.Sequence, key)
						if restoreErr == nil {
							compareEnv, _ = envfile.Parse(string(restoredData))
							compareLabel = fmt.Sprintf("backup v%d (%s)", latest.Sequence, latest.Timestamp.Format("2006-01-02 15:04"))
						}
					}
				}
			}
		}
	}

	ui.Header("EnvSync Diff")

	if compareEnv == nil {
		ui.Line(fmt.Sprintf("  File: %s (%d variables)", targetFile, currentEnv.VariableCount()))
		ui.Blank()
		ui.Line(ui.StyleDim.Render("  No previous version to compare against."))
		ui.Line(ui.StyleDim.Render("  Run 'envsync backup' to create a baseline."))
		ui.Blank()
		return nil
	}

	ui.Line(fmt.Sprintf("  %s %s %s", targetFile, ui.StyleDim.Render(ui.IconArrow), compareLabel))
	ui.Blank()

	diff := envfile.Diff(compareEnv, currentEnv)
	if !diff.HasChanges() {
		ui.Success("Files are identical")
	} else {
		fmt.Print(ui.RenderDiff(diff))
	}
	ui.Blank()

	return nil
}

func init() {
	diffCmd.Flags().StringVar(&diffAgainst, "against", "", "Compare against a specific file")
	rootCmd.AddCommand(diffCmd)
}
