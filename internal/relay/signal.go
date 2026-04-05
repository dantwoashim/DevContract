// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/envsync/envsync/internal/crypto"
)

// SignalClient manages WebSocket connections to the signaling Durable Object.
type SignalClient struct {
	baseURL     string
	teamID      string
	fingerprint string
	privateKey  []byte
}

// PeerEndpoint is the network endpoint information exchanged during signaling.
type PeerEndpoint struct {
	Fingerprint string `json:"fingerprint"`
	PublicIP    string `json:"public_ip"`
	Port        int    `json:"port"`
	NATType     string `json:"nat_type"` // "full-cone", "restricted", "symmetric", "unknown"
}

// NewSignalClient creates a new signaling client.
func NewSignalClient(baseURL, teamID string, kp *crypto.KeyPair) *SignalClient {
	return &SignalClient{
		baseURL:     baseURL,
		teamID:      teamID,
		fingerprint: kp.Fingerprint,
		privateKey:  kp.Ed25519Private[:],
	}
}

// ExchangeEndpoints connects to the signaling room, broadcasts our endpoint,
// and waits for a peer to respond with theirs.
func (sc *SignalClient) ExchangeEndpoints(localEndpoint PeerEndpoint, timeout time.Duration) (*PeerEndpoint, error) {
	// In a real implementation, this would:
	// 1. Open WebSocket connection to the signaling room
	// 2. Send our PeerEndpoint as JSON
	// 3. Wait up to `timeout` for a peer_joined or signal message
	// 4. Parse the peer's PeerEndpoint
	// 5. Return it for hole-punching

	// For now, use HTTP polling as a fallback signaling mechanism
	return sc.pollForPeer(localEndpoint, timeout)
}

// pollForPeer uses HTTP to register and poll for a peer's endpoint.
func (sc *SignalClient) pollForPeer(localEndpoint PeerEndpoint, timeout time.Duration) (*PeerEndpoint, error) {
	// Register our endpoint
	data, err := json.Marshal(localEndpoint)
	if err != nil {
		return nil, fmt.Errorf("marshaling endpoint: %w", err)
	}
	url := fmt.Sprintf("%s/signal/%s/register", sc.baseURL, sc.teamID)

	req, err := http.NewRequest("PUT", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-EnvSync-Fingerprint", sc.fingerprint)

	client := &http.Client{Timeout: 5 * time.Second}
	// #nosec G704 -- the request target is the configured EnvSync signaling service.
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registering endpoint: %w", err)
	}
	if err := resp.Body.Close(); err != nil {
		return nil, fmt.Errorf("closing register response: %w", err)
	}

	// Poll for peer
	deadline := time.Now().Add(timeout)
	pollURL := fmt.Sprintf("%s/signal/%s/peers", sc.baseURL, sc.teamID)

	for time.Now().Before(deadline) {
		req, err := http.NewRequest("GET", pollURL, nil)
		if err != nil {
			return nil, fmt.Errorf("building peer poll request: %w", err)
		}
		req.Header.Set("X-EnvSync-Fingerprint", sc.fingerprint)

		// #nosec G704 -- the request target is the configured EnvSync signaling service.
		resp, err := client.Do(req)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if resp.StatusCode == 200 {
			var peers []PeerEndpoint
			if err := json.NewDecoder(resp.Body).Decode(&peers); err == nil {
				for _, p := range peers {
					if p.Fingerprint != sc.fingerprint {
						if err := resp.Body.Close(); err != nil {
							return nil, fmt.Errorf("closing peer poll response: %w", err)
						}
						return &p, nil
					}
				}
			}
		}
		if err := resp.Body.Close(); err != nil {
			return nil, fmt.Errorf("closing peer poll response: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil, fmt.Errorf("no peer found within %s", timeout)
}
