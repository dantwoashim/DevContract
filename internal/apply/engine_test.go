// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dantwoashim/Env_sync/internal/crypto"
	"github.com/dantwoashim/Env_sync/internal/revision"
)

func TestApplyOverwriteWritesIncomingData(t *testing.T) {
	target := writeTempFile(t, "API_KEY=old\n")
	result, err := Apply(Options{
		TargetFile:    target,
		IncomingData:  []byte("API_KEY=new\n"),
		Policy:        PolicyOverwrite,
		Interactive:   false,
		BackupEnabled: false,
	})
	if err != nil {
		t.Fatalf("apply overwrite: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected overwrite policy to apply incoming data")
	}
	assertFileContents(t, target, "API_KEY=new\n")
}

func TestApplyKeepLocalRefusesIncomingData(t *testing.T) {
	target := writeTempFile(t, "API_KEY=old\n")
	result, err := Apply(Options{
		TargetFile:   target,
		IncomingData: []byte("API_KEY=new\n"),
		Policy:       PolicyKeepLocal,
	})
	if err == nil || err != ErrConflictRefused {
		t.Fatalf("apply keep-local error = %v, want %v", err, ErrConflictRefused)
	}
	if result.Applied {
		t.Fatal("expected keep-local policy to refuse incoming data")
	}
	assertFileContents(t, target, "API_KEY=old\n")
}

func TestApplyInteractiveRequiresUIWhenDisabled(t *testing.T) {
	target := writeTempFile(t, "API_KEY=old\n")
	result, err := Apply(Options{
		TargetFile:   target,
		IncomingData: []byte("API_KEY=new\n"),
		Policy:       PolicyInteractive,
		Interactive:  false,
	})
	if err == nil || err != ErrInteractiveRequired {
		t.Fatalf("apply interactive error = %v, want %v", err, ErrInteractiveRequired)
	}
	if !result.InteractiveRequired {
		t.Fatal("expected interactive policy to mark interactive requirement")
	}
}

func TestApplyThreeWayUsesKnownRevisionBase(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("HOME", dataHome)
	t.Setenv("USERPROFILE", dataHome)

	target := writeTempFile(t, "API_KEY=old\nLOCAL_ONLY=2\n")
	key, err := crypto.DeriveAtRestKey([]byte("test-merge-key"))
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}

	revStore, err := revision.New()
	if err != nil {
		t.Fatalf("revision store: %v", err)
	}
	baseData := []byte("API_KEY=base\nLOCAL_ONLY=1\n")
	baseRevisionID := revision.RevisionID(baseData)
	if _, err := revStore.SaveRevision("project-merge", baseRevisionID, "", baseData, key); err != nil {
		t.Fatalf("seed base revision: %v", err)
	}
	incoming := []byte("API_KEY=base\nLOCAL_ONLY=1\nREMOTE_ONLY=2\n")

	result, err := Apply(Options{
		ProjectID:      "project-merge",
		TargetFile:     target,
		IncomingData:   incoming,
		BaseRevisionID: baseRevisionID,
		NewRevisionID:  revision.RevisionID(incoming),
		Policy:         PolicyThreeWay,
		BackupEnabled:  false,
		BackupKey:      key,
		MaxVersions:    10,
	})
	if err != nil {
		t.Fatalf("apply three-way: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected three-way merge to apply merged data")
	}

	contents := readTempFile(t, target)
	if !strings.Contains(contents, "LOCAL_ONLY=2") {
		t.Fatalf("expected merged file to keep local change, got:\n%s", contents)
	}
	if !strings.Contains(contents, "REMOTE_ONLY=2") {
		t.Fatalf("expected merged file to include remote change, got:\n%s", contents)
	}
}

func TestApplyThreeWayRejectsUnknownRevisionBase(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("HOME", dataHome)
	t.Setenv("USERPROFILE", dataHome)

	target := writeTempFile(t, "API_KEY=old\nLOCAL_ONLY=2\n")
	key, err := crypto.DeriveAtRestKey([]byte("test-merge-key"))
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}

	_, err = Apply(Options{
		ProjectID:      "project-merge",
		TargetFile:     target,
		IncomingData:   []byte("API_KEY=base\nLOCAL_ONLY=1\nREMOTE_ONLY=2\n"),
		BaseRevisionID: "missing-base",
		NewRevisionID:  "incoming-revision",
		Policy:         PolicyThreeWay,
		BackupEnabled:  false,
		BackupKey:      key,
		MaxVersions:    10,
	})
	if err == nil || !strings.Contains(err.Error(), ErrUnknownAncestry.Error()) {
		t.Fatalf("apply three-way unknown ancestry error = %v, want %v", err, ErrUnknownAncestry)
	}
}

func TestApplyThreeWayFallsBackToSharedAncestorCandidate(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("HOME", dataHome)
	t.Setenv("USERPROFILE", dataHome)

	target := writeTempFile(t, "API_KEY=local\nLOCAL_ONLY=2\n")
	key, err := crypto.DeriveAtRestKey([]byte("test-merge-key"))
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}

	revStore, err := revision.New()
	if err != nil {
		t.Fatalf("revision store: %v", err)
	}

	rootData := []byte("API_KEY=base\nLOCAL_ONLY=1\n")
	rootID := revision.RevisionID(rootData)
	if _, err := revStore.SaveRevision("project-merge", rootID, "", rootData, key); err != nil {
		t.Fatalf("seed root revision: %v", err)
	}

	localData := []byte("API_KEY=local\nLOCAL_ONLY=2\n")
	localMeta, err := revStore.SaveRevision("project-merge", revision.RevisionID(localData), rootID, localData, key)
	if err != nil {
		t.Fatalf("seed local revision: %v", err)
	}
	if err := revStore.MarkCurrent("project-merge", localMeta.ID, localData); err != nil {
		t.Fatalf("mark current: %v", err)
	}

	incoming := []byte("API_KEY=base\nLOCAL_ONLY=1\nREMOTE_ONLY=2\n")
	result, err := Apply(Options{
		ProjectID:           "project-merge",
		TargetFile:          target,
		IncomingData:        incoming,
		BaseRevisionID:      "missing-base",
		AncestorRevisionIDs: []string{rootID},
		NewRevisionID:       revision.RevisionID(incoming),
		Policy:              PolicyThreeWay,
		BackupEnabled:       false,
		BackupKey:           key,
		MaxVersions:         10,
	})
	if err != nil {
		t.Fatalf("apply three-way with ancestor fallback: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected three-way merge to apply merged data")
	}

	contents := readTempFile(t, target)
	if !strings.Contains(contents, "LOCAL_ONLY=2") || !strings.Contains(contents, "REMOTE_ONLY=2") {
		t.Fatalf("expected merged file to contain local and remote changes, got:\n%s", contents)
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func readTempFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	return string(data)
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	got := readTempFile(t, path)
	if got != want {
		t.Fatalf("file contents = %q, want %q", got, want)
	}
}
