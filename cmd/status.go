// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"time"

	"github.com/envsync/envsync/internal/audit"
	"github.com/envsync/envsync/internal/peer"
	"github.com/envsync/envsync/internal/relay"
	"github.com/envsync/envsync/internal/store"
	"github.com/envsync/envsync/internal/ui"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current sync status",
	Long:  "Displays team info, last sync time, pending blobs, peer count, and version store summary.",
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

	ui.Header("EnvSync Status")

	// Identity
	short := kp.Fingerprint
	if len(short) > 30 {
		short = short[:30] + "..."
	}
	ui.Line(fmt.Sprintf("  Identity: %s", short))
	ui.Blank()

	// Team info
	pc, pcErr := peer.LoadProjectConfig()
	if pcErr == nil && pc.TeamID != "" {
		team, teamErr := peer.LoadTeam(pc.TeamID)
		if teamErr == nil {
			ui.Line(fmt.Sprintf("  Team:     %s (%d members)", team.Name, len(team.Members)))
		} else {
			ui.Line(fmt.Sprintf("  Team ID:  %s", pc.TeamID))
		}
		ui.Line(fmt.Sprintf("  File:     %s", pc.DefaultFile))
		ui.Line(fmt.Sprintf("  Strategy: %s", pc.SyncStrategy))
	} else {
		ui.Line("  Team:     (not configured — run 'envsync init')")
	}
	ui.Blank()

	// Relay pending
	client := relay.NewClient(cfg.Relay.URL, kp)
	teamID := ""
	if pc != nil {
		teamID = pc.TeamID
	}
	if teamID != "" {
		pending, err := client.ListPending(teamID)
		if err == nil {
			if len(pending) > 0 {
				ui.Warning(fmt.Sprintf("  %d pending blobs on relay — run 'envsync pull'", len(pending)))
			} else {
				ui.Success("  No pending blobs on relay")
			}
		} else {
			ui.Line("  Relay:    unavailable")
		}
	}
	ui.Blank()

	// Last activity from audit log
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

	// Version store
	vStore, storeErr := store.New(cfg.Sync.MaxVersions)
	if storeErr == nil && teamID != "" {
		versions, listErr := vStore.List(teamID)
		if listErr == nil {
			ui.Line(fmt.Sprintf("  Backups:  %d versions stored", len(versions)))
		}
	}

	ui.Blank()
	return nil
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
