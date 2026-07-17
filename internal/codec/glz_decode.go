// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"encoding/binary"
	"fmt"

	"github.com/maskraven/virt-viewer/internal/protocol"
)

// GLZ stream header (after optional zlib unwrap), big-endian multi-byte fields:
//
//	magic u32, version u32, type_byte u8 (type | top_down<<4),
//	width u32, height u32, stride u32, image_id u64, win_head_dist u32
const glzHeaderSize = 4 + 4 + 1 + 4 + 4 + 4 + 8 + 4 // 33

const (
	glzImageTypeMask = 0x0f
	glzImageTypeLog  = 4 // top_down in bit 4+
)

func errGLZ(format string, args ...any) error {
	return fmt.Errorf("codec: glz: "+format, args...)
}

// glzHeader is the parsed GLZ bitstream header.
type glzHeader struct {
	lzType      uint32
	width       uint32
	height      uint32
	stride      uint32
	topDown     bool
	id          uint64
	winHeadDist uint32
	grossPixels int
}

func parseGLZHeader(data []byte) (glzHeader, []byte, error) {
	var h glzHeader
	if len(data) < glzHeaderSize {
		return h, nil, errGLZ("header short: %d", len(data))
	}
	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != lzMagic {
		return h, nil, errGLZ("bad magic 0x%08x", magic)
	}
	version := binary.BigEndian.Uint32(data[4:8])
	if version != lzVersion {
		return h, nil, errGLZ("bad version 0x%08x", version)
	}
	tmp := data[8]
	h.lzType = uint32(tmp & glzImageTypeMask)
	h.topDown = (tmp >> glzImageTypeLog) != 0
	h.width = binary.BigEndian.Uint32(data[9:13])
	h.height = binary.BigEndian.Uint32(data[13:17])
	h.stride = binary.BigEndian.Uint32(data[17:21])
	h.id = binary.BigEndian.Uint64(data[21:29])
	h.winHeadDist = binary.BigEndian.Uint32(data[29:33])
	rest := data[glzHeaderSize:]

	if h.width == 0 || h.height == 0 {
		return h, nil, errGLZ("empty dimensions %dx%d", h.width, h.height)
	}
	if int(h.width) > protocol.MaxSurfaceSide || int(h.height) > protocol.MaxSurfaceSide {
		return h, nil, errGLZ("dimensions %dx%d exceed max side %d",
			h.width, h.height, protocol.MaxSurfaceSide)
	}
	nPix := int64(h.width) * int64(h.height)
	if nPix*4 > protocol.MaxSurfaceBytes {
		return h, nil, errGLZ("image %dx%d exceeds surface bound", h.width, h.height)
	}
	h.grossPixels = int(nPix)
	return h, rest, nil
}

// decodeGLZStream decodes a raw GLZ bitstream (header + control stream) into
// stream-order RGBA. window may be nil when no cross-image matches are expected.
// The decoded pixels are NOT vertically flipped; caller applies top_down.
func decodeGLZStream(data []byte, window *GLZWindow, expectW, expectH uint32) (*RGBA, glzHeader, error) {
	var zero glzHeader
	h, stream, err := parseGLZHeader(data)
	if err != nil {
		return nil, zero, err
	}
	if expectW != 0 && h.width != expectW {
		return nil, zero, errGLZ("width %d != image desc %d", h.width, expectW)
	}
	if expectH != 0 && h.height != expectH {
		return nil, zero, errGLZ("height %d != image desc %d", h.height, expectH)
	}

	w, ht := int(h.width), int(h.height)
	out := make([]byte, h.grossPixels*4)

	switch h.lzType {
	case lzTypeRGB32, lzTypeRGB24:
		if err := glzDecompressRGB32(stream, out, h.grossPixels, h.id, window, true); err != nil {
			return nil, zero, err
		}
	case lzTypeRGBA:
		rest, err := glzDecompressRGB32Consumed(stream, out, h.grossPixels, h.id, window, false)
		if err != nil {
			return nil, zero, err
		}
		if err := glzDecompressAlpha(rest, out, h.grossPixels, h.id, window); err != nil {
			return nil, zero, err
		}
	case lzTypeRGB16:
		if err := glzDecompressRGB16ToRGBA(stream, out, h.grossPixels, h.id, window); err != nil {
			return nil, zero, err
		}
	case lzTypeXXXA:
		if err := glzDecompressAlpha(stream, out, h.grossPixels, h.id, window); err != nil {
			return nil, zero, err
		}
	default:
		return nil, zero, errGLZ("unsupported type %d", h.lzType)
	}

	img := &RGBA{Width: w, Height: ht, Stride: w * 4, Pix: out}
	if h.lzType == lzTypeRGB32 || h.lzType == lzTypeRGB24 || h.lzType == lzTypeRGB16 {
		ForceOpaque(img)
	}
	return img, h, nil
}

