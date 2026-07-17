// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build (darwin || windows) && !noaudio

package audio

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

// Available reports that a host backend is compiled into this binary.
func Available() bool {
	return true
}

// openHostSink creates a Sink backed by ebitengine/oto (Core Audio / WASAPI
// via purego — no cgo on macOS and Windows).
func openHostSink() (*Sink, error) {
	s := &Sink{
		volume: 1,
		pcm:    newPCMBuffer(defaultPCMCap),
		dev:    &otoDevice{},
	}
	return s, nil
}

// otoDevice lazily creates the process-wide oto.Context on first ensure.
// Oto allows only one Context per process; sample rate / channels are fixed
// after creation. Later streams that differ are accepted; the player keeps
// the original layout (SPICE usually sticks to one rate for a session).
type otoDevice struct {
	mu sync.Mutex

	ctx    *oto.Context
	player *oto.Player
	rate   int
	chans  int
	ready  bool
	closed bool
}

func (d *otoDevice) ensure(channels, frequency int, src io.Reader) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("audio: device closed")
	}
	if channels < 1 {
		channels = 1
	}
	if channels > 2 {
		channels = 2
	}
	if frequency < 8000 {
		frequency = 48000
	}

	if d.ctx == nil {
		opts := &oto.NewContextOptions{
			SampleRate:   frequency,
			ChannelCount: channels,
			Format:       oto.FormatSignedInt16LE,
			// Modest buffer: balance latency vs underrun for remote desktop.
			BufferSize: 50 * time.Millisecond,
		}
		ctx, readyCh, err := oto.NewContext(opts)
		if err != nil {
			return fmt.Errorf("oto.NewContext: %w", err)
		}
		// Wait briefly for the device to become ready; do not block forever.
		select {
		case <-readyCh:
		case <-time.After(2 * time.Second):
			// Context may still become ready; continue and try to play.
		}
		d.ctx = ctx
		d.rate = frequency
		d.chans = channels
		d.ready = true
	}

	// Recreate player against the current PCM source (fresh stream).
	if d.player != nil {
		d.player.Pause()
		_ = d.player.Close()
		d.player = nil
	}
	p := d.ctx.NewPlayer(src)
	// Small buffer for lower latency on remote desktop PCM.
	p.SetBufferSize(d.rate * d.chans * 2 / 20) // ~50ms of S16
	d.player = p
	return nil
}

func (d *otoDevice) setVolume(v float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.player == nil {
		return
	}
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	d.player.SetVolume(v)
}

func (d *otoDevice) play() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.player != nil {
		d.player.Play()
	}
}

func (d *otoDevice) pause() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.player != nil {
		d.player.Pause()
	}
}

func (d *otoDevice) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.player != nil {
		d.player.Pause()
		_ = d.player.Close()
		d.player = nil
	}
	// oto Context cannot be destroyed / recreated in-process; leave ctx.
}
