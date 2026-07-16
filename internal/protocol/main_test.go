// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/maskraven/virt-viewer/internal/protocol"
)

func TestMainInit_RoundTrip(t *testing.T) {
	in := protocol.MainInit{
		SessionID:           0xdeadbeef,
		DisplayChannelsHint: 1,
		SupportedMouseModes: 3,
		CurrentMouseMode:    2,
		AgentConnected:      0,
		AgentTokens:         10,
		MultiMediaTime:      1000,
		RamHint:             0x10000,
	}
	b := in.Encode()
	if len(b) != protocol.MainInitSize {
		t.Fatalf("len=%d", len(b))
	}
	if binary.LittleEndian.Uint32(b[0:4]) != 0xdeadbeef {
		t.Fatalf("session_id wire")
	}
	out, err := protocol.DecodeMainInit(b)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v want %+v", out, in)
	}
}

func TestChannelsList_RoundTrip(t *testing.T) {
	in := protocol.ChannelsList{Channels: []protocol.ChannelID{
		{Type: protocol.ChannelDisplay, ID: 0},
		{Type: protocol.ChannelInputs, ID: 0},
		{Type: protocol.ChannelCursor, ID: 0},
		{Type: protocol.ChannelPlayback, ID: 0},
	}}
	b := in.Encode()
	if binary.LittleEndian.Uint32(b[0:4]) != 4 {
		t.Fatal("num")
	}
	if b[4] != protocol.ChannelDisplay || b[5] != 0 {
		t.Fatal("first channel")
	}
	out, err := protocol.DecodeChannelsList(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Channels) != 4 {
		t.Fatalf("len=%d", len(out.Channels))
	}
	for i := range in.Channels {
		if out.Channels[i] != in.Channels[i] {
			t.Fatalf("[%d] got %+v", i, out.Channels[i])
		}
	}
}

func TestDecodeChannelsList_Short(t *testing.T) {
	_, err := protocol.DecodeChannelsList([]byte{1, 0, 0}) // incomplete num
	if err == nil {
		t.Fatal("expected error")
	}
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, 2)
	buf = append(buf, 2, 0) // only one channel of two
	_, err = protocol.DecodeChannelsList(buf)
	if err == nil {
		t.Fatal("expected truncated error")
	}
}

func TestMainInit_EncodeMessage(t *testing.T) {
	m := protocol.MainInit{SessionID: 42}
	pkt, err := protocol.EncodeMessage(protocol.MsgMainInit, m.Encode())
	if err != nil {
		t.Fatal(err)
	}
	msg, err := protocol.DecodeMessage(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != protocol.MsgMainInit {
		t.Fatalf("type=%d", msg.Type)
	}
	if !bytes.Equal(msg.Data, m.Encode()) {
		t.Fatal("body mismatch")
	}
}
