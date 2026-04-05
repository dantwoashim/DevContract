// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/discovery"
	"github.com/envsync/envsync/internal/envfile"
	"github.com/envsync/envsync/internal/peer"
	"github.com/envsync/envsync/internal/store"
	"github.com/envsync/envsync/internal/transport"
	"github.com/flynn/noise"
)

type pullPeerRegistry interface {
	ListTeams() ([]string, error)
	ListPeers(teamID string) ([]peer.Peer, error)
}

var newPullPeerRegistry = func() (pullPeerRegistry, error) {
	return peer.NewRegistry()
}

// PullOptions configures a pull operation.
type PullOptions struct {
	EnvFilePath string
	Port        int
	TeamID      string

	KeyPair      *crypto.KeyPair
	NoiseKeypair noise.DHKey

	ConfirmBeforeApply bool
	ProjectID          string
	BackupEnabled      bool
	BackupKey          [32]byte
	MaxVersions        int
	Advertise          bool
	AdvertiseVersion   string

	OnListening func(port int)
	OnReceived  func(payload EnvPayload, diff *envfile.DiffResult)
	OnConfirm   func(diff *envfile.DiffResult) bool
	OnApplied   func(fileName string)
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

	listener, err := transport.Listen(transport.ListenerOptions{
		Port:         port,
		LocalKeypair: opts.NoiseKeypair,
		VerifyPeer: func(publicKey []byte) error {
			return verifyTrustedPullPeer(publicKey, opts.TeamID)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("starting listener: %w", err)
	}
	defer listener.Close()

	var advertiser *discovery.Advertiser
	if opts.Advertise && opts.TeamID != "" {
		addr := listener.Addr()
		advertisePort := port
		if tcpAddr, ok := addr.(*net.TCPAddr); ok {
			advertisePort = tcpAddr.Port
		}

		advertiser, _ = discovery.NewAdvertiser(
			advertisePort,
			crypto.ComputeFingerprint(opts.KeyPair.X25519Public),
			opts.TeamID,
			opts.AdvertiseVersion,
		)
	}
	if advertiser != nil {
		defer advertiser.Stop()
	}

	if opts.OnListening != nil {
		addr := listener.Addr()
		if tcpAddr, ok := addr.(*net.TCPAddr); ok {
			opts.OnListening(tcpAddr.Port)
		} else {
			opts.OnListening(port)
		}
	}

	conn, err := listener.Accept(ctx)
	if err != nil {
		return nil, fmt.Errorf("waiting for connection: %w", err)
	}
	defer conn.Close()

	msg, err := ReceiveMessage(conn)
	if err != nil {
		return nil, fmt.Errorf("receiving message: %w", err)
	}

	if msg.Type != MsgEnvPush {
		_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("expected ENV_PUSH")})
		return nil, fmt.Errorf("unexpected message type: 0x%02x", msg.Type)
	}

	payload, err := DecodeEnvPayload(msg.Payload)
	if err != nil {
		_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("invalid payload")})
		return nil, fmt.Errorf("decoding payload: %w", err)
	}

	actualChecksum := sha256.Sum256(payload.Data)
	if actualChecksum != payload.Checksum {
		_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("checksum mismatch")})
		return nil, fmt.Errorf("data checksum mismatch")
	}

	peerID := hex.EncodeToString(conn.RemotePublicKey())
	lastSeq := loadLastSequence([]byte(peerID))
	if payload.Sequence <= lastSeq {
		_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("replayed sequence number")})
		return nil, fmt.Errorf("replay detected: sequence %d <= last seen %d from peer", payload.Sequence, lastSeq)
	}

	if payload.Timestamp > 0 {
		age := time.Now().Unix() - payload.Timestamp
		if age > 72*3600 {
			_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("payload expired (>72h old)")})
			return nil, fmt.Errorf("payload timestamp too old: %ds ago", age)
		}
		if age < -300 {
			_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("payload timestamp in the future")})
			return nil, fmt.Errorf("payload timestamp in the future by %ds", -age)
		}
	}

	result.FileName = payload.FileName
	result.FileSize = len(payload.Data)

	receivedEnv, err := envfile.Parse(string(payload.Data))
	if err != nil {
		_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("invalid .env format")})
		return nil, fmt.Errorf("parsing received .env: %w", err)
	}
	result.VarCount = receivedEnv.VariableCount()

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

	if opts.OnReceived != nil {
		opts.OnReceived(payload, diff)
	}
	if diff != nil {
		result.DiffSummary = diff.Summary()
	}

	if opts.ConfirmBeforeApply && diff != nil && diff.HasChanges() && opts.OnConfirm != nil {
		if !opts.OnConfirm(diff) {
			_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("user rejected changes")})
			result.Applied = false
			return result, nil
		}
	}

	if opts.BackupEnabled && len(localData) > 0 && opts.ProjectID != "" {
		maxVersions := opts.MaxVersions
		if maxVersions <= 0 {
			maxVersions = 10
		}
		vStore, err := store.New(maxVersions)
		if err != nil {
			_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("failed to create backup store")})
			return nil, fmt.Errorf("creating backup store: %w", err)
		}

		seq, err := vStore.NextSequence(opts.ProjectID)
		if err != nil {
			_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("failed to compute backup sequence")})
			return nil, fmt.Errorf("computing backup sequence: %w", err)
		}

		if err := vStore.Save(opts.ProjectID, localData, seq, opts.BackupKey); err != nil {
			_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("failed to create backup")})
			return nil, fmt.Errorf("creating pre-apply backup: %w", err)
		}
	}

	if err := os.WriteFile(envPath, payload.Data, 0600); err != nil {
		_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("failed to write file")})
		return nil, fmt.Errorf("writing %s: %w", envPath, err)
	}

	_ = SendMessage(conn, Message{Type: MsgAck})

	result.Applied = true
	saveLastSequence([]byte(peerID), payload.Sequence)

	if opts.OnApplied != nil {
		opts.OnApplied(envPath)
	}

	return result, nil
}

