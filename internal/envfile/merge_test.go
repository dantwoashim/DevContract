// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package envfile

import (
	"testing"
)

func TestThreeWayMergeNoConflict(t *testing.T) {
	base, _ := Parse("DB_URL=postgres://localhost\nAPI_KEY=base_key\n")
	ours, _ := Parse("DB_URL=postgres://localhost\nAPI_KEY=our_new_key\n")
	theirs, _ := Parse("DB_URL=postgres://production\nAPI_KEY=base_key\n")

	result := ThreeWayMerge(base, ours, theirs)

	if result.HasConflicts() {
		t.Errorf("expected no conflicts, got %d", len(result.Conflicts))
	}

	m := envToMap(result.Merged)
	if m["API_KEY"] != "our_new_key" {
		t.Errorf("API_KEY: got %q, want %q", m["API_KEY"], "our_new_key")
	}
	if m["DB_URL"] != "postgres://production" {
		t.Errorf("DB_URL: got %q, want %q", m["DB_URL"], "postgres://production")
	}
}

func TestThreeWayMergeConflict(t *testing.T) {
	base, _ := Parse("API_KEY=base_key\n")
	ours, _ := Parse("API_KEY=our_key\n")
	theirs, _ := Parse("API_KEY=their_key\n")

	result := ThreeWayMerge(base, ours, theirs)

	if !result.HasConflicts() {
		t.Fatal("expected conflicts")
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(result.Conflicts))
	}

	c := result.Conflicts[0]
	if c.Key != "API_KEY" {
		t.Errorf("conflict key: got %q", c.Key)
	}
	if c.OurValue != "our_key" {
		t.Errorf("our value: got %q", c.OurValue)
	}
	if c.TheirValue != "their_key" {
		t.Errorf("their value: got %q", c.TheirValue)
	}
}

func TestThreeWayMergeBothSame(t *testing.T) {
	base, _ := Parse("KEY=base\n")
	ours, _ := Parse("KEY=same\n")
	theirs, _ := Parse("KEY=same\n")

	result := ThreeWayMerge(base, ours, theirs)

	if result.HasConflicts() {
		t.Error("expected no conflicts when both changed to same value")
	}

	m := envToMap(result.Merged)
	if m["KEY"] != "same" {
		t.Errorf("KEY: got %q, want %q", m["KEY"], "same")
	}
}

func TestThreeWayMergeAdditions(t *testing.T) {
	base, _ := Parse("EXISTING=val\n")
	ours, _ := Parse("EXISTING=val\nOUR_NEW=hello\n")
	theirs, _ := Parse("EXISTING=val\nTHEIR_NEW=world\n")

	result := ThreeWayMerge(base, ours, theirs)

	if result.HasConflicts() {
		t.Error("expected no conflicts for independent additions")
	}

	m := envToMap(result.Merged)
	if m["OUR_NEW"] != "hello" {
		t.Errorf("OUR_NEW: got %q", m["OUR_NEW"])
	}
	if m["THEIR_NEW"] != "world" {
		t.Errorf("THEIR_NEW: got %q", m["THEIR_NEW"])
	}
}

func TestThreeWayMergeDeletion(t *testing.T) {
	base, _ := Parse("KEEP=yes\nDELETE=me\n")
	ours, _ := Parse("KEEP=yes\n")              // we deleted DELETE
	theirs, _ := Parse("KEEP=yes\nDELETE=me\n") // they kept it unchanged

	result := ThreeWayMerge(base, ours, theirs)

	if result.HasConflicts() {
		t.Error("expected no conflicts for one-sided deletion")
	}

	m := envToMap(result.Merged)
	if _, exists := m["DELETE"]; exists {
		t.Error("DELETE should have been removed")
	}
}

func TestThreeWayMergePreservesLocalCommentsAndOrder(t *testing.T) {
	base, _ := Parse("# shared comment\nA=1\nB=2\n")
	ours, _ := Parse("# local comment\nA=1\nB=2\nLOCAL=keep\n")
	theirs, _ := Parse("# shared comment\nA=1\nB=3\nREMOTE=new\n")

	result := ThreeWayMerge(base, ours, theirs)
	if result.HasConflicts() {
		t.Fatalf("expected no conflicts, got %d", len(result.Conflicts))
	}

	rendered := Write(result.Merged)
	expected := "# local comment\nA=1\nB=3\nLOCAL=keep\nREMOTE=new"
	if rendered != expected {
		t.Fatalf("rendered merge:\n%s\nwant:\n%s", rendered, expected)
	}
}
