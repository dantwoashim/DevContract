// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dantwoashim/devcontract/internal/config"
)

func TestEnsureProjectContextRepairsIncompleteConfig(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".devcontract.toml")
	initial := strings.Join([]string{
		"name = \"DevContract\"",
		"default_file = \".env\"",
		"sync_strategy = \"three-way\"",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(initial), 0600); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg := &config.Config{}
	cfg.Relay.URL = "https://relay.example.com"

	ctx, err := ensureProjectContext(cfg)
	if err != nil {
		t.Fatalf("ensure project context: %v", err)
	}
	if ctx.ProjectID == "" {
		t.Fatal("expected repaired project config to gain a project ID")
	}
	if ctx.Config.SyncStrategy != "three-way" {
		t.Fatalf("expected sync strategy to be preserved, got %q", ctx.Config.SyncStrategy)
	}
	if ctx.Config.Name != "DevContract" {
		t.Fatalf("expected name to be preserved, got %q", ctx.Config.Name)
	}
	if ctx.Config.RelayURL != "https://relay.example.com" {
		t.Fatalf("expected relay URL to be backfilled, got %q", ctx.Config.RelayURL)
	}

	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read repaired config: %v", err)
	}
	text := string(saved)
	if !strings.Contains(text, "project_id = ") {
		t.Fatalf("expected repaired config to contain project_id, got:\n%s", text)
	}
	reloaded, err := loadProjectContext()
	if err != nil {
		t.Fatalf("reload repaired project context: %v", err)
	}
	if reloaded.Config.SyncStrategy != "three-way" {
		t.Fatalf("expected repaired config to keep sync strategy, got %q", reloaded.Config.SyncStrategy)
	}
}

func TestLoadProjectContextReportsMissingProjectID(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".devcontract.toml"), []byte("name = \"DevContract\"\n"), 0600); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if _, err := loadProjectContext(); err == nil {
		t.Fatal("expected loadProjectContext to reject an ID-less project config")
	} else if !strings.Contains(err.Error(), "project config is missing project_id") {
		t.Fatalf("expected missing project ID error, got %v", err)
	}
}
