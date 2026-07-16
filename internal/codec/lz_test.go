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

// packLZHeader builds the 28-byte big-endian LZ stream header.
func packLZHeader(typ, w, h, stride, topDown uint32) []byte {
	hbuf := make([]byte, 28)
	binary.BigEndian.PutUint32(hbuf[0:4], 0x20205a4c) // LZ magic
	binary.BigEndian.PutUint32(hbuf[4:8], 0x00010001) // version 1.1
	binary.BigEndian.PutUint32(hbuf[8:12], typ)
	binary.BigEndian.PutUint32(hbuf[12:16], w)
	binary.BigEndian.PutUint32(hbuf[16:20], h)
	binary.BigEndian.PutUint32(hbuf[20:24], stride)
	binary.BigEndian.PutUint32(hbuf[24:28], topDown)
	return hbuf
}

// encodeLZRGB32Literals emits an LZ RGB32 stream of pure literals (no matches).
// pixels is tightly packed RGBA; only R,G,B are encoded (wire B,G,R).
func encodeLZRGB32Literals(w, h int, topDown bool, pixels []byte) []byte {
	n := w * h
	var body []byte
	// Emit in chunks of at most lzMaxCopy (32) literals.
	const maxCopy = 32
	for i := 0; i < n; {
		chunk := n - i
		if chunk > maxCopy {
			chunk = maxCopy
		}
		body = append(body, byte(chunk-1)) // copy count biased by 1
		for j := 0; j < chunk; j++ {
			off := (i + j) * 4
			r, g, b := pixels[off], pixels[off+1], pixels[off+2]
			body = append(body, b, g, r)
		}
		i += chunk
	}
	td := uint32(0)
	if topDown {
		td = 1
	}
	stream := append(packLZHeader(8 /*RGB32*/, uint32(w), uint32(h), uint32(w*4), td), body...)
	return stream
}

// encodeLZRGB32SolidRLE encodes a solid-color RGB32 image: 1 literal + RLE match.
func encodeLZRGB32SolidRLE(w, h int, r, g, b byte) []byte {
	n := w * h
	if n < 1 {
		panic("empty")
	}
	var body []byte
	// 1 literal
	body = append(body, 0x00, b, g, r)
	if n > 1 {
		// Match of length (n-1), offset 1.
		// RGB32: final_len = (ctrl>>5) when <7 after biases; for short runs use low len.
		remain := n - 1
		// Emit matches of up to a reasonable size. For remain < 7: ctrl>>5 = remain.
		for remain > 0 {
			var length int
			if remain <= 6 {
				length = remain
				ctrl := byte(length << 5)       // ofs high bits 0
				body = append(body, ctrl, 0x00) // ofs low = 0 → final ofs=1
				remain = 0
			} else {
				// length coded as 7 + extra, with final = 7+extra for RGB32
				// after: length-- → 6; +codes; +1 → 7+sum
				extra := remain - 7
				if extra > 254 {
					// multi-byte extension: use 255s
					body = append(body, 7<<5) // ctrl with len field 7
					for extra >= 255 {
						body = append(body, 255)
						extra -= 255
					}
					body = append(body, byte(extra), 0x00)
					remain = 0
				} else {
					body = append(body, 7<<5, byte(extra), 0x00)
					remain = 0
				}
			}
		}
	}
	stream := append(packLZHeader(8, uint32(w), uint32(h), uint32(w*4), 1), body...)
	return stream
}

// packSpiceLZRGB wraps an LZ stream as SpiceImage type LZ_RGB.
func packSpiceLZRGB(w, h int, stream []byte) []byte {
	header := make([]byte, protocol.SpiceImageDescSize)
	header[8] = protocol.ImageTypeLZRGB
	binary.LittleEndian.PutUint32(header[10:14], uint32(w))
	binary.LittleEndian.PutUint32(header[14:18], uint32(h))
	payload := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(payload[:4], uint32(len(stream)))
	copy(payload[4:], stream)
	return append(header, payload...)
}

