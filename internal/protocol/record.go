// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/binary"
	"fmt"
)

// RecordStart is SpiceMsgRecordStart (SPICE_MSG_RECORD_START body).
//
// Wire (packed LE): channels u32 + format enum16 + frequency u32.
// Unlike PlaybackStart there is no multimedia time field.
type RecordStart struct {
	Channels  uint32
	Format    uint16 // AudioFmt*
	Frequency uint32 // samples per second per channel
}

// Encode serializes SpiceMsgRecordStart.
func (s RecordStart) Encode() []byte {
	buf := make([]byte, RecordStartSize)
	binary.LittleEndian.PutUint32(buf[0:4], s.Channels)
	binary.LittleEndian.PutUint16(buf[4:6], s.Format)
	binary.LittleEndian.PutUint32(buf[6:10], s.Frequency)
	return buf
}

// DecodeRecordStart parses a RECORD_START body.
func DecodeRecordStart(b []byte) (RecordStart, error) {
	if len(b) < RecordStartSize {
		return RecordStart{}, fmt.Errorf("spice: RECORD_START short: %d want %d",
			len(b), RecordStartSize)
	}
	return RecordStart{
		Channels:  binary.LittleEndian.Uint32(b[0:4]),
		Format:    binary.LittleEndian.Uint16(b[4:6]),
		Frequency: binary.LittleEndian.Uint32(b[6:10]),
	}, nil
}

// RecordMode is the client→server SPICE_MSGC_RECORD_MODE body.
//
// Wire matches PlaybackMode: time u32 + mode enum16 + optional mode-specific data.
type RecordMode = PlaybackMode

// DecodeRecordMode parses a client RECORD_MODE body (same layout as PLAYBACK_MODE).
func DecodeRecordMode(b []byte) (RecordMode, error) {
	return DecodePlaybackMode(b)
}

// EncodeRecordMode serializes a RECORD_MODE body.
func EncodeRecordMode(m RecordMode) []byte {
	return m.Encode()
}

// RecordData is the client→server SPICE_MSGC_RECORD_DATA body.
//
// Wire matches PlaybackData: time u32 + audio bytes.
type RecordData = PlaybackData

// DecodeRecordData parses a client RECORD_DATA body.
func DecodeRecordData(b []byte) (RecordData, error) {
	return DecodePlaybackData(b)
}

// EncodeRecordData serializes a RECORD_DATA body.
func EncodeRecordData(d RecordData) []byte {
	return d.Encode()
}

// EncodeRecordStartMark serializes SPICE_MSGC_RECORD_START_MARK (time u32).
func EncodeRecordStartMark(timeMs uint32) []byte {
	buf := make([]byte, RecordStartMarkSize)
	binary.LittleEndian.PutUint32(buf, timeMs)
	return buf
}

// DecodeRecordStartMark parses SPICE_MSGC_RECORD_START_MARK.
func DecodeRecordStartMark(b []byte) (uint32, error) {
	if len(b) < RecordStartMarkSize {
		return 0, fmt.Errorf("spice: RECORD_START_MARK short: %d want %d",
			len(b), RecordStartMarkSize)
	}
	return binary.LittleEndian.Uint32(b[:RecordStartMarkSize]), nil
}

// RecordVolume is SPICE_MSG_RECORD_VOLUME (same wire as PlaybackVolume).
type RecordVolume = PlaybackVolume

// DecodeRecordVolume parses a RECORD_VOLUME body.
func DecodeRecordVolume(b []byte) (RecordVolume, error) {
	return DecodePlaybackVolume(b)
}

// DecodeRecordMute parses SPICE_MSG_RECORD_MUTE (uint8 mute).
func DecodeRecordMute(b []byte) (bool, error) {
	if len(b) < RecordMuteSize {
		return false, fmt.Errorf("spice: RECORD_MUTE short: %d", len(b))
	}
	return b[0] != 0, nil
}

// EncodeRecordMute serializes a mute flag.
func EncodeRecordMute(mute bool) []byte {
	return EncodePlaybackMute(mute)
}
