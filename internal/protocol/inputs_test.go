// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/maskraven/spice-viewer/internal/protocol"
)

func TestMakeScancodeCode_Plain(t *testing.T) {
	// 'A' make = 0x1e, break = 0x9e
	down := protocol.MakeScancodeCode(0x1e, false)
	up := protocol.MakeScancodeCode(0x1e, true)
	if down != 0x1e {
		t.Fatalf("down=%#x want 0x1e", down)
	}
	if up != 0x9e {
		t.Fatalf("up=%#x want 0x9e", up)
	}
}

func TestMakeScancodeCode_Extended(t *testing.T) {
	// Right Ctrl: spice-gtk scancode 0x11d → wire e0 1d 00 00
	down := protocol.MakeScancodeCode(0x11d, false)
	up := protocol.MakeScancodeCode(0x11d, true)
	// LE uint32 bytes: e0, 1d, 0, 0 → 0x1de0
	if down != 0x1de0 {
		t.Fatalf("RCtrl down=%#x want 0x1de0", down)
	}
	// LE bytes: e0, 9d, 0, 0 → 0x9de0
	if up != 0x9de0 {
		t.Fatalf("RCtrl up=%#x want 0x9de0", up)
	}

	body := protocol.EncodeKeyDown(down)
	if len(body) != 4 || body[0] != 0xe0 || body[1] != 0x1d || body[2] != 0 || body[3] != 0 {
		t.Fatalf("wire bytes %v", body)
	}
}

func TestEncodeKeyDown_Framed(t *testing.T) {
	code := protocol.MakeScancodeCode(0x1c, false) // Enter
	body := protocol.EncodeKeyDown(code)
	msg, err := protocol.EncodeMessage(protocol.MsgcInputsKeyDown, body)
	if err != nil {
		t.Fatal(err)
	}
	// mini-header: type=101 (0x65), size=4
	if binary.LittleEndian.Uint16(msg[0:2]) != protocol.MsgcInputsKeyDown {
		t.Fatalf("type=%d", binary.LittleEndian.Uint16(msg[0:2]))
	}
	if binary.LittleEndian.Uint32(msg[2:6]) != 4 {
		t.Fatalf("size=%d", binary.LittleEndian.Uint32(msg[2:6]))
	}
	if binary.LittleEndian.Uint32(msg[6:10]) != 0x1c {
		t.Fatalf("code=%#x", binary.LittleEndian.Uint32(msg[6:10]))
	}

	dec, err := protocol.DecodeMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := protocol.DecodeKeyCode(dec.Data)
	if err != nil || got != 0x1c {
		t.Fatalf("decode code=%#x err=%v", got, err)
	}
}

func TestMouseMotion_RoundTrip(t *testing.T) {
	in := protocol.MouseMotion{DX: -3, DY: 7, ButtonsState: protocol.MouseButtonMaskLeft}
	b := in.Encode()
	if len(b) != protocol.MouseMotionSize {
		t.Fatalf("len=%d", len(b))
	}
	out, err := protocol.DecodeMouseMotion(b)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v want %+v", out, in)
	}
	// Wire: int32 LE -3, int32 LE 7, uint16 LE 1
	if binary.LittleEndian.Uint32(b[0:4]) != 0xfffffffd {
		t.Fatalf("dx wire %#x", binary.LittleEndian.Uint32(b[0:4]))
	}
}

func TestMousePosition_RoundTrip(t *testing.T) {
	in := protocol.MousePosition{
		X: 100, Y: 200,
		ButtonsState: protocol.MouseButtonMaskRight,
		DisplayID:    0,
	}
	b := in.Encode()
	if len(b) != protocol.MousePositionSize {
		t.Fatalf("len=%d", len(b))
	}
	out, err := protocol.DecodeMousePosition(b)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v want %+v", out, in)
	}
}

func TestMouseButtonEvent_RoundTrip(t *testing.T) {
	in := protocol.MouseButtonEvent{
		Button:       protocol.MouseButtonLeft,
		ButtonsState: protocol.MouseButtonMaskLeft,
	}
	b := in.Encode()
	if len(b) != protocol.MouseButtonEventSize {
		t.Fatalf("len=%d", len(b))
	}
	out, err := protocol.DecodeMouseButtonEvent(b)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v", out)
	}
}

func TestMainMouseMode_RoundTrip(t *testing.T) {
	in := protocol.MainMouseMode{
		Supported: uint16(protocol.MouseModeServer | protocol.MouseModeClient),
		Current:   uint16(protocol.MouseModeClient),
	}
	b := in.Encode()
	if len(b) != protocol.MainMouseModeSize {
		t.Fatalf("len=%d", len(b))
	}
	out, err := protocol.DecodeMainMouseMode(b)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v", out)
	}
}

func TestEncodeMouseModeRequest(t *testing.T) {
	b := protocol.EncodeMouseModeRequest(uint16(protocol.MouseModeClient))
	if len(b) != 2 || binary.LittleEndian.Uint16(b) != uint16(protocol.MouseModeClient) {
		t.Fatalf("%v", b)
	}
	mode, err := protocol.DecodeMouseModeRequest(b)
	if err != nil || mode != uint16(protocol.MouseModeClient) {
		t.Fatalf("mode=%d err=%v", mode, err)
	}
}

