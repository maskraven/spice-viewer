// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package codec_test

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"testing"

	"github.com/maskraven/virt-viewer/internal/codec"
	"github.com/maskraven/virt-viewer/internal/protocol"
)

// packGLZHeader builds the 33-byte big-endian GLZ stream header.
func packGLZHeader(typ uint8, topDown bool, w, h, stride uint32, id uint64, winHeadDist uint32) []byte {
	hbuf := make([]byte, 33)
	binary.BigEndian.PutUint32(hbuf[0:4], 0x20205a4c)
	binary.BigEndian.PutUint32(hbuf[4:8], 0x00010001)
	tb := typ & 0x0f
	if topDown {
		tb |= 1 << 4
	}
	hbuf[8] = tb
	binary.BigEndian.PutUint32(hbuf[9:13], w)
	binary.BigEndian.PutUint32(hbuf[13:17], h)
	binary.BigEndian.PutUint32(hbuf[17:21], stride)
	binary.BigEndian.PutUint64(hbuf[21:29], id)
	binary.BigEndian.PutUint32(hbuf[29:33], winHeadDist)
	return hbuf
}

// encodeGLZRGB32Literals emits a GLZ RGB32 stream of pure literals (no matches).
// pixels is tightly packed RGBA; only R,G,B are encoded (wire B,G,R).
func encodeGLZRGB32Literals(w, h int, topDown bool, id uint64, winHeadDist uint32, pixels []byte) []byte {
	n := w * h
	var body []byte
	const maxCopy = 32
	for i := 0; i < n; {
		chunk := n - i
		if chunk > maxCopy {
			chunk = maxCopy
		}
		body = append(body, byte(chunk-1))
		for j := 0; j < chunk; j++ {
			off := (i + j) * 4
			r, g, b := pixels[off], pixels[off+1], pixels[off+2]
			body = append(body, b, g, r)
		}
		i += chunk
	}
	hdr := packGLZHeader(8 /*RGB32*/, topDown, uint32(w), uint32(h), uint32(w*4), id, winHeadDist)
	return append(hdr, body...)
}

// encodeGLZRGB32SolidRLE: 1 literal + same-image RLE for remaining pixels.
func encodeGLZRGB32SolidRLE(w, h int, id uint64, winHeadDist uint32, r, g, b byte) []byte {
	n := w * h
	var body []byte
	body = append(body, 0x00, b, g, r) // 1 literal
	remain := n - 1
	for remain > 0 {
		var length int
		if remain <= 6 {
			length = remain
			// ctrl: len<<5 | pixel_flag=0 | ofs_low=0
			body = append(body, byte(length<<5), 0x00, 0x00)
			remain = 0
		} else {
			// length >= 7: field 7 + extension (0 for exactly 7)
			extra := remain - 7
			body = append(body, 7<<5)
			for extra >= 255 {
				body = append(body, 255)
				extra -= 255
			}
			body = append(body, byte(extra), 0x00, 0x00) // ext, pixel_ofs hi, image
			remain = 0
		}
	}
	hdr := packGLZHeader(8, true, uint32(w), uint32(h), uint32(w*4), id, winHeadDist)
	return append(hdr, body...)
}

// encodeGLZRGB32CopyFromPrev encodes an image that is a full-pixel copy from
// (id - imageDist) starting at pixel 0. Assumes same dimensions.
func encodeGLZRGB32CopyFromPrev(w, h int, id uint64, winHeadDist uint32, imageDist uint32) []byte {
	n := w * h
	var body []byte
	// One match covering all n pixels from other image, pixel_ofs=0, image_dist set.
	// length coding: if n <= 6 use field n; if n==7 need field 7 + ext 0; else 7+extra
	remain := n
	// We emit a single match of length n.
	length := remain
	if length <= 6 {
		body = append(body, byte(length<<5)) // pixel_flag=0, ofs_low=0
		body = append(body, 0x00)            // pixel_ofs high
		// image_flag=0, image_dist in low 6 bits (must fit)
		if imageDist > 0x3f {
			panic("imageDist too large for short encoding")
		}
		body = append(body, byte(imageDist&0x3f))
	} else {
		extra := length - 7
		body = append(body, 7<<5)
		for extra >= 255 {
			body = append(body, 255)
			extra -= 255
		}
		body = append(body, byte(extra))
		body = append(body, 0x00) // pixel ofs high
		if imageDist > 0x3f {
			panic("imageDist too large for short encoding")
		}
		body = append(body, byte(imageDist&0x3f))
	}
	hdr := packGLZHeader(8, true, uint32(w), uint32(h), uint32(w*4), id, winHeadDist)
	return append(hdr, body...)
}

