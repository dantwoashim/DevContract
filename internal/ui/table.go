// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package ui

import "strings"

// Column defines a table column.
type Column struct {
	Title string
	Width int // 0 = auto
}

// Table renders a formatted table.
type Table struct {
	Columns []Column
	Rows    [][]string
}

// NewTable creates a table with the given column titles.
func NewTable(titles ...string) *Table {
	cols := make([]Column, len(titles))
	for i, t := range titles {
		cols[i] = Column{Title: t}
	}
	return &Table{Columns: cols}
}

// AddRow adds a row to the table.
func (t *Table) AddRow(values ...string) {
	t.Rows = append(t.Rows, values)
}

// Render returns the formatted table string.
func (t *Table) Render() string {
	if len(t.Columns) == 0 {
		return ""
	}

	// Calculate column widths
	widths := make([]int, len(t.Columns))
	for i, col := range t.Columns {
		widths[i] = len(col.Title)
	}
	for _, row := range t.Rows {
		for i, val := range row {
			if i < len(widths) && len(val) > widths[i] {
				widths[i] = len(val)
			}
		}
	}

	// Cap to terminal width
	termWidth := TerminalWidth() - 6 // padding
	totalWidth := 0
	for _, w := range widths {
		totalWidth += w + 2
	}
	if totalWidth > termWidth {
		// Proportionally shrink
		ratio := float64(termWidth) / float64(totalWidth)
		for i := range widths {
			widths[i] = int(float64(widths[i]) * ratio)
			if widths[i] < 4 {
				widths[i] = 4
			}
		}
	}

	var sb strings.Builder

	// Header
	sb.WriteString("    ")
	for i, col := range t.Columns {
		sb.WriteString(StyleBold.Render(padRight(col.Title, widths[i])))
		if i < len(t.Columns)-1 {
			sb.WriteString("  ")
		}
	}
	sb.WriteString("\n")

	// Separator
	sb.WriteString("    ")
	for i, w := range widths {
		sb.WriteString(StyleDim.Render(strings.Repeat("─", w)))
		if i < len(widths)-1 {
			sb.WriteString("  ")
		}
	}
	sb.WriteString("\n")

	// Rows
	for _, row := range t.Rows {
		sb.WriteString("    ")
		for i := range t.Columns {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			val = truncate(val, widths[i])
			sb.WriteString(padRight(val, widths[i]))
			if i < len(t.Columns)-1 {
				sb.WriteString("  ")
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// padRight pads a string to the given width.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

// truncate shortens a string with ellipsis.
func truncate(s string, maxLen int) string {
	if maxLen < 4 {
		maxLen = 4
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// PrintTable is a convenience function.
func PrintTable(titles []string, rows [][]string) {
	t := NewTable(titles...)
	for _, row := range rows {
		t.AddRow(row...)
	}
	printOutf("%s", t.Render())
}
