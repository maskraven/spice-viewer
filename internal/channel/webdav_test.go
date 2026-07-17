// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"testing"

	"github.com/maskraven/virt-viewer/internal/channel"
	"github.com/maskraven/virt-viewer/internal/protocol"
)

func TestWebDAVPortInitAndData(t *testing.T) {
	h := &channel.NullVMCHandler{}
	w := channel.NewWebDAV(nil, channel.WebDAVOpts{ShareRoot: "/tmp/share", Handler: h})
	if w.ShareRoot() != "/tmp/share" {
		t.Fatalf("root=%q", w.ShareRoot())
	}
	init := protocol.PortInit{Name: "org.spice-space.webdav.0", Opened: false}
	if err := w.HandleMessage(protocol.MsgPortInit, init.Encode()); err != nil {
		t.Fatal(err)
	}
	if !w.InitSeen() || w.PortName() != "org.spice-space.webdav.0" {
		t.Fatalf("initSeen=%v name=%q", w.InitSeen(), w.PortName())
	}
	if err := w.HandleMessage(protocol.MsgSpiceVMCData, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	frames, bytes := w.Stats()
	if frames != 1 || bytes != 4 || h.Count != 1 {
		t.Fatalf("frames=%d bytes=%d h.Count=%d", frames, bytes, h.Count)
	}
}

func TestWebDAVPortEvent(t *testing.T) {
	w := channel.NewWebDAV(nil, channel.WebDAVOpts{})
	if err := w.HandleMessage(protocol.MsgPortEvent, protocol.EncodePortEvent(protocol.PortEventClosed)); err != nil {
		t.Fatal(err)
	}
}

func TestWebDAVDiscardNoShareRoot(t *testing.T) {
	w := channel.NewWebDAV(nil, channel.WebDAVOpts{})
	if err := w.HandleMessage(protocol.MsgSpiceVMCData, nil); err != nil {
		t.Fatal(err)
	}
	frames, _ := w.Stats()
	if frames != 1 {
		t.Fatalf("frames=%d", frames)
	}
}
