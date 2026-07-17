// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
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
		protocol.MsgDisplayStreamCreate,
		protocol.MsgDisplayStreamData,
		protocol.MsgDisplayStreamDataSized,
		protocol.MsgDisplayStreamClip,
		protocol.MsgDisplayStreamDestroy,
		protocol.MsgDisplayStreamDestroyAll,
	}
	for _, typ := range allowed {
		if !channel.IsDisplayAllowed(typ) {
			t.Errorf("type %d should be allowed", typ)
		}
	}
	if channel.IsDisplayAllowed(protocol.MsgDisplayDrawBlend) {
		t.Error("DRAW_BLEND should not be allowlisted")
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

	// Bounds soft-skip on fill outside surface (does not abort HandleMessage)
	skips := ch.DrawSkipCount()
	bad := encodeDrawFill(0, image.Rect(0, 0, 100, 8), 0x000000ff)
	if err := ch.HandleMessage(protocol.MsgDisplayDrawFill, bad); err != nil {
		t.Fatalf("bounds fill should soft-skip, got %v", err)
	}
	if ch.DrawSkipCount() <= skips {
		t.Fatal("expected draw skip counter increment on bounds reject")
	}
}

func TestDisplayDrawCopyRaw(t *testing.T) {
	// Two independent runs with same wire COPY → same NullDriver hash.
	hashOf := func() string {
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

		// Wire BGRX blue X=0: B=0xff G=0 R=0 → decoded RGBA 00,00,ff,ff
		body := encodeDrawCopyRaw(0, image.Rect(0, 0, 2, 2), 2, 2, 0xff, 0x00, 0x00, 0x00)
		if err := ch.HandleMessage(protocol.MsgDisplayDrawCopy, body); err != nil {
			t.Fatalf("draw copy: %v", err)
		}
		_ = ch.HandleMessage(protocol.MsgDisplayMark, nil)
		pix, _, _, stride := drv.Snapshot()
		// Top-left should be blue opaque RGBA
		if pix[0] != 0x00 || pix[1] != 0x00 || pix[2] != 0xff || pix[3] != 0xff {
			t.Fatalf("pixel = %02x%02x%02x%02x want blue opaque (stride=%d)",
				pix[0], pix[1], pix[2], pix[3], stride)
		}
		return drv.Hash()
	}

	h1 := hashOf()
	h2 := hashOf()
	if h1 == "" || h1 != h2 {
		t.Fatalf("COPY hash unstable: %q vs %q", h1, h2)
	}
}

func TestDisplayDrawCopyLZDecodes(t *testing.T) {
	drv := display.NewNullDriver()
	comp := display.NewCompositor(drv)
	ch := channel.NewDisplay(nil, comp)

	var sc [20]byte
	binary.LittleEndian.PutUint32(sc[4:8], 4)
	binary.LittleEndian.PutUint32(sc[8:12], 4)
	binary.LittleEndian.PutUint32(sc[12:16], protocol.SurfaceFmt32xRGB)
	binary.LittleEndian.PutUint32(sc[16:20], protocol.SurfaceFlagPrimary)
	_ = ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:])

	// Valid LZ DRAW_COPY must decode (no soft-skip) and paint blue.
	body := encodeDrawCopyLZ(0, image.Rect(0, 0, 2, 2), 2, 2, 0xff, 0x00, 0x00)
	if err := ch.HandleMessage(protocol.MsgDisplayDrawCopy, body); err != nil {
		t.Fatalf("LZ DRAW_COPY: %v", err)
	}
	if skips := ch.ImageSkipCounts(); skips[protocol.ImageTypeLZRGB] != 0 {
		t.Fatalf("LZ must not soft-skip, got skips %v", skips)
	}
	_ = ch.HandleMessage(protocol.MsgDisplayMark, nil)
	pix, _, _, _ := drv.Snapshot()
	// BGR blue on wire → RGBA 00 00 ff ff
	if pix[0] != 0x00 || pix[1] != 0x00 || pix[2] != 0xff || pix[3] != 0xff {
		t.Fatalf("pixel=%02x%02x%02x%02x want blue opaque", pix[0], pix[1], pix[2], pix[3])
	}
}

