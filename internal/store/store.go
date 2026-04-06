// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/fsutil"
)

const (
	projectLockName      = ".lock"
	sequenceManifestName = "sequence.txt"
	lockWaitTimeout      = 2 * time.Second
	lockRetryDelay       = 25 * time.Millisecond
)

// Store manages encrypted .env version history.
type Store struct {
	maxVersions int
	baseDir     string
}

// VersionInfo describes a stored version.
type VersionInfo struct {
	Sequence  int
	Timestamp time.Time
	FilePath  string
	SizeBytes int64
}

// New creates a new Store with the given maximum version count.
func New(maxVersions int) (*Store, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil, err
	}

	return &Store{
		maxVersions: maxVersions,
		baseDir:     filepath.Join(dataDir, "store"),
	}, nil
}

// projectDir returns the directory for a specific project namespace.
func (s *Store) projectDir(projectID string) string {
	return filepath.Join(s.baseDir, projectID)
}

func (s *Store) lockPath(projectID string) string {
	return filepath.Join(s.projectDir(projectID), projectLockName)
}

func (s *Store) manifestPath(projectID string) string {
	return filepath.Join(s.projectDir(projectID), sequenceManifestName)
}

// Save encrypts and saves a .env file as a new version.
func (s *Store) Save(projectID string, content []byte, sequence int, encryptionKey [32]byte) error {
	if sequence < 1 {
		return fmt.Errorf("invalid version sequence %d", sequence)
	}

	return s.withProjectLock(projectID, func(dir string) error {
		encrypted, err := crypto.Encrypt(content, encryptionKey)
		if err != nil {
			return fmt.Errorf("encrypting version: %w", err)
		}

		timestamp := time.Now().UTC().Format("20060102T150405Z")
		filename := fmt.Sprintf("%06d_%s.enc", sequence, timestamp)
		filePath := filepath.Join(dir, filename)
		if _, err := os.Stat(filePath); err == nil {
			return fmt.Errorf("version %d already exists", sequence)
		}

		if err := fsutil.AtomicWriteFile(filePath, encrypted, 0600); err != nil {
			return fmt.Errorf("writing version file: %w", err)
		}
		if err := s.writeManifestLocked(projectID, sequence); err != nil {
			return err
		}
		return s.rotateLocked(projectID)
	})
}

