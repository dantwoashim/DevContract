// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/ui"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config [key] [value]",
	Short: "View or modify configuration",
	Long: `View or modify EnvSync configuration.

  envsync config                    Show all settings
  envsync config relay.url          Show a specific setting
  envsync config relay.url <value>  Set a specific value`,
	RunE: runConfig,
}

func runConfig(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	switch len(args) {
	case 0:
		// Show all
		return showAllConfig(cfg)
	case 1:
		// Show specific
		return showConfigKey(cfg, args[0])
	case 2:
		// Set
		return setConfigKey(cfg, args[0], args[1])
	default:
		return fmt.Errorf("too many arguments — use: envsync config [key] [value]")
	}
}

func showAllConfig(cfg *config.Config) error {
	ui.Header("Configuration")

	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			fmt.Printf("  %s\n", line)
		}
	}
	ui.Blank()

	path, _ := config.ConfigFilePath()
	ui.Line(fmt.Sprintf("  Config file: %s", path))
	ui.Blank()
	return nil
}

func showConfigKey(cfg *config.Config, key string) error {
	val, err := getConfigValue(cfg, key)
	if err != nil {
		return err
	}
	fmt.Println(val)
	return nil
}

func setConfigKey(cfg *config.Config, key, value string) error {
	if err := setConfigValue(cfg, key, value); err != nil {
		return err
	}

	if err := config.SaveConfig(cfg); err != nil {
		return err
	}

	ui.Success(fmt.Sprintf("Set %s = %s", key, value))
	return nil
}

func getConfigValue(cfg *config.Config, key string) (string, error) {
	switch key {
	case "identity.ssh_key_path":
		return cfg.Identity.SSHKeyPath, nil
	case "identity.display_name", "identity.github_username":
		return cfg.Identity.DisplayName, nil
	case "identity.fingerprint":
		return cfg.Identity.Fingerprint, nil
	case "relay.url":
		return cfg.Relay.URL, nil
	case "relay.timeout_seconds":
		return fmt.Sprintf("%d", cfg.Relay.TimeoutSeconds), nil
	case "network.listen_port":
		return fmt.Sprintf("%d", cfg.Network.ListenPort), nil
	case "network.mdns_enabled":
		return fmt.Sprintf("%t", cfg.Network.MDNSEnabled), nil
	case "network.holepunch_enabled":
		return fmt.Sprintf("%t", cfg.Network.HolePunchEnabled), nil
	case "sync.default_file":
		return cfg.Sync.DefaultFile, nil
	case "sync.merge_strategy":
		return cfg.Sync.MergeStrategy, nil
	case "sync.max_versions":
		return fmt.Sprintf("%d", cfg.Sync.MaxVersions), nil
	case "sync.auto_backup":
		return fmt.Sprintf("%t", cfg.Sync.AutoBackup), nil
	case "sync.confirm_before_apply":
		return fmt.Sprintf("%t", cfg.Sync.ConfirmBeforeApply), nil
	case "ui.color":
		return fmt.Sprintf("%t", cfg.UI.Color), nil
	case "ui.verbose":
		return fmt.Sprintf("%t", cfg.UI.Verbose), nil
	default:
		return "", fmt.Errorf("unknown config key: %q\n\nAvailable keys: identity.ssh_key_path, identity.display_name, relay.url, network.listen_port, sync.merge_strategy, ui.color, ...", key)
	}
}

func setConfigValue(cfg *config.Config, key, value string) error {
	switch key {
	case "identity.ssh_key_path":
		if _, err := os.Stat(value); err != nil {
			ui.Warning(fmt.Sprintf("File %q does not exist", value))
		}
		cfg.Identity.SSHKeyPath = value
	case "identity.display_name", "identity.github_username":
		cfg.Identity.DisplayName = value
	case "relay.url":
		cfg.Relay.URL = value
	case "relay.timeout_seconds":
		var v int
		if _, err := fmt.Sscanf(value, "%d", &v); err != nil {
			return fmt.Errorf("invalid integer: %q", value)
		}
		cfg.Relay.TimeoutSeconds = v
	case "network.listen_port":
		var v int
		if _, err := fmt.Sscanf(value, "%d", &v); err != nil {
			return fmt.Errorf("invalid integer: %q", value)
		}
		cfg.Network.ListenPort = v
	case "network.mdns_enabled":
		cfg.Network.MDNSEnabled = value == "true"
	case "network.holepunch_enabled":
		cfg.Network.HolePunchEnabled = value == "true"
	case "sync.default_file":
		cfg.Sync.DefaultFile = value
	case "sync.merge_strategy":
		cfg.Sync.MergeStrategy = value
	case "sync.max_versions":
		var v int
		if _, err := fmt.Sscanf(value, "%d", &v); err != nil {
			return fmt.Errorf("invalid integer: %q", value)
		}
		cfg.Sync.MaxVersions = v
	case "sync.auto_backup":
		cfg.Sync.AutoBackup = value == "true"
	case "sync.confirm_before_apply":
		cfg.Sync.ConfirmBeforeApply = value == "true"
	case "ui.color":
		cfg.UI.Color = value == "true"
	case "ui.verbose":
		cfg.UI.Verbose = value == "true"
	default:
		return fmt.Errorf("unknown config key: %q", key)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(configCmd)
}
