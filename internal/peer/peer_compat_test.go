// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package peer

import "testing"

func TestPeerNormalizeMigratesLegacyDisplayName(t *testing.T) {
	p := &Peer{
		LegacyGitHubUsername: "alice",
		Fingerprint:          "SHA256:test",
		Trust:                TrustTrusted,
	}

	if err := p.Validate(); err != nil {
		t.Fatalf("validate peer: %v", err)
	}
	if p.DisplayName != "alice" {
		t.Fatalf("expected display name to be migrated, got %q", p.DisplayName)
	}
	if p.LegacyGitHubUsername != "" {
		t.Fatalf("expected legacy field to be cleared, got %q", p.LegacyGitHubUsername)
	}
}
