// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package envfile

import (
	"bufio"
	"fmt"
	"strings"
	"unicode"
)

// Entry represents a single line/entry in a .env file.
type Entry struct {
	// Key is the variable name (empty for comments/blank lines).
	Key string

	// Value is the parsed value.
	Value string

	// RawLine is the original line for round-trip preservation.
	RawLine string

	// Type classifies this entry.
	Type EntryType

	// Quote style used (for round-trip).
	Quote QuoteStyle
}

// EntryType classifies an entry in the .env file.
type EntryType int

const (
	EntryKeyValue EntryType = iota
	EntryComment
	EntryBlank
)

// QuoteStyle indicates how the value was quoted.
type QuoteStyle int

const (
	QuoteNone   QuoteStyle = iota
	QuoteSingle            // 'value'
	QuoteDouble            // "value"
)

// EnvFile represents a parsed .env file preserving structure.
type EnvFile struct {
	Entries []Entry
}

// Parse parses a .env file from a string, preserving comments, ordering, and blank lines.
func Parse(content string) (*EnvFile, error) {
	// Strip UTF-8 BOM if present
	content = strings.TrimPrefix(content, "\xEF\xBB\xBF")

	// Normalize CRLF → LF
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	env := &EnvFile{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		entry, err := parseLine(line, lineNum)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		// Handle multiline double-quoted values
		if entry.Type == EntryKeyValue && entry.Quote == QuoteDouble && isOpenQuote(line) {
			var multilineValue strings.Builder
			multilineValue.WriteString(entry.Value)

			for scanner.Scan() {
				lineNum++
				nextLine := scanner.Text()
				entry.RawLine += "\n" + nextLine

				if strings.HasSuffix(strings.TrimRight(nextLine, " \t"), `"`) && !strings.HasSuffix(strings.TrimRight(nextLine, " \t"), `\"`) {
					// Closing quote found
					closingPart := strings.TrimRight(nextLine, " \t")
					closingPart = closingPart[:len(closingPart)-1] // Remove closing quote
					multilineValue.WriteString("\n")
					multilineValue.WriteString(closingPart)
					break
				} else {
					multilineValue.WriteString("\n")
					multilineValue.WriteString(nextLine)
				}
			}

			entry.Value = processDoubleQuoteEscapes(multilineValue.String())
		}

		env.Entries = append(env.Entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading .env content: %w", err)
	}

	return env, nil
}

// parseLine parses a single line of a .env file.
func parseLine(line string, lineNum int) (Entry, error) {
	raw := line
	trimmed := strings.TrimSpace(line)

	// Blank line
	if trimmed == "" {
		return Entry{RawLine: raw, Type: EntryBlank}, nil
	}

	// Comment line
	if strings.HasPrefix(trimmed, "#") {
		return Entry{RawLine: raw, Type: EntryComment}, nil
	}

	// Strip 'export ' prefix
	cleanLine := strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))

	// Split on first '='
	eqIdx := strings.IndexByte(cleanLine, '=')
	if eqIdx < 0 {
		return Entry{RawLine: raw, Type: EntryComment}, nil // Treat as unparseable comment
	}

	key := strings.TrimSpace(cleanLine[:eqIdx])
	rawValue := cleanLine[eqIdx+1:]

	// Validate key
	if key == "" {
		return Entry{}, fmt.Errorf("empty key")
	}
	for _, r := range key {
		if !isKeyChar(r) {
			return Entry{}, fmt.Errorf("invalid character %q in key %q", r, key)
		}
	}

	// Parse value
	value, quote := parseValue(rawValue)

	return Entry{
		Key:     key,
		Value:   value,
		RawLine: raw,
		Type:    EntryKeyValue,
		Quote:   quote,
	}, nil
}

// parseValue parses the value portion of a KEY=VALUE line.
func parseValue(raw string) (string, QuoteStyle) {
	trimmed := strings.TrimSpace(raw)

	if trimmed == "" {
		return "", QuoteNone
	}

	// Single-quoted: verbatim, no escapes (except \')
	if strings.HasPrefix(trimmed, "'") {
		end := strings.LastIndex(trimmed, "'")
		if end > 0 {
			inner := trimmed[1:end]
			return inner, QuoteSingle
		}
		// No closing quote — treat as unquoted
		return trimmed[1:], QuoteSingle
	}

	// Double-quoted: process escape sequences
	if strings.HasPrefix(trimmed, `"`) {
		end := findClosingDoubleQuote(trimmed)
		if end > 0 {
			inner := trimmed[1:end]
			return processDoubleQuoteEscapes(inner), QuoteDouble
		}
		// No closing quote on this line — multiline, return raw for now
		inner := trimmed[1:]
		return inner, QuoteDouble
	}

	// Unquoted: trim, strip inline comments
	value := trimmed
	if commentIdx := findInlineComment(value); commentIdx >= 0 {
		value = strings.TrimRight(value[:commentIdx], " \t")
	}

	return value, QuoteNone
}

// processDoubleQuoteEscapes handles escape sequences in double-quoted values.
func processDoubleQuoteEscapes(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case '\'':
				b.WriteByte('\'')
			default:
				b.WriteByte('\\')
				b.WriteByte(s[i+1])
			}
			i += 2
		} else {
			b.WriteByte(s[i])
			i++
		}
	}

	return b.String()
}

