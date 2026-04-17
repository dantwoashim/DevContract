// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestLogger(t *testing.T) *Logger {
	t.Helper()
	tmpDir := t.TempDir()
	return &Logger{path: filepath.Join(tmpDir, "audit.jsonl")}
}

func TestLogAndRead(t *testing.T) {
	logger := newTestLogger(t)

	entries := []Entry{
		{Event: EventPush, Peer: "alice", File: ".env", VarsChanged: 5, Method: "lan"},
		{Event: EventPull, Peer: "bob", File: ".env", VarsChanged: 3, Method: "relay"},
		{Event: EventInvite, Peer: "charlie"},
	}

	for _, e := range entries {
		if err := logger.Log(e); err != nil {
			t.Fatalf("log: %v", err)
		}
	}

	// Read all
	result, err := logger.Read(0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("got %d entries, want 3", len(result))
	}

	// Newest first
	if result[0].Event != EventInvite {
		t.Errorf("first entry = %s, want invite", result[0].Event)
	}
}

func TestReadWithLimit(t *testing.T) {
	logger := newTestLogger(t)

	for i := 0; i < 10; i++ {
		if err := logger.Log(Entry{Event: EventPush, Peer: "alice"}); err != nil {
			t.Fatalf("log: %v", err)
		}
	}

	result, err := logger.Read(3)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("got %d entries, want 3", len(result))
	}
}

func TestFilterByPeer(t *testing.T) {
	logger := newTestLogger(t)

	for _, entry := range []Entry{
		{Event: EventPush, Peer: "alice"},
		{Event: EventPush, Peer: "bob"},
		{Event: EventPull, Peer: "alice"},
	} {
		if err := logger.Log(entry); err != nil {
			t.Fatalf("log: %v", err)
		}
	}

	result, err := logger.FilterByPeer("alice", 0)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("got %d entries for alice, want 2", len(result))
	}
}

func TestFilterByEvent(t *testing.T) {
	logger := newTestLogger(t)

	for _, entry := range []Entry{
		{Event: EventPush, Peer: "alice"},
		{Event: EventPull, Peer: "bob"},
		{Event: EventPush, Peer: "charlie"},
	} {
		if err := logger.Log(entry); err != nil {
			t.Fatalf("log: %v", err)
		}
	}

	result, err := logger.FilterByEvent(EventPush, 0)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("got %d push entries, want 2", len(result))
	}
}

func TestReadEmptyLog(t *testing.T) {
	logger := newTestLogger(t)

	result, err := logger.Read(0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty log, got %v", result)
	}
}

func TestTimestampAutoSet(t *testing.T) {
	logger := newTestLogger(t)

	before := time.Now().Add(-time.Second)
	if err := logger.Log(Entry{Event: EventPush, Peer: "alice"}); err != nil {
		t.Fatalf("log: %v", err)
	}
	after := time.Now().Add(time.Second)

	entries, _ := logger.Read(0)
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}

	ts := entries[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Errorf("timestamp %v not between %v and %v", ts, before, after)
	}
}

func TestLogFileAppendOnly(t *testing.T) {
	logger := newTestLogger(t)

	for _, entry := range []Entry{
		{Event: EventPush, Peer: "alice"},
		{Event: EventPull, Peer: "bob"},
	} {
		if err := logger.Log(entry); err != nil {
			t.Fatalf("log: %v", err)
		}
	}

	// Read raw file — should have exactly 2 lines
	data, err := os.ReadFile(logger.path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	lineCount := 0
	for _, b := range data {
		if b == '\n' {
			lineCount++
		}
	}
	if lineCount != 2 {
		t.Errorf("expected 2 lines in JSONL, got %d", lineCount)
	}
}
