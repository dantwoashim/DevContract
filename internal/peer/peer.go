// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package peer

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/envsync/envsync/internal/crypto"
)

// TrustState represents the trust level of a peer.
type TrustState string

const (
	TrustUnknown TrustState = "unknown"
	TrustPending TrustState = "pending"
	TrustTrusted TrustState = "trusted"
	TrustRevoked TrustState = "revoked"
)

// Peer represents a known peer in the registry.
type Peer struct {
	// GitHubUsername of the peer (e.g., "alice"). This is display metadata only.
	GitHubUsername string `toml:"github_username"`

	// Fingerprint is the Ed25519 identity fingerprint (SHA256:...).
	Fingerprint string `toml:"fingerprint"`

	// Ed25519Public is the identity public key (base64).
	Ed25519Public string `toml:"ed25519_public,omitempty"`

	// X25519Public is the transport public key (base64).
	X25519Public string `toml:"x25519_public,omitempty"`

	// TransportFingerprint is the fingerprint of the X25519 transport key.
	TransportFingerprint string `toml:"transport_fingerprint,omitempty"`

	// DeviceName is an optional human-readable device label.
	DeviceName string `toml:"device_name,omitempty"`

	// Trust is the current trust state.
	Trust TrustState `toml:"trust"`

	// FirstSeen is when this peer was first encountered.
	FirstSeen time.Time `toml:"first_seen"`

	// LastSeen is the last successful sync with this peer.
	LastSeen time.Time `toml:"last_seen"`

	// TrustedAt is when the peer was explicitly trusted.
	TrustedAt time.Time `toml:"trusted_at,omitempty"`

	// RevokedAt is when the peer was revoked.
	RevokedAt time.Time `toml:"revoked_at,omitempty"`
}

// Team represents a team of peers sharing .env files.
type Team struct {
	// ID is a unique team identifier.
	ID string `toml:"id"`

	// Name is a human-readable team name.
	Name string `toml:"name"`

	// CreatedBy is the identity fingerprint of the team creator.
	CreatedBy string `toml:"created_by"`

	// CreatedAt is when the team was created.
	CreatedAt time.Time `toml:"created_at"`

	// Members is the list of identity fingerprints in this team.
	Members []string `toml:"members"`
}

// Validate checks if a peer's data is valid.
func (p *Peer) Validate() error {
	if p.Fingerprint == "" {
		return fmt.Errorf("peer has empty fingerprint")
	}
	if p.Trust == "" {
		return fmt.Errorf("peer has empty trust state")
	}
	return nil
}

// CanSync returns true if this peer is trusted and can participate in sync.
func (p *Peer) CanSync() bool {
	return p.Trust == TrustTrusted
}

// IsRevoked returns true if this peer is revoked.
func (p *Peer) IsRevoked() bool {
	return p.Trust == TrustRevoked
}

// PromoteToTrusted moves the peer from pending to trusted.
func (p *Peer) PromoteToTrusted() error {
	if p.Trust != TrustPending && p.Trust != TrustUnknown {
		return fmt.Errorf("cannot trust peer in state %q", p.Trust)
	}
	p.Trust = TrustTrusted
	p.TrustedAt = time.Now()
	return nil
}

// Revoke moves the peer to revoked state.
func (p *Peer) Revoke() error {
	if p.Trust == TrustRevoked {
		return fmt.Errorf("peer already revoked")
	}
	p.Trust = TrustRevoked
	p.RevokedAt = time.Now()
	return nil
}

// EffectiveTransportFingerprint returns the fingerprint used for transport verification.
func (p *Peer) EffectiveTransportFingerprint() string {
	if p.TransportFingerprint != "" {
		return p.TransportFingerprint
	}

	pub, err := p.TransportPublicKeyBytes()
	if err != nil {
		return ""
	}

	var key [32]byte
	copy(key[:], pub)
	return crypto.ComputeFingerprint(key)
}

// TransportPublicKeyBytes decodes the stored X25519 public key.
func (p *Peer) TransportPublicKeyBytes() ([]byte, error) {
	if p.X25519Public == "" {
		return nil, fmt.Errorf("peer has no X25519 public key")
	}

	decoded, err := base64.StdEncoding.DecodeString(p.X25519Public)
	if err != nil {
		return nil, fmt.Errorf("decoding X25519 public key: %w", err)
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("invalid X25519 public key length: %d", len(decoded))
	}
	return decoded, nil
}

// MatchesTransportPublicKey returns true if the provided key matches the stored transport identity.
func (p *Peer) MatchesTransportPublicKey(publicKey []byte) bool {
	if len(publicKey) != 32 {
		return false
	}

	if expected, err := p.TransportPublicKeyBytes(); err == nil {
		return bytes.Equal(expected, publicKey)
	}

	if fp := p.EffectiveTransportFingerprint(); fp != "" {
		var pk [32]byte
		copy(pk[:], publicKey)
		return fp == crypto.ComputeFingerprint(pk)
	}

	return false
}

// StatusIcon returns a visual indicator for the peer's trust state.
func (p *Peer) StatusIcon() string {
	switch p.Trust {
	case TrustTrusted:
		return "✓"
	case TrustPending:
		return "?"
	case TrustRevoked:
		return "✗"
	default:
		return "·"
	}
}
