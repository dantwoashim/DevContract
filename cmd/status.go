// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"time"

	"github.com/dantwoashim/devcontract/internal/audit"
	"github.com/dantwoashim/devcontract/internal/peer"
	"github.com/dantwoashim/devcontract/internal/relay"
	"github.com/dantwoashim/devcontract/internal/store"
	"github.com/dantwoashim/devcontract/internal/ui"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current sync status",
	Long:  "Displays project info, relay state, last activity, and backup history.",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	kp, err := loadIdentity()
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	project, projectErr := loadProjectContext()

	ui.Header("DevContract Status")

	short := kp.Fingerprint
	if len(short) > 30 {
		short = short[:30] + "..."
	}
	ui.Line(fmt.Sprintf("  Identity:   %s", short))
	ui.Line(fmt.Sprintf("  Transport:  %s", cfg.Identity.TransportFingerprint))
	if projectErr == nil {
		ui.Line(fmt.Sprintf("  Project ID: %s", project.ProjectID))
	}
	ui.Blank()

	teamID := ""
	if projectErr == nil && project != nil {
		teamID = project.ProjectID
		registry, registryErr := peer.NewRegistry()
		if registryErr == nil {
			team, teamErr := registry.LoadTeam(project.ProjectID)
			if teamErr == nil {
				ui.Line(fmt.Sprintf("  Project:    %s (%d members)", team.Name, len(team.Members)))
			} else {
				ui.Line(fmt.Sprintf("  Project:    %s", project.ProjectID))
			}
		} else {
			ui.Line(fmt.Sprintf("  Project:    %s", project.ProjectID))
		}
		ui.Line(fmt.Sprintf("  File:       %s", project.Config.DefaultFile))
		ui.Line(fmt.Sprintf("  Strategy:   %s", project.Config.SyncStrategy))
	} else {
		ui.Line("  Project:    (not configured — run 'devcontract init')")
	}
	ui.Blank()

	client := relay.NewClient(projectRelayURL(project, cfg), kp)
	if teamID != "" {
		pending, err := client.ListPending(teamID)
		if err == nil {
			if len(pending) > 0 {
				ui.Warning(fmt.Sprintf("  %d pending blobs on relay — run 'devcontract pull'", len(pending)))
			} else {
				ui.Success("  No pending blobs on relay")
			}
		} else {
			ui.Line("  Relay:      unavailable")
		}

		rejected, err := client.ListRejectedBlobs(teamID)
		if err == nil {
			if len(rejected) > 0 {
				ui.Warning(fmt.Sprintf("  %d rejected relay blob(s) need operator review", len(rejected)))
			} else {
				ui.Line("  Relay rejects: none")
			}
		}
	}
	ui.Blank()

	logger, logErr := audit.NewLogger()
	if logErr == nil {
		entries, readErr := logger.Read(1)
		if readErr == nil && len(entries) > 0 {
			last := entries[0]
			ago := time.Since(last.Timestamp).Truncate(time.Second)
			ui.Line(fmt.Sprintf("  Last activity: %s %s (%s ago)", last.Event, last.Peer, ago))
		} else {
			ui.Line("  Last activity: (no events)")
		}
	}

	if teamID != "" {
		vStore, storeErr := store.New(cfg.Sync.MaxVersions)
		if storeErr == nil {
			versions, listErr := vStore.List(teamID)
			if listErr == nil {
				ui.Line(fmt.Sprintf("  Backups:    %d versions stored", len(versions)))
			}
		}
	}

	ui.Blank()
	return nil
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
