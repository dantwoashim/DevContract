// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package peer

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestSaveProjectConfigWritesNearestProjectFile(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "nested", "repo")
	if err := os.MkdirAll(subdir, 0700); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	rootConfigPath := filepath.Join(root, ".devcontract.toml")
	if err := os.WriteFile(rootConfigPath, []byte("project_id = \"root-project\"\n"), 0600); err != nil {
		t.Fatalf("write root config: %v", err)
	}

	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("chdir subdir: %v", err)
	}

	pc := &ProjectConfig{
		ProjectID:    "root-project",
		Name:         "devcontract",
		DefaultFile:  ".env",
		SyncStrategy: "three-way",
	}
	if err := SaveProjectConfig(pc); err != nil {
		t.Fatalf("save project config: %v", err)
	}

	if _, err := os.Stat(filepath.Join(subdir, ".devcontract.toml")); !os.IsNotExist(err) {
		t.Fatalf("expected no nested config file, got err=%v", err)
	}

	saved, err := loadProjectConfigFromPath(rootConfigPath)
	if err != nil {
		t.Fatalf("reload root config: %v", err)
	}
	if saved.ProjectID != "root-project" {
		t.Fatalf("expected root config project ID to be preserved, got %q", saved.ProjectID)
	}
	if saved.Name != "devcontract" {
		t.Fatalf("expected root config name to be updated, got %q", saved.Name)
	}
}