func TestDecodeLZRGBSolidLiteral(t *testing.T) {
	// 2×2 solid blue via pure literals.
	pix := make([]byte, 2*2*4)
	for i := 0; i < 4; i++ {
		pix[i*4+0] = 0x00 // R
		pix[i*4+1] = 0x00 // G
		pix[i*4+2] = 0xff // B
		pix[i*4+3] = 0xff
	}
	stream := encodeLZRGB32Literals(2, 2, true, pix)
	data := packSpiceLZRGB(2, 2, stream)
	img, err := codec.DecodeSpiceImage(data)
	if err != nil {
		t.Fatalf("DecodeSpiceImage LZ: %v", err)
	}
	if img.Width != 2 || img.Height != 2 || img.Stride != 8 {
		t.Fatalf("dims %dx%d stride=%d", img.Width, img.Height, img.Stride)
	}
	for i := 0; i < 4; i++ {
		off := i * 4
		if img.Pix[off] != 0x00 || img.Pix[off+1] != 0x00 || img.Pix[off+2] != 0xff || img.Pix[off+3] != 0xff {
			t.Fatalf("pixel[%d]=%02x%02x%02x%02x want 0000ffff", i,
				img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3])
		}
	}
}

func TestDecodeLZRGBSolidRLE(t *testing.T) {
	// 3×3 solid red via 1 literal + RLE.
	stream := encodeLZRGB32SolidRLE(3, 3, 0xcc, 0x11, 0x22)
	data := packSpiceLZRGB(3, 3, stream)
	img, err := codec.DecodeSpiceImage(data)
	if err != nil {
		t.Fatalf("DecodeSpiceImage RLE: %v", err)
	}
	if img.Width != 3 || img.Height != 3 {
		t.Fatalf("dims %dx%d", img.Width, img.Height)
	}
	for i := 0; i < 9; i++ {
		off := i * 4
		if img.Pix[off] != 0xcc || img.Pix[off+1] != 0x11 || img.Pix[off+2] != 0x22 || img.Pix[off+3] != 0xff {
			t.Fatalf("pixel[%d]=%02x%02x%02x%02x want cc1122ff", i,
				img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3])
		}
	}
}

func TestDecodeLZRGBBottomUpFlip(t *testing.T) {
	// 1×2: top pixel red, bottom blue — encoded bottom-up so wire order is bottom then top.
	// When top_down=0, first decoded row is the bottom of the image and must be flipped.
	pixels := []byte{
		// first in stream (bottom): blue
		0x00, 0x00, 0xff, 0xff,
		// second (top): red
		0xff, 0x00, 0x00, 0xff,
	}
	stream := encodeLZRGB32Literals(1, 2, false, pixels)
	img, err := codec.DecodeLZRGB(append(
		func() []byte {
			p := make([]byte, 4+len(stream))
			binary.LittleEndian.PutUint32(p[:4], uint32(len(stream)))
			copy(p[4:], stream)
			return p
		}(),
	), 1, 2)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// After flip: y=0 is red, y=1 is blue.
	if img.Pix[0] != 0xff || img.Pix[1] != 0x00 || img.Pix[2] != 0x00 {
		t.Fatalf("top=%02x%02x%02x want red", img.Pix[0], img.Pix[1], img.Pix[2])
	}
	if img.Pix[4] != 0x00 || img.Pix[5] != 0x00 || img.Pix[6] != 0xff {
		t.Fatalf("bottom=%02x%02x%02x want blue", img.Pix[4], img.Pix[5], img.Pix[6])
	}
}

func TestDecodeLZRGBGolden1x1(t *testing.T) {
	// Hand-crafted 1×1 RGB32 green (B=0 G=0xff R=0), top-down.
	// Header + ctrl=0 + BGR.
	stream := []byte{
		0x20, 0x20, 0x5a, 0x4c, // magic
		0x00, 0x01, 0x00, 0x01, // version
		0x00, 0x00, 0x00, 0x08, // RGB32
		0x00, 0x00, 0x00, 0x01, // w
		0x00, 0x00, 0x00, 0x01, // h
		0x00, 0x00, 0x00, 0x04, // stride
		0x00, 0x00, 0x00, 0x01, // top_down
		0x00,             // 1 literal
		0x00, 0xff, 0x00, // BGR green
	}
	payload := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(payload[:4], uint32(len(stream)))
	copy(payload[4:], stream)
	img, err := codec.DecodeLZRGB(payload, 1, 1)
	if err != nil {
		t.Fatalf("golden: %v", err)
	}
	if img.Pix[0] != 0x00 || img.Pix[1] != 0xff || img.Pix[2] != 0x00 || img.Pix[3] != 0xff {
		t.Fatalf("pixel=%02x%02x%02x%02x want 00ff00ff", img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3])
	}
}

