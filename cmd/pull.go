// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/envsync/envsync/internal/audit"
	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/envfile"
	"github.com/envsync/envsync/internal/relay"
	envsync "github.com/envsync/envsync/internal/sync"
	"github.com/envsync/envsync/internal/ui"
	"github.com/spf13/cobra"
)

var (
	pullTimeoutSeconds int
	pullServiceKeyPath string
	pullTeamID         string
	pullRelayURL       string
	pullJSON           bool
)

type pullReport struct {
	ProjectID    string   `json:"project_id"`
	TargetFile   string   `json:"target_file"`
	RelayChecked bool     `json:"relay_checked"`
	RelayApplied int      `json:"relay_applied"`
	LANApplied   bool     `json:"lan_applied"`
	Method       string   `json:"method"`
	Methods      []string `json:"methods,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull .env from project peers",
	Long: `Checks the relay for pending encrypted blobs, then listens for LAN pushes.

Priority: encrypted relay first -> LAN direct.`,
	RunE: runPull,
}

func runPull(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	var kp *crypto.KeyPair
	if pullServiceKeyPath != "" {
		kp, err = loadIdentityFromServiceKey(pullServiceKeyPath)
	} else {
		kp, err = loadIdentity()
	}
	if err != nil {
		return err
	}

	if pullServiceKeyPath == "" && cfg.Identity.Fingerprint == "" {
		ui.RenderError(ui.StructuredError{
			Category:   ui.ErrConfig,
			Message:    "Not initialized",
			Cause:      "No identity configured",
			Suggestion: "Run 'envsync init' to set up your identity",
		})
		return fmt.Errorf("not initialized: run 'envsync init' first")
	}

	project, _ := loadProjectContext()
	if pullTeamID != "" {
		if project == nil {
			project = &projectContext{Config: nil, ProjectID: pullTeamID}
		} else {
			project.ProjectID = pullTeamID
		}
	}
	if project == nil || project.ProjectID == "" {
		return fmt.Errorf("project ID is not configured\n\n  Run 'envsync init' or use '--team <project-id>'")
	}

	noiseKP := crypto.NewNoiseKeypair(kp.X25519Private, kp.X25519Public)
	targetFile, _ := cmd.Flags().GetString("file")
	targetFile = projectTargetFile(targetFile, cmd.Flags().Changed("file"), project, cfg)

	report := pullReport{
		ProjectID:  project.ProjectID,
		TargetFile: targetFile,
	}

	relayURL := pullRelayURL
	if relayURL == "" {
		relayURL = projectRelayURL(project, cfg)
	}

	ui.Header("EnvSync Pull")

	relayClient := relay.NewClient(relayURL, kp)
	memberKeys, _ := relayClient.ListTeamMembers(project.ProjectID)
	memberKeyMap := make(map[string]ed25519.PublicKey, len(memberKeys))
	for _, member := range memberKeys {
		if member.PublicKey == "" {
			continue
		}
		decoded, decErr := base64.StdEncoding.DecodeString(member.PublicKey)
		if decErr == nil && len(decoded) == ed25519.PublicKeySize {
			memberKeyMap[member.Fingerprint] = ed25519.PublicKey(decoded)
		}
	}

	ui.Line("  Checking relay for pending blobs...")
	report.RelayChecked = true
	pending, relayErr := relayClient.ListPending(project.ProjectID)
	if relayErr == nil && len(pending) > 0 {
		ui.Line(fmt.Sprintf("  Found %d pending blob(s) on relay", len(pending)))

		for _, blob := range pending {
			ui.Line(fmt.Sprintf("  - Downloading %s from %s...", blob.Filename, shortFP(blob.SenderFingerprint)))

			data, ephKeyB64, _, sigB64, err := relayClient.DownloadBlob(project.ProjectID, blob.BlobID)
			if err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("relay download failed for %s: %v", blob.BlobID, err))
				ui.Warning(fmt.Sprintf("  Download failed: %s", err))
				continue
			}

			ephKeyBytes, decErr := base64.StdEncoding.DecodeString(ephKeyB64)
			if decErr != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("invalid relay ephemeral key for %s: %v", blob.BlobID, decErr))
				ui.Warning(fmt.Sprintf("  Invalid ephemeral key: %s", decErr))
				continue
			}

			var ephKey [32]byte
			copy(ephKey[:], ephKeyBytes)

			if len(sigB64) > 0 {
				pubKey := memberKeyMap[blob.SenderFingerprint]
				sigBytes, sigErr := base64.StdEncoding.DecodeString(sigB64)
				if pubKey == nil || sigErr != nil || !crypto.VerifyBlobSignature(pubKey, data, ephKey[:], blob.SenderFingerprint, sigBytes) {
					report.Warnings = append(report.Warnings, fmt.Sprintf("signature verification failed for %s", blob.SenderFingerprint))
					ui.Warning(fmt.Sprintf("  Signature verification failed for %s", blob.SenderFingerprint))
					continue
				}
			}

			plaintext, err := crypto.DecryptFromSender(data, ephKey, kp.X25519Private, kp.X25519Public)
			if err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("relay decrypt failed for %s: %v", blob.BlobID, err))
				ui.Warning(fmt.Sprintf("  Decryption failed: %s", err))
				continue
			}

			applied, applyErr := applyReceivedData(project.ProjectID, cfg, kp, targetFile, plaintext, blob.Filename)
			if applyErr != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("relay apply failed for %s: %v", blob.BlobID, applyErr))
				ui.Warning(fmt.Sprintf("  Apply failed: %s", applyErr))
				continue
			}
			if !applied {
				continue
			}

			report.RelayApplied++
			if delErr := relayClient.DeleteBlob(project.ProjectID, blob.BlobID); delErr != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("relay cleanup failed for %s: %v", blob.BlobID, delErr))
				ui.Warning(fmt.Sprintf("  Failed to clean up blob: %s", delErr))
			}

			logger, _ := audit.NewLogger()
			if logger != nil {
				_ = logger.Log(audit.Entry{
					Event:  audit.EventPull,
					Peer:   blob.SenderFingerprint,
					File:   targetFile,
					Method: "relay",
				})
			}
		}

		ui.Blank()
		return renderPullReport(report, nil)
	}

	if relayErr == nil {
		ui.Line("  No pending blobs on relay")
	} else {
		report.Warnings = append(report.Warnings, fmt.Sprintf("relay unavailable: %v", relayErr))
	}

	ui.Line("  Listening for LAN push...")
	ui.Blank()

	lanCtx := context.Background()
	if pullTimeoutSeconds > 0 {
		var cancel context.CancelFunc
		lanCtx, cancel = context.WithTimeout(context.Background(), time.Duration(pullTimeoutSeconds)*time.Second)
		defer cancel()
	}

	result, err := envsync.Pull(lanCtx, envsync.PullOptions{
		EnvFilePath:        targetFile,
		Port:               config.DefaultPort,
		TeamID:             project.ProjectID,
		KeyPair:            kp,
		NoiseKeypair:       noiseKP,
		ConfirmBeforeApply: cfg.Sync.ConfirmBeforeApply,
		ProjectID:          project.ProjectID,
		BackupEnabled:      cfg.Sync.AutoBackup,
		BackupKey:          mustDeriveAtRestKey(kp),
		MaxVersions:        cfg.Sync.MaxVersions,
		Advertise:          cfg.Network.MDNSEnabled,
		AdvertiseVersion:   Version,
		OnListening: func(port int) {
			ui.Line(fmt.Sprintf("  - Listening on port %d", port))
		},
		OnReceived: func(payload envsync.EnvPayload, diff *envfile.DiffResult) {
			ui.Line(fmt.Sprintf("  - Received %s (%d bytes)", payload.FileName, len(payload.Data)))
			if diff != nil && diff.HasChanges() {
				ui.Blank()
				fmt.Print(ui.RenderDiff(diff))
				ui.Blank()
			}
		},
		OnConfirm: func(diff *envfile.DiffResult) bool {
			if diff == nil {
				return true
			}
			return ui.ConfirmAction(fmt.Sprintf("Apply changes? (%s)", diff.Summary()), true)
		},
		OnApplied: func(fileName string) {
			ui.Success(fmt.Sprintf("Applied to %s", fileName))
		},
	})
	if err != nil {
		if !pullJSON {
			ui.RenderError(ui.StructuredError{
				Category:   ui.ErrSync,
				Message:    "Pull failed",
				Cause:      err.Error(),
				Suggestion: "Ensure the sender is running 'envsync push' or that relay delivery is enabled",
			})
		}
		report.Warnings = append(report.Warnings, err.Error())
		return renderPullReport(report, err)
	}

	if result.Applied {
		report.LANApplied = true
		logger, _ := audit.NewLogger()
		if logger != nil {
			_ = logger.Log(audit.Entry{
				Event:       audit.EventPull,
				File:        result.FileName,
				VarsChanged: result.VarCount,
				Method:      "lan",
			})
		}
	}

	ui.Blank()
	return renderPullReport(report, nil)
}

func shortFP(fp string) string {
	if len(fp) > 16 {
		return fp[:16] + "..."
	}
	return fp
}

// applyReceivedData shows diff, confirms, backs up the current file, and writes the replacement.
func applyReceivedData(projectID string, cfg *config.Config, kp *crypto.KeyPair, targetFile string, data []byte, fileName string) (bool, error) {
	receivedEnv, err := envfile.Parse(string(data))
	if err != nil {
		return false, fmt.Errorf("parsing received data: %w", err)
	}

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

		if cfg.Sync.AutoBackup {
			if _, err := backupCurrentVersion(projectID, currentData, cfg, kp); err != nil {
				return false, fmt.Errorf("creating pre-apply backup: %w", err)
			}
		}
	}

	if err := writeEnvFile(targetFile, data); err != nil {
		return false, err
	}

	ui.Success(fmt.Sprintf("Applied %s (%d variables)", fileName, receivedEnv.VariableCount()))
	return true, nil
}

func mustDeriveAtRestKey(kp *crypto.KeyPair) [32]byte {
	key, err := crypto.DeriveAtRestKey(kp.X25519Private[:])
	if err != nil {
		return [32]byte{}
	}
	return key
}

func renderPullReport(report pullReport, runErr error) error {
	report.Methods = activePullMethods(report)
	report.Method = "none"
	if len(report.Methods) == 1 {
		report.Method = report.Methods[0]
	} else if len(report.Methods) > 1 {
		report.Method = strings.Join(report.Methods, "+")
	}

	if pullJSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
	}
	return runErr
}

func activePullMethods(report pullReport) []string {
	methods := []string{}
	if report.RelayApplied > 0 {
		methods = append(methods, "relay")
	}
	if report.LANApplied {
		methods = append(methods, "lan")
	}
	return methods
}

func init() {
	pullCmd.Flags().IntVar(&pullTimeoutSeconds, "timeout", 0, "Optional timeout in seconds for LAN listen mode")
	pullCmd.Flags().StringVar(&pullServiceKeyPath, "service-key", "", "Path to an EnvSync service key for relay-only automation")
	pullCmd.Flags().StringVar(&pullTeamID, "team", "", "Override the project ID/team namespace")
	pullCmd.Flags().StringVar(&pullRelayURL, "relay", "", "Override the relay URL")
	pullCmd.Flags().BoolVar(&pullJSON, "json", false, "Print pull results as JSON")
	rootCmd.AddCommand(pullCmd)
}
