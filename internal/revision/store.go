// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dantwoashim/devcontract/internal/config"
	"github.com/dantwoashim/devcontract/internal/crypto"
	"github.com/dantwoashim/devcontract/internal/fsutil"
)

type Store struct {
	baseDir string
}

type Metadata struct {
	ID          string    `json:"id"`
	ParentID    string    `json:"parent_id,omitempty"`
	ParentIDs   []string  `json:"parent_ids,omitempty"`
	ContentHash string    `json:"content_hash"`
	CreatedAt   time.Time `json:"created_at"`
}

type PeerAck struct {
	Fingerprint    string    `json:"fingerprint"`
	RevisionID     string    `json:"revision_id"`
	AcknowledgedAt time.Time `json:"acknowledged_at"`
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

func (s *Store) peerAckPath(projectID string) string {
	return filepath.Join(s.projectDir(projectID), "peer_acks.json")
}

func (s *Store) ensureProjectDirs(projectID string) error {
	return os.MkdirAll(s.snapshotsDir(projectID), 0700)
}

func (s *Store) SaveRevision(projectID, revisionID, parentID string, data []byte, key [32]byte) (*Metadata, error) {
	return s.SaveRevisionWithParents(projectID, revisionID, []string{parentID}, data, key)
}

func (s *Store) SaveRevisionWithParents(projectID, revisionID string, parentIDs []string, data []byte, key [32]byte) (*Metadata, error) {
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
		ParentIDs:   normalizeParentIDs(parentIDs),
		ContentHash: RevisionID(data),
		CreatedAt:   time.Now().UTC(),
	}
	meta.ParentID = primaryParentID(meta.ParentIDs)

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
	meta.ParentIDs = normalizeParentIDs(append([]string{meta.ParentID}, meta.ParentIDs...))
	meta.ParentID = primaryParentID(meta.ParentIDs)
	return &meta, nil
}

func (s *Store) Ancestors(projectID, revisionID string, limit int) ([]string, error) {
	if revisionID == "" || limit == 0 {
		return nil, nil
	}
	if limit < 0 {
		limit = 256
	}

	visited := map[string]struct{}{revisionID: {}}
	queue := []string{revisionID}
	ancestors := make([]string, 0, limit)

	for len(queue) > 0 && len(ancestors) < limit {
		current := queue[0]
		queue = queue[1:]

		meta, err := s.Metadata(projectID, current)
		if err != nil {
			return nil, err
		}
		if meta == nil {
			continue
		}

		for _, parentID := range meta.ParentIDs {
			if parentID == "" {
				continue
			}
			if _, seen := visited[parentID]; seen {
				continue
			}
			visited[parentID] = struct{}{}
			ancestors = append(ancestors, parentID)
			queue = append(queue, parentID)
			if len(ancestors) >= limit {
				break
			}
		}
	}

	return ancestors, nil
}

func (s *Store) NearestCommonAncestor(projectID, currentRevisionID string, candidates []string) (string, error) {
	if currentRevisionID == "" {
		return "", nil
	}

	localAncestors, err := s.Ancestors(projectID, currentRevisionID, 512)
	if err != nil {
		return "", err
	}
	localSet := map[string]struct{}{currentRevisionID: {}}
	for _, ancestorID := range localAncestors {
		localSet[ancestorID] = struct{}{}
	}

	for _, candidateID := range normalizeParentIDs(candidates) {
		if _, ok := localSet[candidateID]; ok {
			return candidateID, nil
		}
	}
	return "", nil
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

	parentIDs := []string{}
	if state != nil {
		parentIDs = []string{state.CurrentRevisionID}
	}

	meta, err := s.SaveRevisionWithParents(projectID, currentHash, parentIDs, data, key)
	if err != nil {
		return nil, err
	}
	if err := s.MarkCurrent(projectID, meta.ID, data); err != nil {
		return nil, err
	}
	return meta, nil
}

func (s *Store) LoadPeerAcks(projectID string) (map[string]PeerAck, error) {
	data, err := os.ReadFile(s.peerAckPath(projectID))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]PeerAck{}, nil
		}
		return nil, fmt.Errorf("reading peer acknowledgements: %w", err)
	}

	var acks map[string]PeerAck
	if err := json.Unmarshal(data, &acks); err != nil {
		return nil, fmt.Errorf("parsing peer acknowledgements: %w", err)
	}
	if acks == nil {
		acks = map[string]PeerAck{}
	}
	return acks, nil
}

func (s *Store) PeerAck(projectID, fingerprint string) (*PeerAck, error) {
	acks, err := s.LoadPeerAcks(projectID)
	if err != nil {
		return nil, err
	}
	ack, ok := acks[fingerprint]
	if !ok {
		return nil, nil
	}
	return &ack, nil
}

func (s *Store) MarkPeerAck(projectID, fingerprint, revisionID string) error {
	if projectID == "" || fingerprint == "" || revisionID == "" {
		return nil
	}
	if err := s.ensureProjectDirs(projectID); err != nil {
		return fmt.Errorf("creating revision store: %w", err)
	}

	acks, err := s.LoadPeerAcks(projectID)
	if err != nil {
		return err
	}
	acks[fingerprint] = PeerAck{
		Fingerprint:    fingerprint,
		RevisionID:     revisionID,
		AcknowledgedAt: time.Now().UTC(),
	}

	encoded, err := json.MarshalIndent(acks, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding peer acknowledgements: %w", err)
	}
	return fsutil.AtomicWriteFile(s.peerAckPath(projectID), encoded, 0600)
}

func normalizeParentIDs(parentIDs []string) []string {
	seen := make(map[string]struct{}, len(parentIDs))
	normalized := make([]string, 0, len(parentIDs))
	for _, parentID := range parentIDs {
		if parentID == "" {
			continue
		}
		if _, ok := seen[parentID]; ok {
			continue
		}
		seen[parentID] = struct{}{}
		normalized = append(normalized, parentID)
	}
	return normalized
}

func primaryParentID(parentIDs []string) string {
	if len(parentIDs) == 0 {
		return ""
	}
	return parentIDs[0]
}
