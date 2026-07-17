// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package webdav

import (
	"encoding/binary"
	"fmt"
)

// ClientFrame is a minimal client_id-prefixed payload used by some SPICE WebDAV
// mux implementations. Layout is not fully standardized across versions; this
// helper only peels a leading little-endian u32 client id when present.
//
// Full phodav protocol support is out of scope for the Phase 3 scaffold.
type ClientFrame struct {
	ClientID uint32
	Payload  []byte
}

// MinClientFrameSize is client_id u32.
const MinClientFrameSize = 4

// ParseClientFrame peels a leading LE uint32 client_id from b.
// Payload is a sub-slice of b (copy if retained).
func ParseClientFrame(b []byte) (ClientFrame, error) {
	if len(b) < MinClientFrameSize {
		return ClientFrame{}, fmt.Errorf("webdav: client frame short: %d", len(b))
	}
	return ClientFrame{
		ClientID: binary.LittleEndian.Uint32(b[0:4]),
		Payload:  b[4:],
	}, nil
}

// EncodeClientFrame serializes ClientID + Payload.
func EncodeClientFrame(f ClientFrame) []byte {
	buf := make([]byte, MinClientFrameSize+len(f.Payload))
	binary.LittleEndian.PutUint32(buf[0:4], f.ClientID)
	copy(buf[4:], f.Payload)
	return buf
}

// ShareRootHook is an optional callback surface for a future share backend.
// The channel layer may hold a path string; richer FS access lands later.
type ShareRootHook interface {
	// Root returns the host directory being shared (empty if none).
	Root() string
}

// StaticShareRoot implements ShareRootHook with a fixed path.
type StaticShareRoot struct {
	Path string
}

// Root implements ShareRootHook.
func (s StaticShareRoot) Root() string { return s.Path }
