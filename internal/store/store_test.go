// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestStore(t *testing.T, maxVersions int) *Store {
	t.Helper()
	tmpDir := t.TempDir()
	return &Store{
		maxVersions: maxVersions,
		baseDir:     filepath.Join(tmpDir, "store"),
	}
}

func testEncryptionKey() [32]byte {
	var key [32]byte
	copy(key[:], []byte("test-key-32-bytes-exactly-padded!"))
	return key
}

func TestSaveAndRestore(t *testing.T) {
	s := newTestStore(t, 10)
	key := testEncryptionKey()
	project := "test-project"

	original := []byte("SECRET_KEY=abc123\nDB_HOST=localhost")

	if err := s.Save(project, original, 1, key); err != nil {
		t.Fatalf("save: %v", err)
	}

	restored, err := s.Restore(project, 1, key)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	if string(restored) != string(original) {
		t.Errorf("restored = %q, want %q", restored, original)
	}
}

func TestListVersions(t *testing.T) {
	s := newTestStore(t, 10)
	key := testEncryptionKey()
	project := "test-project"

	for i := 1; i <= 5; i++ {
		if err := s.Save(project, []byte("data"), i, key); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	versions, err := s.List(project)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(versions) != 5 {
		t.Errorf("got %d versions, want 5", len(versions))
	}

	if versions[0].Sequence != 5 {
		t.Errorf("first version seq = %d, want 5", versions[0].Sequence)
	}
}

func TestLatestVersion(t *testing.T) {
	s := newTestStore(t, 10)
	key := testEncryptionKey()
	project := "test-project"

	if err := s.Save(project, []byte("v1"), 1, key); err != nil {
		t.Fatalf("save v1: %v", err)
	}
	if err := s.Save(project, []byte("v2"), 2, key); err != nil {
		t.Fatalf("save v2: %v", err)
	}

	latest, err := s.Latest(project)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest == nil {
		t.Fatal("latest is nil")
	}
	if latest.Sequence != 2 {
		t.Errorf("latest seq = %d, want 2", latest.Sequence)
	}
}

func TestRotation(t *testing.T) {
	s := newTestStore(t, 3)
	key := testEncryptionKey()
	project := "test-project"

	for i := 1; i <= 6; i++ {
		if err := s.Save(project, []byte("data"), i, key); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	versions, err := s.List(project)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(versions) != 3 {
		t.Errorf("got %d versions after rotation, want 3", len(versions))
	}
	if versions[0].Sequence != 6 {
		t.Errorf("newest = %d, want 6", versions[0].Sequence)
	}
	if versions[2].Sequence != 4 {
		t.Errorf("oldest = %d, want 4", versions[2].Sequence)
	}
}

func TestRestoreWrongKey(t *testing.T) {
	s := newTestStore(t, 10)
	key := testEncryptionKey()
	project := "test-project"

	if err := s.Save(project, []byte("secret data"), 1, key); err != nil {
		t.Fatalf("save: %v", err)
	}

	var wrongKey [32]byte
	copy(wrongKey[:], []byte("wrong-key-32-bytes-exactly-pad!!"))

	_, err := s.Restore(project, 1, wrongKey)
	if err == nil {
		t.Error("expected error with wrong key")
	}
}

func TestRestoreNonexistentVersion(t *testing.T) {
	s := newTestStore(t, 10)
	key := testEncryptionKey()

	_, err := s.Restore("nonexistent", 1, key)
	if err == nil {
		t.Error("expected error for nonexistent version")
	}
}

func TestLatestEmpty(t *testing.T) {
	s := newTestStore(t, 10)

	latest, err := s.Latest("empty-project")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest != nil {
		t.Error("expected nil for empty project")
	}
}

func TestEncryptedFileFormat(t *testing.T) {
	s := newTestStore(t, 10)
	key := testEncryptionKey()
	project := "test-project"

	if err := s.Save(project, []byte("test"), 1, key); err != nil {
		t.Fatalf("save: %v", err)
	}

	dir := s.projectDir(project)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	encFiles := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".enc" {
			encFiles++
		}
	}
	if encFiles != 1 {
		t.Errorf("expected 1 encrypted file, got %d", encFiles)
	}
}

func TestNextSequenceReservesUniqueNumbersUnderConcurrency(t *testing.T) {
	s := newTestStore(t, 10)
	project := "concurrent-project"

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		sequences = map[int]struct{}{}
	)

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seq, err := s.NextSequence(project)
			if err != nil {
				t.Errorf("next sequence: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if _, exists := sequences[seq]; exists {
				t.Errorf("duplicate reserved sequence %d", seq)
			}
			sequences[seq] = struct{}{}
		}()
	}
	wg.Wait()

	if len(sequences) != 8 {
		t.Fatalf("reserved %d unique sequences, want 8", len(sequences))
	}
}

