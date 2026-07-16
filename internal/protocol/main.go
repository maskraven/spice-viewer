// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/binary"
	"fmt"
)

// MainInit is SpiceMsgMainInit (SPICE_MSG_MAIN_INIT body).
//
// Wire layout (little-endian, packed): 8 × uint32 = 32 bytes.
//
//	session_id, display_channels_hint, supported_mouse_modes, current_mouse_mode,
//	agent_connected, agent_tokens, multi_media_time, ram_hint
//
// spice-protocol @ 499cc8326a672e9e5747efc017319b19e1594b42.
type MainInit struct {
	SessionID           uint32
	DisplayChannelsHint uint32
	SupportedMouseModes uint32
	CurrentMouseMode    uint32
	AgentConnected      uint32
	AgentTokens         uint32
	MultiMediaTime      uint32
	RamHint             uint32
}

// Encode serializes SpiceMsgMainInit.
func (m MainInit) Encode() []byte {
	buf := make([]byte, MainInitSize)
	binary.LittleEndian.PutUint32(buf[0:4], m.SessionID)
	binary.LittleEndian.PutUint32(buf[4:8], m.DisplayChannelsHint)
	binary.LittleEndian.PutUint32(buf[8:12], m.SupportedMouseModes)
	binary.LittleEndian.PutUint32(buf[12:16], m.CurrentMouseMode)
	binary.LittleEndian.PutUint32(buf[16:20], m.AgentConnected)
	binary.LittleEndian.PutUint32(buf[20:24], m.AgentTokens)
	binary.LittleEndian.PutUint32(buf[24:28], m.MultiMediaTime)
	binary.LittleEndian.PutUint32(buf[28:32], m.RamHint)
	return buf
}

// DecodeMainInit parses a SpiceMsgMainInit body.
func DecodeMainInit(b []byte) (MainInit, error) {
	if len(b) < MainInitSize {
		return MainInit{}, fmt.Errorf("spice: MAIN_INIT short: %d want %d", len(b), MainInitSize)
	}
	return MainInit{
		SessionID:           binary.LittleEndian.Uint32(b[0:4]),
		DisplayChannelsHint: binary.LittleEndian.Uint32(b[4:8]),
		SupportedMouseModes: binary.LittleEndian.Uint32(b[8:12]),
		CurrentMouseMode:    binary.LittleEndian.Uint32(b[12:16]),
		AgentConnected:      binary.LittleEndian.Uint32(b[16:20]),
		AgentTokens:         binary.LittleEndian.Uint32(b[20:24]),
		MultiMediaTime:      binary.LittleEndian.Uint32(b[24:28]),
		RamHint:             binary.LittleEndian.Uint32(b[28:32]),
	}, nil
}

// ChannelID is SpiceChannelId: channel type + instance id (2 packed bytes).
type ChannelID struct {
	Type uint8
	ID   uint8
}

// ChannelsList is SpiceMsgChannels (SPICE_MSG_MAIN_CHANNELS_LIST body).
//
//	uint32 num_of_channels
//	SpiceChannelId channels[num]  // each type u8 + id u8, packed
type ChannelsList struct {
	Channels []ChannelID
}

// Encode serializes SpiceMsgChannels.
func (c ChannelsList) Encode() []byte {
	n := len(c.Channels)
	buf := make([]byte, 4+2*n)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(n))
	off := 4
	for _, ch := range c.Channels {
		buf[off] = ch.Type
		buf[off+1] = ch.ID
		off += 2
	}
	return buf
}

// DecodeChannelsList parses a SpiceMsgChannels body.
func DecodeChannelsList(b []byte) (ChannelsList, error) {
	if len(b) < 4 {
		return ChannelsList{}, fmt.Errorf("spice: CHANNELS_LIST short: %d", len(b))
	}
	n := binary.LittleEndian.Uint32(b[0:4])
	need := 4 + 2*int(n)
	if len(b) < need {
		return ChannelsList{}, fmt.Errorf("spice: CHANNELS_LIST truncated: num=%d body=%d need=%d",
			n, len(b), need)
	}
	// Guard absurd channel counts (DoS).
	if n > 256 {
		return ChannelsList{}, fmt.Errorf("spice: CHANNELS_LIST num_of_channels %d too large", n)
	}
	out := make([]ChannelID, n)
	off := 4
	for i := uint32(0); i < n; i++ {
		out[i] = ChannelID{Type: b[off], ID: b[off+1]}
		off += 2
	}
	return ChannelsList{Channels: out}, nil
}
