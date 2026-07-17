// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/binary"
	"fmt"
)

// SpiceVMCData is the body of SPICE_MSG_SPICEVMC_DATA / SPICE_MSGC_SPICEVMC_DATA.
//
// Wire: opaque byte payload (spice.proto Data message).
type SpiceVMCData struct {
	Data []byte
}

// Encode serializes a VMC DATA body (identity: raw payload).
func (d SpiceVMCData) Encode() []byte {
	if len(d.Data) == 0 {
		return nil
	}
	return append([]byte(nil), d.Data...)
}

// DecodeSpiceVMCData returns the opaque VMC DATA payload (sub-slice of b).
func DecodeSpiceVMCData(b []byte) SpiceVMCData {
	return SpiceVMCData{Data: b}
}

// SpiceVMCCompressedData is SPICE_MSG_SPICEVMC_COMPRESSED_DATA body.
//
// Wire (spice.proto CompressedData):
//
//	type enum8 (DataCompress*)
//	if type != NONE: uncompressed_size u32
//	compressed_data[]
type SpiceVMCCompressedData struct {
	Type             uint8  // DataCompress*
	UncompressedSize uint32 // valid when Type != DataCompressNone
	Data             []byte // compressed (or raw when Type==NONE)
}

// Encode serializes a VMC COMPRESSED_DATA body.
func (c SpiceVMCCompressedData) Encode() []byte {
	if c.Type == DataCompressNone {
		buf := make([]byte, VMCCompressedTypeSize+len(c.Data))
		buf[0] = c.Type
		copy(buf[VMCCompressedTypeSize:], c.Data)
		return buf
	}
	buf := make([]byte, VMCCompressedHeaderFull+len(c.Data))
	buf[0] = c.Type
	binary.LittleEndian.PutUint32(buf[1:5], c.UncompressedSize)
	copy(buf[VMCCompressedHeaderFull:], c.Data)
	return buf
}

// DecodeSpiceVMCCompressedData parses a VMC COMPRESSED_DATA body.
// Data is a sub-slice of b (copy if retained beyond b's lifetime).
func DecodeSpiceVMCCompressedData(b []byte) (SpiceVMCCompressedData, error) {
	if len(b) < VMCCompressedTypeSize {
		return SpiceVMCCompressedData{}, fmt.Errorf("spice: VMC_COMPRESSED_DATA short: %d", len(b))
	}
	typ := b[0]
	if typ == DataCompressNone {
		return SpiceVMCCompressedData{
			Type: typ,
			Data: b[VMCCompressedTypeSize:],
		}, nil
	}
	if len(b) < VMCCompressedHeaderFull {
		return SpiceVMCCompressedData{}, fmt.Errorf(
			"spice: VMC_COMPRESSED_DATA truncated header: %d want >= %d",
			len(b), VMCCompressedHeaderFull)
	}
	return SpiceVMCCompressedData{
		Type:             typ,
		UncompressedSize: binary.LittleEndian.Uint32(b[1:5]),
		Data:             b[VMCCompressedHeaderFull:],
	}, nil
}

// PortInit is SPICE_MSG_PORT_INIT body.
//
// Wire: name_size u32 + name[name_size] (zero-terminated on wire per spice.proto)
// + opened u8. Decode is lenient about the trailing NUL inside name_size.
type PortInit struct {
	Name   string
	Opened bool
}

// Encode serializes PortInit (name includes a trailing NUL in the name field).
func (p PortInit) Encode() []byte {
	name := p.Name
	// spice.proto: name is zero-terminated inside name_size.
	nb := append([]byte(name), 0)
	buf := make([]byte, 4+len(nb)+1)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(len(nb)))
	copy(buf[4:], nb)
	if p.Opened {
		buf[4+len(nb)] = 1
	}
	return buf
}

// DecodePortInit parses SPICE_MSG_PORT_INIT.
func DecodePortInit(b []byte) (PortInit, error) {
	if len(b) < 5 {
		return PortInit{}, fmt.Errorf("spice: PORT_INIT short: %d", len(b))
	}
	n := int(binary.LittleEndian.Uint32(b[0:4]))
	if n < 0 || 4+n+1 > len(b) {
		return PortInit{}, fmt.Errorf("spice: PORT_INIT name_size=%d body=%d", n, len(b))
	}
	nameBytes := b[4 : 4+n]
	// Trim trailing NULs for the Go string.
	for len(nameBytes) > 0 && nameBytes[len(nameBytes)-1] == 0 {
		nameBytes = nameBytes[:len(nameBytes)-1]
	}
	return PortInit{
		Name:   string(nameBytes),
		Opened: b[4+n] != 0,
	}, nil
}

// EncodePortEvent serializes SPICE_MSG_PORT_EVENT / SPICE_MSGC_PORT_EVENT (uint8).
func EncodePortEvent(ev uint8) []byte {
	return []byte{ev}
}

// DecodePortEvent parses a PORT_EVENT body.
func DecodePortEvent(b []byte) (uint8, error) {
	if len(b) < 1 {
		return 0, fmt.Errorf("spice: PORT_EVENT short")
	}
	return b[0], nil
}