// findClosingDoubleQuote finds the index of the closing " in a double-quoted value,
// respecting escaped quotes.
func findClosingDoubleQuote(s string) int {
	if len(s) < 2 || s[0] != '"' {
		return -1
	}

	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++ // Skip escaped character
			continue
		}
		if s[i] == '"' {
			return i
		}
	}
	return -1
}

// findInlineComment finds the index of an inline comment (#) in an unquoted value.
func findInlineComment(s string) int {
	for i, r := range s {
		if r == '#' && i > 0 && s[i-1] == ' ' {
			return i
		}
	}
	return -1
}

// isOpenQuote checks if a line has an opening double quote without a closing one.
func isOpenQuote(line string) bool {
	trimmed := strings.TrimSpace(line)
	eqIdx := strings.IndexByte(trimmed, '=')
	if eqIdx < 0 {
		return false
	}
	valuePart := strings.TrimSpace(trimmed[eqIdx+1:])
	if !strings.HasPrefix(valuePart, `"`) {
		return false
	}
	return findClosingDoubleQuote(valuePart) < 0
}

// isKeyChar checks if a character is valid in an env variable key.
func isKeyChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.'
}

// Get returns the value for a key, or empty string if not found.
func (e *EnvFile) Get(key string) (string, bool) {
	for _, entry := range e.Entries {
		if entry.Type == EntryKeyValue && entry.Key == key {
			return entry.Value, true
		}
	}
	return "", false
}

// Set sets a key to a value. If the key exists, it updates it; otherwise appends.
func (e *EnvFile) Set(key, value string) {
	for i, entry := range e.Entries {
		if entry.Type == EntryKeyValue && entry.Key == key {
			e.Entries[i].Value = value
			// Preserve the original quote style for round-trip fidelity
			e.Entries[i].RawLine = key + "=" + quoteValue(value, entry.Quote)
			return
		}
	}
	// Append new entry — detect appropriate quoting
	quote := QuoteNone
	for _, r := range value {
		if r == ' ' || r == '\t' || r == '#' || r == '"' || r == '\'' || r == '\n' {
			quote = QuoteDouble
			break
		}
	}
	e.Entries = append(e.Entries, Entry{
		Key:     key,
		Value:   value,
		RawLine: key + "=" + quoteValue(value, quote),
		Type:    EntryKeyValue,
		Quote:   quote,
	})
}

// Delete removes a key from the file.
func (e *EnvFile) Delete(key string) bool {
	for i, entry := range e.Entries {
		if entry.Type == EntryKeyValue && entry.Key == key {
			e.Entries = append(e.Entries[:i], e.Entries[i+1:]...)
			return true
		}
	}
	return false
}

// Keys returns all variable keys in order.
func (e *EnvFile) Keys() []string {
	var keys []string
	for _, entry := range e.Entries {
		if entry.Type == EntryKeyValue {
			keys = append(keys, entry.Key)
		}
	}
	return keys
}

// ToMap returns all key-value pairs as a map.
func (e *EnvFile) ToMap() map[string]string {
	m := make(map[string]string)
	for _, entry := range e.Entries {
		if entry.Type == EntryKeyValue {
			m[entry.Key] = entry.Value
		}
	}
	return m
}

// VariableCount returns the number of key-value entries.
func (e *EnvFile) VariableCount() int {
	count := 0
	for _, entry := range e.Entries {
		if entry.Type == EntryKeyValue {
			count++
		}
	}
	return count
}

// Interpolate replaces ${VAR} and $VAR references in double-quoted values
// with the values of other variables defined in the same file.
// Single-quoted values are never interpolated (they are literal).
// This is optional and must be called explicitly after Parse().
func (e *EnvFile) Interpolate() {
	vars := e.ToMap()

	for i, entry := range e.Entries {
		if entry.Type != EntryKeyValue || entry.Quote != QuoteDouble {
			continue
		}

		value := entry.Value
		// Replace ${VAR} patterns
		result := interpolateValue(value, vars)
		if result != value {
			e.Entries[i].Value = result
		}
	}
}

// interpolateValue replaces ${VAR} and $VAR references in a value string.
func interpolateValue(value string, vars map[string]string) string {
	var result strings.Builder
	i := 0
	for i < len(value) {
		if value[i] == '$' && i+1 < len(value) {
			if value[i+1] == '{' {
				// ${VAR} syntax
				end := strings.Index(value[i:], "}")
				if end > 0 {
					varName := value[i+2 : i+end]
					if v, ok := vars[varName]; ok {
						result.WriteString(v)
					} else {
						result.WriteString(value[i : i+end+1])
					}
					i += end + 1
					continue
				}
			} else if isVarNameChar(rune(value[i+1])) {
				// $VAR syntax (no braces)
				j := i + 1
				for j < len(value) && isVarNameChar(rune(value[j])) {
					j++
				}
				varName := value[i+1 : j]
				if v, ok := vars[varName]; ok {
					result.WriteString(v)
				} else {
					result.WriteString(value[i:j])
				}
				i = j
				continue
			}
		}
		result.WriteByte(value[i])
		i++
	}
	return result.String()
}

// isVarNameChar returns true if the rune is valid in an env variable name.
func isVarNameChar(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
}
