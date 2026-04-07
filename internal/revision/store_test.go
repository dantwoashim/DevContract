// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package revision

import (
	"path/filepath"
	"testing"

	"github.com/envsync/envsync/internal/crypto"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{baseDir: filepath.Join(t.TempDir(), "revisions")}
}

func testKey(t *testing.T) [32]byte {
	t.Helper()
	key, err := crypto.DeriveAtRestKey([]byte("revision-test-key-material"))
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}
	return key
}

func TestSaveAndLoadRevision(t *testing.T) {
	s := newTestStore(t)
	key := testKey(t)
	data := []byte("API_KEY=abc123\n")

	meta, err := s.SaveRevision("project", RevisionID(data), "", data, key)
	if err != nil {
		t.Fatalf("save revision: %v", err)
	}
	if meta.ID == "" {
		t.Fatal("expected revision ID")
	}

	restored, err := s.LoadRevision("project", meta.ID, key)
	if err != nil {
		t.Fatalf("load revision: %v", err)
	}
	if string(restored) != string(data) {
		t.Fatalf("restored = %q, want %q", restored, data)
	}
}

func TestSyncCurrentTracksParentRevision(t *testing.T) {
	s := newTestStore(t)
	key := testKey(t)

	first, err := s.SyncCurrent("project", []byte("ONE=1\n"), key)
	if err != nil {
		t.Fatalf("sync first: %v", err)
	}
	second, err := s.SyncCurrent("project", []byte("ONE=2\n"), key)
	if err != nil {
		t.Fatalf("sync second: %v", err)
	}
	if second.ParentID != first.ID {
		t.Fatalf("parent = %q, want %q", second.ParentID, first.ID)
	}
}
