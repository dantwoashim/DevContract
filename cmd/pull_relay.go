// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/dantwoashim/Env_sync/internal/apply"
	"github.com/dantwoashim/Env_sync/internal/audit"
	"github.com/dantwoashim/Env_sync/internal/config"
	"github.com/dantwoashim/Env_sync/internal/crypto"
	"github.com/dantwoashim/Env_sync/internal/envfile"
	"github.com/dantwoashim/Env_sync/internal/relay"
	envsync "github.com/dantwoashim/Env_sync/internal/sync"
	"github.com/dantwoashim/Env_sync/internal/ui"
)

type pullApplyOptions struct {
	Policy      apply.Policy
	Interactive bool
	BackupKey   [32]byte
}

func pullPendingRelay(projectID, relayURL, targetFile string, cfg *config.Config, kp *crypto.KeyPair, opts pullApplyOptions) (*relayPullSummary, error) {
	relayClient := relay.NewClient(relayURL, kp)
	memberKeys, _ := relayClient.ListTeamMembers(projectID)
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

	pending, err := relayClient.ListPending(projectID)
	if err != nil {
		return nil, err
	}

	summary := &relayPullSummary{}
	summary.FoundCount = len(pending)
	for _, blob := range pending {
		ui.Line(fmt.Sprintf("  - Downloading %s from %s...", blob.Filename, shortFP(blob.SenderFingerprint)))

		data, ephKeyB64, _, sigB64, err := relayClient.DownloadBlob(projectID, blob.BlobID)
		if err != nil {
			summary.Warnings = append(summary.Warnings, fmt.Sprintf("relay download failed for %s: %v", blob.BlobID, err))
			ui.Warning(fmt.Sprintf("  Download failed: %s", err))
			continue
		}

		ephKeyBytes, decErr := base64.StdEncoding.DecodeString(ephKeyB64)
		if decErr != nil {
			summary.Warnings = append(summary.Warnings, fmt.Sprintf("invalid relay ephemeral key for %s: %v", blob.BlobID, decErr))
			ui.Warning(fmt.Sprintf("  Invalid ephemeral key: %s", decErr))
			rejectRelayBlob(relayClient, projectID, blob.BlobID, fmt.Sprintf("invalid ephemeral key: %v", decErr), summary)
			continue
		}

		var ephKey [32]byte
		copy(ephKey[:], ephKeyBytes)

		if err := verifyRelayBlobSignature(memberKeyMap, blob.SenderFingerprint, data, ephKey, sigB64); err != nil {
			summary.Warnings = append(summary.Warnings, err.Error())
			ui.Warning(fmt.Sprintf("  %s", err))
			rejectRelayBlob(relayClient, projectID, blob.BlobID, err.Error(), summary)
			continue
		}

		plaintext, err := crypto.DecryptFromSender(data, ephKey, kp.X25519Private, kp.X25519Public)
		if err != nil {
			summary.Warnings = append(summary.Warnings, fmt.Sprintf("relay decrypt failed for %s: %v", blob.BlobID, err))
			ui.Warning(fmt.Sprintf("  Decryption failed: %s", err))
			rejectRelayBlob(relayClient, projectID, blob.BlobID, fmt.Sprintf("decrypt failed: %v", err), summary)
			continue
		}

		payload, err := envsync.DecodeEnvPayload(plaintext)
		if err != nil {
			summary.Warnings = append(summary.Warnings, fmt.Sprintf("relay payload decode failed for %s: %v", blob.BlobID, err))
			ui.Warning(fmt.Sprintf("  Invalid relay payload: %s", err))
			rejectRelayBlob(relayClient, projectID, blob.BlobID, fmt.Sprintf("payload decode failed: %v", err), summary)
			continue
		}
		if payload.Checksum != envsync.NewEnvPayload(payload.FileName, payload.Data, payload.Sequence, payload.BaseRevisionID, payload.RevisionID).Checksum {
			summary.Warnings = append(summary.Warnings, fmt.Sprintf("relay checksum mismatch for %s", blob.BlobID))
			ui.Warning("  Relay payload checksum mismatch")
			rejectRelayBlob(relayClient, projectID, blob.BlobID, "payload checksum mismatch", summary)
			continue
		}

		applyResult, applyErr := apply.Apply(apply.Options{
			ProjectID:           projectID,
			TargetFile:          targetFile,
			IncomingFile:        payload.FileName,
			IncomingData:        payload.Data,
			BaseRevisionID:      payload.BaseRevisionID,
			AncestorRevisionIDs: payload.AncestorRevisionIDs,
			NewRevisionID:       payload.RevisionID,
			Policy:              opts.Policy,
			Interactive:         opts.Interactive,
			BackupEnabled:       cfg.Sync.AutoBackup,
			BackupKey:           opts.BackupKey,
			MaxVersions:         cfg.Sync.MaxVersions,
			ConfirmApply: func(diff *envfile.DiffResult) bool {
				if diff == nil {
					return true
				}
				fmt.Print(ui.RenderDiff(diff))
				ui.Blank()
				return ui.ConfirmAction(fmt.Sprintf("Apply relay changes? (%s)", diff.Summary()), true)
			},
			ResolveConflicts: resolvePullConflicts,
		})
		if applyErr != nil {
			summary.Warnings = append(summary.Warnings, fmt.Sprintf("relay apply failed for %s: %v", blob.BlobID, applyErr))
			summary.InteractiveRequired = summary.InteractiveRequired || errors.Is(applyErr, apply.ErrInteractiveRequired)
			summary.ManualInterventionNeeded = true
			ui.Warning(fmt.Sprintf("  Apply failed: %s", applyErr))
			continue
		}

		summary.BackupCreated = summary.BackupCreated || applyResult.BackupCreated
		if applyResult.ConflictPolicyApplied != "" {
			summary.ConflictPolicyApplied = applyResult.ConflictPolicyApplied
		}
		if delErr := relayClient.DeleteBlob(projectID, blob.BlobID); delErr != nil {
			summary.Warnings = append(summary.Warnings, fmt.Sprintf("relay cleanup failed for %s: %v", blob.BlobID, delErr))
			ui.Warning(fmt.Sprintf("  Failed to clean up blob: %s", delErr))
			continue
		}

		summary.HandledCount++
		if !applyResult.Applied {
			continue
		}

		summary.AppliedCount++
		logger, _ := audit.NewLogger()
		if logger != nil {
			_ = logger.Log(audit.Entry{
				Event:       audit.EventPull,
				Peer:        blob.SenderFingerprint,
				File:        targetFile,
				Method:      "relay",
				VarsChanged: applyResult.VariableCount,
				Details:     applyResult.Summary,
			})
		}
	}

	return summary, nil
}

func rejectRelayBlob(client *relay.Client, projectID, blobID, reason string, summary *relayPullSummary) {
	if err := client.RejectBlob(projectID, blobID, reason); err != nil {
		summary.Warnings = append(summary.Warnings, fmt.Sprintf("relay reject failed for %s: %v", blobID, err))
		ui.Warning(fmt.Sprintf("  Failed to reject blob: %s", err))
	}
}
