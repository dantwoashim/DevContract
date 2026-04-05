// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package transport

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/relay"
	"github.com/flynn/noise"
)

// HolePunchOptions configures a hole-punch attempt.
type HolePunchOptions struct {
	Signal       *relay.SignalClient
	LocalKeypair noise.DHKey
	KeyPair      *crypto.KeyPair
	LocalPort    int
	Timeout      time.Duration
	VerifyPeer   func(publicKey []byte) error // Peer verification callback
}

// HolePunch attempts to establish a direct TCP connection through NAT.
func HolePunch(ctx context.Context, opts HolePunchOptions) (*crypto.SecureConn, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	if opts.LocalPort == 0 {
		opts.LocalPort = 7733
	}

	localEndpoint := relay.PeerEndpoint{
		Fingerprint: opts.KeyPair.Fingerprint,
		Port:        opts.LocalPort,
		NATType:     "unknown",
	}

	// Use STUN to discover NAT type and public IP
	stunResult, err := DiscoverNAT()
	if err == nil {
		localEndpoint.PublicIP = stunResult.PublicIP.String()
		localEndpoint.NATType = stunResult.NATType.String()

		// If our NAT is symmetric, hole-punching will almost certainly fail.
		if stunResult.NATType == NATSymmetric {
			return nil, fmt.Errorf("symmetric NAT detected — hole-punch unlikely to succeed, use relay")
		}
	} else {
		// Fallback: detect local IP via UDP trick
		publicIP, err := detectPublicIP(ctx)
		if err == nil {
			localEndpoint.PublicIP = publicIP
		} else {
			localEndpoint.PublicIP = "0.0.0.0"
		}
	}

	peerEndpoint, err := opts.Signal.ExchangeEndpoints(localEndpoint, opts.Timeout)
	if err != nil {
		return nil, fmt.Errorf("signaling: %w", err)
	}

	peerAddr := fmt.Sprintf("%s:%d", peerEndpoint.PublicIP, peerEndpoint.Port)

	localAddr := &net.TCPAddr{Port: opts.LocalPort}

	dialer := &net.Dialer{
		LocalAddr: localAddr,
		Timeout:   opts.Timeout,
		Control:   reuseControl,
	}

	connCh := make(chan net.Conn, 2)
	errCh := make(chan error, 2)

	// Goroutine 1: dial the peer
	go func() {
		conn, err := dialer.DialContext(ctx, "tcp", peerAddr)
		if err != nil {
			errCh <- err
			return
		}
		connCh <- conn
	}()

	// Goroutine 2: listen for incoming
	go func() {
		listener, err := net.ListenTCP("tcp", localAddr)
		if err != nil {
			errCh <- err
			return
		}
		defer listener.Close()
		if err := listener.SetDeadline(time.Now().Add(opts.Timeout)); err != nil {
			errCh <- fmt.Errorf("setting listener deadline: %w", err)
			return
		}
		conn, err := listener.Accept()
		if err != nil {
			errCh <- err
			return
		}
		connCh <- conn
	}()

	timer := time.NewTimer(opts.Timeout)
	defer timer.Stop()

	var rawConn net.Conn
	for i := 0; i < 2; i++ {
		select {
		case conn := <-connCh:
			rawConn = conn
			goto connected
		case <-errCh:
			continue
		case <-timer.C:
			return nil, fmt.Errorf("hole-punch timed out after %s", opts.Timeout)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("both hole-punch attempts failed")

connected:
	// Perform Noise_XX handshake over the punched connection
	secureConn, err := crypto.PerformHandshake(rawConn, crypto.NoiseConfig{
		StaticKeypair: opts.LocalKeypair,
		IsInitiator:   true,
		VerifyPeer:    opts.VerifyPeer,
	})
	if err != nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("noise handshake over punched conn: %w", err)
	}

	return secureConn, nil
}

// detectPublicIP tries to discover our public IP address.
func detectPublicIP(ctx context.Context) (string, error) {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}