// glzReadMatch decodes match length, pixel offset, and image_dist after ctrl was read.
// lengthBias is 0 (RGB32/24), 1 (RGB16), or 2 (alpha/PLT).
func glzReadMatch(r *lzReader, ctrl byte, lengthBias, remaining int) (length, pixelOfs int, imageDist uint32, err error) {
	if remaining <= 0 {
		return 0, 0, 0, errGLZ("match with no remaining pixels")
	}
	lenField := int(ctrl >> 5)
	pixelFlag := (ctrl >> 4) & 0x01
	pixelOfs = int(ctrl & 0x0f)

	length = lenField
	if length == 7 {
		for {
			code, e := r.read()
			if e != nil {
				return 0, 0, 0, errGLZ("truncated length extension")
			}
			length += int(code)
			if int64(length)+int64(lengthBias) > int64(remaining) {
				return 0, 0, 0, errGLZ("match length exceeds remaining %d", remaining)
			}
			if code != 255 {
				break
			}
		}
	}

	code, err := r.read()
	if err != nil {
		return 0, 0, 0, errGLZ("truncated pixel ofs: %v", err)
	}
	pixelOfs += int(code) << 4

	code, err = r.read()
	if err != nil {
		return 0, 0, 0, errGLZ("truncated image flag: %v", err)
	}
	imageFlag := (code >> 6) & 0x03

	if pixelFlag == 0 {
		// Short pixel offset.
		imageDist = uint32(code & 0x3f)
		for i := 0; i < int(imageFlag); i++ {
			b, e := r.read()
			if e != nil {
				return 0, 0, 0, errGLZ("truncated image_dist: %v", e)
			}
			imageDist += uint32(b) << (6 + 8*i)
		}
	} else {
		// Long pixel offset; low bit of "pixel_flag" is re-read from this byte.
		longOfs := (code >> 5) & 0x01
		pixelOfs += int(code&0x1f) << 12
		imageDist = 0
		for i := 0; i < int(imageFlag); i++ {
			b, e := r.read()
			if e != nil {
				return 0, 0, 0, errGLZ("truncated image_dist: %v", e)
			}
			imageDist += uint32(b) << (8 * i)
		}
		if longOfs != 0 {
			b, e := r.read()
			if e != nil {
				return 0, 0, 0, errGLZ("truncated very-long pixel ofs: %v", e)
			}
			pixelOfs += int(b) << 17
		}
	}

	length += lengthBias
	if length <= 0 || length > remaining {
		return 0, 0, 0, errGLZ("match length %d exceeds remaining %d", length, remaining)
	}

	// Same-image offsets are biased by +1 (spice-gtk decode-glz-tmpl).
	if imageDist == 0 {
		pixelOfs++
	}
	if pixelOfs < 0 {
		return 0, 0, 0, errGLZ("negative pixel offset")
	}
	return length, pixelOfs, imageDist, nil
}

