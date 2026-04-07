// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dantwoashim/Env_sync/internal/config"
	"github.com/dantwoashim/Env_sync/internal/fsutil"
	"github.com/dantwoashim/Env_sync/internal/ui"
)

type contractTrustEntry struct {
	ContractHash string    `json:"contract_hash"`
	TrustedAt    time.Time `json:"trusted_at"`
}

type contractTrustStore map[string]contractTrustEntry

func ensureContractTrust(ctx *contractContext, commands []string, trustFlag bool) error {
	if len(commands) == 0 {
		return nil
	}

	contractHash, err := contractHash(ctx.Path)
	if err != nil {
		return err
	}
	trusted, err := isContractTrusted(ctx.Root, contractHash)
	if err != nil {
		return err
	}
	if trusted {
		return nil
	}
	if trustFlag {
		return persistContractTrust(ctx.Root, contractHash)
	}

	ui.Warning("This repo contract can execute shell commands on your machine.")
	ui.Line(fmt.Sprintf("  Contract: %s", filepath.ToSlash(ctx.Path)))
	ui.Line("  Commands:")
	for _, command := range commands {
		ui.Line(fmt.Sprintf("    - %s", command))
	}
	ui.Blank()

	if !ui.ConfirmAction("Trust this contract and continue?", false) {
		return fmt.Errorf("contract not trusted; re-run with --trust-contract or --restricted")
	}
	return persistContractTrust(ctx.Root, contractHash)
}

func contractTrustedForContext(ctx *contractContext) (bool, error) {
	hash, err := contractHash(ctx.Path)
	if err != nil {
		return false, err
	}
	return isContractTrusted(ctx.Root, hash)
}

func contractHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading contract for trust verification: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func isContractTrusted(root, hash string) (bool, error) {
	store, err := loadContractTrustStore()
	if err != nil {
		return false, err
	}
	entry, ok := store[root]
	return ok && entry.ContractHash == hash, nil
}

func persistContractTrust(root, hash string) error {
	store, err := loadContractTrustStore()
	if err != nil {
		return err
	}
	store[root] = contractTrustEntry{
		ContractHash: hash,
		TrustedAt:    time.Now().UTC(),
	}
	return saveContractTrustStore(store)
}

func loadContractTrustStore() (contractTrustStore, error) {
	path, err := contractTrustFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return contractTrustStore{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading contract trust store: %w", err)
	}

	var store contractTrustStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parsing contract trust store: %w", err)
	}
	return store, nil
}

func saveContractTrustStore(store contractTrustStore) error {
	path, err := contractTrustFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating contract trust directory: %w", err)
	}

	encoded, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding contract trust store: %w", err)
	}
	return fsutil.AtomicWriteFile(path, encoded, 0600)
}

func contractTrustFilePath() (string, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "contract-trust.json"), nil
}
