// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dantwoashim/Env_sync/internal/apply"
	"github.com/dantwoashim/Env_sync/internal/audit"
	"github.com/dantwoashim/Env_sync/internal/config"
	"github.com/dantwoashim/Env_sync/internal/crypto"
	"github.com/dantwoashim/Env_sync/internal/envfile"
	envsync "github.com/dantwoashim/Env_sync/internal/sync"
	"github.com/dantwoashim/Env_sync/internal/ui"
	"github.com/spf13/cobra"
)

var (
	pullTimeoutSeconds int
	pullServiceKeyPath string
	pullProjectID      string
	pullRelayURL       string
	pullJSON           bool
	pullNonInteractive bool
	pullConflictPolicy string
)

type pullReport struct {
	ProjectID                string   `json:"project_id"`
	TargetFile               string   `json:"target_file"`
	RelayChecked             bool     `json:"relay_checked"`
	RelayAttempted           bool     `json:"relay_attempted"`
	RelayUnavailable         bool     `json:"relay_unavailable"`
	RelayPendingFound        int      `json:"relay_pending_found"`
	RelayHandled             int      `json:"relay_handled"`
	RelayApplied             int      `json:"relay_applied"`
	LANAttempted             bool     `json:"lan_attempted"`
	LANApplied               bool     `json:"lan_applied"`
	Method                   string   `json:"method"`
	Methods                  []string `json:"methods,omitempty"`
	Warnings                 []string `json:"warnings,omitempty"`
	InteractiveRequired      bool     `json:"interactive_required"`
	ManualInterventionNeeded bool     `json:"manual_intervention_required"`
	ConflictPolicyApplied    string   `json:"conflict_policy_applied,omitempty"`
	BackupCreated            bool     `json:"backup_created"`
}

type relayPullSummary struct {
	FoundCount               int
	HandledCount             int
	AppliedCount             int
	Warnings                 []string
	InteractiveRequired      bool
	ManualInterventionNeeded bool
	ConflictPolicyApplied    string
	BackupCreated            bool
}

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull .env from project peers",
	Long: `Checks the relay for pending encrypted blobs, then listens for LAN pushes.

Priority: encrypted relay first -> LAN direct.`,
	RunE: runPull,
}

