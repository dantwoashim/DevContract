// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package audit

import (
	"crypto/hmac"
	"crypto/rand"
	cryptosha256 "crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dantwoashim/devcontract/internal/config"
	"github.com/dantwoashim/devcontract/internal/fsutil"
)

// EventType identifies an audit event.
type EventType string

const (
	EventPush              EventType = "push"
	EventPull              EventType = "pull"
	EventInvite            EventType = "invite"
	EventJoin              EventType = "join"
	EventRevoke            EventType = "revoke"
	EventKeyRotate         EventType = "key_rotate"
	EventOwnershipTransfer EventType = "ownership_transfer"
	EventConflictResolved  EventType = "conflict_resolved"
	EventBackup            EventType = "backup"
	EventRestore           EventType = "restore"
)

// Entry is a single audit log entry with tamper-evident chaining.
type Entry struct {
	Timestamp     time.Time `json:"timestamp"`
	Event         EventType `json:"event"`
	Peer          string    `json:"peer,omitempty"`
	File          string    `json:"file,omitempty"`
	VarsChanged   int       `json:"vars_changed,omitempty"`
	DeliveryCount int       `json:"delivery_count,omitempty"`
	Method        string    `json:"method,omitempty"`
	Details       string    `json:"details,omitempty"`
	PrevHash      string    `json:"prev_hash,omitempty"`
	HMAC          string    `json:"hmac,omitempty"`
}

// Logger is an append-only, tamper-evident audit log.
type Logger struct {
	mu       sync.Mutex
	path     string
	lastHash string
}

// NewLogger creates a new audit logger and loads the previous chain hash.
func NewLogger() (*Logger, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}

	logPath := filepath.Join(dataDir, "audit.jsonl")
	logger := &Logger{path: logPath}

	data, err := os.ReadFile(logPath)
	if err == nil && len(data) > 0 {
		lines := splitLines(data)
		for i := len(lines) - 1; i >= 0; i-- {
			if len(lines[i]) == 0 {
				continue
			}
			h := cryptosha256.Sum256(lines[i])
			logger.lastHash = hex.EncodeToString(h[:])
			break
		}
	}

	return logger, nil
}

// Log appends a tamper-evident event to the audit log.
func (l *Logger) Log(entry Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	entry.PrevHash = l.lastHash

	entry.HMAC = ""
	entryBytes, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling audit entry: %w", err)
	}

	hmacKey, err := loadOrCreateAuditKey(l.path)
	if err != nil {
		return fmt.Errorf("loading audit integrity key: %w", err)
	}
	mac := hmac.New(cryptosha256.New, hmacKey)
	mac.Write(entryBytes)
	entry.HMAC = hex.EncodeToString(mac.Sum(nil))

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling audit entry: %w", err)
	}

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("opening audit log: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}

	h := cryptosha256.Sum256(data)
	l.lastHash = hex.EncodeToString(h[:])
	return nil
}

// Read returns all audit entries, newest first, after verifying integrity.
func (l *Logger) Read(limit int) ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if err := verifyEntries(data, l.path); err != nil {
		return nil, err
	}

	var all []Entry
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, fmt.Errorf("parsing audit entry: %w", err)
		}
		all = append(all, entry)
	}

	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// FilterByPeer returns entries for a specific peer.
func (l *Logger) FilterByPeer(peer string, limit int) ([]Entry, error) {
	all, err := l.Read(0)
	if err != nil {
		return nil, err
	}
	var filtered []Entry
	for _, entry := range all {
		if entry.Peer == peer {
			filtered = append(filtered, entry)
			if limit > 0 && len(filtered) >= limit {
				break
			}
		}
	}
	return filtered, nil
}

// FilterByEvent returns entries of a specific event type.
func (l *Logger) FilterByEvent(event EventType, limit int) ([]Entry, error) {
	all, err := l.Read(0)
	if err != nil {
		return nil, err
	}
	var filtered []Entry
	for _, entry := range all {
		if entry.Event == event {
			filtered = append(filtered, entry)
			if limit > 0 && len(filtered) >= limit {
				break
			}
		}
	}
	return filtered, nil
}

// Verify checks the on-disk audit log without returning entries.
func (l *Logger) Verify() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := os.ReadFile(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return verifyEntries(data, l.path)
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

func loadOrCreateAuditKey(auditPath string) ([]byte, error) {
	keyPath := auditPath + ".key"
	data, err := os.ReadFile(keyPath)
	if err == nil && len(data) == 32 {
		return data, nil
	}
	if err == nil && len(data) != 32 {
		return nil, fmt.Errorf("invalid audit key length %d", len(data))
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading audit key: %w", err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating audit key: %w", err)
	}
	if err := fsutil.AtomicWriteFile(keyPath, key, 0600); err != nil {
		return nil, fmt.Errorf("persisting audit key: %w", err)
	}
	return key, nil
}

func verifyEntries(data []byte, auditPath string) error {
	key, err := loadOrCreateAuditKey(auditPath)
	if err != nil {
		return err
	}

	prevHash := ""
	for idx, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}

		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			return fmt.Errorf("parsing audit entry %d: %w", idx+1, err)
		}
		if entry.PrevHash != prevHash {
			return fmt.Errorf("audit chain mismatch at entry %d", idx+1)
		}

		expectedHMAC := entry.HMAC
		entry.HMAC = ""
		entryBytes, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("re-encoding audit entry %d: %w", idx+1, err)
		}

		mac := hmac.New(cryptosha256.New, key)
		mac.Write(entryBytes)
		actualHMAC := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expectedHMAC), []byte(actualHMAC)) {
			return fmt.Errorf("audit HMAC mismatch at entry %d", idx+1)
		}

		h := cryptosha256.Sum256(line)
		prevHash = hex.EncodeToString(h[:])
	}
	return nil
}