func TestDecodeLZRGBBadMagic(t *testing.T) {
	stream := make([]byte, 28)
	binary.BigEndian.PutUint32(stream[0:4], 0xdeadbeef)
	payload := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(payload[:4], uint32(len(stream)))
	copy(payload[4:], stream)
	_, err := codec.DecodeLZRGB(payload, 0, 0)
	if err == nil {
		t.Fatal("expected bad magic error")
	}
}

func TestDecodeLZRGBShortData(t *testing.T) {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, 100)
	_, err := codec.DecodeLZRGB(payload, 1, 1)
	if err == nil {
		t.Fatal("expected short data")
	}
}

func TestDecodeLZRGBMultiChunkLiterals(t *testing.T) {
	// 40 pixels requires two literal blocks (32 + 8).
	w, h := 8, 5
	pix := make([]byte, w*h*4)
	for i := 0; i < w*h; i++ {
		pix[i*4+0] = byte(i)
		pix[i*4+1] = byte(i + 1)
		pix[i*4+2] = byte(i + 2)
		pix[i*4+3] = 0xff
	}
	stream := encodeLZRGB32Literals(w, h, true, pix)
	img, err := codec.DecodeLZRGB(func() []byte {
		p := make([]byte, 4+len(stream))
		binary.LittleEndian.PutUint32(p[:4], uint32(len(stream)))
		copy(p[4:], stream)
		return p
	}(), uint32(w), uint32(h))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for i := 0; i < w*h; i++ {
		off := i * 4
		if img.Pix[off] != byte(i) || img.Pix[off+1] != byte(i+1) || img.Pix[off+2] != byte(i+2) {
			t.Fatalf("pixel[%d]=%02x%02x%02x want %02x%02x%02x", i,
				img.Pix[off], img.Pix[off+1], img.Pix[off+2],
				byte(i), byte(i+1), byte(i+2))
		}
		if img.Pix[off+3] != 0xff {
			t.Fatalf("alpha[%d]=%02x", i, img.Pix[off+3])
		}
	}
}

func TestDecodeSpiceImageStillUnsupportedQuic(t *testing.T) {
	data := make([]byte, protocol.SpiceImageDescSize+4)
	data[8] = protocol.ImageTypeQuic
	binary.LittleEndian.PutUint32(data[10:14], 1)
	binary.LittleEndian.PutUint32(data[14:18], 1)
	_, err := codec.DecodeSpiceImage(data)
	if err == nil {
		t.Fatal("expected error for Quic image")
	}
	var uerr *codec.UnsupportedImageError
	if !errors.As(err, &uerr) || uerr.Type != protocol.ImageTypeQuic {
		t.Fatalf("want UnsupportedImageError Quic, got %v", err)
	}
	if !errors.Is(err, codec.ErrUnsupportedImage) {
		t.Fatalf("want ErrUnsupportedImage unwrap, got %v", err)
	}
}

func TestDecodeLZRGBAWithAlpha(t *testing.T) {
	// 1×1 RGBA: RGB plane one literal, alpha plane one literal.
	// RGB32 type field is 9 for RGBA; stream has RGB then alpha.
	var body []byte
	body = append(body, 0x00, 0x11, 0x22, 0x33) // BGR literal
	body = append(body, 0x00, 0xaa)             // alpha literal 0xaa
	stream := append(packLZHeader(9 /*RGBA*/, 1, 1, 4, 1), body...)
	payload := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(payload[:4], uint32(len(stream)))
	copy(payload[4:], stream)
	img, err := codec.DecodeLZRGB(payload, 1, 1)
	if err != nil {
		t.Fatalf("RGBA: %v", err)
	}
	// R=0x33 G=0x22 B=0x11 A=0xaa
	if img.Pix[0] != 0x33 || img.Pix[1] != 0x22 || img.Pix[2] != 0x11 || img.Pix[3] != 0xaa {
		t.Fatalf("pixel=%02x%02x%02x%02x want 332211aa",
			img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3])
	}
}