func packSpiceGLZRGB(w, h int, stream []byte) []byte {
	header := make([]byte, protocol.SpiceImageDescSize)
	header[8] = protocol.ImageTypeGLZRGB
	binary.LittleEndian.PutUint32(header[10:14], uint32(w))
	binary.LittleEndian.PutUint32(header[14:18], uint32(h))
	payload := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(payload[:4], uint32(len(stream)))
	copy(payload[4:], stream)
	return append(header, payload...)
}

func packSpiceZlibGLZRGB(w, h int, stream []byte) []byte {
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	_, _ = zw.Write(stream)
	_ = zw.Close()
	zblob := zbuf.Bytes()

	header := make([]byte, protocol.SpiceImageDescSize)
	header[8] = protocol.ImageTypeZlibGLZRGB
	binary.LittleEndian.PutUint32(header[10:14], uint32(w))
	binary.LittleEndian.PutUint32(header[14:18], uint32(h))
	payload := make([]byte, 8+len(zblob))
	binary.LittleEndian.PutUint32(payload[0:4], uint32(len(stream)))
	binary.LittleEndian.PutUint32(payload[4:8], uint32(len(zblob)))
	copy(payload[8:], zblob)
	return append(header, payload...)
}

func TestGLZDecodeLiterals(t *testing.T) {
	pix := make([]byte, 2*2*4)
	for i := 0; i < 4; i++ {
		pix[i*4+0] = 0x10
		pix[i*4+1] = 0x20
		pix[i*4+2] = 0x30
		pix[i*4+3] = 0xff
	}
	stream := encodeGLZRGB32Literals(2, 2, true, 1, 0, pix)
	payload := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(payload[:4], uint32(len(stream)))
	copy(payload[4:], stream)

	img, err := codec.DecodeGLZRGB(payload, 2, 2)
	if err != nil {
		t.Fatalf("DecodeGLZRGB: %v", err)
	}
	if img.Width != 2 || img.Height != 2 {
		t.Fatalf("dims %dx%d", img.Width, img.Height)
	}
	for i := 0; i < 4; i++ {
		off := i * 4
		if img.Pix[off] != 0x10 || img.Pix[off+1] != 0x20 || img.Pix[off+2] != 0x30 || img.Pix[off+3] != 0xff {
			t.Fatalf("pixel[%d]=%02x%02x%02x%02x", i, img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3])
		}
	}
}

func TestGLZDecodeSolidRLE(t *testing.T) {
	stream := encodeGLZRGB32SolidRLE(3, 3, 1, 0, 0xcc, 0x11, 0x22)
	payload := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(payload[:4], uint32(len(stream)))
	copy(payload[4:], stream)
	img, err := codec.DecodeGLZRGB(payload, 3, 3)
	if err != nil {
		t.Fatalf("DecodeGLZRGB RLE: %v", err)
	}
	for i := 0; i < 9; i++ {
		off := i * 4
		if img.Pix[off] != 0xcc || img.Pix[off+1] != 0x11 || img.Pix[off+2] != 0x22 || img.Pix[off+3] != 0xff {
			t.Fatalf("pixel[%d]=%02x%02x%02x%02x", i, img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3])
		}
	}
}

