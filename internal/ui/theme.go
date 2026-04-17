// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Brand colors
var (
	ColorBrand   = lipgloss.Color("#7C3AED") // Purple
	ColorSuccess = lipgloss.Color("#10B981") // Green
	ColorWarning = lipgloss.Color("#F59E0B") // Amber
	ColorError   = lipgloss.Color("#EF4444") // Red
	ColorMuted   = lipgloss.Color("#6B7280") // Gray
	ColorInfo    = lipgloss.Color("#3B82F6") // Blue
	ColorWhite   = lipgloss.Color("#F9FAFB") // Off-white
	ColorDim     = lipgloss.Color("#4B5563") // Dark gray
)

// Text styles
var (
	StyleTitle = lipgloss.NewStyle().
			Foreground(ColorBrand).
			Bold(true)

	StyleSubtitle = lipgloss.NewStyle().
			Foreground(ColorWhite).
			Bold(true)

	StyleBody = lipgloss.NewStyle().
			Foreground(ColorWhite)

	StyleCode = lipgloss.NewStyle().
			Foreground(ColorInfo)

	StyleDim = lipgloss.NewStyle().
			Foreground(ColorMuted)

	StyleBold = lipgloss.NewStyle().
			Foreground(ColorWhite).
			Bold(true)

	StyleSuccess = lipgloss.NewStyle().
			Foreground(ColorSuccess)

	StyleWarning = lipgloss.NewStyle().
			Foreground(ColorWarning)

	StyleError = lipgloss.NewStyle().
			Foreground(ColorError)

	StyleBrand = lipgloss.NewStyle().
			Foreground(ColorBrand).
			Bold(true)
)

// Icon constants
const (
	IconSuccess = "✓"
	IconError   = "✗"
	IconWarning = "⚠"
	IconInfo    = "▸"
	IconBrand   = "✦"
	IconArrow   = "→"
	IconDot     = "·"
)

// Formatted icon helpers
func BrandIcon() string   { return StyleBrand.Render(IconBrand) }
func SuccessIcon() string { return StyleSuccess.Render(IconSuccess) }
func ErrorIcon() string   { return StyleError.Render(IconError) }
func WarningIcon() string { return StyleWarning.Render(IconWarning) }
func InfoIcon() string    { return StyleBrand.Render(IconInfo) }

// Box styles
var (
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorMuted).
			Padding(0, 1)

	ErrorBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorError).
			Padding(0, 1)

	SuccessBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorSuccess).
			Padding(0, 1)
)

// NoColor returns true if color output should be suppressed.
func NoColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return true
	}
	if os.Getenv("TERM") == "dumb" {
		return true
	}
	return false
}

// TerminalWidth returns the current terminal width, defaulting to 80.
func TerminalWidth() int {
	// Default width — sufficient for most terminals
	return 120
}

// Indent adds consistent left padding.
func Indent(s string) string {
	return "  " + s
}

// IndentBy adds n spaces of left padding.
func IndentBy(s string, n int) string {
	pad := ""
	for i := 0; i < n; i++ {
		pad += " "
	}
	return pad + s
}
