// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin && !windows && !linux

package h264

// Stub for GOOS without a Phase-3 H.264 backend.
// macOS → decode_darwin.go (VideoToolbox)
// Windows → decode_windows.go (Media Foundation)
// Linux → decode_linux.go (user-provided FFmpeg on PATH)

func available() bool { return false }

func newDecoder() (Decoder, error) {
	return nil, ErrUnavailable
}
