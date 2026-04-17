// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package envfile

import (
	"testing"
)

func TestParseBasicKeyValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		key   string
		value string
	}{
		{"simple", "KEY=value", "KEY", "value"},
		{"spaces around equals", "KEY = value", "KEY", "value"},
		{"trailing spaces", "KEY = value  ", "KEY", "value"},
		{"empty value", "KEY=", "KEY", ""},
		{"explicit empty double", `KEY=""`, "KEY", ""},
		{"explicit empty single", `KEY=''`, "KEY", ""},
		{"underscored key", "DATABASE_URL=postgres://localhost:5432/mydb", "DATABASE_URL", "postgres://localhost:5432/mydb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			val, ok := env.Get(tt.key)
			if !ok {
				t.Fatalf("key %q not found", tt.key)
			}
			if val != tt.value {
				t.Errorf("got %q, want %q", val, tt.value)
			}
		})
	}
}

func TestParseExportPrefix(t *testing.T) {
	env, err := Parse("export SECRET_KEY=my_secret")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	val, ok := env.Get("SECRET_KEY")
	if !ok {
		t.Fatal("key not found")
	}
	if val != "my_secret" {
		t.Errorf("got %q, want %q", val, "my_secret")
	}
}

func TestParseSingleQuotes(t *testing.T) {
	env, err := Parse(`PASSWORD='p@$$w0rd!#with"special'`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	val, _ := env.Get("PASSWORD")
	expected := `p@$$w0rd!#with"special`
	if val != expected {
		t.Errorf("got %q, want %q", val, expected)
	}
}

func TestParseDoubleQuotesEscapes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		key   string
		value string
	}{
		{"newline escape", `GREETING="Hello\nWorld"`, "GREETING", "Hello\nWorld"},
		{"tab escape", `TAB="Hello\tWorld"`, "TAB", "Hello\tWorld"},
		{"escaped quote", `KEY="has \"quotes\""`, "KEY", `has "quotes"`},
		{"backslash escape", `KEY="path\\to\\file"`, "KEY", `path\to\file`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			val, _ := env.Get(tt.key)
			if val != tt.value {
				t.Errorf("got %q, want %q", val, tt.value)
			}
		})
	}
}

func TestParseSingleQuoteLiteral(t *testing.T) {
	// Single quotes: no escape processing
	env, err := Parse(`KEY='hello\nworld'`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	val, _ := env.Get("KEY")
	if val != `hello\nworld` {
		t.Errorf("got %q, want %q", val, `hello\nworld`)
	}
}

func TestParseComments(t *testing.T) {
	input := `# This is a comment
DATABASE_URL=postgres://localhost:5432/mydb
# Another comment`

	env, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if env.VariableCount() != 1 {
		t.Errorf("expected 1 variable, got %d", env.VariableCount())
	}

	val, _ := env.Get("DATABASE_URL")
	if val != "postgres://localhost:5432/mydb" {
		t.Errorf("got %q", val)
	}
}

func TestParseInlineComment(t *testing.T) {
	env, err := Parse(`KEY=value # comment`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	val, _ := env.Get("KEY")
	if val != "value" {
		t.Errorf("got %q, want %q", val, "value")
	}
}

func TestParseHashInsideQuotes(t *testing.T) {
	env, err := Parse(`KEY="value # not comment"`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	val, _ := env.Get("KEY")
	if val != "value # not comment" {
		t.Errorf("got %q, want %q", val, "value # not comment")
	}
}

func TestParseBlankLines(t *testing.T) {
	input := `KEY1=val1

KEY2=val2

`
	env, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if env.VariableCount() != 2 {
		t.Errorf("expected 2 variables, got %d", env.VariableCount())
	}
}

func TestParseMultipleEntries(t *testing.T) {
	input := `DATABASE_URL=postgres://localhost:5432/mydb
API_KEY=sk_test_12345
export SECRET_KEY=my_secret
# Comment
EMPTY_VAR=`

	env, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if env.VariableCount() != 4 {
		t.Errorf("expected 4 variables, got %d", env.VariableCount())
	}

	tests := map[string]string{
		"DATABASE_URL": "postgres://localhost:5432/mydb",
		"API_KEY":      "sk_test_12345",
		"SECRET_KEY":   "my_secret",
		"EMPTY_VAR":    "",
	}

	for k, expected := range tests {
		val, ok := env.Get(k)
		if !ok {
			t.Errorf("key %q not found", k)
			continue
		}
		if val != expected {
			t.Errorf("%s: got %q, want %q", k, val, expected)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	input := `# Database configuration
DATABASE_URL=postgres://localhost:5432/mydb
API_KEY=sk_test_12345

# Secrets
export SECRET_KEY=my_secret
EMPTY_VAR=`

	env, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	output := Write(env)

	// Re-parse the output
	env2, err := Parse(output)
	if err != nil {
		t.Fatalf("Re-parse error: %v", err)
	}

	// Compare maps
	map1 := env.ToMap()
	map2 := env2.ToMap()

	if len(map1) != len(map2) {
		t.Fatalf("map size mismatch: %d vs %d", len(map1), len(map2))
	}

	for k, v1 := range map1 {
		v2, ok := map2[k]
		if !ok {
			t.Errorf("key %q missing after round-trip", k)
		}
		if v1 != v2 {
			t.Errorf("key %q: %q → %q", k, v1, v2)
		}
	}
}

func TestDiff(t *testing.T) {
	local, _ := Parse(`KEEP=same
MODIFY=old
REMOVE=gone`)

	remote, _ := Parse(`KEEP=same
MODIFY=new
ADD=fresh`)

	diff := Diff(local, remote)

	if len(diff.Added) != 1 || diff.Added[0].Key != "ADD" {
		t.Errorf("expected 1 added (ADD), got %v", diff.Added)
	}
	if len(diff.Removed) != 1 || diff.Removed[0].Key != "REMOVE" {
		t.Errorf("expected 1 removed (REMOVE), got %v", diff.Removed)
	}
	if len(diff.Modified) != 1 || diff.Modified[0].Key != "MODIFY" {
		t.Errorf("expected 1 modified (MODIFY), got %v", diff.Modified)
	}
	if diff.UnchangedCount != 1 {
		t.Errorf("expected 1 unchanged, got %d", diff.UnchangedCount)
	}
}

func TestSetAndDelete(t *testing.T) {
	env, _ := Parse("KEY1=val1\nKEY2=val2")

	env.Set("KEY1", "updated")
	val, _ := env.Get("KEY1")
	if val != "updated" {
		t.Errorf("Set failed: got %q", val)
	}

	env.Set("KEY3", "new")
	val, ok := env.Get("KEY3")
	if !ok || val != "new" {
		t.Errorf("Set new key failed: got %q, ok=%v", val, ok)
	}

	deleted := env.Delete("KEY2")
	if !deleted {
		t.Error("Delete returned false")
	}
	_, ok = env.Get("KEY2")
	if ok {
		t.Error("Key still exists after delete")
	}
}
