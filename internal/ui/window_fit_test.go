// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import "testing"

func TestFitContentSize_PreservesRatio(t *testing.T) {
	// 1920×1080 → max 1280×800 → should be 1280×720 (16:9).
	w, h := fitContentSize(1920, 1080, 1280, 800, 640, 400)
	if w != 1280 {
		t.Fatalf("w = %v; want 1280", w)
	}
	ratio := w / h
	want := float32(1920) / 1080
	if diff := ratio - want; diff > 0.01 || diff < -0.01 {
		t.Fatalf("ratio %v want %v (size %vx%v)", ratio, want, w, h)
	}
	if h > 800 {
		t.Fatalf("h %v exceeds max", h)
	}
}

func TestFitContentSize_Portrait(t *testing.T) {
	w, h := fitContentSize(1080, 1920, 1280, 800, 640, 400)
	if h != 800 {
		t.Fatalf("h = %v; want 800", h)
	}
	ratio := w / h
	want := float32(1080) / 1920
	if diff := ratio - want; diff > 0.01 || diff < -0.01 {
		t.Fatalf("ratio %v want %v", ratio, want)
	}
}

func TestFitContentSize_SmallGuest(t *testing.T) {
	w, h := fitContentSize(320, 240, 1280, 800, 640, 400)
	// Scale up toward min while keeping 4:3 (320:240 = 4:3 → 640×480).
	if w < 639 || w > 641 {
		t.Fatalf("w %v want ~640", w)
	}
	ratio := w / h
	want := float32(320) / 240
	if diff := ratio - want; diff > 0.02 || diff < -0.02 {
		t.Fatalf("ratio %v want %v (%vx%v)", ratio, want, w, h)
	}
}

func TestFitContentSize_Invalid(t *testing.T) {
	w, h := fitContentSize(0, 0, 1280, 800, 640, 400)
	if w != 640 || h != 400 {
		t.Fatalf("got %vx%v", w, h)
	}
}
