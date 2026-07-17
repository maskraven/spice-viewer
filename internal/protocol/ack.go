// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/binary"
	"io"
	"sync"
)

// AckState tracks SPICE per-channel message ACK windows as implemented by
// spice-gtk (spice-channel.c / channel-base.c):
//
//  1. On SPICE_MSG_SET_ACK: reply SPICE_MSGC_ACK_SYNC(generation) and set
//     count = window = ack.window.
//  2. After each subsequent received message: if count > 0, count--. When
//     count hits 0, send SPICE_MSGC_ACK (empty body) and reset count = window.
//
// Without (2), the server stops sending after `window` messages — the display
// freezes while inputs may still appear to work.
type AckState struct {
	mu     sync.Mutex
	window uint32
	count  uint32
}

// OnSetAck handles SPICE_MSG_SET_ACK body (generation u32, window u32).
// Writes ACK_SYNC(generation) to w.
func (a *AckState) OnSetAck(w io.Writer, data []byte) error {
	if a == nil || w == nil || len(data) < 8 {
		return nil
	}
	gen := binary.LittleEndian.Uint32(data[0:4])
	window := binary.LittleEndian.Uint32(data[4:8])
	a.mu.Lock()
	a.window = window
	a.count = window
	a.mu.Unlock()

	var body [4]byte
	binary.LittleEndian.PutUint32(body[:], gen)
	return WriteMessage(w, MsgcAckSync, body[:])
}

// AfterRead must be called once per successfully read channel message.
// spice-gtk counts before dispatching the message handler.
func (a *AckState) AfterRead(w io.Writer) error {
	if a == nil || w == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// Acks disabled until SET_ACK (window stays 0).
	if a.window == 0 || a.count == 0 {
		return nil
	}
	a.count--
	if a.count == 0 {
		a.count = a.window
		return WriteMessage(w, MsgcAck, nil)
	}
	return nil
}
