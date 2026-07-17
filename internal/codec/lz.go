// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"encoding/binary"
	"fmt"

	"github.com/maskraven/spice-viewer/internal/protocol"
)

// SPICE LZ (FastLZ-derived) constants from spice-common/common/lz_common.h
// and lz.c. Wire header fields after the magic are big-endian.
const (
	// lzMagic is 0x20205a4c; big-endian wire bytes are 20 20 5a 4c ("  ZL").
	lzMagic        uint32 = 0x20205a4c
	lzVersionMajor        = 1
	lzVersionMinor        = 1
	lzVersion      uint32 = (lzVersionMajor << 16) | lzVersionMinor // 0x00010001

	lzMaxCopy     = 32
	lzMaxDistance = 8191 // 2^13

	// LzImageType values (spice-common LzImageType enum).
	lzTypeInvalid = 0
	lzTypePLT1LE  = 1
	lzTypePLT1BE  = 2
	lzTypePLT4LE  = 3
	lzTypePLT4BE  = 4
	lzTypePLT8    = 5
	lzTypeRGB16   = 6
	lzTypeRGB24   = 7
	lzTypeRGB32   = 8
	lzTypeRGBA    = 9
	lzTypeXXXA    = 10
	lzTypeA8      = 11

	lzHeaderSize = 28 // magic + version + type + w + h + stride + top_down (7×u32 BE)
)

// DecodeLZRGB decodes a SPICE_IMAGE_TYPE_LZ_RGB payload (after SpiceImageDescriptor)
// into RGBA8888.
//
// Wire layout:
//
//	uint32 data_size  (little-endian; size of the LZ bitstream that follows)
//	uint8  data[data_size]
//
// The LZ bitstream begins with a 28-byte big-endian header:
//
//	magic("  ZL"/0x20205a4c), version(1.1), type, width, height, stride, top_down
//
// followed by FastLZ-style control/literal/match bytes (spice-common lz_decompress_tmpl).
//
// Supported types: RGB16, RGB24, RGB32, RGBA. Palette LZ is SPICE_IMAGE_TYPE_LZ_PLT.
func DecodeLZRGB(payload []byte, expectW, expectH uint32) (*RGBA, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("codec: lz_rgb size short: %d", len(payload))
	}
	dataSize := binary.LittleEndian.Uint32(payload[:4])
	if dataSize == 0 {
		return nil, fmt.Errorf("codec: lz_rgb empty data")
	}
	if int64(dataSize) > protocol.MaxSurfaceBytes {
		return nil, fmt.Errorf("codec: lz_rgb data_size %d exceeds bound", dataSize)
	}
	if len(payload) < 4+int(dataSize) {
		return nil, fmt.Errorf("codec: lz_rgb data short: have %d need %d", len(payload)-4, dataSize)
	}
	return decodeLZStream(payload[4:4+dataSize], expectW, expectH)
}

