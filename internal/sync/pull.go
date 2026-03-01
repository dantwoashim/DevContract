// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/envfile"
	"github.com/envsync/envsync/internal/peer"
	"github.com/envsync/envsync/internal/transport"
	"github.com/flynn/noise"
)

// PullOptions configures a pull operation.
type PullOptions struct {
	// EnvFilePath is where to write the received .env file.
	EnvFilePath string

	// Port to listen on for incoming connections.
	Port int

	// KeyPair is the local identity.
	KeyPair *crypto.KeyPair

	// NoiseKeypair derived from KeyPair.
	NoiseKeypair noise.DHKey

	// ConfirmBeforeApply prompts before overwriting.
	ConfirmBeforeApply bool

	// OnListening is called when the listener is ready.
	OnListening func(port int)

	// OnReceived is called when data is received and verified.
	OnReceived func(payload EnvPayload, diff *envfile.DiffResult)

	// OnConfirm is called to ask for user confirmation. Return true to apply.
	OnConfirm func(diff *envfile.DiffResult) bool

	// OnApplied is called after the file is written.
	OnApplied func(fileName string)
}

// PullResult summarizes the pull operation.
type PullResult struct {
	FileName    string
	FileSize    int
	VarCount    int
	Applied     bool
	DiffSummary string
}

