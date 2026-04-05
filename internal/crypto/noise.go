// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package crypto

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"time"

	"github.com/flynn/noise"
)

// NoiseConfig holds configuration for a Noise_XX handshake.
type NoiseConfig struct {
	// StaticKeypair is the local peer's X25519 keypair (converted from Ed25519).
	StaticKeypair noise.DHKey

	// IsInitiator is true if this peer initiates the handshake.
	IsInitiator bool

	// VerifyPeer is called with the remote peer's static public key.
	// Return nil to accept, or an error to reject.
	VerifyPeer func(publicKey []byte) error
}

// SecureConn wraps a net.Conn with Noise protocol encryption.
type SecureConn struct {
	conn     net.Conn
	send     *noise.CipherState
	recv     *noise.CipherState
	remotePK []byte
}

// PerformHandshake performs a Noise_XX handshake over the given connection.
// Returns a SecureConn with established encrypted channels.
func PerformHandshake(conn net.Conn, cfg NoiseConfig) (*SecureConn, error) {
	// Configure the Noise handshake
	cs := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)

	handshakeConfig := noise.Config{
		CipherSuite:   cs,
		Pattern:       noise.HandshakeXX,
		Initiator:     cfg.IsInitiator,
		StaticKeypair: cfg.StaticKeypair,
	}

	hs, err := noise.NewHandshakeState(handshakeConfig)
	if err != nil {
		return nil, fmt.Errorf("creating Noise handshake state: %w", err)
	}

	var sendCS, recvCS *noise.CipherState
	var remotePK []byte

	if cfg.IsInitiator {
		// Message 1: → e
		msg1, _, _, err := hs.WriteMessage(nil, nil)
		if err != nil {
			return nil, fmt.Errorf("writing Noise message 1: %w", err)
		}
		if err := writeFrame(conn, msg1); err != nil {
			return nil, fmt.Errorf("sending Noise message 1: %w", err)
		}

		// Message 2: ← e, ee, s, es
		msg2, err := readFrame(conn)
		if err != nil {
			return nil, fmt.Errorf("receiving Noise message 2: %w", err)
		}
		_, _, _, err = hs.ReadMessage(nil, msg2)
		if err != nil {
			return nil, fmt.Errorf("reading Noise message 2: %w", err)
		}

		// Verify the remote peer's static key
		remotePK = hs.PeerStatic()
		if cfg.VerifyPeer != nil {
			if err := cfg.VerifyPeer(remotePK); err != nil {
				return nil, fmt.Errorf("peer verification failed: %w", err)
			}
		}

		// Message 3: → s, se
		var msg3 []byte
		msg3, sendCS, recvCS, err = hs.WriteMessage(nil, nil)
		if err != nil {
			return nil, fmt.Errorf("writing Noise message 3: %w", err)
		}
		if err := writeFrame(conn, msg3); err != nil {
			return nil, fmt.Errorf("sending Noise message 3: %w", err)
		}

	} else {
		// Message 1: ← e
		msg1, err := readFrame(conn)
		if err != nil {
			return nil, fmt.Errorf("receiving Noise message 1: %w", err)
		}
		_, _, _, err = hs.ReadMessage(nil, msg1)
		if err != nil {
			return nil, fmt.Errorf("reading Noise message 1: %w", err)
		}

		// Message 2: → e, ee, s, es
		msg2, _, _, err := hs.WriteMessage(nil, nil)
		if err != nil {
			return nil, fmt.Errorf("writing Noise message 2: %w", err)
		}
		if err := writeFrame(conn, msg2); err != nil {
			return nil, fmt.Errorf("sending Noise message 2: %w", err)
		}

		// Message 3: ← s, se
		msg3, err := readFrame(conn)
		if err != nil {
			return nil, fmt.Errorf("receiving Noise message 3: %w", err)
		}
		_, recvCS, sendCS, err = hs.ReadMessage(nil, msg3)
		if err != nil {
			return nil, fmt.Errorf("reading Noise message 3: %w", err)
		}

		// Verify the remote peer's static key
		remotePK = hs.PeerStatic()
		if cfg.VerifyPeer != nil {
			if err := cfg.VerifyPeer(remotePK); err != nil {
				return nil, fmt.Errorf("peer verification failed: %w", err)
			}
		}
	}

	return &SecureConn{
		conn:     conn,
		send:     sendCS,
		recv:     recvCS,
		remotePK: remotePK,
	}, nil
}

// RemotePublicKey returns the remote peer's static public key.
func (sc *SecureConn) RemotePublicKey() []byte {
	return sc.remotePK
}

// Send encrypts and sends a message over the secure connection.
func (sc *SecureConn) Send(plaintext []byte) error {
	ciphertext, err := sc.send.Encrypt(nil, nil, plaintext)
	if err != nil {
		return fmt.Errorf("encrypting message: %w", err)
	}
	return writeFrame(sc.conn, ciphertext)
}

// Receive reads and decrypts a message from the secure connection.
func (sc *SecureConn) Receive() ([]byte, error) {
	ciphertext, err := readFrame(sc.conn)
	if err != nil {
		return nil, fmt.Errorf("reading encrypted message: %w", err)
	}
	plaintext, err := sc.recv.Decrypt(nil, nil, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypting message: %w", err)
	}
	return plaintext, nil
}

// Close closes the underlying connection.
func (sc *SecureConn) Close() error {
	return sc.conn.Close()
}

// SetReadDeadline sets the read deadline on the underlying connection.
func (sc *SecureConn) SetReadDeadline(t time.Time) error {
	return sc.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline on the underlying connection.
func (sc *SecureConn) SetWriteDeadline(t time.Time) error {
	return sc.conn.SetWriteDeadline(t)
}

// writeFrame writes a length-prefixed frame to the connection.
// Enforces a maximum frame size of 65535 bytes (matching readFrame).
func writeFrame(conn net.Conn, data []byte) error {
	if len(data) > 65535 {
		return fmt.Errorf("frame too large to send: %d bytes (max 65535)", len(data))
	}
	// 4-byte big-endian length prefix
	lenBuf := make([]byte, 4)
	if len(data) > math.MaxUint32 {
		return fmt.Errorf("frame length overflows uint32: %d", len(data))
	}
	// #nosec G115 -- frame length is bounded by the checks above.
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))
	if _, err := conn.Write(lenBuf); err != nil {
		return err
	}
	_, err := conn.Write(data)
	return err
}

// readFrame reads a length-prefixed frame from the connection.
func readFrame(conn net.Conn) ([]byte, error) {
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(lenBuf)
	if length > 65535 {
		return nil, fmt.Errorf("frame too large: %d bytes (max 65535)", length)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, err
	}

	return data, nil
}

// NewNoiseKeypair creates a Noise DH keypair from X25519 keys.
func NewNoiseKeypair(privateKey, publicKey [32]byte) noise.DHKey {
	return noise.DHKey{
		Private: privateKey[:],
		Public:  publicKey[:],
	}
}
