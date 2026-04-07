// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package discovery

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

const (
	// ServiceName is the mDNS service name for EnvSync peers.
	ServiceName = "_envsync._tcp"

	// DefaultMDNSTimeout is how long to scan for peers.
	DefaultMDNSTimeout = 2 * time.Second
)

// Peer represents a discovered peer on the LAN.
type Peer struct {
	// Name is the mDNS instance name.
	Name string

	// Addr is the peer's IP + port.
	Addr *net.TCPAddr

	// Fingerprint is the peer's SSH key fingerprint.
	Fingerprint string

	// TeamID is the team this peer belongs to.
	TeamID string

	// Version is the EnvSync version the peer is running.
	Version string
}

// Advertiser manages mDNS service advertisement.
type Advertiser struct {
	server *mdns.Server
	port   int
	mu     sync.Mutex
}

// NewAdvertiser creates a new mDNS advertiser.
func NewAdvertiser(port int, fingerprint, teamID, version string) (*Advertiser, error) {
	// Build TXT records
	txt := []string{
		"fp=" + fingerprint,
		"team=" + teamID,
		"ver=" + version,
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "envsync"
	}

	// Create mDNS service entry
	service, err := mdns.NewMDNSService(
		hostname,    // instance name
		ServiceName, // service
		"",          // domain (default .local)
		"",          // host name
		port,        // port
		nil,         // IPs (auto-detect)
		txt,         // TXT records
	)
	if err != nil {
		return nil, fmt.Errorf("creating mDNS service: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		return nil, fmt.Errorf("starting mDNS server: %w", err)
	}

	return &Advertiser{
		server: server,
		port:   port,
	}, nil
}

// Stop stops the mDNS advertisement.
func (a *Advertiser) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server != nil {
		_ = a.server.Shutdown()
		a.server = nil
	}
}

// Discover scans the LAN for EnvSync peers.
// Returns all discovered peers within the timeout.
func Discover(ctx context.Context, timeout time.Duration, ownFingerprint string) ([]Peer, error) {
	if timeout == 0 {
		timeout = DefaultMDNSTimeout
	}

	entryCh := make(chan *mdns.ServiceEntry, 16)
	var peers []Peer
	var mu sync.Mutex
	seen := map[string]struct{}{}

	// Collect entries in background
	done := make(chan struct{})
	go func() {
		defer close(done)
		for entry := range entryCh {
			peer := entryToPeer(entry)
			if peer != nil && peer.Fingerprint != ownFingerprint {
				key := peer.Fingerprint + "|" + peer.TeamID + "|" + peer.Addr.String()
				mu.Lock()
				if _, ok := seen[key]; ok {
					mu.Unlock()
					continue
				}
				seen[key] = struct{}{}
				peers = append(peers, *peer)
				mu.Unlock()
			}
		}
	}()

	// Start the query
	params := mdns.DefaultParams(ServiceName)
	params.Entries = entryCh
	params.Timeout = timeout
	params.DisableIPv6 = false

	// Use context for cancellation
	if deadline, ok := ctx.Deadline(); ok {
		params.Timeout = time.Until(deadline)
	}

	err := mdns.Query(params)
	close(entryCh)
	<-done

	if err != nil {
		return peers, fmt.Errorf("mDNS query failed: %w", err)
	}

	return peers, nil
}

// entryToPeer converts an mDNS service entry to a Peer.
func entryToPeer(entry *mdns.ServiceEntry) *Peer {
	if entry == nil {
		return nil
	}

	peer := &Peer{
		Name: entry.Name,
	}

	// Determine IP (prefer IPv4)
	ip := entry.AddrV4
	if ip == nil {
		ip = entry.AddrV6
	}
	if ip == nil {
		return nil
	}

	peer.Addr = &net.TCPAddr{
		IP:   ip,
		Port: entry.Port,
	}

	// Parse TXT records
	for _, txt := range entry.InfoFields {
		parts := strings.SplitN(txt, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "fp":
			peer.Fingerprint = parts[1]
		case "team":
			peer.TeamID = parts[1]
		case "ver":
			peer.Version = parts[1]
		}
	}

	if peer.Fingerprint == "" || peer.TeamID == "" {
		return nil
	}

	return peer
}

// DiscoverOne scans until one peer on the specified team is found, or times out.
func DiscoverOne(ctx context.Context, timeout time.Duration, teamID, ownFingerprint string) (*Peer, error) {
	peers, err := Discover(ctx, timeout, ownFingerprint)
	if err != nil {
		return nil, err
	}

	for _, p := range peers {
		if teamID == "" || p.TeamID == teamID {
			return &p, nil
		}
	}

	return nil, fmt.Errorf("no peers found for team %q within %s", teamID, timeout)
}
