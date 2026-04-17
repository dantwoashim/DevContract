// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceKeyGenerateAndExport(t *testing.T) {
	sk, err := GenerateServiceKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(sk.PrivateKey) != 64 {
		t.Errorf("private key len = %d, want 64", len(sk.PrivateKey))
	}
	if len(sk.PublicKey) != 32 {
		t.Errorf("public key len = %d, want 32", len(sk.PublicKey))
	}

	// Export PEM
	privPEM := sk.ExportPrivateKey()
	if len(privPEM) == 0 {
		t.Fatal("empty private PEM")
	}
	if !bytes.Contains(privPEM, []byte("DEVCONTRACT SERVICE KEY")) {
		t.Error("private PEM missing type header")
	}

	pubPEM := sk.ExportPublicKey()
	if len(pubPEM) == 0 {
		t.Fatal("empty public PEM")
	}
	if !bytes.Contains(pubPEM, []byte("DEVCONTRACT SERVICE PUBLIC KEY")) {
		t.Error("public PEM missing type header")
	}
}

func TestServiceKeyImportRoundTrip(t *testing.T) {
	original, err := GenerateServiceKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	exported := original.ExportPrivateKey()
	imported, err := ImportServiceKey(exported)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if !bytes.Equal(original.PublicKey, imported.PublicKey) {
		t.Error("public keys don't match after round-trip")
	}
	if !bytes.Equal(original.PrivateKey.Seed(), imported.PrivateKey.Seed()) {
		t.Error("private key seeds don't match after round-trip")
	}
}

func TestServiceKeyImportInvalid(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"garbage", []byte("not a pem")},
		{"wrong type", []byte("-----BEGIN RSA PRIVATE KEY-----\nYWJj\n-----END RSA PRIVATE KEY-----\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ImportServiceKey(tt.data)
			if err == nil {
				t.Error("expected error for invalid PEM")
			}
		})
	}
}

func TestServiceKeySaveLoadFile(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test.key")

	original, err := GenerateServiceKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if err := original.SaveToFile(keyPath); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Check file permissions
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Error("key file is empty")
	}

	loaded, err := LoadServiceKeyFromFile(keyPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !bytes.Equal(original.PublicKey, loaded.PublicKey) {
		t.Error("public keys don't match after file round-trip")
	}
}
