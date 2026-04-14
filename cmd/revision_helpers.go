// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"os"

	"github.com/dantwoashim/Env_sync/internal/revision"
)

type resolvedRevisionLineage struct {
	BaseRevisionID      string
	RevisionID          string
	AncestorRevisionIDs []string
}

func resolveCurrentRevision(projectID, targetFile string, backupKey [32]byte) (*resolvedRevisionLineage, error) {
	if projectID == "" {
		return &resolvedRevisionLineage{}, nil
	}

	data, err := os.ReadFile(targetFile)
	if err != nil {
		return nil, fmt.Errorf("reading %s for revision ancestry: %w", targetFile, err)
	}

	revStore, err := revision.New()
	if err != nil {
		return nil, fmt.Errorf("creating revision store: %w", err)
	}

	meta, err := revStore.SyncCurrent(projectID, data, backupKey)
	if err != nil {
		return nil, fmt.Errorf("syncing revision ancestry: %w", err)
	}
	if meta == nil {
		return &resolvedRevisionLineage{}, nil
	}

	ancestors, err := revStore.Ancestors(projectID, meta.ID, 32)
	if err != nil {
		return nil, fmt.Errorf("loading revision ancestors: %w", err)
	}

	return &resolvedRevisionLineage{
		BaseRevisionID:      meta.ParentID,
		RevisionID:          meta.ID,
		AncestorRevisionIDs: ancestors,
	}, nil
}
