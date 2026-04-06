// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package peer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/envsync/envsync/internal/config"
	"github.com/pelletier/go-toml/v2"
)

// Registry manages the persistent store of known peers and teams.
type Registry struct {
	mu      sync.RWMutex
	baseDir string
}

// NewRegistry creates a new peer registry backed by the filesystem.
func NewRegistry() (*Registry, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil, err
	}

	baseDir := filepath.Join(dataDir, "teams")
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("creating teams directory: %w", err)
	}

	return &Registry{baseDir: baseDir}, nil
}

// SaveTeam persists a team to disk.
func (r *Registry) SaveTeam(team *Team) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	teamDir := filepath.Join(r.baseDir, team.ID)
	if err := os.MkdirAll(teamDir, 0700); err != nil {
		return fmt.Errorf("creating team dir: %w", err)
	}

	data, err := toml.Marshal(team)
	if err != nil {
		return fmt.Errorf("marshaling team: %w", err)
	}

	return os.WriteFile(filepath.Join(teamDir, "team.toml"), data, 0600)
}

// LoadTeam loads a team from disk.
func (r *Registry) LoadTeam(teamID string) (*Team, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	path := filepath.Join(r.baseDir, teamID, "team.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("team %q not found", teamID)
		}
		return nil, err
	}

	var team Team
	if err := toml.Unmarshal(data, &team); err != nil {
		return nil, fmt.Errorf("parsing team file: %w", err)
	}

	return &team, nil
}

// ListTeams returns all known team IDs.
func (r *Registry) ListTeams() ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries, err := os.ReadDir(r.baseDir)
	if err != nil {
		return nil, err
	}

	var teams []string
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(filepath.Join(r.baseDir, entry.Name(), "team.toml")); err == nil {
				teams = append(teams, entry.Name())
			}
		}
	}

	return teams, nil
}

// SavePeer persists a peer within a team.
func (r *Registry) SavePeer(teamID string, p *Peer) error {
	if err := p.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	peerDir := filepath.Join(r.baseDir, teamID, "peers")
	if err := os.MkdirAll(peerDir, 0700); err != nil {
		return fmt.Errorf("creating peers dir: %w", err)
	}

	data, err := toml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshaling peer: %w", err)
	}

	// Use a sanitized filename from fingerprint
	filename := sanitizeFingerprint(p.Fingerprint) + ".toml"
	return os.WriteFile(filepath.Join(peerDir, filename), data, 0600)
}

// LoadPeer loads a specific peer from a team.
func (r *Registry) LoadPeer(teamID, fingerprint string) (*Peer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	filename := sanitizeFingerprint(fingerprint) + ".toml"
	path := filepath.Join(r.baseDir, teamID, "peers", filename)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("peer not found in team %q", teamID)
		}
		return nil, err
	}

	var p Peer
	if err := toml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing peer file: %w", err)
	}
	p.Normalize()

	return &p, nil
}

// ListPeers returns all peers in a team.
func (r *Registry) ListPeers(teamID string) ([]Peer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	peerDir := filepath.Join(r.baseDir, teamID, "peers")
	entries, err := os.ReadDir(peerDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var peers []Peer
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(peerDir, entry.Name()))
		if err != nil {
			continue
		}

		var p Peer
		if err := toml.Unmarshal(data, &p); err != nil {
			continue
		}
		p.Normalize()
		peers = append(peers, p)
	}

	return peers, nil
}

// DeletePeer removes a peer from a team.
func (r *Registry) DeletePeer(teamID, fingerprint string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	filename := sanitizeFingerprint(fingerprint) + ".toml"
	path := filepath.Join(r.baseDir, teamID, "peers", filename)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing peer: %w", err)
	}
	return nil
}

// FindPeerByLabel searches all projects for a peer with the given display label.
func (r *Registry) FindPeerByLabel(label string) (*Peer, string, error) {
	teams, err := r.ListTeams()
	if err != nil {
		return nil, "", err
	}

	for _, teamID := range teams {
		peers, err := r.ListPeers(teamID)
		if err != nil {
			continue
		}
		for _, p := range peers {
			if p.DisplayName == label {
				return &p, teamID, nil
			}
		}
	}

	return nil, "", fmt.Errorf("member %q not found in any project", label)
}

// sanitizeFingerprint makes a fingerprint safe for use as a filename.
func sanitizeFingerprint(fp string) string {
	result := make([]byte, 0, len(fp))
	for _, b := range []byte(fp) {
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '-', b == '_':
			result = append(result, b)
		default:
			result = append(result, '_')
		}
	}
	return string(result)
}
