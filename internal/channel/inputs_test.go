// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"bytes"
	"testing"

	"github.com/maskraven/virt-viewer/internal/channel"
	"github.com/maskraven/virt-viewer/internal/protocol"
)

func readMsg(t *testing.T, buf *bytes.Buffer) protocol.Message {
	t.Helper()
	msg, err := protocol.ReadMessage(buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	return msg
}

func TestInputs_KeyDownUp_ScancodeEncoding(t *testing.T) {
	var buf bytes.Buffer
	in := channel.NewInputs(&buf, 0)

	if err := in.KeyDown(channel.ScanA); err != nil {
		t.Fatal(err)
	}
	if err := in.KeyUp(channel.ScanA); err != nil {
		t.Fatal(err)
	}

	down := readMsg(t, &buf)
	if down.Type != protocol.MsgcInputsKeyDown {
		t.Fatalf("type=%d", down.Type)
	}
	code, err := protocol.DecodeKeyCode(down.Data)
	if err != nil || code != 0x1e {
		t.Fatalf("down code=%#x err=%v", code, err)
	}

	up := readMsg(t, &buf)
	if up.Type != protocol.MsgcInputsKeyUp {
		t.Fatalf("type=%d", up.Type)
	}
	code, err = protocol.DecodeKeyCode(up.Data)
	if err != nil || code != 0x9e {
		t.Fatalf("up code=%#x err=%v", code, err)
	}
}

func TestInputs_KeyDown_ExtendedScancode(t *testing.T) {
	var buf bytes.Buffer
	in := channel.NewInputs(&buf, 0)
	if err := in.KeyDown(channel.ScanRCtrl); err != nil {
		t.Fatal(err)
	}
	msg := readMsg(t, &buf)
	code, err := protocol.DecodeKeyCode(msg.Data)
	if err != nil {
		t.Fatal(err)
	}
	// Wire e0 1d 00 00
	if code != 0x1de0 {
		t.Fatalf("code=%#x", code)
	}
	if msg.Data[0] != 0xe0 || msg.Data[1] != 0x1d {
		t.Fatalf("bytes %v", msg.Data)
	}

	// Extended KeyUp (RCtrl, Delete).
	if err := in.KeyUp(channel.ScanRCtrl); err != nil {
		t.Fatal(err)
	}
	up := readMsg(t, &buf)
	if up.Type != protocol.MsgcInputsKeyUp {
		t.Fatalf("type=%d", up.Type)
	}
	code, err = protocol.DecodeKeyCode(up.Data)
	if err != nil || code != 0x9de0 {
		t.Fatalf("RCtrl up=%#x err=%v", code, err)
	}
	if up.Data[0] != 0xe0 || up.Data[1] != 0x9d {
		t.Fatalf("RCtrl up bytes %v", up.Data)
	}

	if err := in.KeyUp(channel.ScanDelete); err != nil {
		t.Fatal(err)
	}
	delUp := readMsg(t, &buf)
	code, err = protocol.DecodeKeyCode(delUp.Data)
	// Delete make 0x153 → break e0 0xd3 → LE 0xd3e0
	if err != nil || code != 0xd3e0 {
		t.Fatalf("Delete up=%#x err=%v", code, err)
	}
}

func TestInputs_MouseMove_ServerMode_Relative(t *testing.T) {
	var buf bytes.Buffer
	in := channel.NewInputs(&buf, 0)
	in.SetMouseMode(protocol.MouseModeServer, protocol.MouseModeServer)

	if err := in.MouseMove(5, -2); err != nil {
		t.Fatal(err)
	}
	msg := readMsg(t, &buf)
	if msg.Type != protocol.MsgcInputsMouseMotion {
		t.Fatalf("type=%d want MOTION", msg.Type)
	}
	m, err := protocol.DecodeMouseMotion(msg.Data)
	if err != nil {
		t.Fatal(err)
	}
	if m.DX != 5 || m.DY != -2 {
		t.Fatalf("got %+v", m)
	}
}

func TestInputs_MouseMove_ClientMode_Absolute(t *testing.T) {
	var buf bytes.Buffer
	in := channel.NewInputs(&buf, 2)
	in.SetMouseMode(protocol.MouseModeClient|protocol.MouseModeServer, protocol.MouseModeClient)

	if err := in.MouseMove(320, 240); err != nil {
		t.Fatal(err)
	}
	msg := readMsg(t, &buf)
	if msg.Type != protocol.MsgcInputsMousePosition {
		t.Fatalf("type=%d want POSITION", msg.Type)
	}
	p, err := protocol.DecodeMousePosition(msg.Data)
	if err != nil {
		t.Fatal(err)
	}
	if p.X != 320 || p.Y != 240 || p.DisplayID != 2 {
		t.Fatalf("got %+v", p)
	}
}

func TestInputs_MouseButton_AndWheel(t *testing.T) {
	var buf bytes.Buffer
	in := channel.NewInputs(&buf, 0)

	if err := in.MouseButton(protocol.MouseButtonLeft, true); err != nil {
		t.Fatal(err)
	}
	if in.Buttons() != protocol.MouseButtonMaskLeft {
		t.Fatalf("buttons=%#x", in.Buttons())
	}
	msg := readMsg(t, &buf)
	if msg.Type != protocol.MsgcInputsMousePress {
		t.Fatalf("type=%d", msg.Type)
	}
	ev, err := protocol.DecodeMouseButtonEvent(msg.Data)
	if err != nil || ev.Button != protocol.MouseButtonLeft || ev.ButtonsState != protocol.MouseButtonMaskLeft {
		t.Fatalf("ev=%+v err=%v", ev, err)
	}

	if err := in.MouseButton(protocol.MouseButtonLeft, false); err != nil {
		t.Fatal(err)
	}
	msg = readMsg(t, &buf)
	if msg.Type != protocol.MsgcInputsMouseRelease {
		t.Fatalf("type=%d", msg.Type)
	}
	if in.Buttons() != 0 {
		t.Fatalf("buttons after release %#x", in.Buttons())
	}

	// Wheel up: press+release of ButtonUp
	if err := in.MouseWheel(1); err != nil {
		t.Fatal(err)
	}
	press := readMsg(t, &buf)
	rel := readMsg(t, &buf)
	if press.Type != protocol.MsgcInputsMousePress || rel.Type != protocol.MsgcInputsMouseRelease {
		t.Fatalf("wheel types %d %d", press.Type, rel.Type)
	}
	pev, _ := protocol.DecodeMouseButtonEvent(press.Data)
	if pev.Button != protocol.MouseButtonUp {
		t.Fatalf("wheel button %d", pev.Button)
	}
}

func TestInputs_ModeSwitch_FromServerMessages(t *testing.T) {
	var buf bytes.Buffer
	in := channel.NewInputs(&buf, 0)
	// MAIN_INIT style
	in.SetMouseMode(protocol.MouseModeServer|protocol.MouseModeClient, protocol.MouseModeServer)
	if in.MouseMode() != protocol.MouseModeServer {
		t.Fatalf("mode=%d", in.MouseMode())
	}

	// Server switches to CLIENT via MAIN_MOUSE_MODE
	body := protocol.MainMouseMode{
		Supported: uint16(protocol.MouseModeServer | protocol.MouseModeClient),
		Current:   uint16(protocol.MouseModeClient),
	}.Encode()
	if err := in.HandleMainMouseMode(protocol.Message{
		Type: protocol.MsgMainMouseMode,
		Data: body,
	}); err != nil {
		t.Fatal(err)
	}
	if in.MouseMode() != protocol.MouseModeClient {
		t.Fatalf("after switch mode=%d", in.MouseMode())
	}
	if in.SupportedMouseModes()&(protocol.MouseModeClient) == 0 {
		t.Fatal("supported missing client")
	}

	// Subsequent MouseMove uses position
	if err := in.MouseMove(1, 2); err != nil {
		t.Fatal(err)
	}
	msg := readMsg(t, &buf)
	if msg.Type != protocol.MsgcInputsMousePosition {
		t.Fatalf("expected position after client mode, got %d", msg.Type)
	}
}

func TestInputs_HandleMessage_InitAndModifiersAndAck(t *testing.T) {
	var buf bytes.Buffer
	in := channel.NewInputs(&buf, 0)

	// INIT with caps lock
	if err := in.HandleMessage(protocol.Message{
		Type: protocol.MsgInputsInit,
		Data: protocol.EncodeKeyModifiers(protocol.CapsLockModifier),
	}); err != nil {
		t.Fatal(err)
	}
	if in.Modifiers() != protocol.CapsLockModifier {
		t.Fatalf("mods=%#x", in.Modifiers())
	}

	// KEY_MODIFIERS update
	if err := in.HandleMessage(protocol.Message{
		Type: protocol.MsgInputsKeyModifiers,
		Data: protocol.EncodeKeyModifiers(protocol.NumLockModifier),
	}); err != nil {
		t.Fatal(err)
	}
	if in.Modifiers() != protocol.NumLockModifier {
		t.Fatalf("mods=%#x", in.Modifiers())
	}

	// Force motion count up then ack
	in.SetMouseMode(protocol.MouseModeServer, protocol.MouseModeServer)
	for i := 0; i < protocol.InputMotionAckBunch; i++ {
		if err := in.MouseMotion(1, 0); err != nil {
			t.Fatal(err)
		}
	}
	// drain
	for buf.Len() > 0 {
		_, _ = protocol.ReadMessage(&buf)
	}
	if err := in.HandleMessage(protocol.Message{Type: protocol.MsgInputsMouseMotionAck}); err != nil {
		t.Fatal(err)
	}
}

func TestInputs_RequestPreferredMouseMode(t *testing.T) {
	var mainBuf bytes.Buffer
	var inBuf bytes.Buffer
	in := channel.NewInputs(&inBuf, 0)
	in.SetMouseMode(protocol.MouseModeServer|protocol.MouseModeClient, protocol.MouseModeServer)

	want, err := in.RequestPreferredMouseMode(&mainBuf)
	if err != nil {
		t.Fatal(err)
	}
	if want != protocol.MouseModeClient {
		t.Fatalf("want client got %d", want)
	}
	msg := readMsg(t, &mainBuf)
	if msg.Type != protocol.MsgcMainMouseModeRequest {
		t.Fatalf("type=%d", msg.Type)
	}
	mode, err := protocol.DecodeMouseModeRequest(msg.Data)
	if err != nil || mode != uint16(protocol.MouseModeClient) {
		t.Fatalf("mode=%d err=%v", mode, err)
	}
}

func TestInputs_RequestPreferredMouseMode_EmptySupported(t *testing.T) {
	var mainBuf bytes.Buffer
	in := channel.NewInputs(&bytes.Buffer{}, 0)
	// No SetMouseMode — supported mask is 0.
	want, err := in.RequestPreferredMouseMode(&mainBuf)
	if err == nil || want != 0 {
		t.Fatalf("want error and 0, got want=%d err=%v", want, err)
	}
	if mainBuf.Len() != 0 {
		t.Fatalf("must not write on empty supported mask, got %d bytes", mainBuf.Len())
	}
}

func TestInputs_MotionFlood_CoalesceAndACKFlush_Server(t *testing.T) {
	var buf bytes.Buffer
	in := channel.NewInputs(&buf, 0)
	in.SetMouseMode(protocol.MouseModeServer, protocol.MouseModeServer)

	limit := protocol.InputMotionAckBunch * 2
	// Send limit distinct motions (each flushed immediately while under budget).
	for i := 0; i < limit; i++ {
		if err := in.MouseMove(1, 0); err != nil {
			t.Fatal(err)
		}
	}
	// Over limit: deltas coalesce into pending (no new wire message yet).
	if err := in.MouseMove(3, 4); err != nil {
		t.Fatal(err)
	}
	if err := in.MouseMove(7, -1); err != nil {
		t.Fatal(err)
	}
	// Zero relative must not consume budget or write.
	if err := in.MouseMove(0, 0); err != nil {
		t.Fatal(err)
	}

	n := 0
	for buf.Len() > 0 {
		if _, err := protocol.ReadMessage(&buf); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != limit {
		t.Fatalf("msgs before ACK=%d want %d", n, limit)
	}

	// ACK frees bunch slots and flushes one coalesced motion (dx=3+7, dy=4-1).
	if err := in.HandleMessage(protocol.Message{Type: protocol.MsgInputsMouseMotionAck}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected catch-up motion after ACK")
	}
	msg := readMsg(t, &buf)
	if msg.Type != protocol.MsgcInputsMouseMotion {
		t.Fatalf("type=%d", msg.Type)
	}
	m, err := protocol.DecodeMouseMotion(msg.Data)
	if err != nil {
		t.Fatal(err)
	}
	if m.DX != 10 || m.DY != 3 {
		t.Fatalf("coalesced motion got %+v want dx=10 dy=3", m)
	}
	if buf.Len() != 0 {
		t.Fatalf("extra bytes after catch-up: %d", buf.Len())
	}

	// Further moves are accepted again (budget recovered).
	if err := in.MouseMove(1, 1); err != nil {
		t.Fatal(err)
	}
	msg = readMsg(t, &buf)
	m, err = protocol.DecodeMouseMotion(msg.Data)
	if err != nil || m.DX != 1 || m.DY != 1 {
		t.Fatalf("post-recovery motion %+v err=%v", m, err)
	}
}

func TestInputs_MotionFlood_CoalesceAndACKFlush_Client(t *testing.T) {
	var buf bytes.Buffer
	in := channel.NewInputs(&buf, 1)
	in.SetMouseMode(protocol.MouseModeClient, protocol.MouseModeClient)

	limit := protocol.InputMotionAckBunch * 2
	for i := 0; i < limit; i++ {
		if err := in.MouseMove(int32(i), int32(i)); err != nil {
			t.Fatal(err)
		}
	}
	// Over limit: keep latest absolute only.
	if err := in.MouseMove(999, 1001); err != nil {
		t.Fatal(err)
	}
	if err := in.MouseMove(42, 43); err != nil {
		t.Fatal(err)
	}

	n := 0
	for buf.Len() > 0 {
		if _, err := protocol.ReadMessage(&buf); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != limit {
		t.Fatalf("msgs before ACK=%d want %d", n, limit)
	}

	if err := in.HandleMessage(protocol.Message{Type: protocol.MsgInputsMouseMotionAck}); err != nil {
		t.Fatal(err)
	}
	msg := readMsg(t, &buf)
	if msg.Type != protocol.MsgcInputsMousePosition {
		t.Fatalf("type=%d", msg.Type)
	}
	p, err := protocol.DecodeMousePosition(msg.Data)
	if err != nil {
		t.Fatal(err)
	}
	if p.X != 42 || p.Y != 43 || p.DisplayID != 1 {
		t.Fatalf("catch-up position %+v", p)
	}
}

func TestInputs_ZeroRelativeMotion_NoWrite(t *testing.T) {
	var buf bytes.Buffer
	in := channel.NewInputs(&buf, 0)
	in.SetMouseMode(protocol.MouseModeServer, protocol.MouseModeServer)
	if err := in.MouseMotion(0, 0); err != nil {
		t.Fatal(err)
	}
	if err := in.MouseMove(0, 0); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("zero relative must not write, got %d bytes", buf.Len())
	}
}

func TestInputs_ConcurrentKeyAndMouse_Framing(t *testing.T) {
	var buf bytes.Buffer
	in := channel.NewInputs(&buf, 0)
	in.SetMouseMode(protocol.MouseModeServer, protocol.MouseModeServer)

	const nKey = 50
	const nMouse = 50
	done := make(chan error, 2)
	go func() {
		for i := 0; i < nKey; i++ {
			if err := in.KeyDown(channel.ScanA); err != nil {
				done <- err
				return
			}
			if err := in.KeyUp(channel.ScanA); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	go func() {
		for i := 0; i < nMouse; i++ {
			if err := in.MouseMove(1, 0); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}

	// Demux complete mini-header messages; total should be 2*nKey keys + some motions
	// (motions may coalesce under flood — at least keys must all be intact).
	var keys, motions int
	for buf.Len() > 0 {
		msg, err := protocol.ReadMessage(&buf)
		if err != nil {
			t.Fatalf("framed demux failed (interleaved write?): %v remaining=%d", err, buf.Len())
		}
		switch msg.Type {
		case protocol.MsgcInputsKeyDown, protocol.MsgcInputsKeyUp:
			keys++
			if _, err := protocol.DecodeKeyCode(msg.Data); err != nil {
				t.Fatal(err)
			}
		case protocol.MsgcInputsMouseMotion:
			motions++
			if _, err := protocol.DecodeMouseMotion(msg.Data); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unexpected type %d", msg.Type)
		}
	}
	if keys != 2*nKey {
		t.Fatalf("keys=%d want %d", keys, 2*nKey)
	}
	if motions == 0 {
		t.Fatal("expected some motion messages")
	}
}

func TestInputs_MainInitMouseBits_ConsistentWithFlags16(t *testing.T) {
	// MAIN_INIT carries mouse modes as uint32; MAIN_MOUSE_MODE uses flags16.
	// PreferMouseMode / request path must treat the same bit values.
	init := protocol.MainInit{
		SessionID:           1,
		SupportedMouseModes: protocol.MouseModeServer | protocol.MouseModeClient,
		CurrentMouseMode:    protocol.MouseModeServer,
	}
	var mainBuf, inBuf bytes.Buffer
	in := channel.NewInputs(&inBuf, 0)
	in.SetMouseMode(init.SupportedMouseModes, init.CurrentMouseMode)

	want, err := in.RequestPreferredMouseMode(&mainBuf)
	if err != nil || want != protocol.MouseModeClient {
		t.Fatalf("want=%d err=%v", want, err)
	}
	msg := readMsg(t, &mainBuf)
	mode, err := protocol.DecodeMouseModeRequest(msg.Data)
	if err != nil || uint32(mode) != protocol.MouseModeClient {
		t.Fatalf("mode=%d err=%v", mode, err)
	}

	// Server grants CLIENT via flags16 MAIN_MOUSE_MODE body.
	mm := protocol.MainMouseMode{
		Supported: uint16(init.SupportedMouseModes),
		Current:   uint16(protocol.MouseModeClient),
	}
	if err := in.HandleMainMouseMode(protocol.Message{
		Type: protocol.MsgMainMouseMode,
		Data: mm.Encode(),
	}); err != nil {
		t.Fatal(err)
	}
	if in.MouseMode() != protocol.MouseModeClient {
		t.Fatalf("mode=%d", in.MouseMode())
	}
}

func TestScancodeTable_CommonKeys(t *testing.T) {
	if channel.LetterScancode('a') != channel.ScanA {
		t.Fatal("a")
	}
	if channel.LetterScancode('Z') != channel.ScanZ {
		t.Fatal("Z")
	}
	if channel.DigitScancode('0') != channel.Scan0 {
		t.Fatal("0")
	}
	if channel.DigitScancode('5') != channel.Scan5 {
		t.Fatal("5")
	}
	if channel.LetterScancode('!') != 0 {
		t.Fatal("non-letter")
	}
	// Secure attention sequence pieces
	if channel.ScanLCtrl != 0x1d || channel.ScanLAlt != 0x38 || channel.ScanDelete != 0x153 {
		t.Fatalf("CAD scancodes ctrl=%#x alt=%#x del=%#x",
			channel.ScanLCtrl, channel.ScanLAlt, channel.ScanDelete)
	}
	// MakeScancodeCode alias
	if channel.MakeScancodeCode(channel.ScanEnter, false) != 0x1c {
		t.Fatal("enter")
	}
}

func TestInputs_ButtonsCarriedInMotion(t *testing.T) {
	var buf bytes.Buffer
	in := channel.NewInputs(&buf, 0)
	in.SetMouseMode(protocol.MouseModeServer, protocol.MouseModeServer)
	_ = in.MouseButton(protocol.MouseButtonRight, true)
	_ = readMsg(t, &buf) // press

	if err := in.MouseMotion(0, 1); err != nil {
		t.Fatal(err)
	}
	msg := readMsg(t, &buf)
	m, err := protocol.DecodeMouseMotion(msg.Data)
	if err != nil {
		t.Fatal(err)
	}
	if m.ButtonsState != protocol.MouseButtonMaskRight {
		t.Fatalf("buttons_state=%#x", m.ButtonsState)
	}
}
