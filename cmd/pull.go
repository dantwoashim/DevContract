// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/envsync/envsync/internal/audit"
	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/envfile"
	"github.com/envsync/envsync/internal/peer"
	"github.com/envsync/envsync/internal/relay"
	envsync "github.com/envsync/envsync/internal/sync"
	"github.com/envsync/envsync/internal/ui"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull .env from team peers",
	Long: `Listens for incoming pushes and checks relay for pending blobs.

Priority: check relay first → then listen on LAN.`,
	RunE: runPull,
}

func runPull(cmd *cobra.Command, args []string) error {
	kp, err := loadIdentity()
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Verify identity is configured
	if cfg.Identity.Fingerprint == "" {
		ui.RenderError(ui.StructuredError{
			Category:   ui.ErrConfig,
			Message:    "Not initialized",
			Cause:      "No identity configured",
			Suggestion: "Run 'envsync init' to set up your identity",
		})
		return fmt.Errorf("not initialized: run 'envsync init' first")
	}

	noiseKP := crypto.NewNoiseKeypair(kp.X25519Private, kp.X25519Public)
	targetFile, _ := cmd.Flags().GetString("file")
	if targetFile == "" {
		targetFile = ".env"
	}

	ui.Header("EnvSync Pull")

	// Phase 1: Check relay for pending blobs
	relayClient := relay.NewClient(cfg.Relay.URL, kp)

	// Prefer team ID from project config (.envsync.toml), fallback to derived
	teamID := ""
	if pc, pcErr := peer.LoadProjectConfig(); pcErr == nil && pc.TeamID != "" {
		teamID = pc.TeamID
	} else {
		teamID = generateTeamID(kp.Fingerprint)
	}

	ui.Line("  Checking relay for pending blobs...")
	pending, relayErr := relayClient.ListPending(teamID)
	if relayErr == nil && len(pending) > 0 {
		ui.Line(fmt.Sprintf("  Found %d pending blob(s) on relay", len(pending)))

		for _, blob := range pending {
			ui.Line(fmt.Sprintf("  ▸ Downloading %s from %s...", blob.Filename, shortFP(blob.SenderFingerprint)))

			data, ephKeyB64, _, err := relayClient.DownloadBlob(teamID, blob.BlobID)
			if err != nil {
				ui.Warning(fmt.Sprintf("  Download failed: %s", err))
				continue
			}

			// Decrypt the blob
			ephKeyBytes, decErr := base64.StdEncoding.DecodeString(ephKeyB64)
			if decErr != nil {
				ui.Warning(fmt.Sprintf("  Invalid ephemeral key: %s", decErr))
				continue
			}
			var ephKey [32]byte
			copy(ephKey[:], ephKeyBytes)

			plaintext, err := crypto.DecryptFromSender(data, ephKey, kp.X25519Private, kp.X25519Public)
			if err != nil {
				ui.Warning(fmt.Sprintf("  Decryption failed: %s", err))
				continue
			}

			// Show diff and confirm
			applied, applyErr := applyReceivedData(targetFile, plaintext, blob.Filename)
			if applyErr != nil {
				ui.Warning(fmt.Sprintf("  Apply failed: %s", applyErr))
				continue
			}

			if applied {
				// Clean up the blob from relay (best-effort)
				if delErr := relayClient.DeleteBlob(teamID, blob.BlobID); delErr != nil {
					ui.Warning(fmt.Sprintf("  Failed to clean up blob: %s", delErr))
				}

				// Audit (best-effort)
				logger, _ := audit.NewLogger()
				if logger != nil {
					if auditErr := logger.Log(audit.Entry{
						Event:  audit.EventPull,
						Peer:   blob.SenderFingerprint,
						File:   targetFile,
						Method: "relay",
					}); auditErr != nil {
						ui.Warning(fmt.Sprintf("  Audit log write failed: %s", auditErr))
					}
				}
			}
		}

		ui.Blank()
		return nil
	}

	if relayErr == nil {
		ui.Line("  No pending blobs on relay")
	}

	// Phase 2: Listen for LAN push
	ui.Line("  Listening for LAN push...")
	ui.Blank()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := envsync.Pull(ctx, envsync.PullOptions{
		EnvFilePath:        targetFile,
		Port:               config.DefaultPort,
		KeyPair:            kp,
		NoiseKeypair:       noiseKP,
		ConfirmBeforeApply: true,
		OnListening: func(port int) {
			ui.Line(fmt.Sprintf("  ▸ Listening on port %d (Ctrl+C to cancel)", port))
		},
		OnReceived: func(payload envsync.EnvPayload, diff *envfile.DiffResult) {
			ui.Line(fmt.Sprintf("  ▸ Received %s (%d bytes)", payload.FileName, len(payload.Data)))
			if diff != nil && diff.HasChanges() {
				ui.Blank()
				fmt.Print(ui.RenderDiff(diff))
				ui.Blank()
			}
		},
		OnConfirm: func(diff *envfile.DiffResult) bool {
			return ui.ConfirmAction(fmt.Sprintf("Apply changes? (%s)", diff.Summary()), true)
		},
		OnApplied: func(fileName string) {
			ui.Success(fmt.Sprintf("Applied to %s", fileName))
		},
	})
	if err != nil {
		ui.RenderError(ui.StructuredError{
			Category:   ui.ErrSync,
			Message:    "Pull failed",
			Cause:      err.Error(),
			Suggestion: "Ensure the sender is running 'envsync push'",
		})
		return err
	}

	// Audit log
	if result.Applied {
		logger, _ := audit.NewLogger()
		if logger != nil {
			logger.Log(audit.Entry{
				Event:       audit.EventPull,
				File:        result.FileName,
				VarsChanged: result.VarCount,
				Method:      "lan",
			})
		}
	}

	ui.Blank()
	return nil
}

func shortFP(fp string) string {
	if len(fp) > 16 {
		return fp[:16] + "..."
	}
	return fp
}

// applyReceivedData shows diff, confirms, and writes the file.
func applyReceivedData(targetFile string, data []byte, fileName string) (bool, error) {
	receivedEnv, err := envfile.Parse(string(data))
	if err != nil {
		return false, fmt.Errorf("parsing received data: %w", err)
	}

	// Diff against current
	currentData, _ := readLocalEnv(targetFile)
	if currentData != nil {
		currentEnv, _ := envfile.Parse(string(currentData))
		if currentEnv != nil {
			diff := envfile.Diff(currentEnv, receivedEnv)
			if diff.HasChanges() {
				fmt.Print(ui.RenderDiff(diff))
				ui.Blank()
				if !ui.ConfirmAction("Apply these changes?", true) {
					ui.Line("  Skipped.")
					return false, nil
				}
			}
		}
	}

	if err := writeEnvFile(targetFile, data); err != nil {
		return false, err
	}

	ui.Success(fmt.Sprintf("Applied %s (%d variables)", fileName, receivedEnv.VariableCount()))
	return true, nil
}

func init() {
	rootCmd.AddCommand(pullCmd)
}
