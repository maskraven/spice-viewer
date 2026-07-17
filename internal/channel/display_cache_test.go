// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"encoding/binary"
	"image"
	"testing"

	"github.com/maskraven/spice-viewer/internal/channel"
	"github.com/maskraven/spice-viewer/internal/display"
	"github.com/maskraven/spice-viewer/internal/protocol"
	"github.com/maskraven/spice-viewer/pkg/spice"
)

// TestFromCacheHit paints a bitmap with CACHE_ME then redraws via FROM_CACHE.
func TestFromCacheHit(t *testing.T) {
	drv := spice.NewNullDriver()
	comp := display.NewCompositor(spice.AsDriver(drv))
	ch := channel.NewDisplay(nil, comp)
	if err := comp.CreateSurface(0, 2, 2, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary); err != nil {
		t.Fatal(err)
	}

	id := uint64(0xdeadbeef)
	bitmap := encodeCachedBitmap(t, id, true)
	fromCache := make([]byte, protocol.SpiceImageDescSize)
	binary.LittleEndian.PutUint64(fromCache[0:8], id)
	fromCache[8] = protocol.ImageTypeFromCache
	binary.LittleEndian.PutUint32(fromCache[10:14], 2)
	binary.LittleEndian.PutUint32(fromCache[14:18], 2)

	body1 := encodeDrawCopyBody(t, 0, image.Rect(0, 0, 2, 2), bitmap)
	if err := ch.HandleMessage(protocol.MsgDisplayDrawCopy, body1); err != nil {
		t.Fatal(err)
	}
	body2 := encodeDrawCopyBody(t, 0, image.Rect(0, 0, 2, 2), fromCache)
	if err := ch.HandleMessage(protocol.MsgDisplayDrawCopy, body2); err != nil {
		t.Fatal(err)
	}
	skips := ch.ImageSkipCounts()
	if skips[protocol.ImageTypeFromCache] != 0 {
		t.Fatalf("FROM_CACHE should hit, skips=%v", skips)
	}
	if drv.PresentCount < 1 {
		t.Fatalf("expected Present after cache hit, got %d", drv.PresentCount)
	}
}

func encodeCachedBitmap(t *testing.T, id uint64, cacheMe bool) []byte {
	t.Helper()
	// bitmap payload: format, flags, w, h, stride, palette_ptr, 2x2 BGRX
	payload := make([]byte, 18+16)
	payload[0] = protocol.BitmapFmt32Bit
	payload[1] = protocol.BitmapFlagTopDown
	binary.LittleEndian.PutUint32(payload[2:6], 2)
	binary.LittleEndian.PutUint32(payload[6:10], 2)
	binary.LittleEndian.PutUint32(payload[10:14], 8)
	binary.LittleEndian.PutUint32(payload[14:18], 0)
	for i := 0; i < 4; i++ {
		off := 18 + i*4
		payload[off+0] = 0
		payload[off+1] = 0
		payload[off+2] = 255
		payload[off+3] = 0
	}

	desc := make([]byte, protocol.SpiceImageDescSize+len(payload))
	binary.LittleEndian.PutUint64(desc[0:8], id)
	desc[8] = protocol.ImageTypeBitmap
	if cacheMe {
		desc[9] = protocol.ImageFlagCacheMe
	}
	binary.LittleEndian.PutUint32(desc[10:14], 2)
	binary.LittleEndian.PutUint32(desc[14:18], 2)
	copy(desc[18:], payload)
	return desc
}

func encodeDrawCopyBody(t *testing.T, surfaceID uint32, box image.Rectangle, imageBlob []byte) []byte {
	t.Helper()
	fixed := 21 + 4 + 16 + 2 + 1 + 13
	imgPtr := uint32(fixed)
	body := make([]byte, fixed+len(imageBlob))
	binary.LittleEndian.PutUint32(body[0:4], surfaceID)
	binary.LittleEndian.PutUint32(body[4:8], uint32(box.Min.Y))
	binary.LittleEndian.PutUint32(body[8:12], uint32(box.Min.X))
	binary.LittleEndian.PutUint32(body[12:16], uint32(box.Max.Y))
	binary.LittleEndian.PutUint32(body[16:20], uint32(box.Max.X))
	body[20] = protocol.ClipTypeNone
	binary.LittleEndian.PutUint32(body[21:25], imgPtr)
	binary.LittleEndian.PutUint32(body[25:29], 0)
	binary.LittleEndian.PutUint32(body[29:33], 0)
	binary.LittleEndian.PutUint32(body[33:37], uint32(box.Dy()))
	binary.LittleEndian.PutUint32(body[37:41], uint32(box.Dx()))
	copy(body[imgPtr:], imageBlob)
	return body
}
