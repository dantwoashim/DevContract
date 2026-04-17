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

var transferOwnershipCmd = &cobra.Command{
	Use:   "transfer-ownership <label-or-fingerprint>",
	Short: "Transfer project ownership to another human member",
	Long:  "Promotes the target member to owner on the relay and demotes the current owner to member in one explicit workflow.",
	Args:  cobra.ExactArgs(1),
	RunE:  runTransferOwnership,
}

func runTransferOwnership(cmd *cobra.Command, args []string) error {
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

	target, err := registry.FindPeerInTeam(project.ProjectID, selector)
	if err != nil {
		return err
	}

	client := relay.NewClient(projectRelayURL(project, cfg), kp)
	if err := client.TransferOwnership(project.ProjectID, target.Fingerprint); err != nil {
		return fmt.Errorf("transferring ownership: %w", err)
	}

	fmt.Println()
	fmt.Printf("  + Ownership transferred to %s\n", target.DisplayName)
	fmt.Printf("  - Project: %s\n", project.ProjectID)
	fmt.Println()

	logger, _ := audit.NewLogger()
	if logger != nil {
		_ = logger.Log(audit.Entry{
			Event:   audit.EventOwnershipTransfer,
			Peer:    target.Fingerprint,
			Details: fmt.Sprintf("ownership transferred for project %s", project.ProjectID),
		})
	}

	return nil
}

func init() {
	rootCmd.AddCommand(transferOwnershipCmd)
}