func TestDisplayDrawCopyInvalidQuicSoftSkip(t *testing.T) {
	drv := display.NewNullDriver()
	comp := display.NewCompositor(drv)
	ch := channel.NewDisplay(nil, comp)

	var sc [20]byte
	binary.LittleEndian.PutUint32(sc[4:8], 4)
	binary.LittleEndian.PutUint32(sc[8:12], 4)
	binary.LittleEndian.PutUint32(sc[12:16], protocol.SurfaceFmt32xRGB)
	binary.LittleEndian.PutUint32(sc[16:20], protocol.SurfaceFlagPrimary)
	_ = ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:])

	// Invalid Quic payload must soft-skip (not error) so Display.Run continues.
	body := encodeDrawCopyQuicStub(0, image.Rect(0, 0, 2, 2))
	if err := ch.HandleMessage(protocol.MsgDisplayDrawCopy, body); err != nil {
		t.Fatalf("invalid Quic DRAW_COPY must soft-skip, got fatal error: %v", err)
	}
	if ch.DrawSkipCount() < 1 {
		t.Fatal("expected draw skip count >= 1 for bad Quic")
	}
	// Second invalid Quic op still non-fatal
	if err := ch.HandleMessage(protocol.MsgDisplayDrawCopy, body); err != nil {
		t.Fatalf("second Quic skip: %v", err)
	}
	if ch.DrawSkipCount() < 2 {
		t.Fatal("expected second Quic skip counted")
	}
}

func TestDisplayDrawCopyUnsupportedGLZSoftSkip(t *testing.T) {
	drv := display.NewNullDriver()
	comp := display.NewCompositor(drv)
	ch := channel.NewDisplay(nil, comp)

	var sc [20]byte
	binary.LittleEndian.PutUint32(sc[4:8], 4)
	binary.LittleEndian.PutUint32(sc[8:12], 4)
	binary.LittleEndian.PutUint32(sc[12:16], protocol.SurfaceFmt32xRGB)
	binary.LittleEndian.PutUint32(sc[16:20], protocol.SurfaceFlagPrimary)
	_ = ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:])

	body := encodeDrawCopyUnsupportedType(0, image.Rect(0, 0, 2, 2), protocol.ImageTypeGLZRGB)
	if err := ch.HandleMessage(protocol.MsgDisplayDrawCopy, body); err != nil {
		t.Fatalf("GLZ DRAW_COPY must soft-skip: %v", err)
	}
	skips := ch.ImageSkipCounts()
	if skips[protocol.ImageTypeGLZRGB] < 1 {
		t.Fatalf("expected GLZ image skip, got %v", skips)
	}
}

func TestDisplayEmptyClipRectsNoDraw(t *testing.T) {
	drv := display.NewNullDriver()
	comp := display.NewCompositor(drv)
	ch := channel.NewDisplay(nil, comp)

	var sc [20]byte
	binary.LittleEndian.PutUint32(sc[4:8], 4)
	binary.LittleEndian.PutUint32(sc[8:12], 4)
	binary.LittleEndian.PutUint32(sc[12:16], protocol.SurfaceFmt32xRGB)
	binary.LittleEndian.PutUint32(sc[16:20], protocol.SurfaceFlagPrimary)
	_ = ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:])

	// Pre-fill green so empty clip must not overwrite.
	_ = ch.HandleMessage(protocol.MsgDisplayDrawFill, encodeDrawFill(0, image.Rect(0, 0, 4, 4), 0x0000ff00))
	// Red fill with CLIP_RECTS num=0 → no-op
	body := encodeDrawFillClipEmpty(0, image.Rect(0, 0, 4, 4), 0x000000ff)
	if err := ch.HandleMessage(protocol.MsgDisplayDrawFill, body); err != nil {
		t.Fatal(err)
	}
	_ = ch.HandleMessage(protocol.MsgDisplayMark, nil)
	pix, _, _, _ := drv.Snapshot()
	// Still green (G=0xff)
	if pix[1] != 0xff || pix[0] != 0 {
		t.Fatalf("empty clip should leave green, got %02x%02x%02x", pix[0], pix[1], pix[2])
	}
}

