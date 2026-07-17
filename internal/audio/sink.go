// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package audio

import (
	"io"
	"log"
	"sync"
)

// AudioFmtS16 matches protocol.AudioFmtS16 (signed 16-bit LE PCM).
// Duplicated here so this package does not depend on internal/protocol.
const AudioFmtS16 uint16 = 1

// Driver is the host sink API. It matches pkg/spice.PlaybackDriver so a
// non-nil value from OpenDefault can be assigned to ConnectConfig.Drivers.Playback.
type Driver interface {
	Start(channels int, format uint16, frequency int)
	Stop()
	WritePCM(samples []byte, timeMs uint32)
	SetVolume(volumes []uint16)
	SetMute(mute bool)
}

// Sink is a host playback driver. On platforms without a backend, OpenDefault
// returns nil and tests may use NewNullSink.
//
// Methods are safe for concurrent use with the playback channel goroutine.
type Sink struct {
	mu sync.Mutex

	// stream config (last Start)
	channels  int
	format    uint16
	frequency int
	playing   bool
	mute      bool
	volume    float64 // 0..1, applied to the host player when present

	// platform handle; nil when stub or not yet opened
	dev device

	// pcm is the live ring fed by WritePCM and drained by the device reader.
	pcm *pcmBuffer

	// test hooks / diagnostics
	StartCount int
	StopCount  int
	WriteCount int
	WriteBytes int
}

// device is the platform audio handle (oto player, etc.).
type device interface {
	// ensure opens or reconfigures for the given layout. Returns an error on
	// hard failure; soft failure should log and leave the device silent.
	ensure(channels, frequency int, src io.Reader) error
	setVolume(v float64)
	play()
	pause()
	close()
}

// OpenDefault tries to open the default host playback device.
// Returns nil when the platform has no backend, the backend fails to init, or
// the noaudio build tag is set. Callers must treat nil as "use NullPlayback".
func OpenDefault() Driver {
	if !Available() {
		return nil
	}
	s, err := openHostSink()
	if err != nil {
		log.Printf("audio: OpenDefault: %v (continuing without host playback)", err)
		return nil
	}
	return s
}

// NewNullSink returns a Sink that accepts PCM but never opens a host device.
// Useful for unit tests of Start/Stop/Write without hardware.
func NewNullSink() *Sink {
	return &Sink{
		volume: 1,
		pcm:    newPCMBuffer(defaultPCMCap),
	}
}

// Start implements Driver.
func (s *Sink) Start(channels int, format uint16, frequency int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.StartCount++

	if channels < 1 {
		channels = 1
	}
	if channels > 2 {
		// Host path plays mono or stereo; extra channels are downmixed later.
		channels = 2
	}
	if frequency < 1 {
		frequency = 48000
	}
	s.channels = channels
	s.format = format
	s.frequency = frequency
	s.playing = true

	if s.pcm == nil {
		s.pcm = newPCMBuffer(defaultPCMCap)
	}
	s.pcm.reset()

	if format != AudioFmtS16 {
		log.Printf("audio: Start: unsupported format %d (want S16); silencing", format)
		return
	}
	if s.dev == nil {
		return
	}
	if err := s.dev.ensure(channels, frequency, s.pcm); err != nil {
		log.Printf("audio: Start: device ensure: %v (silencing)", err)
		return
	}
	s.dev.setVolume(s.effectiveVolumeLocked())
	s.dev.play()
}

// Stop implements Driver.
func (s *Sink) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.StopCount++
	s.playing = false
	if s.pcm != nil {
		s.pcm.reset()
	}
	if s.dev != nil {
		s.dev.pause()
	}
}

// WritePCM implements Driver. samples is interleaved S16LE; it is copied.
func (s *Sink) WritePCM(samples []byte, timeMs uint32) {
	if s == nil || len(samples) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.WriteCount++
	s.WriteBytes += len(samples)
	_ = timeMs

	if !s.playing || s.mute || s.format != AudioFmtS16 {
		return
	}
	if s.pcm == nil {
		return
	}
	// Copy so the caller may reuse the buffer.
	s.pcm.write(append([]byte(nil), samples...))
}

// SetVolume implements Driver. volumes are 0..65535 per channel.
func (s *Sink) SetVolume(volumes []uint16) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.volume = volumesToGain(volumes)
	if s.dev != nil {
		s.dev.setVolume(s.effectiveVolumeLocked())
	}
}

// SetMute implements Driver.
func (s *Sink) SetMute(mute bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mute = mute
	if s.dev != nil {
		s.dev.setVolume(s.effectiveVolumeLocked())
	}
}

// Close releases the host device. Safe to call multiple times.
// OpenDefault sinks should be closed when the GUI session ends (best-effort).
func (s *Sink) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.playing = false
	if s.pcm != nil {
		s.pcm.close()
	}
	if s.dev != nil {
		s.dev.close()
		s.dev = nil
	}
}

// Snapshot returns diagnostic counters (tests).
func (s *Sink) Snapshot() (channels, frequency int, format uint16, playing, mute bool, volume float64, starts, stops, writes, bytes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channels, s.frequency, s.format, s.playing, s.mute, s.volume,
		s.StartCount, s.StopCount, s.WriteCount, s.WriteBytes
}

func (s *Sink) effectiveVolumeLocked() float64 {
	if s.mute {
		return 0
	}
	if s.volume < 0 {
		return 0
	}
	if s.volume > 1 {
		return 1
	}
	return s.volume
}

// volumesToGain maps SPICE per-channel 0..65535 volumes to a single 0..1 gain.
// Empty/nil means full volume (1).
func volumesToGain(volumes []uint16) float64 {
	if len(volumes) == 0 {
		return 1
	}
	var sum uint32
	for _, v := range volumes {
		sum += uint32(v)
	}
	avg := float64(sum) / float64(len(volumes))
	return avg / 65535.0
}

// --- PCM ring buffer (io.Reader for the host player) ---

// defaultPCMCap is ~200ms of stereo S16 at 48 kHz.
const defaultPCMCap = 48000 * 2 * 2 * 200 / 1000

// pcmBuffer is a bounded byte queue. Read returns silence on underrun so the
// host player never blocks the audio callback path longer than necessary.
type pcmBuffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	cap    int
	closed bool
}

func newPCMBuffer(capacity int) *pcmBuffer {
	if capacity < 1 {
		capacity = defaultPCMCap
	}
	b := &pcmBuffer{cap: capacity}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *pcmBuffer) write(p []byte) {
	if len(p) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	// Drop oldest samples if the queue would exceed cap (prefer low latency).
	if len(b.buf)+len(p) > b.cap {
		overflow := len(b.buf) + len(p) - b.cap
		if overflow >= len(b.buf) {
			b.buf = b.buf[:0]
		} else {
			b.buf = b.buf[overflow:]
		}
	}
	b.buf = append(b.buf, p...)
	b.cond.Signal()
}

func (b *pcmBuffer) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = b.buf[:0]
	b.closed = false
}

func (b *pcmBuffer) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.buf = b.buf[:0]
	b.cond.Broadcast()
}

// Read implements io.Reader. On underrun, fills the remainder with zeros
// (silence) and returns len(p). Returns io.EOF only after close with empty buf.
func (b *pcmBuffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed && len(b.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.buf)
	b.buf = b.buf[n:]
	// Pad underrun with silence so oto keeps a steady stream.
	for i := n; i < len(p); i++ {
		p[i] = 0
	}
	return len(p), nil
}

// buffered returns queued byte count (tests).
func (b *pcmBuffer) buffered() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf)
}
