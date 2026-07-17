// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

// Package audio provides host playback sinks for SPICE RAW S16LE PCM.
//
// The playback channel (internal/channel, pkg/spice.PlaybackDriver) already
// decodes guest audio to interleaved signed 16-bit little-endian samples.
// This package turns those samples into host output when a platform backend
// is available.
//
// Product policy:
//
//   - GUI path calls OpenDefault() and, when non-nil, sets Drivers.Playback.
//   - Headless / tests keep NullPlayback (silent discard).
//   - OpenDefault never panics; init failure returns nil (session continues).
//   - Available() is true only when a real backend is compiled in.
//
// Backends (Phase 3):
//
//   - macOS / Windows: ebitengine/oto/v3 via purego (no cgo; Core Audio / WASAPI).
//   - Linux and other GOOS: stub (Available false) until an ALSA/Pulse path lands.
//
// Build tag noaudio forces the stub on any platform (CI / silent builds).
package audio
