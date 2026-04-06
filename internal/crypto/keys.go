// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package crypto

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/ssh"
)

var (
	// ErrPassphraseRequired indicates that a private key is encrypted and needs a passphrase.
	ErrPassphraseRequired = errors.New("ssh key passphrase required")
	// ErrInvalidPassphrase indicates that the supplied passphrase did not unlock the key.
	ErrInvalidPassphrase = errors.New("ssh key passphrase was not accepted")
)

// KeyPair holds the cryptographic identity derived from an SSH Ed25519 key.
type KeyPair struct {
	// Ed25519 keys (signing)
	Ed25519Private ed25519.PrivateKey
	Ed25519Public  ed25519.PublicKey

	// X25519 keys (Diffie-Hellman, derived from Ed25519)
	X25519Private [32]byte
	X25519Public  [32]byte

	// Fingerprint in OpenSSH format: "SHA256:<base64>"
	Fingerprint string

	// Path to the SSH key file
	KeyPath string
}

// LoadSSHKey reads an Ed25519 SSH private key and derives the X25519 DH key pair.
// It handles OpenSSH format, PEM format, and detects passphrase-protected keys.
func LoadSSHKey(path string) (*KeyPair, error) {
	return LoadSSHKeyWithPassphrase(path, nil)
}

// LoadSSHKeyWithPassphrase reads an Ed25519 SSH private key and derives the
// X25519 DH key pair, using the provided passphrase when the key is encrypted.
func LoadSSHKeyWithPassphrase(path string, passphrase []byte) (*KeyPair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("SSH key not found at %s. Generate one with: ssh-keygen -t ed25519", path)
		}
		return nil, fmt.Errorf("reading SSH key: %w", err)
	}

	return ParseSSHKeyWithPassphrase(data, passphrase, path)
}

// ParseSSHKey parses raw SSH key bytes into a KeyPair.
func ParseSSHKey(data []byte, keyPath string) (*KeyPair, error) {
	return ParseSSHKeyWithPassphrase(data, nil, keyPath)
}

// ParseSSHKeyWithPassphrase parses raw SSH key bytes into a KeyPair, using the
// provided passphrase when the key is encrypted.
func ParseSSHKeyWithPassphrase(data, passphrase []byte, keyPath string) (*KeyPair, error) {
	// Try parsing as OpenSSH format first (most common)
	rawKey, err := ssh.ParseRawPrivateKey(data)
	if err != nil {
		// Check if it's passphrase-protected
		if isPassphraseError(err) {
			if len(passphrase) == 0 {
				return nil, fmt.Errorf("%w for %s", ErrPassphraseRequired, keyPath)
			}
			rawKey, err = ssh.ParseRawPrivateKeyWithPassphrase(data, passphrase)
			if err != nil {
				if isPassphraseError(err) {
					return nil, fmt.Errorf("%w for %s", ErrInvalidPassphrase, keyPath)
				}
				return nil, fmt.Errorf("parsing passphrase-protected SSH key: %w", err)
			}
		} else {
			// Try PEM format
			block, _ := pem.Decode(data)
			if block != nil {
				rawKey, err = ssh.ParseRawPrivateKey(pem.EncodeToMemory(block))
				if err != nil {
					if isPassphraseError(err) {
						if len(passphrase) == 0 {
							return nil, fmt.Errorf("%w for %s", ErrPassphraseRequired, keyPath)
						}
						rawKey, err = ssh.ParseRawPrivateKeyWithPassphrase(pem.EncodeToMemory(block), passphrase)
						if err != nil {
							if isPassphraseError(err) {
								return nil, fmt.Errorf("%w for %s", ErrInvalidPassphrase, keyPath)
							}
							return nil, fmt.Errorf("parsing passphrase-protected PEM SSH key: %w", err)
						}
					} else {
						return nil, fmt.Errorf("parsing PEM SSH key: %w", err)
					}
				}
			} else {
				return nil, fmt.Errorf("unrecognized SSH key format: %w", err)
			}
		}
	}

	// Extract Ed25519 key
	ed25519Key, ok := rawKey.(*ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("SSH key is not Ed25519 (got %T). EnvSync requires Ed25519 keys.\n"+
			"  Generate one: ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519", rawKey)
	}

	return NewKeyPairFromEd25519(*ed25519Key)
}

