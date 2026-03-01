// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package audit

import (
	"crypto/hmac"
	"crypto/rand"
	cryptosha256 "crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/envsync/envsync/internal/config"
)

// EventType identifies an audit event.
type EventType string

const (
	EventPush             EventType = "push"
	EventPull             EventType = "pull"
	EventInvite           EventType = "invite"
	EventJoin             EventType = "join"
	EventRevoke           EventType = "revoke"
	EventConflictResolved EventType = "conflict_resolved"
	EventBackup           EventType = "backup"
	EventRestore          EventType = "restore"
)

// Entry is a single audit log entry with tamper-evident chaining.
type Entry struct {
	Timestamp   time.Time `json:"timestamp"`
	Event       EventType `json:"event"`
	Peer        string    `json:"peer,omitempty"`
	File        string    `json:"file,omitempty"`
	VarsChanged int       `json:"vars_changed,omitempty"`
	Method      string    `json:"method,omitempty"`
	Details     string    `json:"details,omitempty"`
	PrevHash    string    `json:"prev_hash,omitempty"`
	HMAC        string    `json:"hmac,omitempty"`
}

// Logger is an append-only, tamper-evident audit log.
type Logger struct {
	mu       sync.Mutex
	path     string
	lastHash string // SHA-256 of the previous entry for chaining
}

// NewLogger creates a new audit logger.
// It loads the hash of the last entry to maintain chain continuity.
func NewLogger() (*Logger, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}

	logPath := filepath.Join(dataDir, "audit.jsonl")

	l := &Logger{
		path: logPath,
	}

	// Load last hash from existing audit log for chain continuity
	data, err := os.ReadFile(logPath)
	if err == nil && len(data) > 0 {
		// Find the last newline-terminated entry
		lines := splitLines(data)
		for i := len(lines) - 1; i >= 0; i-- {
			if len(lines[i]) > 0 {
				h := cryptosha256.Sum256(lines[i])
				l.lastHash = hex.EncodeToString(h[:])
				break
			}
		}
	}

	return l, nil
}

// Log appends a tamper-evident event to the audit log.
func (l *Logger) Log(entry Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	// Chain: include hash of previous entry
	entry.PrevHash = l.lastHash

	// Compute HMAC over entry content (minus HMAC field itself)
	entry.HMAC = "" // Clear before computing
	entryBytes, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling audit entry: %w", err)
	}

	// Derive HMAC key from a persistent secret stored alongside the audit log
	hmacKey := loadOrCreateAuditKey(l.path)
	mac := hmac.New(cryptosha256.New, hmacKey)
	mac.Write(entryBytes)
	entry.HMAC = hex.EncodeToString(mac.Sum(nil))

	// Serialize final entry with HMAC
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling audit entry: %w", err)
	}

	// Update chain hash
	h := cryptosha256.Sum256(data)
	l.lastHash = hex.EncodeToString(h[:])

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("opening audit log: %w", err)
	}
	defer f.Close()

	_, err = f.Write(append(data, '\n'))
	return err
}

// Read returns all audit entries, newest first.
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

	var all []Entry
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		all = append(all, entry)
	}

	// Reverse (newest first)
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
	for _, e := range all {
		if e.Peer == peer {
			filtered = append(filtered, e)
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
	for _, e := range all {
		if e.Event == event {
			filtered = append(filtered, e)
			if limit > 0 && len(filtered) >= limit {
				break
			}
		}
	}

	return filtered, nil
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

// loadOrCreateAuditKey returns a persistent 32-byte HMAC key.
// If the key file doesn't exist, it creates one with crypto/rand.
func loadOrCreateAuditKey(auditPath string) []byte {
	keyPath := auditPath + ".key"
	data, err := os.ReadFile(keyPath)
	if err == nil && len(data) == 32 {
		return data
	}

	// Generate new key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// crypto/rand failure is catastrophic — log and use zeroed key
		// (HMAC with zero key is still a MAC, just not secret)
		fmt.Fprintf(os.Stderr, "envsync: WARNING: crypto/rand failed, audit HMAC integrity degraded\n")
		return make([]byte, 32)
	}

	_ = os.WriteFile(keyPath, key, 0600)
	return key
}
