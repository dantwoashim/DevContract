// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/dantwoashim/Env_sync/internal/audit"
	"github.com/dantwoashim/Env_sync/internal/crypto"
	"github.com/dantwoashim/Env_sync/internal/relay"
	envsync "github.com/dantwoashim/Env_sync/internal/sync"
	"github.com/dantwoashim/Env_sync/internal/ui"
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
	backupKey, err := atRestKey(kp)
	if err != nil {
		return err
	}
	lineage, err := resolveCurrentRevision(project.ProjectID, targetFile, backupKey)
	if err != nil {
		return err
	}

	relayClient := relay.NewClient(projectRelayURL(project, cfg), kp)

	ui.Header("EnvSync Push")

	result := envsync.Orchestrate(context.Background(), envsync.OrchestratorOptions{
		EnvFilePath:         targetFile,
		TeamID:              project.ProjectID,
		KeyPair:             kp,
		NoiseKeypair:        noiseKP,
		RelayClient:         relayClient,
		RelayURL:            projectRelayURL(project, cfg),
		Sequence:            time.Now().UnixMilli(),
		BaseRevisionID:      lineage.BaseRevisionID,
		RevisionID:          lineage.RevisionID,
		AncestorRevisionIDs: lineage.AncestorRevisionIDs,
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

	switch {
	case result.DeliveredCount > 0 && result.QueuedCount > 0:
		ui.Success(fmt.Sprintf("Delivered to %d peer(s) and queued %d relay fallback delivery(ies) via %s (%s)",
			result.DeliveredCount, result.QueuedCount, result.Method, result.Duration.Truncate(time.Millisecond)))
	case result.DeliveredCount > 0:
		ui.Success(fmt.Sprintf("Delivered to %d/%d peer(s) via %s (%s)",
			result.DeliveredCount, result.PeerCount, result.Method, result.Duration.Truncate(time.Millisecond)))
	case result.QueuedCount > 0:
		ui.Success(fmt.Sprintf("Queued encrypted relay delivery for %d/%d peer(s) (%s)",
			result.QueuedCount, result.PeerCount, result.Duration.Truncate(time.Millisecond)))
	}

	logger, logErr := audit.NewLogger()
	if logErr == nil {
		_ = logger.Log(audit.Entry{
			Event:         audit.EventPush,
			File:          targetFile,
			DeliveryCount: result.DeliveredCount + result.QueuedCount,
			Method:        result.Method,
			Details:       fmt.Sprintf("%d peers (%d delivered, %d queued), %s", result.PeerCount, result.DeliveredCount, result.QueuedCount, result.Duration.Truncate(time.Millisecond)),
		})
	}

	ui.Blank()
	return nil
}

func init() {
	rootCmd.AddCommand(pushCmd)
}
