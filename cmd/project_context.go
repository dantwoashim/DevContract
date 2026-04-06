// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/peer"
)

type projectContext struct {
	Config    *peer.ProjectConfig
	ProjectID string
}

var errProjectConfigMissingID = errors.New("project config is missing project_id")

func loadProjectContext() (*projectContext, error) {
	pc, err := peer.LoadProjectConfig()
	if err != nil {
		return nil, err
	}

	projectID := pc.CanonicalProjectID()
	if projectID == "" {
		return nil, fmt.Errorf("%w in %s", errProjectConfigMissingID, config.ProjectConfigPath())
	}

	return &projectContext{
		Config:    pc,
		ProjectID: projectID,
	}, nil
}

func requireProjectContext() (*projectContext, error) {
	ctx, err := loadProjectContext()
	if err == nil {
		return ctx, nil
	}
	return nil, fmt.Errorf("%w\n\n  Run 'envsync init' in the project root first", err)
}

func ensureProjectContext(cfg *config.Config) (*projectContext, error) {
	if pc, err := peer.LoadProjectConfig(); err == nil {
		updated := false
		if pc.ProjectID == "" {
			projectID, idErr := peer.GenerateProjectID()
			if idErr != nil {
				return nil, idErr
			}
			pc.ProjectID = projectID
			updated = true
		}
		if cfg != nil && pc.RelayURL == "" && cfg.Relay.URL != "" {
			pc.RelayURL = cfg.Relay.URL
			updated = true
		}
		if pc.Name == "" {
			pc.Name = filepath.Base(mustGetwd())
			updated = true
		}
		if updated {
			if err := peer.SaveProjectConfig(pc); err != nil {
				return nil, err
			}
		}
		return &projectContext{
			Config:    pc,
			ProjectID: pc.CanonicalProjectID(),
		}, nil
	} else if _, findErr := config.FindProjectConfig(); findErr == nil {
		return nil, err
	}

	defaultFile := ".env"
	syncStrategy := "interactive"
	relayURL := ""
	if cfg != nil {
		if cfg.Sync.DefaultFile != "" {
			defaultFile = cfg.Sync.DefaultFile
		}
		if cfg.Sync.MergeStrategy != "" {
			syncStrategy = cfg.Sync.MergeStrategy
		}
		relayURL = cfg.Relay.URL
	}

	name := filepath.Base(mustGetwd())
	pc, err := peer.NewProjectConfig(name, defaultFile, syncStrategy, relayURL)
	if err != nil {
		return nil, err
	}
	if err := peer.SaveProjectConfig(pc); err != nil {
		return nil, err
	}

	return &projectContext{
		Config:    pc,
		ProjectID: pc.CanonicalProjectID(),
	}, nil
}

func projectTargetFile(cmdFile string, cmdFileExplicit bool, project *projectContext, cfg *config.Config) string {
	if cmdFileExplicit && cmdFile != "" {
		return cmdFile
	}
	if project != nil && project.Config != nil && project.Config.DefaultFile != "" {
		return project.Config.DefaultFile
	}
	if cfg != nil && cfg.Sync.DefaultFile != "" {
		return cfg.Sync.DefaultFile
	}
	if cmdFile != "" {
		return cmdFile
	}
	return ".env"
}

func displayMemberLabel(cfg *config.Config, kp *crypto.KeyPair) string {
	if cfg != nil && cfg.Identity.DisplayName != "" {
		return cfg.Identity.DisplayName
	}
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return hostname
	}
	if kp != nil && kp.Fingerprint != "" {
		if len(kp.Fingerprint) > 16 {
			return kp.Fingerprint[:16]
		}
		return kp.Fingerprint
	}
	return "envsync-user"
}

func ed25519PublicKeyBase64(kp *crypto.KeyPair) string {
	return base64.StdEncoding.EncodeToString(kp.Ed25519Public)
}

func x25519PublicKeyBase64(kp *crypto.KeyPair) string {
	return base64.StdEncoding.EncodeToString(kp.X25519Public[:])
}

func defaultTeam(name, projectID, creatorFingerprint string) *peer.Team {
	return &peer.Team{
		ID:        projectID,
		Name:      name,
		CreatedBy: creatorFingerprint,
		CreatedAt: time.Now(),
		Members:   []string{creatorFingerprint},
	}
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "envsync-project"
	}
	return wd
}
