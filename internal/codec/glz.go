// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/maskraven/virt-viewer/internal/protocol"
)

// DecodeGLZRGB decodes a SPICE_IMAGE_TYPE_GLZ_RGB payload (after SpiceImageDescriptor)
// with a fresh dictionary (same-image matches only). Prefer GLZWindow.Decode for
// multi-image dictionary state on the display channel.
//
// Wire layout:
//
//	uint32 data_size  (little-endian)
//	uint8  data[data_size]  // GLZ bitstream
func DecodeGLZRGB(payload []byte, expectW, expectH uint32) (*RGBA, error) {
	return NewGLZWindow(int(protocol.DisplayGlzWindowBytes)).Decode(payload, false, expectW, expectH)
}

// DecodeZlibGLZRGB decodes SPICE_IMAGE_TYPE_ZLIB_GLZ_RGB with a fresh dictionary.
//
// Wire layout:
//
//	uint32 glz_data_size  (little-endian; inflated size)
//	uint32 data_size      (little-endian; zlib blob size)
//	uint8  data[data_size]
func DecodeZlibGLZRGB(payload []byte, expectW, expectH uint32) (*RGBA, error) {
	return NewGLZWindow(int(protocol.DisplayGlzWindowBytes)).Decode(payload, true, expectW, expectH)
}

// Decode decodes a GLZ or ZLIB_GLZ payload, inserts the result into the window,
// and returns a display-oriented RGBA (top-down, flipped if needed).
//
// The window retains stream-order pixels for future cross-image matches.
// zlibWrapped selects ZLIB_GLZ_RGB (true) vs GLZ_RGB (false) wire framing.
func (w *GLZWindow) Decode(payload []byte, zlibWrapped bool, expectW, expectH uint32) (*RGBA, error) {
	if w == nil {
		return nil, errGLZ("nil window")
	}
	stream, err := extractGLZStream(payload, zlibWrapped)
	if err != nil {
		return nil, err
	}
	img, hdr, err := decodeGLZStream(stream, w, expectW, expectH)
	if err != nil {
		return nil, err
	}

	// Dictionary stores a private copy in stream order (before vertical flip).
	entryPix := make([]byte, len(img.Pix))
	copy(entryPix, img.Pix)
	w.mu.Lock()
	w.addLocked(&glzEntry{
		id:          hdr.id,
		winHeadDist: hdr.winHeadDist,
		width:       img.Width,
		height:      img.Height,
		pix:         entryPix,
	})
	w.mu.Unlock()

	// Return display-oriented image.
	out := &RGBA{
		Width:  img.Width,
		Height: img.Height,
		Stride: img.Stride,
		Pix:    make([]byte, len(img.Pix)),
	}
	copy(out.Pix, img.Pix)
	if !hdr.topDown {
		flipRGBAVertical(out)
	}
	return out, nil
}

// extractGLZStream unwraps the SpiceImage payload framing into a raw GLZ bitstream.
func extractGLZStream(payload []byte, zlibWrapped bool) ([]byte, error) {
	if zlibWrapped {
		return extractZlibGLZ(payload)
	}
	return extractPlainGLZ(payload)
}

func extractPlainGLZ(payload []byte) ([]byte, error) {
	if len(payload) < 4 {
		return nil, errGLZ("payload size short: %d", len(payload))
	}
	dataSize := binary.LittleEndian.Uint32(payload[:4])
	if dataSize == 0 {
		return nil, errGLZ("empty data")
	}
	if int64(dataSize) > protocol.MaxSurfaceBytes {
		return nil, errGLZ("data_size %d exceeds bound", dataSize)
	}
	if len(payload) < 4+int(dataSize) {
		return nil, errGLZ("data short: have %d need %d", len(payload)-4, dataSize)
	}
	return payload[4 : 4+dataSize], nil
}

func extractZlibGLZ(payload []byte) ([]byte, error) {
	if len(payload) < 8 {
		return nil, errGLZ("zlib_glz header short: %d", len(payload))
	}
	glzDataSize := binary.LittleEndian.Uint32(payload[0:4])
	dataSize := binary.LittleEndian.Uint32(payload[4:8])
	if glzDataSize == 0 || dataSize == 0 {
		return nil, errGLZ("zlib_glz empty sizes glz=%d zlib=%d", glzDataSize, dataSize)
	}
	if int64(glzDataSize) > protocol.MaxSurfaceBytes {
		return nil, errGLZ("zlib_glz glz_data_size %d exceeds bound", glzDataSize)
	}
	if int64(dataSize) > protocol.MaxSurfaceBytes {
		return nil, errGLZ("zlib_glz data_size %d exceeds bound", dataSize)
	}
	if len(payload) < 8+int(dataSize) {
		return nil, errGLZ("zlib_glz data short: have %d need %d", len(payload)-8, dataSize)
	}
	zblob := payload[8 : 8+dataSize]

	zr, err := zlib.NewReader(bytes.NewReader(zblob))
	if err != nil {
		return nil, fmt.Errorf("codec: glz: zlib open: %w", err)
	}
	defer zr.Close()

	// Limit inflate to declared size (+ small slack rejected below).
	limited := io.LimitReader(zr, int64(glzDataSize)+1)
	out, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("codec: glz: zlib inflate: %w", err)
	}
	if uint32(len(out)) != glzDataSize {
		return nil, errGLZ("zlib inflate size %d != glz_data_size %d", len(out), glzDataSize)
	}
	return out, nil
}
