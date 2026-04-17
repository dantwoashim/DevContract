// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package envfile

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ValidationResult holds all validation findings.
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// IsValid returns true if there are no errors.
func (v *ValidationResult) IsValid() bool {
	return len(v.Errors) == 0
}

// HasWarnings returns true if there are warnings.
func (v *ValidationResult) HasWarnings() bool {
	return len(v.Warnings) > 0
}

const (
	// MaxValueLength is the maximum allowed value length (64KB).
	MaxValueLength = 64 * 1024
	// MaxFileSize is the maximum allowed .env file size (1MB).
	MaxFileSize = 1024 * 1024
)

// Validate checks an EnvFile for common issues.
func Validate(ef *EnvFile) *ValidationResult {
	result := &ValidationResult{}

	if ef == nil {
		result.Errors = append(result.Errors, "nil env file")
		return result
	}

	seen := make(map[string]int) // key → line number

	for i, entry := range ef.Entries {
		if entry.Key == "" {
			continue // comment or blank line
		}

		lineNum := i + 1

		// Duplicate key check
		if prevLine, exists := seen[entry.Key]; exists {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("line %d: duplicate key %q (first seen on line %d)", lineNum, entry.Key, prevLine))
		}
		seen[entry.Key] = lineNum

		// Binary content check
		if !utf8.ValidString(entry.Value) {
			result.Errors = append(result.Errors,
				fmt.Sprintf("line %d: key %q contains invalid UTF-8 (binary content)", lineNum, entry.Key))
		}
		for _, r := range entry.Value {
			if r == 0 {
				result.Errors = append(result.Errors,
					fmt.Sprintf("line %d: key %q contains null byte", lineNum, entry.Key))
				break
			}
		}

		// Max value length
		if len(entry.Value) > MaxValueLength {
			result.Errors = append(result.Errors,
				fmt.Sprintf("line %d: key %q value is %d bytes (max %d)", lineNum, entry.Key, len(entry.Value), MaxValueLength))
		}

		// Suspicious pattern warnings
		if looksLikePrivateKey(entry.Value) {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("line %d: key %q looks like a private key — consider using a key file instead", lineNum, entry.Key))
		}

		if looksLikeMultilineJSON(entry.Value) {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("line %d: key %q contains multi-line JSON — use single-line or a config file", lineNum, entry.Key))
		}
	}

	return result
}

// ValidateRaw validates raw .env file content before parsing.
func ValidateRaw(content []byte) *ValidationResult {
	result := &ValidationResult{}

	if len(content) > MaxFileSize {
		result.Errors = append(result.Errors,
			fmt.Sprintf("file size %d bytes exceeds maximum %d bytes (1MB)", len(content), MaxFileSize))
	}

	if !utf8.Valid(content) {
		result.Errors = append(result.Errors, "file contains invalid UTF-8 encoding")
	}

	return result
}

func looksLikePrivateKey(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "-----begin") &&
		(strings.Contains(lower, "private key") || strings.Contains(lower, "rsa"))
}

func looksLikeMultilineJSON(value string) bool {
	trimmed := strings.TrimSpace(value)
	return (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) &&
		strings.Count(trimmed, "\n") > 2
}
