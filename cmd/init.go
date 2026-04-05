// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize EnvSync for the current project",
	Long:  "Reads your SSH Ed25519 key, derives the EnvSync identity bundle, writes config, creates a stable project ID, and scaffolds a repo contract.",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	cfg := config.Default()

	fmt.Println()
	fmt.Printf("  * EnvSync %s\n", Version)
	fmt.Println()

	keyPath := cfg.Identity.SSHKeyPath
	if flagValue, _ := cmd.Flags().GetString("ssh-key"); flagValue != "" {
		keyPath = flagValue
	}

	if len(keyPath) >= 2 && (keyPath[:2] == "~/" || keyPath[:2] == "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		keyPath = filepath.Join(home, keyPath[2:])
	}

	fmt.Printf("  - Reading SSH key from %s\n", keyPath)

	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return fmt.Errorf("SSH key not found at %s\n\n  Generate one with:\n    ssh-keygen -t ed25519 -f %s", keyPath, keyPath)
	}

	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("reading SSH key: %w", err)
	}

	if crypto.IsPassphraseProtected(keyData) {
		fmt.Println()
		fmt.Println("  ! SSH key is passphrase-protected.")
		fmt.Println("    This build of EnvSync still needs direct access to the raw Ed25519 key material.")
		fmt.Println("    Options:")
		fmt.Printf("    1. Remove passphrase: ssh-keygen -p -f %s\n", keyPath)
		fmt.Println("    2. Use a dedicated unencrypted Ed25519 key just for EnvSync")
		fmt.Println()
		return fmt.Errorf("passphrase-protected SSH keys are not yet supported in this build")
	}

	kp, err := crypto.LoadSSHKey(keyPath)
	if err != nil {
		return err
	}

	hostname, _ := os.Hostname()

	cfg.Identity.SSHKeyPath = keyPath
	cfg.Identity.Fingerprint = kp.Fingerprint
	cfg.Identity.IdentityPublicKey = base64.StdEncoding.EncodeToString(kp.Ed25519Public)
	cfg.Identity.TransportPublicKey = x25519PublicKeyBase64(kp)
	cfg.Identity.TransportFingerprint = crypto.ComputeFingerprint(kp.X25519Public)
	cfg.Identity.DeviceName = hostname
	cfg.Network.HolePunchEnabled = false

	if err := config.EnsureDirs(); err != nil {
		return fmt.Errorf("creating EnvSync directories: %w", err)
	}

	if err := config.SaveConfig(cfg); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	project, err := ensureProjectContext(cfg)
	if err != nil {
		return fmt.Errorf("writing project config: %w", err)
	}

	contractPath, createdContract, err := ensureProjectContract(project)
	if err != nil {
		return fmt.Errorf("writing repo contract: %w", err)
	}

	configPath, _ := config.ConfigFilePath()
	dataDir, _ := config.DataDir()

	fmt.Printf("  - Fingerprint: %s\n", kp.Fingerprint)
	fmt.Printf("  - Transport:   %s\n", cfg.Identity.TransportFingerprint)
	fmt.Printf("  - Device:      %s\n", cfg.Identity.DeviceName)
	fmt.Printf("  - Config:      %s\n", configPath)
	fmt.Printf("  - Project ID:  %s\n", project.ProjectID)
	fmt.Printf("  - Contract:    %s\n", contractPath)
	fmt.Printf("  - Store:       %s\\store\\\n", dataDir)
	fmt.Println()
	if createdContract {
		fmt.Println("  - Created a starter AI onboarding contract at .envsync/contract.yaml")
	}
	fmt.Println("  x Ready. Run 'envsync bootstrap' to scaffold local onboarding, then invite teammates.")

	return nil
}

func init() {
	initCmd.Flags().String("ssh-key", "", "Path to SSH private key (default: ~/.ssh/id_ed25519)")
	rootCmd.AddCommand(initCmd)
}
