// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"time"

	"github.com/envsync/envsync/internal/peer"
	"github.com/envsync/envsync/internal/relay"
	"github.com/spf13/cobra"
)

var peersReconcileImport bool

var peersReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Compare local project members with the relay member roster",
	Long:  "Shows local-vs-relay membership drift and can import relay-only members into the local registry as pending entries.",
	RunE:  runPeersReconcile,
}

func runPeersReconcile(cmd *cobra.Command, args []string) error {
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

	team, err := registry.LoadTeam(project.ProjectID)
	if err != nil {
		return fmt.Errorf("loading local project metadata: %w", err)
	}

	localPeers, err := registry.ListPeers(project.ProjectID)
	if err != nil {
		return fmt.Errorf("loading local peer registry: %w", err)
	}

	client := relay.NewClient(projectRelayURL(project, cfg), kp)
	relayMembers, err := client.ListTeamMembers(project.ProjectID)
	if err != nil {
		return fmt.Errorf("loading relay member roster: %w", err)
	}

	localPeerMap := make(map[string]peer.Peer, len(localPeers))
	for _, p := range localPeers {
		localPeerMap[p.Fingerprint] = p
	}

	relayMap := make(map[string]relay.TeamMember, len(relayMembers))
	for _, member := range relayMembers {
		relayMap[member.Fingerprint] = member
	}

	localMissingOnRelay := []string{}
	for _, fingerprint := range team.Members {
		if _, ok := relayMap[fingerprint]; ok || fingerprint == kp.Fingerprint {
			continue
		}
		localMissingOnRelay = append(localMissingOnRelay, fingerprint)
	}

	relayMissingLocal := []relay.TeamMember{}
	imported := 0
	for _, member := range relayMembers {
		if member.Fingerprint == kp.Fingerprint {
			continue
		}
		if _, ok := localPeerMap[member.Fingerprint]; ok {
			continue
		}
		relayMissingLocal = append(relayMissingLocal, member)
		if !peersReconcileImport {
			continue
		}

		p := &peer.Peer{
			DisplayName:          member.Username,
			RelayUsername:        member.Username,
			Fingerprint:          member.Fingerprint,
			Ed25519Public:        member.PublicKey,
			X25519Public:         member.TransportPublicKey,
			TransportFingerprint: member.TransportFingerprint,
			Trust:                peer.TrustPending,
			FirstSeen:            time.Now(),
			LastSeen:             time.Now(),
		}
		if err := registry.SavePeer(project.ProjectID, p); err != nil {
			return fmt.Errorf("importing relay member %s: %w", member.Fingerprint, err)
		}
		team.AddMember(member.Fingerprint)
		imported++
	}

	if peersReconcileImport && imported > 0 {
		if err := registry.SaveTeam(team); err != nil {
			return fmt.Errorf("saving updated project member list: %w", err)
		}
	}

	fmt.Println()
	fmt.Printf("  * Project %s membership reconciliation\n", project.ProjectID)
	fmt.Println()

	if len(localMissingOnRelay) == 0 && len(relayMissingLocal) == 0 {
		fmt.Println("  + Local registry and relay membership are in sync")
		fmt.Println()
		return nil
	}

	if len(localMissingOnRelay) > 0 {
		fmt.Println("  Local-only entries:")
		for _, fingerprint := range localMissingOnRelay {
			fmt.Printf("    - %s\n", fingerprint)
		}
		fmt.Println()
	}

	if len(relayMissingLocal) > 0 {
		fmt.Println("  Relay-only entries:")
		for _, member := range relayMissingLocal {
			fmt.Printf("    - %s (%s)\n", member.Username, member.Fingerprint)
		}
		if peersReconcileImport {
			fmt.Println()
			fmt.Printf("  + Imported %d relay member(s) into the local registry as pending\n", imported)
		} else {
			fmt.Println()
			fmt.Println("  Run 'envsync peers reconcile --import-pending' to import relay-only members locally as pending")
		}
		fmt.Println()
	}

	return nil
}

func init() {
	peersReconcileCmd.Flags().BoolVar(&peersReconcileImport, "import-pending", false, "Import relay-only members into the local registry as pending")
	peersCmd.AddCommand(peersReconcileCmd)
}