// decodeLZStream decodes a raw LZ bitstream (magic header + compressed pixels).
func decodeLZStream(data []byte, expectW, expectH uint32) (*RGBA, error) {
	if len(data) < lzHeaderSize {
		return nil, fmt.Errorf("codec: lz header short: %d", len(data))
	}
	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != lzMagic {
		return nil, fmt.Errorf("codec: lz bad magic 0x%08x", magic)
	}
	version := binary.BigEndian.Uint32(data[4:8])
	if version != lzVersion {
		return nil, fmt.Errorf("codec: lz bad version 0x%08x", version)
	}
	lzType := binary.BigEndian.Uint32(data[8:12])
	width := binary.BigEndian.Uint32(data[12:16])
	height := binary.BigEndian.Uint32(data[16:20])
	// stride := binary.BigEndian.Uint32(data[20:24]) — unused for RGB decode
	topDown := binary.BigEndian.Uint32(data[24:28])
	stream := data[lzHeaderSize:]

	if expectW != 0 && width != expectW {
		return nil, fmt.Errorf("codec: lz width %d != image desc %d", width, expectW)
	}
	if expectH != 0 && height != expectH {
		return nil, fmt.Errorf("codec: lz height %d != image desc %d", height, expectH)
	}
	if width == 0 || height == 0 {
		return nil, fmt.Errorf("codec: lz empty dimensions %dx%d", width, height)
	}
	if int(width) > protocol.MaxSurfaceSide || int(height) > protocol.MaxSurfaceSide {
		return nil, fmt.Errorf("codec: lz dimensions %dx%d exceed max side %d",
			width, height, protocol.MaxSurfaceSide)
	}
	nPix := int64(width) * int64(height)
	if nPix*4 > protocol.MaxSurfaceBytes {
		return nil, fmt.Errorf("codec: lz image %dx%d exceeds surface bound", width, height)
	}

	w, h := int(width), int(height)
	outStride := w * 4
	out := make([]byte, int(nPix)*4)

	switch lzType {
	case lzTypeRGB32, lzTypeRGB24:
		if err := lzDecompressRGB32(stream, out, int(nPix), true); err != nil {
			return nil, err
		}
	case lzTypeRGBA:
		// RGB plane then separate alpha (XXXA) plane, both over nPix samples.
		rest, err := lzDecompressRGB32Consumed(stream, out, int(nPix), false)
		if err != nil {
			return nil, err
		}
		if err := lzDecompressAlpha(rest, out, int(nPix)); err != nil {
			return nil, err
		}
	case lzTypeRGB16:
		if err := lzDecompressRGB16ToRGBA(stream, out, int(nPix)); err != nil {
			return nil, err
		}
	case lzTypeXXXA:
		// Alpha-only layer (unusual as standalone LZ_RGB); leave RGB zero.
		if err := lzDecompressAlpha(stream, out, int(nPix)); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("codec: lz unsupported type %d", lzType)
	}

	img := &RGBA{Width: w, Height: h, Stride: outStride, Pix: out}
	if topDown == 0 {
		flipRGBAVertical(img)
	}
	// RGB32/RGB24/RGB16: force opaque (xRGB). RGBA keeps decoded alpha.
	if lzType == lzTypeRGB32 || lzType == lzTypeRGB24 || lzType == lzTypeRGB16 {
		ForceOpaque(img)
	}
	return img, nil
}

// lzReader is a bounds-checked byte cursor over the compressed stream.
type lzReader struct {
	b []byte
	i int
}

func (r *lzReader) read() (byte, error) {
	if r.i >= len(r.b) {
		return 0, fmt.Errorf("codec: lz stream truncated at %d", r.i)
	}
	c := r.b[r.i]
	r.i++
	return c, nil
}

// lzReadMatchLen resolves match length after ctrl was read.
// lengthField is (ctrl >> 5) still including the initial bias encoding
// (before the spice-common length--). bias is the type-specific post-extension
// add (+1 RGB32/24, +2 RGB16, +3 alpha). remaining is nPix-op.
//
// Extension bytes are accumulated in int64 and rejected if the final length
// (after bias) would exceed remaining — preventing uint32 wrap of 0xFF runs.
func lzReadMatchLen(r *lzReader, lengthField uint32, bias, remaining int) (int, error) {
	if remaining <= 0 {
		return 0, fmt.Errorf("codec: lz match with no remaining pixels")
	}
	// spice-common: len = (ctrl>>5) - 1, then optional 0xFF extension, then +bias.
	length := int64(lengthField) - 1
	if length < 0 {
		return 0, fmt.Errorf("codec: lz invalid length field %d", lengthField)
	}
	if length == 7-1 {
		for {
			code, err := r.read()
			if err != nil {
				return 0, err
			}
			length += int64(code)
			// Cap early so a long 0xFF run cannot wrap a uint32 later.
			if length+int64(bias) > int64(remaining) {
				return 0, fmt.Errorf("codec: lz match length exceeds remaining %d", remaining)
			}
			if code != 255 {
				break
			}
		}
	}
	final := length + int64(bias)
	if final <= 0 || final > int64(remaining) {
		return 0, fmt.Errorf("codec: lz match length %d exceeds remaining %d", final, remaining)
	}
	return int(final), nil
}

