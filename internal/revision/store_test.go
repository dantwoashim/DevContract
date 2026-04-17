// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package revision

import (
	"path/filepath"
	"testing"

	"github.com/dantwoashim/devcontract/internal/crypto"
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
	if len(second.ParentIDs) != 1 || second.ParentIDs[0] != first.ID {
		t.Fatalf("parent_ids = %v, want [%q]", second.ParentIDs, first.ID)
	}
}

func TestNearestCommonAncestorFollowsRemoteCandidateOrder(t *testing.T) {
	s := newTestStore(t)
	key := testKey(t)

	rootData := []byte("ONE=1\n")
	rootID := RevisionID(rootData)
	if _, err := s.SaveRevision("project", rootID, "", rootData, key); err != nil {
		t.Fatalf("save root: %v", err)
	}

	secondData := []byte("ONE=2\n")
	secondID := RevisionID(secondData)
	if _, err := s.SaveRevision("project", secondID, rootID, secondData, key); err != nil {
		t.Fatalf("save second: %v", err)
	}

	thirdData := []byte("ONE=3\n")
	thirdID := RevisionID(thirdData)
	if _, err := s.SaveRevision("project", thirdID, secondID, thirdData, key); err != nil {
		t.Fatalf("save third: %v", err)
	}

	ancestorID, err := s.NearestCommonAncestor("project", thirdID, []string{"missing", secondID, rootID})
	if err != nil {
		t.Fatalf("nearest common ancestor: %v", err)
	}
	if ancestorID != secondID {
		t.Fatalf("nearest common ancestor = %q, want %q", ancestorID, secondID)
	}
}

func TestMarkPeerAckPersistsAcknowledgement(t *testing.T) {
	s := newTestStore(t)
	if err := s.MarkPeerAck("project", "peer-fp", "rev-123"); err != nil {
		t.Fatalf("mark peer ack: %v", err)
	}

	ack, err := s.PeerAck("project", "peer-fp")
	if err != nil {
		t.Fatalf("load peer ack: %v", err)
	}
	if ack == nil {
		t.Fatal("expected peer ack")
	}
	if ack.RevisionID != "rev-123" {
		t.Fatalf("revision_id = %q, want %q", ack.RevisionID, "rev-123")
	}
}
