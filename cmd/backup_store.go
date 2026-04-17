// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"

	"github.com/dantwoashim/devcontract/internal/config"
	"github.com/dantwoashim/devcontract/internal/crypto"
	"github.com/dantwoashim/devcontract/internal/store"
)

func atRestKey(kp *crypto.KeyPair) ([32]byte, error) {
	key, err := crypto.DeriveAtRestKey(kp.X25519Private[:])
	if err != nil {
		return [32]byte{}, fmt.Errorf("deriving backup key: %w", err)
	}
	return key, nil
}

func legacyBackupNamespace(key [32]byte) string {
	return fmt.Sprintf("%x", key[:8])
}

func openProjectStore(cfg *config.Config, projectID string, key [32]byte) (*store.Store, error) {
	maxVersions := 10
	if cfg != nil && cfg.Sync.MaxVersions > 0 {
		maxVersions = cfg.Sync.MaxVersions
	}

	vStore, err := store.New(maxVersions)
	if err != nil {
		return nil, err
	}
	if projectID != "" {
		if err := vStore.MigrateNamespace(legacyBackupNamespace(key), projectID); err != nil {
			return nil, fmt.Errorf("migrating legacy backups: %w", err)
		}
	}

	return vStore, nil
}
