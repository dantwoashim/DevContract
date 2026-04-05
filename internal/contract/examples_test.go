// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package contract_test

import (
	"path/filepath"
	"testing"

	"github.com/envsync/envsync/internal/contract"
)

func TestExampleContractsValidate(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("..", "..", "examples", "contracts", "*.yaml"))
	if err != nil {
		t.Fatalf("glob example contracts: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no example contracts found")
	}

	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			spec, err := contract.Load(path)
			if err != nil {
				t.Fatalf("load contract: %v", err)
			}
			report := spec.Validate()
			if !report.OK() {
				t.Fatalf("contract validation failed: %v", report.Errors)
			}
		})
	}
}