// loadLastSequence reads the last-seen sequence number for a peer from disk.
// Uses a lock file to prevent race conditions with concurrent pulls.
func loadLastSequence(peerPK []byte) int64 {
	dataDir, err := config.DataDir()
	if err != nil {
		return 0
	}
	dir := filepath.Join(dataDir, "sequences")
	_ = os.MkdirAll(dir, 0700)

	path := filepath.Join(dir, hex.EncodeToString(peerPK)+".seq")
	lockPath := path + ".lock"

	// Acquire lock
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		// Fallback: read without lock
		return readSequenceFile(path)
	}
	defer lock.Close()
	defer os.Remove(lockPath)

	return readSequenceFile(path)
}

// saveLastSequence persists the last-seen sequence number for a peer to disk.
// Uses a lock file to prevent race conditions with concurrent pulls.
func saveLastSequence(peerPK []byte, seq int64) {
	dataDir, err := config.DataDir()
	if err != nil {
		return
	}
	dir := filepath.Join(dataDir, "sequences")
	_ = os.MkdirAll(dir, 0700)

	path := filepath.Join(dir, hex.EncodeToString(peerPK)+".seq")
	lockPath := path + ".lock"

	// Acquire lock
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		// Fallback: write without lock
		_ = os.WriteFile(path, []byte(strconv.FormatInt(seq, 10)), 0600)
		return
	}
	defer lock.Close()
	defer os.Remove(lockPath)

	_ = os.WriteFile(path, []byte(strconv.FormatInt(seq, 10)), 0600)
}

// readSequenceFile reads a sequence number from a file path.
func readSequenceFile(path string) int64 {
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

func verifyTrustedPullPeer(publicKey []byte, teamID string) error {
	reg, err := newPullPeerRegistry()
	if err != nil {
		return fmt.Errorf("loading local trust registry: %w", err)
	}

	var teams []string
	if teamID != "" {
		teams = []string{teamID}
	} else {
		teams, err = reg.ListTeams()
		if err != nil {
			return fmt.Errorf("listing trusted projects: %w", err)
		}
	}

	if len(teams) == 0 {
		return errors.New("no trusted projects configured for LAN pull; run 'envsync invite' or 'envsync join' first")
	}

	hadPeers := false
	for _, trustedTeamID := range teams {
		peers, err := reg.ListPeers(trustedTeamID)
		if err != nil {
			return fmt.Errorf("loading trusted peers for %s: %w", trustedTeamID, err)
		}
		if len(peers) == 0 {
			continue
		}
		hadPeers = true

		for _, p := range peers {
			if !p.MatchesTransportPublicKey(publicKey) {
				continue
			}
			if !p.CanSync() {
				return fmt.Errorf("peer %s is not trusted (status: %s)", p.Fingerprint, p.Trust)
			}
			return nil
		}
	}

	if !hadPeers {
		if teamID != "" {
			return fmt.Errorf("no trusted peers configured for project %s; run 'envsync invite' or 'envsync join' first", teamID)
		}
		return errors.New("no trusted peers configured for LAN pull; run 'envsync invite' or 'envsync join' first")
	}

	return fmt.Errorf("unknown peer transport key - use 'envsync invite' or 'envsync join' first")
}
