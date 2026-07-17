// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

// Package h264 decodes SPICE H.264 display streams.
//
// Platform backends (see docs/phase3.md):
//   - macOS: VideoToolbox (system framework, cgo)
//   - Windows: Media Foundation H.264 MFT (system APIs, cgo → RGBA)
//   - Linux: user-provided FFmpeg CLI on PATH (never bundled; subprocess)
//   - other GOOS: stub with Available() == false
//
// Default product policy: do not ship FFmpeg. Advertise H.264 on the display
// channel only when Available() is true; soft-skip streams otherwise.
package h264

import (
	"errors"
	"fmt"

	"github.com/maskraven/spice-viewer/internal/codec"
)

// ErrUnavailable is returned when no decoder backend is available for this
// GOOS (or the backend failed to initialize — e.g. missing system FFmpeg).
var ErrUnavailable = errors.New("h264: OS decoder unavailable")

// ErrDecode is returned for bitstream or decoder runtime failures.
var ErrDecode = errors.New("h264: decode failed")

// Decoder turns Annex-B access units into RGBA frames.
//
// Implementations are not required to be concurrent-safe; the display channel
// owns one decoder per stream and calls Decode from a single goroutine.
type Decoder interface {
	// Decode feeds one access unit (Annex-B, may contain SPS/PPS + slice NALs).
	// w/h are stream dimensions from STREAM_CREATE (hints; 0 = unknown).
	// Returns nil, ErrDecode on recoverable frame skips; fatal init errors
	// return ErrUnavailable.
	Decode(annexB []byte, w, h int) (*codec.RGBA, error)
	// Close releases native decoder resources.
	Close()
}

// Available reports whether this binary can decode H.264 (OS stack or
// discoverable user FFmpeg on Linux). Display/session must not advertise
// DisplayCapCodecH264 when false.
func Available() bool {
	return available()
}

// New creates a platform Decoder. Returns ErrUnavailable when none is built,
// FFmpeg is missing on Linux, or initialization fails.
func New() (Decoder, error) {
	return newDecoder()
}

// DecodeOnce is a convenience for tests and one-shot frames: create, decode, close.
func DecodeOnce(annexB []byte, w, h int) (*codec.RGBA, error) {
	d, err := New()
	if err != nil {
		return nil, err
	}
	defer d.Close()
	return d.Decode(annexB, w, h)
}

// FormatError wraps a parse/dimension problem with context.
func FormatError(msg string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrDecode, fmt.Sprintf(msg, args...))
}
