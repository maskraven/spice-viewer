// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/maskraven/virt-viewer/internal/channel"
	"github.com/maskraven/virt-viewer/internal/protocol"
)

func TestIsPlaybackMessage(t *testing.T) {
	for _, typ := range []uint16{
		protocol.MsgPlaybackData,
		protocol.MsgPlaybackMode,
		protocol.MsgPlaybackStart,
		protocol.MsgPlaybackStop,
		protocol.MsgPlaybackVolume,
		protocol.MsgPlaybackMute,
		protocol.MsgPlaybackLatency,
	} {
		if !channel.IsPlaybackMessage(typ) {
			t.Errorf("type %d should be a playback message", typ)
		}
	}
	if channel.IsPlaybackMessage(1) {
		t.Error("common MIGRATE should not be classified as playback-only")
	}
}

func TestPlaybackRawPCMWrite(t *testing.T) {
	drv := channel.NewNullPlayback()
	ch := channel.NewPlayback(nil, drv)

	if err := ch.HandleMessage(protocol.MsgPlaybackMode, protocol.PlaybackMode{
		Time: 1, Mode: protocol.AudioDataModeRaw,
	}.Encode()); err != nil {
		t.Fatalf("MODE: %v", err)
	}
	if ch.Mode() != protocol.AudioDataModeRaw {
		t.Fatalf("mode=%d", ch.Mode())
	}

	start := protocol.PlaybackStart{
		Channels: 2, Format: protocol.AudioFmtS16, Frequency: 48000, Time: 10,
	}
	if err := ch.HandleMessage(protocol.MsgPlaybackStart, start.Encode()); err != nil {
		t.Fatalf("START: %v", err)
	}
	if !ch.Started() {
		t.Fatal("expected started")
	}
	channels, format, freq, playing, _, _, _, _, _, _ := drv.Snapshot()
	if channels != 2 || format != protocol.AudioFmtS16 || freq != 48000 || !playing {
		t.Fatalf("driver start state: ch=%d fmt=%d freq=%d playing=%v", channels, format, freq, playing)
	}

	// Four S16LE samples (2 channels × 2 frames): L=0x0001 R=0x0002 L=0x0003 R=0x0004
	pcm := []byte{0x01, 0x00, 0x02, 0x00, 0x03, 0x00, 0x04, 0x00}
	if err := ch.HandleMessage(protocol.MsgPlaybackData, protocol.PlaybackData{
		Time: 100, Data: pcm,
	}.Encode()); err != nil {
		t.Fatalf("DATA: %v", err)
	}
	_, _, _, _, _, _, writes, nbytes, lastTime, got := drv.Snapshot()
	if writes != 1 || nbytes != len(pcm) || lastTime != 100 {
		t.Fatalf("writes=%d bytes=%d time=%d", writes, nbytes, lastTime)
	}
	if !bytes.Equal(got, pcm) {
		t.Fatalf("pcm=%x want %x", got, pcm)
	}

	if err := ch.HandleMessage(protocol.MsgPlaybackStop, nil); err != nil {
		t.Fatalf("STOP: %v", err)
	}
	if ch.Started() {
		t.Fatal("expected stopped")
	}
	_, _, _, playing, _, _, _, _, _, _ = drv.Snapshot()
	if playing {
		t.Fatal("driver still playing after STOP")
	}
}

func TestPlaybackMuteSkipsPCM(t *testing.T) {
	drv := channel.NewNullPlayback()
	ch := channel.NewPlayback(nil, drv)
	_ = ch.HandleMessage(protocol.MsgPlaybackMode, protocol.PlaybackMode{Mode: protocol.AudioDataModeRaw}.Encode())
	_ = ch.HandleMessage(protocol.MsgPlaybackStart, protocol.PlaybackStart{
		Channels: 1, Format: protocol.AudioFmtS16, Frequency: 44100,
	}.Encode())
	if err := ch.HandleMessage(protocol.MsgPlaybackMute, protocol.EncodePlaybackMute(true)); err != nil {
		t.Fatal(err)
	}
	pcm := []byte{0x00, 0x10}
	_ = ch.HandleMessage(protocol.MsgPlaybackData, protocol.PlaybackData{Time: 1, Data: pcm}.Encode())
	_, _, _, _, mute, _, writes, _, _, _ := drv.Snapshot()
	if !mute {
		t.Fatal("mute not set on driver")
	}
	if writes != 0 {
		t.Fatalf("writes=%d want 0 while muted", writes)
	}
}

func TestPlaybackVolume(t *testing.T) {
	drv := channel.NewNullPlayback()
	ch := channel.NewPlayback(nil, drv)
	vol := protocol.PlaybackVolume{Volumes: []uint16{100, 200}}
	if err := ch.HandleMessage(protocol.MsgPlaybackVolume, vol.Encode()); err != nil {
		t.Fatal(err)
	}
	_, _, _, _, _, vols, _, _, _, _ := drv.Snapshot()
	if len(vols) != 2 || vols[0] != 100 || vols[1] != 200 {
		t.Fatalf("vols=%v", vols)
	}
}

func TestPlaybackIgnoresOpusData(t *testing.T) {
	drv := channel.NewNullPlayback()
	ch := channel.NewPlayback(nil, drv)
	_ = ch.HandleMessage(protocol.MsgPlaybackMode, protocol.PlaybackMode{Mode: protocol.AudioDataModeOpus}.Encode())
	_ = ch.HandleMessage(protocol.MsgPlaybackStart, protocol.PlaybackStart{
		Channels: 2, Format: protocol.AudioFmtS16, Frequency: 48000,
	}.Encode())
	_ = ch.HandleMessage(protocol.MsgPlaybackData, protocol.PlaybackData{
		Time: 1, Data: []byte{0xde, 0xad, 0xbe, 0xef},
	}.Encode())
	_, _, _, _, _, _, writes, _, _, _ := drv.Snapshot()
	if writes != 0 {
		t.Fatalf("OPUS data must not be written as PCM; writes=%d", writes)
	}
}

