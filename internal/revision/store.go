// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dantwoashim/Env_sync/internal/config"
	"github.com/dantwoashim/Env_sync/internal/crypto"
	"github.com/dantwoashim/Env_sync/internal/fsutil"
)

type Store struct {
	baseDir string
}

type Metadata struct {
	ID          string    `json:"id"`
	ParentID    string    `json:"parent_id,omitempty"`
	ContentHash string    `json:"content_hash"`
	CreatedAt   time.Time `json:"created_at"`
}

type State struct {
	CurrentRevisionID  string `json:"current_revision_id"`
	CurrentContentHash string `json:"current_content_hash"`
}

func New() (*Store, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil, err
	}

	return &Store{baseDir: filepath.Join(dataDir, "revisions")}, nil
}

func RevisionID(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Store) projectDir(projectID string) string {
	return filepath.Join(s.baseDir, projectID)
}

func (s *Store) snapshotsDir(projectID string) string {
	return filepath.Join(s.projectDir(projectID), "snapshots")
}

func (s *Store) statePath(projectID string) string {
	return filepath.Join(s.projectDir(projectID), "state.json")
}

func (s *Store) snapshotPath(projectID, revisionID string) string {
	return filepath.Join(s.snapshotsDir(projectID), revisionID+".enc")
}

func (s *Store) metadataPath(projectID, revisionID string) string {
	return filepath.Join(s.snapshotsDir(projectID), revisionID+".json")
}

func (s *Store) ensureProjectDirs(projectID string) error {
	return os.MkdirAll(s.snapshotsDir(projectID), 0700)
}

func (s *Store) SaveRevision(projectID, revisionID, parentID string, data []byte, key [32]byte) (*Metadata, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}
	if revisionID == "" {
		revisionID = RevisionID(data)
	}
	if err := s.ensureProjectDirs(projectID); err != nil {
		return nil, fmt.Errorf("creating revision store: %w", err)
	}

	if meta, err := s.Metadata(projectID, revisionID); err == nil && meta != nil {
		return meta, nil
	}

	encrypted, err := crypto.Encrypt(data, key)
	if err != nil {
		return nil, fmt.Errorf("encrypting revision snapshot: %w", err)
	}

	meta := &Metadata{
		ID:          revisionID,
		ParentID:    parentID,
		ContentHash: RevisionID(data),
		CreatedAt:   time.Now().UTC(),
	}

	encodedMeta, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding revision metadata: %w", err)
	}

	if err := fsutil.AtomicWriteFile(s.snapshotPath(projectID, revisionID), encrypted, 0600); err != nil {
		return nil, fmt.Errorf("writing revision snapshot: %w", err)
	}
	if err := fsutil.AtomicWriteFile(s.metadataPath(projectID, revisionID), encodedMeta, 0600); err != nil {
		return nil, fmt.Errorf("writing revision metadata: %w", err)
	}

	return meta, nil
}

func (s *Store) Metadata(projectID, revisionID string) (*Metadata, error) {
	data, err := os.ReadFile(s.metadataPath(projectID, revisionID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading revision metadata: %w", err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parsing revision metadata: %w", err)
	}
	return &meta, nil
}

func (s *Store) HasRevision(projectID, revisionID string) (bool, error) {
	_, err := os.Stat(s.snapshotPath(projectID, revisionID))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("checking revision snapshot: %w", err)
}

func (s *Store) LoadRevision(projectID, revisionID string, key [32]byte) ([]byte, error) {
	data, err := os.ReadFile(s.snapshotPath(projectID, revisionID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("revision %s not found", revisionID)
		}
		return nil, fmt.Errorf("reading revision snapshot: %w", err)
	}

	plaintext, err := crypto.Decrypt(data, key)
	if err != nil {
		return nil, fmt.Errorf("decrypting revision snapshot: %w", err)
	}
	return plaintext, nil
}

func (s *Store) Current(projectID string) (*State, error) {
	data, err := os.ReadFile(s.statePath(projectID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading revision state: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing revision state: %w", err)
	}
	return &state, nil
}

func (s *Store) MarkCurrent(projectID, revisionID string, data []byte) error {
	if err := s.ensureProjectDirs(projectID); err != nil {
		return fmt.Errorf("creating revision store: %w", err)
	}

	state := State{
		CurrentRevisionID:  revisionID,
		CurrentContentHash: RevisionID(data),
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding revision state: %w", err)
	}

	return fsutil.AtomicWriteFile(s.statePath(projectID), encoded, 0600)
}

func (s *Store) SyncCurrent(projectID string, data []byte, key [32]byte) (*Metadata, error) {
	state, err := s.Current(projectID)
	if err != nil {
		return nil, err
	}

	currentHash := RevisionID(data)
	if state != nil && state.CurrentRevisionID != "" && state.CurrentContentHash == currentHash {
		meta, metaErr := s.Metadata(projectID, state.CurrentRevisionID)
		if metaErr == nil && meta != nil {
			return meta, nil
		}
	}

	parentID := ""
	if state != nil {
		parentID = state.CurrentRevisionID
	}

	meta, err := s.SaveRevision(projectID, currentHash, parentID, data, key)
	if err != nil {
		return nil, err
	}
	if err := s.MarkCurrent(projectID, meta.ID, data); err != nil {
		return nil, err
	}
	return meta, nil
}
