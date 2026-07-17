// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice

import (
	"image"

	"github.com/maskraven/spice-viewer/internal/display"
)

// DisplayDriver receives composited frames from the SPICE session.
//
// UI backends implement this interface. For headless tests and CI, use
// NullDriver which retains pixels and exposes a content hash.
//
// Pixel layout is RGBA8888. Present supplies the surface buffer (stride is
// bytes per row). Dirty is the damaged region; Invalidate marks a region
// without necessarily attaching new pixels.
type DisplayDriver interface {
	// SetDesktopSize notifies the driver of primary surface dimensions.
	SetDesktopSize(w, h int)
	// Present delivers surface pixels with a dirty region.
	Present(pix []byte, stride int, dirty image.Rectangle)
	// Invalidate marks a region dirty (re-blit from last Present buffer).
	Invalidate(region image.Rectangle)
}

// Compile-time check: NullDriver implements DisplayDriver.
var _ DisplayDriver = (*NullDriver)(nil)

// NullDriver is a headless DisplayDriver for deterministic frame verification.
// It is an alias of display.NullDriver.
type NullDriver = display.NullDriver

// NewNullDriver returns a NullDriver suitable as a DisplayDriver.
func NewNullDriver() *NullDriver {
	return display.NewNullDriver()
}

// AsDriver returns d as the internal display.Driver interface (same method set).
// Useful when constructing a display.Compositor from a public DisplayDriver.
func AsDriver(d DisplayDriver) display.Driver {
	if d == nil {
		return nil
	}
	return displayDriverAdapter{d}
}

type displayDriverAdapter struct {
	d DisplayDriver
}

func (a displayDriverAdapter) SetDesktopSize(w, h int) {
	a.d.SetDesktopSize(w, h)
}

func (a displayDriverAdapter) Present(pix []byte, stride int, dirty image.Rectangle) {
	a.d.Present(pix, stride, dirty)
}

func (a displayDriverAdapter) Invalidate(region image.Rectangle) {
	a.d.Invalidate(region)
}
