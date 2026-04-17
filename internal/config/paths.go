// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigFilePath returns the full path to the config TOML file.
func ConfigFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// PeersDir returns the directory for peer registry files.
func PeersDir(teamID string) (string, error) {
	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "teams", teamID, "peers"), nil
}

// TeamFilePath returns the path for a team's metadata file.
func TeamFilePath(teamID string) (string, error) {
	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "teams", teamID, "team.toml"), nil
}

// StoreDir returns the version store directory for a project.
func StoreDir(projectHash string) (string, error) {
	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "store", projectHash), nil
}

// AuditLogPath returns the path to the audit log file.
func AuditLogPath() (string, error) {
	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "audit.jsonl"), nil
}

// ProjectConfigPath returns the path to the per-project config file.
func ProjectConfigPath() string {
	return ".devcontract.toml"
}

// ContractDirPath returns the path to the repo-owned contract directory.
func ContractDirPath() string {
	return ".devcontract"
}

// ContractFilePath returns the path to the repo-owned contract file.
func ContractFilePath() string {
	return filepath.Join(ContractDirPath(), "contract.yaml")
}

func findUp(relativePath string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		candidate := filepath.Join(dir, relativePath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("%s not found", relativePath)
}

// FindProjectConfig searches up from cwd to find .devcontract.toml.
func FindProjectConfig() (string, error) {
	path, err := findUp(ProjectConfigPath())
	if err != nil {
		return "", fmt.Errorf(".devcontract.toml not found (run 'devcontract init' to create one)")
	}
	return path, nil
}

// FindContractFile searches up from cwd to find .devcontract/contract.yaml.
func FindContractFile() (string, error) {
	path, err := findUp(ContractFilePath())
	if err != nil {
		return "", fmt.Errorf("%s not found (run 'devcontract init' to create one)", ContractFilePath())
	}
	return path, nil
}

// FindProjectRoot resolves the repository root using either project config or contract metadata.
func FindProjectRoot() (string, error) {
	if projectPath, err := FindProjectConfig(); err == nil {
		return filepath.Dir(projectPath), nil
	}

	contractPath, err := FindContractFile()
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(contractPath)), nil
}
