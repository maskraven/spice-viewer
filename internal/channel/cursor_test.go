// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/maskraven/virt-viewer/internal/channel"
	"github.com/maskraven/virt-viewer/internal/protocol"
)

func TestIsCursorMessage(t *testing.T) {
	for _, typ := range []uint16{
		protocol.MsgCursorInit,
		protocol.MsgCursorReset,
		protocol.MsgCursorSet,
		protocol.MsgCursorMove,
		protocol.MsgCursorHide,
	} {
		if !channel.IsCursorMessage(typ) {
			t.Errorf("type %d should be a cursor message", typ)
		}
	}
	// Message type ids are channel-local (e.g. MAIN_INIT also uses 103);
	// values outside the cursor range must not match.
	if channel.IsCursorMessage(1) { // SPICE_MSG_MIGRATE — common, not cursor-specific
		t.Error("common MIGRATE should not be classified as cursor-only via IsCursorMessage")
	}
	if channel.IsCursorMessage(999) {
		t.Error("unknown high type should not be a cursor message")
	}
}

func TestCursorSetAlphaAndHide(t *testing.T) {
	drv := channel.NewNullDriver()
	ch := channel.NewCursor(nil, drv)

	// 2x2 ALPHA: each pixel BGRA LE; first pixel pure red opaque → RGBA ff,00,00,ff
	body := encodeCursorSet(10, 20, true, 0, // unique
		protocol.CursorTypeAlpha, 2, 2, 1, 1,
		[]byte{
			0x00, 0x00, 0xff, 0xff, // BGRA red
			0x00, 0xff, 0x00, 0xff, // BGRA green
			0xff, 0x00, 0x00, 0xff, // BGRA blue
			0x00, 0x00, 0x00, 0x80, // BGRA black half-alpha
		},
	)
	if err := ch.HandleMessage(protocol.MsgCursorSet, body); err != nil {
		t.Fatalf("SET: %v", err)
	}
	rgba, w, h, hotX, hotY, x, y, visible := drv.Snapshot()
	if w != 2 || h != 2 || hotX != 1 || hotY != 1 {
		t.Fatalf("shape %dx%d hot=%d,%d", w, h, hotX, hotY)
	}
	if x != 10 || y != 20 || !visible {
		t.Fatalf("pos/visible = %d,%d vis=%v", x, y, visible)
	}
	if len(rgba) != 16 {
		t.Fatalf("rgba len %d", len(rgba))
	}
	// pixel0 RGBA red
	if rgba[0] != 0xff || rgba[1] != 0x00 || rgba[2] != 0x00 || rgba[3] != 0xff {
		t.Fatalf("pixel0 = %02x%02x%02x%02x want red", rgba[0], rgba[1], rgba[2], rgba[3])
	}
	// pixel1 green
	if rgba[4] != 0x00 || rgba[5] != 0xff || rgba[6] != 0x00 {
		t.Fatalf("pixel1 = %02x%02x%02x want green", rgba[4], rgba[5], rgba[6])
	}

	if err := ch.HandleMessage(protocol.MsgCursorHide, nil); err != nil {
		t.Fatalf("HIDE: %v", err)
	}
	_, _, _, _, _, _, _, visible = drv.Snapshot()
	if visible {
		t.Fatal("expected hidden after HIDE")
	}
	if _, _, hide, _ := drv.Counts(); hide != 1 {
		t.Fatalf("HideCount=%d", hide)
	}
}

func TestCursorMoveAndReset(t *testing.T) {
	drv := channel.NewNullDriver()
	ch := channel.NewCursor(nil, drv)

	// Install a tiny shape first
	body := encodeCursorSet(0, 0, true, 1, protocol.CursorTypeAlpha, 1, 1, 0, 0,
		[]byte{0, 0, 0xff, 0xff})
	if err := ch.HandleMessage(protocol.MsgCursorSet, body); err != nil {
		t.Fatal(err)
	}

	var move [4]byte
	binary.LittleEndian.PutUint16(move[0:2], uint16(int16(100)))
	yNeg := int16(-5)
	binary.LittleEndian.PutUint16(move[2:4], uint16(yNeg))
	if err := ch.HandleMessage(protocol.MsgCursorMove, move[:]); err != nil {
		t.Fatal(err)
	}
	_, _, _, _, _, x, y, vis := drv.Snapshot()
	if x != 100 || y != int(yNeg) || !vis {
		t.Fatalf("move got %d,%d vis=%v", x, y, vis)
	}

	if err := ch.HandleMessage(protocol.MsgCursorReset, nil); err != nil {
		t.Fatal(err)
	}
	_, w, h, _, _, _, _, vis := drv.Snapshot()
	if w != 0 || h != 0 || vis {
		t.Fatalf("after reset w=%d h=%d vis=%v", w, h, vis)
	}
	if _, _, _, reset := drv.Counts(); reset != 1 {
		t.Fatalf("ResetCount=%d", reset)
	}
}

