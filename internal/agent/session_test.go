// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"bytes"
	"testing"
)

func TestMessageRoundTrip(t *testing.T) {
	m := Message{Protocol: Protocol, Type: MsgClipboard, Data: []byte("hi")}
	out, err := DecodeMessage(m.Encode())
	if err != nil || string(out.Data) != "hi" {
		t.Fatalf("%v %v", out, err)
	}
}

type recHandler struct {
	grabs int
	texts []string
}

func (r *recHandler) GuestGrabbed(uint8, []uint32)          { r.grabs++ }
func (r *recHandler) GuestData(_ uint8, _ uint32, d []byte) { r.texts = append(r.texts, string(d)) }
func (r *recHandler) GuestReleased(uint8)                   {}

func TestSessionClipboardFlow(t *testing.T) {
	var buf bytes.Buffer
	h := &recHandler{}
	s := New(&buf, h)
	if err := s.HandleMainAgentConnected(10); err != nil {
		t.Fatal(err)
	}
	peer := Message{Protocol: Protocol, Type: MsgAnnounceCapabilities,
		Data: EncodeAnnounceCapabilities(false, capsFromBits(CapClipboard, CapClipboardByDemand, CapClipboardSelection, CapMonitorsConfig)),
	}.Encode()
	if err := s.HandleAgentData(peer); err != nil {
		t.Fatal(err)
	}
	if !s.Active() {
		t.Fatal("inactive")
	}
	if err := s.SetHostClipboard("from-host"); err != nil {
		t.Fatal(err)
	}
	req := Message{Protocol: Protocol, Type: MsgClipboardRequest,
		Data: EncodeClipboardRequest(SelectionClipboard, ClipboardUTF8Text, true)}.Encode()
	if err := s.HandleAgentData(req); err != nil {
		t.Fatal(err)
	}
	grab := Message{Protocol: Protocol, Type: MsgClipboardGrab,
		Data: EncodeClipboardGrab(SelectionClipboard, []uint32{ClipboardUTF8Text}, true)}.Encode()
	if err := s.HandleAgentData(grab); err != nil {
		t.Fatal(err)
	}
	data := Message{Protocol: Protocol, Type: MsgClipboard,
		Data: EncodeClipboardData(SelectionClipboard, ClipboardUTF8Text, []byte("from-guest"), true)}.Encode()
	if err := s.HandleAgentData(data); err != nil {
		t.Fatal(err)
	}
	if h.grabs != 1 || len(h.texts) != 1 || h.texts[0] != "from-guest" {
		t.Fatalf("h=%+v", h)
	}
	if err := s.SendMonitorsConfig(1920, 1080); err != nil {
		t.Fatal(err)
	}
}
