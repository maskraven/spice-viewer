// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"image"
	"image/color"
	"sync"
)

// Surface is a thread-safe RGBA frame buffer implementing spice.DisplayDriver.
// The SPICE display path calls Present from a non-UI goroutine; the Fyne
// backend snapshots pixels on the UI thread.
//
// Pixel layout is RGBA8888. HiDPI scaling is left to the present path (best
// effort): this type stores guest-native pixels only.
type Surface struct {
	mu sync.Mutex

	width, height int
	stride        int
	pix           []byte // length height*width*4 when allocated

	// notify is invoked after Present / SetDesktopSize / Invalidate without
	// holding s.mu (may schedule UI work).
	notify func()

	PresentCount    int
	InvalidateCount int
	LastDirty       image.Rectangle
}

// NewSurface returns an empty Surface.
func NewSurface() *Surface {
	return &Surface{}
}

// SetNotify registers a callback invoked after frame updates (e.g. schedule
// a Fyne refresh). The callback is invoked without holding the surface lock.
func (s *Surface) SetNotify(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notify = fn
}

func (s *Surface) fireNotify() {
	s.mu.Lock()
	n := s.notify
	s.mu.Unlock()
	if n != nil {
		n()
	}
}

// SetDesktopSize implements spice.DisplayDriver.
func (s *Surface) SetDesktopSize(w, h int) {
	s.mu.Lock()
	s.width = w
	s.height = h
	if w > 0 && h > 0 {
		s.stride = w * 4
		s.pix = make([]byte, h*s.stride)
		for i := 3; i < len(s.pix); i += 4 {
			s.pix[i] = 0xff
		}
	} else {
		s.stride = 0
		s.pix = nil
	}
	s.mu.Unlock()
	s.fireNotify()
}

// Present implements spice.DisplayDriver.
func (s *Surface) Present(pix []byte, stride int, dirty image.Rectangle) {
	s.mu.Lock()
	s.PresentCount++
	s.LastDirty = dirty
	if stride <= 0 || s.height <= 0 || s.width <= 0 {
		s.mu.Unlock()
		s.fireNotify()
		return
	}
	dstStride := s.width * 4
	if s.pix == nil || len(s.pix) != s.height*dstStride {
		s.stride = dstStride
		s.pix = make([]byte, s.height*dstStride)
	}
	need := s.height * stride
	if len(pix) >= need {
		for y := 0; y < s.height; y++ {
			srcOff := y * stride
			dstOff := y * dstStride
			copy(s.pix[dstOff:dstOff+dstStride], pix[srcOff:srcOff+dstStride])
		}
	} else if len(pix) > 0 {
		copy(s.pix, pix)
	}
	s.stride = dstStride
	s.mu.Unlock()
	s.fireNotify()
}

// Invalidate implements spice.DisplayDriver.
func (s *Surface) Invalidate(region image.Rectangle) {
	s.mu.Lock()
	s.InvalidateCount++
	s.mu.Unlock()
	s.fireNotify()
	_ = region
}

// Size returns the current desktop dimensions.
func (s *Surface) Size() (w, h int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.width, s.height
}

// Snapshot returns a copy of the current frame as image.RGBA (nil if empty).
func (s *Surface) Snapshot() *image.RGBA {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.width <= 0 || s.height <= 0 || len(s.pix) == 0 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, s.width, s.height))
	dstStride := img.Stride
	srcStride := s.width * 4
	for y := 0; y < s.height; y++ {
		copy(img.Pix[y*dstStride:y*dstStride+srcStride], s.pix[y*srcStride:y*srcStride+srcStride])
	}
	return img
}

// At returns the color at guest pixel (x,y); used in tests.
func (s *Surface) At(x, y int) color.Color {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x < 0 || y < 0 || x >= s.width || y >= s.height || len(s.pix) == 0 {
		return color.RGBA{}
	}
	off := y*s.width*4 + x*4
	return color.RGBA{R: s.pix[off], G: s.pix[off+1], B: s.pix[off+2], A: s.pix[off+3]}
}
