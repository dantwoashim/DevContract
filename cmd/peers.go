// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"

	"github.com/dantwoashim/Env_sync/internal/peer"
	"github.com/spf13/cobra"
)

var peersCmd = &cobra.Command{
	Use:   "peers",
	Short: "List project members",
	Long:  "Shows all known members in your project registry with their trust status.",
	RunE:  runPeers,
}

func runPeers(cmd *cobra.Command, args []string) error {
	registry, err := peer.NewRegistry()
	if err != nil {
		return err
	}

	projects, err := registry.ListTeams()
	if err != nil {
		return err
	}

	if len(projects) == 0 {
		fmt.Println()
		fmt.Println("  No project members found. Start one with:")
		fmt.Println("    envsync invite project-member")
		fmt.Println()
		return nil
	}

	fmt.Println()
	for _, projectID := range projects {
		project, err := registry.LoadTeam(projectID)
		if err != nil {
			continue
		}

		fmt.Printf("  * Project: %s\n", project.Name)
		fmt.Println()

		peers, err := registry.ListPeers(projectID)
		if err != nil {
			continue
		}

		if len(peers) == 0 {
			fmt.Println("    No members yet.")
		} else {
			fmt.Print("    ")
			fmt.Printf("%-2s %-20s %-44s %s\n", "", "Member", "Fingerprint", "Status")
			fmt.Print("    ")
			fmt.Println("-- -------------------- -------------------------------------------- --------")
			for _, p := range peers {
				label := p.DisplayName
				if label == "" {
					label = "(unnamed)"
				}
				fingerprint := p.Fingerprint
				if len(fingerprint) > 44 {
					fingerprint = fingerprint[:44]
				}
				fmt.Print("    ")
				fmt.Printf("%s  %-20s %-44s %s\n", p.StatusIcon(), label, fingerprint, p.Trust)
			}
		}
		fmt.Println()
	}

	return nil
}

func init() {
	rootCmd.AddCommand(peersCmd)
}
