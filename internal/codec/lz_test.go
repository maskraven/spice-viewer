// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package codec_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/maskraven/spice-viewer/internal/codec"
	"github.com/maskraven/spice-viewer/internal/protocol"
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
	img, err := codec.DecodeLZRGB(func() []byte {
		p := make([]byte, 4+len(stream))
		binary.LittleEndian.PutUint32(p[:4], uint32(len(stream)))
		copy(p[4:], stream)
		return p
	}(), 1, 2)
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

func TestDecodeSpiceImageStillUnsupportedGLZ(t *testing.T) {
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

func payloadFromStream(stream []byte) []byte {
	p := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(p[:4], uint32(len(stream)))
	copy(p[4:], stream)
	return p
}

func TestDecodeLZRGB16Golden1x1(t *testing.T) {
	// RGB555 full red: r5=0x1f → packed 0x7c00 → wire hi=0x7c lo=0x00
	// Expand: R = (0x1f<<3)|(0x1f>>2) = 0xff
	stream := []byte{
		0x20, 0x20, 0x5a, 0x4c, // magic
		0x00, 0x01, 0x00, 0x01, // version
		0x00, 0x00, 0x00, 0x06, // RGB16
		0x00, 0x00, 0x00, 0x01, // w
		0x00, 0x00, 0x00, 0x01, // h
		0x00, 0x00, 0x00, 0x02, // stride
		0x00, 0x00, 0x00, 0x01, // top_down
		0x00,       // 1 literal
		0x7c, 0x00, // RGB555 red
	}
	img, err := codec.DecodeLZRGB(payloadFromStream(stream), 1, 1)
	if err != nil {
		t.Fatalf("rgb16 golden: %v", err)
	}
	if img.Pix[0] != 0xff || img.Pix[1] != 0x00 || img.Pix[2] != 0x00 || img.Pix[3] != 0xff {
		t.Fatalf("pixel=%02x%02x%02x%02x want ff0000ff",
			img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3])
	}
}

func TestDecodeLZRGB16SolidRLE(t *testing.T) {
	// 2×2 solid green RGB555: g5=0x1f → 0x03e0 → hi=0x03 lo=0xe0
	// Expand G = (0x1f<<3)|(0x1f>>2) = 0xff
	// Stream: 1 literal + RLE match len=3 ofs=1 (RGB16 bias +2 → ctrl>>5 field = 1 for final len 3?
	// final_len = (ctrl>>5 - 1) + 2 = ctrl>>5 + 1. Want final=3 → ctrl>>5 = 2.
	// ctrl = (2<<5)|0 = 0x40, ofs byte 0 → final ofs 1.
	var body []byte
	body = append(body, 0x00, 0x03, 0xe0) // 1 literal green
	body = append(body, 0x40, 0x00)       // match len=3 ofs=1
	stream := append(packLZHeader(6 /*RGB16*/, 2, 2, 4, 1), body...)
	img, err := codec.DecodeLZRGB(payloadFromStream(stream), 2, 2)
	if err != nil {
		t.Fatalf("rgb16 rle: %v", err)
	}
	for i := 0; i < 4; i++ {
		off := i * 4
		if img.Pix[off] != 0x00 || img.Pix[off+1] != 0xff || img.Pix[off+2] != 0x00 || img.Pix[off+3] != 0xff {
			t.Fatalf("pixel[%d]=%02x%02x%02x%02x want 00ff00ff", i,
				img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3])
		}
	}
}

func TestDecodeLZRGBDictBackref(t *testing.T) {
	// Non-RLE dictionary: [A, B] then match ofs=2 len=2 → [A, B, A, B].
	// A = red (BGR 00 00 ff), B = blue (BGR ff 00 00)
	// 2 literals: ctrl=1
	// match len=2 ofs=2: final_len=(ctrl>>5-1)+1 = ctrl>>5 → want 2 → ctrl high=2
	// stored ofs = 1 (code=1): ctrl low=0, code=1
	body := []byte{
		0x01,             // 2 literals
		0x00, 0x00, 0xff, // A red
		0xff, 0x00, 0x00, // B blue
		0x40, 0x01, // match len=2 ofs=2
	}
	stream := append(packLZHeader(8, 2, 2, 8, 1), body...)
	img, err := codec.DecodeLZRGB(payloadFromStream(stream), 2, 2)
	if err != nil {
		t.Fatalf("dict backref: %v", err)
	}
	want := [][3]byte{
		{0xff, 0x00, 0x00}, // A red
		{0x00, 0x00, 0xff}, // B blue
		{0xff, 0x00, 0x00}, // A
		{0x00, 0x00, 0xff}, // B
	}
	for i, c := range want {
		off := i * 4
		if img.Pix[off] != c[0] || img.Pix[off+1] != c[1] || img.Pix[off+2] != c[2] || img.Pix[off+3] != 0xff {
			t.Fatalf("pixel[%d]=%02x%02x%02x%02x want %02x%02x%02xff",
				i, img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3], c[0], c[1], c[2])
		}
	}
}

