// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package peer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPeerTrustStateMachine(t *testing.T) {
	p := &Peer{
		DisplayName: "alice",
		Fingerprint: "SHA256:testfingerprint",
		Trust:       TrustUnknown,
		FirstSeen:   time.Now(),
	}

	// Unknown → Trusted
	if err := p.PromoteToTrusted(); err != nil {
		t.Fatalf("promote failed: %v", err)
	}
	if p.Trust != TrustTrusted {
		t.Errorf("expected trusted, got %s", p.Trust)
	}
	if !p.CanSync() {
		t.Error("trusted peer should be able to sync")
	}

	// Trusted → cannot promote again
	if err := p.PromoteToTrusted(); err == nil {
		t.Error("should not be able to promote already trusted peer")
	}

	// Trusted → Revoked
	if err := p.Revoke(); err != nil {
		t.Fatalf("revoke failed: %v", err)
	}
	if p.Trust != TrustRevoked {
		t.Errorf("expected revoked, got %s", p.Trust)
	}
	if p.CanSync() {
		t.Error("revoked peer should not be able to sync")
	}

	// Revoked → cannot revoke again
	if err := p.Revoke(); err == nil {
		t.Error("should not be able to revoke already revoked peer")
	}
}

func TestRegistryPersistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Override the data dir for testing
	registry := &Registry{baseDir: tmpDir}

	// Save a team
	team := &Team{
		ID:        "test-team-123",
		Name:      "Test Team",
		CreatedBy: "SHA256:creator",
		CreatedAt: time.Now(),
		Members:   []string{"SHA256:alice", "SHA256:bob"},
	}
	if err := registry.SaveTeam(team); err != nil {
		t.Fatalf("save team: %v", err)
	}

	// Load it back
	loaded, err := registry.LoadTeam("test-team-123")
	if err != nil {
		t.Fatalf("load team: %v", err)
	}
	if loaded.Name != "Test Team" {
		t.Errorf("name: got %q, want %q", loaded.Name, "Test Team")
	}

	// Save a peer
	alice := &Peer{
		DisplayName: "alice",
		Fingerprint: "SHA256:alicefp",
		Trust:       TrustTrusted,
		FirstSeen:   time.Now(),
		LastSeen:    time.Now(),
	}
	if err := registry.SavePeer("test-team-123", alice); err != nil {
		t.Fatalf("save peer: %v", err)
	}

	// Load peer back
	loadedPeer, err := registry.LoadPeer("test-team-123", "SHA256:alicefp")
	if err != nil {
		t.Fatalf("load peer: %v", err)
	}
	if loadedPeer.DisplayName != "alice" {
		t.Errorf("display name: got %q", loadedPeer.DisplayName)
	}
	if loadedPeer.Trust != TrustTrusted {
		t.Errorf("trust: got %q", loadedPeer.Trust)
	}

	// List peers
	peers, err := registry.ListPeers("test-team-123")
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}

	// List teams
	teams, err := registry.ListTeams()
	if err != nil {
		t.Fatalf("list teams: %v", err)
	}
	if len(teams) != 1 {
		t.Errorf("expected 1 team, got %d", len(teams))
	}

	// Delete peer
	if err := registry.DeletePeer("test-team-123", "SHA256:alicefp"); err != nil {
		t.Fatalf("delete peer: %v", err)
	}

	// Verify deleted
	peers, _ = registry.ListPeers("test-team-123")
	if len(peers) != 0 {
		t.Errorf("expected 0 peers after delete, got %d", len(peers))
	}
}

func TestRegistryNonexistent(t *testing.T) {
	registry := &Registry{baseDir: filepath.Join(os.TempDir(), "envsync_test_nonexistent")}
	defer os.RemoveAll(registry.baseDir)

	_, err := registry.LoadTeam("no-such-team")
	if err == nil {
		t.Error("expected error loading nonexistent team")
	}
}

func TestPeerStatusIcon(t *testing.T) {
	tests := []struct {
		trust TrustState
		icon  string
	}{
		{TrustTrusted, "✓"},
		{TrustPending, "?"},
		{TrustRevoked, "✗"},
		{TrustUnknown, "·"},
	}

	for _, tt := range tests {
		p := Peer{Trust: tt.trust, Fingerprint: "test"}
		if got := p.StatusIcon(); got != tt.icon {
			t.Errorf("trust %s: got %q, want %q", tt.trust, got, tt.icon)
		}
	}
}
