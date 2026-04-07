// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package sync

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/envsync/envsync/internal/crypto"
)

// MessageType identifies the type of wire protocol message.
type MessageType uint8

const (
	MsgEnvPush     MessageType = 0x01
	MsgEnvPullReq  MessageType = 0x02
	MsgEnvPullResp MessageType = 0x03
	MsgAck         MessageType = 0x10
	MsgNack        MessageType = 0x11
	MsgPeerInfo    MessageType = 0x20
	MsgPing        MessageType = 0x30
	MsgPong        MessageType = 0x31
)

// Message represents a wire protocol message.
type Message struct {
	Type    MessageType
	Payload []byte
}

// EnvPayload is the payload of an ENV_PUSH or ENV_PULL_RESP message.
type EnvPayload struct {
	Version        uint16
	Sequence       int64
	Timestamp      int64
	BaseRevisionID string
	RevisionID     string
	FileName       string
	Data           []byte
	Checksum       [32]byte
}

const (
	// ProtocolVersion is the current wire protocol version.
	ProtocolVersion uint16 = 2

	// MaxMessageSize is the maximum message payload (64KB).
	MaxMessageSize = 65536
)

// SendMessage sends a typed message over a secure connection.
func SendMessage(conn *crypto.SecureConn, msg Message) error {
	// Frame: Type (1B) + Payload
	frame := make([]byte, 1+len(msg.Payload))
	frame[0] = byte(msg.Type)
	copy(frame[1:], msg.Payload)

	return conn.Send(frame)
}

// ReceiveMessage receives a typed message from a secure connection.
func ReceiveMessage(conn *crypto.SecureConn) (Message, error) {
	frame, err := conn.Receive()
	if err != nil {
		return Message{}, fmt.Errorf("receiving message: %w", err)
	}

	if len(frame) < 1 {
		return Message{}, fmt.Errorf("empty message frame")
	}

	return Message{
		Type:    MessageType(frame[0]),
		Payload: frame[1:],
	}, nil
}

// EncodeEnvPayload serializes an env payload into bytes.
func EncodeEnvPayload(p EnvPayload) ([]byte, error) {
	baseRevisionBytes := []byte(p.BaseRevisionID)
	revisionBytes := []byte(p.RevisionID)
	nameBytes := []byte(p.FileName)
	if p.Sequence < 0 {
		return nil, fmt.Errorf("sequence must be non-negative")
	}
	if p.Timestamp < 0 {
		return nil, fmt.Errorf("timestamp must be non-negative")
	}
	if len(baseRevisionBytes) > math.MaxUint16 {
		return nil, fmt.Errorf("base revision too long: %d bytes", len(baseRevisionBytes))
	}
	if len(revisionBytes) > math.MaxUint16 {
		return nil, fmt.Errorf("revision too long: %d bytes", len(revisionBytes))
	}
	if len(nameBytes) > math.MaxUint16 {
		return nil, fmt.Errorf("filename too long: %d bytes", len(nameBytes))
	}
	if len(p.Data) > math.MaxUint32 {
		return nil, fmt.Errorf("payload too large: %d bytes", len(p.Data))
	}

	// Version(2) + Sequence(8) + Timestamp(8) + BaseLen(2) + Base + RevLen(2) + Rev + NameLen(2) + Name + DataLen(4) + Data + Checksum(32)
	size := 2 + 8 + 8 + 2 + len(baseRevisionBytes) + 2 + len(revisionBytes) + 2 + len(nameBytes) + 4 + len(p.Data) + 32
	buf := make([]byte, 0, size)

	// Version
	buf = binary.BigEndian.AppendUint16(buf, p.Version)
	// Sequence
	buf = binary.BigEndian.AppendUint64(buf, uint64(p.Sequence))
	// Timestamp
	buf = binary.BigEndian.AppendUint64(buf, uint64(p.Timestamp))
	// BaseRevisionID
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(baseRevisionBytes)))
	buf = append(buf, baseRevisionBytes...)
	// RevisionID
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(revisionBytes)))
	buf = append(buf, revisionBytes...)
	// FileName
	// #nosec G115 -- length is bounded by the check above.
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(nameBytes)))
	buf = append(buf, nameBytes...)
	// Data
	// #nosec G115 -- length is bounded by the check above.
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(p.Data)))
	buf = append(buf, p.Data...)
	// Checksum
	buf = append(buf, p.Checksum[:]...)

	return buf, nil
}

