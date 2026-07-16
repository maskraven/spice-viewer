// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"image"
	"image/color"
	"testing"

	"github.com/maskraven/virt-viewer/pkg/spice"
)

func TestSurface_ImplementsDisplayDriver(t *testing.T) {
	var _ spice.DisplayDriver = (*Surface)(nil)
}

func TestSurface_Present(t *testing.T) {
	s := NewSurface()
	notified := 0
	s.SetNotify(func() { notified++ })

	s.SetDesktopSize(2, 2)
	if notified != 1 {
		t.Fatalf("notify after size: %d", notified)
	}

	// Red pixel at (0,0), green at (1,0), blue at (0,1), white at (1,1).
	pix := []byte{
		255, 0, 0, 255, 0, 255, 0, 255,
		0, 0, 255, 255, 255, 255, 255, 255,
	}
	s.Present(pix, 8, image.Rect(0, 0, 2, 2))
	if s.PresentCount != 1 {
		t.Fatalf("PresentCount=%d", s.PresentCount)
	}
	if notified != 2 {
		t.Fatalf("notify after present: %d", notified)
	}

	if c := s.At(0, 0).(color.RGBA); c.R != 255 || c.G != 0 {
		t.Errorf("pixel00 = %+v", c)
	}
	if c := s.At(1, 1).(color.RGBA); c.R != 255 || c.A != 255 {
		t.Errorf("pixel11 = %+v", c)
	}

	snap := s.Snapshot()
	if snap == nil || snap.Bounds().Dx() != 2 || snap.Bounds().Dy() != 2 {
		t.Fatalf("snapshot bounds %v", snap)
	}

	s.Invalidate(image.Rect(0, 0, 1, 1))
	if s.InvalidateCount != 1 {
		t.Fatalf("InvalidateCount=%d", s.InvalidateCount)
	}
}

func TestSurface_PresentStride(t *testing.T) {
	s := NewSurface()
	s.SetDesktopSize(1, 1)
	// stride 8 with 4 padding bytes.
	pix := []byte{10, 20, 30, 255, 0, 0, 0, 0}
	s.Present(pix, 8, image.Rectangle{})
	c := s.At(0, 0).(color.RGBA)
	if c.R != 10 || c.G != 20 || c.B != 30 {
		t.Fatalf("got %+v", c)
	}
}
