// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"bytes"
	"testing"

	"github.com/maskraven/spice-viewer/internal/protocol"
)

func TestPlaybackModeRoundTrip(t *testing.T) {
	in := protocol.PlaybackMode{Time: 0x11223344, Mode: protocol.AudioDataModeRaw}
	b := in.Encode()
	if len(b) != protocol.PlaybackModeFixedSize {
		t.Fatalf("len=%d want %d", len(b), protocol.PlaybackModeFixedSize)
	}
	out, err := protocol.DecodePlaybackMode(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.Time != in.Time || out.Mode != in.Mode || len(out.Data) != 0 {
		t.Fatalf("got %+v want %+v", out, in)
	}
}

func TestPlaybackModeWithData(t *testing.T) {
	in := protocol.PlaybackMode{
		Time: 1,
		Mode: protocol.AudioDataModeOpus,
		Data: []byte{0xaa, 0xbb},
	}
	out, err := protocol.DecodePlaybackMode(in.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if out.Mode != protocol.AudioDataModeOpus || !bytes.Equal(out.Data, in.Data) {
		t.Fatalf("got %+v", out)
	}
}

func TestPlaybackModeShort(t *testing.T) {
	if _, err := protocol.DecodePlaybackMode([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected short error")
	}
}

func TestPlaybackStartRoundTrip(t *testing.T) {
	in := protocol.PlaybackStart{
		Channels:  2,
		Format:    protocol.AudioFmtS16,
		Frequency: 48000,
		Time:      99,
	}
	b := in.Encode()
	if len(b) != protocol.PlaybackStartSize {
		t.Fatalf("len=%d", len(b))
	}
	out, err := protocol.DecodePlaybackStart(b)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v want %+v", out, in)
	}
}

func TestPlaybackDataRoundTrip(t *testing.T) {
	pcm := []byte{0x00, 0x01, 0xff, 0x7f} // two S16LE samples
	in := protocol.PlaybackData{Time: 42, Data: pcm}
	b := in.Encode()
	out, err := protocol.DecodePlaybackData(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.Time != 42 || !bytes.Equal(out.Data, pcm) {
		t.Fatalf("got time=%d data=%x", out.Time, out.Data)
	}
}

func TestPlaybackVolumeRoundTrip(t *testing.T) {
	in := protocol.PlaybackVolume{Volumes: []uint16{0, 32768, 65535}}
	out, err := protocol.DecodePlaybackVolume(in.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Volumes) != 3 || out.Volumes[0] != 0 || out.Volumes[1] != 32768 || out.Volumes[2] != 65535 {
		t.Fatalf("got %v", out.Volumes)
	}
}

func TestPlaybackVolumeEmpty(t *testing.T) {
	out, err := protocol.DecodePlaybackVolume([]byte{0})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Volumes) != 0 {
		t.Fatalf("volumes=%v", out.Volumes)
	}
}

func TestPlaybackMuteAndLatency(t *testing.T) {
	mute, err := protocol.DecodePlaybackMute(protocol.EncodePlaybackMute(true))
	if err != nil || !mute {
		t.Fatalf("mute=%v err=%v", mute, err)
	}
	mute, err = protocol.DecodePlaybackMute(protocol.EncodePlaybackMute(false))
	if err != nil || mute {
		t.Fatalf("mute=%v err=%v", mute, err)
	}
	ms, err := protocol.DecodePlaybackLatency(protocol.EncodePlaybackLatency(200))
	if err != nil || ms != 200 {
		t.Fatalf("ms=%d err=%v", ms, err)
	}
}

func TestPlaybackMessageConstants(t *testing.T) {
	// spice/enums.h order from MsgFirstAvail.
	if protocol.MsgPlaybackData != 101 || protocol.MsgPlaybackMode != 102 ||
		protocol.MsgPlaybackStart != 103 || protocol.MsgPlaybackStop != 104 ||
		protocol.MsgPlaybackVolume != 105 || protocol.MsgPlaybackMute != 106 ||
		protocol.MsgPlaybackLatency != 107 {
		t.Fatalf("playback message ids out of order")
	}
	if protocol.ChannelPlayback != 5 {
		t.Fatalf("ChannelPlayback=%d want 5", protocol.ChannelPlayback)
	}
}
