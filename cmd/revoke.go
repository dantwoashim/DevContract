// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"strings"

	"github.com/envsync/envsync/internal/audit"
	"github.com/envsync/envsync/internal/peer"
	"github.com/envsync/envsync/internal/relay"
	"github.com/spf13/cobra"
)

var revokeCmd = &cobra.Command{
	Use:   "revoke <label>",
	Short: "Remove a member from your project",
	Long:  "Revokes a member's access and removes them from the relay.",
	Args:  cobra.ExactArgs(1),
	RunE:  runRevoke,
}

func runRevoke(cmd *cobra.Command, args []string) error {
	memberLabel := strings.TrimPrefix(args[0], "@")

	kp, err := loadIdentity()
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	project, _ := loadProjectContext()

	registry, err := peer.NewRegistry()
	if err != nil {
		return err
	}

	p, projectID, err := registry.FindPeerByLabel(memberLabel)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  * Revoking %s from project access\n", memberLabel)
	fmt.Println()

	if err := p.Revoke(); err != nil {
		return err
	}
	if err := registry.SavePeer(projectID, p); err != nil {
		return err
	}

	client := relay.NewClient(projectRelayURL(project, cfg), kp)
	if err := client.RemoveTeamMember(projectID, memberLabel); err != nil {
		fmt.Printf("  ! Relay: %s\n", err)
	}

	fmt.Printf("  + %s revoked from project %s\n", memberLabel, projectID)
	fmt.Printf("  - Status: %s %s\n", p.StatusIcon(), p.Trust)
	fmt.Println()
	fmt.Println("  New relay deliveries will stop, but this does not revoke secrets they already received.")

	logger, _ := audit.NewLogger()
	if logger != nil {
		_ = logger.Log(audit.Entry{
			Event:   audit.EventRevoke,
			Peer:    memberLabel,
			Details: fmt.Sprintf("project %s", projectID),
		})
	}

	return nil
}

func init() {
	rootCmd.AddCommand(revokeCmd)
}
