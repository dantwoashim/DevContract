// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

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
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/dantwoashim/devcontract/internal/apply"
	"github.com/dantwoashim/devcontract/internal/config"
	"github.com/dantwoashim/devcontract/internal/crypto"
	"github.com/dantwoashim/devcontract/internal/discovery"
	"github.com/dantwoashim/devcontract/internal/envfile"
	"github.com/dantwoashim/devcontract/internal/fsutil"
	"github.com/dantwoashim/devcontract/internal/peer"
	"github.com/dantwoashim/devcontract/internal/transport"
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
	ConflictPolicy     apply.Policy
	Interactive        bool
	ProjectID          string
	BackupEnabled      bool
	BackupKey          [32]byte
	MaxVersions        int
	Advertise          bool
	AdvertiseVersion   string

	OnListening        func(port int)
	OnReceived         func(payload EnvPayload, diff *envfile.DiffResult)
	OnConfirm          func(diff *envfile.DiffResult) bool
	OnResolveConflicts func(conflicts []envfile.Conflict) ([]apply.ConflictResolution, bool)
	OnApplied          func(fileName string)
}

// PullResult summarizes the pull operation.
type PullResult struct {
	FileName                 string
	FileSize                 int
	VarCount                 int
	Applied                  bool
	DiffSummary              string
	ConflictPolicyApplied    string
	BackupCreated            bool
	InteractiveRequired      bool
	ManualInterventionNeeded bool
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

	applyResult, err := apply.Apply(apply.Options{
		ProjectID:           opts.ProjectID,
		TargetFile:          envPath,
		IncomingFile:        payload.FileName,
		IncomingData:        payload.Data,
		BaseRevisionID:      payload.BaseRevisionID,
		AncestorRevisionIDs: payload.AncestorRevisionIDs,
		NewRevisionID:       payload.RevisionID,
		Policy:              normalizePullPolicy(opts),
		Interactive:         opts.Interactive,
		BackupEnabled:       opts.BackupEnabled,
		BackupKey:           opts.BackupKey,
		MaxVersions:         opts.MaxVersions,
		ConfirmApply:        opts.OnConfirm,
		ResolveConflicts:    opts.OnResolveConflicts,
	})
	if err != nil {
		_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte(err.Error())})
		return nil, err
	}

	result.Applied = applyResult.Applied
	result.ConflictPolicyApplied = applyResult.ConflictPolicyApplied
	result.BackupCreated = applyResult.BackupCreated
	result.InteractiveRequired = applyResult.InteractiveRequired
	result.ManualInterventionNeeded = applyResult.ManualInterventionNeeded
	if result.Applied || (applyResult.Diff != nil && !applyResult.Diff.HasChanges()) {
		_ = SendMessage(conn, Message{Type: MsgAck})
	} else {
		_ = SendMessage(conn, Message{Type: MsgNack, Payload: []byte("incoming data was not applied")})
	}
	saveLastSequence([]byte(peerID), payload.Sequence)

	if result.Applied && opts.OnApplied != nil {
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
	return withSequenceLock(lockPath, func() int64 {
		return readSequenceFile(path)
	})
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
	withSequenceLock(lockPath, func() int64 {
		_ = fsutil.AtomicWriteFile(path, []byte(strconv.FormatInt(seq, 10)), 0600)
		return 0
	})
}

func normalizePullPolicy(opts PullOptions) apply.Policy {
	if opts.ConflictPolicy != "" {
		return opts.ConflictPolicy
	}
	if opts.ConfirmBeforeApply {
		return apply.PolicyInteractive
	}
	return apply.PolicyOverwrite
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

func withSequenceLock(lockPath string, fn func() int64) int64 {
	deadline := time.Now().Add(2 * time.Second)
	for {
		lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			_ = lockFile.Close()
			defer os.Remove(lockPath)
			return fn()
		}
		if !isSequenceLockBusy(err) {
			return fn()
		}
		if time.Now().After(deadline) {
			return fn()
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func isSequenceLockBusy(err error) bool {
	if err == nil {
		return false
	}
	if os.IsExist(err) || os.IsPermission(err) {
		return true
	}

	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		if errno, ok := pathErr.Err.(syscall.Errno); ok && runtime.GOOS == "windows" {
			switch errno {
			case 5, 32, 33:
				return true
			}
		}
	}
	return false
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
		return errors.New("no trusted projects configured for LAN pull; run 'devcontract invite' or 'devcontract join' first")
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
			return fmt.Errorf("no trusted peers configured for project %s; run 'devcontract invite' or 'devcontract join' first", teamID)
		}
		return errors.New("no trusted peers configured for LAN pull; run 'devcontract invite' or 'devcontract join' first")
	}

	return fmt.Errorf("unknown peer transport key - use 'devcontract invite' or 'devcontract join' first")
}
