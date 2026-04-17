// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

func setTestConfigDir(t *testing.T, dir string) {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
}

func TestConfigRoundTripPreservesDisplayName(t *testing.T) {
	cfg := Default()
	cfg.Identity.DisplayName = "test-user"
	cfg.Relay.URL = "https://custom-relay.example.com"

	data, err := toml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Config
	if err := toml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	decoded.Identity.Normalize()

	if decoded.Identity.DisplayName != "test-user" {
		t.Fatalf("display name mismatch: got %q", decoded.Identity.DisplayName)
	}
	if decoded.Relay.URL != "https://custom-relay.example.com" {
		t.Fatalf("relay URL mismatch: got %q", decoded.Relay.URL)
	}
}

func TestLoadConfigMigratesLegacyGitHubUsername(t *testing.T) {
	tmpDir := t.TempDir()
	setTestConfigDir(t, tmpDir)

	configDir, err := ConfigDir()
	if err != nil {
		t.Fatalf("config dir: %v", err)
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	path := filepath.Join(configDir, "config.toml")
	raw := strings.Join([]string{
		"[identity]",
		`github_username = "legacy-user"`,
		`fingerprint = "SHA256:test"`,
		"",
		"[relay]",
		`url = "https://relay.devcontract.dev"`,
		`timeout_seconds = 10`,
		"",
		"[network]",
		`listen_port = 7733`,
		`mdns_enabled = true`,
		`mdns_timeout_ms = 2000`,
		`holepunch_timeout_ms = 5000`,
		`holepunch_enabled = false`,
		"",
		"[sync]",
		`default_file = ".env"`,
		`auto_backup = true`,
		`max_versions = 10`,
		`confirm_before_apply = true`,
		`merge_strategy = "interactive"`,
		"",
		"[ui]",
		`color = true`,
		`verbose = false`,
		"",
		"[telemetry]",
		`enabled = false`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if loaded.Identity.DisplayName != "legacy-user" {
		t.Fatalf("expected migrated display name, got %q", loaded.Identity.DisplayName)
	}
	if loaded.Identity.LegacyGitHubUsername != "" {
		t.Fatalf("legacy username should be cleared after normalize")
	}
}

func TestSaveConfigWritesCanonicalDisplayName(t *testing.T) {
	tmpDir := t.TempDir()
	setTestConfigDir(t, tmpDir)

	cfg := Default()
	cfg.Identity.DisplayName = "integration-test"

	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	path, err := ConfigFilePath()
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, "display_name") || !strings.Contains(text, "integration-test") {
		t.Fatalf("expected canonical display_name in config file:\n%s", text)
	}
	if strings.Contains(text, "github_username") {
		t.Fatalf("legacy github_username should not be written:\n%s", text)
	}
}
