// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package codec_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/maskraven/virt-viewer/internal/codec"
	"github.com/maskraven/virt-viewer/internal/protocol"
)

// packSpiceBitmap builds a SpiceImage (type BITMAP) with one BGRX pixel row set.
func packSpiceBitmap(w, h int, topDown bool, b, g, r, a byte) []byte {
	stride := w * 4
	header := make([]byte, protocol.SpiceImageDescSize)
	// id = 0
	header[8] = protocol.ImageTypeBitmap
	header[9] = 0 // flags
	binary.LittleEndian.PutUint32(header[10:14], uint32(w))
	binary.LittleEndian.PutUint32(header[14:18], uint32(h))

	bmFlags := byte(0)
	if topDown {
		bmFlags |= protocol.BitmapFlagTopDown
	}
	bm := make([]byte, 18+stride*h)
	bm[0] = protocol.BitmapFmt32Bit
	bm[1] = bmFlags
	binary.LittleEndian.PutUint32(bm[2:6], uint32(w))
	binary.LittleEndian.PutUint32(bm[6:10], uint32(h))
	binary.LittleEndian.PutUint32(bm[10:14], uint32(stride))
	// palette_ptr = 0 at [14:18]
	pix := bm[18:]
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := y*stride + x*4
			// BGRX on wire
			pix[off+0] = b
			pix[off+1] = g
			pix[off+2] = r
			pix[off+3] = a
		}
	}
	return append(header, bm...)
}

func TestDecodeSpiceImageRaw32(t *testing.T) {
	// Solid red-ish: B=0x11 G=0x22 R=0x33 X=0x00 on wire → RGBA 33,22,11,FF
	// (32BIT forces A=0xFF via DecodeBitmap; do not call ForceOpaque again).
	data := packSpiceBitmap(2, 2, true, 0x11, 0x22, 0x33, 0x00)
	img, err := codec.DecodeSpiceImage(data)
	if err != nil {
		t.Fatalf("DecodeSpiceImage: %v", err)
	}
	if img.Width != 2 || img.Height != 2 || img.Stride != 8 {
		t.Fatalf("dims: %dx%d stride=%d", img.Width, img.Height, img.Stride)
	}
	if len(img.Pix) != 16 {
		t.Fatalf("pix len %d", len(img.Pix))
	}
	// First pixel RGBA — alpha must be opaque without extra ForceOpaque.
	if img.Pix[0] != 0x33 || img.Pix[1] != 0x22 || img.Pix[2] != 0x11 || img.Pix[3] != 0xff {
		t.Fatalf("pixel RGBA = %02x%02x%02x%02x want 332211ff",
			img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3])
	}
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			t.Fatalf("pix[%d] alpha = %02x want ff", i, img.Pix[i])
		}
	}
}

func TestDecodeBitmapBottomUpFlip(t *testing.T) {
	// Without TOP_DOWN, first wire row is bottom of image.
	w, h := 1, 2
	stride := 4
	header := make([]byte, protocol.SpiceImageDescSize)
	header[8] = protocol.ImageTypeBitmap
	binary.LittleEndian.PutUint32(header[10:14], uint32(w))
	binary.LittleEndian.PutUint32(header[14:18], uint32(h))

	bm := make([]byte, 18+stride*h)
	bm[0] = protocol.BitmapFmt32Bit
	bm[1] = 0 // no TOP_DOWN
	binary.LittleEndian.PutUint32(bm[2:6], uint32(w))
	binary.LittleEndian.PutUint32(bm[6:10], uint32(h))
	binary.LittleEndian.PutUint32(bm[10:14], uint32(stride))
	// Wire row 0 (bottom): pure red BGRX → R=0xff
	bm[18+0] = 0x00
	bm[18+1] = 0x00
	bm[18+2] = 0xff
	bm[18+3] = 0x00
	// Wire row 1 (top): pure blue BGRX → B=0xff
	bm[18+4] = 0xff
	bm[18+5] = 0x00
	bm[18+6] = 0x00
	bm[18+7] = 0x00

	img, err := codec.DecodeSpiceImage(append(header, bm...))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Top row (y=0) should be blue (from wire row 1)
	if img.Pix[0] != 0x00 || img.Pix[1] != 0x00 || img.Pix[2] != 0xff {
		t.Fatalf("top pixel = %02x%02x%02x want blue", img.Pix[0], img.Pix[1], img.Pix[2])
	}
	// Bottom row (y=1) should be red
	if img.Pix[4] != 0xff || img.Pix[5] != 0x00 || img.Pix[6] != 0x00 {
		t.Fatalf("bottom pixel = %02x%02x%02x want red", img.Pix[4], img.Pix[5], img.Pix[6])
	}
}

func TestDecodeSpiceImageUnsupportedType(t *testing.T) {
	// GLZ is not implemented in Phase 2; still soft-skipped via UnsupportedImageError.
	data := make([]byte, protocol.SpiceImageDescSize+4)
	data[8] = protocol.ImageTypeGLZRGB
	binary.LittleEndian.PutUint32(data[10:14], 1)
	binary.LittleEndian.PutUint32(data[14:18], 1)
	_, err := codec.DecodeSpiceImage(data)
	if err == nil {
		t.Fatal("expected error for GLZ image")
	}
	var uerr *codec.UnsupportedImageError
	if !errors.As(err, &uerr) || uerr.Type != protocol.ImageTypeGLZRGB {
		t.Fatalf("want UnsupportedImageError GLZ, got %v", err)
	}
	if !errors.Is(err, codec.ErrUnsupportedImage) {
		t.Fatalf("want ErrUnsupportedImage unwrap, got %v", err)
	}
}

func TestDecodeBitmapBounds(t *testing.T) {
	// Oversized dimension
	payload := make([]byte, 18)
	payload[0] = protocol.BitmapFmt32Bit
	payload[1] = protocol.BitmapFlagTopDown
	binary.LittleEndian.PutUint32(payload[2:6], protocol.MaxSurfaceSide+1)
	binary.LittleEndian.PutUint32(payload[6:10], 1)
	binary.LittleEndian.PutUint32(payload[10:14], (protocol.MaxSurfaceSide+1)*4)
	_, err := codec.DecodeBitmap(payload, 0, 0)
	if err == nil {
		t.Fatal("expected dimension reject")
	}
}

func TestDecodeBitmapShortData(t *testing.T) {
	payload := make([]byte, 18)
	payload[0] = protocol.BitmapFmt32Bit
	payload[1] = protocol.BitmapFlagTopDown
	binary.LittleEndian.PutUint32(payload[2:6], 10)
	binary.LittleEndian.PutUint32(payload[6:10], 10)
	binary.LittleEndian.PutUint32(payload[10:14], 40)
	_, err := codec.DecodeBitmap(payload, 10, 10)
	if err == nil {
		t.Fatal("expected short data error")
	}
}
