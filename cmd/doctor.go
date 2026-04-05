// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/contract"
	"github.com/envsync/envsync/internal/guard"
	"github.com/envsync/envsync/internal/peer"
	"github.com/envsync/envsync/internal/relay"
	"github.com/envsync/envsync/internal/store"
	"github.com/envsync/envsync/internal/ui"
	"github.com/spf13/cobra"
)

type doctorCheck struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
	Blocking bool   `json:"blocking"`
}

type doctorReport struct {
	RepoRoot     string        `json:"repo_root,omitempty"`
	ProjectID    string        `json:"project_id,omitempty"`
	ContractPath string        `json:"contract_path,omitempty"`
	RelayURL     string        `json:"relay_url,omitempty"`
	Checks       []doctorCheck `json:"checks"`
	Blocking     int           `json:"blocking"`
	Warnings     int           `json:"warnings"`
}

var (
	doctorJSON      bool
	doctorSkipRelay bool
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run health checks for the current repo and EnvSync setup",
	Long:  "Validates the repo contract, local environment, agent files, secret safety, EnvSync identity, project metadata, backups, and relay state.",
	RunE:  runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	report := doctorReport{}
	addCheck := func(name, status, detail string, blocking bool) {
		report.Checks = append(report.Checks, doctorCheck{
			Name:     name,
			Status:   status,
			Detail:   detail,
			Blocking: blocking,
		})
		switch status {
		case "fail":
			if blocking {
				report.Blocking++
			} else {
				report.Warnings++
			}
		case "warn":
			report.Warnings++
		}
	}

	cfg, cfgErr := loadConfig()
	if cfgErr != nil {
		addCheck("config", "fail", cfgErr.Error(), true)
	} else if err := cfg.Validate(); err != nil {
		addCheck("config", "fail", err.Error(), true)
	} else {
		addCheck("config", "pass", "Global config loaded and validated", false)
	}

	var project *projectContext
	project, _ = loadProjectContext()
	if project != nil {
		report.ProjectID = project.ProjectID
		report.RelayURL = projectRelayURL(project, cfg)
		addCheck("project", "pass", fmt.Sprintf("Project ID %s", project.ProjectID), false)
	} else {
		addCheck("project", "warn", "Project config not loaded; run 'envsync init' if this repo has not been initialized yet", false)
	}

	repoRoot, rootErr := config.FindProjectRoot()
	if rootErr != nil {
		repoRoot = mustGetwd()
	}
	report.RepoRoot = repoRoot

	spec, contractPath, contractErr := contract.LoadProject()
	if contractErr != nil {
		addCheck("contract", "fail", contractErr.Error(), true)
	} else {
		report.ContractPath = contractPath
		validation := spec.Validate()
		if len(validation.Errors) > 0 {
			addCheck("contract", "fail", strings.Join(validation.Errors, "; "), true)
		} else if len(validation.Warnings) > 0 {
			addCheck("contract", "warn", strings.Join(validation.Warnings, "; "), false)
		} else {
			addCheck("contract", "pass", fmt.Sprintf("Loaded %s", contractPath), false)
		}
	}

	var kpDetail string
	kp, identityErr := loadIdentityIfAvailable()
	if identityErr != nil {
		addCheck("identity", "warn", fmt.Sprintf("Identity not available: %v", identityErr), false)
	} else {
		if cfg != nil && cfg.Identity.Fingerprint != "" && cfg.Identity.Fingerprint != kp.Fingerprint {
			addCheck("identity", "fail", "Configured fingerprint does not match the active private key. Re-run 'envsync init'.", true)
		} else {
			kpDetail = fmt.Sprintf("Identity %s", shortenDoctor(kp.Fingerprint, 20))
			addCheck("identity", "pass", kpDetail, false)
		}
	}

	targetFile, _ := cmd.Flags().GetString("file")
	targetFile = projectTargetFile(targetFile, cmd.Flags().Changed("file"), project, cfg)
	targetFilePath, pathErr := safeRepoPath(repoRoot, targetFile)
	if pathErr != nil {
		targetFilePath = filepath.Join(repoRoot, targetFile)
	}

	switch _, err := os.Stat(targetFilePath); {
	case err == nil:
		addCheck("env-file", "pass", fmt.Sprintf("Target file %s exists", filepath.ToSlash(targetFile)), false)
	case os.IsNotExist(err):
		addCheck("env-file", "warn", fmt.Sprintf("Target file %s does not exist yet; bootstrap or pull can create it", filepath.ToSlash(targetFile)), false)
	default:
		addCheck("env-file", "fail", fmt.Sprintf("Cannot access %s: %v", filepath.ToSlash(targetFile), err), true)
	}

	if spec != nil {
		values := repoEnvValues(repoRoot, spec, targetFilePath)
		for _, envVar := range spec.Env.Required {
			if strings.TrimSpace(values[envVar.Name]) == "" {
				addCheck("env:"+envVar.Name, "fail", fmt.Sprintf("Required env var %s is missing from local files or shell environment", envVar.Name), true)
			} else {
				addCheck("env:"+envVar.Name, "pass", fmt.Sprintf("Required env var %s is available", envVar.Name), false)
			}
		}

		for _, runtimeSpec := range spec.Runtimes {
			if !commandExists(runtimeSpec.Binary) {
				status := "warn"
				blocking := false
				if runtimeSpec.Required {
					status = "fail"
					blocking = true
				}
				addCheck("runtime:"+runtimeSpec.Name, status, fmt.Sprintf("Binary %s not found", runtimeSpec.Binary), blocking)
				continue
			}
			addCheck("runtime:"+runtimeSpec.Name, "pass", fmt.Sprintf("%s %s", runtimeSpec.Binary, shortenDoctor(commandVersion(runtimeSpec.Binary, runtimeSpec.VersionArg), 60)), false)
		}

		for _, service := range spec.Services {
			host := service.Host
			if host == "" {
				host = "127.0.0.1"
			}
			if service.Port == 0 {
				continue
			}
			address := fmt.Sprintf("%s:%d", host, service.Port)
			conn, err := net.DialTimeout("tcp", address, 500*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				addCheck("service:"+service.Name, "pass", fmt.Sprintf("%s is reachable on %s", service.Name, address), false)
				continue
			}
			status := "warn"
			blocking := false
			if service.Required {
				status = "fail"
				blocking = true
			}
			detail := fmt.Sprintf("%s is not reachable on %s", service.Name, address)
			if service.Start != "" {
				detail += fmt.Sprintf(" (start with: %s)", service.Start)
			}
			addCheck("service:"+service.Name, status, detail, blocking)
		}

		for _, contractCheck := range spec.Doctor.Checks {
			runContractDoctorCheck(repoRoot, contractCheck, addCheck)
		}

		agentNames := make([]string, 0, len(spec.Agents))
		for agentName := range spec.Agents {
			agentNames = append(agentNames, agentName)
		}
		sort.Strings(agentNames)
		for _, agentName := range agentNames {
			target := spec.Agents[agentName]
			outputPath, err := safeRepoPath(repoRoot, target.Output)
			if err != nil {
				addCheck("agent:"+agentName, "fail", err.Error(), true)
				continue
			}
			if _, err := os.Stat(outputPath); err == nil {
				addCheck("agent:"+agentName, "pass", fmt.Sprintf("Agent instructions present at %s", filepath.ToSlash(target.Output)), false)
			} else {
				addCheck("agent:"+agentName, "warn", fmt.Sprintf("Agent instructions missing at %s; run 'envsync agent install --agent %s'", filepath.ToSlash(target.Output), agentName), false)
			}

			if target.MCPOutput == "" {
				continue
			}
			mcpPath, err := safeRepoPath(repoRoot, target.MCPOutput)
			if err != nil {
				addCheck("mcp:"+agentName, "fail", err.Error(), true)
				continue
			}
			data, err := os.ReadFile(mcpPath)
			if err != nil {
				addCheck("mcp:"+agentName, "warn", fmt.Sprintf("MCP config missing at %s; run 'envsync agent install --agent %s'", filepath.ToSlash(target.MCPOutput), agentName), false)
				continue
			}
			if !json.Valid(data) {
				addCheck("mcp:"+agentName, "fail", fmt.Sprintf("MCP config at %s is not valid JSON", filepath.ToSlash(target.MCPOutput)), true)
			} else if err := validateMCPConfig(data); err != nil {
				addCheck("mcp:"+agentName, "fail", fmt.Sprintf("MCP config at %s is invalid: %v", filepath.ToSlash(target.MCPOutput), err), true)
			} else {
				addCheck("mcp:"+agentName, "pass", fmt.Sprintf("MCP config valid at %s", filepath.ToSlash(target.MCPOutput)), false)
			}
		}

		guardReport, err := guard.ScanContractAware(repoRoot, spec, nil)
		if err != nil {
			addCheck("guard", "warn", fmt.Sprintf("Guard scan failed: %v", err), false)
		} else if len(guardReport.Findings) == 0 {
			addCheck("guard", "pass", "No unsafe secrets found in agent-facing files", false)
		} else {
			highest := "warn"
			blocking := false
			if guardReport.HasSeverity(guard.SeverityError) {
				highest = "fail"
				blocking = true
			}
			addCheck("guard", highest, fmt.Sprintf("%d issue(s) found in agent-facing files; run 'envsync guard scan' for details", len(guardReport.Findings)), blocking)
		}
	}

	if project != nil && cfg != nil && kp != nil {
		registry, registryErr := peer.NewRegistry()
		if registryErr != nil {
			addCheck("registry", "fail", registryErr.Error(), true)
		} else {
			team, err := registry.LoadTeam(project.ProjectID)
			if err != nil {
				addCheck("registry", "warn", fmt.Sprintf("Local team metadata missing for %s", project.ProjectID), false)
			} else {
				addCheck("registry", "pass", fmt.Sprintf("Local team metadata loaded (%d members)", len(team.Members)), false)
			}
		}

		vStore, storeErr := store.New(cfg.Sync.MaxVersions)
		if storeErr != nil {
			addCheck("backups", "fail", storeErr.Error(), true)
		} else if _, err := vStore.NextSequence(project.ProjectID); err != nil {
			addCheck("backups", "fail", err.Error(), true)
		} else {
			addCheck("backups", "pass", "Backup store is writable for this project namespace", false)
		}

		if !doctorSkipRelay {
			client := relay.NewClient(projectRelayURL(project, cfg), kp)
			health, err := client.Health()
			if err != nil {
				addCheck("relay", "warn", fmt.Sprintf("Relay health check failed: %v", err), false)
			} else {
				status, _ := health["status"].(string)
				if status == "" {
					status = "ok"
				}
				addCheck("relay", "pass", fmt.Sprintf("Relay reachable at %s (%s)", projectRelayURL(project, cfg), status), false)
			}

			members, err := client.ListTeamMembers(project.ProjectID)
			if err != nil {
				addCheck("membership", "warn", fmt.Sprintf("Could not verify relay membership: %v", err), false)
			} else {
				foundSelf := false
				for _, member := range members {
					if member.Fingerprint == kp.Fingerprint {
						foundSelf = true
						break
					}
				}
				if foundSelf {
					addCheck("membership", "pass", fmt.Sprintf("Current device is registered on relay (%d member records)", len(members)), false)
				} else {
					addCheck("membership", "warn", "Current device is not registered on relay. Run invite/join or service-key register as needed.", false)
				}
			}
		}
	}

	return renderDoctor(report)
}

