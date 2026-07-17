// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"bytes"
	"testing"

	"github.com/maskraven/virt-viewer/internal/protocol"
)

func TestSpiceVMCDataEncode(t *testing.T) {
	d := protocol.SpiceVMCData{Data: []byte{1, 2, 3}}
	b := d.Encode()
	if !bytes.Equal(b, []byte{1, 2, 3}) {
		t.Fatalf("%x", b)
	}
	got := protocol.DecodeSpiceVMCData(b)
	if !bytes.Equal(got.Data, d.Data) {
		t.Fatalf("%x", got.Data)
	}
}

func TestSpiceVMCCompressedNone(t *testing.T) {
	in := protocol.SpiceVMCCompressedData{
		Type: protocol.DataCompressNone,
		Data: []byte{9, 8, 7},
	}
	out, err := protocol.DecodeSpiceVMCCompressedData(in.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != protocol.DataCompressNone || !bytes.Equal(out.Data, in.Data) {
		t.Fatalf("%+v", out)
	}
}

func TestSpiceVMCCompressedLZ4(t *testing.T) {
	in := protocol.SpiceVMCCompressedData{
		Type:             protocol.DataCompressLZ4,
		UncompressedSize: 100,
		Data:             []byte{0xde, 0xad},
	}
	b := in.Encode()
	if len(b) != protocol.VMCCompressedHeaderFull+2 {
		t.Fatalf("len=%d", len(b))
	}
	out, err := protocol.DecodeSpiceVMCCompressedData(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != protocol.DataCompressLZ4 || out.UncompressedSize != 100 ||
		!bytes.Equal(out.Data, in.Data) {
		t.Fatalf("%+v", out)
	}
}

func TestPortInitRoundTrip(t *testing.T) {
	in := protocol.PortInit{Name: "org.spice-space.webdav.0", Opened: true}
	out, err := protocol.DecodePortInit(in.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != in.Name || out.Opened != in.Opened {
		t.Fatalf("got %+v want %+v", out, in)
	}
}

func TestPortEventRoundTrip(t *testing.T) {
	b := protocol.EncodePortEvent(protocol.PortEventOpened)
	ev, err := protocol.DecodePortEvent(b)
	if err != nil {
		t.Fatal(err)
	}
	if ev != protocol.PortEventOpened {
		t.Fatalf("ev=%d", ev)
	}
}

func TestSpiceVMCMessageIDs(t *testing.T) {
	if protocol.MsgSpiceVMCData != 101 || protocol.MsgSpiceVMCCompressedData != 102 {
		t.Fatalf("server VMC ids: %d %d", protocol.MsgSpiceVMCData, protocol.MsgSpiceVMCCompressedData)
	}
	if protocol.MsgcSpiceVMCData != 101 || protocol.MsgcSpiceVMCCompressedData != 102 {
		t.Fatalf("client VMC ids: %d %d", protocol.MsgcSpiceVMCData, protocol.MsgcSpiceVMCCompressedData)
	}
	if protocol.SpiceVMCCapDataCompressLZ4 != 0 {
		t.Fatalf("LZ4 cap bit %d want 0", protocol.SpiceVMCCapDataCompressLZ4)
	}
}
