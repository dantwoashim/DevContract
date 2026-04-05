// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package crypto

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/flynn/noise"
)

func generateNoiseKeypairForTest(t *testing.T) noise.DHKey {
	t.Helper()
	cs := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)
	kp, err := cs.GenerateKeypair(nil)
	if err != nil {
		t.Fatalf("generating noise keypair: %v", err)
	}
	return kp
}

func TestNoiseHandshakeRoundTrip(t *testing.T) {
	aliceKP := generateNoiseKeypairForTest(t)
	bobKP := generateNoiseKeypairForTest(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	var wg sync.WaitGroup
	var serverConn *SecureConn
	var serverErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		raw, err := ln.Accept()
		if err != nil {
			serverErr = err
			return
		}
		serverConn, serverErr = PerformHandshake(raw, NoiseConfig{
			StaticKeypair: bobKP,
			IsInitiator:   false,
		})
	}()

	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	clientConn, err := PerformHandshake(raw, NoiseConfig{
		StaticKeypair: aliceKP,
		IsInitiator:   true,
	})
	if err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	defer clientConn.Close()

	// Wait for server goroutine to finish
	wg.Wait()

	if serverErr != nil {
		t.Fatalf("server handshake: %v", serverErr)
	}
	if serverConn == nil {
		t.Fatal("server conn is nil")
	}
	defer serverConn.Close()

	// Verify remote public keys
	if len(clientConn.RemotePublicKey()) != 32 {
		t.Errorf("client remote PK len = %d, want 32", len(clientConn.RemotePublicKey()))
	}
	if len(serverConn.RemotePublicKey()) != 32 {
		t.Errorf("server remote PK len = %d, want 32", len(serverConn.RemotePublicKey()))
	}

	// Test bidirectional messaging
	testMessage := []byte("hello from alice")
	if err := clientConn.Send(testMessage); err != nil {
		t.Fatalf("client send: %v", err)
	}

	received, err := serverConn.Receive()
	if err != nil {
		t.Fatalf("server receive: %v", err)
	}
	if string(received) != string(testMessage) {
		t.Errorf("received = %q, want %q", received, testMessage)
	}

	// Reverse direction
	testReply := []byte("hello from bob")
	if err := serverConn.Send(testReply); err != nil {
		t.Fatalf("server send: %v", err)
	}

	reply, err := clientConn.Receive()
	if err != nil {
		t.Fatalf("client receive: %v", err)
	}
	if string(reply) != string(testReply) {
		t.Errorf("reply = %q, want %q", reply, testReply)
	}
}

func TestNoiseHandshakeVerifyPeerCalled(t *testing.T) {
	aliceKP := generateNoiseKeypairForTest(t)
	bobKP := generateNoiseKeypairForTest(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	var verified bool
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		raw, _ := ln.Accept()
		conn, _ := PerformHandshake(raw, NoiseConfig{
			StaticKeypair: bobKP,
			IsInitiator:   false,
			VerifyPeer: func(pk []byte) error {
				verified = true
				return nil
			},
		})
		if conn != nil {
			conn.Close()
		}
	}()

	raw, _ := net.Dial("tcp", ln.Addr().String())
	conn, err := PerformHandshake(raw, NoiseConfig{
		StaticKeypair: aliceKP,
		IsInitiator:   true,
	})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	conn.Close()
	wg.Wait()

	if !verified {
		t.Error("server's VerifyPeer was not called")
	}
}

func TestNoiseMultipleMessages(t *testing.T) {
	aliceKP := generateNoiseKeypairForTest(t)
	bobKP := generateNoiseKeypairForTest(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	var wg sync.WaitGroup
	serverDone := make(chan *SecureConn, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		raw, _ := ln.Accept()
		conn, _ := PerformHandshake(raw, NoiseConfig{
			StaticKeypair: bobKP,
			IsInitiator:   false,
		})
		serverDone <- conn
	}()

	raw, _ := net.Dial("tcp", ln.Addr().String())
	client, err := PerformHandshake(raw, NoiseConfig{
		StaticKeypair: aliceKP,
		IsInitiator:   true,
	})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	defer client.Close()

	server := <-serverDone
	if server == nil {
		t.Fatal("server conn nil")
	}
	defer server.Close()

	// Send 10 messages in each direction
	for i := 0; i < 10; i++ {
		msg := []byte("message")
		if err := client.Send(msg); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
		received, err := server.Receive()
		if err != nil {
			t.Fatalf("receive %d: %v", i, err)
		}
		if string(received) != string(msg) {
			t.Errorf("msg %d: got %q want %q", i, received, msg)
		}
	}
}

func TestNewNoiseKeypair(t *testing.T) {
	var priv, pub [32]byte
	copy(priv[:], []byte("12345678901234567890123456789012"))
	copy(pub[:], []byte("abcdefghijklmnopqrstuvwxyz123456"))

	kp := NewNoiseKeypair(priv, pub)
	if len(kp.Private) != 32 {
		t.Errorf("private len = %d, want 32", len(kp.Private))
	}
	if len(kp.Public) != 32 {
		t.Errorf("public len = %d, want 32", len(kp.Public))
	}
}

// TestNoiseHandshakeTimeout verifies connections with immediate close
func TestNoiseHandshakeTimeout(t *testing.T) {
	aliceKP := generateNoiseKeypairForTest(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Immediately close the accepted connection
	go func() {
		raw, _ := ln.Accept()
		if raw != nil {
			_ = raw.Close()
		}
	}()

	raw, _ := net.Dial("tcp", ln.Addr().String())
	if err := raw.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}

	_, err = PerformHandshake(raw, NoiseConfig{
		StaticKeypair: aliceKP,
		IsInitiator:   true,
	})
	if err == nil {
		t.Error("expected error when peer closes immediately")
	}
}
