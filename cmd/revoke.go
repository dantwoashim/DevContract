// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"strings"

	"github.com/dantwoashim/devcontract/internal/audit"
	"github.com/dantwoashim/devcontract/internal/peer"
	"github.com/dantwoashim/devcontract/internal/relay"
	"github.com/spf13/cobra"
)

var revokeCmd = &cobra.Command{
	Use:   "revoke <label-or-fingerprint>",
	Short: "Remove a member from your project",
	Long:  "Revokes a member's access and removes them from the relay.",
	Args:  cobra.ExactArgs(1),
	RunE:  runRevoke,
}

func runRevoke(cmd *cobra.Command, args []string) error {
	selector := strings.TrimPrefix(args[0], "@")

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

	registry, err := peer.NewRegistry()
	if err != nil {
		return err
	}

	p, err := registry.FindPeerInTeam(project.ProjectID, selector)
	if err != nil {
		return err
	}
	projectID := project.ProjectID
	memberLabel := p.DisplayName
	if memberLabel == "" {
		memberLabel = p.RelayUsername
	}
	if memberLabel == "" {
		memberLabel = p.Fingerprint
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
	if team, teamErr := registry.LoadTeam(projectID); teamErr == nil {
		team.RemoveMember(p.Fingerprint)
		if err := registry.SaveTeam(team); err != nil {
			return err
		}
	}

	client := relay.NewClient(projectRelayURL(project, cfg), kp)
	if err := client.RemoveTeamMemberByFingerprint(projectID, p.Fingerprint); err != nil {
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
			Peer:    p.Fingerprint,
			Details: fmt.Sprintf("project %s", projectID),
		})
	}

	return nil
}

func init() {
	rootCmd.AddCommand(revokeCmd)
}