func TestDecodeLZRGBFarDistance(t *testing.T) {
	// Far distance triggers when (ctrl&31)==31 and code==255, then
	// ofs = (hi<<8|lo) + MAX_DISTANCE(8191), then +1 bias.
	// Minimal far distance: hi=lo=0 → final ofs = 8192.
	// Image 8192×2: first row solid red via RLE, second row first pixel via far match.
	const w, h = 8192, 2
	var body []byte
	// 1 literal red + long RLE for remaining 8191 of first row… but RLE only covers
	// consecutive matches of the previous pixel. Emit: 1 lit + match len 8191 ofs=1.
	// final_len for RGB32 with extension: length starts as 6 after --, +extra +1 bias.
	// Want final=8191 → after bias length_before_bias+1=8191 → length_before_bias=8190
	// After -- from field 7: length=6; need 8190-6=8184 more from extension bytes.
	body = append(body, 0x00, 0x00, 0x00, 0xff) // red literal
	// ctrl with len field 7, ofs high 0: 0xe0
	body = append(body, 0xe0)
	// Extension: 8184 = 32*255 + 24 → thirty-two 0xFF then 0x18
	extra := 8184
	for extra >= 255 {
		body = append(body, 255)
		extra -= 255
	}
	body = append(body, byte(extra))
	body = append(body, 0x00) // ofs low → final ofs 1 (RLE)

	// Far match: len=1, ofs=8192. ctrl = (1<<5)|31 = 0x3f, code=0xff, hi=0, lo=0
	// Then 8191 more RLE of that pixel to fill the second row, or one far match of 8192.
	// Single far match of length 8192:
	// final=8192 → length_before_bias=8191; field 7 → 6 + ext; ext=8185
	body = append(body, 0xff) // ctrl: len field 7, ofs high 31 → (7<<5)|31 = 0xff
	extra = 8185
	for extra >= 255 {
		body = append(body, 255)
		extra -= 255
	}
	body = append(body, byte(extra))
	body = append(body, 0xff, 0x00, 0x00) // far distance code + hi + lo

	stream := append(packLZHeader(8, w, h, w*4, 1), body...)
	img, err := codec.DecodeLZRGB(payloadFromStream(stream), w, h)
	if err != nil {
		t.Fatalf("far distance: %v", err)
	}
	if img.Width != w || img.Height != h {
		t.Fatalf("dims %dx%d", img.Width, img.Height)
	}
	// Every pixel red opaque
	for i := 0; i < w*h; i++ {
		off := i * 4
		if img.Pix[off] != 0xff || img.Pix[off+1] != 0x00 || img.Pix[off+2] != 0x00 || img.Pix[off+3] != 0xff {
			t.Fatalf("pixel[%d]=%02x%02x%02x%02x want ff0000ff", i,
				img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3])
		}
	}
}

func TestDecodeLZRGBMatchLenOverflow(t *testing.T) {
	// Malicious stream: ctrl with max length field + many 0xFF extension bytes
	// that would wrap a uint32 length. Decoder must reject, not wrap.
	var body []byte
	body = append(body, 0x00, 0x00, 0x00, 0xff) // 1 red literal so a match is legal at op=1
	// Match with len field 7 and ~20×0xFF would yield huge length; remaining is 0 after 1×1.
	// Use 1×2 image so remaining=1 after first literal — any extended match >1 fails.
	body = append(body, 0xe0) // len field 7, ofs high 0
	// Enough 0xFF to exceed remaining before bias even without wrap
	for i := 0; i < 8; i++ {
		body = append(body, 255)
	}
	body = append(body, 0)    // terminate extension with non-0xFF (won't be reached if capped early)
	body = append(body, 0x00) // ofs

	stream := append(packLZHeader(8, 1, 2, 4, 1), body...)
	_, err := codec.DecodeLZRGB(payloadFromStream(stream), 1, 2)
	if err == nil {
		t.Fatal("expected match length overflow error")
	}
}

func TestDecodeLZRGBDimMismatch(t *testing.T) {
	stream := encodeLZRGB32Literals(2, 2, true, make([]byte, 16))
	_, err := codec.DecodeLZRGB(payloadFromStream(stream), 3, 2)
	if err == nil {
		t.Fatal("expected width mismatch")
	}
}

func TestDecodeLZRGBBadVersion(t *testing.T) {
	stream := packLZHeader(8, 1, 1, 4, 1)
	binary.BigEndian.PutUint32(stream[4:8], 0x00020000) // bad version
	stream = append(stream, 0x00, 0x00, 0x00, 0x00)
	_, err := codec.DecodeLZRGB(payloadFromStream(stream), 1, 1)
	if err == nil {
		t.Fatal("expected bad version")
	}
}

func TestDecodeLZRGBUnsupportedType(t *testing.T) {
	// Palette type inside LZ_RGB stream header is not supported here.
	stream := packLZHeader(5 /*PLT8*/, 1, 1, 1, 1)
	stream = append(stream, 0x00, 0x00)
	_, err := codec.DecodeLZRGB(payloadFromStream(stream), 1, 1)
	if err == nil {
		t.Fatal("expected unsupported lz type")
	}
}
