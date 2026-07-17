// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build windows && !cgo

package h264

import (
	"fmt"

	"github.com/maskraven/virt-viewer/internal/codec"
)

// Pure-Go Windows fallback when cgo is disabled (cross-compile, some CI).
// Still reports Available() so link caps stay consistent with cgo builds;
// Decode soft-fails until a cgo Media Foundation binary is used.

func available() bool { return true }

type nocgoDecoder struct{}

func newDecoder() (Decoder, error) {
	return &nocgoDecoder{}, nil
}

func (d *nocgoDecoder) Close() {}

func (d *nocgoDecoder) Decode(annexB []byte, w, h int) (*codec.RGBA, error) {
	if len(annexB) == 0 {
		return nil, FormatError("empty access unit")
	}
	_ = w
	_ = h
	return nil, fmt.Errorf("%w: Windows H.264 requires cgo Media Foundation build", ErrDecode)
}
