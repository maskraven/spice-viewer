// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package audio

import (
	"io"
	"testing"
	"time"
)

func TestAvailableConsistentWithOpenDefault(t *testing.T) {
	av := Available()
	d := OpenDefault()
	if !av && d != nil {
		t.Fatal("OpenDefault must return nil when Available is false")
	}
	// When Available is true, OpenDefault may still return nil if the device
	// fails; that is non-fatal. Only assert no panic and type safety.
	if d != nil {
		// Soft lifecycle without asserting audible output.
		d.Start(2, AudioFmtS16, 48000)
		d.SetVolume([]uint16{65535, 65535})
		d.SetMute(false)
		// 1 ms of silence stereo S16 @ 48 kHz: 48 * 2 * 2 = 192 bytes
		silent := make([]byte, 192)
		d.WritePCM(silent, 0)
		d.Stop()
		if s, ok := d.(*Sink); ok {
			s.Close()
		}
	}
}

func TestNullSinkStartStopWrite(t *testing.T) {
	s := NewNullSink()
	s.Start(2, AudioFmtS16, 44100)
	ch, freq, format, playing, mute, vol, starts, stops, writes, nbytes := s.Snapshot()
	if ch != 2 || freq != 44100 || format != AudioFmtS16 || !playing {
		t.Fatalf("start state: ch=%d freq=%d fmt=%d playing=%v", ch, freq, format, playing)
	}
	if starts != 1 || mute || vol != 1 {
		t.Fatalf("starts=%d mute=%v vol=%v", starts, mute, vol)
	}

	pcm := []byte{0x01, 0x00, 0x02, 0x00, 0x03, 0x00, 0x04, 0x00}
	s.WritePCM(pcm, 42)
	_, _, _, _, _, _, _, _, writes, nbytes = s.Snapshot()
	if writes != 1 || nbytes != len(pcm) {
		t.Fatalf("writes=%d bytes=%d", writes, nbytes)
	}
	if s.pcm.buffered() != len(pcm) {
		t.Fatalf("buffered=%d want %d", s.pcm.buffered(), len(pcm))
	}

	s.SetMute(true)
	s.WritePCM(pcm, 43)
	_, _, _, _, mute, _, _, _, writes, _ = s.Snapshot()
	if !mute || writes != 2 {
		t.Fatalf("mute write: mute=%v writes=%d", mute, writes)
	}
	// Mute must not enqueue.
	if s.pcm.buffered() != len(pcm) {
		t.Fatalf("mute should not append; buffered=%d", s.pcm.buffered())
	}

	s.Stop()
	_, _, _, playing, _, _, _, stops, _, _ = s.Snapshot()
	if playing || stops != 1 {
		t.Fatalf("after stop: playing=%v stops=%d", playing, stops)
	}
	if s.pcm.buffered() != 0 {
		t.Fatalf("stop should reset buffer; buffered=%d", s.pcm.buffered())
	}
}

func TestNullSinkUnsupportedFormatSilences(t *testing.T) {
	s := NewNullSink()
	s.Start(1, 99, 8000) // not S16
	s.WritePCM([]byte{0, 0}, 0)
	if s.pcm.buffered() != 0 {
		t.Fatal("unsupported format must not enqueue PCM")
	}
}

func TestVolumesToGain(t *testing.T) {
	if g := volumesToGain(nil); g != 1 {
		t.Fatalf("nil → %v want 1", g)
	}
	if g := volumesToGain([]uint16{}); g != 1 {
		t.Fatalf("empty → %v want 1", g)
	}
	if g := volumesToGain([]uint16{65535}); g != 1 {
		t.Fatalf("full → %v want 1", g)
	}
	if g := volumesToGain([]uint16{0}); g != 0 {
		t.Fatalf("zero → %v want 0", g)
	}
	half := volumesToGain([]uint16{32768, 32768})
	if half < 0.49 || half > 0.51 {
		t.Fatalf("half → %v", half)
	}
}

func TestSetVolumeAndMuteAffectGain(t *testing.T) {
	s := NewNullSink()
	s.Start(1, AudioFmtS16, 16000)
	s.SetVolume([]uint16{0})
	_, _, _, _, _, vol, _, _, _, _ := s.Snapshot()
	if vol != 0 {
		t.Fatalf("vol=%v", vol)
	}
	s.SetVolume([]uint16{65535})
	s.SetMute(true)
	s.mu.Lock()
	g := s.effectiveVolumeLocked()
	s.mu.Unlock()
	if g != 0 {
		t.Fatalf("muted gain=%v", g)
	}
	s.SetMute(false)
	s.mu.Lock()
	g = s.effectiveVolumeLocked()
	s.mu.Unlock()
	if g != 1 {
		t.Fatalf("unmuted gain=%v", g)
	}
}

func TestPCMBufferReadSilenceOnUnderrun(t *testing.T) {
	b := newPCMBuffer(1024)
	out := make([]byte, 8)
	n, err := b.Read(out)
	if err != nil || n != 8 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	for _, v := range out {
		if v != 0 {
			t.Fatalf("expected silence, got %v", out)
		}
	}

	b.write([]byte{1, 2, 3, 4})
	n, err = b.Read(out)
	if err != nil || n != 8 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if out[0] != 1 || out[1] != 2 || out[2] != 3 || out[3] != 4 {
		t.Fatalf("got %v", out[:4])
	}
	// remainder silence
	if out[4] != 0 || out[5] != 0 {
		t.Fatalf("pad %v", out[4:])
	}
}

func TestPCMBufferDropOldestWhenFull(t *testing.T) {
	b := newPCMBuffer(8)
	b.write([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	b.write([]byte{9, 10}) // overflow 2 → drop 1,2
	if b.buffered() != 8 {
		t.Fatalf("buffered=%d want 8", b.buffered())
	}
	out := make([]byte, 8)
	_, _ = b.Read(out)
	// After drop of 2 bytes: 3..8,9,10
	want := []byte{3, 4, 5, 6, 7, 8, 9, 10}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("out=%v want %v", out, want)
		}
	}
}

func TestPCMBufferCloseEOF(t *testing.T) {
	b := newPCMBuffer(64)
	b.close()
	n, err := b.Read(make([]byte, 4))
	if n != 0 || err != io.EOF {
		t.Fatalf("n=%d err=%v want EOF", n, err)
	}
}

func TestWritePCMDoesNotRetainCallerBuffer(t *testing.T) {
	s := NewNullSink()
	s.Start(1, AudioFmtS16, 8000)
	pcm := []byte{0xaa, 0xbb}
	s.WritePCM(pcm, 0)
	pcm[0] = 0x00 // mutate caller slice
	out := make([]byte, 2)
	// Drain via Read on the internal buffer.
	_, _ = s.pcm.Read(out)
	if out[0] != 0xaa || out[1] != 0xbb {
		t.Fatalf("sink retained caller buffer: %x", out)
	}
}

func TestOpenDefaultNeverPanics(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = OpenDefault()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("OpenDefault hung")
	}
}

// Compile-time check: *Sink implements Driver.
var _ Driver = (*Sink)(nil)