func TestPlaybackDataBeforeStartIgnored(t *testing.T) {
	drv := channel.NewNullPlayback()
	ch := channel.NewPlayback(nil, drv)
	_ = ch.HandleMessage(protocol.MsgPlaybackMode, protocol.PlaybackMode{Mode: protocol.AudioDataModeRaw}.Encode())
	_ = ch.HandleMessage(protocol.MsgPlaybackData, protocol.PlaybackData{
		Time: 1, Data: []byte{0, 0},
	}.Encode())
	_, _, _, _, _, _, writes, _, _, _ := drv.Snapshot()
	if writes != 0 {
		t.Fatalf("writes=%d before START", writes)
	}
}

func TestPlaybackRunLoop(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	drv := channel.NewNullPlayback()
	ch := channel.NewPlayback(c1, drv)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- ch.Run(ctx) }()

	// Server side: MODE, START, DATA, STOP, then close.
	writeMsg := func(typ uint16, body []byte) {
		t.Helper()
		if err := protocol.WriteMessage(c2, typ, body); err != nil {
			t.Fatalf("write type %d: %v", typ, err)
		}
	}
	writeMsg(protocol.MsgPlaybackMode, protocol.PlaybackMode{Mode: protocol.AudioDataModeRaw}.Encode())
	writeMsg(protocol.MsgPlaybackStart, protocol.PlaybackStart{
		Channels: 1, Format: protocol.AudioFmtS16, Frequency: 8000, Time: 0,
	}.Encode())
	pcm := []byte{0x10, 0x20}
	writeMsg(protocol.MsgPlaybackData, protocol.PlaybackData{Time: 5, Data: pcm}.Encode())
	writeMsg(protocol.MsgPlaybackStop, nil)

	// Wait for the Write to land.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, _, _, _, _, _, writes, _, _, _ := drv.Snapshot()
		if writes >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, _, _, _, _, _, writes, nbytes, lastTime, got := drv.Snapshot()
	if writes != 1 || nbytes != 2 || lastTime != 5 || !bytes.Equal(got, pcm) {
		t.Fatalf("writes=%d bytes=%d time=%d pcm=%x", writes, nbytes, lastTime, got)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && err != io.EOF {
			// cancel or close is fine
			if err != context.Canceled {
				// Accept closed pipe / cancel.
				_ = err
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit")
	}
}

func TestPlaybackSetAckAndPing(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ch := channel.NewPlayback(c1, channel.NewNullPlayback())

	// SET_ACK: generation + window. net.Pipe is unbuffered — write in a
	// goroutine while the test reads the reply.
	var setAck [8]byte
	binary.LittleEndian.PutUint32(setAck[0:4], 7)
	binary.LittleEndian.PutUint32(setAck[4:8], 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- ch.HandleMessage(protocol.MsgSetAck, setAck[:])
	}()
	msg, err := protocol.ReadMessage(c2)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if msg.Type != protocol.MsgcAckSync {
		t.Fatalf("type=%d want ACK_SYNC", msg.Type)
	}
	if binary.LittleEndian.Uint32(msg.Data) != 7 {
		t.Fatalf("gen=%d", binary.LittleEndian.Uint32(msg.Data))
	}

	// PING
	var ping [12]byte
	binary.LittleEndian.PutUint32(ping[0:4], 3)
	binary.LittleEndian.PutUint64(ping[4:12], 99)
	go func() {
		errCh <- ch.HandleMessage(protocol.MsgPing, ping[:])
	}()
	msg, err = protocol.ReadMessage(c2)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if msg.Type != protocol.MsgcPong {
		t.Fatalf("type=%d want PONG", msg.Type)
	}
}

// Mock writer used as PlaybackDriver for interface tests.
type mockPCMWriter struct {
	wrote [][]byte
	times []uint32
}

func (m *mockPCMWriter) Start(channels int, format uint16, frequency int) {}
func (m *mockPCMWriter) Stop()                                            {}
func (m *mockPCMWriter) WritePCM(samples []byte, timeMs uint32) {
	m.wrote = append(m.wrote, append([]byte(nil), samples...))
	m.times = append(m.times, timeMs)
}
func (m *mockPCMWriter) SetVolume(volumes []uint16) {}
func (m *mockPCMWriter) SetMute(mute bool)          {}

func TestPlaybackMockWriter(t *testing.T) {
	w := &mockPCMWriter{}
	ch := channel.NewPlayback(nil, w)
	_ = ch.HandleMessage(protocol.MsgPlaybackMode, protocol.PlaybackMode{Mode: protocol.AudioDataModeRaw}.Encode())
	_ = ch.HandleMessage(protocol.MsgPlaybackStart, protocol.PlaybackStart{
		Channels: 1, Format: protocol.AudioFmtS16, Frequency: 16000,
	}.Encode())
	_ = ch.HandleMessage(protocol.MsgPlaybackData, protocol.PlaybackData{
		Time: 50, Data: []byte{0xaa, 0xbb},
	}.Encode())
	if len(w.wrote) != 1 || !bytes.Equal(w.wrote[0], []byte{0xaa, 0xbb}) || w.times[0] != 50 {
		t.Fatalf("mock writer: %+v", w)
	}
}
