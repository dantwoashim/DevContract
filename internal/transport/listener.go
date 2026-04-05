// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package transport

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/envsync/envsync/internal/crypto"
	"github.com/flynn/noise"
)

// Listener accepts incoming encrypted connections.
type Listener struct {
	listener     net.Listener
	localKeypair noise.DHKey
	verifyPeer   func(publicKey []byte) error
	connections  chan *crypto.SecureConn
	errors       chan error
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// ListenerOptions configures the TCP listener.
type ListenerOptions struct {
	// Port to bind to.
	Port int

	// LocalKeypair is our Noise static keypair.
	LocalKeypair noise.DHKey

	// VerifyPeer validates incoming connections.
	VerifyPeer func(publicKey []byte) error
}

// Listen starts accepting TCP connections on the given port.
func Listen(opts ListenerOptions) (*Listener, error) {
	addr := fmt.Sprintf(":%d", opts.Port)
	tcpListener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("binding to %s: %w", addr, err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	l := &Listener{
		listener:     tcpListener,
		localKeypair: opts.LocalKeypair,
		verifyPeer:   opts.VerifyPeer,
		connections:  make(chan *crypto.SecureConn, 8),
		errors:       make(chan error, 8),
		ctx:          ctx,
		cancel:       cancel,
	}

	l.wg.Add(1)
	go l.acceptLoop()

	return l, nil
}

// Accept returns the next authenticated connection, or blocks until one arrives.
func (l *Listener) Accept(ctx context.Context) (*crypto.SecureConn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case err := <-l.errors:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.ctx.Done():
		return nil, fmt.Errorf("listener closed")
	}
}

// Addr returns the listener's network address.
func (l *Listener) Addr() net.Addr {
	return l.listener.Addr()
}

// Close stops the listener and cleans up.
func (l *Listener) Close() error {
	l.cancel()
	err := l.listener.Close()
	l.wg.Wait()
	close(l.connections)
	close(l.errors)
	return err
}

func (l *Listener) acceptLoop() {
	defer l.wg.Done()

	for {
		// Set a short accept deadline so we can check for cancellation
		if tcpL, ok := l.listener.(*net.TCPListener); ok {
			if err := tcpL.SetDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
				select {
				case l.errors <- fmt.Errorf("setting accept deadline: %w", err):
				case <-l.ctx.Done():
				}
				return
			}
		}

		conn, err := l.listener.Accept()
		if err != nil {
			// Check if we're shutting down
			select {
			case <-l.ctx.Done():
				return
			default:
			}

			// Timeout is normal, keep looping
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}

			l.errors <- fmt.Errorf("accept error: %w", err)
			continue
		}

		// Handle connection in a goroutine
		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			l.handleConnection(conn)
		}()
	}
}

func (l *Listener) handleConnection(conn net.Conn) {
	// Set handshake timeout
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		_ = conn.Close()
		select {
		case l.errors <- fmt.Errorf("setting handshake deadline: %w", err):
		case <-l.ctx.Done():
		}
		return
	}

	// Noise_XX handshake (responder)
	secureConn, err := crypto.PerformHandshake(conn, crypto.NoiseConfig{
		StaticKeypair: l.localKeypair,
		IsInitiator:   false,
		VerifyPeer: func(publicKey []byte) error {
			if l.verifyPeer != nil {
				return l.verifyPeer(publicKey)
			}
			return nil
		},
	})
	if err != nil {
		_ = conn.Close()
		l.errors <- fmt.Errorf("handshake failed from %s: %w", conn.RemoteAddr(), err)
		return
	}

	// Clear deadline
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = secureConn.Close()
		select {
		case l.errors <- fmt.Errorf("clearing handshake deadline: %w", err):
		case <-l.ctx.Done():
		}
		return
	}

	select {
	case l.connections <- secureConn:
	case <-l.ctx.Done():
		_ = secureConn.Close()
	}
}
