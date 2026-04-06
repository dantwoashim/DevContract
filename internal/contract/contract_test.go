// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package contract

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndValidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".envsync", "contract.yaml")
	spec := Default("Demo Service")
	spec.Env.Optional = nil
	spec.Env.Required = []EnvVar{{Name: "SERVICE_TOKEN", Source: "shared"}}
	spec.Run.Default = "dev"
	spec.Run.Targets["dev"] = RunTarget{Command: "npm run dev"}

	if err := Save(path, spec); err != nil {
		t.Fatalf("save contract: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}

	report := loaded.Validate()
	if !report.OK() {
		t.Fatalf("expected valid contract, got errors: %v", report.Errors)
	}
	if loaded.Project.Slug != "demo-service" {
		t.Fatalf("unexpected slug %q", loaded.Project.Slug)
	}
}

func TestEnsureDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".envsync", "contract.yaml")

	created, err := EnsureDefault(path, "Test Repo")
	if err != nil {
		t.Fatalf("ensure default: %v", err)
	}
	if !created {
		t.Fatal("expected contract to be created")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("contract path missing: %v", err)
	}
}

func TestInvalidContract(t *testing.T) {
	spec := Default("Invalid")
	spec.Env.Required = []EnvVar{{Name: "not-valid", Source: "shared"}}

	report := spec.Validate()
	if report.OK() {
		t.Fatal("expected validation errors")
	}
}