// Append encrypts and saves a .env file as the next version in one atomic,
// locked operation.
func (s *Store) Append(projectID string, content []byte, encryptionKey [32]byte) (*VersionInfo, error) {
	var version *VersionInfo

	err := s.withProjectLock(projectID, func(dir string) error {
		current, err := s.readManifestLocked(projectID)
		if err != nil {
			return err
		}

		sequence := current + 1
		encrypted, err := crypto.Encrypt(content, encryptionKey)
		if err != nil {
			return fmt.Errorf("encrypting version: %w", err)
		}

		now := time.Now().UTC()
		timestamp := now.Format("20060102T150405Z")
		filename := fmt.Sprintf("%06d_%s.enc", sequence, timestamp)
		filePath := filepath.Join(dir, filename)

		if err := fsutil.AtomicWriteFile(filePath, encrypted, 0600); err != nil {
			return fmt.Errorf("writing version file: %w", err)
		}
		if err := s.writeManifestLocked(projectID, sequence); err != nil {
			return err
		}
		if err := s.rotateLocked(projectID); err != nil {
			return err
		}

		version = &VersionInfo{
			Sequence:  sequence,
			Timestamp: now,
			FilePath:  filePath,
			SizeBytes: int64(len(encrypted)),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return version, nil
}

// Restore decrypts and returns a specific version.
func (s *Store) Restore(projectID string, sequence int, encryptionKey [32]byte) ([]byte, error) {
	versions, err := s.List(projectID)
	if err != nil {
		return nil, err
	}

	for _, v := range versions {
		if v.Sequence == sequence {
			data, err := os.ReadFile(v.FilePath)
			if err != nil {
				return nil, fmt.Errorf("reading version file: %w", err)
			}

			plaintext, err := crypto.Decrypt(data, encryptionKey)
			if err != nil {
				return nil, fmt.Errorf("decrypting version: %w", err)
			}

			return plaintext, nil
		}
	}

	return nil, fmt.Errorf("version %d not found", sequence)
}

// Reencrypt rewrites all stored versions for a project with a new at-rest key.
func (s *Store) Reencrypt(projectID string, oldKey, newKey [32]byte) error {
	return s.withProjectLock(projectID, func(dir string) error {
		versions, err := s.List(projectID)
		if err != nil {
			return err
		}

		for _, v := range versions {
			data, err := os.ReadFile(v.FilePath)
			if err != nil {
				return fmt.Errorf("reading version %d: %w", v.Sequence, err)
			}

			plaintext, err := crypto.Decrypt(data, oldKey)
			if err != nil {
				return fmt.Errorf("decrypting version %d with previous key: %w", v.Sequence, err)
			}

			reencrypted, err := crypto.Encrypt(plaintext, newKey)
			if err != nil {
				return fmt.Errorf("encrypting version %d with new key: %w", v.Sequence, err)
			}

			if err := fsutil.AtomicWriteFile(v.FilePath, reencrypted, 0600); err != nil {
				return fmt.Errorf("rewriting version %d: %w", v.Sequence, err)
			}
		}

		return nil
	})
}

// List returns all stored versions for a project, newest first.
func (s *Store) List(projectID string) ([]VersionInfo, error) {
	dir := s.projectDir(projectID)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading version store: %w", err)
	}

	var versions []VersionInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".enc") {
			continue
		}

		info, err := parseVersionFilename(entry.Name())
		if err != nil {
			continue
		}
		info.FilePath = filepath.Join(dir, entry.Name())

		fileInfo, err := entry.Info()
		if err == nil {
			info.SizeBytes = fileInfo.Size()
		}

		versions = append(versions, info)
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Sequence > versions[j].Sequence
	})

	return versions, nil
}

// Latest returns the most recent version, or nil if none exist.
func (s *Store) Latest(projectID string) (*VersionInfo, error) {
	versions, err := s.List(projectID)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, nil
	}
	return &versions[0], nil
}

// NextSequence returns the next monotonically increasing version sequence number for a project.
func (s *Store) NextSequence(projectID string) (int, error) {
	var next int
	err := s.withProjectLock(projectID, func(string) error {
		current, err := s.readManifestLocked(projectID)
		if err != nil {
			return err
		}
		next = current + 1
		return s.writeManifestLocked(projectID, next)
	})
	if err != nil {
		return 0, err
	}
	return next, nil
}

// CheckWritable verifies that the project namespace can be locked and created
// without mutating version history.
func (s *Store) CheckWritable(projectID string) error {
	return s.withProjectLock(projectID, func(string) error {
		return nil
	})
}

// MigrateNamespace copies legacy encrypted backup files into the canonical
// project namespace without deleting the source namespace.
func (s *Store) MigrateNamespace(fromProjectID, toProjectID string) error {
	if fromProjectID == "" || toProjectID == "" || fromProjectID == toProjectID {
		return nil
	}

	fromVersions, err := s.List(fromProjectID)
	if err != nil {
		return err
	}
	if len(fromVersions) == 0 {
		return nil
	}

	toVersions, err := s.List(toProjectID)
	if err != nil {
		return err
	}
	if len(toVersions) > 0 {
		return nil
	}

	return s.withProjectLock(toProjectID, func(toDir string) error {
		fromDir := s.projectDir(fromProjectID)
		entries, err := os.ReadDir(fromDir)
		if err != nil {
			return fmt.Errorf("reading legacy namespace: %w", err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			srcPath := filepath.Join(fromDir, entry.Name())
			dstPath := filepath.Join(toDir, entry.Name())
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return fmt.Errorf("reading legacy backup %s: %w", srcPath, err)
			}
			if err := fsutil.AtomicWriteFile(dstPath, data, 0600); err != nil {
				return fmt.Errorf("copying legacy backup %s: %w", srcPath, err)
			}
		}

		current, err := s.readManifestLocked(fromProjectID)
		if err != nil {
			return err
		}
		if current > 0 {
			if err := s.writeManifestLocked(toProjectID, current); err != nil {
				return err
			}
		}

		return s.rotateLocked(toProjectID)
	})
}

