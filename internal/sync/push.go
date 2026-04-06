// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/discovery"
	"github.com/envsync/envsync/internal/envfile"
	"github.com/envsync/envsync/internal/transport"
	"github.com/flynn/noise"
)

// PushOptions configures a push operation.
type PushOptions struct {
	// EnvFilePath is the path to the .env file to push.
	EnvFilePath string

	// TeamID identifies the team to push to.
	TeamID string

	// KeyPair is the local identity.
	KeyPair *crypto.KeyPair

	// NoiseKeypair derived from KeyPair.
	NoiseKeypair noise.DHKey

	// Sequence is the version sequence number.
	Sequence int64

	// OnPeerFound is called when a peer is discovered.
	OnPeerFound func(peer discovery.Peer)

	// OnHandshake is called after noise handshake succeeds.
	OnHandshake func(fingerprint string)

	// OnComplete is called when push to a peer completes.
	OnComplete func(peer discovery.Peer)

	// OnError is called when push to a peer fails.
	OnError func(peer discovery.Peer, err error)
}

// PushResult summarizes the push operation.
type PushResult struct {
	FileName    string
	FileSize    int
	VarCount    int
	PeersFound  int
	PeersSynced int
	Errors      []error
}

// Push discovers peers and sends the .env file to each one.
func Push(ctx context.Context, opts PushOptions) (*PushResult, error) {
	// Read the env file
	envPath := opts.EnvFilePath
	if envPath == "" {
		envPath = ".env"
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", envPath, err)
	}

	fileName := filepath.Base(envPath)
	result := &PushResult{
		FileName: fileName,
		FileSize: len(data),
	}

	// Count variables using parser for accuracy (not raw '=' counting)
	if parsed, parseErr := envfile.Parse(string(data)); parseErr == nil {
		result.VarCount = parsed.VariableCount()
	}

	// Create wire protocol payload
	payload := NewEnvPayload(fileName, data, opts.Sequence)
	encodedPayload, err := EncodeEnvPayload(payload)
	if err != nil {
		return result, fmt.Errorf("encoding sync payload: %w", err)
	}

	// Discover peers via mDNS
	peers, err := discovery.Discover(ctx, discovery.DefaultMDNSTimeout, crypto.ComputeFingerprint(opts.KeyPair.X25519Public))
	if err != nil {
		return result, fmt.Errorf("peer discovery: %w", err)
	}

	result.PeersFound = len(peers)

	if len(peers) == 0 {
		return result, fmt.Errorf("no peers found on LAN. Ensure the recipient is running 'envsync pull'")
	}

	// Push to each peer
	for _, peer := range peers {
		if opts.TeamID != "" && peer.TeamID != opts.TeamID {
			continue
		}

		if opts.OnPeerFound != nil {
			opts.OnPeerFound(peer)
		}

		// Connect + Noise handshake
		secureConn, err := transport.Dial(transport.DialOptions{
			Address:             peer.Addr.String(),
			Timeout:             transport.DefaultDialTimeout,
			LocalKeypair:        opts.NoiseKeypair,
			ExpectedFingerprint: peer.Fingerprint,
		})
		if err != nil {
			if opts.OnError != nil {
				opts.OnError(peer, err)
			}
			result.Errors = append(result.Errors, err)
			continue
		}
		// Do NOT defer in loop — close explicitly after we're done with this peer
		// defer secureConn.Close() would hold all connections open until function return

		if opts.OnHandshake != nil {
			var pk [32]byte
			copy(pk[:], secureConn.RemotePublicKey())
			opts.OnHandshake(crypto.ComputeFingerprint(pk))
		}

		// Send ENV_PUSH message
		err = SendMessage(secureConn, Message{
			Type:    MsgEnvPush,
			Payload: encodedPayload,
		})
		if err != nil {
			if opts.OnError != nil {
				opts.OnError(peer, err)
			}
			result.Errors = append(result.Errors, err)
			if closeErr := secureConn.Close(); closeErr != nil {
				result.Errors = append(result.Errors, fmt.Errorf("closing connection to %s: %w", peer.Fingerprint, closeErr))
			}
			continue
		}

		// Wait for ACK/NACK with timeout to prevent infinite blocking
		if err := secureConn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			if opts.OnError != nil {
				opts.OnError(peer, err)
			}
			result.Errors = append(result.Errors, err)
			if closeErr := secureConn.Close(); closeErr != nil {
				result.Errors = append(result.Errors, fmt.Errorf("closing connection to %s: %w", peer.Fingerprint, closeErr))
			}
			continue
		}
		resp, err := ReceiveMessage(secureConn)
		if err != nil {
			if opts.OnError != nil {
				opts.OnError(peer, err)
			}
			result.Errors = append(result.Errors, err)
			if closeErr := secureConn.Close(); closeErr != nil {
				result.Errors = append(result.Errors, fmt.Errorf("closing connection to %s: %w", peer.Fingerprint, closeErr))
			}
			continue
		}

		if resp.Type == MsgNack {
			err := fmt.Errorf("peer rejected push: %s", string(resp.Payload))
			if opts.OnError != nil {
				opts.OnError(peer, err)
			}
			result.Errors = append(result.Errors, err)
			if closeErr := secureConn.Close(); closeErr != nil {
				result.Errors = append(result.Errors, fmt.Errorf("closing connection to %s: %w", peer.Fingerprint, closeErr))
			}
			continue
		}

		result.PeersSynced++
		if opts.OnComplete != nil {
			opts.OnComplete(peer)
		}
		if closeErr := secureConn.Close(); closeErr != nil {
			result.Errors = append(result.Errors, fmt.Errorf("closing connection to %s: %w", peer.Fingerprint, closeErr))
		}
	}

	return result, nil
}
