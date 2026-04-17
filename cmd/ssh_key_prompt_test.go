// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestLoadSSHKeyWithPromptUsesEnvironmentPassphrase(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	block, err := ssh.MarshalPrivateKeyWithPassphrase(privateKey, "test", []byte("devcontract-secret"))
	if err != nil {
		t.Fatalf("marshal encrypted key: %v", err)
	}

	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	t.Setenv(sshKeyPassphraseEnv, "devcontract-secret")

	keyPair, err := loadSSHKeyWithPrompt(keyPath)
	if err != nil {
		t.Fatalf("load SSH key: %v", err)
	}
	if keyPair.Fingerprint == "" {
		t.Fatal("expected fingerprint to be populated")
	}
}
