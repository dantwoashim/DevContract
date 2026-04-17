// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"

	"github.com/dantwoashim/Env_sync/internal/relay"
	"github.com/dantwoashim/Env_sync/internal/ui"
	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Show current relay limits",
	Long:  "Shows the relay tier and usage reported by the current deployment.",
	RunE:  runUpgrade,
}

var upgradePlan string

func runUpgrade(cmd *cobra.Command, args []string) error {
	kp, err := loadIdentity()
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	project, err := requireProjectContext()
	if err != nil {
		return err
	}

	client := relay.NewClient(projectRelayURL(project, cfg), kp)
	status, err := client.GetTierStatus(project.ProjectID)
	if err != nil {
		ui.RenderError(ui.ErrRelayUnavailable(err.Error()))
		return err
	}

	ui.Header("EnvSync Relay Limits")

	table := ui.NewTable("", "Current", "Team", "Enterprise")
	table.AddRow("Members", fmtUsage(status.Usage.Members, status.Limits.Members), "unlimited", "custom")
	table.AddRow("Relay syncs/day", fmtUsage(status.Usage.BlobsToday, status.Limits.BlobsPerDay), "unlimited", "custom")
	table.AddRow("History", fmt.Sprintf("%dd", status.Limits.HistoryDays), "30 days", "365 days")
	fmt.Print(table.Render())
	ui.Blank()

	ui.Line(fmt.Sprintf("Current tier: %s", ui.StyleBold.Render(status.Tier)))
	ui.Line("This reflects the relay entitlements configured on the current deployment.")
	return nil
}

func fmtUsage(current, limit int) string {
	if limit < 0 {
		return fmt.Sprintf("%d / unlimited", current)
	}
	return fmt.Sprintf("%d / %d", current, limit)
}

func init() {
	upgradeCmd.Flags().StringVar(&upgradePlan, "plan", "team", "Retained for compatibility; relay entitlements are configured by the deployment")
	rootCmd.AddCommand(upgradeCmd)
}
