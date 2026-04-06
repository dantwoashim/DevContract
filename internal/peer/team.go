// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package peer

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/fsutil"
	toml "github.com/pelletier/go-toml/v2"
)

const (
	defaultProjectFile  = ".env"
	defaultSyncStrategy = "interactive"
)

// GenerateTeamID creates a deterministic legacy team ID from creator fingerprint + name.
// New projects should prefer GenerateProjectID and persist it in .envsync.toml.
func GenerateTeamID(creatorFingerprint, name string) string {
	h := sha256.New()
	h.Write([]byte(creatorFingerprint))
	h.Write([]byte(":"))
	h.Write([]byte(name))
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// GenerateProjectID creates a stable random project identifier for a repository.
func GenerateProjectID() (string, error) {
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generating project ID: %w", err)
	}
	return hex.EncodeToString(secret), nil
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
	ConfigVersion int    `toml:"config_version,omitempty"`
	ProjectID     string `toml:"project_id,omitempty"`
	LegacyTeamID  string `toml:"team_id,omitempty"`
	Name          string `toml:"name,omitempty"`
	DefaultFile   string `toml:"default_file,omitempty"`
	SyncStrategy  string `toml:"sync_strategy,omitempty"`
	RelayURL      string `toml:"relay_url,omitempty"`
}

// CanonicalProjectID returns the single project/team ID used throughout the CLI.
func (pc *ProjectConfig) CanonicalProjectID() string {
	if pc == nil {
		return ""
	}
	if pc.ProjectID != "" {
		return pc.ProjectID
	}
	return pc.LegacyTeamID
}

// Normalize backfills defaults and migrates legacy team_id data into project_id.
func (pc *ProjectConfig) Normalize() {
	if pc == nil {
		return
	}
	if pc.ProjectID == "" {
		pc.ProjectID = pc.LegacyTeamID
	}
	pc.LegacyTeamID = ""
	if pc.DefaultFile == "" {
		pc.DefaultFile = defaultProjectFile
	}
	if pc.SyncStrategy == "" {
		pc.SyncStrategy = defaultSyncStrategy
	}
	if pc.ConfigVersion == 0 {
		pc.ConfigVersion = 1
	}
}

// NewProjectConfig creates a fresh per-project config with a stable project ID.
func NewProjectConfig(name, defaultFile, syncStrategy, relayURL string) (*ProjectConfig, error) {
	projectID, err := GenerateProjectID()
	if err != nil {
		return nil, err
	}

	pc := &ProjectConfig{
		ConfigVersion: 1,
		ProjectID:     projectID,
		Name:          name,
		DefaultFile:   defaultFile,
		SyncStrategy:  syncStrategy,
		RelayURL:      relayURL,
	}
	pc.Normalize()
	return pc, nil
}

func loadProjectConfigFromPath(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading project config: %w", err)
	}

	var pc ProjectConfig
	if err := toml.Unmarshal(data, &pc); err != nil {
		return nil, fmt.Errorf("parsing project config: %w", err)
	}
	pc.Normalize()
	return &pc, nil
}

// LoadProjectConfig reads the .envsync.toml from the current (or parent) directory.
func LoadProjectConfig() (*ProjectConfig, error) {
	path, err := config.FindProjectConfig()
	if err != nil {
		return nil, err
	}
	return loadProjectConfigFromPath(path)
}

func projectConfigWritePath() string {
	if path, err := config.FindProjectConfig(); err == nil {
		return path
	}
	return config.ProjectConfigPath()
}

// SaveProjectConfig writes .envsync.toml in the current directory.
func SaveProjectConfig(pc *ProjectConfig) error {
	pc.Normalize()
	data, err := toml.Marshal(pc)
	if err != nil {
		return fmt.Errorf("encoding project config: %w", err)
	}
	return fsutil.AtomicWriteFile(projectConfigWritePath(), data, 0600)
}
