// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/pelletier/go-toml/v2"
)

// Load reads the config file from the config directory.
// If the file does not exist, it returns Default() config.
// Returns error on non-ENOENT failures (permissions, corrupt TOML).
func Load() (*Config, error) {
	configDir, err := ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("finding config directory: %w", err)
	}

	path := filepath.Join(configDir, "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := Default()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

// Save writes the config to the config directory.
func Save(cfg *Config) error {
	configDir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	path := filepath.Join(configDir, "config.toml")
	return os.WriteFile(path, data, 0600)
}

// DefaultPort is the default TCP port for EnvSync connections.
const DefaultPort = 7733

// Config represents the global EnvSync configuration.
type Config struct {
	Identity  IdentityConfig  `toml:"identity"`
	Relay     RelayConfig     `toml:"relay"`
	Network   NetworkConfig   `toml:"network"`
	Sync      SyncConfig      `toml:"sync"`
	UI        UIConfig        `toml:"ui"`
	Telemetry TelemetryConfig `toml:"telemetry"`
}

// IdentityConfig holds the user's cryptographic identity.
type IdentityConfig struct {
	SSHKeyPath           string `toml:"ssh_key_path"`
	GitHubUsername       string `toml:"github_username"`
	Fingerprint          string `toml:"fingerprint"`
	IdentityPublicKey    string `toml:"identity_public_key,omitempty"`
	TransportPublicKey   string `toml:"transport_public_key,omitempty"`
	TransportFingerprint string `toml:"transport_fingerprint,omitempty"`
	DeviceName           string `toml:"device_name,omitempty"`
}

// RelayConfig holds relay server settings.
type RelayConfig struct {
	URL            string `toml:"url"`
	TimeoutSeconds int    `toml:"timeout_seconds"`
}

// NetworkConfig holds network settings.
type NetworkConfig struct {
	ListenPort         int  `toml:"listen_port"`
	MDNSEnabled        bool `toml:"mdns_enabled"`
	MDNSTimeoutMs      int  `toml:"mdns_timeout_ms"`
	HolePunchTimeoutMs int  `toml:"holepunch_timeout_ms"`
	// HolePunchEnabled is retained for compatibility with older configs.
	// Production EnvSync uses LAN direct plus encrypted relay fallback.
	HolePunchEnabled bool `toml:"holepunch_enabled"`
}

// SyncConfig holds synchronization settings.
type SyncConfig struct {
	DefaultFile        string `toml:"default_file"`
	AutoBackup         bool   `toml:"auto_backup"`
	MaxVersions        int    `toml:"max_versions"`
	ConfirmBeforeApply bool   `toml:"confirm_before_apply"`
	MergeStrategy      string `toml:"merge_strategy"`
}

// UIConfig holds UI settings.
type UIConfig struct {
	Color   bool `toml:"color"`
	Verbose bool `toml:"verbose"`
}

// TelemetryConfig holds telemetry settings.
type TelemetryConfig struct {
	Enabled bool `toml:"enabled"`
}

// Default returns a Config with sensible defaults.
func Default() *Config {
	return &Config{
		Identity: IdentityConfig{
			SSHKeyPath: defaultSSHKeyPath(),
		},
		Relay: RelayConfig{
			URL:            "https://relay.envsync.dev",
			TimeoutSeconds: 10,
		},
		Network: NetworkConfig{
			ListenPort:         DefaultPort,
			MDNSEnabled:        true,
			MDNSTimeoutMs:      2000,
			HolePunchTimeoutMs: 5000,
			HolePunchEnabled:   false,
		},
		Sync: SyncConfig{
			DefaultFile:        ".env",
			AutoBackup:         true,
			MaxVersions:        10,
			ConfirmBeforeApply: true,
			MergeStrategy:      "interactive",
		},
		UI: UIConfig{
			Color:   true,
			Verbose: false,
		},
		Telemetry: TelemetryConfig{
			Enabled: false,
		},
	}
}

func defaultSSHKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.ssh/id_ed25519"
	}
	return filepath.Join(home, ".ssh", "id_ed25519")
}

// Validate checks the config for errors.
func (c *Config) Validate() error {
	if c.Network.ListenPort < 1 || c.Network.ListenPort > 65535 {
		return fmt.Errorf("listen_port must be between 1 and 65535, got %d", c.Network.ListenPort)
	}
	if c.Sync.MaxVersions < 1 {
		return fmt.Errorf("max_versions must be at least 1, got %d", c.Sync.MaxVersions)
	}
	switch c.Sync.MergeStrategy {
	case "interactive", "overwrite", "keep-local", "three-way":
		// valid
	default:
		return fmt.Errorf("unknown merge_strategy: %q (use: interactive, overwrite, keep-local, three-way)", c.Sync.MergeStrategy)
	}
	return nil
}

// ConfigDir returns the EnvSync config directory for the current platform.
func ConfigDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("cannot determine home directory: %w", err)
			}
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "envsync"), nil

	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", "envsync"), nil

	default: // Linux and others
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "envsync"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		return filepath.Join(home, ".config", "envsync"), nil
	}
}

// DataDir returns the EnvSync data directory (for store, audit logs, etc).
// On all platforms, this is ~/.envsync/ for simplicity and discoverability.
func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".envsync"), nil
}

// EnsureDirs creates the config and data directories if they don't exist.
func EnsureDirs() error {
	configDir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	dataDir, err := DataDir()
	if err != nil {
		return err
	}
	for _, sub := range []string{"store", "teams"} {
		if err := os.MkdirAll(filepath.Join(dataDir, sub), 0700); err != nil {
			return fmt.Errorf("creating data directory %s: %w", sub, err)
		}
	}

	return nil
}
