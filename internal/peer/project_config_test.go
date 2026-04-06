// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package peer

import "testing"

func TestProjectConfigNormalizeMigratesLegacyTeamID(t *testing.T) {
	pc := &ProjectConfig{
		LegacyTeamID: "legacy-team",
	}

	pc.Normalize()

	if pc.ProjectID != "legacy-team" {
		t.Fatalf("expected project ID to be migrated, got %q", pc.ProjectID)
	}
	if pc.LegacyTeamID != "" {
		t.Fatalf("expected legacy team ID to be cleared, got %q", pc.LegacyTeamID)
	}
}
