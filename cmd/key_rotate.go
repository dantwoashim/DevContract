// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/envsync/envsync/internal/audit"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/peer"
	"github.com/envsync/envsync/internal/relay"
	"github.com/envsync/envsync/internal/store"
	"github.com/envsync/envsync/internal/ui"
	"github.com/spf13/cobra"
)

var keyRotateCmd = &cobra.Command{
	Use:   "key-rotate",
	Short: "Rotate your identity after an SSH key change",
	Long: `Rotate your EnvSync identity when you move to a different Ed25519 SSH key.

This command:
  1. loads the previous and replacement SSH keys
  2. re-encrypts local backups with the new at-rest key
  3. updates local team metadata to the new fingerprint
  4. re-registers your new public keys on the relay`,
	RunE: runKeyRotate,
}

var (
	newKeyPath string
	oldKeyPath string
)

func runKeyRotate(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if cfg.Identity.Fingerprint == "" {
		return fmt.Errorf("no existing identity found\n\n  Run 'envsync init' first")
	}

	currentKeyPath := cfg.Identity.SSHKeyPath
	if currentKeyPath == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return fmt.Errorf("determining home directory: %w", homeErr)
		}
		currentKeyPath = filepath.Join(home, ".ssh", "id_ed25519")
	}
	if oldKeyPath == "" {
		oldKeyPath = currentKeyPath
	}
	if newKeyPath == "" {
		newKeyPath = currentKeyPath
	}

	oldKP, err := loadSSHKeyWithPrompt(oldKeyPath)
	if err != nil {
		return fmt.Errorf("loading previous SSH key: %w", err)
	}
	if oldKP.Fingerprint != cfg.Identity.Fingerprint {
		return fmt.Errorf("previous SSH key fingerprint %s does not match the configured identity %s", oldKP.Fingerprint, cfg.Identity.Fingerprint)
	}

	newKP, err := loadSSHKeyWithPrompt(newKeyPath)
	if err != nil {
		return fmt.Errorf("loading replacement SSH key: %w", err)
	}
	if newKP.Fingerprint == oldKP.Fingerprint {
		ui.Warning("Replacement SSH key matches the current identity; nothing to rotate.")
		return nil
	}

	ui.Header("Key Rotation")
	ui.Line(fmt.Sprintf("  Previous identity: %s", ui.StyleDim.Render(oldKP.Fingerprint)))
	ui.Line(fmt.Sprintf("  New identity:      %s", ui.StyleCode.Render(newKP.Fingerprint)))
	ui.Blank()

	oldAtRestKey, err := atRestKey(oldKP)
	if err != nil {
		return err
	}
	newAtRestKey, err := atRestKey(newKP)
	if err != nil {
		return err
	}

	registry, err := peer.NewRegistry()
	if err != nil {
		return err
	}

	projectIDs, err := registry.ListTeams()
	if err != nil {
		return err
	}

	vStore, err := store.New(cfg.Sync.MaxVersions)
	if err != nil {
		return err
	}

	rotatedBackups := 0
	updatedTeams := 0
	relayUpdates := 0
	client := relay.NewClient(projectRelayURL(nil, cfg), newKP)
	memberLabel := displayMemberLabel(cfg, newKP)

	for _, projectID := range projectIDs {
		latest, err := vStore.Latest(projectID)
		if err != nil {
			return fmt.Errorf("checking backups for %s: %w", projectID, err)
		}
		if latest != nil {
			if err := vStore.Reencrypt(projectID, oldAtRestKey, newAtRestKey); err != nil {
				return fmt.Errorf("re-encrypting backups for %s: %w", projectID, err)
			}
			rotatedBackups++
		}

		team, err := registry.LoadTeam(projectID)
		if err == nil {
			if team.CreatedBy == oldKP.Fingerprint {
				team.CreatedBy = newKP.Fingerprint
			}
			replaced := false
			for i, member := range team.Members {
				if member == oldKP.Fingerprint {
					team.Members[i] = newKP.Fingerprint
					replaced = true
				}
			}
			if !replaced {
				team.AddMember(newKP.Fingerprint)
			}
			if err := registry.SaveTeam(team); err != nil {
				return fmt.Errorf("saving updated team metadata for %s: %w", projectID, err)
			}
			updatedTeams++
		}

		role := "member"
		if team != nil && team.CreatedBy == newKP.Fingerprint {
			role = "owner"
		}
		if err := client.AddTeamMember(
			projectID,
			memberLabel,
			newKP.Fingerprint,
			base64.StdEncoding.EncodeToString(newKP.Ed25519Public),
			base64.StdEncoding.EncodeToString(newKP.X25519Public[:]),
			crypto.ComputeFingerprint(newKP.X25519Public),
			role,
		); err == nil {
			relayUpdates++
		} else {
			ui.Warning(fmt.Sprintf("  Relay update skipped for %s: %s", projectID, err))
		}
	}

	cfg.Identity.SSHKeyPath = newKeyPath
	cfg.Identity.Fingerprint = newKP.Fingerprint
	cfg.Identity.IdentityPublicKey = base64.StdEncoding.EncodeToString(newKP.Ed25519Public)
	cfg.Identity.TransportPublicKey = base64.StdEncoding.EncodeToString(newKP.X25519Public[:])
	cfg.Identity.TransportFingerprint = crypto.ComputeFingerprint(newKP.X25519Public)
	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	ui.Success("Identity rotation complete")
	ui.Line(fmt.Sprintf("  Re-encrypted backup namespaces: %d", rotatedBackups))
	ui.Line(fmt.Sprintf("  Updated team records:          %d", updatedTeams))
	ui.Line(fmt.Sprintf("  Relay registrations updated:   %d", relayUpdates))
	ui.Blank()

	logger, _ := audit.NewLogger()
	if logger != nil {
		_ = logger.Log(audit.Entry{
			Event:   audit.EventKeyRotate,
			Details: fmt.Sprintf("identity rotated from %s to %s", oldKP.Fingerprint, newKP.Fingerprint),
		})
	}

	return nil
}

func init() {
	keyRotateCmd.Flags().StringVar(&newKeyPath, "key", "", "Path to the replacement SSH private key")
	keyRotateCmd.Flags().StringVar(&oldKeyPath, "old-key", "", "Path to the previous SSH private key (defaults to the configured key path)")
	rootCmd.AddCommand(keyRotateCmd)
}
