// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"bytes"
	"testing"

	"github.com/maskraven/spice-viewer/internal/protocol"
)

func TestRecordStartRoundTrip(t *testing.T) {
	in := protocol.RecordStart{
		Channels:  1,
		Format:    protocol.AudioFmtS16,
		Frequency: 48000,
	}
	b := in.Encode()
	if len(b) != protocol.RecordStartSize {
		t.Fatalf("len=%d want %d", len(b), protocol.RecordStartSize)
	}
	out, err := protocol.DecodeRecordStart(b)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v want %+v", out, in)
	}
}

func TestRecordStartShort(t *testing.T) {
	if _, err := protocol.DecodeRecordStart([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected short error")
	}
}

func TestRecordModeAndDataReusePlaybackLayout(t *testing.T) {
	mode := protocol.RecordMode{Time: 7, Mode: protocol.AudioDataModeRaw}
	mb := protocol.EncodeRecordMode(mode)
	mout, err := protocol.DecodeRecordMode(mb)
	if err != nil {
		t.Fatal(err)
	}
	if mout.Time != 7 || mout.Mode != protocol.AudioDataModeRaw {
		t.Fatalf("mode %+v", mout)
	}

	data := protocol.RecordData{Time: 9, Data: []byte{0, 1, 2, 3}}
	db := protocol.EncodeRecordData(data)
	dout, err := protocol.DecodeRecordData(db)
	if err != nil {
		t.Fatal(err)
	}
	if dout.Time != 9 || !bytes.Equal(dout.Data, data.Data) {
		t.Fatalf("data %+v", dout)
	}
}

func TestRecordStartMarkRoundTrip(t *testing.T) {
	b := protocol.EncodeRecordStartMark(0xaabbccdd)
	if len(b) != protocol.RecordStartMarkSize {
		t.Fatalf("len=%d", len(b))
	}
	v, err := protocol.DecodeRecordStartMark(b)
	if err != nil {
		t.Fatal(err)
	}
	if v != 0xaabbccdd {
		t.Fatalf("got %#x", v)
	}
}

func TestRecordCapBitsDifferFromPlayback(t *testing.T) {
	// Documented divergence: record has no LATENCY, so OPUS is bit 2 not 3.
	if protocol.RecordCapOpus != 2 {
		t.Fatalf("RecordCapOpus=%d want 2", protocol.RecordCapOpus)
	}
	if protocol.PlaybackCapOpus != 3 {
		t.Fatalf("PlaybackCapOpus=%d want 3", protocol.PlaybackCapOpus)
	}
	if protocol.RecordCapVolume != protocol.PlaybackCapVolume {
		t.Fatalf("VOLUME bit should match: record=%d playback=%d",
			protocol.RecordCapVolume, protocol.PlaybackCapVolume)
	}
}

func TestRecordMuteVolume(t *testing.T) {
	if m, err := protocol.DecodeRecordMute(protocol.EncodeRecordMute(true)); err != nil || !m {
		t.Fatalf("mute true: m=%v err=%v", m, err)
	}
	vol := protocol.RecordVolume{Volumes: []uint16{100, 200}}
	out, err := protocol.DecodeRecordVolume(vol.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Volumes) != 2 || out.Volumes[0] != 100 || out.Volumes[1] != 200 {
		t.Fatalf("volumes %+v", out.Volumes)
	}
}
