// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"testing"

	"github.com/maskraven/spice-viewer/internal/channel"
	"github.com/maskraven/spice-viewer/internal/protocol"
)

func TestPhase1OpenPolicy(t *testing.T) {
	cases := []struct {
		typ   uint8
		open  bool
		fatal bool
	}{
		{protocol.ChannelDisplay, true, true},
		{protocol.ChannelInputs, true, true},
		{protocol.ChannelCursor, true, false},
		{protocol.ChannelMain, false, false},
		{protocol.ChannelPlayback, true, false}, // Phase 2 best-effort
		{protocol.ChannelRecord, true, false},   // Phase 3 best-effort
		{protocol.ChannelUSBRedir, true, false}, // Phase 3 best-effort
		{protocol.ChannelPort, false, false},    // not opened (no consumer)
		{protocol.ChannelWebDAV, true, false},   // Phase 3 best-effort
	}
	for _, tc := range cases {
		if got := channel.IsPhase1Open(tc.typ); got != tc.open {
			t.Errorf("type %d IsPhase1Open=%v want %v", tc.typ, got, tc.open)
		}
		if got := channel.IsFatalOpen(tc.typ); got != tc.fatal {
			t.Errorf("type %d IsFatalOpen=%v want %v", tc.typ, got, tc.fatal)
		}
	}
}