func TestPreferMouseMode(t *testing.T) {
	if got := protocol.PreferMouseMode(protocol.MouseModeServer | protocol.MouseModeClient); got != protocol.MouseModeClient {
		t.Fatalf("prefer both: %d", got)
	}
	if got := protocol.PreferMouseMode(protocol.MouseModeServer); got != protocol.MouseModeServer {
		t.Fatalf("prefer server-only: %d", got)
	}
	if got := protocol.PreferMouseMode(0); got != 0 {
		t.Fatalf("prefer none: %d", got)
	}
}

func TestKeyModifiers_RoundTrip(t *testing.T) {
	mods := protocol.CapsLockModifier | protocol.NumLockModifier
	b := protocol.EncodeKeyModifiers(mods)
	got, err := protocol.DecodeKeyModifiers(b)
	if err != nil || got != mods {
		t.Fatalf("got=%#x err=%v", got, err)
	}
}

func TestDecodeShortBodies(t *testing.T) {
	if _, err := protocol.DecodeKeyCode([]byte{1}); err == nil {
		t.Fatal("expected short key code error")
	}
	if _, err := protocol.DecodeMouseMotion([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected short motion error")
	}
	if _, err := protocol.DecodeMousePosition(bytes.Repeat([]byte{0}, 10)); err == nil {
		t.Fatal("expected short position error")
	}
	if _, err := protocol.DecodeMouseButtonEvent([]byte{1}); err == nil {
		t.Fatal("expected short button error")
	}
	if _, err := protocol.DecodeMainMouseMode([]byte{1}); err == nil {
		t.Fatal("expected short mouse mode error")
	}
	if _, err := protocol.DecodeKeyModifiers([]byte{1}); err == nil {
		t.Fatal("expected short key modifiers error")
	}
	if _, err := protocol.DecodeMouseModeRequest([]byte{1}); err == nil {
		t.Fatal("expected short mouse mode request error")
	}
}

func TestWireBodySizes(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"KeyCode", len(protocol.EncodeKeyDown(0x1e)), protocol.KeyCodeSize},
		{"KeyUp", len(protocol.EncodeKeyUp(0x9e)), protocol.KeyCodeSize},
		{"KeyModifiers", len(protocol.EncodeKeyModifiers(protocol.CapsLockModifier)), protocol.KeyModifiersSize},
		{"MouseMotion", len(protocol.MouseMotion{DX: 1, DY: -1}.Encode()), protocol.MouseMotionSize},
		{"MousePosition", len(protocol.MousePosition{X: 1, Y: 2, DisplayID: 0}.Encode()), protocol.MousePositionSize},
		{"MouseButton", len(protocol.MouseButtonEvent{Button: 1}.Encode()), protocol.MouseButtonEventSize},
		{"MainMouseMode", len(protocol.MainMouseMode{Supported: 3, Current: 2}.Encode()), protocol.MainMouseModeSize},
		{"MouseModeRequest", len(protocol.EncodeMouseModeRequest(2)), protocol.MainMouseModeReqSize},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s len=%d want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestButtonMaskFor(t *testing.T) {
	if protocol.ButtonMaskFor(protocol.MouseButtonLeft) != protocol.MouseButtonMaskLeft {
		t.Fatal("left")
	}
	if protocol.ButtonMaskFor(protocol.MouseButtonUp) != protocol.MouseButtonMaskUp {
		t.Fatal("up")
	}
	if protocol.ButtonMaskFor(0) != 0 {
		t.Fatal("invalid")
	}
}

func TestKnownScancodeMessages(t *testing.T) {
	// Known vectors: letter A down/up mini-header framed.
	cases := []struct {
		name     string
		scancode uint16
		release  bool
		wantCode uint32
		msgType  uint16
	}{
		{"A-down", 0x1e, false, 0x1e, protocol.MsgcInputsKeyDown},
		{"A-up", 0x1e, true, 0x9e, protocol.MsgcInputsKeyUp},
		{"Enter-down", 0x1c, false, 0x1c, protocol.MsgcInputsKeyDown},
		{"RCtrl-down", 0x11d, false, 0x1de0, protocol.MsgcInputsKeyDown},
		{"Delete-down", 0x153, false, 0x53e0, protocol.MsgcInputsKeyDown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := protocol.MakeScancodeCode(tc.scancode, tc.release)
			if code != tc.wantCode {
				t.Fatalf("code=%#x want %#x", code, tc.wantCode)
			}
			body := protocol.EncodeKeyDown(code)
			framed, err := protocol.EncodeMessage(tc.msgType, body)
			if err != nil {
				t.Fatal(err)
			}
			msg, err := protocol.DecodeMessage(framed)
			if err != nil {
				t.Fatal(err)
			}
			if msg.Type != tc.msgType {
				t.Fatalf("type %d", msg.Type)
			}
			got, err := protocol.DecodeKeyCode(msg.Data)
			if err != nil || got != tc.wantCode {
				t.Fatalf("got %#x err %v", got, err)
			}
		})
	}
}
