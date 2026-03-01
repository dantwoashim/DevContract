// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package peer

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/envsync/envsync/internal/config"
	toml "github.com/pelletier/go-toml/v2"
)

// GenerateTeamID creates a team ID from creator fingerprint + name + random secret.
// The secret prevents team IDs from being computable from public fingerprints.
func GenerateTeamID(creatorFingerprint, name string) string {
	// Generate a random component so team IDs aren't guessable
	secret := make([]byte, 16)
	rand.Read(secret) // crypto/rand — if this fails we get zeros (still mixed in)

	h := sha256.New()
	h.Write([]byte(creatorFingerprint))
	h.Write([]byte(":"))
	h.Write([]byte(name))
	h.Write([]byte(":"))
	h.Write(secret)
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// CreateTeam creates a new team and saves it.
func CreateTeam(name, creatorFingerprint string) (*Team, error) {
	team := &Team{
		ID:        GenerateTeamID(creatorFingerprint, name),
		Name:      name,
		CreatedBy: creatorFingerprint,
		CreatedAt: time.Now(),
		Members:   []string{creatorFingerprint},
	}

	if err := SaveTeam(team); err != nil {
		return nil, err
	}

	return team, nil
}

// SaveTeam writes team metadata to disk.
func SaveTeam(team *Team) error {
	path, err := config.TeamFilePath(team.ID)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating team directory: %w", err)
	}

	data, err := toml.Marshal(team)
	if err != nil {
		return fmt.Errorf("encoding team: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}

// LoadTeam reads team metadata from disk.
func LoadTeam(teamID string) (*Team, error) {
	path, err := config.TeamFilePath(teamID)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("team %s not found", teamID)
		}
		return nil, fmt.Errorf("reading team: %w", err)
	}

	var team Team
	if err := toml.Unmarshal(data, &team); err != nil {
		return nil, fmt.Errorf("parsing team: %w", err)
	}

	return &team, nil
}

// AddMember adds a fingerprint to the team's member list.
func (t *Team) AddMember(fingerprint string) {
	for _, m := range t.Members {
		if m == fingerprint {
			return
		}
	}
	t.Members = append(t.Members, fingerprint)
}

// RemoveMember removes a fingerprint from the team's member list.
func (t *Team) RemoveMember(fingerprint string) {
	filtered := t.Members[:0]
	for _, m := range t.Members {
		if m != fingerprint {
			filtered = append(filtered, m)
		}
	}
	t.Members = filtered
}

// HasMember checks if a fingerprint is in the team.
func (t *Team) HasMember(fingerprint string) bool {
	for _, m := range t.Members {
		if m == fingerprint {
			return true
		}
	}
	return false
}

// ProjectConfig represents the per-project .envsync.toml file.
type ProjectConfig struct {
	TeamID       string `toml:"team_id"`
	DefaultFile  string `toml:"default_file"`
	SyncStrategy string `toml:"sync_strategy"`
}

// LoadProjectConfig reads the .envsync.toml from the current (or parent) directory.
func LoadProjectConfig() (*ProjectConfig, error) {
	path, err := config.FindProjectConfig()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading project config: %w", err)
	}

	var pc ProjectConfig
	if err := toml.Unmarshal(data, &pc); err != nil {
		return nil, fmt.Errorf("parsing project config: %w", err)
	}

	return &pc, nil
}

// SaveProjectConfig writes .envsync.toml in the current directory.
func SaveProjectConfig(pc *ProjectConfig) error {
	data, err := toml.Marshal(pc)
	if err != nil {
		return fmt.Errorf("encoding project config: %w", err)
	}
	return os.WriteFile(config.ProjectConfigPath(), data, 0600)
}
