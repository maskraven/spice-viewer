// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/binary"
	"fmt"
)

// PlaybackMode is SpiceMsgPlaybackMode (SPICE_MSG_PLAYBACK_MODE body).
//
// Wire (packed LE): time u32 + mode enum16 + optional mode-specific data.
type PlaybackMode struct {
	Time uint32
	Mode uint16 // AudioDataMode*
	Data []byte // mode-specific; usually empty for RAW
}

// Encode serializes SpiceMsgPlaybackMode.
func (m PlaybackMode) Encode() []byte {
	buf := make([]byte, PlaybackModeFixedSize+len(m.Data))
	binary.LittleEndian.PutUint32(buf[0:4], m.Time)
	binary.LittleEndian.PutUint16(buf[4:6], m.Mode)
	copy(buf[PlaybackModeFixedSize:], m.Data)
	return buf
}

// DecodePlaybackMode parses a PLAYBACK_MODE body.
func DecodePlaybackMode(b []byte) (PlaybackMode, error) {
	if len(b) < PlaybackModeFixedSize {
		return PlaybackMode{}, fmt.Errorf("spice: PLAYBACK_MODE short: %d want >= %d",
			len(b), PlaybackModeFixedSize)
	}
	out := PlaybackMode{
		Time: binary.LittleEndian.Uint32(b[0:4]),
		Mode: binary.LittleEndian.Uint16(b[4:6]),
	}
	if len(b) > PlaybackModeFixedSize {
		out.Data = append([]byte(nil), b[PlaybackModeFixedSize:]...)
	}
	return out, nil
}

// PlaybackStart is SpiceMsgPlaybackStart (SPICE_MSG_PLAYBACK_START body).
//
// Wire (packed LE): channels u32 + format enum16 + frequency u32 + time u32.
type PlaybackStart struct {
	Channels  uint32
	Format    uint16 // AudioFmt*
	Frequency uint32 // samples per second per channel
	Time      uint32 // multimedia time stamp
}

// Encode serializes SpiceMsgPlaybackStart.
func (s PlaybackStart) Encode() []byte {
	buf := make([]byte, PlaybackStartSize)
	binary.LittleEndian.PutUint32(buf[0:4], s.Channels)
	binary.LittleEndian.PutUint16(buf[4:6], s.Format)
	binary.LittleEndian.PutUint32(buf[6:10], s.Frequency)
	binary.LittleEndian.PutUint32(buf[10:14], s.Time)
	return buf
}

// DecodePlaybackStart parses a PLAYBACK_START body.
func DecodePlaybackStart(b []byte) (PlaybackStart, error) {
	if len(b) < PlaybackStartSize {
		return PlaybackStart{}, fmt.Errorf("spice: PLAYBACK_START short: %d want %d",
			len(b), PlaybackStartSize)
	}
	return PlaybackStart{
		Channels:  binary.LittleEndian.Uint32(b[0:4]),
		Format:    binary.LittleEndian.Uint16(b[4:6]),
		Frequency: binary.LittleEndian.Uint32(b[6:10]),
		Time:      binary.LittleEndian.Uint32(b[10:14]),
	}, nil
}

// PlaybackData is SpiceMsgPlaybackPacket (SPICE_MSG_PLAYBACK_DATA body).
//
// Wire (packed LE): time u32 + raw/compressed audio bytes.
// For AudioDataModeRaw + AudioFmtS16 the payload is interleaved signed 16-bit LE PCM.
type PlaybackData struct {
	Time uint32
	Data []byte
}

// Encode serializes SpiceMsgPlaybackPacket.
func (d PlaybackData) Encode() []byte {
	buf := make([]byte, PlaybackDataHeaderSize+len(d.Data))
	binary.LittleEndian.PutUint32(buf[0:4], d.Time)
	copy(buf[PlaybackDataHeaderSize:], d.Data)
	return buf
}

// DecodePlaybackData parses a PLAYBACK_DATA body.
// Data is a sub-slice of b (caller must not retain beyond b's lifetime without copy).
func DecodePlaybackData(b []byte) (PlaybackData, error) {
	if len(b) < PlaybackDataHeaderSize {
		return PlaybackData{}, fmt.Errorf("spice: PLAYBACK_DATA short: %d want >= %d",
			len(b), PlaybackDataHeaderSize)
	}
	return PlaybackData{
		Time: binary.LittleEndian.Uint32(b[0:4]),
		Data: b[PlaybackDataHeaderSize:],
	}, nil
}

// PlaybackVolume is SpiceMsgAudioVolume (SPICE_MSG_PLAYBACK_VOLUME body).
//
// Wire: nchannels u8 + volume[nchannels] u16 LE.
// Volume scale is 0..65535 (full scale).
type PlaybackVolume struct {
	Volumes []uint16
}

// Encode serializes SpiceMsgAudioVolume.
func (v PlaybackVolume) Encode() []byte {
	n := len(v.Volumes)
	if n > 255 {
		n = 255
	}
	buf := make([]byte, 1+2*n)
	buf[0] = uint8(n)
	off := 1
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(buf[off:off+2], v.Volumes[i])
		off += 2
	}
	return buf
}

// DecodePlaybackVolume parses a PLAYBACK_VOLUME body.
func DecodePlaybackVolume(b []byte) (PlaybackVolume, error) {
	if len(b) < PlaybackVolumeMinSize {
		return PlaybackVolume{}, fmt.Errorf("spice: PLAYBACK_VOLUME short: %d", len(b))
	}
	n := int(b[0])
	need := 1 + 2*n
	if len(b) < need {
		return PlaybackVolume{}, fmt.Errorf("spice: PLAYBACK_VOLUME truncated: n=%d body=%d need=%d",
			n, len(b), need)
	}
	if n == 0 {
		return PlaybackVolume{}, nil
	}
	vols := make([]uint16, n)
	off := 1
	for i := 0; i < n; i++ {
		vols[i] = binary.LittleEndian.Uint16(b[off : off+2])
		off += 2
	}
	return PlaybackVolume{Volumes: vols}, nil
}

// DecodePlaybackMute parses SPICE_MSG_PLAYBACK_MUTE (uint8 mute).
func DecodePlaybackMute(b []byte) (bool, error) {
	if len(b) < PlaybackMuteSize {
		return false, fmt.Errorf("spice: PLAYBACK_MUTE short: %d", len(b))
	}
	return b[0] != 0, nil
}

// EncodePlaybackMute serializes a mute flag.
func EncodePlaybackMute(mute bool) []byte {
	if mute {
		return []byte{1}
	}
	return []byte{0}
}

// DecodePlaybackLatency parses SPICE_MSG_PLAYBACK_LATENCY (uint32 latency_ms).
func DecodePlaybackLatency(b []byte) (uint32, error) {
	if len(b) < PlaybackLatencySize {
		return 0, fmt.Errorf("spice: PLAYBACK_LATENCY short: %d want %d", len(b), PlaybackLatencySize)
	}
	return binary.LittleEndian.Uint32(b[:PlaybackLatencySize]), nil
}

// EncodePlaybackLatency serializes latency_ms.
func EncodePlaybackLatency(ms uint32) []byte {
	buf := make([]byte, PlaybackLatencySize)
	binary.LittleEndian.PutUint32(buf, ms)
	return buf
}
