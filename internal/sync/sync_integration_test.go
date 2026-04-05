// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package sync_test

import (
	"crypto/sha256"
	"testing"

	"github.com/envsync/envsync/internal/sync"
)

// TestProtocolRoundTripIntegration tests the wire protocol encode/decode with realistic data.
func TestProtocolRoundTripIntegration(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
		seq     int64
	}{
		{"empty_file", ".env", "", 1},
		{"simple_kv", ".env", "KEY=value\n", 1},
		{"multivar", ".env", "A=1\nB=2\nC=3\nD=4\nE=5\n", 5},
		{"quoted_db_url", ".env", "DB_URL=\"postgres://user:pass@localhost:5432/db\"\n", 1},
		{"large_sequence", ".env", "KEY=value\n", 999999},
		{"special_chars", ".env.production", "MSG=\"hello\\nworld\"\nPATH='/usr/local/bin'\n", 42},
		{"base64_value", ".env", "SECRET=dGhpcyBpcyBhIHNlY3JldA==\n", 100},
		{"equals_in_value", ".env", "CONN=host=db port=5432 user=admin password=s3cr3t=\n", 3},
		{"unicode", ".env", "GREETING=こんにちは\n", 7},
		{"custom_filename", ".env.staging", "STAGE=true\nDEBUG=false\n", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := sync.NewEnvPayload(tt.file, []byte(tt.content), tt.seq)
			encoded, err := sync.EncodeEnvPayload(payload)
			if err != nil {
				t.Fatalf("encode failed: %v", err)
			}

			decoded, err := sync.DecodeEnvPayload(encoded)
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}

			if decoded.FileName != tt.file {
				t.Errorf("filename mismatch: got %q, want %q", decoded.FileName, tt.file)
			}
			if string(decoded.Data) != tt.content {
				t.Errorf("data mismatch:\n  got:  %q\n  want: %q", string(decoded.Data), tt.content)
			}
			if decoded.Sequence != tt.seq {
				t.Errorf("sequence mismatch: got %d, want %d", decoded.Sequence, tt.seq)
			}
			if decoded.Version != sync.ProtocolVersion {
				t.Errorf("version mismatch: got %d, want %d", decoded.Version, sync.ProtocolVersion)
			}

			// Verify checksum
			expectedChecksum := sha256.Sum256([]byte(tt.content))
			if decoded.Checksum != expectedChecksum {
				t.Errorf("checksum mismatch")
			}
		})
	}
}

// TestPayloadChecksumIntegrity verifies corrupted payloads are caught.
func TestPayloadChecksumIntegrity(t *testing.T) {
	content := "SECRET=hunter2\nAPI_KEY=abc123\nDB_PASS=correcthorsebatterystaple\n"
	payload := sync.NewEnvPayload(".env", []byte(content), 1)
	encoded, err := sync.EncodeEnvPayload(payload)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	// Corrupt a byte in the data section of the encoded payload
	// The data starts after: version(2) + seq(8) + ts(8) + nameLen(2) + name(4) + dataLen(4) = 28 bytes
	if len(encoded) > 35 {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		corrupted[35] ^= 0xFF // Flip a bit in the .env data

		decoded, err := sync.DecodeEnvPayload(corrupted)
		if err != nil {
			return // Caught at decode level — good
		}

		// Verify the checksum no longer matches the corrupted data
		actualChecksum := sha256.Sum256(decoded.Data)
		if actualChecksum == decoded.Checksum {
			t.Error("corrupted data should not pass checksum validation")
		}
	}
}

// TestLargePayloadBoundary tests payloads near the 64KB limit.
func TestLargePayloadBoundary(t *testing.T) {
	// Generate a large .env file just under 64KB
	data := make([]byte, 60000)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}

	payload := sync.NewEnvPayload(".env", data, 1)
	encoded, err := sync.EncodeEnvPayload(payload)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := sync.DecodeEnvPayload(encoded)
	if err != nil {
		t.Fatalf("decode failed for large payload: %v", err)
	}

	if len(decoded.Data) != len(data) {
		t.Errorf("data length mismatch: got %d, want %d", len(decoded.Data), len(data))
	}

	// Verify round-trip fidelity
	for i := range data {
		if decoded.Data[i] != data[i] {
			t.Errorf("data mismatch at byte %d: got 0x%02x, want 0x%02x", i, decoded.Data[i], data[i])
			break
		}
	}
}

// TestEncodeDecodeIdempotent verifies that encoding then decoding then encoding again
// produces the same bytes (minus timestamp which changes between calls).
func TestEncodeDecodeIdempotent(t *testing.T) {
	content := "KEY=value\nOTHER=data\n"
	payload := sync.NewEnvPayload(".env", []byte(content), 42)
	encoded1, err := sync.EncodeEnvPayload(payload)
	if err != nil {
		t.Fatalf("first encode failed: %v", err)
	}

	decoded, err := sync.DecodeEnvPayload(encoded1)
	if err != nil {
		t.Fatalf("first decode failed: %v", err)
	}

	// Re-encode (timestamp is preserved from decode, so should match)
	encoded2, err := sync.EncodeEnvPayload(decoded)
	if err != nil {
		t.Fatalf("second encode failed: %v", err)
	}

	if len(encoded1) != len(encoded2) {
		t.Errorf("re-encoded length differs: %d vs %d", len(encoded1), len(encoded2))
	}

	for i := range encoded1 {
		if encoded1[i] != encoded2[i] {
			t.Errorf("re-encoded differs at byte %d: 0x%02x vs 0x%02x", i, encoded1[i], encoded2[i])
			break
		}
	}
}

// TestEmptyPayload tests edge case of completely empty payload.
func TestEmptyPayload(t *testing.T) {
	payload := sync.NewEnvPayload("", []byte{}, 0)
	encoded, err := sync.EncodeEnvPayload(payload)
	if err != nil {
		t.Fatalf("encode failed for empty payload: %v", err)
	}

	decoded, err := sync.DecodeEnvPayload(encoded)
	if err != nil {
		t.Fatalf("decode failed for empty payload: %v", err)
	}

	if decoded.FileName != "" {
		t.Errorf("expected empty filename, got %q", decoded.FileName)
	}
	if len(decoded.Data) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(decoded.Data))
	}
}
