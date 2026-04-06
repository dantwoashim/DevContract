// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"

	"github.com/envsync/envsync/internal/audit"
	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/relay"
	"github.com/envsync/envsync/internal/ui"
)

func pullPendingRelay(projectID, relayURL, targetFile string, cfg *config.Config, kp *crypto.KeyPair) (int, error) {
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
		return 0, err
	}

	appliedCount := 0
	for _, blob := range pending {
		ui.Line(fmt.Sprintf("  - Downloading %s from %s...", blob.Filename, shortFP(blob.SenderFingerprint)))

		data, ephKeyB64, _, sigB64, err := relayClient.DownloadBlob(projectID, blob.BlobID)
		if err != nil {
			ui.Warning(fmt.Sprintf("  Download failed: %s", err))
			continue
		}

		ephKeyBytes, decErr := base64.StdEncoding.DecodeString(ephKeyB64)
		if decErr != nil {
			ui.Warning(fmt.Sprintf("  Invalid ephemeral key: %s", decErr))
			continue
		}

		var ephKey [32]byte
		copy(ephKey[:], ephKeyBytes)

		if err := verifyRelayBlobSignature(memberKeyMap, blob.SenderFingerprint, data, ephKey, sigB64); err != nil {
			ui.Warning(fmt.Sprintf("  %s", err))
			continue
		}

		plaintext, err := crypto.DecryptFromSender(data, ephKey, kp.X25519Private, kp.X25519Public)
		if err != nil {
			ui.Warning(fmt.Sprintf("  Decryption failed: %s", err))
			continue
		}

		applied, applyErr := applyReceivedData(projectID, cfg, kp, targetFile, plaintext, blob.Filename)
		if applyErr != nil {
			ui.Warning(fmt.Sprintf("  Apply failed: %s", applyErr))
			continue
		}
		if !applied {
			continue
		}

		appliedCount++
		if delErr := relayClient.DeleteBlob(projectID, blob.BlobID); delErr != nil {
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

	return appliedCount, nil
}
