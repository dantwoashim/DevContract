// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"time"

	"github.com/dantwoashim/devcontract/internal/audit"
	"github.com/dantwoashim/devcontract/internal/relay"
	"github.com/dantwoashim/devcontract/internal/ui"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "View sync audit log",
	Long:  "Displays recent sync events with timestamps, peers, and details.",
	RunE:  runAudit,
}

var (
	auditLast  int
	auditPeer  string
	auditEvent string
)

var auditRelayCmd = &cobra.Command{
	Use:   "relay",
	Short: "View relay-side administrative audit history",
	Long:  "Displays relay-side administrative events such as membership changes, invite lifecycle, and blob handling.",
	RunE:  runRelayAudit,
}

func runAudit(cmd *cobra.Command, args []string) error {
	logger, err := audit.NewLogger()
	if err != nil {
		return err
	}

	var entries []audit.Entry
	switch {
	case auditPeer != "":
		entries, err = logger.FilterByPeer(auditPeer, auditLast)
	case auditEvent != "":
		entries, err = logger.FilterByEvent(audit.EventType(auditEvent), auditLast)
	default:
		entries, err = logger.Read(auditLast)
	}
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		ui.Header("Audit Log")
		ui.Line("No events recorded yet.")
		ui.Blank()
		return nil
	}

	ui.Header("Audit Log")
	table := ui.NewTable("Time", "Event", "Peer", "File", "Method", "Details")
	for _, entry := range entries {
		peer := entry.Peer
		if peer == "" {
			peer = "-"
		}
		file := entry.File
		if file == "" {
			file = "-"
		}
		method := entry.Method
		if method == "" {
			method = "-"
		}

		details := entry.Details
		if details == "" && entry.VarsChanged > 0 {
			details = fmt.Sprintf("%d vars", entry.VarsChanged)
		}
		if details == "" && entry.DeliveryCount > 0 {
			details = fmt.Sprintf("%d deliveries", entry.DeliveryCount)
		}
		if details == "" {
			details = "-"
		}

		table.AddRow(
			entry.Timestamp.Format("01-02 15:04"),
			string(entry.Event),
			peer,
			file,
			method,
			details,
		)
	}
	fmt.Print(table.Render())
	ui.Blank()
	return nil
}

func init() {
	auditCmd.Flags().IntVar(&auditLast, "last", 20, "Show last N events")
	auditCmd.Flags().StringVar(&auditPeer, "peer", "", "Filter by peer @username")
	auditCmd.Flags().StringVar(&auditEvent, "event", "", "Filter by event type (push, pull, invite, etc.)")
	auditRelayCmd.Flags().IntVar(&auditLast, "last", 20, "Show last N relay audit events")
	auditCmd.AddCommand(auditRelayCmd)
	rootCmd.AddCommand(auditCmd)
}

func runRelayAudit(cmd *cobra.Command, args []string) error {
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
	events, err := client.ListTeamAudit(project.ProjectID, auditLast)
	if err != nil {
		return fmt.Errorf("loading relay audit history: %w", err)
	}

	ui.Header("Relay Audit")
	if len(events) == 0 {
		ui.Line("No relay audit events recorded yet.")
		ui.Blank()
		return nil
	}

	table := ui.NewTable("Time", "Action", "Actor", "Result", "Details")
	for _, event := range events {
		actor := event.ActorFingerprint
		if actor == "" {
			actor = "-"
		}
		details := event.Details
		if details == "" {
			switch {
			case event.TargetFingerprint != "":
				details = event.TargetFingerprint
			case event.InviteHash != "":
				details = event.InviteHash
			case event.BlobID != "":
				details = event.BlobID
			default:
				details = "-"
			}
		}
		table.AddRow(
			time.Unix(event.CreatedAt, 0).UTC().Format("01-02 15:04"),
			event.Action,
			actor,
			event.Result,
			details,
		)
	}
	fmt.Print(table.Render())
	ui.Blank()
	return nil
}
