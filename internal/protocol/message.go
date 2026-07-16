// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// MiniHeader is SpiceMiniDataHeader used after link when MINI_HEADER is negotiated.
//
//	uint16 type
//	uint32 size  // body length
type MiniHeader struct {
	Type uint16
	Size uint32
}

// Encode serializes the mini-header (6 bytes, little-endian).
func (h MiniHeader) Encode() []byte {
	buf := make([]byte, MiniHeaderSize)
	binary.LittleEndian.PutUint16(buf[0:2], h.Type)
	binary.LittleEndian.PutUint32(buf[2:6], h.Size)
	return buf
}

// DecodeMiniHeader parses a 6-byte SpiceMiniDataHeader.
func DecodeMiniHeader(b []byte) (MiniHeader, error) {
	if len(b) < MiniHeaderSize {
		return MiniHeader{}, fmt.Errorf("spice: mini-header short: %d", len(b))
	}
	return MiniHeader{
		Type: binary.LittleEndian.Uint16(b[0:2]),
		Size: binary.LittleEndian.Uint32(b[2:6]),
	}, nil
}

// DataHeader is SpiceDataHeader (full header; Phase 1 prefers mini-header).
type DataHeader struct {
	Serial  uint64
	Type    uint16
	Size    uint32
	SubList uint32
}

// Encode serializes the full data header (18 bytes).
func (h DataHeader) Encode() []byte {
	buf := make([]byte, DataHeaderSize)
	binary.LittleEndian.PutUint64(buf[0:8], h.Serial)
	binary.LittleEndian.PutUint16(buf[8:10], h.Type)
	binary.LittleEndian.PutUint32(buf[10:14], h.Size)
	binary.LittleEndian.PutUint32(buf[14:18], h.SubList)
	return buf
}

// DecodeDataHeader parses an 18-byte SpiceDataHeader.
func DecodeDataHeader(b []byte) (DataHeader, error) {
	if len(b) < DataHeaderSize {
		return DataHeader{}, fmt.Errorf("spice: data header short: %d", len(b))
	}
	return DataHeader{
		Serial:  binary.LittleEndian.Uint64(b[0:8]),
		Type:    binary.LittleEndian.Uint16(b[8:10]),
		Size:    binary.LittleEndian.Uint32(b[10:14]),
		SubList: binary.LittleEndian.Uint32(b[14:18]),
	}, nil
}

// Message is a post-link channel message (type + body).
type Message struct {
	Type uint16
	Data []byte
}

// WriteMessage writes a mini-header framed message (Phase 1 framing).
func WriteMessage(w io.Writer, typ uint16, data []byte) error {
	if data == nil {
		data = []byte{}
	}
	if uint32(len(data)) > MaxMessageBody {
		return fmt.Errorf("spice: message body %d exceeds max %d", len(data), MaxMessageBody)
	}
	hdr := MiniHeader{Type: typ, Size: uint32(len(data))}
	if _, err := w.Write(hdr.Encode()); err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	_, err := w.Write(data)
	return err
}

// ReadMessage reads a mini-header framed message (Phase 1 framing).
func ReadMessage(r io.Reader) (Message, error) {
	var hdrBuf [MiniHeaderSize]byte
	if _, err := io.ReadFull(r, hdrBuf[:]); err != nil {
		return Message{}, err
	}
	hdr, err := DecodeMiniHeader(hdrBuf[:])
	if err != nil {
		return Message{}, err
	}
	if hdr.Size > MaxMessageBody {
		return Message{}, fmt.Errorf("spice: message size %d exceeds max %d", hdr.Size, MaxMessageBody)
	}
	var data []byte
	if hdr.Size > 0 {
		data = make([]byte, hdr.Size)
		if _, err := io.ReadFull(r, data); err != nil {
			return Message{}, err
		}
	} else {
		data = []byte{}
	}
	return Message{Type: hdr.Type, Data: data}, nil
}

// EncodeMessage returns mini-header || body as a single buffer.
func EncodeMessage(typ uint16, data []byte) ([]byte, error) {
	if data == nil {
		data = []byte{}
	}
	if uint32(len(data)) > MaxMessageBody {
		return nil, fmt.Errorf("spice: message body %d exceeds max %d", len(data), MaxMessageBody)
	}
	hdr := MiniHeader{Type: typ, Size: uint32(len(data))}
	out := make([]byte, MiniHeaderSize+len(data))
	copy(out, hdr.Encode())
	copy(out[MiniHeaderSize:], data)
	return out, nil
}

// DecodeMessage parses mini-header || body from a buffer (exact length).
func DecodeMessage(b []byte) (Message, error) {
	if len(b) < MiniHeaderSize {
		return Message{}, fmt.Errorf("spice: message short: %d", len(b))
	}
	hdr, err := DecodeMiniHeader(b[:MiniHeaderSize])
	if err != nil {
		return Message{}, err
	}
	if MiniHeaderSize+int(hdr.Size) != len(b) {
		return Message{}, fmt.Errorf("spice: message size %d does not match buffer %d",
			hdr.Size, len(b)-MiniHeaderSize)
	}
	data := make([]byte, hdr.Size)
	copy(data, b[MiniHeaderSize:])
	return Message{Type: hdr.Type, Data: data}, nil
}