// Pull listens for an incoming push and applies the received .env file.
func Pull(ctx context.Context, opts PullOptions) (*PullResult, error) {
	port := opts.Port
	if port == 0 {
		port = config.DefaultPort
	}

	result := &PullResult{}

	// Start listener
	listener, err := transport.Listen(transport.ListenerOptions{
		Port:         port,
		LocalKeypair: opts.NoiseKeypair,
		VerifyPeer: func(publicKey []byte) error {
			// Verify against trust registry — reject revoked peers
			if len(publicKey) != 32 {
				return fmt.Errorf("invalid public key length: %d", len(publicKey))
			}
			var pk [32]byte
			copy(pk[:], publicKey)
			fp := crypto.ComputeFingerprint(pk)

			// Load registry and check trust across all teams
			reg, err := peer.NewRegistry()
			if err != nil {
				// If registry can't be loaded, allow (first use)
				return nil
			}

			teams, err := reg.ListTeams()
			if err != nil || len(teams) == 0 {
				// No teams yet — TOFU: accept first connection
				return nil
			}

			// Search all teams for this peer
			for _, teamID := range teams {
				p, err := reg.LoadPeer(teamID, fp)
				if err == nil {
					if !p.CanSync() {
						return fmt.Errorf("peer %s is not trusted (status: %s)", fp[:12], p.Trust)
					}
					return nil // Found and trusted
				}
			}

			// Unknown peer — reject: they must be invited first
			return fmt.Errorf("unknown peer %s — use 'envsync invite @username' to add them", fp[:12])
		},
	})
	if err != nil {
		return nil, fmt.Errorf("starting listener: %w", err)
	}
	defer listener.Close()

	if opts.OnListening != nil {
		addr := listener.Addr()
		if tcpAddr, ok := addr.(*net.TCPAddr); ok {
			opts.OnListening(tcpAddr.Port)
		} else {
			opts.OnListening(port)
		}
	}

	// Wait for a connection
	conn, err := listener.Accept(ctx)
	if err != nil {
		return nil, fmt.Errorf("waiting for connection: %w", err)
	}
	defer conn.Close()

	// Receive message
	msg, err := ReceiveMessage(conn)
	if err != nil {
		return nil, fmt.Errorf("receiving message: %w", err)
	}

	if msg.Type != MsgEnvPush {
		// Send NACK
		SendMessage(conn, Message{Type: MsgNack, Payload: []byte("expected ENV_PUSH")})
		return nil, fmt.Errorf("unexpected message type: 0x%02x", msg.Type)
	}

	// Decode payload
	payload, err := DecodeEnvPayload(msg.Payload)
	if err != nil {
		SendMessage(conn, Message{Type: MsgNack, Payload: []byte("invalid payload")})
		return nil, fmt.Errorf("decoding payload: %w", err)
	}

	// Verify checksum
	actualChecksum := sha256.Sum256(payload.Data)
	if actualChecksum != payload.Checksum {
		SendMessage(conn, Message{Type: MsgNack, Payload: []byte("checksum mismatch")})
		return nil, fmt.Errorf("data checksum mismatch — possible corruption")
	}

	// Validate sequence: reject replays (use hex-encoded remote PK as stable peer ID)
	peerID := hex.EncodeToString(conn.RemotePublicKey())
	lastSeq := loadLastSequence([]byte(peerID))
	if payload.Sequence <= lastSeq {
		SendMessage(conn, Message{Type: MsgNack, Payload: []byte("replayed sequence number")})
		return nil, fmt.Errorf("replay detected: sequence %d ≤ last seen %d from peer", payload.Sequence, lastSeq)
	}

	// Validate timestamp: reject payloads older than 72 hours
	if payload.Timestamp > 0 {
		age := time.Now().Unix() - payload.Timestamp
		if age > 72*3600 {
			SendMessage(conn, Message{Type: MsgNack, Payload: []byte("payload expired (>72h old)")})
			return nil, fmt.Errorf("payload timestamp too old: %ds ago", age)
		}
		if age < -300 { // 5 min clock skew tolerance
			SendMessage(conn, Message{Type: MsgNack, Payload: []byte("payload timestamp in the future")})
			return nil, fmt.Errorf("payload timestamp in the future by %ds", -age)
		}
	}

	result.FileName = payload.FileName
	result.FileSize = len(payload.Data)

	// Parse received env
	receivedEnv, err := envfile.Parse(string(payload.Data))
	if err != nil {
		SendMessage(conn, Message{Type: MsgNack, Payload: []byte("invalid .env format")})
		return nil, fmt.Errorf("parsing received .env: %w", err)
	}
	result.VarCount = receivedEnv.VariableCount()

	// Compute diff against local file
	envPath := opts.EnvFilePath
	if envPath == "" {
		envPath = ".env"
	}

	var diff *envfile.DiffResult
	localData, err := os.ReadFile(envPath)
	if err == nil {
		localEnv, parseErr := envfile.Parse(string(localData))
		if parseErr == nil {
			diff = envfile.Diff(localEnv, receivedEnv)
		}
	}
	// If local file doesn't exist, all vars are "added"

	if opts.OnReceived != nil {
		opts.OnReceived(payload, diff)
	}

	if diff != nil {
		result.DiffSummary = diff.Summary()
	}

	// Confirm before apply
	if opts.ConfirmBeforeApply && diff != nil && diff.HasChanges() {
		if opts.OnConfirm != nil {
			if !opts.OnConfirm(diff) {
				SendMessage(conn, Message{Type: MsgNack, Payload: []byte("user rejected changes")})
				result.Applied = false
				return result, nil
			}
		}
	}

	// Write the file
	if err := os.WriteFile(envPath, payload.Data, 0600); err != nil {
		SendMessage(conn, Message{Type: MsgNack, Payload: []byte("failed to write file")})
		return nil, fmt.Errorf("writing %s: %w", envPath, err)
	}

	// Send ACK
	SendMessage(conn, Message{Type: MsgAck})

	result.Applied = true

	// Persist the sequence number for this peer to prevent future replays
	saveLastSequence([]byte(peerID), payload.Sequence)

	if opts.OnApplied != nil {
		opts.OnApplied(envPath)
	}

	return result, nil
}

// loadLastSequence reads the last-seen sequence number for a peer from disk.
func loadLastSequence(peerPK []byte) int64 {
	dataDir, err := config.DataDir()
	if err != nil {
		return 0
	}
	path := filepath.Join(dataDir, "sequences", hex.EncodeToString(peerPK)+".seq")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	seq, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return 0
	}
	return seq
}

// saveLastSequence persists the last-seen sequence number for a peer to disk.
func saveLastSequence(peerPK []byte, seq int64) {
	dataDir, err := config.DataDir()
	if err != nil {
		return
	}
	dir := filepath.Join(dataDir, "sequences")
	os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, hex.EncodeToString(peerPK)+".seq")
	os.WriteFile(path, []byte(strconv.FormatInt(seq, 10)), 0600)
}
