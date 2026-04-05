// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"

	"github.com/envsync/envsync/internal/relay"
	"github.com/envsync/envsync/internal/ui"
	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Show current relay entitlement status",
	Long:  "Managed checkout is disabled in this build. This command only shows the current relay tier and usage.",
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

	ui.Header("EnvSync Plan")

	table := ui.NewTable("", "Current", "Team", "Enterprise")
	table.AddRow("Members", fmtUsage(status.Usage.Members, status.Limits.Members), "unlimited", "custom")
	table.AddRow("Relay syncs/day", fmtUsage(status.Usage.BlobsToday, status.Limits.BlobsPerDay), "unlimited", "custom")
	table.AddRow("History", fmt.Sprintf("%dd", status.Limits.HistoryDays), "30 days", "365 days")
	fmt.Print(table.Render())
	ui.Blank()

	ui.Line(fmt.Sprintf("Current tier: %s", ui.StyleBold.Render(status.Tier)))
	ui.Warning("Managed billing and hosted checkout are disabled in this build.")
	ui.Line("Contact the relay administrator or configure entitlements directly on the relay deployment if you need a different tier.")
	return nil
}

func fmtUsage(current, limit int) string {
	if limit < 0 {
		return fmt.Sprintf("%d / unlimited", current)
	}
	return fmt.Sprintf("%d / %d", current, limit)
}

func init() {
	upgradeCmd.Flags().StringVar(&upgradePlan, "plan", "team", "Retained for compatibility; managed checkout is disabled")
	rootCmd.AddCommand(upgradeCmd)
}
