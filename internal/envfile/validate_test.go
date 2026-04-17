// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package envfile

import (
	"strings"
	"testing"
)

func TestValidateClean(t *testing.T) {
	ef := &EnvFile{
		Entries: []Entry{
			{Key: "DB_HOST", Value: "localhost"},
			{Key: "DB_PORT", Value: "5432"},
		},
	}

	result := Validate(ef)
	if !result.IsValid() {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
	if result.HasWarnings() {
		t.Errorf("expected no warnings, got: %v", result.Warnings)
	}
}

func TestValidateDuplicateKeys(t *testing.T) {
	ef := &EnvFile{
		Entries: []Entry{
			{Key: "API_KEY", Value: "abc"},
			{Key: "DB_HOST", Value: "localhost"},
			{Key: "API_KEY", Value: "xyz"},
		},
	}

	result := Validate(ef)
	if !result.IsValid() {
		t.Error("duplicates should be warnings not errors")
	}
	if !result.HasWarnings() {
		t.Error("expected duplicate key warning")
	}
	if !strings.Contains(result.Warnings[0], "duplicate") {
		t.Errorf("warning should mention duplicate: %s", result.Warnings[0])
	}
}

func TestValidateNullByte(t *testing.T) {
	ef := &EnvFile{
		Entries: []Entry{
			{Key: "BAD_KEY", Value: "value\x00with\x00nulls"},
		},
	}

	result := Validate(ef)
	if result.IsValid() {
		t.Error("expected error for null bytes")
	}
}

func TestValidateMaxValueLength(t *testing.T) {
	ef := &EnvFile{
		Entries: []Entry{
			{Key: "HUGE", Value: strings.Repeat("x", MaxValueLength+1)},
		},
	}

	result := Validate(ef)
	if result.IsValid() {
		t.Error("expected error for oversized value")
	}
}

func TestValidatePrivateKeyWarning(t *testing.T) {
	ef := &EnvFile{
		Entries: []Entry{
			{Key: "PK", Value: "-----BEGIN RSA PRIVATE KEY-----\nMIIE..."},
		},
	}

	result := Validate(ef)
	if !result.HasWarnings() {
		t.Error("expected private key warning")
	}
}

func TestValidateRawFileSize(t *testing.T) {
	huge := make([]byte, MaxFileSize+1)
	result := ValidateRaw(huge)
	if result.IsValid() {
		t.Error("expected error for oversized file")
	}
}

func TestValidateNil(t *testing.T) {
	result := Validate(nil)
	if result.IsValid() {
		t.Error("expected error for nil env file")
	}
}

func TestValidateCommentsSkipped(t *testing.T) {
	ef := &EnvFile{
		Entries: []Entry{
			{Key: "", Value: ""},
			{Key: "FOO", Value: "bar"},
		},
	}

	result := Validate(ef)
	if !result.IsValid() {
		t.Errorf("comments should not cause errors: %v", result.Errors)
	}
}
