// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice

import (
	"fmt"
	"strings"

	"github.com/maskraven/virt-viewer/internal/protocol"
)

// PerformanceProfile is a product-level SPICE display preference bundle.
//
// The protocol has no first-class “profile”; these map to
// SPICE_MSGC_DISPLAY_PREFERRED_COMPRESSION and
// SPICE_MSGC_DISPLAY_PREFERRED_VIDEO_CODEC_TYPE (server may ignore if
// QEMU pins image-compression / wan options).
type PerformanceProfile int

const (
	// ProfileDefault is auto_glz stills + H.264 (if available) then MJPEG streams.
	ProfileDefault PerformanceProfile = iota
	// ProfileLAN prefers lower CPU / lower latency (auto_lz, then LZ).
	ProfileLAN
	// ProfileWAN prefers bandwidth savings (auto_glz / GLZ) + efficient video.
	ProfileWAN
	// ProfileQuality prefers lossless-ish stills (off / quic) over ratio.
	ProfileQuality
)

// ParsePerformanceProfile parses "default", "lan", "wan", "quality" (case-insensitive).
func ParsePerformanceProfile(s string) (PerformanceProfile, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "default", "auto":
		return ProfileDefault, nil
	case "lan", "local", "performance", "fast":
		return ProfileLAN, nil
	case "wan", "bandwidth", "low-bandwidth":
		return ProfileWAN, nil
	case "quality", "lossless", "hq":
		return ProfileQuality, nil
	default:
		return ProfileDefault, fmt.Errorf("spice: unknown performance profile %q (want default|lan|wan|quality)", s)
	}
}

// String returns a stable CLI/UI name.
func (p PerformanceProfile) String() string {
	switch p {
	case ProfileLAN:
		return "lan"
	case ProfileWAN:
		return "wan"
	case ProfileQuality:
		return "quality"
	default:
		return "default"
	}
}

// Label is a short human label for menus.
func (p PerformanceProfile) Label() string {
	switch p {
	case ProfileLAN:
		return "LAN (low latency)"
	case ProfileWAN:
		return "WAN (bandwidth)"
	case ProfileQuality:
		return "Quality (lossless-ish)"
	default:
		return "Default (auto)"
	}
}

// ImageCompression is the SpiceImageCompression preference for this profile.
func (p PerformanceProfile) ImageCompression() uint8 {
	switch p {
	case ProfileLAN:
		return protocol.ImageCompressionAutoLZ
	case ProfileWAN:
		return protocol.ImageCompressionAutoGLZ
	case ProfileQuality:
		return protocol.ImageCompressionOff
	default:
		return protocol.ImageCompressionAutoGLZ
	}
}

// VideoCodecs returns preferred video codec types in order (first = best).
// h264Available gates H.264 (must match display channel caps).
func (p PerformanceProfile) VideoCodecs(h264Available bool) []uint8 {
	switch p {
	case ProfileLAN:
		// Prefer light streams when available; MJPEG always ok.
		if h264Available {
			return []uint8{protocol.VideoCodecH264, protocol.VideoCodecMJPEG}
		}
		return []uint8{protocol.VideoCodecMJPEG}
	case ProfileWAN:
		if h264Available {
			return []uint8{protocol.VideoCodecH264, protocol.VideoCodecMJPEG}
		}
		return []uint8{protocol.VideoCodecMJPEG}
	case ProfileQuality:
		// Still prefer efficient stream if server streams; MJPEG fallback.
		if h264Available {
			return []uint8{protocol.VideoCodecMJPEG, protocol.VideoCodecH264}
		}
		return []uint8{protocol.VideoCodecMJPEG}
	default:
		if h264Available {
			return []uint8{protocol.VideoCodecH264, protocol.VideoCodecMJPEG}
		}
		return []uint8{protocol.VideoCodecMJPEG}
	}
}