// DecodeEnvPayload deserializes an env payload from bytes.
func DecodeEnvPayload(data []byte) (EnvPayload, error) {
	r := newBytesReader(data)
	p := EnvPayload{}

	// Version
	v, err := r.readUint16()
	if err != nil {
		return p, fmt.Errorf("reading version: %w", err)
	}
	p.Version = v

	// Sequence
	seq, err := r.readUint64()
	if err != nil {
		return p, fmt.Errorf("reading sequence: %w", err)
	}
	if seq > math.MaxInt64 {
		return p, fmt.Errorf("sequence overflows int64: %d", seq)
	}
	p.Sequence = int64(seq)

	// Timestamp
	ts, err := r.readUint64()
	if err != nil {
		return p, fmt.Errorf("reading timestamp: %w", err)
	}
	if ts > math.MaxInt64 {
		return p, fmt.Errorf("timestamp overflows int64: %d", ts)
	}
	p.Timestamp = int64(ts)

	// BaseRevisionID
	baseRevisionLen, err := r.readUint16()
	if err != nil {
		return p, fmt.Errorf("reading base revision length: %w", err)
	}
	baseRevisionBytes, err := r.readBytes(int(baseRevisionLen))
	if err != nil {
		return p, fmt.Errorf("reading base revision: %w", err)
	}
	p.BaseRevisionID = string(baseRevisionBytes)

	// RevisionID
	revisionLen, err := r.readUint16()
	if err != nil {
		return p, fmt.Errorf("reading revision length: %w", err)
	}
	revisionBytes, err := r.readBytes(int(revisionLen))
	if err != nil {
		return p, fmt.Errorf("reading revision: %w", err)
	}
	p.RevisionID = string(revisionBytes)

	// FileName
	nameLen, err := r.readUint16()
	if err != nil {
		return p, fmt.Errorf("reading filename length: %w", err)
	}
	nameBytes, err := r.readBytes(int(nameLen))
	if err != nil {
		return p, fmt.Errorf("reading filename: %w", err)
	}
	p.FileName = string(nameBytes)

	// Data
	dataLen, err := r.readUint32()
	if err != nil {
		return p, fmt.Errorf("reading data length: %w", err)
	}
	p.Data, err = r.readBytes(int(dataLen))
	if err != nil {
		return p, fmt.Errorf("reading data: %w", err)
	}

	// Checksum
	checksumBytes, err := r.readBytes(32)
	if err != nil {
		return p, fmt.Errorf("reading checksum: %w", err)
	}
	copy(p.Checksum[:], checksumBytes)

	return p, nil
}

// NewEnvPayload creates an EnvPayload from raw .env content.
func NewEnvPayload(fileName string, data []byte, sequence int64, baseRevisionID, revisionID string) EnvPayload {
	return EnvPayload{
		Version:        ProtocolVersion,
		Sequence:       sequence,
		Timestamp:      time.Now().Unix(),
		BaseRevisionID: baseRevisionID,
		RevisionID:     revisionID,
		FileName:       fileName,
		Data:           data,
		Checksum:       sha256Sum(data),
	}
}

// bytesReader is a simple reader for decoding binary data.
type bytesReader struct {
	data []byte
	pos  int
}

func newBytesReader(data []byte) *bytesReader {
	return &bytesReader{data: data}
}

func (r *bytesReader) readUint16() (uint16, error) {
	if r.pos+2 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint16(r.data[r.pos:])
	r.pos += 2
	return v, nil
}

func (r *bytesReader) readUint32() (uint32, error) {
	if r.pos+4 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *bytesReader) readUint64() (uint64, error) {
	if r.pos+8 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint64(r.data[r.pos:])
	r.pos += 8
	return v, nil
}

func (r *bytesReader) readBytes(n int) ([]byte, error) {
	if r.pos+n > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}
	b := make([]byte, n)
	copy(b, r.data[r.pos:r.pos+n])
	r.pos += n
	return b, nil
}

func sha256Sum(data []byte) [32]byte {
	// Using crypto/sha256
	var h [32]byte
	sum := sha256Digest(data)
	copy(h[:], sum[:])
	return h
}