func TestGLZWindowCrossImageMatch(t *testing.T) {
	win := codec.NewGLZWindow(16 << 20)

	// Image id=1 solid green via literals.
	pix := make([]byte, 2*2*4)
	for i := 0; i < 4; i++ {
		pix[i*4+0] = 0x00
		pix[i*4+1] = 0xaa
		pix[i*4+2] = 0x00
		pix[i*4+3] = 0xff
	}
	stream1 := encodeGLZRGB32Literals(2, 2, true, 1, 10, pix)
	p1 := make([]byte, 4+len(stream1))
	binary.LittleEndian.PutUint32(p1[:4], uint32(len(stream1)))
	copy(p1[4:], stream1)
	img1, err := win.Decode(p1, false, 2, 2)
	if err != nil {
		t.Fatalf("img1: %v", err)
	}
	if img1.Pix[1] != 0xaa {
		t.Fatalf("img1 green channel %02x", img1.Pix[1])
	}
	if win.Len() != 1 {
		t.Fatalf("window len=%d want 1", win.Len())
	}

	// Image id=2 is a full copy of id=1 (dist=1).
	stream2 := encodeGLZRGB32CopyFromPrev(2, 2, 2, 10, 1)
	p2 := make([]byte, 4+len(stream2))
	binary.LittleEndian.PutUint32(p2[:4], uint32(len(stream2)))
	copy(p2[4:], stream2)
	img2, err := win.Decode(p2, false, 2, 2)
	if err != nil {
		t.Fatalf("img2: %v", err)
	}
	for i := 0; i < 4; i++ {
		off := i * 4
		if img2.Pix[off] != 0x00 || img2.Pix[off+1] != 0xaa || img2.Pix[off+2] != 0x00 {
			t.Fatalf("img2 pixel[%d]=%02x%02x%02x", i, img2.Pix[off], img2.Pix[off+1], img2.Pix[off+2])
		}
	}
	if win.Len() != 2 {
		t.Fatalf("window len=%d want 2", win.Len())
	}
}

func TestGLZWindowWinHeadDistEvict(t *testing.T) {
	win := codec.NewGLZWindow(16 << 20)
	// id=5 with win_head_dist=2 → release before id 3 (keep 3,4,5…)
	// First insert id=1,2,3 then id=5 with dist 2 → free id < 3.
	for _, id := range []uint64{1, 2, 3} {
		stream := encodeGLZRGB32SolidRLE(1, 1, id, 100, 0xff, 0, 0)
		p := make([]byte, 4+len(stream))
		binary.LittleEndian.PutUint32(p[:4], uint32(len(stream)))
		copy(p[4:], stream)
		if _, err := win.Decode(p, false, 1, 1); err != nil {
			t.Fatalf("id %d: %v", id, err)
		}
	}
	if win.Len() != 3 {
		t.Fatalf("len=%d want 3", win.Len())
	}
	stream := encodeGLZRGB32SolidRLE(1, 1, 5, 2, 0, 0xff, 0)
	p := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(p[:4], uint32(len(stream)))
	copy(p[4:], stream)
	if _, err := win.Decode(p, false, 1, 1); err != nil {
		t.Fatalf("id 5: %v", err)
	}
	// releaseBefore = 5-2 = 3 → free ids 1,2; keep 3 and 5 (4 never existed)
	if win.Len() != 2 {
		t.Fatalf("after win_head_dist len=%d want 2 (ids 3,5)", win.Len())
	}
}

func TestGLZWindowMaxBytesEvict(t *testing.T) {
	// Each 2x2 RGBA image is 16 bytes; budget allows one image.
	win := codec.NewGLZWindow(20)
	for id := uint64(1); id <= 3; id++ {
		stream := encodeGLZRGB32SolidRLE(2, 2, id, 1000, byte(id), 0, 0)
		p := make([]byte, 4+len(stream))
		binary.LittleEndian.PutUint32(p[:4], uint32(len(stream)))
		copy(p[4:], stream)
		if _, err := win.Decode(p, false, 2, 2); err != nil {
			t.Fatalf("id %d: %v", id, err)
		}
	}
	if win.Len() != 1 {
		t.Fatalf("maxBytes eviction: len=%d want 1", win.Len())
	}
	if win.Bytes() > 20 {
		t.Fatalf("bytes %d > max 20", win.Bytes())
	}
}

