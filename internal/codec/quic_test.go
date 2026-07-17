// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package codec_test

import (
	"encoding/binary"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/maskraven/virt-viewer/internal/codec"
	"github.com/maskraven/virt-viewer/internal/protocol"
)

func packSpiceQuic(w, h int, quicBytes []byte) []byte {
	header := make([]byte, protocol.SpiceImageDescSize)
	header[8] = protocol.ImageTypeQuic
	binary.LittleEndian.PutUint32(header[10:14], uint32(w))
	binary.LittleEndian.PutUint32(header[14:18], uint32(h))
	payload := make([]byte, 4+len(quicBytes))
	binary.LittleEndian.PutUint32(payload[:4], uint32(len(quicBytes)))
	copy(payload[4:], quicBytes)
	return append(header, payload...)
}

func TestDecodeQuicFixture(t *testing.T) {
	quicPath := filepath.Join("testdata", "test1.quic")
	pngPath := filepath.Join("testdata", "test1.png")
	qb, err := os.ReadFile(quicPath)
	if err != nil {
		t.Fatalf("read quic fixture: %v", err)
	}
	f, err := os.Open(pngPath)
	if err != nil {
		t.Fatalf("read png fixture: %v", err)
	}
	defer f.Close()
	master, err := png.Decode(f)
	if err != nil {
		t.Fatalf("png decode: %v", err)
	}
	mb := master.Bounds()
	w, h := mb.Dx(), mb.Dy()

	data := packSpiceQuic(w, h, qb)
	img, err := codec.DecodeSpiceImage(data)
	if err != nil {
		t.Fatalf("DecodeSpiceImage Quic: %v", err)
	}
	if img.Width != w || img.Height != h {
		t.Fatalf("dims %dx%d want %dx%d", img.Width, img.Height, w, h)
	}

	// Compare against master PNG pixel-for-pixel (fixture is lossless Quic RGB24).
	mismatches := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := master.At(mb.Min.X+x, mb.Min.Y+y).RGBA()
			off := y*img.Stride + x*4
			if img.Pix[off] != uint8(r>>8) || img.Pix[off+1] != uint8(g>>8) ||
				img.Pix[off+2] != uint8(b>>8) || img.Pix[off+3] != uint8(a>>8) {
				mismatches++
				if mismatches <= 3 {
					t.Logf("mismatch at %d,%d got %02x%02x%02x%02x want %02x%02x%02x%02x",
						x, y,
						img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3],
						uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8))
				}
			}
		}
	}
	if mismatches != 0 {
		t.Fatalf("%d pixel mismatches vs golden PNG", mismatches)
	}
}

func TestDecodeQuicBadMagic(t *testing.T) {
	// data_size=4, garbage bitstream
	payload := []byte{4, 0, 0, 0, 'N', 'O', 'P', 'E'}
	_, err := codec.DecodeQuic(payload, 0, 0)
	if err == nil {
		t.Fatal("expected bad magic error")
	}
}

func TestDecodeQuicEmpty(t *testing.T) {
	payload := []byte{0, 0, 0, 0}
	_, err := codec.DecodeQuic(payload, 1, 1)
	if err == nil {
		t.Fatal("expected empty data error")
	}
}

// Ensure image.Image type assertion path is exercised via DecodeQuic public API.
func TestDecodeQuicDimsMismatch(t *testing.T) {
	qb, err := os.ReadFile(filepath.Join("testdata", "test1.quic"))
	if err != nil {
		t.Fatal(err)
	}
	// Fixture is 59x24; claim wrong size.
	payload := make([]byte, 4+len(qb))
	binary.LittleEndian.PutUint32(payload[:4], uint32(len(qb)))
	copy(payload[4:], qb)
	_, err = codec.DecodeQuic(payload, 10, 10)
	if err == nil {
		t.Fatal("expected dimension mismatch")
	}
}
