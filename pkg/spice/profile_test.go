// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice_test

import (
	"testing"

	"github.com/maskraven/virt-viewer/internal/protocol"
	"github.com/maskraven/virt-viewer/pkg/spice"
)

func TestParsePerformanceProfile(t *testing.T) {
	cases := map[string]spice.PerformanceProfile{
		"":        spice.ProfileDefault,
		"default": spice.ProfileDefault,
		"LAN":     spice.ProfileLAN,
		"wan":     spice.ProfileWAN,
		"quality": spice.ProfileQuality,
	}
	for in, want := range cases {
		got, err := spice.ParsePerformanceProfile(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q: got %v want %v", in, got, want)
		}
	}
	if _, err := spice.ParsePerformanceProfile("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestProfileImageCompression(t *testing.T) {
	if spice.ProfileLAN.ImageCompression() != protocol.ImageCompressionAutoLZ {
		t.Fatal("LAN")
	}
	if spice.ProfileWAN.ImageCompression() != protocol.ImageCompressionAutoGLZ {
		t.Fatal("WAN")
	}
	if spice.ProfileQuality.ImageCompression() != protocol.ImageCompressionOff {
		t.Fatal("quality")
	}
	if spice.ProfileDefault.ImageCompression() != protocol.ImageCompressionAutoGLZ {
		t.Fatal("default")
	}
}

func TestProfileVideoCodecs(t *testing.T) {
	with := spice.ProfileWAN.VideoCodecs(true)
	if len(with) < 1 || with[0] != protocol.VideoCodecH264 {
		t.Fatalf("want H264 first when available: %v", with)
	}
	without := spice.ProfileWAN.VideoCodecs(false)
	if len(without) != 1 || without[0] != protocol.VideoCodecMJPEG {
		t.Fatalf("want MJPEG only: %v", without)
	}
}
