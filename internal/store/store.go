// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
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

		if err := atomicWriteFile(filePath, encrypted, 0600); err != nil {
			return fmt.Errorf("writing version file: %w", err)
		}
		if err := s.writeManifestLocked(projectID, sequence); err != nil {
			return err
		}
		return s.rotateLocked(projectID)
	})
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
	return os.WriteFile(s.manifestPath(projectID), []byte(strconv.Itoa(sequence)), 0600)
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
		if !os.IsExist(err) {
			return fmt.Errorf("acquiring project lock: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("acquiring project lock: timed out waiting for %s", lockPath)
		}
		time.Sleep(lockRetryDelay)
	}
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, ".envsync-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Chmod(mode); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
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
