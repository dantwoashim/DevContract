// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"

	"github.com/envsync/envsync/internal/audit"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/relay"
	"github.com/envsync/envsync/internal/store"
	envsync "github.com/envsync/envsync/internal/sync"
	"github.com/envsync/envsync/internal/ui"
	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push .env to project peers",
	Long: `Reads the project .env file and sends it to discovered peers.

Fallback order: LAN direct -> encrypted relay.`,
	RunE: runPush,
}

func runPush(cmd *cobra.Command, args []string) error {
	kp, err := loadIdentity()
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		ui.RenderError(ui.StructuredError{
			Category:   ui.ErrConfig,
			Message:    "Config not found or corrupted",
			Cause:      err.Error(),
			Suggestion: "Run 'envsync init' first",
		})
		return fmt.Errorf("config: %w", err)
	}

	if cfg.Identity.Fingerprint == "" {
		ui.RenderError(ui.StructuredError{
			Category:   ui.ErrConfig,
			Message:    "Not initialized",
			Cause:      "No identity configured",
			Suggestion: "Run 'envsync init' to set up your identity",
		})
		return fmt.Errorf("not initialized: run 'envsync init' first")
	}

	project, err := requireProjectContext()
	if err != nil {
		return err
	}

	noiseKP := crypto.NewNoiseKeypair(kp.X25519Private, kp.X25519Public)
	targetFile, _ := cmd.Flags().GetString("file")
	targetFile = projectTargetFile(targetFile, cmd.Flags().Changed("file"), project, cfg)

	relayClient := relay.NewClient(projectRelayURL(project, cfg), kp)

	ui.Header("EnvSync Push")

	seq := int64(1)
	if vs, storeErr := store.New(cfg.Sync.MaxVersions); storeErr == nil {
		next, nextErr := vs.NextSequence(project.ProjectID)
		if nextErr == nil {
			seq = int64(next)
		}
	}

	result := envsync.Orchestrate(context.Background(), envsync.OrchestratorOptions{
		EnvFilePath:  targetFile,
		TeamID:       project.ProjectID,
		KeyPair:      kp,
		NoiseKeypair: noiseKP,
		RelayClient:  relayClient,
		RelayURL:     projectRelayURL(project, cfg),
		Sequence:     seq,
		OnStatus: func(status string) {
			ui.Line(fmt.Sprintf("  %s", status))
		},
	})

	ui.Blank()

	if result.Error != nil {
		ui.RenderError(ui.StructuredError{
			Category:   ui.ErrSync,
			Message:    "Push failed",
			Cause:      result.Error.Error(),
			Suggestion: "Check relay connectivity or confirm peers have joined this project",
		})
		return result.Error
	}

	ui.Success(fmt.Sprintf("Pushed to %d/%d peers via %s (%s)",
		result.SyncedCount, result.PeerCount, result.Method, result.Duration.Truncate(1e6)))

	logger, logErr := audit.NewLogger()
	if logErr == nil {
		_ = logger.Log(audit.Entry{
			Event:       audit.EventPush,
			File:        targetFile,
			VarsChanged: result.SyncedCount,
			Method:      result.Method,
			Details:     fmt.Sprintf("%d peers, %s", result.PeerCount, result.Duration.Truncate(1e6)),
		})
	}

	ui.Blank()
	return nil
}

func init() {
	rootCmd.AddCommand(pushCmd)
}
