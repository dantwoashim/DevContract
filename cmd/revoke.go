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
	username := strings.TrimPrefix(args[0], "@")

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

	// Find the peer
	p, teamID, err := registry.FindPeerByUsername(username)
	if err != nil {
		return fmt.Errorf("peer @%s not found in any team", username)
	}

	fmt.Println()
	fmt.Printf("  ✦ Revoking @%s from team\n", username)
	fmt.Println()

	// Revoke locally
	if err := p.Revoke(); err != nil {
		return err
	}
	if err := registry.SavePeer(teamID, p); err != nil {
		return err
	}

	// Remove from relay
	client := relay.NewClient(projectRelayURL(project, cfg), kp)
	if err := client.RemoveTeamMember(teamID, username); err != nil {
		fmt.Printf("  ⚠ Relay: %s\n", err)
	}

	fmt.Printf("  ✓ @%s revoked from team %s\n", username, teamID)
	fmt.Printf("  ▸ Status: %s %s\n", p.StatusIcon(), p.Trust)
	fmt.Println()
	fmt.Println("  They can no longer sync or decrypt your .env files.")

	// Audit
	logger, _ := audit.NewLogger()
	if logger != nil {
		logger.Log(audit.Entry{
			Event:   audit.EventRevoke,
			Peer:    username,
			Details: fmt.Sprintf("team %s", teamID),
		})
	}

	return nil
}

func init() {
	rootCmd.AddCommand(revokeCmd)
}
