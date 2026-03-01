// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize EnvSync (reads SSH key, creates config)",
	Long:  "Reads your SSH Ed25519 key, derives cryptographic identity, and creates the EnvSync configuration.",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	cfg := config.Default()

	fmt.Println()
	fmt.Printf("  ✦ EnvSync %s\n", Version)
	fmt.Println()

	// Resolve SSH key path
	keyPath := cfg.Identity.SSHKeyPath
	if envFile, _ := cmd.Flags().GetString("ssh-key"); envFile != "" {
		keyPath = envFile
	}

	// Expand ~ if needed
	if len(keyPath) >= 2 && (keyPath[:2] == "~/" || keyPath[:2] == "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		keyPath = filepath.Join(home, keyPath[2:])
	}

	fmt.Printf("  ▸ Reading SSH key from %s\n", keyPath)

	// Check if key exists
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return fmt.Errorf("SSH key not found at %s\n\n"+
			"  Generate one with:\n"+
			"    ssh-keygen -t ed25519 -f %s\n", keyPath, keyPath)
	}

	// Check for passphrase before loading
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("reading SSH key: %w", err)
	}

	if crypto.IsPassphraseProtected(keyData) {
		fmt.Println()
		fmt.Println("  ⚠ SSH key is passphrase-protected.")
		fmt.Println("    EnvSync needs access to the raw key for encryption.")
		fmt.Println("    Options:")
		fmt.Printf("    1. Remove passphrase: ssh-keygen -p -f %s\n", keyPath)
		fmt.Println("    2. Use ssh-agent (EnvSync will read from agent)")
		fmt.Println()
		return fmt.Errorf("passphrase-protected SSH keys are not yet supported")
	}

	// Load and derive keys
	kp, err := crypto.LoadSSHKey(keyPath)
	if err != nil {
		return err
	}

	fmt.Printf("  ▸ Key type: Ed25519\n")
	fmt.Printf("  ▸ Fingerprint: %s\n", kp.Fingerprint)

	// Update config
	cfg.Identity.SSHKeyPath = keyPath
	cfg.Identity.Fingerprint = kp.Fingerprint

	// Create directories
	if err := config.EnsureDirs(); err != nil {
		return fmt.Errorf("creating EnvSync directories: %w", err)
	}

	configDir, err := config.ConfigDir()
	if err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "config.toml")

	// Actually write the config file to disk
	if err := config.SaveConfig(cfg); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	fmt.Printf("  ▸ Created config at %s\n", configPath)

	dataDir, err := config.DataDir()
	if err != nil {
		return err
	}
	fmt.Printf("  ▸ Encrypted store initialized at %s/store/\n", dataDir)

	fmt.Println()
	fmt.Println("  ✓ Ready. Run 'envsync invite @teammate' to start a team.")

	return nil
}

func init() {
	initCmd.Flags().String("ssh-key", "", "Path to SSH private key (default: ~/.ssh/id_ed25519)")
	rootCmd.AddCommand(initCmd)
}
