// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package sync

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/discovery"
	"github.com/envsync/envsync/internal/peer"
	"github.com/envsync/envsync/internal/relay"
	"github.com/envsync/envsync/internal/transport"
	"github.com/flynn/noise"
)

// OrchestratorOptions configures the sync orchestrator.
type OrchestratorOptions struct {
	EnvFilePath  string
	TeamID       string
	KeyPair      *crypto.KeyPair
	NoiseKeypair noise.DHKey
	RelayClient  *relay.Client
	RelayURL     string // Signal/relay base URL (from config)
	Sequence     int64
	OnStatus     func(status string)
}

// OrchestratorResult summarizes the sync.
type OrchestratorResult struct {
	Method      string // "lan", "holepunch", "relay"
	PeerCount   int
	SyncedCount int
	Duration    time.Duration
	Error       error
}

// Orchestrate runs the full fallback chain: LAN → hole-punch → relay.
func Orchestrate(ctx context.Context, opts OrchestratorOptions) *OrchestratorResult {
	start := time.Now()
	result := &OrchestratorResult{}

	report := func(s string) {
		if opts.OnStatus != nil {
			opts.OnStatus(s)
		}
	}

	// Read the file once
	data, err := readEnvFile(opts.EnvFilePath)
	if err != nil {
		result.Error = err
		result.Duration = time.Since(start)
		return result
	}
	fileName := filepath.Base(opts.EnvFilePath)
	if opts.EnvFilePath == "" {
		fileName = ".env"
	}

	// Phase 1: Try LAN discovery (2s timeout)
	report("Scanning LAN for peers...")
	lanCtx, lanCancel := context.WithTimeout(ctx, 2*time.Second)
	defer lanCancel()

	peers, err := discovery.Discover(lanCtx, discovery.DefaultMDNSTimeout, opts.KeyPair.Fingerprint)
	if err == nil && len(peers) > 0 {
		report(fmt.Sprintf("Found %d peer(s) on LAN", len(peers)))

		for _, p := range peers {
			if opts.TeamID != "" && p.TeamID != opts.TeamID {
				continue
			}

			result.PeerCount++

			conn, err := transport.Dial(transport.DialOptions{
				Address:             p.Addr.String(),
				Timeout:             transport.DefaultDialTimeout,
				LocalKeypair:        opts.NoiseKeypair,
				ExpectedFingerprint: p.Fingerprint,
			})
			if err != nil {
				report(fmt.Sprintf("LAN connect failed: %s", err))
				continue
			}

			payload := NewEnvPayload(fileName, data, opts.Sequence)
			err = SendMessage(conn, Message{Type: MsgEnvPush, Payload: EncodeEnvPayload(payload)})
			conn.Close()

			if err == nil {
				result.SyncedCount++
				report("✓ Synced via LAN direct")
			}
		}

		if result.SyncedCount > 0 {
			result.Method = "lan"
			result.Duration = time.Since(start)
			return result
		}
	}

	// Phase 2: Hole-punch attempt
	report("Attempting hole-punch...")
	if opts.RelayClient != nil && opts.TeamID != "" {
		relayURL := opts.RelayURL
		if relayURL == "" {
			relayURL = "https://relay.envsync.dev"
		}
		signal := relay.NewSignalClient(
			relayURL,
			opts.TeamID,
			opts.KeyPair,
		)

		hpCtx, hpCancel := context.WithTimeout(ctx, 5*time.Second)
		defer hpCancel()

		secureConn, err := transport.HolePunch(hpCtx, transport.HolePunchOptions{
			Signal:       signal,
			LocalKeypair: opts.NoiseKeypair,
			KeyPair:      opts.KeyPair,
			Timeout:      5 * time.Second,
		})
		if err == nil {
			payload := NewEnvPayload(fileName, data, opts.Sequence)
			err = SendMessage(secureConn, Message{Type: MsgEnvPush, Payload: EncodeEnvPayload(payload)})
			secureConn.Close()

			if err == nil {
				result.Method = "holepunch"
				result.SyncedCount = 1
				result.PeerCount = 1
				result.Duration = time.Since(start)
				report("✓ Synced via hole-punch")
				return result
			}
		}
		report(fmt.Sprintf("Hole-punch failed: %s", err))
	}

	// Phase 3: Relay fallback — encrypt for each trusted peer
	if opts.RelayClient != nil && opts.TeamID != "" {
		report("Peers offline — uploading to encrypted relay...")
		result.Method = "relay"

		// Load trusted peers from registry
		registry, err := peer.NewRegistry()
		if err != nil {
			result.Error = fmt.Errorf("loading peer registry: %w", err)
			result.Duration = time.Since(start)
			return result
		}

		allPeers, err := registry.ListPeers(opts.TeamID)
		if err != nil {
			result.Error = fmt.Errorf("loading peer registry: %w", err)
			result.Duration = time.Since(start)
			return result
		}

		if len(allPeers) == 0 {
			result.Error = fmt.Errorf("no trusted peers in team — invite someone first")
			result.Duration = time.Since(start)
			return result
		}

		// Filter to trusted only
		var trustedPeers []peer.Peer
		for _, p := range allPeers {
			if p.CanSync() {
				trustedPeers = append(trustedPeers, p)
			}
		}

		for _, tp := range trustedPeers {
			if tp.Fingerprint == opts.KeyPair.Fingerprint {
				continue // skip self
			}

			// Encrypt blob for this peer using their X25519 public key
			recipientPubBytes, err := base64.StdEncoding.DecodeString(tp.X25519Public)
			if err != nil || len(recipientPubBytes) != 32 {
				report(fmt.Sprintf("Skipping %s — invalid public key", tp.GitHubUsername))
				continue
			}

			var recipientPub [32]byte
			copy(recipientPub[:], recipientPubBytes)

			ephPub, encrypted, err := crypto.EncryptForRecipient(data, recipientPub)
			if err != nil {
				report(fmt.Sprintf("Encrypt failed for %s: %s", tp.GitHubUsername, err))
				continue
			}

			blobID := fmt.Sprintf("%s-%d-%d", fileName, opts.Sequence, time.Now().UnixMilli())
			ephPubB64 := base64.StdEncoding.EncodeToString(ephPub[:])

			// Sign the encrypted blob for sender authentication
			sig := crypto.SignBlob(opts.KeyPair.Ed25519Private, encrypted, ephPub[:], opts.KeyPair.Fingerprint)
			sigB64 := base64.StdEncoding.EncodeToString(sig)

			err = opts.RelayClient.UploadBlob(
				opts.TeamID, blobID, encrypted,
				opts.KeyPair.Fingerprint, tp.Fingerprint,
				ephPubB64, fileName,
				sigB64,
			)
			if err != nil {
				report(fmt.Sprintf("Relay upload failed for %s: %s", tp.GitHubUsername, err))
				continue
			}

			result.SyncedCount++
			report(fmt.Sprintf("✓ Uploaded for %s via relay", tp.GitHubUsername))
		}

		result.PeerCount = len(trustedPeers) - 1
		result.Duration = time.Since(start)
		if result.SyncedCount == 0 {
			result.Error = fmt.Errorf("relay upload failed for all peers")
		}
		return result
	}

	result.Error = fmt.Errorf("no peers found and no relay configured")
	result.Duration = time.Since(start)
	return result
}

// readEnvFile reads the target .env file.
func readEnvFile(path string) ([]byte, error) {
	if path == "" {
		path = ".env"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return data, nil
}
