// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package config

import "testing"

func TestIdentityNormalizeMigratesLegacyDisplayName(t *testing.T) {
	cfg := &IdentityConfig{
		LegacyGitHubUsername: "alice",
	}

	cfg.Normalize()

	if cfg.DisplayName != "alice" {
		t.Fatalf("expected display name to be migrated, got %q", cfg.DisplayName)
	}
	if cfg.LegacyGitHubUsername != "" {
		t.Fatalf("expected legacy field to be cleared, got %q", cfg.LegacyGitHubUsername)
	}
}
