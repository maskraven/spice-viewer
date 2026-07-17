// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0
//
// SPICE Quic image decoder (predictive Golomb/SFALIC-derived coding).
// Ported from the pure-Go implementation in github.com/Shells-com/spice/quic
// (MIT License, Copyright 2021 E Shells Inc.), which follows spice-common/common/quic.c.

package codec

func ceil_log2(val uint32) uint32 {
	if val == 1 {
		return 0
	}

	result := uint32(0)
	val -= 1
	for ; val > 0; val = val >> 1 {
		result++
	}

	return result
}