func TestDisplaySurfaceCreateBoundsSoftSkip(t *testing.T) {
	ch := channel.NewDisplay(nil, display.NewCompositor(nil))
	var sc [20]byte
	binary.LittleEndian.PutUint32(sc[4:8], protocol.MaxSurfaceSide+1)
	binary.LittleEndian.PutUint32(sc[8:12], 1)
	binary.LittleEndian.PutUint32(sc[12:16], protocol.SurfaceFmt32xRGB)
	binary.LittleEndian.PutUint32(sc[16:20], protocol.SurfaceFlagPrimary)
	if err := ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:]); err != nil {
		t.Fatalf("bounds create should soft-skip: %v", err)
	}
	if ch.DrawSkipCount() < 1 {
		t.Fatal("expected skip on oversize surface")
	}
}

func TestDisplayIgnoreUnknown(t *testing.T) {
	ch := channel.NewDisplay(nil, display.NewCompositor(nil))
	// DRAW_BLEND is not allowlisted.
	if err := ch.HandleMessage(protocol.MsgDisplayDrawBlend, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	counts := ch.UnknownCounts()
	if counts[protocol.MsgDisplayDrawBlend] != 1 {
		t.Fatalf("unknown counts: %v", counts)
	}
}

func TestDisplayStreamMJPEG(t *testing.T) {
	drv := display.NewNullDriver()
	comp := display.NewCompositor(drv)
	ch := channel.NewDisplay(nil, comp)

	const w, h = 8, 8
	var sc [20]byte
	binary.LittleEndian.PutUint32(sc[4:8], w)
	binary.LittleEndian.PutUint32(sc[8:12], h)
	binary.LittleEndian.PutUint32(sc[12:16], protocol.SurfaceFmt32xRGB)
	binary.LittleEndian.PutUint32(sc[16:20], protocol.SurfaceFlagPrimary)
	if err := ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:]); err != nil {
		t.Fatal(err)
	}

	// STREAM_CREATE: surface 0, stream id 1, TOP_DOWN, MJPEG, 8x8 dest full surface
	create := encodeStreamCreate(0, 1, protocol.StreamFlagTopDown, protocol.VideoCodecMJPEG,
		w, h, image.Rect(0, 0, w, h))
	if err := ch.HandleMessage(protocol.MsgDisplayStreamCreate, create); err != nil {
		t.Fatalf("stream create: %v", err)
	}

	// Solid red JPEG frame
	jb := encodeSolidJPEG(t, w, h, 255, 0, 0)
	dataMsg := encodeStreamData(1, 0, jb)
	if err := ch.HandleMessage(protocol.MsgDisplayStreamData, dataMsg); err != nil {
		t.Fatalf("stream data: %v", err)
	}
	if ch.DrawSkipCount() != 0 {
		t.Fatalf("unexpected draw skips: %d", ch.DrawSkipCount())
	}
	_ = ch.HandleMessage(protocol.MsgDisplayMark, nil)
	pix, _, _, _ := drv.Snapshot()
	// JPEG lossy: R should dominate
	if pix[0] < 180 {
		t.Fatalf("pixel R=%d want high red after MJPEG stream", pix[0])
	}

	// Destroy stream
	var destroy [4]byte
	binary.LittleEndian.PutUint32(destroy[:], 1)
	if err := ch.HandleMessage(protocol.MsgDisplayStreamDestroy, destroy[:]); err != nil {
		t.Fatal(err)
	}
	// Data after destroy soft-skips
	if err := ch.HandleMessage(protocol.MsgDisplayStreamData, dataMsg); err != nil {
		t.Fatal(err)
	}
	if ch.DrawSkipCount() < 1 {
		t.Fatal("expected skip for data on destroyed stream")
	}
}