// lzReadMatchOfs resolves the back-reference distance (after +1 bias) from
// the low 5 bits of ctrl and following stream bytes (including far distance).
func lzReadMatchOfs(r *lzReader, ctrlLow5 byte) (int, error) {
	ofs := uint32(ctrlLow5) << 8
	code, err := r.read()
	if err != nil {
		return 0, err
	}
	ofs += uint32(code)
	// Far distance (spice-common): code==255 and high 5 bits of distance were all 1.
	if code == 255 && (ofs-uint32(code)) == (31<<8) {
		hi, err := r.read()
		if err != nil {
			return 0, err
		}
		lo, err := r.read()
		if err != nil {
			return 0, err
		}
		ofs = uint32(hi)<<8 + uint32(lo) + lzMaxDistance
	}
	// Offset bias +1. Far-path max is ~65535+8191+1, always fits int.
	ofs++
	if ofs == 0 {
		return 0, fmt.Errorf("codec: lz zero backref after bias")
	}
	return int(ofs), nil
}

// lzDecompressRGB32 decodes spice-common lz_rgb32_decompress into tightly packed
// RGBA (R,G,B from wire B,G,R; A set when defaultAlpha, else left 0).
func lzDecompressRGB32(src []byte, out []byte, nPix int, defaultAlpha bool) error {
	_, err := lzDecompressRGB32Consumed(src, out, nPix, defaultAlpha)
	return err
}

