// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/maskraven/virt-viewer/internal/protocol"
)

// ErrUnsupportedImage is returned when DecodeSpiceImage sees a type that is
// not yet implemented (Quic, JPEG, GLZ, …). Display channel soft-skips these.
var ErrUnsupportedImage = errors.New("codec: unsupported image type")

// UnsupportedImageError carries the SpiceImage type byte for skip counters.
type UnsupportedImageError struct {
	Type uint8
}

func (e *UnsupportedImageError) Error() string {
	return fmt.Sprintf("%v %d", ErrUnsupportedImage, e.Type)
}

func (e *UnsupportedImageError) Unwrap() error { return ErrUnsupportedImage }

// RGBA is a decoded image in RGBA8888 (A=0xFF for xRGB sources).
type RGBA struct {
	Width  int
	Height int
	Stride int
	Pix    []byte // len == Height * Stride; stride is Width*4 after decode
}

// DecodeSpiceImage decodes a SpiceImage starting at data (descriptor + payload).
// Supports SPICE_IMAGE_TYPE_BITMAP (raw) and SPICE_IMAGE_TYPE_LZ_RGB.
// Other types return *UnsupportedImageError wrapping ErrUnsupportedImage.
func DecodeSpiceImage(data []byte) (*RGBA, error) {
	if len(data) < protocol.SpiceImageDescSize {
		return nil, fmt.Errorf("codec: spice image short: %d", len(data))
	}
	typ := data[8]
	// flags := data[9]
	width := binary.LittleEndian.Uint32(data[10:14])
	height := binary.LittleEndian.Uint32(data[14:18])
	payload := data[protocol.SpiceImageDescSize:]

	switch typ {
	case protocol.ImageTypeBitmap:
		return DecodeBitmap(payload, width, height)
	case protocol.ImageTypeLZRGB:
		return DecodeLZRGB(payload, width, height)
	default:
		return nil, &UnsupportedImageError{Type: typ}
	}
}

// DecodeBitmap decodes a SPICE_IMAGE_TYPE_BITMAP payload into RGBA8888.
//
// Wire layout (after SpiceImageDescriptor):
//
//	uint8  format
//	uint8  flags   (PAL_CACHE_ME, PAL_FROM_CACHE, TOP_DOWN)
//	uint32 width
//	uint32 height
//	uint32 stride  (bytes per row on the wire)
//	if PAL_FROM_CACHE: uint64 palette_id
//	else:              uint32 palette_ptr (offset; ignored for 32-bit formats)
//	uint8  data[stride * height]
//
// QEMU sends 32-bit pixels as BGRX; we convert to RGBA and force A=0xFF for 32BIT.
// If TOP_DOWN is clear, rows are flipped to top-down order.
func DecodeBitmap(payload []byte, expectW, expectH uint32) (*RGBA, error) {
	if len(payload) < 18 {
		return nil, fmt.Errorf("codec: bitmap header short: %d", len(payload))
	}
	format := payload[0]
	flags := payload[1]
	width := binary.LittleEndian.Uint32(payload[2:6])
	height := binary.LittleEndian.Uint32(payload[6:10])
	stride := binary.LittleEndian.Uint32(payload[10:14])

	if expectW != 0 && width != expectW {
		return nil, fmt.Errorf("codec: bitmap width %d != image desc %d", width, expectW)
	}
	if expectH != 0 && height != expectH {
		return nil, fmt.Errorf("codec: bitmap height %d != image desc %d", height, expectH)
	}
	if width == 0 || height == 0 {
		return nil, fmt.Errorf("codec: bitmap empty dimensions %dx%d", width, height)
	}
	if int(width) > protocol.MaxSurfaceSide || int(height) > protocol.MaxSurfaceSide {
		return nil, fmt.Errorf("codec: bitmap dimensions %dx%d exceed max side %d",
			width, height, protocol.MaxSurfaceSide)
	}
	if stride < width*bytesPerPixel(format) && format != protocol.BitmapFmtInvalid {
		// For 32-bit formats stride must be at least width*4.
		if format == protocol.BitmapFmt32Bit || format == protocol.BitmapFmtRGBA {
			return nil, fmt.Errorf("codec: bitmap stride %d too small for width %d", stride, width)
		}
	}

	headerLen := 18
	if flags&protocol.BitmapFlagPalFromCache != 0 {
		headerLen = 22
	}
	if len(payload) < headerLen {
		return nil, fmt.Errorf("codec: bitmap header short: %d want %d", len(payload), headerLen)
	}
	pixData := payload[headerLen:]

	need := int64(height) * int64(stride)
	if need < 0 || need > protocol.MaxSurfaceBytes {
		return nil, fmt.Errorf("codec: bitmap payload size %d exceeds bound", need)
	}
	if int64(len(pixData)) < need {
		return nil, fmt.Errorf("codec: bitmap data short: have %d need %d", len(pixData), need)
	}
	pixData = pixData[:need]

	switch format {
	case protocol.BitmapFmt32Bit:
		img, err := decodeBGRX(pixData, int(width), int(height), int(stride), flags)
		if err != nil {
			return nil, err
		}
		ForceOpaque(img) // xRGB: X channel is not alpha
		return img, nil
	case protocol.BitmapFmtRGBA:
		return decodeBGRX(pixData, int(width), int(height), int(stride), flags)
	default:
		return nil, fmt.Errorf("codec: unsupported bitmap format %d", format)
	}
}

func bytesPerPixel(format uint8) uint32 {
	switch format {
	case protocol.BitmapFmt32Bit, protocol.BitmapFmtRGBA:
		return 4
	case protocol.BitmapFmt24Bit:
		return 3
	case protocol.BitmapFmt16Bit:
		return 2
	default:
		return 1
	}
}

// decodeBGRX converts wire BGRX/BGRA rows to tightly-packed RGBA8888.
func decodeBGRX(data []byte, width, height, stride int, flags uint8) (*RGBA, error) {
	outStride := width * 4
	out := make([]byte, height*outStride)

	topDown := flags&protocol.BitmapFlagTopDown != 0
	for y := 0; y < height; y++ {
		srcY := y
		if !topDown {
			srcY = height - 1 - y
		}
		srcOff := srcY * stride
		dstOff := y * outStride
		for x := 0; x < width; x++ {
			si := srcOff + x*4
			di := dstOff + x*4
			// Wire: B, G, R, X/A → RGBA
			out[di+0] = data[si+2] // R
			out[di+1] = data[si+1] // G
			out[di+2] = data[si+0] // B
			out[di+3] = data[si+3] // A or X
		}
	}
	return &RGBA{Width: width, Height: height, Stride: outStride, Pix: out}, nil
}

// DecodeBitmap32 is like DecodeBitmap but forces A=0xFF (xRGB / 32BIT semantics).
// Prefer DecodeBitmap; this helper exists for tests and DRAW_COPY raw paths that
// want opaque pixels regardless of the X channel.
func ForceOpaque(img *RGBA) {
	if img == nil {
		return
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xff
	}
}
