// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package sync

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dantwoashim/Env_sync/internal/crypto"
	"github.com/dantwoashim/Env_sync/internal/discovery"
	"github.com/dantwoashim/Env_sync/internal/peer"
	"github.com/dantwoashim/Env_sync/internal/relay"
	"github.com/dantwoashim/Env_sync/internal/revision"
	"github.com/dantwoashim/Env_sync/internal/transport"
	"github.com/flynn/noise"
)

// OrchestratorOptions configures the sync orchestrator.
type OrchestratorOptions struct {
	EnvFilePath         string
	TeamID              string
	KeyPair             *crypto.KeyPair
	NoiseKeypair        noise.DHKey
	RelayClient         *relay.Client
	RelayURL            string
	Sequence            int64
	BaseRevisionID      string
	RevisionID          string
	AncestorRevisionIDs []string
	OnStatus            func(status string)
}

// OrchestratorResult summarizes the sync.
type OrchestratorResult struct {
	Method         string
	PeerCount      int
	DeliveredCount int
	QueuedCount    int
	Duration       time.Duration
	Error          error
}

// Orchestrate runs the honest fallback chain: LAN direct delivery first, then
// encrypted relay queueing for remaining trusted peers.
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

	registryPeers := trustedRelayPeers(opts.TeamID, opts.KeyPair)
	result.PeerCount = len(registryPeers)
	peerAckBase := loadPeerAckBase(opts.TeamID)

	report("Scanning LAN for peers...")
	lanCtx, lanCancel := context.WithTimeout(ctx, 2*time.Second)
	defer lanCancel()

	discoveredPeers, discoverErr := discovery.Discover(lanCtx, discovery.DefaultMDNSTimeout, opts.KeyPair.Fingerprint)
	deliveredFingerprints := map[string]struct{}{}
	transportIndex := make(map[string]peer.Peer, len(registryPeers))
	for _, p := range registryPeers {
		if fp := p.EffectiveTransportFingerprint(); fp != "" {
			transportIndex[fp] = p
		}
	}

	if discoverErr == nil && len(discoveredPeers) > 0 {
		filtered := make([]discovery.Peer, 0, len(discoveredPeers))
		for _, discoveredPeer := range discoveredPeers {
			if opts.TeamID != "" && discoveredPeer.TeamID != opts.TeamID {
				continue
			}
			filtered = append(filtered, discoveredPeer)
		}
		if result.PeerCount == 0 {
			result.PeerCount = len(filtered)
		}

		if len(filtered) > 0 {
			report(fmt.Sprintf("Found %d peer(s) on LAN", len(filtered)))
		}

		for _, discoveredPeer := range filtered {
			matchedPeer, trusted := transportIndex[discoveredPeer.Fingerprint]
			if !trusted {
				report(fmt.Sprintf("Skipping untrusted LAN peer %s", discoveredPeer.Fingerprint))
				continue
			}

			conn, err := transport.Dial(transport.DialOptions{
				Address:             discoveredPeer.Addr.String(),
				Timeout:             transport.DefaultDialTimeout,
				LocalKeypair:        opts.NoiseKeypair,
				ExpectedFingerprint: discoveredPeer.Fingerprint,
			})
			if err != nil {
				report(fmt.Sprintf("LAN connect failed for %s: %s", discoveredPeer.Fingerprint, err))
				continue
			}

			payload := NewEnvPayloadWithAncestors(fileName, data, opts.Sequence, peerBaseRevisionID(opts, peerAckBase, matchedPeer.Fingerprint), opts.RevisionID, opts.AncestorRevisionIDs)
			encodedPayload, encodeErr := EncodeEnvPayload(payload)
			if encodeErr != nil {
				_ = conn.Close()
				report(fmt.Sprintf("LAN payload encode failed: %s", encodeErr))
				continue
			}

			err = SendMessage(conn, Message{Type: MsgEnvPush, Payload: encodedPayload})
			if err == nil {
				err = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			}
			if err == nil {
				var resp Message
				resp, err = ReceiveMessage(conn)
				if err == nil && resp.Type == MsgNack {
					err = fmt.Errorf("peer rejected push: %s", string(resp.Payload))
				}
			}
			if closeErr := conn.Close(); closeErr != nil && err == nil {
				err = closeErr
			}

			if err != nil {
				report(fmt.Sprintf("LAN delivery failed for %s: %s", discoveredPeer.Fingerprint, err))
				continue
			}

			result.DeliveredCount++
			deliveredFingerprints[matchedPeer.Fingerprint] = struct{}{}
			recordPeerAck(opts.TeamID, matchedPeer.Fingerprint, opts.RevisionID)
			report(fmt.Sprintf("+ Delivered to %s via LAN", peerLabel(matchedPeer)))
		}
	}

	if opts.RelayClient != nil && opts.TeamID != "" {
		report("Hole-punch is disabled; using encrypted relay fallback for offline peers.")

		for _, trustedPeer := range registryPeers {
			if trustedPeer.Fingerprint == opts.KeyPair.Fingerprint {
				continue
			}
			if _, delivered := deliveredFingerprints[trustedPeer.Fingerprint]; delivered {
				continue
			}

			recipientPubBytes, err := base64.StdEncoding.DecodeString(trustedPeer.X25519Public)
			if err != nil || len(recipientPubBytes) != 32 {
				report(fmt.Sprintf("Skipping %s - invalid public key", peerLabel(trustedPeer)))
				continue
			}

			var recipientPub [32]byte
			copy(recipientPub[:], recipientPubBytes)

			payload := NewEnvPayloadWithAncestors(fileName, data, opts.Sequence, peerBaseRevisionID(opts, peerAckBase, trustedPeer.Fingerprint), opts.RevisionID, opts.AncestorRevisionIDs)
			encodedPayload, encodeErr := EncodeEnvPayload(payload)
			if encodeErr != nil {
				report(fmt.Sprintf("Relay payload encode failed for %s: %s", peerLabel(trustedPeer), encodeErr))
				continue
			}

			ephPub, encrypted, err := crypto.EncryptForRecipient(encodedPayload, recipientPub)
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

			result.QueuedCount++
			report(fmt.Sprintf("+ Queued encrypted relay delivery for %s", peerLabel(trustedPeer)))
		}
	}

	switch {
	case result.DeliveredCount > 0 && result.QueuedCount > 0:
		result.Method = "lan+relay"
	case result.DeliveredCount > 0:
		result.Method = "lan"
	case result.QueuedCount > 0:
		result.Method = "relay"
	default:
		result.Method = "none"
	}

	result.Duration = time.Since(start)
	if result.DeliveredCount == 0 && result.QueuedCount == 0 {
		if len(registryPeers) == 0 && opts.TeamID != "" {
			result.Error = fmt.Errorf("no trusted peers in project - invite someone first")
		} else if discoverErr != nil {
			result.Error = fmt.Errorf("peer discovery failed: %w", discoverErr)
		} else {
			result.Error = fmt.Errorf("no peers accepted the push and nothing was queued for relay")
		}
	}

	return result
}