func renderDoctor(report doctorReport) error {
	if doctorJSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
	} else {
		ui.Header("EnvSync Doctor")
		for _, check := range report.Checks {
			label := "[ok]"
			switch check.Status {
			case "warn":
				label = "[warn]"
			case "fail":
				label = "[fail]"
			}
			ui.Line(fmt.Sprintf("  %s %-16s %s", label, check.Name, check.Detail))
		}
		ui.Blank()
		if report.RepoRoot != "" {
			ui.Line(fmt.Sprintf("  Repo root:     %s", report.RepoRoot))
		}
		if report.ContractPath != "" {
			ui.Line(fmt.Sprintf("  Contract path: %s", report.ContractPath))
		}
		if report.ProjectID != "" {
			ui.Line(fmt.Sprintf("  Project ID:    %s", report.ProjectID))
		}
		if report.RelayURL != "" {
			ui.Line(fmt.Sprintf("  Relay URL:     %s", report.RelayURL))
		}
		ui.Blank()
	}

	if report.Blocking > 0 {
		return fmt.Errorf("doctor found %d blocking issue(s) and %d warning(s)", report.Blocking, report.Warnings)
	}
	if !doctorJSON {
		if report.Warnings > 0 {
			ui.Warning(fmt.Sprintf("Doctor found %d warning(s) and no blocking issues", report.Warnings))
		} else {
			ui.Success("Doctor found no blocking issues")
		}
	}
	return nil
}

