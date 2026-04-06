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

// Orchestrate runs the full fallback chain: LAN -> hole-punch -> relay.
func Orchestrate(ctx context.Context, opts OrchestratorOptions) *OrchestratorResult {
	start := time.Now()
	result := &OrchestratorResult{}

	report := func(s string) {
		if opts.OnStatus != nil {
			opts.OnStatus(s)
		}
	}

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

	report("Scanning LAN for peers...")
	lanCtx, lanCancel := context.WithTimeout(ctx, 2*time.Second)
	defer lanCancel()

	discoveredPeers, err := discovery.Discover(lanCtx, discovery.DefaultMDNSTimeout, opts.KeyPair.Fingerprint)
	if err == nil && len(discoveredPeers) > 0 {
		report(fmt.Sprintf("Found %d peer(s) on LAN", len(discoveredPeers)))

		for _, discoveredPeer := range discoveredPeers {
			if opts.TeamID != "" && discoveredPeer.TeamID != opts.TeamID {
				continue
			}

			result.PeerCount++

			conn, err := transport.Dial(transport.DialOptions{
				Address:             discoveredPeer.Addr.String(),
				Timeout:             transport.DefaultDialTimeout,
				LocalKeypair:        opts.NoiseKeypair,
				ExpectedFingerprint: discoveredPeer.Fingerprint,
			})
			if err != nil {
				report(fmt.Sprintf("LAN connect failed: %s", err))
				continue
			}

			payload := NewEnvPayload(fileName, data, opts.Sequence)
			encodedPayload, encodeErr := EncodeEnvPayload(payload)
			if encodeErr != nil {
				_ = conn.Close()
				report(fmt.Sprintf("LAN payload encode failed: %s", encodeErr))
				continue
			}
			err = SendMessage(conn, Message{Type: MsgEnvPush, Payload: encodedPayload})
			if closeErr := conn.Close(); closeErr != nil && err == nil {
				err = closeErr
			}

			if err == nil {
				result.SyncedCount++
				report("+ Synced via LAN direct")
			}
		}

		if result.SyncedCount > 0 {
			result.Method = "lan"
			result.Duration = time.Since(start)
			return result
		}
	}

	report("Attempting hole-punch...")
	if opts.RelayClient != nil && opts.TeamID != "" {
		relayURL := opts.RelayURL
		if relayURL == "" {
			relayURL = "https://relay.envsync.dev"
		}
		signal := relay.NewSignalClient(relayURL, opts.TeamID, opts.KeyPair)

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
			encodedPayload, encodeErr := EncodeEnvPayload(payload)
			if encodeErr != nil {
				_ = secureConn.Close()
				report(fmt.Sprintf("Hole-punch payload encode failed: %s", encodeErr))
			} else {
				err = SendMessage(secureConn, Message{Type: MsgEnvPush, Payload: encodedPayload})
			}
			if closeErr := secureConn.Close(); closeErr != nil && err == nil {
				err = closeErr
			}

			if err == nil {
				result.Method = "holepunch"
				result.SyncedCount = 1
				result.PeerCount = 1
				result.Duration = time.Since(start)
				report("+ Synced via hole-punch")
				return result
			}
		}
		report(fmt.Sprintf("Hole-punch failed: %s", err))
	}

	if opts.RelayClient != nil && opts.TeamID != "" {
		report("Peers offline - uploading to encrypted relay...")
		result.Method = "relay"

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
			result.Error = fmt.Errorf("no trusted peers in project - invite someone first")
			result.Duration = time.Since(start)
			return result
		}

		var trustedPeers []peer.Peer
		for _, registryPeer := range allPeers {
			if registryPeer.CanSync() {
				trustedPeers = append(trustedPeers, registryPeer)
			}
		}

		for _, trustedPeer := range trustedPeers {
			if trustedPeer.Fingerprint == opts.KeyPair.Fingerprint {
				continue
			}

			recipientPubBytes, err := base64.StdEncoding.DecodeString(trustedPeer.X25519Public)
			if err != nil || len(recipientPubBytes) != 32 {
				report(fmt.Sprintf("Skipping %s - invalid public key", peerLabel(trustedPeer)))
				continue
			}

			var recipientPub [32]byte
			copy(recipientPub[:], recipientPubBytes)

			ephPub, encrypted, err := crypto.EncryptForRecipient(data, recipientPub)
			if err != nil {
				report(fmt.Sprintf("Encrypt failed for %s: %s", peerLabel(trustedPeer), err))
				continue
			}

			blobID := fmt.Sprintf("%s-%d-%d", fileName, opts.Sequence, time.Now().UnixMilli())
			ephPubB64 := base64.StdEncoding.EncodeToString(ephPub[:])

			sig := crypto.SignBlob(opts.KeyPair.Ed25519Private, encrypted, ephPub[:], opts.KeyPair.Fingerprint)
			sigB64 := base64.StdEncoding.EncodeToString(sig)

			err = opts.RelayClient.UploadBlob(
				opts.TeamID,
				blobID,
				encrypted,
				opts.KeyPair.Fingerprint,
				trustedPeer.Fingerprint,
				ephPubB64,
				fileName,
				sigB64,
			)
			if err != nil {
				report(fmt.Sprintf("Relay upload failed for %s: %s", peerLabel(trustedPeer), err))
				continue
			}

			result.SyncedCount++
			report(fmt.Sprintf("+ Uploaded for %s via relay", peerLabel(trustedPeer)))
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

func peerLabel(p peer.Peer) string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	if p.Fingerprint != "" {
		return p.Fingerprint
	}
	return "unknown member"
}
