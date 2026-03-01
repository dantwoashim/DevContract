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
	Short: "Push .env to team peers",
	Long: `Reads the .env file and sends it to all discovered peers.

Falls back through: LAN direct → hole-punch → encrypted relay.`,
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
			Message:    "Config not found",
			Cause:      err.Error(),
			Suggestion: "Run 'envsync init' first",
		})
		return nil
	}

	noiseKP := crypto.NewNoiseKeypair(kp.X25519Private, kp.X25519Public)
	targetFile, _ := cmd.Flags().GetString("file")

	// Create relay client for fallback
	relayClient := relay.NewClient(cfg.Relay.URL, kp)
	teamID := generateTeamID(kp.Fingerprint)

	ui.Header("EnvSync Push")

	// Load next sequence number from version store (monotonically increasing)
	seq := int64(1)
	if vs, storeErr := store.New(50); storeErr == nil {
		versions, listErr := vs.List(teamID)
		if listErr == nil && len(versions) > 0 {
			seq = int64(versions[0].Sequence) + 1
		}
	}

	result := envsync.Orchestrate(context.Background(), envsync.OrchestratorOptions{
		EnvFilePath:  targetFile,
		TeamID:       teamID,
		KeyPair:      kp,
		NoiseKeypair: noiseKP,
		RelayClient:  relayClient,
		RelayURL:     cfg.Relay.URL,
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
			Suggestion: "Check network connectivity or try '--relay' flag",
		})
		return nil
	}

	ui.Success(fmt.Sprintf("Pushed to %d/%d peers via %s (%s)",
		result.SyncedCount, result.PeerCount, result.Method, result.Duration.Truncate(1e6)))

	// Write audit entry
	logger, logErr := audit.NewLogger()
	if logErr == nil {
		logger.Log(audit.Entry{
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