func runPull(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	var kp *crypto.KeyPair
	if pullServiceKeyPath != "" {
		kp, err = loadIdentityFromServiceKey(pullServiceKeyPath)
	} else {
		kp, err = loadIdentity()
	}
	if err != nil {
		return err
	}

	if pullServiceKeyPath == "" && cfg.Identity.Fingerprint == "" {
		ui.RenderError(ui.StructuredError{
			Category:   ui.ErrConfig,
			Message:    "Not initialized",
			Cause:      "No identity configured",
			Suggestion: "Run 'envsync init' to set up your identity",
		})
		return fmt.Errorf("not initialized: run 'envsync init' first")
	}

	project, _ := loadProjectContext()
	if pullProjectID != "" {
		if project == nil {
			project = &projectContext{Config: nil, ProjectID: pullProjectID}
		} else {
			project.ProjectID = pullProjectID
		}
	}
	if project == nil || project.ProjectID == "" {
		return fmt.Errorf("project ID is not configured\n\n  Run 'envsync init' or use '--project <project-id>'")
	}

	policy, interactiveAllowed, err := effectivePullPolicy(cmd, cfg, project)
	if err != nil {
		return err
	}

	backupKey, err := atRestKey(kp)
	if err != nil {
		return err
	}

	noiseKP := crypto.NewNoiseKeypair(kp.X25519Private, kp.X25519Public)
	targetFile, _ := cmd.Flags().GetString("file")
	targetFile = projectTargetFile(targetFile, cmd.Flags().Changed("file"), project, cfg)

	report := pullReport{
		ProjectID:             project.ProjectID,
		TargetFile:            targetFile,
		ConflictPolicyApplied: string(policy),
	}

	relayURL := pullRelayURL
	if relayURL == "" {
		relayURL = projectRelayURL(project, cfg)
	}

	ui.Header("EnvSync Pull")
	ui.Line(fmt.Sprintf("  Conflict policy: %s", policy))

	report.RelayChecked = true
	report.RelayAttempted = true
	ui.Line("  Checking relay for pending blobs...")
	relaySummary, relayErr := pullPendingRelay(project.ProjectID, relayURL, targetFile, cfg, kp, pullApplyOptions{
		Policy:      policy,
		Interactive: interactiveAllowed,
		BackupKey:   backupKey,
	})
	if relayErr == nil && relaySummary.AppliedCount > 0 {
		report.RelayPendingFound = relaySummary.FoundCount
		report.RelayHandled = relaySummary.HandledCount
		report.RelayApplied = relaySummary.AppliedCount
		report.Warnings = append(report.Warnings, relaySummary.Warnings...)
		report.InteractiveRequired = relaySummary.InteractiveRequired
		report.ManualInterventionNeeded = relaySummary.ManualInterventionNeeded
		report.BackupCreated = relaySummary.BackupCreated
		if relaySummary.ConflictPolicyApplied != "" {
			report.ConflictPolicyApplied = relaySummary.ConflictPolicyApplied
		}
		ui.Blank()
		return renderPullReport(report, nil)
	}
	if relaySummary != nil {
		report.RelayPendingFound = relaySummary.FoundCount
		report.RelayHandled = relaySummary.HandledCount
		report.Warnings = append(report.Warnings, relaySummary.Warnings...)
		report.InteractiveRequired = relaySummary.InteractiveRequired
		report.ManualInterventionNeeded = relaySummary.ManualInterventionNeeded
		report.BackupCreated = report.BackupCreated || relaySummary.BackupCreated
		if relaySummary.ConflictPolicyApplied != "" {
			report.ConflictPolicyApplied = relaySummary.ConflictPolicyApplied
		}
	}
	if relayErr != nil {
		report.RelayUnavailable = true
		report.Warnings = append(report.Warnings, fmt.Sprintf("relay unavailable: %v", relayErr))
		ui.Warning(fmt.Sprintf("  Relay unavailable: %v", relayErr))
	} else if report.RelayPendingFound == 0 {
		ui.Line("  No pending blobs on relay")
	} else if report.RelayApplied == 0 && report.RelayHandled > 0 {
		ui.Line(fmt.Sprintf("  Handled %d relay blob(s) without mutating the target file", report.RelayHandled))
	} else if report.RelayPendingFound > 0 && report.RelayHandled == 0 {
		ui.Warning(fmt.Sprintf("  Found %d relay blob(s), but none were handled successfully", report.RelayPendingFound))
	}

	if pullServiceKeyPath != "" {
		ui.Blank()
		return renderPullReport(report, relayErr)
	}

	report.LANAttempted = true
	ui.Line("  Listening for LAN push...")
	ui.Blank()

	lanCtx := context.Background()
	if pullTimeoutSeconds > 0 {
		var cancel context.CancelFunc
		lanCtx, cancel = context.WithTimeout(context.Background(), time.Duration(pullTimeoutSeconds)*time.Second)
		defer cancel()
	}

	result, err := envsync.Pull(lanCtx, envsync.PullOptions{
		EnvFilePath:        targetFile,
		Port:               config.DefaultPort,
		TeamID:             project.ProjectID,
		KeyPair:            kp,
		NoiseKeypair:       noiseKP,
		ConfirmBeforeApply: policy == apply.PolicyInteractive,
		ConflictPolicy:     policy,
		Interactive:        interactiveAllowed,
		ProjectID:          project.ProjectID,
		BackupEnabled:      cfg.Sync.AutoBackup,
		BackupKey:          backupKey,
		MaxVersions:        cfg.Sync.MaxVersions,
		Advertise:          cfg.Network.MDNSEnabled,
		AdvertiseVersion:   Version,
		OnListening: func(port int) {
			ui.Line(fmt.Sprintf("  - Listening on port %d", port))
		},
		OnReceived: func(payload envsync.EnvPayload, diff *envfile.DiffResult) {
			ui.Line(fmt.Sprintf("  - Received %s (%d bytes)", payload.FileName, len(payload.Data)))
			if diff != nil && diff.HasChanges() {
				ui.Blank()
				fmt.Print(ui.RenderDiff(diff))
				ui.Blank()
			}
		},
		OnConfirm: func(diff *envfile.DiffResult) bool {
			if diff == nil {
				return true
			}
			return ui.ConfirmAction(fmt.Sprintf("Apply changes? (%s)", diff.Summary()), true)
		},
		OnResolveConflicts: resolvePullConflicts,
		OnApplied: func(fileName string) {
			ui.Success(fmt.Sprintf("Applied to %s", fileName))
		},
	})
	if err != nil {
		if !pullJSON {
			ui.RenderError(ui.StructuredError{
				Category:   ui.ErrSync,
				Message:    "Pull failed",
				Cause:      err.Error(),
				Suggestion: "Ensure the sender is running 'envsync push' or that relay delivery is enabled",
			})
		}
		report.Warnings = append(report.Warnings, err.Error())
		report.InteractiveRequired = report.InteractiveRequired || errors.Is(err, apply.ErrInteractiveRequired)
		report.ManualInterventionNeeded = true
		return renderPullReport(report, err)
	}

	report.LANApplied = result.Applied
	report.BackupCreated = report.BackupCreated || result.BackupCreated
	report.InteractiveRequired = report.InteractiveRequired || result.InteractiveRequired
	report.ManualInterventionNeeded = report.ManualInterventionNeeded || result.ManualInterventionNeeded
	if result.ConflictPolicyApplied != "" {
		report.ConflictPolicyApplied = result.ConflictPolicyApplied
	}

	if result.Applied {
		logger, _ := audit.NewLogger()
		if logger != nil {
			_ = logger.Log(audit.Entry{
				Event:       audit.EventPull,
				File:        result.FileName,
				VarsChanged: result.VarCount,
				Method:      "lan",
				Details:     result.DiffSummary,
			})
		}
	}

	ui.Blank()
	return renderPullReport(report, nil)
}

