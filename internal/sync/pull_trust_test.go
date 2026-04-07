// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package sync

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/dantwoashim/Env_sync/internal/peer"
)

type stubPullRegistry struct {
	teams []string
	peers map[string][]peer.Peer
	errs  map[string]error
}

func (s stubPullRegistry) ListTeams() ([]string, error) {
	if err := s.errs["teams"]; err != nil {
		return nil, err
	}
	return append([]string(nil), s.teams...), nil
}

func (s stubPullRegistry) ListPeers(teamID string) ([]peer.Peer, error) {
	if err := s.errs["peers:"+teamID]; err != nil {
		return nil, err
	}
	return append([]peer.Peer(nil), s.peers[teamID]...), nil
}

func TestVerifyTrustedPullPeer(t *testing.T) {
	trustedKey := bytes32(7)
	otherKey := bytes32(11)

	tests := []struct {
		name        string
		registry    pullPeerRegistry
		registryErr error
		teamID      string
		publicKey   []byte
		wantErr     string
	}{
		{
			name:        "registry missing",
			registryErr: errors.New("registry unavailable"),
			publicKey:   trustedKey[:],
			wantErr:     "loading local trust registry",
		},
		{
			name:      "empty team list",
			registry:  stubPullRegistry{},
			publicKey: trustedKey[:],
			wantErr:   "no trusted projects configured",
		},
		{
			name: "unknown peer",
			registry: stubPullRegistry{
				teams: []string{"project-a"},
				peers: map[string][]peer.Peer{
					"project-a": {trustedPeer("alice", trustedKey, peer.TrustTrusted)},
				},
			},
			publicKey: otherKey[:],
			wantErr:   "unknown peer transport key",
		},
		{
			name: "revoked peer",
			registry: stubPullRegistry{
				teams: []string{"project-a"},
				peers: map[string][]peer.Peer{
					"project-a": {trustedPeer("alice", trustedKey, peer.TrustRevoked)},
				},
			},
			publicKey: trustedKey[:],
			wantErr:   "is not trusted",
		},
		{
			name: "trusted peer",
			registry: stubPullRegistry{
				teams: []string{"project-a"},
				peers: map[string][]peer.Peer{
					"project-a": {trustedPeer("alice", trustedKey, peer.TrustTrusted)},
				},
			},
			publicKey: trustedKey[:],
		},
	}

	original := newPullPeerRegistry
	t.Cleanup(func() {
		newPullPeerRegistry = original
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newPullPeerRegistry = func() (pullPeerRegistry, error) {
				if tt.registryErr != nil {
					return nil, tt.registryErr
				}
				return tt.registry, nil
			}

			err := verifyTrustedPullPeer(tt.publicKey, tt.teamID)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("verifyTrustedPullPeer() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("verifyTrustedPullPeer() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func trustedPeer(label string, key [32]byte, trust peer.TrustState) peer.Peer {
	return peer.Peer{
		DisplayName:  label,
		Fingerprint:  "SHA256:" + label,
		X25519Public: base64.StdEncoding.EncodeToString(key[:]),
		Trust:        trust,
	}
}

func bytes32(seed byte) [32]byte {
	var key [32]byte
	for i := range key {
		key[i] = seed
	}
	return key
}
