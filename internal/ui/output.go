// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package ui

import (
	"fmt"
	"io"
	"os"
)

var (
	stdoutWriter io.Writer = os.Stdout
	stderrWriter io.Writer = os.Stderr
	quietOutput  bool
)

// SetQuiet configures whether non-error UI output should be suppressed.
func SetQuiet(enabled bool) {
	quietOutput = enabled
}

// SetNoColor configures process-wide no-color behavior for lipgloss rendering.
func SetNoColor(enabled bool) {
	if enabled {
		_ = os.Setenv("NO_COLOR", "1")
		return
	}
	_ = os.Unsetenv("NO_COLOR")
}

func printOutln(text string) {
	if quietOutput {
		return
	}
	fmt.Fprintln(stdoutWriter, text)
}

func printOutf(format string, args ...any) {
	if quietOutput {
		return
	}
	fmt.Fprintf(stdoutWriter, format, args...)
}

func printErrln(text string) {
	fmt.Fprintln(stderrWriter, text)
}

func printErrf(format string, args ...any) {
	fmt.Fprintf(stderrWriter, format, args...)
}
