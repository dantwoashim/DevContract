// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	// MagicBytes identifies a DevContract encrypted file.
	MagicBytes       = "DEVCONT\x01"
	legacyMagicBytes = "ENVSYNC\x01"
	magicLen         = len(MagicBytes)

	// NonceSize for XChaCha20-Poly1305 (24 bytes).
	NonceSize = 24

	// hkdfSalt for at-rest encryption key derivation.
	hkdfSalt = "devcontract-at-rest-v1"

	// hkdfInfo for at-rest encryption key derivation.
	hkdfInfo = "local-storage-encryption"
)

// DeriveAtRestKey derives an encryption key from SSH private key bytes using HKDF-SHA256.
func DeriveAtRestKey(sshPrivateKeyBytes []byte) ([32]byte, error) {
	if len(sshPrivateKeyBytes) == 0 {
		return [32]byte{}, errors.New("empty key material")
	}

	hkdfReader := hkdf.New(sha256.New, sshPrivateKeyBytes, []byte(hkdfSalt), []byte(hkdfInfo))

	var key [32]byte
	if _, err := io.ReadFull(hkdfReader, key[:]); err != nil {
		return [32]byte{}, fmt.Errorf("HKDF key derivation failed: %w", err)
	}

	return key, nil
}

// Encrypt encrypts plaintext using XChaCha20-Poly1305 with the given key.
// Returns the DevContract file format: magic (8) + nonce (24) + ciphertext + tag (16).
func Encrypt(plaintext []byte, key [32]byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return nil, fmt.Errorf("creating XChaCha20-Poly1305: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	// Encrypt
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	// Assemble: magic + nonce + ciphertext (includes Poly1305 tag)
	result := make([]byte, 0, magicLen+NonceSize+len(ciphertext))
	result = append(result, MagicBytes...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// Decrypt decrypts a DevContract encrypted file using XChaCha20-Poly1305.
// Expects format: magic (8) + nonce (24) + ciphertext + tag (16).
func Decrypt(data []byte, key [32]byte) ([]byte, error) {
	minSize := magicLen + NonceSize + chacha20poly1305.Overhead
	if len(data) < minSize {
		return nil, fmt.Errorf("encrypted data too short: got %d bytes, minimum %d", len(data), minSize)
	}

	// Accept the legacy EnvSync prefix so existing local history survives the rebrand.
	magic := string(data[:magicLen])
	if magic != MagicBytes && magic != legacyMagicBytes {
		return nil, errors.New("not a DevContract encrypted file (invalid magic bytes)")
	}

	// Extract nonce and ciphertext
	nonce := data[magicLen : magicLen+NonceSize]
	ciphertext := data[magicLen+NonceSize:]

	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return nil, fmt.Errorf("creating XChaCha20-Poly1305: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong key or corrupted data): %w", err)
	}

	return plaintext, nil
}

// EncryptForRecipient encrypts plaintext for a specific recipient using ephemeral ECDH.
// Returns: ephemeral public key (32) + nonce (24) + ciphertext + tag (16).
// Plaintext is padded to the nearest 1KB boundary to prevent traffic analysis.
func EncryptForRecipient(plaintext []byte, recipientPublicKey [32]byte) (ephemeralPub [32]byte, encrypted []byte, err error) {
	// Pad plaintext to nearest 1KB boundary (2-byte length prefix + data + padding)
	padded, err := padTo1KB(plaintext)
	if err != nil {
		return [32]byte{}, nil, fmt.Errorf("padding plaintext: %w", err)
	}

	// Generate ephemeral X25519 keypair
	// Note: curve25519.X25519() clamps internally, no manual clamping needed.
	var ephemeralPrivate [32]byte
	if _, err := rand.Read(ephemeralPrivate[:]); err != nil {
		return [32]byte{}, nil, fmt.Errorf("generating ephemeral key: %w", err)
	}

	// Compute ephemeral public key
	ephPub, err := curve25519X25519Base(ephemeralPrivate)
	if err != nil {
		return [32]byte{}, nil, fmt.Errorf("computing ephemeral public key: %w", err)
	}

	// ECDH: shared secret
	shared, err := DeriveSharedSecret(ephemeralPrivate, recipientPublicKey)
	if err != nil {
		return [32]byte{}, nil, fmt.Errorf("ECDH key agreement: %w", err)
	}

	// Derive encryption key via HKDF
	recipientFP := ComputeFingerprint(recipientPublicKey)
	hkdfReader := hkdf.New(sha256.New, shared[:], []byte("devcontract-relay-v1"), []byte(recipientFP))

	var encKey [32]byte
	if _, err := io.ReadFull(hkdfReader, encKey[:]); err != nil {
		return [32]byte{}, nil, fmt.Errorf("HKDF for relay encryption: %w", err)
	}

	// Encrypt with derived key (no magic bytes for relay envelopes)
	aead, err := chacha20poly1305.NewX(encKey[:])
	if err != nil {
		return [32]byte{}, nil, fmt.Errorf("creating cipher: %w", err)
	}

	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return [32]byte{}, nil, fmt.Errorf("generating nonce: %w", err)
	}

	// Bind ephemeral public key as AAD to prevent key-substitution attacks
	ciphertext := aead.Seal(nil, nonce, padded, ephPub[:])

	// Assemble: nonce + ciphertext
	encrypted = make([]byte, 0, NonceSize+len(ciphertext))
	encrypted = append(encrypted, nonce...)
	encrypted = append(encrypted, ciphertext...)

	return ephPub, encrypted, nil
}

// DecryptFromSender decrypts data sent by a specific sender using ephemeral ECDH.
func DecryptFromSender(encrypted []byte, ephemeralPublicKey [32]byte, recipientPrivateKey [32]byte, recipientPublicKey [32]byte) ([]byte, error) {
	if len(encrypted) < NonceSize+chacha20poly1305.Overhead {
		return nil, errors.New("encrypted data too short")
	}

	// ECDH: shared secret using recipient's private key and sender's ephemeral public
	shared, err := DeriveSharedSecret(recipientPrivateKey, ephemeralPublicKey)
	if err != nil {
		return nil, fmt.Errorf("ECDH key agreement: %w", err)
	}

	// Derive decryption key via HKDF (must match sender's derivation)
	recipientFP := ComputeFingerprint(recipientPublicKey)
	hkdfReader := hkdf.New(sha256.New, shared[:], []byte("devcontract-relay-v1"), []byte(recipientFP))

	var decKey [32]byte
	if _, err := io.ReadFull(hkdfReader, decKey[:]); err != nil {
		return nil, fmt.Errorf("HKDF for relay decryption: %w", err)
	}

	// Extract nonce and ciphertext
	nonce := encrypted[:NonceSize]
	ciphertext := encrypted[NonceSize:]

	aead, err := chacha20poly1305.NewX(decKey[:])
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	// AAD must match what was used during encryption (ephemeral public key)
	padded, err := aead.Open(nil, nonce, ciphertext, ephemeralPublicKey[:])
	if err != nil {
		return nil, errors.New("decryption failed: wrong key, corrupted data, or not intended for this recipient")
	}

	// Remove 1KB boundary padding
	plaintext, err := unpadFrom1KB(padded)
	if err != nil {
		return nil, fmt.Errorf("unpadding: %w", err)
	}

	return plaintext, nil
}

// curve25519X25519Base computes the X25519 base point multiplication (public from private).
func curve25519X25519Base(privateKey [32]byte) ([32]byte, error) {
	pub, err := curve25519X25519(privateKey[:], curve25519Basepoint())
	if err != nil {
		return [32]byte{}, err
	}
	var result [32]byte
	copy(result[:], pub)
	return result, nil
}

func curve25519X25519(scalar, point []byte) ([]byte, error) {
	// Use golang.org/x/crypto/curve25519
	return curveX25519(scalar, point)
}

func curve25519Basepoint() []byte {
	// X25519 basepoint
	basepoint := [32]byte{9}
	return basepoint[:]
}

// padTo1KB pads data to the nearest 1KB boundary for traffic analysis prevention.
// Format: 2-byte big-endian length prefix + original data + zero padding.
// For payloads > 65535 bytes: 2-byte marker (0xFF, 0xFF) + 4-byte uint32 length + data.
func padTo1KB(data []byte) ([]byte, error) {
	if len(data) > math.MaxUint32 {
		return nil, fmt.Errorf("payload too large to pad: %d bytes", len(data))
	}

	if len(data) > 65535 {
		// Extended format: 2-byte marker + 4-byte uint32 length + data
		totalNeeded := 6 + len(data)
		paddedLen := ((totalNeeded + 1023) / 1024) * 1024
		result := make([]byte, paddedLen)
		result[0] = 0xFF
		result[1] = 0xFF
		// #nosec G115 -- len(data) is bounded by the check above.
		binary.BigEndian.PutUint32(result[2:6], uint32(len(data)))
		copy(result[6:], data)
		return result, nil
	}

	totalNeeded := 2 + len(data) // 2 bytes for length prefix + data
	// Round up to nearest 1024 boundary
	paddedLen := ((totalNeeded + 1023) / 1024) * 1024
	if paddedLen < 1024 {
		paddedLen = 1024
	}

	result := make([]byte, paddedLen)
	// Write length as big-endian uint16
	// #nosec G115 -- this branch only accepts payloads that fit in uint16.
	binary.BigEndian.PutUint16(result[0:2], uint16(len(data)))
	copy(result[2:], data)
	return result, nil
}

// unpadFrom1KB removes 1KB boundary padding by reading the length prefix.
func unpadFrom1KB(padded []byte) ([]byte, error) {
	if len(padded) < 2 {
		return nil, errors.New("padded data too short")
	}

	// Check for extended format marker (0xFF, 0xFF)
	if padded[0] == 0xFF && padded[1] == 0xFF {
		if len(padded) < 6 {
			return nil, errors.New("extended padded data too short")
		}
		dataLen := int(padded[2])<<24 | int(padded[3])<<16 | int(padded[4])<<8 | int(padded[5])
		if 6+dataLen > len(padded) {
			return nil, fmt.Errorf("data length %d exceeds padded buffer %d", dataLen, len(padded)-6)
		}
		return padded[6 : 6+dataLen], nil
	}

	dataLen := int(padded[0])<<8 | int(padded[1])
	if 2+dataLen > len(padded) {
		return nil, fmt.Errorf("data length %d exceeds padded buffer %d", dataLen, len(padded)-2)
	}
	return padded[2 : 2+dataLen], nil
}
