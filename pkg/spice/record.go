// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice

import "github.com/maskraven/spice-viewer/internal/channel"

// RecordDriver supplies host microphone PCM for guest recording (Phase 3).
//
// Wire path: RECORD channel START → client MODE=RAW + START_MARK; optional
// RECORD_DATA from a real mic driver. VOLUME/MUTE are server→client.
//
// A nil RecordDriver uses NullRecord (no PCM frames; silent guest mic).
// Open/runtime failures are best-effort: the session continues without capture.
//
// NullRecord default: acknowledges START but does not inject silence frames
// (avoids a timer loop). Real capture will call the channel SendPCM path later.
type RecordDriver interface {
	// Start configures capture (channels, AudioFmt*, Hz).
	Start(channels int, format uint16, frequency int)
	// Stop ends capture.
	Stop()
	// SetVolume sets per-channel capture gain (0..65535).
	SetVolume(volumes []uint16)
	// SetMute mutes or unmutes capture.
	SetMute(mute bool)
}

// Compile-time check: channel.NullRecord implements RecordDriver.
var _ RecordDriver = (*channel.NullRecord)(nil)

// NullRecord is a headless RecordDriver that never produces PCM.
// It is an alias of channel.NullRecord.
type NullRecord = channel.NullRecord

// NewNullRecord returns a NullRecord.
func NewNullRecord() *NullRecord {
	return channel.NewNullRecord()
}

// asRecordDriver adapts a public RecordDriver to channel.RecordDriver.
func asRecordDriver(d RecordDriver) channel.RecordDriver {
	if d == nil {
		return nil
	}
	if rd, ok := d.(channel.RecordDriver); ok {
		return rd
	}
	return recordDriverAdapter{d}
}

type recordDriverAdapter struct {
	d RecordDriver
}

func (a recordDriverAdapter) Start(channels int, format uint16, frequency int) {
	a.d.Start(channels, format, frequency)
}

func (a recordDriverAdapter) Stop() {
	a.d.Stop()
}

func (a recordDriverAdapter) SetVolume(volumes []uint16) {
	a.d.SetVolume(volumes)
}

func (a recordDriverAdapter) SetMute(mute bool) {
	a.d.SetMute(mute)
}
