// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// guestView is a Fyne widget that displays the SPICE surface with best-effort
// HiDPI presentation: guest-native pixels are stored in Surface; Fyne scales
// them to the widget size using the canvas scale factor for the display.
type guestView struct {
	widget.BaseWidget

	surface *Surface
	img     *canvas.Image

	// scaleMilli is the last known canvas scale * 1000 (HiDPI hint).
	scaleMilli atomic.Uint32

	minW, minH float32
}

func newGuestView(surface *Surface) *guestView {
	img := canvas.NewImageFromImage(nil)
	img.FillMode = canvas.ImageFillContain
	// Smooth scaling looks better on HiDPI when the window is larger than guest.
	img.ScaleMode = canvas.ImageScaleSmooth
	img.SetMinSize(fyne.NewSize(320, 200))

	g := &guestView{
		surface: surface,
		img:     img,
		minW:    320,
		minH:    200,
	}
	g.ExtendBaseWidget(g)

	surface.SetNotify(func() {
		// Present arrives off the UI thread; schedule refresh on main.
		g.refreshFromSurface()
	})
	return g
}

// refreshFromSurface copies the latest frame onto the canvas image.
func (g *guestView) refreshFromSurface() {
	apply := func() {
		snap := g.surface.Snapshot()
		if snap == nil {
			return
		}
		// Replace the image object so Fyne always re-uploads texture pixels.
		// Reusing the same *image.RGBA pointer can leave a stale GPU cache.
		g.img.Image = snap
		g.img.Refresh()
		w, h := g.surface.Size()
		if w > 0 && h > 0 {
			// Cap reported min size so huge guests do not force a giant window.
			g.minW = float32(w)
			g.minH = float32(h)
			if g.minW > 1280 {
				g.minW = 960
			}
			if g.minH > 800 {
				g.minH = 540
			}
		}
		g.Refresh()
	}
	// Present is off the UI thread; schedule refresh without blocking the
	// display channel (DoAndWait can deadlock against the UI event loop).
	if fyne.CurrentApp() != nil {
		fyne.Do(apply)
		return
	}
	apply()
}

// SetScaleHint records the window canvas scale for HiDPI best-effort paths.
func (g *guestView) SetScaleHint(scale float32) {
	if scale <= 0 {
		scale = 1
	}
	g.scaleMilli.Store(uint32(scale * 1000))
}

// ScaleHint returns the last canvas scale (1.0 if unknown).
func (g *guestView) ScaleHint() float32 {
	b := g.scaleMilli.Load()
	if b == 0 {
		return 1
	}
	return float32(b) / 1000
}

func (g *guestView) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(g.img))
}

func (g *guestView) MinSize() fyne.Size {
	return fyne.NewSize(g.minW, g.minH)
}
