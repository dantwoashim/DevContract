// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/envsync/envsync/internal/ui"
	"github.com/spf13/cobra"
)

type bootstrapRuntimeReport struct {
	Name     string `json:"name"`
	Binary   string `json:"binary"`
	Present  bool   `json:"present"`
	Version  string `json:"version,omitempty"`
	Required bool   `json:"required"`
}

type bootstrapStepReport struct {
	Name   string `json:"name"`
	Run    string `json:"run"`
	Status string `json:"status"`
}

type bootstrapReport struct {
	ContractPath string                   `json:"contract_path"`
	RepoRoot     string                   `json:"repo_root"`
	RelayApplied int                      `json:"relay_applied"`
	Outputs      []string                 `json:"outputs"`
	Runtimes     []bootstrapRuntimeReport `json:"runtimes"`
	Steps        []bootstrapStepReport    `json:"steps"`
	Warnings     []string                 `json:"warnings"`
}

var (
	bootstrapJSON     bool
	bootstrapSkipPull bool
	bootstrapSkipRun  bool
	bootstrapDryRun   bool
)

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Bootstrap a repo from its AI onboarding contract",
	Long:  "Pull shared secrets when available, prepare local files, verify required runtimes, and run the contract's bootstrap steps.",
	RunE:  runBootstrap,
}

func runBootstrap(cmd *cobra.Command, args []string) error {
	ctx, err := requireContractContext()
	if err != nil {
		return err
	}

	validation := ctx.Contract.Validate()
	if !validation.OK() {
		return fmt.Errorf("contract validation failed: %s", validation.Errors[0])
	}

	report := bootstrapReport{
		ContractPath: ctx.Path,
		RepoRoot:     ctx.Root,
		Outputs:      []string{},
		Runtimes:     []bootstrapRuntimeReport{},
		Steps:        []bootstrapStepReport{},
		Warnings:     append([]string{}, validation.Warnings...),
	}

	cfg, cfgErr := loadConfig()
	project, projectErr := loadProjectContext()
	kp, identityErr := loadIdentity()

	targetFile, _ := cmd.Flags().GetString("file")
	targetFile = projectTargetFile(targetFile, cmd.Flags().Changed("file"), project, cfg)
	targetFilePath, err := safeRepoPath(ctx.Root, targetFile)
	if err != nil {
		return err
	}

	if !bootstrapSkipPull {
		switch {
		case bootstrapDryRun:
			report.Warnings = append(report.Warnings, "shared secret pull skipped in dry-run mode")
		case cfgErr != nil || projectErr != nil || identityErr != nil:
			report.Warnings = append(report.Warnings, "shared secret pull skipped because EnvSync identity/project setup is incomplete")
		default:
			applied, pullErr := pullPendingRelay(project.ProjectID, projectRelayURL(project, cfg), targetFilePath, cfg, kp)
			if pullErr != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("shared secret pull failed: %v", pullErr))
			} else {
				report.RelayApplied = applied
			}
		}
	}

	for _, output := range ctx.Contract.Bootstrap.Outputs {
		created, outputErr := ensureBootstrapOutput(ctx.Root, output, ctx.Contract.AllEnvNames())
		if outputErr != nil {
			return outputErr
		}
		if created {
			report.Outputs = append(report.Outputs, output.Path)
		}
	}

	if !bootstrapJSON {
		ui.Header("EnvSync Bootstrap")
	}

	blockingRuntimeMissing := false
	for _, runtimeSpec := range ctx.Contract.Runtimes {
		runtimeReport := bootstrapRuntimeReport{
			Name:     runtimeSpec.Name,
			Binary:   runtimeSpec.Binary,
			Present:  commandExists(runtimeSpec.Binary),
			Required: runtimeSpec.Required,
		}
		if runtimeReport.Present {
			runtimeReport.Version = commandVersion(runtimeSpec.Binary, runtimeSpec.VersionArg)
		} else if runtimeSpec.Required {
			blockingRuntimeMissing = true
			report.Warnings = append(report.Warnings, fmt.Sprintf("missing required runtime %s", runtimeSpec.Name))
		}
		report.Runtimes = append(report.Runtimes, runtimeReport)
	}

	if !bootstrapJSON {
		for _, runtimeReport := range report.Runtimes {
			if runtimeReport.Present {
				ui.Line(fmt.Sprintf("  [ok] runtime %-10s %s", runtimeReport.Name, runtimeReport.Version))
			} else {
				ui.Line(fmt.Sprintf("  [warn] runtime %-10s missing", runtimeReport.Name))
			}
		}
	}

	if blockingRuntimeMissing {
		return renderBootstrapReport(report, fmt.Errorf("bootstrap blocked by missing required runtimes"))
	}

	if !bootstrapSkipRun {
		for _, step := range ctx.Contract.Bootstrap.Steps {
			display := renderShellCommand(step.Shell, step.Run)
			stepReport := bootstrapStepReport{Name: step.Name, Run: display, Status: "ok"}
			if bootstrapDryRun {
				stepReport.Status = "planned"
				report.Steps = append(report.Steps, stepReport)
				if !bootstrapJSON {
					ui.Line(fmt.Sprintf("  [plan] step %-10s %s", step.Name, display))
				}
				continue
			}
			if !bootstrapJSON {
				ui.Line(fmt.Sprintf("  [run] step %-10s %s", step.Name, display))
			}
			if err := runShellCommand(ctx.Root, step.Shell, step.Run); err != nil {
				if step.Optional {
					stepReport.Status = "warn"
					report.Warnings = append(report.Warnings, fmt.Sprintf("optional bootstrap step %s failed: %v", step.Name, err))
				} else {
					stepReport.Status = "fail"
					report.Steps = append(report.Steps, stepReport)
					return renderBootstrapReport(report, fmt.Errorf("bootstrap step %s failed: %w", step.Name, err))
				}
			}
			report.Steps = append(report.Steps, stepReport)
		}
	}

	if !bootstrapJSON {
		for _, output := range report.Outputs {
			ui.Success(fmt.Sprintf("Prepared %s", filepath.ToSlash(output)))
		}
	}

	return renderBootstrapReport(report, nil)
}

func renderBootstrapReport(report bootstrapReport, runErr error) error {
	if bootstrapJSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
	} else {
		if report.RelayApplied > 0 {
			ui.Success(fmt.Sprintf("Applied %d shared secret update(s)", report.RelayApplied))
		}
		if len(report.Warnings) > 0 {
			for _, warning := range report.Warnings {
				ui.Warning(warning)
			}
		}
		ui.Blank()
	}
	return runErr
}

func init() {
	bootstrapCmd.Flags().BoolVar(&bootstrapJSON, "json", false, "Print bootstrap results as JSON")
	bootstrapCmd.Flags().BoolVar(&bootstrapSkipPull, "skip-pull", false, "Skip relay pull of shared secrets")
	bootstrapCmd.Flags().BoolVar(&bootstrapSkipRun, "skip-run", false, "Skip executing bootstrap steps")
	bootstrapCmd.Flags().BoolVar(&bootstrapDryRun, "dry-run", false, "Show what bootstrap would do without executing shell steps or pulling shared secrets")
	rootCmd.AddCommand(bootstrapCmd)
}