func glzCopyMatch(out []byte, op, length, pixelOfs int, imageDist uint32, imageID uint64, window *GLZWindow) (newOp int, err error) {
	nPix := len(out) / 4
	if op+length > nPix {
		return op, errGLZ("match write past end")
	}

	if imageDist == 0 {
		// Reference inside current image.
		if pixelOfs > op {
			return op, errGLZ("bad same-image backref ofs=%d op=%d", pixelOfs, op)
		}
		ref := op - pixelOfs
		if ref == op-1 {
			// RLE: repeat previous pixel.
			si := ref * 4
			for i := 0; i < length; i++ {
				di := op * 4
				out[di+0] = out[si+0]
				out[di+1] = out[si+1]
				out[di+2] = out[si+2]
				out[di+3] = out[si+3]
				op++
			}
			return op, nil
		}
		for i := 0; i < length; i++ {
			si := (ref + i) * 4
			di := op * 4
			out[di+0] = out[si+0]
			out[di+1] = out[si+1]
			out[di+2] = out[si+2]
			out[di+3] = out[si+3]
			op++
		}
		return op, nil
	}

	// Cross-image reference.
	src, err := window.bits(imageID, imageDist, pixelOfs)
	if err != nil {
		return op, err
	}
	// Need length pixels from src.
	if len(src) < length*4 {
		return op, errGLZ("cross-image ref short: have %d bytes need %d", len(src), length*4)
	}
	for i := 0; i < length; i++ {
		si := i * 4
		di := op * 4
		out[di+0] = src[si+0]
		out[di+1] = src[si+1]
		out[di+2] = src[si+2]
		out[di+3] = src[si+3]
		op++
	}
	return op, nil
}

func glzDecompressRGB32(src, out []byte, nPix int, imageID uint64, window *GLZWindow, defaultAlpha bool) error {
	_, err := glzDecompressRGB32Consumed(src, out, nPix, imageID, window, defaultAlpha)
	return err
}

func glzDecompressRGB32Consumed(src, out []byte, nPix int, imageID uint64, window *GLZWindow, defaultAlpha bool) ([]byte, error) {
	if len(out) < nPix*4 {
		return nil, errGLZ("out buffer short")
	}
	r := &lzReader{b: src}
	op := 0
	for op < nPix {
		ctrl, err := r.read()
		if err != nil {
			return nil, errGLZ("stream truncated at pixel %d: %v", op, err)
		}
		if ctrl >= lzMaxCopy {
			// RGB32/24 length bias = 0 (spice-gtk: no PLT/RGB16 add).
			length, pixelOfs, imageDist, err := glzReadMatch(r, ctrl, 0, nPix-op)
			if err != nil {
				return nil, err
			}
			// Apply default alpha after copy for RGB32 path by fixing A if needed.
			op, err = glzCopyMatch(out, op, length, pixelOfs, imageDist, imageID, window)
			if err != nil {
				return nil, err
			}
			if defaultAlpha {
				// Ensure A=0xff for newly written run (cross-image may already be opaque).
				for i := op - length; i < op; i++ {
					out[i*4+3] = 0xff
				}
			}
		} else {
			count := int(ctrl) + 1
			if op+count > nPix {
				return nil, errGLZ("literal overflows image")
			}
			for i := 0; i < count; i++ {
				b, err := r.read()
				if err != nil {
					return nil, errGLZ("literal B: %v", err)
				}
				g, err := r.read()
				if err != nil {
					return nil, errGLZ("literal G: %v", err)
				}
				rr, err := r.read()
				if err != nil {
					return nil, errGLZ("literal R: %v", err)
				}
				di := op * 4
				out[di+0] = rr
				out[di+1] = g
				out[di+2] = b
				if defaultAlpha {
					out[di+3] = 0xff
				} else {
					out[di+3] = 0
				}
				op++
			}
		}
	}
	return r.b[r.i:], nil
}