func lzDecompressRGB32Consumed(src []byte, out []byte, nPix int, defaultAlpha bool) ([]byte, error) {
	if len(out) < nPix*4 {
		return nil, fmt.Errorf("codec: lz out buffer short")
	}
	r := &lzReader{b: src}
	op := 0
	for op < nPix {
		ctrl, err := r.read()
		if err != nil {
			return nil, err
		}
		if ctrl >= lzMaxCopy {
			length, err := lzReadMatchLen(r, uint32(ctrl>>5), 1 /*RGB32 bias*/, nPix-op)
			if err != nil {
				return nil, err
			}
			ofs, err := lzReadMatchOfs(r, ctrl&31)
			if err != nil {
				return nil, err
			}
			if ofs > op {
				return nil, fmt.Errorf("codec: lz bad backref ofs=%d op=%d", ofs, op)
			}
			ref := op - ofs
			if ref == op-1 {
				// RLE: repeat previous pixel.
				si := ref * 4
				for i := 0; i < length; i++ {
					di := op * 4
					out[di+0] = out[si+0]
					out[di+1] = out[si+1]
					out[di+2] = out[si+2]
					if defaultAlpha {
						out[di+3] = 0xff
					} else {
						out[di+3] = out[si+3]
					}
					op++
				}
			} else {
				for i := 0; i < length; i++ {
					si := (ref + i) * 4
					di := op * 4
					out[di+0] = out[si+0]
					out[di+1] = out[si+1]
					out[di+2] = out[si+2]
					if defaultAlpha {
						out[di+3] = 0xff
					} else {
						out[di+3] = out[si+3]
					}
					op++
				}
			}
		} else {
			// Literals: count is biased by 1.
			count := int(ctrl) + 1
			if op+count > nPix {
				return nil, fmt.Errorf("codec: lz literal overflows image")
			}
			for i := 0; i < count; i++ {
				b, err := r.read()
				if err != nil {
					return nil, err
				}
				g, err := r.read()
				if err != nil {
					return nil, err
				}
				rr, err := r.read()
				if err != nil {
					return nil, err
				}
				di := op * 4
				// Wire B,G,R → RGBA
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

// lzDecompressAlpha decodes lz_rgb_alpha_decompress (one byte per pixel into A).
// Length bias is +3 (min match 3) per spice-common for LZ_RGB_ALPHA.
func lzDecompressAlpha(src []byte, out []byte, nPix int) error {
	if len(out) < nPix*4 {
		return fmt.Errorf("codec: lz alpha out buffer short")
	}
	r := &lzReader{b: src}
	op := 0
	for op < nPix {
		ctrl, err := r.read()
		if err != nil {
			return err
		}
		if ctrl >= lzMaxCopy {
			length, err := lzReadMatchLen(r, uint32(ctrl>>5), 3 /*alpha bias*/, nPix-op)
			if err != nil {
				return err
			}
			ofs, err := lzReadMatchOfs(r, ctrl&31)
			if err != nil {
				return err
			}
			if ofs > op {
				return fmt.Errorf("codec: lz alpha bad backref ofs=%d op=%d", ofs, op)
			}
			ref := op - ofs
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
			count := int(ctrl) + 1
			if op+count > nPix {
				return fmt.Errorf("codec: lz alpha literal overflows image")
			}
			for i := 0; i < count; i++ {
				a, err := r.read()
				if err != nil {
					return err
				}
				out[op*4+3] = a
				op++
			}
		}
	}
	return nil
}

// lzDecompressRGB16ToRGBA decodes RGB16 (two big-endian bytes per literal pixel
// as produced by spice ENCODE_PIXEL: high then low) expanded to RGBA8888.
// Length bias for RGB16 matches is +2.
func lzDecompressRGB16ToRGBA(src []byte, out []byte, nPix int) error {
	if len(out) < nPix*4 {
		return fmt.Errorf("codec: lz rgb16 out buffer short")
	}
	// Intermediate 16-bit pixels (native host order for backrefs).
	pix16 := make([]uint16, nPix)
	r := &lzReader{b: src}
	op := 0
	for op < nPix {
		ctrl, err := r.read()
		if err != nil {
			return err
		}
		if ctrl >= lzMaxCopy {
			length, err := lzReadMatchLen(r, uint32(ctrl>>5), 2 /*RGB16 bias*/, nPix-op)
			if err != nil {
				return err
			}
			ofs, err := lzReadMatchOfs(r, ctrl&31)
			if err != nil {
				return err
			}
			if ofs > op {
				return fmt.Errorf("codec: lz rgb16 bad backref ofs=%d op=%d", ofs, op)
			}
			ref := op - ofs
			if ref == op-1 {
				v := pix16[ref]
				for i := 0; i < length; i++ {
					pix16[op] = v
					op++
				}
			} else {
				for i := 0; i < length; i++ {
					pix16[op] = pix16[ref+i]
					op++
				}
			}
		} else {
			count := int(ctrl) + 1
			if op+count > nPix {
				return fmt.Errorf("codec: lz rgb16 literal overflows image")
			}
			for i := 0; i < count; i++ {
				hi, err := r.read()
				if err != nil {
					return err
				}
				lo, err := r.read()
				if err != nil {
					return err
				}
				pix16[op] = uint16(hi)<<8 | uint16(lo)
				op++
			}
		}
	}
	// Expand RGB555 (spice GET_r/g/b masks) to 8-bit channels.
	for i := 0; i < nPix; i++ {
		p := pix16[i]
		r5 := (p >> 10) & 0x1f
		g5 := (p >> 5) & 0x1f
		b5 := p & 0x1f
		di := i * 4
		out[di+0] = uint8((r5 << 3) | (r5 >> 2))
		out[di+1] = uint8((g5 << 3) | (g5 >> 2))
		out[di+2] = uint8((b5 << 3) | (b5 >> 2))
		out[di+3] = 0xff
	}
	return nil
}

func flipRGBAVertical(img *RGBA) {
	if img == nil || img.Height <= 1 {
		return
	}
	row := img.Stride
	tmp := make([]byte, row)
	for y := 0; y < img.Height/2; y++ {
		a := y * row
		b := (img.Height - 1 - y) * row
		copy(tmp, img.Pix[a:a+row])
		copy(img.Pix[a:a+row], img.Pix[b:b+row])
		copy(img.Pix[b:b+row], tmp)
	}
}
