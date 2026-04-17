// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestParseSSHKeyWithPassphrase(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	block, err := ssh.MarshalPrivateKeyWithPassphrase(privateKey, "test", []byte("secret-passphrase"))
	if err != nil {
		t.Fatalf("marshal encrypted key: %v", err)
	}
	data := pem.EncodeToMemory(block)

	if _, err := ParseSSHKey(data, "test-key"); !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("expected passphrase required error, got %v", err)
	}

	if _, err := ParseSSHKeyWithPassphrase(data, []byte("wrong-passphrase"), "test-key"); !errors.Is(err, ErrInvalidPassphrase) {
		t.Fatalf("expected invalid passphrase error, got %v", err)
	}

	keyPair, err := ParseSSHKeyWithPassphrase(data, []byte("secret-passphrase"), "test-key")
	if err != nil {
		t.Fatalf("parse with passphrase: %v", err)
	}
	if keyPair.Fingerprint == "" {
		t.Fatal("expected fingerprint to be populated")
	}
}
