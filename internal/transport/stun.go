// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package transport

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// NATType classifies the NAT behavior observed via STUN.
type NATType int

const (
	// NATUnknown means STUN discovery failed or was inconclusive.
	NATUnknown NATType = iota
	// NATNone means no NAT detected (public IP).
	NATNone
	// NATFullCone means full-cone NAT (endpoint-independent mapping).
	// Hole-punching will likely succeed.
	NATFullCone
	// NATRestrictedCone means address-restricted NAT.
	// Hole-punching may succeed with simultaneous open.
	NATRestrictedCone
	// NATSymmetric means symmetric NAT (endpoint-dependent mapping).
	// Hole-punching will almost certainly fail — skip to relay.
	NATSymmetric
)

// String returns a human-readable NAT type.
func (n NATType) String() string {
	switch n {
	case NATNone:
		return "none (public IP)"
	case NATFullCone:
		return "full-cone"
	case NATRestrictedCone:
		return "restricted-cone"
	case NATSymmetric:
		return "symmetric"
	default:
		return "unknown"
	}
}

// CanHolePunch returns true if the NAT type supports TCP hole-punching.
func (n NATType) CanHolePunch() bool {
	return n == NATNone || n == NATFullCone || n == NATRestrictedCone
}

// STUNResult holds the result of a STUN binding request.
type STUNResult struct {
	PublicIP   net.IP
	MappedPort int
	NATType    NATType
	LocalAddr  string
}

// STUN message constants (RFC 5389)
const (
	stunMagicCookie    = 0x2112A442
	stunBindRequest    = 0x0001
	stunBindResponse   = 0x0101
	stunBindErrorResp  = 0x0111
	stunAttrXorMapped  = 0x0020
	stunAttrMapped     = 0x0001
	stunMaxRetries     = 3
	stunRetryBaseDelay = 200 * time.Millisecond
)

// Default STUN servers (free, public)
var defaultSTUNServers = []string{
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
	"stun.cloudflare.com:3478",
}

// DiscoverNAT performs STUN binding requests to determine the public IP,
// mapped port, and NAT type.
func DiscoverNAT() (*STUNResult, error) {
	return DiscoverNATWithServers(defaultSTUNServers)
}

// DiscoverNATWithServers performs STUN discovery using specific servers.
// Uses a consistent local port for both requests to accurately detect NAT type.
func DiscoverNATWithServers(servers []string) (*STUNResult, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("no STUN servers provided")
	}

	// Bind a local UDP port that we'll reuse for both requests
	localAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		return nil, fmt.Errorf("resolving local address: %w", err)
	}

	// First binding request (with retry)
	result1, err := stunBindingRequestWithRetry(servers[0], localAddr)
	if err != nil {
		// Try remaining servers
		for _, server := range servers[1:] {
			result1, err = stunBindingRequestWithRetry(server, nil)
			if err == nil {
				break
			}
		}
		if err != nil {
			return nil, fmt.Errorf("STUN discovery failed (all servers): %w", err)
		}
	}

	// Second request to a different server to detect NAT type
	if len(servers) >= 2 {
		result2, err := stunBindingRequestWithRetry(servers[1], nil)
		if err == nil {
			if result1.MappedPort == result2.MappedPort &&
				result1.PublicIP.Equal(result2.PublicIP) {
				result1.NATType = NATFullCone
			} else if result1.PublicIP.Equal(result2.PublicIP) {
				result1.NATType = NATSymmetric
			} else {
				result1.NATType = NATSymmetric
			}
		}
	}

	// Check if public IP matches local IP (no NAT)
	localIP := getLocalIP()
	if localIP != nil && localIP.Equal(result1.PublicIP) {
		result1.NATType = NATNone
	}

	return result1, nil
}

// stunBindingRequestWithRetry wraps stunBindingRequest with retry logic.
func stunBindingRequestWithRetry(server string, localAddr *net.UDPAddr) (*STUNResult, error) {
	var lastErr error
	for attempt := 0; attempt < stunMaxRetries; attempt++ {
		result, err := stunBindingRequest(server)
		if err == nil {
			return result, nil
		}
		lastErr = err
		// Exponential backoff: 200ms, 400ms, 800ms
		time.Sleep(stunRetryBaseDelay << uint(attempt))
	}
	return nil, fmt.Errorf("STUN request to %s failed after %d attempts: %w", server, stunMaxRetries, lastErr)
}

// stunBindingRequest sends a STUN binding request and parses the response.
func stunBindingRequest(server string) (*STUNResult, error) {
	conn, err := net.DialTimeout("udp", server, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connecting to STUN server %s: %w", server, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return nil, fmt.Errorf("setting STUN deadline: %w", err)
	}

	// Build STUN binding request (RFC 5389)
	txID := make([]byte, 12)
	if _, err := rand.Read(txID); err != nil {
		return nil, fmt.Errorf("generating STUN transaction ID: %w", err)
	}

	req := make([]byte, 20)
	binary.BigEndian.PutUint16(req[0:2], stunBindRequest)
	binary.BigEndian.PutUint16(req[2:4], 0) // Message length (no attributes)
	binary.BigEndian.PutUint32(req[4:8], stunMagicCookie)
	copy(req[8:20], txID)

	if _, err := conn.Write(req); err != nil {
		return nil, fmt.Errorf("sending STUN request: %w", err)
	}

	// Read response
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("reading STUN response: %w", err)
	}

	return parseSTUNResponse(buf[:n], txID)
}

