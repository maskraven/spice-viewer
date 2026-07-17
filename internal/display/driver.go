// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package display

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"sync"
)

// Driver is the frame sink implemented by UI backends and headless tests.
//
// Pixel buffers passed to Present are RGBA8888 with the given stride (bytes per
// row). Dirty is the damaged region in surface coordinates; it may be empty to
// mean "full surface".
type Driver interface {
	// SetDesktopSize notifies the driver of primary surface dimensions.
	SetDesktopSize(w, h int)
	// Present delivers surface pixels (typically the full buffer) with a dirty region.
	Present(pix []byte, stride int, dirty image.Rectangle)
	// Invalidate marks a region dirty without necessarily supplying new pixels
	// (drivers may re-blit from their last Present buffer).
	Invalidate(region image.Rectangle)
}

// NullDriver is a headless Driver that retains the last presented frame and
// exposes a deterministic content hash for tests.
type NullDriver struct {
	mu sync.Mutex

	Width, Height int
	Stride        int
	Pix           []byte // copy of last Present buffer

	PresentCount    int
	InvalidateCount int
	LastDirty       image.Rectangle
	LastInvalidate  image.Rectangle
}

// NewNullDriver returns an empty NullDriver.
func NewNullDriver() *NullDriver {
	return &NullDriver{}
}

// SetDesktopSize implements Driver.
func (n *NullDriver) SetDesktopSize(w, h int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Width = w
	n.Height = h
	if w > 0 && h > 0 {
		n.Stride = w * 4
		n.Pix = make([]byte, h*n.Stride)
		// Opaque black
		for i := 3; i < len(n.Pix); i += 4 {
			n.Pix[i] = 0xff
		}
	} else {
		n.Stride = 0
		n.Pix = nil
	}
}

// Present implements Driver. Copies pix into the retained buffer (full surface).
func (n *NullDriver) Present(pix []byte, stride int, dirty image.Rectangle) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.PresentCount++
	n.LastDirty = dirty
	if stride <= 0 || n.Height <= 0 || n.Width <= 0 {
		return
	}
	need := n.Height * stride
	if len(pix) < need {
		// Partial buffer: copy what we can into existing storage.
		if n.Pix == nil {
			return
		}
		copy(n.Pix, pix)
		return
	}
	if n.Pix == nil || len(n.Pix) != n.Height*n.Width*4 || n.Stride != n.Width*4 {
		n.Stride = n.Width * 4
		n.Pix = make([]byte, n.Height*n.Stride)
	}
	// Copy rows, compacting to width*4 if source stride is larger.
	dstStride := n.Width * 4
	for y := 0; y < n.Height; y++ {
		srcOff := y * stride
		dstOff := y * dstStride
		row := pix[srcOff : srcOff+dstStride]
		copy(n.Pix[dstOff:dstOff+dstStride], row)
	}
	n.Stride = dstStride
}

// Invalidate implements Driver.
func (n *NullDriver) Invalidate(region image.Rectangle) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.InvalidateCount++
	n.LastInvalidate = region
}

// Hash returns the SHA-256 hex digest of the retained pixel buffer
// (empty string if no buffer). Deterministic for identical draw sequences.
func (n *NullDriver) Hash() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.Pix) == 0 {
		return ""
	}
	sum := sha256.Sum256(n.Pix)
	return hex.EncodeToString(sum[:])
}

// Snapshot returns a copy of the retained RGBA buffer and geometry.
func (n *NullDriver) Snapshot() (pix []byte, w, h, stride int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.Pix == nil {
		return nil, n.Width, n.Height, n.Stride
	}
	out := make([]byte, len(n.Pix))
	copy(out, n.Pix)
	return out, n.Width, n.Height, n.Stride
}