func shortFP(fp string) string {
	if len(fp) > 16 {
		return fp[:16] + "..."
	}
	return fp
}

func renderPullReport(report pullReport, runErr error) error {
	report.Methods = activePullMethods(report)
	report.Method = "none"
	if len(report.Methods) == 1 {
		report.Method = report.Methods[0]
	} else if len(report.Methods) > 1 {
		report.Method = strings.Join(report.Methods, "+")
	}

	if pullJSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
	}
	return runErr
}

func activePullMethods(report pullReport) []string {
	methods := []string{}
	if report.RelayApplied > 0 {
		methods = append(methods, "relay")
	}
	if report.LANApplied {
		methods = append(methods, "lan")
	}
	return methods
}

func effectivePullPolicy(cmd *cobra.Command, cfg *config.Config, project *projectContext) (apply.Policy, bool, error) {
	interactiveAllowed := !pullNonInteractive && pullServiceKeyPath == ""
	if pullServiceKeyPath != "" && !cmd.Flags().Changed("on-conflict") {
		return apply.PolicyOverwrite, false, nil
	}

	if cmd.Flags().Changed("on-conflict") {
		policy := apply.Policy(strings.TrimSpace(strings.ToLower(pullConflictPolicy)))
		if err := validatePullPolicy(policy); err != nil {
			return "", false, err
		}
		if policy == apply.PolicyInteractive && !interactiveAllowed {
			return "", false, fmt.Errorf("--on-conflict=interactive cannot be used with --service-key or --non-interactive")
		}
		return policy, interactiveAllowed, nil
	}

	policy := apply.Policy(cfg.Sync.MergeStrategy)
	if project != nil && project.Config != nil && project.Config.SyncStrategy != "" {
		policy = apply.Policy(project.Config.SyncStrategy)
	}
	if !interactiveAllowed && policy == apply.PolicyInteractive {
		return apply.PolicyFail, false, nil
	}
	if err := validatePullPolicy(policy); err != nil {
		return "", false, err
	}
	return policy, interactiveAllowed, nil
}

func validatePullPolicy(policy apply.Policy) error {
	switch policy {
	case apply.PolicyInteractive, apply.PolicyOverwrite, apply.PolicyKeepLocal, apply.PolicyThreeWay, apply.PolicyFail:
		return nil
	default:
		return fmt.Errorf("unsupported conflict policy %q (use interactive, overwrite, keep-local, three-way, or fail)", policy)
	}
}

func resolvePullConflicts(conflicts []envfile.Conflict) ([]apply.ConflictResolution, bool) {
	items := make([]ui.ConflictItem, 0, len(conflicts))
	for _, conflict := range conflicts {
		items = append(items, ui.ConflictItem{
			Key:        conflict.Key,
			BaseValue:  conflict.BaseValue,
			OurValue:   conflict.OurValue,
			TheirValue: conflict.TheirValue,
		})
	}

	resolved := ui.RunMergeTUI(items)
	if resolved.Aborted {
		return nil, false
	}

	choices := make([]apply.ConflictResolution, 0, len(resolved.Conflicts))
	for _, item := range resolved.Conflicts {
		resolution := apply.ConflictResolution{Key: item.Key}
		switch item.Decision {
		case ui.MergeAccept:
			resolution.Action = apply.ConflictUseRemote
		case ui.MergeReject:
			resolution.Action = apply.ConflictUseLocal
		case ui.MergeEdit:
			resolution.Action = apply.ConflictUseCustom
			resolution.Value = item.EditValue
		default:
			resolution.Action = apply.ConflictUseLocal
		}
		choices = append(choices, resolution)
	}
	return choices, true
}

func init() {
	pullCmd.Flags().IntVar(&pullTimeoutSeconds, "timeout", 0, "Optional timeout in seconds for LAN listen mode")
	pullCmd.Flags().StringVar(&pullServiceKeyPath, "service-key", "", "Path to an EnvSync service key for relay-only automation")
	pullCmd.Flags().StringVar(&pullProjectID, "project", "", "Override the current project ID")
	pullCmd.Flags().StringVar(&pullProjectID, "team", "", "Deprecated alias for --project")
	_ = pullCmd.Flags().MarkHidden("team")
	pullCmd.Flags().StringVar(&pullRelayURL, "relay", "", "Override the relay URL")
	pullCmd.Flags().BoolVar(&pullJSON, "json", false, "Print pull results as JSON")
	pullCmd.Flags().BoolVar(&pullNonInteractive, "non-interactive", false, "Disable prompts and fail when interactive conflict resolution would be required")
	pullCmd.Flags().StringVar(&pullConflictPolicy, "on-conflict", "", "Conflict policy: interactive, overwrite, keep-local, three-way, or fail")
	rootCmd.AddCommand(pullCmd)
}