func TestSaveLeavesNoTempFiles(t *testing.T) {
	s := newTestStore(t, 10)
	key := testEncryptionKey()
	project := "temp-cleanup"

	if err := s.Save(project, []byte("hello"), 1, key); err != nil {
		t.Fatalf("save: %v", err)
	}

	entries, err := os.ReadDir(s.projectDir(project))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".devcontract-") {
			t.Fatalf("temporary file %s was left behind", entry.Name())
		}
	}
}

func TestAppendAllocatesSequentialVersions(t *testing.T) {
	s := newTestStore(t, 10)
	key := testEncryptionKey()
	project := "append-project"

	v1, err := s.Append(project, []byte("ONE=1\n"), key)
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	v2, err := s.Append(project, []byte("TWO=2\n"), key)
	if err != nil {
		t.Fatalf("append second: %v", err)
	}

	if v1.Sequence != 1 || v2.Sequence != 2 {
		t.Fatalf("append sequences = %d,%d, want 1,2", v1.Sequence, v2.Sequence)
	}

	restored, err := s.Restore(project, 2, key)
	if err != nil {
		t.Fatalf("restore second: %v", err)
	}
	if string(restored) != "TWO=2\n" {
		t.Fatalf("restored content = %q", restored)
	}
}

func TestMigrateNamespaceCopiesLegacyBackups(t *testing.T) {
	s := newTestStore(t, 10)
	key := testEncryptionKey()
	legacy := "legacy-project"
	canonical := "canonical-project"

	if err := s.Save(legacy, []byte("LEGACY=1\n"), 1, key); err != nil {
		t.Fatalf("save legacy: %v", err)
	}
	if err := s.Save(legacy, []byte("LEGACY=2\n"), 2, key); err != nil {
		t.Fatalf("save legacy second: %v", err)
	}

	if err := s.MigrateNamespace(legacy, canonical); err != nil {
		t.Fatalf("migrate namespace: %v", err)
	}

	versions, err := s.List(canonical)
	if err != nil {
		t.Fatalf("list canonical: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("got %d migrated versions, want 2", len(versions))
	}

	restored, err := s.Restore(canonical, 2, key)
	if err != nil {
		t.Fatalf("restore migrated version: %v", err)
	}
	if string(restored) != "LEGACY=2\n" {
		t.Fatalf("restored migrated content = %q", restored)
	}
}

func TestReencryptPreservesStoredVersions(t *testing.T) {
	s := newTestStore(t, 10)
	oldKey := testEncryptionKey()
	var newKey [32]byte
	copy(newKey[:], []byte("new-key-32-bytes-exactly-padded!!"))
	project := "reencrypt-project"

	if _, err := s.Append(project, []byte("ONE=1\n"), oldKey); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if _, err := s.Append(project, []byte("TWO=2\n"), oldKey); err != nil {
		t.Fatalf("append second: %v", err)
	}

	if err := s.Reencrypt(project, oldKey, newKey); err != nil {
		t.Fatalf("reencrypt: %v", err)
	}

	if _, err := s.Restore(project, 2, oldKey); err == nil {
		t.Fatal("restore with old key unexpectedly succeeded after reencrypt")
	}

	restored, err := s.Restore(project, 2, newKey)
	if err != nil {
		t.Fatalf("restore with new key: %v", err)
	}
	if string(restored) != "TWO=2\n" {
		t.Fatalf("restored content = %q", restored)
	}
}
