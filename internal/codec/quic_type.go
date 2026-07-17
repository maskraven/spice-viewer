// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0
//
// SPICE Quic image decoder (predictive Golomb/SFALIC-derived coding).
// Ported from the pure-Go implementation in github.com/Shells-com/spice/quic
// (MIT License, Copyright 2021 E Shells Inc.), which follows spice-common/common/quic.c.

package codec

import "fmt"

type quicImageType uint32

const (
	quicImageTypeInvalid quicImageType = iota
	quicImageTypeGray
	quicImageTypeRGB16
	quicImageTypeRGB24
	quicImageTypeRGB32
	quicImageTypeRGBA
)

func (t quicImageType) bpc() uint32 {
	switch t {
	case quicImageTypeGray:
		return 8
	case quicImageTypeRGB16:
		return 5
	case quicImageTypeRGB24:
		return 8
	case quicImageTypeRGB32:
		return 8
	case quicImageTypeRGBA:
		return 8
	default:
		// invalid
		return 0
	}
}

func (t quicImageType) String() string {
	switch t {
	case quicImageTypeInvalid:
		return "INVALID"
	case quicImageTypeGray:
		return "GRAY"
	case quicImageTypeRGB16:
		return "RGB16"
	case quicImageTypeRGB24:
		return "RGB24"
	case quicImageTypeRGB32:
		return "RGB32"
	case quicImageTypeRGBA:
		return "RGBA"
	default:
		return fmt.Sprintf("quicImageType(%d)", t)
	}
}

func (t quicImageType) Stride() int {
	switch t {
	case quicImageTypeInvalid:
		return -1
	case quicImageTypeGray:
		return 2
	case quicImageTypeRGB16:
		return 2
	case quicImageTypeRGB24:
		return 4
	case quicImageTypeRGB32:
		return 4
	case quicImageTypeRGBA:
		return 4
	default:
		return -1
	}
}
