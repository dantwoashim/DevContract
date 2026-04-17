// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package envfile

import (
	"strings"
)

// Write serializes an EnvFile back to a string, preserving original formatting.
func Write(env *EnvFile) string {
	var b strings.Builder

	for i, entry := range env.Entries {
		switch entry.Type {
		case EntryBlank:
			b.WriteString("")
		case EntryComment:
			b.WriteString(entry.RawLine)
		case EntryKeyValue:
			if entry.RawLine != "" && !entryModified(entry) {
				// Preserve original formatting
				b.WriteString(entry.RawLine)
			} else {
				// Reconstruct from parsed data
				b.WriteString(entry.Key)
				b.WriteString("=")
				b.WriteString(quoteValue(entry.Value, entry.Quote))
			}
		}

		if i < len(env.Entries)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// entryModified checks if an entry has been modified from its original form.
func entryModified(entry Entry) bool {
	if entry.RawLine == "" {
		return true
	}

	// Quick check: re-parse the raw line and compare
	reparsed, err := parseLine(entry.RawLine, 0)
	if err != nil {
		return true
	}

	return reparsed.Key != entry.Key || reparsed.Value != entry.Value
}

// quoteValue applies appropriate quoting to a value.
func quoteValue(value string, preferredQuote QuoteStyle) string {
	// Determine if quoting is needed
	needsQuoting := false
	needsDoubleQuote := false

	if value == "" {
		return `""`
	}

	for _, r := range value {
		if r == ' ' || r == '\t' || r == '#' || r == '"' || r == '\'' {
			needsQuoting = true
		}
		if r == '\n' || r == '\r' || r == '\t' {
			needsDoubleQuote = true
		}
	}

	if needsDoubleQuote || preferredQuote == QuoteDouble {
		return `"` + escapeDoubleQuoteValue(value) + `"`
	}

	if needsQuoting || preferredQuote == QuoteSingle {
		return "'" + value + "'"
	}

	return value
}

// escapeDoubleQuoteValue escapes special characters for double-quoted values.
func escapeDoubleQuoteValue(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}