func (s *Store) rotateLocked(projectID string) error {
	versions, err := s.List(projectID)
	if err != nil {
		return err
	}
	if len(versions) <= s.maxVersions {
		return nil
	}
	for _, v := range versions[s.maxVersions:] {
		if err := os.Remove(v.FilePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing old version: %w", err)
		}
	}
	return nil
}

func (s *Store) readManifestLocked(projectID string) (int, error) {
	data, err := os.ReadFile(s.manifestPath(projectID))
	if err == nil {
		value, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if convErr != nil {
			return 0, fmt.Errorf("parsing sequence manifest: %w", convErr)
		}
		return value, nil
	}
	if !os.IsNotExist(err) {
		return 0, fmt.Errorf("reading sequence manifest: %w", err)
	}

	latest, err := s.Latest(projectID)
	if err != nil {
		return 0, err
	}
	if latest == nil {
		return 0, nil
	}
	return latest.Sequence, nil
}

func (s *Store) writeManifestLocked(projectID string, sequence int) error {
	current, err := s.readManifestWithoutFallback(projectID)
	if err != nil {
		return err
	}
	if current > sequence {
		sequence = current
	}
	return fsutil.AtomicWriteFile(s.manifestPath(projectID), []byte(strconv.Itoa(sequence)), 0600)
}

func (s *Store) readManifestWithoutFallback(projectID string) (int, error) {
	data, err := os.ReadFile(s.manifestPath(projectID))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("reading sequence manifest: %w", err)
	}
	value, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil {
		return 0, fmt.Errorf("parsing sequence manifest: %w", convErr)
	}
	return value, nil
}

func (s *Store) withProjectLock(projectID string, fn func(dir string) error) error {
	dir := s.projectDir(projectID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating project store dir: %w", err)
	}

	lockPath := s.lockPath(projectID)
	deadline := time.Now().Add(lockWaitTimeout)
	for {
		lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			_ = lockFile.Close()
			defer os.Remove(lockPath)
			return fn(dir)
		}
		if !isProjectLockBusy(err) {
			return fmt.Errorf("acquiring project lock: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("acquiring project lock: timed out waiting for %s", lockPath)
		}
		time.Sleep(lockRetryDelay)
	}
}

func isProjectLockBusy(err error) bool {
	if err == nil {
		return false
	}
	if os.IsExist(err) || os.IsPermission(err) {
		return true
	}

	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		if errno, ok := pathErr.Err.(syscall.Errno); ok && runtime.GOOS == "windows" {
			switch errno {
			case 5, 32, 33:
				return true
			}
		}
	}

	return false
}

// parseVersionFilename parses a filename like "000042_20260228T134500Z.enc".
func parseVersionFilename(name string) (VersionInfo, error) {
	name = strings.TrimSuffix(name, ".enc")
	parts := strings.SplitN(name, "_", 2)
	if len(parts) != 2 {
		return VersionInfo{}, fmt.Errorf("malformed version filename: %s", name)
	}

	seq, err := strconv.Atoi(parts[0])
	if err != nil {
		return VersionInfo{}, fmt.Errorf("invalid sequence number: %w", err)
	}

	ts, err := time.Parse("20060102T150405Z", parts[1])
	if err != nil {
		return VersionInfo{}, fmt.Errorf("invalid timestamp: %w", err)
	}

	return VersionInfo{
		Sequence:  seq,
		Timestamp: ts,
	}, nil
}
