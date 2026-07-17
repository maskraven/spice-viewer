// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package codec_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/maskraven/spice-viewer/internal/codec"
	"github.com/maskraven/spice-viewer/internal/protocol"
)

func encodeTestJPEG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	// Quality 90 keeps solid colors close enough for threshold checks.
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	return buf.Bytes()
}

func packSpiceJPEG(w, h int, jpegBytes []byte) []byte {
	header := make([]byte, protocol.SpiceImageDescSize)
	header[8] = protocol.ImageTypeJPEG
	binary.LittleEndian.PutUint32(header[10:14], uint32(w))
	binary.LittleEndian.PutUint32(header[14:18], uint32(h))
	payload := make([]byte, 4+len(jpegBytes))
	binary.LittleEndian.PutUint32(payload[:4], uint32(len(jpegBytes)))
	copy(payload[4:], jpegBytes)
	return append(header, payload...)
}

func TestDecodeJPEGSolid(t *testing.T) {
	// Solid blue-ish; JPEG is lossy so allow small channel error.
	jb := encodeTestJPEG(t, 4, 4, color.RGBA{R: 10, G: 20, B: 200, A: 255})
	data := packSpiceJPEG(4, 4, jb)
	img, err := codec.DecodeSpiceImage(data)
	if err != nil {
		t.Fatalf("DecodeSpiceImage JPEG: %v", err)
	}
	if img.Width != 4 || img.Height != 4 {
		t.Fatalf("dims %dx%d", img.Width, img.Height)
	}
	// Sample center-ish pixel.
	off := (1*img.Stride + 1*4)
	if abs(int(img.Pix[off+2])-200) > 30 { // B channel
		t.Fatalf("pixel B=%d want ~200 (RGBA=%02x%02x%02x%02x)",
			img.Pix[off+2], img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3])
	}
	if img.Pix[off+3] != 0xff {
		t.Fatalf("alpha=%02x want ff", img.Pix[off+3])
	}
}

func TestDecodeJPEGBytes(t *testing.T) {
	jb := encodeTestJPEG(t, 2, 2, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	img, err := codec.DecodeJPEGBytes(jb)
	if err != nil {
		t.Fatalf("DecodeJPEGBytes: %v", err)
	}
	if img.Width != 2 || img.Height != 2 {
		t.Fatalf("dims %dx%d", img.Width, img.Height)
	}
	// Red should dominate R channel.
	if img.Pix[0] < 200 {
		t.Fatalf("R=%d want high red", img.Pix[0])
	}
}

func TestDecodeJPEGShort(t *testing.T) {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, 10)
	_, err := codec.DecodeJPEG(payload, 1, 1)
	if err == nil {
		t.Fatal("expected short jpeg error")
	}
}

func TestDecodeJPEGBadData(t *testing.T) {
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint32(payload[:4], 4)
	copy(payload[4:], []byte{0, 1, 2, 3})
	_, err := codec.DecodeJPEG(payload, 0, 0)
	if err == nil {
		t.Fatal("expected bad jpeg error")
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
