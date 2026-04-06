// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package peer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/fsutil"
	"github.com/pelletier/go-toml/v2"
)

// Registry manages the persistent store of known peers and teams.
type Registry struct {
	mu      sync.RWMutex
	baseDir string
}

type IntegrityIssue struct {
	Path   string
	Detail string
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

	return fsutil.AtomicWriteFile(filepath.Join(teamDir, "team.toml"), data, 0600)
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

	filename := sanitizeFingerprint(p.Fingerprint) + ".toml"
	return fsutil.AtomicWriteFile(filepath.Join(peerDir, filename), data, 0600)
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
	if err := p.Validate(); err != nil {
		return nil, err
	}

	return &p, nil
}

// ListPeers returns all peers in a team and fails if any peer file is unreadable or malformed.
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

		path := filepath.Join(peerDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading peer file %s: %w", path, err)
		}

		var p Peer
		if err := toml.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("parsing peer file %s: %w", path, err)
		}
		p.Normalize()
		if err := p.Validate(); err != nil {
			return nil, fmt.Errorf("validating peer file %s: %w", path, err)
		}
		peers = append(peers, p)
	}

	return peers, nil
}

// ScanIntegrity reports unreadable or malformed registry files without loading them into active state.
func (r *Registry) ScanIntegrity(teamID string) ([]IntegrityIssue, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var issues []IntegrityIssue
	paths := []string{filepath.Join(r.baseDir, teamID, "team.toml")}
	peerDir := filepath.Join(r.baseDir, teamID, "peers")
	entries, err := os.ReadDir(peerDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
				continue
			}
			paths = append(paths, filepath.Join(peerDir, entry.Name()))
		}
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			issues = append(issues, IntegrityIssue{Path: path, Detail: err.Error()})
			continue
		}

		if strings.HasSuffix(path, "team.toml") {
			var team Team
			if err := toml.Unmarshal(data, &team); err != nil {
				issues = append(issues, IntegrityIssue{Path: path, Detail: fmt.Sprintf("invalid team metadata: %v", err)})
			}
			continue
		}

		var p Peer
		if err := toml.Unmarshal(data, &p); err != nil {
			issues = append(issues, IntegrityIssue{Path: path, Detail: fmt.Sprintf("invalid peer file: %v", err)})
			continue
		}
		p.Normalize()
		if err := p.Validate(); err != nil {
			issues = append(issues, IntegrityIssue{Path: path, Detail: fmt.Sprintf("invalid peer metadata: %v", err)})
		}
	}

	return issues, nil
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

	var match *Peer
	var matchTeam string
	for _, teamID := range teams {
		peer, err := r.FindPeerInTeam(teamID, label)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				continue
			}
			return nil, "", err
		}
		if match != nil {
			return nil, "", fmt.Errorf("member %q is ambiguous across projects; use the fingerprint instead", label)
		}
		match = peer
		matchTeam = teamID
	}

	if match == nil {
		return nil, "", fmt.Errorf("member %q not found in any project", label)
	}
	return match, matchTeam, nil
}

// FindPeerInTeam resolves a member by exact fingerprint, relay username, or display label within one project.
func (r *Registry) FindPeerInTeam(teamID, selector string) (*Peer, error) {
	if strings.HasPrefix(selector, "SHA256:") {
		return r.LoadPeer(teamID, selector)
	}

	peers, err := r.ListPeers(teamID)
	if err != nil {
		return nil, err
	}

	var matches []Peer
	for _, p := range peers {
		if p.DisplayName == selector || p.RelayUsername == selector {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("member %q not found in project %s", selector, teamID)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("member %q is ambiguous in project %s; use the fingerprint", selector, teamID)
	}
	return &matches[0], nil
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