func TestCursorSetFlagsNoneHides(t *testing.T) {
	drv := channel.NewNullDriver()
	ch := channel.NewCursor(nil, drv)
	// visible=1 but FLAGS_NONE → no shape → hide
	body := make([]byte, 5+2)
	binary.LittleEndian.PutUint16(body[0:2], 1)
	binary.LittleEndian.PutUint16(body[2:4], 2)
	body[4] = 1
	binary.LittleEndian.PutUint16(body[5:7], protocol.CursorFlagNone)
	if err := ch.HandleMessage(protocol.MsgCursorSet, body); err != nil {
		t.Fatal(err)
	}
	_, _, _, _, _, _, _, vis := drv.Snapshot()
	if vis {
		t.Fatal("FLAGS_NONE should hide")
	}
}

func TestCursorDecodeErrorNoPanic(t *testing.T) {
	drv := channel.NewNullDriver()
	ch := channel.NewCursor(nil, drv)

	// Truncated SET (too short for header)
	err := ch.HandleMessage(protocol.MsgCursorSet, []byte{1, 0, 2, 0, 1, 0, 0})
	if err == nil {
		t.Fatal("expected decode error")
	}
	// Truncated ALPHA data
	body := encodeCursorSet(0, 0, true, 9, protocol.CursorTypeAlpha, 4, 4, 0, 0, []byte{1, 2, 3})
	err = ch.HandleMessage(protocol.MsgCursorSet, body)
	if err == nil {
		t.Fatal("expected short ALPHA error")
	}
	// Oversized dimensions
	body = encodeCursorSet(0, 0, true, 9, protocol.CursorTypeAlpha, protocol.MaxCursorSide+1, 1, 0, 0, nil)
	err = ch.HandleMessage(protocol.MsgCursorSet, body)
	if err == nil {
		t.Fatal("expected size error")
	}
	// MOVE short
	if err := ch.HandleMessage(protocol.MsgCursorMove, []byte{1}); err == nil {
		t.Fatal("expected MOVE short error")
	}
	// Unknown type ignored
	if err := ch.HandleMessage(9999, []byte{1, 2, 3}); err != nil {
		t.Fatalf("unknown should be ignored: %v", err)
	}
	// Driver still usable after errors
	ok := encodeCursorSet(3, 4, true, 1, protocol.CursorTypeAlpha, 1, 1, 0, 0,
		[]byte{0, 0, 0xff, 0xff})
	if err := ch.HandleMessage(protocol.MsgCursorSet, ok); err != nil {
		t.Fatalf("recovery SET: %v", err)
	}
	if set, _, _, _ := drv.Counts(); set < 1 {
		t.Fatal("expected successful set after errors")
	}
}

func TestCursorMonoDecode(t *testing.T) {
	drv := channel.NewNullDriver()
	ch := channel.NewCursor(nil, drv)
	// 8x1 mono: AND all 0x00 (opaque), XOR 0xFF (white)
	and := []byte{0x00}
	xor := []byte{0xff}
	body := encodeCursorSet(0, 0, true, 2, protocol.CursorTypeMono, 8, 1, 0, 0, append(and, xor...))
	if err := ch.HandleMessage(protocol.MsgCursorSet, body); err != nil {
		t.Fatal(err)
	}
	rgba, w, h, _, _, _, _, _ := drv.Snapshot()
	if w != 8 || h != 1 || len(rgba) != 32 {
		t.Fatalf("mono shape %dx%d len=%d", w, h, len(rgba))
	}
	// first pixel white opaque
	if rgba[0] != 0xff || rgba[3] != 0xff {
		t.Fatalf("pixel0 = %02x a=%02x", rgba[0], rgba[3])
	}
}

