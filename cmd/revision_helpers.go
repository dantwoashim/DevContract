// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"os"

	"github.com/envsync/envsync/internal/revision"
)

func resolveCurrentRevision(projectID, targetFile string, backupKey [32]byte) (string, string, error) {
	if projectID == "" {
		return "", "", nil
	}

	data, err := os.ReadFile(targetFile)
	if err != nil {
		return "", "", fmt.Errorf("reading %s for revision ancestry: %w", targetFile, err)
	}

	revStore, err := revision.New()
	if err != nil {
		return "", "", fmt.Errorf("creating revision store: %w", err)
	}

	meta, err := revStore.SyncCurrent(projectID, data, backupKey)
	if err != nil {
		return "", "", fmt.Errorf("syncing revision ancestry: %w", err)
	}
	if meta == nil {
		return "", "", nil
	}
	return meta.ParentID, meta.ID, nil
}
