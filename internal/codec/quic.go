// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0
//
// SPICE Quic image decoder. Wire format and algorithm follow spice-common
// common/quic*.c; pure-Go port structure adapted from github.com/Shells-com/spice/quic
// (MIT License, Copyright 2021 E Shells Inc.).

package codec

import (
	"bytes"
	"fmt"
	"image"

	"github.com/maskraven/spice-viewer/internal/protocol"
)

// DecodeQuic decodes a SPICE_IMAGE_TYPE_QUIC payload (after SpiceImageDescriptor).
//
// Wire layout:
//
//	uint32 data_size
//	uint8  quic_bitstream[data_size]  // magic "QUIC", version, type, w, h, …
//
// Supported types: RGB24 and RGB32 (solid/gradient/natural images). Other Quic
// variants (GRAY, RGB16, RGBA) return an error so the display channel soft-skips.
func DecodeQuic(payload []byte, expectW, expectH uint32) (*RGBA, error) {
	data, err := binaryDataChunk(payload, "quic")
	if err != nil {
		return nil, err
	}
	img, err := decodeQuicBitstream(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		return nil, fmt.Errorf("codec: quic unexpected image type %T", img)
	}
	w, h := rgba.Bounds().Dx(), rgba.Bounds().Dy()
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("codec: quic empty dimensions %dx%d", w, h)
	}
	if w > protocol.MaxSurfaceSide || h > protocol.MaxSurfaceSide {
		return nil, fmt.Errorf("codec: quic dimensions %dx%d exceed max side %d",
			w, h, protocol.MaxSurfaceSide)
	}
	if expectW != 0 && uint32(w) != expectW {
		return nil, fmt.Errorf("codec: quic width %d != image desc %d", w, expectW)
	}
	if expectH != 0 && uint32(h) != expectH {
		return nil, fmt.Errorf("codec: quic height %d != image desc %d", h, expectH)
	}

	// Convert image.RGBA (may have non-zero Min or padded stride) to our RGBA.
	out := imageToRGBA(rgba)
	// Quic RGB24/32 paths force A=0xFF already; keep opaque for safety.
	ForceOpaque(out)
	return out, nil
}