func loadPeerAckBase(teamID string) map[string]string {
	if teamID == "" {
		return nil
	}

	revStore, err := revision.New()
	if err != nil {
		return nil
	}
	acks, err := revStore.LoadPeerAcks(teamID)
	if err != nil {
		return nil
	}
	base := make(map[string]string, len(acks))
	for fingerprint, ack := range acks {
		base[fingerprint] = ack.RevisionID
	}
	return base
}

func peerBaseRevisionID(opts OrchestratorOptions, peerAckBase map[string]string, fingerprint string) string {
	if ackRevisionID, ok := peerAckBase[fingerprint]; ok && ackRevisionID != "" {
		return ackRevisionID
	}
	return opts.BaseRevisionID
}

func recordPeerAck(teamID, fingerprint, revisionID string) {
	if teamID == "" || fingerprint == "" || revisionID == "" {
		return
	}
	revStore, err := revision.New()
	if err != nil {
		return
	}
	_ = revStore.MarkPeerAck(teamID, fingerprint, revisionID)
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

func trustedRelayPeers(teamID string, kp *crypto.KeyPair) []peer.Peer {
	if teamID == "" {
		return nil
	}

	registry, err := peer.NewRegistry()
	if err != nil {
		return nil
	}

	allPeers, err := registry.ListPeers(teamID)
	if err != nil {
		return nil
	}

	trusted := make([]peer.Peer, 0, len(allPeers))
	for _, p := range allPeers {
		if !p.CanSync() {
			continue
		}
		if kp != nil && p.Fingerprint == kp.Fingerprint {
			continue
		}
		trusted = append(trusted, p)
	}
	return trusted
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
