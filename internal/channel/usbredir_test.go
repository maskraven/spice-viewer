// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"testing"

	"github.com/maskraven/virt-viewer/internal/channel"
	"github.com/maskraven/virt-viewer/internal/protocol"
)

func TestUSBRedirVMCData(t *testing.T) {
	h := &channel.NullVMCHandler{}
	u := channel.NewUSBRedir(nil, channel.USBRedirOpts{ChannelID: 2, Handler: h})
	if u.ChannelID() != 2 {
		t.Fatalf("id=%d", u.ChannelID())
	}
	payload := []byte{0x12, 0x34, 0x56}
	if err := u.HandleMessage(protocol.MsgSpiceVMCData, payload); err != nil {
		t.Fatal(err)
	}
	frames, bytes := u.Stats()
	if frames != 1 || bytes != 3 {
		t.Fatalf("stats frames=%d bytes=%d", frames, bytes)
	}
	if h.Count != 1 || h.Bytes != 3 {
		t.Fatalf("handler count=%d bytes=%d", h.Count, h.Bytes)
	}
}

func TestUSBRedirCompressedDiscard(t *testing.T) {
	u := channel.NewUSBRedir(nil, channel.USBRedirOpts{})
	body := protocol.SpiceVMCCompressedData{
		Type:             protocol.DataCompressLZ4,
		UncompressedSize: 10,
		Data:             []byte{1, 2},
	}.Encode()
	if err := u.HandleMessage(protocol.MsgSpiceVMCCompressedData, body); err != nil {
		t.Fatal(err)
	}
}

func TestUSBFilterDefaultAllow(t *testing.T) {
	u := channel.NewUSBRedir(nil, channel.USBRedirOpts{})
	if !u.Filter().Allow(0x1234, 0x5678) {
		t.Fatal("NilUSBFilter should allow")
	}
}

func TestIsVMCMessage(t *testing.T) {
	if !channel.IsVMCMessage(protocol.MsgSpiceVMCData) {
		t.Fatal("DATA")
	}
	if !channel.IsVMCMessage(protocol.MsgSpiceVMCCompressedData) {
		t.Fatal("COMPRESSED")
	}
	// Message type numbers are channel-scoped; 101 is also RECORD_START on the
	// record channel. IsVMCMessage only classifies the numeric id (VMC DATA=101).
	if channel.IsVMCMessage(protocol.MsgPing) {
		t.Fatal("common PING should not be classified as VMC-only")
	}
}