func TestDisplayDrawCopyJPEG(t *testing.T) {
	drv := display.NewNullDriver()
	comp := display.NewCompositor(drv)
	ch := channel.NewDisplay(nil, comp)

	var sc [20]byte
	binary.LittleEndian.PutUint32(sc[4:8], 4)
	binary.LittleEndian.PutUint32(sc[8:12], 4)
	binary.LittleEndian.PutUint32(sc[12:16], protocol.SurfaceFmt32xRGB)
	binary.LittleEndian.PutUint32(sc[16:20], protocol.SurfaceFlagPrimary)
	_ = ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:])

	body := encodeDrawCopyJPEG(0, image.Rect(0, 0, 4, 4), 4, 4, 0, 0, 255) // blue
	if err := ch.HandleMessage(protocol.MsgDisplayDrawCopy, body); err != nil {
		t.Fatalf("JPEG DRAW_COPY: %v", err)
	}
	if skips := ch.ImageSkipCounts(); skips[protocol.ImageTypeJPEG] != 0 {
		t.Fatalf("JPEG must not soft-skip, got %v", skips)
	}
	_ = ch.HandleMessage(protocol.MsgDisplayMark, nil)
	pix, _, _, _ := drv.Snapshot()
	if pix[2] < 180 { // B channel high
		t.Fatalf("pixel B=%d want high blue", pix[2])
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

func TestDisplayImgPtrInvalidSoftSkip(t *testing.T) {
	comp := display.NewCompositor(display.NewNullDriver())
	ch := channel.NewDisplay(nil, comp)
	var sc [20]byte
	binary.LittleEndian.PutUint32(sc[4:8], 4)
	binary.LittleEndian.PutUint32(sc[8:12], 4)
	binary.LittleEndian.PutUint32(sc[12:16], protocol.SurfaceFmt32xRGB)
	binary.LittleEndian.PutUint32(sc[16:20], protocol.SurfaceFlagPrimary)
	_ = ch.HandleMessage(protocol.MsgDisplaySurfaceCreate, sc[:])

	// Body with img_ptr past end
	base := appendDisplayBase(nil, 0, image.Rect(0, 0, 2, 2))
	body := make([]byte, len(base)+36)
	copy(body, base)
	binary.LittleEndian.PutUint32(body[len(base):], uint32(len(body))) // ptr == len
	if err := ch.HandleMessage(protocol.MsgDisplayDrawCopy, body); err != nil {
		t.Fatalf("invalid img_ptr should soft-skip: %v", err)
	}
	if ch.DrawSkipCount() < 1 {
		t.Fatal("expected skip")
	}
}

// encodeDrawFill builds DRAW_FILL body: DisplayBase(clip none) + solid brush + ropd + empty qmask.
func encodeDrawFill(surfaceID uint32, box image.Rectangle, color uint32) []byte {
	buf := appendDisplayBase(nil, surfaceID, box)
	buf = append(buf, protocol.BrushTypeSolid)
	var c [4]byte
	binary.LittleEndian.PutUint32(c[:], color)
	buf = append(buf, c[:]...)
	var ropd [2]byte
	binary.LittleEndian.PutUint16(ropd[:], protocol.RopdOpPut)
	buf = append(buf, ropd[:]...)
	buf = append(buf, 0)                   // qmask flags
	buf = append(buf, make([]byte, 12)...) // pos + ptr
	return buf
}

// encodeDrawFillClipEmpty is FILL with CLIP_RECTS and num_rects=0.
func encodeDrawFillClipEmpty(surfaceID uint32, box image.Rectangle, color uint32) []byte {
	var hdr [25]byte
	binary.LittleEndian.PutUint32(hdr[0:4], surfaceID)
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(box.Min.Y))
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(box.Min.X))
	binary.LittleEndian.PutUint32(hdr[12:16], uint32(box.Max.Y))
	binary.LittleEndian.PutUint32(hdr[16:20], uint32(box.Max.X))
	hdr[20] = protocol.ClipTypeRects
	// num_rects = 0 at [21:25]
	buf := append([]byte{}, hdr[:]...)
	buf = append(buf, protocol.BrushTypeSolid)
	var c [4]byte
	binary.LittleEndian.PutUint32(c[:], color)
	buf = append(buf, c[:]...)
	var ropd [2]byte
	binary.LittleEndian.PutUint16(ropd[:], protocol.RopdOpPut)
	buf = append(buf, ropd[:]...)
	buf = append(buf, 0)
	buf = append(buf, make([]byte, 12)...)
	return buf
}

// encodeDrawCopyRaw builds DRAW_COPY with an embedded 32BIT top-down bitmap.
func encodeDrawCopyRaw(surfaceID uint32, box image.Rectangle, w, h int, b, g, r, a byte) []byte {
	base := appendDisplayBase(nil, surfaceID, box)
	fixed := 36
	imgOff := len(base) + fixed
	img := packBitmapImage(w, h, b, g, r, a)
	body := make([]byte, imgOff+len(img))
	copy(body, base)
	off := len(base)
	binary.LittleEndian.PutUint32(body[off:off+4], uint32(imgOff))
	off += 4
	binary.LittleEndian.PutUint32(body[off:off+4], 0)
	binary.LittleEndian.PutUint32(body[off+4:off+8], 0)
	binary.LittleEndian.PutUint32(body[off+8:off+12], uint32(h))
	binary.LittleEndian.PutUint32(body[off+12:off+16], uint32(w))
	off += 16
	binary.LittleEndian.PutUint16(body[off:off+2], protocol.RopdOpPut)
	off += 2
	body[off] = 0
	off++
	off += 13
	copy(body[imgOff:], img)
	return body
}