// glzDecompressAlpha decodes the GLZ alpha plane (length bias +2).
func glzDecompressAlpha(src, out []byte, nPix int, imageID uint64, window *GLZWindow) error {
	if len(out) < nPix*4 {
		return errGLZ("alpha out buffer short")
	}
	r := &lzReader{b: src}
	op := 0
	for op < nPix {
		ctrl, err := r.read()
		if err != nil {
			return errGLZ("alpha stream truncated: %v", err)
		}
		if ctrl >= lzMaxCopy {
			length, pixelOfs, imageDist, err := glzReadMatch(r, ctrl, 2 /*alpha bias*/, nPix-op)
			if err != nil {
				return err
			}
			if imageDist == 0 {
				if pixelOfs > op {
					return errGLZ("alpha bad backref ofs=%d op=%d", pixelOfs, op)
				}
				ref := op - pixelOfs
				if ref == op-1 {
					a := out[ref*4+3]
					for i := 0; i < length; i++ {
						out[op*4+3] = a
						op++
					}
				} else {
					for i := 0; i < length; i++ {
						out[op*4+3] = out[(ref+i)*4+3]
						op++
					}
				}
			} else {
				srcPix, err := window.bits(imageID, imageDist, pixelOfs)
				if err != nil {
					return err
				}
				if len(srcPix) < length*4 {
					return errGLZ("alpha cross-image ref short")
				}
				for i := 0; i < length; i++ {
					out[op*4+3] = srcPix[i*4+3]
					op++
				}
			}
		} else {
			count := int(ctrl) + 1
			if op+count > nPix {
				return errGLZ("alpha literal overflows image")
			}
			for i := 0; i < count; i++ {
				a, err := r.read()
				if err != nil {
					return errGLZ("alpha literal: %v", err)
				}
				out[op*4+3] = a
				op++
			}
		}
	}
	return nil
}

// glzDecompressRGB16ToRGBA expands GLZ RGB16 (spice-gtk TO_RGB32 path) to RGBA.
// Length bias is +1.
func glzDecompressRGB16ToRGBA(src, out []byte, nPix int, imageID uint64, window *GLZWindow) error {
	if len(out) < nPix*4 {
		return errGLZ("rgb16 out buffer short")
	}
	r := &lzReader{b: src}
	op := 0
	for op < nPix {
		ctrl, err := r.read()
		if err != nil {
			return errGLZ("rgb16 stream truncated: %v", err)
		}
		if ctrl >= lzMaxCopy {
			length, pixelOfs, imageDist, err := glzReadMatch(r, ctrl, 1 /*RGB16 bias*/, nPix-op)
			if err != nil {
				return err
			}
			op, err = glzCopyMatch(out, op, length, pixelOfs, imageDist, imageID, window)
			if err != nil {
				return err
			}
			// Ensure opaque.
			for i := op - length; i < op; i++ {
				out[i*4+3] = 0xff
			}
		} else {
			count := int(ctrl) + 1
			if op+count > nPix {
				return errGLZ("rgb16 literal overflows image")
			}
			for i := 0; i < count; i++ {
				// spice-gtk decode-glz-tmpl.c LZ_RGB16 TO_RGB32 COPY_COMP_PIXEL
				t0, err := r.read()
				if err != nil {
					return errGLZ("rgb16 byte0: %v", err)
				}
				t1, err := r.read()
				if err != nil {
					return errGLZ("rgb16 byte1: %v", err)
				}
				// Intermediate names match C: r,b then rewrite to true channels.
				rr := t0
				bb := t1
				gg := ((rr << 6) | (bb >> 2)) & ^byte(0x07)
				gg |= gg >> 5
				rr = ((rr << 1) & ^byte(0x07)) | ((rr >> 4) & 0x07)
				bb = (bb << 3) | ((bb >> 2) & 0x07)
				di := op * 4
				out[di+0] = rr
				out[di+1] = gg
				out[di+2] = bb
				out[di+3] = 0xff
				op++
			}
		}
	}
	return nil
}
