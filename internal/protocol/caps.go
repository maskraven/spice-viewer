// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

// CapBit returns a single capability word with bit n set (bit index n).
func CapBit(n uint) uint32 {
	return 1 << n
}

// CapsFromBits packs capability bit indices into a []uint32 word vector
// (one bit per capability, low bit of word 0 is capability 0).
func CapsFromBits(bits ...uint) []uint32 {
	if len(bits) == 0 {
		return nil
	}
	var max uint
	for _, b := range bits {
		if b > max {
			max = b
		}
	}
	words := make([]uint32, max/32+1)
	for _, b := range bits {
		words[b/32] |= 1 << (b % 32)
	}
	return words
}

// HasCap reports whether capability bit index n is set in the caps word vector.
func HasCap(caps []uint32, n uint) bool {
	word := n / 32
	if int(word) >= len(caps) {
		return false
	}
	return caps[word]&(1<<(n%32)) != 0
}

// Phase1CommonCaps is the Phase 1 client common-capability advertisement:
// Auth selection + AuthSpice + mini-header (no SASL).
func Phase1CommonCaps() []uint32 {
	return CapsFromBits(
		CommonCapProtocolAuthSelection,
		CommonCapAuthSpice,
		CommonCapMiniHeader,
	)
}

// DisplayChannelCaps returns the display channel capability vector.
// Always advertises sized streams, multi-codec, and MJPEG (stdlib JPEG).
// When h264Available is true, also sets DisplayCapCodecH264 so the server
// may emit H.264 streams; callers must pass h264.Available() (or equivalent)
// and must not advertise H.264 when the decoder backend is missing.
func DisplayChannelCaps(h264Available bool) []uint32 {
	bits := []uint{
		DisplayCapSizedStream,
		DisplayCapMultiCodec,
		DisplayCapCodecMJPEG,
	}
	if h264Available {
		bits = append(bits, DisplayCapCodecH264)
	}
	return CapsFromBits(bits...)
}

// IntersectCaps returns the bitwise AND of two cap vectors (word-wise),
// truncated to the shorter length.
func IntersectCaps(a, b []uint32) []uint32 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return nil
	}
	out := make([]uint32, n)
	for i := 0; i < n; i++ {
		out[i] = a[i] & b[i]
	}
	return out
}