// encodeDrawCopyQuicStub embeds a SpiceImage with type QUIC and invalid payload.
func encodeDrawCopyQuicStub(surfaceID uint32, box image.Rectangle) []byte {
	return encodeDrawCopyUnsupportedType(surfaceID, box, protocol.ImageTypeQuic)
}

// encodeDrawCopyUnsupportedType embeds a SpiceImage of the given type with dummy payload.
func encodeDrawCopyUnsupportedType(surfaceID uint32, box image.Rectangle, imgType uint8) []byte {
	base := appendDisplayBase(nil, surfaceID, box)
	fixed := 36
	imgOff := len(base) + fixed
	img := make([]byte, protocol.SpiceImageDescSize+8)
	img[8] = imgType
	binary.LittleEndian.PutUint32(img[10:14], 2)
	binary.LittleEndian.PutUint32(img[14:18], 2)
	body := make([]byte, imgOff+len(img))
	copy(body, base)
	off := len(base)
	binary.LittleEndian.PutUint32(body[off:off+4], uint32(imgOff))
	off += 4
	binary.LittleEndian.PutUint32(body[off+8:off+12], 2)
	binary.LittleEndian.PutUint32(body[off+12:off+16], 2)
	off += 16
	binary.LittleEndian.PutUint16(body[off:off+2], protocol.RopdOpPut)
	copy(body[imgOff:], img)
	return body
}

func encodeDrawCopyJPEG(surfaceID uint32, box image.Rectangle, w, h int, r, g, b byte) []byte {
	base := appendDisplayBase(nil, surfaceID, box)
	fixed := 36
	imgOff := len(base) + fixed
	jb := mustEncodeSolidJPEG(w, h, r, g, b)
	img := make([]byte, protocol.SpiceImageDescSize+4+len(jb))
	img[8] = protocol.ImageTypeJPEG
	binary.LittleEndian.PutUint32(img[10:14], uint32(w))
	binary.LittleEndian.PutUint32(img[14:18], uint32(h))
	binary.LittleEndian.PutUint32(img[protocol.SpiceImageDescSize:protocol.SpiceImageDescSize+4], uint32(len(jb)))
	copy(img[protocol.SpiceImageDescSize+4:], jb)

	body := make([]byte, imgOff+len(img))
	copy(body, base)
	off := len(base)
	binary.LittleEndian.PutUint32(body[off:off+4], uint32(imgOff))
	off += 4
	binary.LittleEndian.PutUint32(body[off:off+4], 0)
	binary.LittleEndian.PutUint32(body[off+4:off+8], 0)
	binary.LittleEndian.PutUint32(body[off+8:off+12], uint32(h))
	binary.LittleEndian.PutUint32(body[off+12:off+16], uint32(w))
	off += 16
	binary.LittleEndian.PutUint16(body[off:off+2], protocol.RopdOpPut)
	copy(body[imgOff:], img)
	return body
}

func encodeStreamCreate(surfaceID, streamID uint32, flags, codec uint8, w, h int, dest image.Rectangle) []byte {
	// surface_id, id, flags, codec, stamp u64, stream_w/h, src_w/h, dest rect, clip none
	buf := make([]byte, protocol.StreamCreateFixedSize+1)
	binary.LittleEndian.PutUint32(buf[0:4], surfaceID)
	binary.LittleEndian.PutUint32(buf[4:8], streamID)
	buf[8] = flags
	buf[9] = codec
	// stamp zero
	binary.LittleEndian.PutUint32(buf[18:22], uint32(w))
	binary.LittleEndian.PutUint32(buf[22:26], uint32(h))
	binary.LittleEndian.PutUint32(buf[26:30], uint32(w))
	binary.LittleEndian.PutUint32(buf[30:34], uint32(h))
	binary.LittleEndian.PutUint32(buf[34:38], uint32(dest.Min.Y))
	binary.LittleEndian.PutUint32(buf[38:42], uint32(dest.Min.X))
	binary.LittleEndian.PutUint32(buf[42:46], uint32(dest.Max.Y))
	binary.LittleEndian.PutUint32(buf[46:50], uint32(dest.Max.X))
	buf[50] = protocol.ClipTypeNone
	return buf
}

