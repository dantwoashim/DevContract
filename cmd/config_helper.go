// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/pelletier/go-toml/v2"
)

// loadConfig reads the config file from the default location.
func loadConfig() (*config.Config, error) {
	configDir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	if err != nil {
		// Return defaults if config doesn't exist
		if os.IsNotExist(err) {
			return config.Default(), nil
		}
		return nil, err
	}

	cfg := config.Default()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// saveConfig writes the config to disk.
func saveConfig(cfg *config.Config) error {
	configDir, err := config.ConfigDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(configDir, "config.toml"), data, 0600)
}

// loadIdentity reads the SSH key and derives the crypto identity.
func loadIdentity() (*crypto.KeyPair, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	keyPath := ""
	if cfg != nil {
		keyPath = cfg.Identity.SSHKeyPath
	}

	if keyPath == "" {
		home, _ := os.UserHomeDir()
		keyPath = filepath.Join(home, ".ssh", "id_ed25519")
	}

	kp, err := crypto.LoadSSHKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("loading identity: %w\n\n  Run 'envsync init' first", err)
	}
	return kp, nil
}

// readLocalEnv reads the local .env file, returning nil if it doesn't exist.
func readLocalEnv(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// writeEnvFile writes data to the .env file with restricted permissions.
func writeEnvFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}
