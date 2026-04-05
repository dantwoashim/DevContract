// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
)

// loadConfig reads the config file from the standard location and applies migrations.
func loadConfig() (*config.Config, error) {
	return config.LoadConfig()
}

// loadIdentity reads the configured SSH key and derives the crypto identity.
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

// loadIdentityFromServiceKey loads an EnvSync service key and derives the full identity bundle from it.
func loadIdentityFromServiceKey(path string) (*crypto.KeyPair, error) {
	sk, err := crypto.LoadServiceKeyFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("loading service key: %w", err)
	}

	return loadIdentityFromEd25519(sk.PrivateKey)
}

func loadIdentityFromEd25519(privateKey ed25519.PrivateKey) (*crypto.KeyPair, error) {
	kp, err := crypto.NewKeyPairFromEd25519(privateKey)
	if err != nil {
		return nil, fmt.Errorf("deriving service identity: %w", err)
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
