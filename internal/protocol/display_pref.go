// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

import "encoding/binary"

// EncodePreferredCompression builds SPICE_MSGC_DISPLAY_PREFERRED_COMPRESSION
// (one uint8 SpiceImageCompression).
func EncodePreferredCompression(imageCompression uint8) []byte {
	return []byte{imageCompression}
}

// EncodePreferredVideoCodecType builds
// SPICE_MSGC_DISPLAY_PREFERRED_VIDEO_CODEC_TYPE:
//
//	uint32 num_of_codecs (LE) + codecs[num] as uint8 SpiceVideoCodecType
//
// codecs is preference order (first = most preferred). Empty returns nil body
// (caller should skip the message).
func EncodePreferredVideoCodecType(codecs []uint8) []byte {
	if len(codecs) == 0 {
		return nil
	}
	buf := make([]byte, 4+len(codecs))
	binary.LittleEndian.PutUint32(buf[0:4], uint32(len(codecs)))
	copy(buf[4:], codecs)
	return buf
}