func shortenDoctor(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max || max < 4 {
		return value
	}
	return value[:max-3] + "..."
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "Print doctor results as JSON")
	doctorCmd.Flags().BoolVar(&doctorSkipRelay, "skip-relay", false, "Skip relay health and membership checks")
	rootCmd.AddCommand(doctorCmd)
}

func runContractDoctorCheck(repoRoot string, check contract.DoctorCheck, addCheck func(name, status, detail string, blocking bool)) {
	name := "doctor:" + check.Name
	required := check.Required
	fail := func(detail string) {
		status := "warn"
		if required {
			status = "fail"
		}
		addCheck(name, status, detail, required)
	}

	switch strings.ToLower(strings.TrimSpace(check.Type)) {
	case "file_exists":
		path, err := safeRepoPath(repoRoot, check.Target)
		if err != nil {
			fail(err.Error())
			return
		}
		if _, err := os.Stat(path); err != nil {
			fail(fmt.Sprintf("Expected file %s is missing", filepath.ToSlash(check.Target)))
			return
		}
		addCheck(name, "pass", fmt.Sprintf("File %s exists", filepath.ToSlash(check.Target)), false)
	case "json_valid":
		path, err := safeRepoPath(repoRoot, check.Target)
		if err != nil {
			fail(err.Error())
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fail(fmt.Sprintf("Cannot read JSON file %s: %v", filepath.ToSlash(check.Target), err))
			return
		}
		if !json.Valid(data) {
			fail(fmt.Sprintf("JSON file %s is not valid", filepath.ToSlash(check.Target)))
			return
		}
		addCheck(name, "pass", fmt.Sprintf("JSON file %s is valid", filepath.ToSlash(check.Target)), false)
	case "tcp":
		target := strings.TrimSpace(check.Target)
		conn, err := net.DialTimeout("tcp", target, 500*time.Millisecond)
		if err != nil {
			fail(fmt.Sprintf("TCP target %s is unreachable: %v", target, err))
			return
		}
		_ = conn.Close()
		addCheck(name, "pass", fmt.Sprintf("TCP target %s is reachable", target), false)
	default:
		fail(fmt.Sprintf("Unsupported doctor check type %q", check.Type))
	}
}

func validateMCPConfig(data []byte) error {
	var payload struct {
		Servers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if len(payload.Servers) == 0 {
		return fmt.Errorf("missing servers object")
	}
	for name, server := range payload.Servers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("server name cannot be empty")
		}
		if strings.TrimSpace(server.Command) == "" {
			return fmt.Errorf("server %q is missing a command", name)
		}
	}
	return nil
}