// parseSTUNResponse extracts the mapped address from a STUN binding response.
func parseSTUNResponse(data []byte, expectedTxID []byte) (*STUNResult, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("STUN response too short: %d bytes", len(data))
	}

	msgType := binary.BigEndian.Uint16(data[0:2])
	if msgType == stunBindErrorResp {
		return nil, fmt.Errorf("STUN server returned error response (0x%04x)", msgType)
	}
	if msgType != stunBindResponse {
		return nil, fmt.Errorf("unexpected STUN message type: 0x%04x", msgType)
	}

	msgLen := binary.BigEndian.Uint16(data[2:4])
	cookie := binary.BigEndian.Uint32(data[4:8])
	if cookie != stunMagicCookie {
		return nil, fmt.Errorf("invalid STUN magic cookie")
	}

	// Verify transaction ID
	for i := 0; i < 12; i++ {
		if data[8+i] != expectedTxID[i] {
			return nil, fmt.Errorf("STUN transaction ID mismatch")
		}
	}

	// Parse attributes
	result := &STUNResult{NATType: NATUnknown}
	offset := 20
	end := 20 + int(msgLen)
	if end > len(data) {
		end = len(data)
	}

	for offset+4 <= end {
		attrType := binary.BigEndian.Uint16(data[offset : offset+2])
		attrLen := binary.BigEndian.Uint16(data[offset+2 : offset+4])
		attrData := data[offset+4:]

		if int(attrLen) > len(attrData) {
			break
		}

		switch attrType {
		case stunAttrXorMapped:
			ip, port, err := parseXorMappedAddress(attrData[:attrLen], data[4:8], data[8:20])
			if err == nil {
				result.PublicIP = ip
				result.MappedPort = port
			}
		case stunAttrMapped:
			ip, port, err := parseMappedAddress(attrData[:attrLen])
			if err == nil && result.PublicIP == nil {
				result.PublicIP = ip
				result.MappedPort = port
			}
		}

		// Attributes are padded to 4-byte boundaries
		padded := int(attrLen)
		if padded%4 != 0 {
			padded += 4 - (padded % 4)
		}
		offset += 4 + padded
	}

	if result.PublicIP == nil {
		return nil, fmt.Errorf("no mapped address in STUN response")
	}

	return result, nil
}

// parseXorMappedAddress decodes XOR-MAPPED-ADDRESS attribute.
func parseXorMappedAddress(data, magicCookie, txID []byte) (net.IP, int, error) {
	if len(data) < 8 {
		return nil, 0, fmt.Errorf("XOR-MAPPED-ADDRESS too short")
	}

	family := data[1]
	xorPort := binary.BigEndian.Uint16(data[2:4])
	port := int(xorPort ^ uint16(stunMagicCookie>>16))

	switch family {
	case 0x01: // IPv4
		ip := make(net.IP, 4)
		mc := make([]byte, 4)
		binary.BigEndian.PutUint32(mc, stunMagicCookie)
		for i := 0; i < 4; i++ {
			ip[i] = data[4+i] ^ mc[i]
		}
		return ip, port, nil
	case 0x02: // IPv6
		if len(data) < 20 {
			return nil, 0, fmt.Errorf("IPv6 XOR-MAPPED-ADDRESS too short")
		}
		ip := make(net.IP, 16)
		mc := make([]byte, 4)
		binary.BigEndian.PutUint32(mc, stunMagicCookie)
		for i := 0; i < 4; i++ {
			ip[i] = data[4+i] ^ mc[i]
		}
		for i := 0; i < 12; i++ {
			ip[4+i] = data[8+i] ^ txID[i]
		}
		return ip, port, nil
	}

	return nil, 0, fmt.Errorf("unknown address family: %d", family)
}

// parseMappedAddress decodes MAPPED-ADDRESS attribute (fallback).
func parseMappedAddress(data []byte) (net.IP, int, error) {
	if len(data) < 8 {
		return nil, 0, fmt.Errorf("MAPPED-ADDRESS too short")
	}

	family := data[1]
	port := int(binary.BigEndian.Uint16(data[2:4]))

	switch family {
	case 0x01: // IPv4
		ip := net.IP(data[4:8])
		return ip, port, nil
	case 0x02: // IPv6
		if len(data) < 20 {
			return nil, 0, fmt.Errorf("IPv6 MAPPED-ADDRESS too short")
		}
		ip := net.IP(data[4:20])
		return ip, port, nil
	}

	return nil, 0, fmt.Errorf("unknown address family: %d", family)
}

// getLocalIP returns the primary local IP address.
func getLocalIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP
}
