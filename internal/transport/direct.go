// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package transport

import (
	"fmt"
	"net"
	"time"

	"github.com/envsync/envsync/internal/crypto"
	"github.com/flynn/noise"
)

const (
	// DefaultDialTimeout for LAN connections.
	DefaultDialTimeout = 2 * time.Second

	// WANDialTimeout for longer-lived direct connections.
	WANDialTimeout = 5 * time.Second
)

// DialOptions configures a direct TCP connection.
type DialOptions struct {
	// Address is the target IP:port.
	Address string

	// Timeout for the TCP connection.
	Timeout time.Duration

	// LocalKeypair is our Noise static keypair (X25519).
	LocalKeypair noise.DHKey

	// ExpectedFingerprint if set, reject if peer doesn't match.
	ExpectedFingerprint string

	// OnUnknownPeer is called when the remote peer's key is not recognized.
	// Return nil to accept, error to reject.
	OnUnknownPeer func(fingerprint string) error
}

// Dial establishes a direct TCP connection with Noise protocol encryption.
// Returns an authenticated, encrypted SecureConn.
func Dial(opts DialOptions) (*crypto.SecureConn, error) {
	if opts.Timeout == 0 {
		opts.Timeout = DefaultDialTimeout
	}

	// TCP connect
	conn, err := net.DialTimeout("tcp", opts.Address, opts.Timeout)
	if err != nil {
		return nil, fmt.Errorf("TCP connect to %s: %w", opts.Address, err)
	}

	// Set read/write deadline for handshake
	if err := conn.SetDeadline(time.Now().Add(opts.Timeout)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("setting deadline: %w", err)
	}

	// Noise_XX handshake (initiator)
	secureConn, err := crypto.PerformHandshake(conn, crypto.NoiseConfig{
		StaticKeypair: opts.LocalKeypair,
		IsInitiator:   true,
		VerifyPeer: func(publicKey []byte) error {
			return verifyPeer(publicKey, opts.ExpectedFingerprint, opts.OnUnknownPeer)
		},
	})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("Noise handshake with %s: %w", opts.Address, err)
	}

	// Clear deadline for normal operation
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = secureConn.Close()
		return nil, fmt.Errorf("clearing deadline: %w", err)
	}

	return secureConn, nil
}

// verifyPeer checks the remote peer's identity.
func verifyPeer(publicKey []byte, expectedFingerprint string, onUnknown func(string) error) error {
	var pk [32]byte
	copy(pk[:], publicKey)
	fingerprint := crypto.ComputeFingerprint(pk)

	// If we have an expected fingerprint, strict match
	if expectedFingerprint != "" {
		if fingerprint != expectedFingerprint {
			return fmt.Errorf("peer fingerprint mismatch: expected %s, got %s", expectedFingerprint, fingerprint)
		}
		return nil
	}

	// Delegate unknown-peer handling to the caller.
	if onUnknown != nil {
		return onUnknown(fingerprint)
	}

	return nil
}