func TestGLZWindowReset(t *testing.T) {
	win := codec.NewGLZWindow(1 << 20)
	stream := encodeGLZRGB32SolidRLE(1, 1, 1, 0, 1, 2, 3)
	p := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(p[:4], uint32(len(stream)))
	copy(p[4:], stream)
	if _, err := win.Decode(p, false, 1, 1); err != nil {
		t.Fatal(err)
	}
	win.Reset()
	if win.Len() != 0 || win.Bytes() != 0 {
		t.Fatalf("after reset len=%d bytes=%d", win.Len(), win.Bytes())
	}
}

func TestGLZHeaderErrors(t *testing.T) {
	win := codec.NewGLZWindow(1 << 20)
	// Bad magic
	stream := encodeGLZRGB32SolidRLE(1, 1, 1, 0, 1, 2, 3)
	stream[0] = 0
	p := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(p[:4], uint32(len(stream)))
	copy(p[4:], stream)
	if _, err := win.Decode(p, false, 1, 1); err == nil {
		t.Fatal("expected bad magic error")
	}

	// Short payload
	if _, err := win.Decode([]byte{1, 0, 0, 0}, false, 1, 1); err == nil {
		t.Fatal("expected short data error")
	}

	// Width mismatch
	stream = encodeGLZRGB32SolidRLE(2, 2, 1, 0, 1, 2, 3)
	p = make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(p[:4], uint32(len(stream)))
	copy(p[4:], stream)
	if _, err := win.Decode(p, false, 3, 2); err == nil {
		t.Fatal("expected width mismatch")
	}
}

func TestGLZBottomUpFlip(t *testing.T) {
	// 2x1 top_down=false: two pixels different colors; after flip row order reverses.
	// For height=2 width=1: stream order pixel0 then pixel1; flip swaps them.
	pix := []byte{
		0xaa, 0x00, 0x00, 0xff, // row0 in stream
		0x00, 0xbb, 0x00, 0xff, // row1 in stream
	}
	stream := encodeGLZRGB32Literals(1, 2, false, 1, 0, pix)
	p := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(p[:4], uint32(len(stream)))
	copy(p[4:], stream)
	img, err := codec.DecodeGLZRGB(p, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	// Display top-down: first row should be stream's last row.
	if img.Pix[0] != 0x00 || img.Pix[1] != 0xbb {
		t.Fatalf("top pixel after flip = %02x%02x want 00bb", img.Pix[0], img.Pix[1])
	}
	if img.Pix[4] != 0xaa || img.Pix[5] != 0x00 {
		t.Fatalf("bottom pixel after flip = %02x%02x want aa00", img.Pix[4], img.Pix[5])
	}
}

func TestZlibGLZRGB(t *testing.T) {
	stream := encodeGLZRGB32SolidRLE(2, 2, 1, 0, 0x12, 0x34, 0x56)
	// Build payload via helper then strip SpiceImageDescriptor.
	full := packSpiceZlibGLZRGB(2, 2, stream)
	payload := full[protocol.SpiceImageDescSize:]
	img, err := codec.DecodeZlibGLZRGB(payload, 2, 2)
	if err != nil {
		t.Fatalf("DecodeZlibGLZRGB: %v", err)
	}
	if img.Pix[0] != 0x12 || img.Pix[1] != 0x34 || img.Pix[2] != 0x56 {
		t.Fatalf("pixel %02x%02x%02x", img.Pix[0], img.Pix[1], img.Pix[2])
	}
}

func TestGLZMissingDictionaryRef(t *testing.T) {
	win := codec.NewGLZWindow(1 << 20)
	// id=2 references dist=1 but id=1 never decoded.
	stream := encodeGLZRGB32CopyFromPrev(2, 2, 2, 0, 1)
	p := make([]byte, 4+len(stream))
	binary.LittleEndian.PutUint32(p[:4], uint32(len(stream)))
	copy(p[4:], stream)
	if _, err := win.Decode(p, false, 2, 2); err == nil {
		t.Fatal("expected dictionary miss error")
	}
}

// Silence unused if packSpiceGLZRGB only used conceptually.
var _ = packSpiceGLZRGB
