// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package sync

import (
	"testing"
)

func TestProtocolRoundTrip(t *testing.T) {
	// Create a payload
	original := NewEnvPayload(".env", []byte("DATABASE_URL=postgres://localhost:5432/mydb\nAPI_KEY=sk_test_12345\n"), 42)

	// Encode
	encoded, err := EncodeEnvPayload(original)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded payload is empty")
	}

	// Decode
	decoded, err := DecodeEnvPayload(encoded)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	// Verify
	if decoded.Version != original.Version {
		t.Errorf("version: got %d, want %d", decoded.Version, original.Version)
	}
	if decoded.Sequence != original.Sequence {
		t.Errorf("sequence: got %d, want %d", decoded.Sequence, original.Sequence)
	}
	if decoded.FileName != original.FileName {
		t.Errorf("filename: got %q, want %q", decoded.FileName, original.FileName)
	}
	if string(decoded.Data) != string(original.Data) {
		t.Errorf("data: got %q, want %q", decoded.Data, original.Data)
	}
	if decoded.Checksum != original.Checksum {
		t.Errorf("checksum mismatch")
	}
}

func TestProtocolDecodeInvalid(t *testing.T) {
	// Too short
	_, err := DecodeEnvPayload([]byte{0x01, 0x02})
	if err == nil {
		t.Error("expected error for short data")
	}
}

func TestProtocolMessageTypes(t *testing.T) {
	if MsgEnvPush != 0x01 {
		t.Errorf("MsgEnvPush: got 0x%02x, want 0x01", MsgEnvPush)
	}
	if MsgAck != 0x10 {
		t.Errorf("MsgAck: got 0x%02x, want 0x10", MsgAck)
	}
	if MsgNack != 0x11 {
		t.Errorf("MsgNack: got 0x%02x, want 0x11", MsgNack)
	}
}