func TestCursorCacheRoundTrip(t *testing.T) {
	drv := channel.NewNullDriver()
	ch := channel.NewCursor(nil, drv)
	// SET with CACHE_ME
	pix := []byte{0x11, 0x22, 0x33, 0xff}
	body := encodeCursorSetFlags(0, 0, true, protocol.CursorFlagCacheMe, 0xabc,
		protocol.CursorTypeAlpha, 1, 1, 0, 0, pix)
	if err := ch.HandleMessage(protocol.MsgCursorSet, body); err != nil {
		t.Fatal(err)
	}
	// SET FROM_CACHE with same unique
	body2 := encodeCursorSetFlags(5, 6, true, protocol.CursorFlagFromCache, 0xabc,
		protocol.CursorTypeAlpha, 0, 0, 0, 0, nil)
	if err := ch.HandleMessage(protocol.MsgCursorSet, body2); err != nil {
		t.Fatal(err)
	}
	rgba, _, _, _, _, x, y, _ := drv.Snapshot()
	if x != 5 || y != 6 {
		t.Fatalf("cache set pos %d,%d", x, y)
	}
	if len(rgba) != 4 || rgba[0] != 0x33 { // BGRA 11,22,33,ff → RGBA 33,22,11,ff
		t.Fatalf("cached rgba %v", rgba)
	}
	// INVAL_ONE then FROM_CACHE should error (non-fatal)
	var id [8]byte
	binary.LittleEndian.PutUint64(id[:], 0xabc)
	_ = ch.HandleMessage(protocol.MsgCursorInvalOne, id[:])
	if err := ch.HandleMessage(protocol.MsgCursorSet, body2); err == nil {
		t.Fatal("expected cache miss")
	}
}

func TestCursorRunDecodeErrorDoesNotKillLoop(t *testing.T) {
	// Server writes a bad SET then a good HIDE; Run must process HIDE after logging the bad SET.
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	drv := channel.NewNullDriver()
	ch := channel.NewCursor(client, drv)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- ch.Run(ctx)
	}()

	// Bad SET (truncated)
	bad, err := protocol.EncodeMessage(protocol.MsgCursorSet, []byte{1, 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.Write(bad); err != nil {
		t.Fatal(err)
	}
	// Good HIDE
	hide, err := protocol.EncodeMessage(protocol.MsgCursorHide, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.Write(hide); err != nil {
		t.Fatal(err)
	}

	// Wait for hide to be processed
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, hide, _ := drv.Counts(); hide >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, _, hide, _ := drv.Counts(); hide < 1 {
		t.Fatal("HIDE not processed after decode error — loop died?")
	}
	if ch.LastError() == nil {
		t.Fatal("expected LastError set from bad SET")
	}

	// Close server → Run should exit with EOF/closed
	server.Close()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) &&
			err.Error() != "EOF" && !errors.Is(err, io.ErrClosedPipe) {
			// pipe close may surface as "io: read/write on closed pipe"
			if err.Error() != "io: read/write on closed pipe" {
				// still OK: channel degraded
				t.Logf("Run exited: %v", err)
			}
		}
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("Run did not exit after conn close")
	}
}

func TestCursorRunContextCancel(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	ch := channel.NewCursor(client, channel.NewNullDriver())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ch.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			// May also get a pipe error depending on race; either is fine.
			t.Logf("Run after cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not honor cancel")
	}
}

// encodeCursorSet builds CURSOR_SET with flags=0 (no cache bits).
func encodeCursorSet(x, y int16, visible bool, unique uint64, typ uint8, w, h, hotX, hotY uint16, pixels []byte) []byte {
	return encodeCursorSetFlags(x, y, visible, 0, unique, typ, w, h, hotX, hotY, pixels)
}

func encodeCursorSetFlags(x, y int16, visible bool, flags uint16, unique uint64, typ uint8, w, h, hotX, hotY uint16, pixels []byte) []byte {
	// prefix 5 + flags 2 + header 17 + data
	body := make([]byte, 0, 5+2+17+len(pixels))
	var p [5]byte
	binary.LittleEndian.PutUint16(p[0:2], uint16(x))
	binary.LittleEndian.PutUint16(p[2:4], uint16(y))
	if visible {
		p[4] = 1
	}
	body = append(body, p[:]...)
	var f [2]byte
	binary.LittleEndian.PutUint16(f[:], flags)
	body = append(body, f[:]...)
	if flags&protocol.CursorFlagNone != 0 {
		return body
	}
	// Always write header for !NONE (matches demarshaller / FROM_CACHE with unique).
	var hdr [17]byte
	binary.LittleEndian.PutUint64(hdr[0:8], unique)
	hdr[8] = typ
	binary.LittleEndian.PutUint16(hdr[9:11], w)
	binary.LittleEndian.PutUint16(hdr[11:13], h)
	binary.LittleEndian.PutUint16(hdr[13:15], hotX)
	binary.LittleEndian.PutUint16(hdr[15:17], hotY)
	body = append(body, hdr[:]...)
	body = append(body, pixels...)
	return body
}
