// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package peer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindPeerInTeamMatchesRelayUsername(t *testing.T) {
	registry := &Registry{baseDir: t.TempDir()}
	team := &Team{ID: "project-1", Name: "Project 1", CreatedBy: "owner", Members: []string{"owner"}}
	if err := registry.SaveTeam(team); err != nil {
		t.Fatalf("save team: %v", err)
	}

	member := &Peer{
		DisplayName: "teammate",
		Fingerprint: "SHA256:member-fingerprint",
		Trust:       TrustTrusted,
	}
	member.RelayUsername = "teammate-relay"
	if err := registry.SavePeer(team.ID, member); err != nil {
		t.Fatalf("save peer: %v", err)
	}

	got, err := registry.FindPeerInTeam(team.ID, "teammate-relay")
	if err != nil {
		t.Fatalf("find peer: %v", err)
	}
	if got.Fingerprint != member.Fingerprint {
		t.Fatalf("fingerprint = %q, want %q", got.Fingerprint, member.Fingerprint)
	}
}

func TestFindPeerInTeamRejectsAmbiguousLabels(t *testing.T) {
	registry := &Registry{baseDir: t.TempDir()}
	team := &Team{ID: "project-1", Name: "Project 1", CreatedBy: "owner", Members: []string{"owner"}}
	if err := registry.SaveTeam(team); err != nil {
		t.Fatalf("save team: %v", err)
	}

	first := &Peer{DisplayName: "shared-label", Fingerprint: "SHA256:first-fingerprint", Trust: TrustTrusted}
	second := &Peer{DisplayName: "shared-label", Fingerprint: "SHA256:second-fingerprint", Trust: TrustTrusted}
	if err := registry.SavePeer(team.ID, first); err != nil {
		t.Fatalf("save first peer: %v", err)
	}
	if err := registry.SavePeer(team.ID, second); err != nil {
		t.Fatalf("save second peer: %v", err)
	}

	_, err := registry.FindPeerInTeam(team.ID, "shared-label")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous error, got %v", err)
	}
}

func TestScanIntegrityReportsMalformedPeerFile(t *testing.T) {
	registry := &Registry{baseDir: t.TempDir()}
	team := &Team{ID: "project-1", Name: "Project 1", CreatedBy: "owner", Members: []string{"owner"}}
	if err := registry.SaveTeam(team); err != nil {
		t.Fatalf("save team: %v", err)
	}

	peerDir := filepath.Join(registry.baseDir, team.ID, "peers")
	if err := os.MkdirAll(peerDir, 0o700); err != nil {
		t.Fatalf("mkdir peers: %v", err)
	}
	badPath := filepath.Join(peerDir, "broken.toml")
	if err := os.WriteFile(badPath, []byte("not = [valid"), 0o600); err != nil {
		t.Fatalf("write bad peer file: %v", err)
	}

	issues, err := registry.ScanIntegrity(team.ID)
	if err != nil {
		t.Fatalf("scan integrity: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issue count = %d, want 1", len(issues))
	}
	if issues[0].Path != badPath {
		t.Fatalf("issue path = %q, want %q", issues[0].Path, badPath)
	}
	if !strings.Contains(issues[0].Detail, "invalid peer file") {
		t.Fatalf("issue detail = %q, want malformed peer detail", issues[0].Detail)
	}
}
