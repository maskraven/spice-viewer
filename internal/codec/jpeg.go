// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"

	"github.com/maskraven/spice-viewer/internal/protocol"
)

// JPEG alpha flags (SpiceJPEGAlphaFlags).
const (
	jpegAlphaFlagTopDown uint8 = 1 << 0
)

// DecodeJPEG decodes a SPICE_IMAGE_TYPE_JPEG payload (after SpiceImageDescriptor).
//
// Wire layout:
//
//	uint32 data_size
//	uint8  jpeg_bytes[data_size]
func DecodeJPEG(payload []byte, expectW, expectH uint32) (*RGBA, error) {
	data, err := binaryDataChunk(payload, "jpeg")
	if err != nil {
		return nil, err
	}
	img, err := DecodeJPEGBytes(data)
	if err != nil {
		return nil, err
	}
	if err := checkImageDims(img, expectW, expectH, "jpeg"); err != nil {
		return nil, err
	}
	return img, nil
}

// DecodeJPEGBytes decodes a raw JPEG bitstream (as used by MJPEG stream frames).
func DecodeJPEGBytes(data []byte) (*RGBA, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("codec: jpeg empty data")
	}
	if int64(len(data)) > protocol.MaxSurfaceBytes {
		return nil, fmt.Errorf("codec: jpeg data size %d exceeds bound", len(data))
	}
	src, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("codec: jpeg decode: %w", err)
	}
	return imageToRGBA(src), nil
}

// DecodeJPEGAlpha decodes SPICE_IMAGE_TYPE_JPEG_ALPHA.
//
// Wire layout:
//
//	uint8  flags (TOP_DOWN = 1<<0)
//	uint32 jpeg_size
//	uint32 data_size   (jpeg_size + lz_alpha_size)
//	uint8  data[data_size]  // JPEG then LZ XXXA alpha plane
func DecodeJPEGAlpha(payload []byte, expectW, expectH uint32) (*RGBA, error) {
	if len(payload) < 9 {
		return nil, fmt.Errorf("codec: jpeg_alpha header short: %d", len(payload))
	}
	flags := payload[0]
	jpegSize := binary.LittleEndian.Uint32(payload[1:5])
	dataSize := binary.LittleEndian.Uint32(payload[5:9])
	if dataSize < jpegSize {
		return nil, fmt.Errorf("codec: jpeg_alpha jpeg_size %d > data_size %d", jpegSize, dataSize)
	}
	if int64(dataSize) > protocol.MaxSurfaceBytes {
		return nil, fmt.Errorf("codec: jpeg_alpha data_size %d exceeds bound", dataSize)
	}
	if len(payload) < 9+int(dataSize) {
		return nil, fmt.Errorf("codec: jpeg_alpha data short: have %d need %d", len(payload)-9, dataSize)
	}
	blob := payload[9 : 9+dataSize]
	jpegData := blob[:jpegSize]
	alphaData := blob[jpegSize:]

	img, err := DecodeJPEGBytes(jpegData)
	if err != nil {
		return nil, err
	}
	if err := checkImageDims(img, expectW, expectH, "jpeg_alpha"); err != nil {
		return nil, err
	}

	// Apply LZ-encoded alpha plane when present (type XXXA).
	if len(alphaData) > 0 {
		alphaImg, err := decodeLZStream(alphaData, uint32(img.Width), uint32(img.Height))
		if err != nil {
			return nil, fmt.Errorf("codec: jpeg_alpha lz: %w", err)
		}
		// Overlay alpha channel; RGB from JPEG.
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i+3] = alphaImg.Pix[i+3]
		}
	}

	if flags&jpegAlphaFlagTopDown == 0 {
		flipRGBAVertical(img)
	}
	return img, nil
}

func binaryDataChunk(payload []byte, name string) ([]byte, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("codec: %s size short: %d", name, len(payload))
	}
	dataSize := binary.LittleEndian.Uint32(payload[:4])
	if dataSize == 0 {
		return nil, fmt.Errorf("codec: %s empty data", name)
	}
	if int64(dataSize) > protocol.MaxSurfaceBytes {
		return nil, fmt.Errorf("codec: %s data_size %d exceeds bound", name, dataSize)
	}
	if len(payload) < 4+int(dataSize) {
		return nil, fmt.Errorf("codec: %s data short: have %d need %d", name, len(payload)-4, dataSize)
	}
	return payload[4 : 4+dataSize], nil
}

func checkImageDims(img *RGBA, expectW, expectH uint32, name string) error {
	if img == nil {
		return fmt.Errorf("codec: %s nil image", name)
	}
	if expectW != 0 && uint32(img.Width) != expectW {
		return fmt.Errorf("codec: %s width %d != image desc %d", name, img.Width, expectW)
	}
	if expectH != 0 && uint32(img.Height) != expectH {
		return fmt.Errorf("codec: %s height %d != image desc %d", name, img.Height, expectH)
	}
	if img.Width > protocol.MaxSurfaceSide || img.Height > protocol.MaxSurfaceSide {
		return fmt.Errorf("codec: %s dimensions %dx%d exceed max side %d",
			name, img.Width, img.Height, protocol.MaxSurfaceSide)
	}
	return nil
}

// imageToRGBA converts any image.Image to tightly packed RGBA8888.
func imageToRGBA(src image.Image) *RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	outStride := w * 4
	out := make([]byte, h*outStride)

	// Fast path for common types.
	switch s := src.(type) {
	case *image.RGBA:
		if s.Rect.Min.X == 0 && s.Rect.Min.Y == 0 && s.Stride == outStride && len(s.Pix) >= h*outStride {
			copy(out, s.Pix[:h*outStride])
			return &RGBA{Width: w, Height: h, Stride: outStride, Pix: out}
		}
	case *image.YCbCr:
		// Fall through to draw path.
	}

	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	copy(out, rgba.Pix)
	return &RGBA{Width: w, Height: h, Stride: outStride, Pix: out}
}