func encodeStreamData(streamID, mmTime uint32, frame []byte) []byte {
	buf := make([]byte, 12+len(frame))
	binary.LittleEndian.PutUint32(buf[0:4], streamID)
	binary.LittleEndian.PutUint32(buf[4:8], mmTime)
	binary.LittleEndian.PutUint32(buf[8:12], uint32(len(frame)))
	copy(buf[12:], frame)
	return buf
}

func encodeSolidJPEG(t *testing.T, w, h int, r, g, b byte) []byte {
	t.Helper()
	return mustEncodeSolidJPEG(w, h, r, g, b)
}

func mustEncodeSolidJPEG(w, h int, r, g, b byte) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := y*img.Stride + x*4
			img.Pix[off+0] = r
			img.Pix[off+1] = g
			img.Pix[off+2] = b
			img.Pix[off+3] = 0xff
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95})
	return buf.Bytes()
}

// encodeDrawCopyLZ builds DRAW_COPY with a solid-color LZ_RGB image (pure literals).
// b,g,r are wire BGR channel values for every pixel.
func encodeDrawCopyLZ(surfaceID uint32, box image.Rectangle, w, h int, b, g, r byte) []byte {
	base := appendDisplayBase(nil, surfaceID, box)
	fixed := 36
	imgOff := len(base) + fixed
	img := packLZRGBImage(w, h, b, g, r)
	body := make([]byte, imgOff+len(img))
	copy(body, base)
	off := len(base)
	binary.LittleEndian.PutUint32(body[off:off+4], uint32(imgOff))
	off += 4
	binary.LittleEndian.PutUint32(body[off:off+4], 0)
	binary.LittleEndian.PutUint32(body[off+4:off+8], 0)
	binary.LittleEndian.PutUint32(body[off+8:off+12], uint32(h))
	binary.LittleEndian.PutUint32(body[off+12:off+16], uint32(w))
	off += 16
	binary.LittleEndian.PutUint16(body[off:off+2], protocol.RopdOpPut)
	copy(body[imgOff:], img)
	return body
}

// packLZRGBImage builds SpiceImage type LZ_RGB with solid BGR pixels (literal stream).
func packLZRGBImage(w, h int, b, g, r byte) []byte {
	n := w * h
	var compressed []byte
	const maxCopy = 32
	// Big-endian LZ header (magic, version 1.1, RGB32, w, h, stride, top_down=1).
	hdr := make([]byte, 28)
	binary.BigEndian.PutUint32(hdr[0:4], 0x20205a4c)
	binary.BigEndian.PutUint32(hdr[4:8], 0x00010001)
	binary.BigEndian.PutUint32(hdr[8:12], 8) // RGB32
	binary.BigEndian.PutUint32(hdr[12:16], uint32(w))
	binary.BigEndian.PutUint32(hdr[16:20], uint32(h))
	binary.BigEndian.PutUint32(hdr[20:24], uint32(w*4))
	binary.BigEndian.PutUint32(hdr[24:28], 1)
	compressed = append(compressed, hdr...)
	for i := 0; i < n; {
		chunk := n - i
		if chunk > maxCopy {
			chunk = maxCopy
		}
		compressed = append(compressed, byte(chunk-1))
		for j := 0; j < chunk; j++ {
			compressed = append(compressed, b, g, r)
		}
		i += chunk
	}

	out := make([]byte, protocol.SpiceImageDescSize+4+len(compressed))
	out[8] = protocol.ImageTypeLZRGB
	binary.LittleEndian.PutUint32(out[10:14], uint32(w))
	binary.LittleEndian.PutUint32(out[14:18], uint32(h))
	binary.LittleEndian.PutUint32(out[protocol.SpiceImageDescSize:protocol.SpiceImageDescSize+4], uint32(len(compressed)))
	copy(out[protocol.SpiceImageDescSize+4:], compressed)
	return out
}

func appendDisplayBase(buf []byte, surfaceID uint32, box image.Rectangle) []byte {
	var hdr [21]byte
	binary.LittleEndian.PutUint32(hdr[0:4], surfaceID)
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(box.Min.Y))
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(box.Min.X))
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
