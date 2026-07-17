// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"testing"

	"github.com/maskraven/spice-viewer/internal/channel"
	"github.com/maskraven/spice-viewer/internal/protocol"
)

func TestIsRecordMessage(t *testing.T) {
	for _, typ := range []uint16{
		protocol.MsgRecordStart,
		protocol.MsgRecordStop,
		protocol.MsgRecordVolume,
		protocol.MsgRecordMute,
	} {
		if !channel.IsRecordMessage(typ) {
			t.Errorf("type %d should be a record message", typ)
		}
	}
	if channel.IsRecordMessage(1) {
		t.Error("common MIGRATE should not be classified as record-only")
	}
}

func TestRecordStartModeMarkNoPCM(t *testing.T) {
	drv := channel.NewNullRecord()
	ch := channel.NewRecord(nil, drv)

	start := protocol.RecordStart{
		Channels:  1,
		Format:    protocol.AudioFmtS16,
		Frequency: 44100,
	}
	if err := ch.HandleMessage(protocol.MsgRecordStart, start.Encode()); err != nil {
		t.Fatalf("START: %v", err)
	}
	if !ch.Started() {
		t.Fatal("expected started")
	}
	if !ch.ModeSent() {
		t.Fatal("expected MODE+START_MARK path completed")
	}
	chs, fmt, freq, rec, _, _, starts, _ := drv.Snapshot()
	if !rec || starts != 1 || chs != 1 || fmt != protocol.AudioFmtS16 || freq != 44100 {
		t.Fatalf("driver state chs=%d fmt=%d freq=%d rec=%v starts=%d", chs, fmt, freq, rec, starts)
	}

	if err := ch.HandleMessage(protocol.MsgRecordStop, nil); err != nil {
		t.Fatalf("STOP: %v", err)
	}
	if ch.Started() {
		t.Fatal("expected stopped")
	}
	_, _, _, rec, _, _, _, stops := drv.Snapshot()
	if rec || stops != 1 {
		t.Fatalf("rec=%v stops=%d", rec, stops)
	}
}

func TestRecordVolumeMute(t *testing.T) {
	drv := channel.NewNullRecord()
	ch := channel.NewRecord(nil, drv)
	vol := protocol.RecordVolume{Volumes: []uint16{10, 20}}
	if err := ch.HandleMessage(protocol.MsgRecordVolume, vol.Encode()); err != nil {
		t.Fatal(err)
	}
	if err := ch.HandleMessage(protocol.MsgRecordMute, protocol.EncodeRecordMute(true)); err != nil {
		t.Fatal(err)
	}
	_, _, _, _, mute, vols, _, _ := drv.Snapshot()
	if !mute || len(vols) != 2 || vols[0] != 10 {
		t.Fatalf("mute=%v vols=%v", mute, vols)
	}
}

func TestRecordInvalidStartSoftFail(t *testing.T) {
	ch := channel.NewRecord(nil, channel.NewNullRecord())
	err := ch.HandleMessage(protocol.MsgRecordStart, protocol.RecordStart{
		Channels: 0, Format: protocol.AudioFmtS16, Frequency: 8000,
	}.Encode())
	if err == nil {
		t.Fatal("expected invalid channels error")
	}
	if ch.LastError() == nil {
		t.Fatal("expected LastError set")
	}
}

func TestRecordUnknownIgnored(t *testing.T) {
	ch := channel.NewRecord(nil, nil)
	if err := ch.HandleMessage(9999, nil); err != nil {
		t.Fatal(err)
	}
	if ch.UnknownCounts()[9999] != 1 {
		t.Fatalf("unknown counts: %v", ch.UnknownCounts())
	}
}
