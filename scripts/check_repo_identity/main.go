// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	canonicalModule = "github.com/dantwoashim/devcontract"
	legacyModule    = "github.com/devcontract/devcontract"
	repoSlug        = "dantwoashim/DevContract"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fail("determine working directory: %v", err)
	}

	assertContains(filepath.Join(root, "go.mod"), "module "+canonicalModule)
	assertContains(filepath.Join(root, "README.md"), "https://github.com/"+repoSlug+".git")
	assertContains(filepath.Join(root, "CONTRIBUTING.md"), "https://github.com/"+repoSlug+".git")
	assertContains(filepath.Join(root, "scripts", "install.sh"), repoSlug)
	assertContains(filepath.Join(root, "scripts", "install.ps1"), repoSlug)
	assertContains(filepath.Join(root, "extension", "package.json"), "https://github.com/"+repoSlug)
	assertContains(filepath.Join(root, ".goreleaser.yml"), "owner: dantwoashim")
	assertContains(filepath.Join(root, ".goreleaser.yml"), "name: devcontract")

	legacyHits := scanForLegacy(root)
	if len(legacyHits) > 0 {
		fail("legacy repo identity still present:\n%s", strings.Join(legacyHits, "\n"))
	}
}

func assertContains(path, want string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fail("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		fail("%s does not contain %q", path, want)
	}
}

func scanForLegacy(root string) []string {
	var hits []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "out", ".wrangler", ".gocache":
				return filepath.SkipDir
			}
			return nil
		}

		if !shouldScan(path) {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if rel == "scripts/check_repo_identity/main.go" {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		line := 0
		for scanner.Scan() {
			line++
			text := scanner.Text()
			if strings.Contains(text, legacyModule) || strings.Contains(text, "devcontract/devcontract") {
				hits = append(hits, fmt.Sprintf("%s:%d", rel, line))
			}
		}
		return scanner.Err()
	})
	return hits
}

func shouldScan(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".md", ".txt", ".json", ".yaml", ".yml", ".ts", ".js", ".ps1", ".sh":
		return true
	default:
		return filepath.Base(path) == "go.mod"
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
