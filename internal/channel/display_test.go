// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"encoding/binary"
	"image"
	"testing"

	"github.com/maskraven/virt-viewer/internal/channel"
	"github.com/maskraven/virt-viewer/internal/display"
	"github.com/maskraven/virt-viewer/internal/protocol"
)

func TestDisplayAllowlist(t *testing.T) {
	allowed := []uint16{
		protocol.MsgDisplayMode,
		protocol.MsgDisplayMark,
		protocol.MsgDisplayReset,
		protocol.MsgDisplayDrawFill,
		protocol.MsgDisplayDrawCopy,
		protocol.MsgDisplaySurfaceCreate,
		protocol.MsgDisplaySurfaceDestroy,
	}
	for _, typ := range allowed {
		if !channel.IsDisplayAllowed(typ) {
			t.Errorf("type %d should be allowed", typ)
		}
	}
	if channel.IsDisplayAllowed(protocol.MsgDisplayDrawBlend) {
		t.Error("DRAW_BLEND should not be allowlisted in Phase 1")
	}
	if channel.IsDisplayAllowed(protocol.MsgDisplayStreamCreate) {
		t.Error("STREAM_CREATE should not be allowlisted")
	}
}

func TestDisplaySurfaceCreateAndFill(t *testing.T) {
	drv := display.NewNullDriver()
	comp := display.NewCompositor(drv)
	ch := channel.NewDisplay(nil, comp)

	// SURFACE_CREATE primary 8x8 xRGB
	var sc [20]byte
	binary.LittleEndian.PutUint32(sc[0:4], 0) // id
	binary.LittleEndian.PutUint32(sc[4:8], 8)
	binary.LittleEndian.PutUint32(sc[8:12], 8)
	binary.LittleEndian.PutUint32(sc[12:16], protocol.SurfaceFmt32xRGB)
	binary.LittleEndian.PutUint32(sc[16:20], protocol.SurfaceFlagPrimary)
	if err := ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:]); err != nil {
		t.Fatalf("surface create: %v", err)
	}
	if drv.Width != 8 || drv.Height != 8 {
		t.Fatalf("desktop size %dx%d", drv.Width, drv.Height)
	}

	// DRAW_FILL solid red over full surface
	body := encodeDrawFill(0, image.Rect(0, 0, 8, 8), 0x000000ff) // R=ff LE
	// SpiceColor LE: R=0xff → uint32 0x000000ff
	if err := ch.HandleMessage(protocol.MsgDisplayDrawFill, body); err != nil {
		t.Fatalf("draw fill: %v", err)
	}
	if err := ch.HandleMessage(protocol.MsgDisplayMark, nil); err != nil {
		t.Fatalf("mark: %v", err)
	}
	hash := drv.Hash()
	if hash == "" {
		t.Fatal("empty hash after fill")
	}

	// Bounds reject on fill outside surface
	bad := encodeDrawFill(0, image.Rect(0, 0, 100, 8), 0x000000ff)
	if err := ch.HandleMessage(protocol.MsgDisplayDrawFill, bad); err == nil {
		t.Fatal("expected bounds error")
	}
}

func TestDisplayDrawCopyRaw(t *testing.T) {
	drv := display.NewNullDriver()
	comp := display.NewCompositor(drv)
	ch := channel.NewDisplay(nil, comp)

	var sc [20]byte
	binary.LittleEndian.PutUint32(sc[4:8], 4)
	binary.LittleEndian.PutUint32(sc[8:12], 4)
	binary.LittleEndian.PutUint32(sc[12:16], protocol.SurfaceFmt32xRGB)
	binary.LittleEndian.PutUint32(sc[16:20], protocol.SurfaceFlagPrimary)
	if err := ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:]); err != nil {
		t.Fatal(err)
	}

	// Wire BGRX blue: B=0xff G=0 R=0 → decoded RGBA 00,00,ff
	body := encodeDrawCopyRaw(0, image.Rect(0, 0, 2, 2), 2, 2, 0xff, 0x00, 0x00, 0x00)
	if err := ch.HandleMessage(protocol.MsgDisplayDrawCopy, body); err != nil {
		t.Fatalf("draw copy: %v", err)
	}
	_ = ch.HandleMessage(protocol.MsgDisplayMark, nil)
	pix, _, _, stride := drv.Snapshot()
	// Top-left should be blue RGBA
	if pix[0] != 0x00 || pix[1] != 0x00 || pix[2] != 0xff {
		t.Fatalf("pixel = %02x%02x%02x want blue (stride=%d)", pix[0], pix[1], pix[2], stride)
	}
}

func TestDisplaySurfaceCreateBounds(t *testing.T) {
	ch := channel.NewDisplay(nil, display.NewCompositor(nil))
	var sc [20]byte
	binary.LittleEndian.PutUint32(sc[4:8], protocol.MaxSurfaceSide+1)
	binary.LittleEndian.PutUint32(sc[8:12], 1)
	binary.LittleEndian.PutUint32(sc[12:16], protocol.SurfaceFmt32xRGB)
	binary.LittleEndian.PutUint32(sc[16:20], protocol.SurfaceFlagPrimary)
	if err := ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:]); err == nil {
		t.Fatal("expected bounds reject")
	}
}

