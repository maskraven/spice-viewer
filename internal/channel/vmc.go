// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel

import (
	"fmt"
	"net"

	"github.com/maskraven/spice-viewer/internal/protocol"
)

// VMCHandler is an optional sink for SpiceVMC DATA payloads (usbredir / port / webdav).
// Implementations must not retain data without copying.
type VMCHandler interface {
	// OnVMCData is called with a copy of the opaque payload (may be empty).
	OnVMCData(payload []byte)
}

// NullVMCHandler discards VMC payloads (default scaffold).
type NullVMCHandler struct {
	Count int
	Bytes int
	Last  []byte
}

// OnVMCData implements VMCHandler.
func (n *NullVMCHandler) OnVMCData(payload []byte) {
	if n == nil {
		return
	}
	n.Count++
	n.Bytes += len(payload)
	if len(payload) > 0 {
		n.Last = append([]byte(nil), payload...)
	} else {
		n.Last = nil
	}
}

// Compile-time check.
var _ VMCHandler = (*NullVMCHandler)(nil)

// ParseVMCData extracts the opaque payload from a SPICE_MSG_SPICEVMC_DATA body.
func ParseVMCData(body []byte) []byte {
	return protocol.DecodeSpiceVMCData(body).Data
}

// ParseVMCCompressed extracts a compressed VMC frame.
// Scaffold does not decompress LZ4; callers typically log and discard.
func ParseVMCCompressed(body []byte) (protocol.SpiceVMCCompressedData, error) {
	return protocol.DecodeSpiceVMCCompressedData(body)
}

// SendVMCData writes SPICE_MSGC_SPICEVMC_DATA with the given opaque payload.
func SendVMCData(conn net.Conn, payload []byte) error {
	if conn == nil {
		return fmt.Errorf("channel: vmc: nil conn")
	}
	return protocol.WriteMessage(conn, protocol.MsgcSpiceVMCData, payload)
}

// IsVMCMessage reports whether typ is a SpiceVMC server message (data / compressed).
func IsVMCMessage(typ uint16) bool {
	switch typ {
	case protocol.MsgSpiceVMCData, protocol.MsgSpiceVMCCompressedData:
		return true
	default:
		return false
	}
}
