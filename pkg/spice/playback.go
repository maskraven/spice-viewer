// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice

import "github.com/maskraven/virt-viewer/internal/channel"

// PlaybackDriver receives decoded guest audio for host playback (Phase 2).
//
// Wire path: PLAYBACK channel MODE/START/DATA/STOP (and VOLUME/MUTE). Only
// RAW S16LE PCM is delivered via WritePCM; compressed modes are ignored by the
// channel layer when the client does not advertise OPUS/CELT.
//
// UI backends implement this interface and attach it via Drivers.Playback.
// A nil PlaybackDriver discards samples (NullPlayback). Open/runtime failures
// are best-effort: the session continues without audible output.
//
// Typical UI hook:
//
//	type sink struct{ /* host audio device */ }
//	func (s *sink) Start(ch int, format uint16, freq int) { /* open stream */ }
//	func (s *sink) Stop() { /* drain/close */ }
//	func (s *sink) WritePCM(samples []byte, timeMs uint32) { /* queue S16LE */ }
//	func (s *sink) SetVolume(vols []uint16) { /* 0..65535 per channel */ }
//	func (s *sink) SetMute(m bool) {}
//
//	cfg.Drivers.Playback = sink
//
// No cgo pure-Go host sink is wired by default on macOS; NullPlayback is the
// safe default for headless/tests. Real sinks live in the UI process.
type PlaybackDriver interface {
	// Start configures the sink for a new stream (channels, AudioFmt*, Hz).
	Start(channels int, format uint16, frequency int)
	// Stop ends the current stream.
	Stop()
	// WritePCM delivers interleaved S16LE PCM; samples must be treated as
	// immutable after return (copy if the sink retains them).
	WritePCM(samples []byte, timeMs uint32)
	// SetVolume sets per-channel volume (0..65535).
	SetVolume(volumes []uint16)
	// SetMute mutes or unmutes.
	SetMute(mute bool)
}

// Compile-time check: channel.NullPlayback implements PlaybackDriver.
var _ PlaybackDriver = (*channel.NullPlayback)(nil)

// NullPlayback is a headless PlaybackDriver that discards samples.
// It is an alias of channel.NullPlayback.
type NullPlayback = channel.NullPlayback

// NewNullPlayback returns a NullPlayback.
func NewNullPlayback() *NullPlayback {
	return channel.NewNullPlayback()
}

// asPlaybackDriver adapts a public PlaybackDriver to channel.PlaybackDriver.
func asPlaybackDriver(d PlaybackDriver) channel.PlaybackDriver {
	if d == nil {
		return nil
	}
	if pd, ok := d.(channel.PlaybackDriver); ok {
		return pd
	}
	return playbackDriverAdapter{d}
}

type playbackDriverAdapter struct {
	d PlaybackDriver
}

func (a playbackDriverAdapter) Start(channels int, format uint16, frequency int) {
	a.d.Start(channels, format, frequency)
}

func (a playbackDriverAdapter) Stop() {
	a.d.Stop()
}

func (a playbackDriverAdapter) WritePCM(samples []byte, timeMs uint32) {
	a.d.WritePCM(samples, timeMs)
}

func (a playbackDriverAdapter) SetVolume(volumes []uint16) {
	a.d.SetVolume(volumes)
}

func (a playbackDriverAdapter) SetMute(mute bool) {
	a.d.SetMute(mute)
}
