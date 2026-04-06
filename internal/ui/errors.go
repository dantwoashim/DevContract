// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package ui

import "fmt"

// ErrorCategory classifies an error for UX purposes.
type ErrorCategory string

const (
	ErrNetwork ErrorCategory = "network"
	ErrAuth    ErrorCategory = "auth"
	ErrConfig  ErrorCategory = "config"
	ErrFile    ErrorCategory = "file"
	ErrCrypto  ErrorCategory = "crypto"
	ErrRelay   ErrorCategory = "relay"
	ErrSync    ErrorCategory = "sync"
)

// StructuredError is a user-facing error with context.
type StructuredError struct {
	Category   ErrorCategory
	Message    string
	Cause      string
	Suggestion string
	DocsURL    string
}

// RenderError displays a structured error with full context.
func RenderError(e StructuredError) {
	printErrln("")
	printErrf("  %s %s\n", ErrorIcon(), StyleError.Render(e.Message))

	if e.Cause != "" {
		printErrf("    %s %s\n", StyleDim.Render("cause:"), e.Cause)
	}

	if e.Suggestion != "" {
		printErrln("")
		printErrf("    %s %s\n", StyleDim.Render("fix:"), e.Suggestion)
	}

	if e.DocsURL != "" {
		printErrf("    %s %s\n", StyleDim.Render("docs:"), StyleCode.Render(e.DocsURL))
	}
	printErrln("")
}

// Common error constructors

// ErrNoSSHKey returns a structured error for missing SSH key.
func ErrNoSSHKey(path string) StructuredError {
	return StructuredError{
		Category:   ErrConfig,
		Message:    "SSH key not found",
		Cause:      fmt.Sprintf("Expected Ed25519 key at %s", path),
		Suggestion: fmt.Sprintf("ssh-keygen -t ed25519 -f %s", path),
	}
}

// ErrNotInitialized returns a structured error for uninitialized project.
func ErrNotInitialized() StructuredError {
	return StructuredError{
		Category:   ErrConfig,
		Message:    "EnvSync not initialized",
		Cause:      "No config file found. Run 'envsync init' first.",
		Suggestion: "envsync init",
	}
}

// ErrNoPeers returns a structured error when no peers are found.
func ErrNoPeers() StructuredError {
	return StructuredError{
		Category:   ErrNetwork,
		Message:    "No peers found on LAN",
		Cause:      "mDNS discovery found no EnvSync instances. The recipient may not be running 'envsync pull'.",
		Suggestion: "Ask another project member to run: envsync pull",
	}
}

// ErrRelayUnavailable returns a structured error for relay failure.
func ErrRelayUnavailable(cause string) StructuredError {
	return StructuredError{
		Category:   ErrRelay,
		Message:    "Relay server unavailable",
		Cause:      cause,
		Suggestion: "Check your internet connection. The relay may be temporarily down.",
	}
}

// ErrEnvFileNotFound returns a structured error for missing .env file.
func ErrEnvFileNotFound(path string) StructuredError {
	return StructuredError{
		Category:   ErrFile,
		Message:    fmt.Sprintf("File not found: %s", path),
		Cause:      "The .env file does not exist in the current directory.",
		Suggestion: "Create the file or use --file to specify a different path.",
	}
}

// ErrFingerprintMismatch returns a structured error for key mismatch.
func ErrFingerprintMismatch(expected, got string) StructuredError {
	return StructuredError{
		Category:   ErrCrypto,
		Message:    "SSH key fingerprint mismatch",
		Cause:      fmt.Sprintf("Expected %s, got %s", expected, got),
		Suggestion: "The peer may have changed their SSH key. Verify with them directly.",
	}
}