// NewKeyPairFromEd25519 creates a KeyPair from an Ed25519 private key,
// performing the birational conversion to X25519.
func NewKeyPairFromEd25519(privateKey ed25519.PrivateKey) (*KeyPair, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Ed25519 private key size: got %d, want %d", len(privateKey), ed25519.PrivateKeySize)
	}

	publicKey := privateKey.Public().(ed25519.PublicKey)

	// Ed25519 → X25519 conversion (birational map)
	x25519Private, err := Ed25519PrivateToX25519(privateKey)
	if err != nil {
		return nil, fmt.Errorf("converting Ed25519 to X25519: %w", err)
	}

	x25519Public, err := Ed25519PublicToX25519(publicKey)
	if err != nil {
		return nil, fmt.Errorf("converting Ed25519 public to X25519: %w", err)
	}

	// Compute fingerprint from the SSH public key
	sshPubKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("creating SSH public key: %w", err)
	}
	fingerprint := ssh.FingerprintSHA256(sshPubKey)

	return &KeyPair{
		Ed25519Private: privateKey,
		Ed25519Public:  publicKey,
		X25519Private:  x25519Private,
		X25519Public:   x25519Public,
		Fingerprint:    fingerprint,
	}, nil
}

// Ed25519PrivateToX25519 converts an Ed25519 private key to an X25519 private key.
// This uses the first 32 bytes of the Ed25519 seed, hashed with SHA-512,
// then clamped per RFC 7748.
func Ed25519PrivateToX25519(privateKey ed25519.PrivateKey) ([32]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return [32]byte{}, errors.New("invalid Ed25519 private key size")
	}

	// The Ed25519 private key seed is the first 32 bytes
	seed := privateKey.Seed()

	// SHA-512 hash of the seed, take first 32 bytes, then clamp
	h := sha512First32(seed)

	// Clamp (per RFC 7748)
	h[0] &= 248
	h[31] &= 127
	h[31] |= 64

	return h, nil
}

// Ed25519PublicToX25519 converts an Ed25519 public key to an X25519 public key.
// This performs the birational map from Edwards to Montgomery form.
func Ed25519PublicToX25519(publicKey ed25519.PublicKey) ([32]byte, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return [32]byte{}, errors.New("invalid Ed25519 public key size")
	}

	// Use the extra25519-style conversion
	// The Ed25519 public key is in Edwards form (y coordinate).
	// Montgomery u = (1 + y) / (1 - y) mod p
	var edPub, x25519Pub [32]byte
	copy(edPub[:], publicKey)

	if !edwardsToMontgomery(&x25519Pub, &edPub) {
		return [32]byte{}, errors.New("failed to convert Ed25519 public key to X25519")
	}

	return x25519Pub, nil
}

// ComputeFingerprint computes the SHA-256 fingerprint of an X25519 public key.
// Returns format: "SHA256:<base64>"
func ComputeFingerprint(pubKey [32]byte) string {
	hash := sha256.Sum256(pubKey[:])
	encoded := base64.RawStdEncoding.EncodeToString(hash[:])
	return "SHA256:" + encoded
}

// IsPassphraseProtected attempts to detect if an SSH key file is passphrase protected.
func IsPassphraseProtected(data []byte) bool {
	_, err := ssh.ParseRawPrivateKey(data)
	return isPassphraseError(err)
}

func isPassphraseError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "passphrase") ||
		strings.Contains(msg, "encrypted") ||
		strings.Contains(msg, "bcrypt_pbkdf") ||
		strings.Contains(msg, "incorrect password") ||
		strings.Contains(msg, "decryption password incorrect")
}

// sha512First32 computes SHA-512 of input and returns the first 32 bytes.
// Used for Ed25519 → X25519 seed conversion per the Ed25519 spec.
func sha512First32(input []byte) [32]byte {
	// We use crypto/sha512 for the Ed25519 → X25519 conversion
	// as specified in the Ed25519 paper.
	// Import is at the top level to avoid confusion with sha256.
	h := sha512Sum(input)
	var result [32]byte
	copy(result[:], h[:32])
	return result
}

// DeriveSharedSecret performs X25519 Diffie-Hellman key agreement.
func DeriveSharedSecret(privateKey, publicKey [32]byte) ([32]byte, error) {
	shared, err := curve25519.X25519(privateKey[:], publicKey[:])
	if err != nil {
		return [32]byte{}, fmt.Errorf("X25519 key agreement failed: %w", err)
	}
	var result [32]byte
	copy(result[:], shared)
	return result, nil
}
