// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
)

// ServiceKey represents a machine-scoped keypair for CI environments.
type ServiceKey struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
}

// GenerateServiceKey creates a new Ed25519 keypair for CI use.
func GenerateServiceKey() (*ServiceKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating service key: %w", err)
	}
	return &ServiceKey{PrivateKey: priv, PublicKey: pub}, nil
}

// ExportPrivateKey serializes the private key as PEM.
func (sk *ServiceKey) ExportPrivateKey() []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "DEVCONTRACT SERVICE KEY",
		Bytes: sk.PrivateKey.Seed(),
	})
}

// ExportPublicKey serializes the public key as PEM.
func (sk *ServiceKey) ExportPublicKey() []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "DEVCONTRACT SERVICE PUBLIC KEY",
		Bytes: sk.PublicKey,
	})
}

// ImportServiceKey loads a service key from PEM-encoded private key.
func ImportServiceKey(data []byte) (*ServiceKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM data")
	}

	if block.Type != "DEVCONTRACT SERVICE KEY" {
		return nil, fmt.Errorf("unexpected PEM type: %s", block.Type)
	}

	if len(block.Bytes) != ed25519.SeedSize {
		return nil, fmt.Errorf("invalid seed size: %d", len(block.Bytes))
	}

	priv := ed25519.NewKeyFromSeed(block.Bytes)
	pub := priv.Public().(ed25519.PublicKey)

	return &ServiceKey{PrivateKey: priv, PublicKey: pub}, nil
}

// SaveToFile writes the private key to a file with restricted permissions.
func (sk *ServiceKey) SaveToFile(path string) error {
	return os.WriteFile(path, sk.ExportPrivateKey(), 0600)
}

// LoadServiceKeyFromFile loads a service key from a PEM file.
func LoadServiceKeyFromFile(path string) (*ServiceKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading service key: %w", err)
	}
	return ImportServiceKey(data)
}
