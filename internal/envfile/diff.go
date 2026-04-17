// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package envfile

import "fmt"

// DiffResult represents the diff between two .env files.
type DiffResult struct {
	Added    []DiffEntry
	Removed  []DiffEntry
	Modified []DiffModified
	// Unchanged keys (not included as entries to save memory).
	UnchangedCount int
}

// DiffEntry represents a single added or removed variable.
type DiffEntry struct {
	Key   string
	Value string
}

// DiffModified represents a changed variable with old and new values.
type DiffModified struct {
	Key      string
	OldValue string
	NewValue string
}

// Diff computes the difference between a local and remote EnvFile.
// local is the current state; remote is the incoming state.
func Diff(local, remote *EnvFile) *DiffResult {
	result := &DiffResult{}

	localMap := local.ToMap()
	remoteMap := remote.ToMap()

	// Find modified and removed keys (iterate local)
	for _, key := range local.Keys() {
		remoteVal, existsInRemote := remoteMap[key]
		if !existsInRemote {
			result.Removed = append(result.Removed, DiffEntry{Key: key, Value: localMap[key]})
		} else if localMap[key] != remoteVal {
			result.Modified = append(result.Modified, DiffModified{
				Key:      key,
				OldValue: localMap[key],
				NewValue: remoteVal,
			})
		} else {
			result.UnchangedCount++
		}
	}

	// Find added keys (in remote but not local)
	for _, key := range remote.Keys() {
		if _, existsInLocal := localMap[key]; !existsInLocal {
			result.Added = append(result.Added, DiffEntry{Key: key, Value: remoteMap[key]})
		}
	}

	return result
}

// HasChanges returns true if there are any differences.
func (d *DiffResult) HasChanges() bool {
	return len(d.Added) > 0 || len(d.Removed) > 0 || len(d.Modified) > 0
}

// Summary returns a human-readable summary of changes.
func (d *DiffResult) Summary() string {
	parts := []string{}

	if len(d.Modified) > 0 {
		parts = append(parts, pluralize(len(d.Modified), "updated"))
	}
	if len(d.Added) > 0 {
		parts = append(parts, pluralize(len(d.Added), "added"))
	}
	if len(d.Removed) > 0 {
		parts = append(parts, pluralize(len(d.Removed), "removed"))
	}
	if d.UnchangedCount > 0 {
		parts = append(parts, pluralize(d.UnchangedCount, "unchanged"))
	}

	if len(parts) == 0 {
		return "no changes"
	}

	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}

func pluralize(count int, label string) string {
	if count == 1 {
		return "1 " + label
	}
	return fmt.Sprintf("%d %s", count, label)
}