func TestDisplayIgnoreUnknown(t *testing.T) {
	ch := channel.NewDisplay(nil, display.NewCompositor(nil))
	if err := ch.HandleMessage(protocol.MsgDisplayStreamCreate, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	counts := ch.UnknownCounts()
	if counts[protocol.MsgDisplayStreamCreate] != 1 {
		t.Fatalf("unknown counts: %v", counts)
	}
}

func TestDisplayResetDestroy(t *testing.T) {
	comp := display.NewCompositor(nil)
	ch := channel.NewDisplay(nil, comp)
	var sc [20]byte
	binary.LittleEndian.PutUint32(sc[4:8], 16)
	binary.LittleEndian.PutUint32(sc[8:12], 16)
	binary.LittleEndian.PutUint32(sc[12:16], protocol.SurfaceFmt32xRGB)
	binary.LittleEndian.PutUint32(sc[16:20], protocol.SurfaceFlagPrimary)
	_ = ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:])

	var dest [4]byte
	binary.LittleEndian.PutUint32(dest[:], 0)
	if err := ch.HandleMessage(protocol.MsgDisplaySurfaceDestroy, dest[:]); err != nil {
		t.Fatal(err)
	}
	if comp.Surface(0) != nil {
		t.Fatal("surface should be destroyed")
	}

	_ = ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:])
	_ = ch.HandleMessage(protocol.MsgDisplayReset, nil)
	if comp.Surface(0) != nil {
		t.Fatal("reset should clear surfaces")
	}
}

// encodeDrawFill builds DRAW_FILL body: DisplayBase(clip none) + solid brush + ropd + empty qmask.
func encodeDrawFill(surfaceID uint32, box image.Rectangle, color uint32) []byte {
	buf := make([]byte, 0, 64)
	buf = appendDisplayBase(buf, surfaceID, box)
	buf = append(buf, protocol.BrushTypeSolid)
	var c [4]byte
	binary.LittleEndian.PutUint32(c[:], color)
	buf = append(buf, c[:]...)
	// ropd = OP_PUT
	var ropd [2]byte
	binary.LittleEndian.PutUint16(ropd[:], protocol.RopdOpPut)
	buf = append(buf, ropd[:]...)
	// QMask: flags, x, y, image_ptr = 0
	buf = append(buf, 0)                   // flags
	buf = append(buf, make([]byte, 12)...) // pos + ptr
	return buf
}

// encodeDrawCopyRaw builds DRAW_COPY with an embedded 32BIT top-down bitmap.
func encodeDrawCopyRaw(surfaceID uint32, box image.Rectangle, w, h int, b, g, r, a byte) []byte {
	// Layout: base | img_ptr | src_area | ropd | scale | qmask | ... image at img_ptr
	// We put image after the fixed trailer so img_ptr points there.
	base := appendDisplayBase(nil, surfaceID, box)
	// Fixed after base: img_ptr(4) + src_area(16) + ropd(2) + scale(1) + qmask(13) = 36
	fixed := 36
	imgOff := len(base) + fixed

	img := packBitmapImage(w, h, b, g, r, a)
	body := make([]byte, imgOff+len(img))
	copy(body, base)
	off := len(base)
	binary.LittleEndian.PutUint32(body[off:off+4], uint32(imgOff))
	off += 4
	// src_area = full image
	binary.LittleEndian.PutUint32(body[off:off+4], 0)             // top
	binary.LittleEndian.PutUint32(body[off+4:off+8], 0)           // left
	binary.LittleEndian.PutUint32(body[off+8:off+12], uint32(h))  // bottom
	binary.LittleEndian.PutUint32(body[off+12:off+16], uint32(w)) // right
	off += 16
	binary.LittleEndian.PutUint16(body[off:off+2], protocol.RopdOpPut)
	off += 2
	body[off] = 0 // scale nearest
	off++
	// qmask zeros
	off += 13
	copy(body[imgOff:], img)
	return body
}

func appendDisplayBase(buf []byte, surfaceID uint32, box image.Rectangle) []byte {
	var hdr [21]byte
	binary.LittleEndian.PutUint32(hdr[0:4], surfaceID)
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(box.Min.Y))  // top
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(box.Min.X)) // left
	binary.LittleEndian.PutUint32(hdr[12:16], uint32(box.Max.Y))
	binary.LittleEndian.PutUint32(hdr[16:20], uint32(box.Max.X))
	hdr[20] = protocol.ClipTypeNone
	return append(buf, hdr[:]...)
}

func packBitmapImage(w, h int, b, g, r, a byte) []byte {
	stride := w * 4
	out := make([]byte, protocol.SpiceImageDescSize+18+stride*h)
	out[8] = protocol.ImageTypeBitmap
	binary.LittleEndian.PutUint32(out[10:14], uint32(w))
	binary.LittleEndian.PutUint32(out[14:18], uint32(h))
	bm := out[protocol.SpiceImageDescSize:]
	bm[0] = protocol.BitmapFmt32Bit
	bm[1] = protocol.BitmapFlagTopDown
	binary.LittleEndian.PutUint32(bm[2:6], uint32(w))
	binary.LittleEndian.PutUint32(bm[6:10], uint32(h))
	binary.LittleEndian.PutUint32(bm[10:14], uint32(stride))
	pix := bm[18:]
	for i := 0; i < w*h; i++ {
		pix[i*4+0] = b
		pix[i*4+1] = g
		pix[i*4+2] = r
		pix[i*4+3] = a
	}
	return out
}
