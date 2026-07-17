// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"encoding/binary"
	"testing"

	"github.com/maskraven/spice-viewer/internal/protocol"
)

func TestEncodePreferredCompression(t *testing.T) {
	b := protocol.EncodePreferredCompression(protocol.ImageCompressionAutoLZ)
	if len(b) != 1 || b[0] != protocol.ImageCompressionAutoLZ {
		t.Fatalf("body = %v", b)
	}
}

func TestEncodePreferredVideoCodecType(t *testing.T) {
	if protocol.EncodePreferredVideoCodecType(nil) != nil {
		t.Fatal("empty should be nil")
	}
	b := protocol.EncodePreferredVideoCodecType([]uint8{
		protocol.VideoCodecH264,
		protocol.VideoCodecMJPEG,
	})
	if len(b) != 6 {
		t.Fatalf("len = %d", len(b))
	}
	if n := binary.LittleEndian.Uint32(b[0:4]); n != 2 {
		t.Fatalf("num = %d", n)
	}
	if b[4] != protocol.VideoCodecH264 || b[5] != protocol.VideoCodecMJPEG {
		t.Fatalf("codecs = %v", b[4:])
	}
}
