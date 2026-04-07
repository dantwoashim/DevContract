// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package ui

import (
	"fmt"
	"strings"

	"github.com/dantwoashim/Env_sync/internal/envfile"
)

// RenderDiff displays a color-coded diff between two env files.
func RenderDiff(diff *envfile.DiffResult) string {
	if diff == nil {
		return ""
	}

	var sb strings.Builder

	// Summary line
	parts := []string{}
	if len(diff.Added) > 0 {
		parts = append(parts, StyleSuccess.Render(fmt.Sprintf("+%d added", len(diff.Added))))
	}
	if len(diff.Removed) > 0 {
		parts = append(parts, StyleError.Render(fmt.Sprintf("-%d removed", len(diff.Removed))))
	}
	if len(diff.Modified) > 0 {
		parts = append(parts, StyleWarning.Render(fmt.Sprintf("~%d modified", len(diff.Modified))))
	}
	if diff.UnchangedCount > 0 {
		parts = append(parts, StyleDim.Render(fmt.Sprintf("=%d unchanged", diff.UnchangedCount)))
	}

	sb.WriteString("    " + strings.Join(parts, "  ") + "\n\n")

	// Modified entries
	for _, m := range diff.Modified {
		sb.WriteString("    " + StyleWarning.Render("~ "))
		sb.WriteString(StyleBold.Render(m.Key) + "  ")
		sb.WriteString(StyleError.Render(MaskValue(m.OldValue)))
		sb.WriteString(" " + StyleDim.Render(IconArrow) + " ")
		sb.WriteString(StyleSuccess.Render(MaskValue(m.NewValue)))
		sb.WriteString("\n")
	}

	// Added entries
	for _, a := range diff.Added {
		sb.WriteString("    " + StyleSuccess.Render("+ "))
		sb.WriteString(StyleBold.Render(a.Key) + "  ")
		sb.WriteString(StyleSuccess.Render(MaskValue(a.Value)))
		sb.WriteString("\n")
	}

	// Removed entries
	for _, r := range diff.Removed {
		sb.WriteString("    " + StyleError.Render("- "))
		sb.WriteString(StyleBold.Render(r.Key) + "  ")
		sb.WriteString(StyleError.Render(MaskValue(r.Value)))
		sb.WriteString("\n")
	}

	return sb.String()
}

// MaskValue shows first 4 and last 2 chars, masks the middle.
func MaskValue(v string) string {
	if len(v) <= 8 {
		return v
	}
	return v[:4] + "****" + v[len(v)-2:]
}

// RenderDiffSummary returns a one-line summary of changes.
func RenderDiffSummary(diff *envfile.DiffResult) string {
	if diff == nil {
		return "no changes"
	}
	parts := []string{}
	if len(diff.Added) > 0 {
		parts = append(parts, fmt.Sprintf("+%d", len(diff.Added)))
	}
	if len(diff.Removed) > 0 {
		parts = append(parts, fmt.Sprintf("-%d", len(diff.Removed)))
	}
	if len(diff.Modified) > 0 {
		parts = append(parts, fmt.Sprintf("~%d", len(diff.Modified)))
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, ", ")
}
