// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"image"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"

	"github.com/maskraven/spice-viewer/pkg/spice"
)

// Compile-time: cursorBridge is a spice.CursorDriver.
var _ spice.CursorDriver = (*cursorBridge)(nil)

// cursorBridge receives SPICE cursor-channel updates and feeds the UI:
// host keeps the system pointer; dual-mode paints a lagging remote ghost.
type cursorBridge struct {
	mu sync.Mutex

	ui *sessionUI

	hotX, hotY int
	w, h       int
	rgba       []byte
	x, y       int
	visible    bool
	hasShape   bool

	// shapeImg is a stable *image.RGBA for the remote ghost overlay.
	shapeImg *image.RGBA
}

// newCursorBridge returns a driver with no shape (hidden).
func newCursorBridge() *cursorBridge {
	return &cursorBridge{}
}

// Bind attaches the live session UI (may be called after Connect starts).
func (c *cursorBridge) Bind(ui *sessionUI) {
	c.mu.Lock()
	c.ui = ui
	c.mu.Unlock()
	c.notifyUI()
}

// SetCursor implements spice.CursorDriver.
func (c *cursorBridge) SetCursor(hotX, hotY int, rgba []byte, w, h int) {
	c.mu.Lock()
	c.hotX, c.hotY = hotX, hotY
	c.w, c.h = w, h
	if len(rgba) > 0 && w > 0 && h > 0 {
		c.rgba = append([]byte(nil), rgba...)
		c.hasShape = true
		c.shapeImg = rgbaToImage(c.rgba, w, h)
		c.visible = true
	} else {
		c.rgba = nil
		c.hasShape = false
		c.shapeImg = nil
	}
	c.mu.Unlock()
	c.notifyUI()
}

// MoveCursor implements spice.CursorDriver.
func (c *cursorBridge) MoveCursor(x, y int) {
	c.mu.Lock()
	c.x, c.y = x, y
	c.visible = true
	c.mu.Unlock()
	c.notifyUI()
}

// HideCursor implements spice.CursorDriver.
func (c *cursorBridge) HideCursor() {
	c.mu.Lock()
	c.visible = false
	c.mu.Unlock()
	c.notifyUI()
}

// ResetCursor implements spice.CursorDriver.
func (c *cursorBridge) ResetCursor() {
	c.mu.Lock()
	c.visible = false
	c.hasShape = false
	c.rgba = nil
	c.shapeImg = nil
	c.w, c.h = 0, 0
	c.hotX, c.hotY = 0, 0
	c.x, c.y = 0, 0
	c.mu.Unlock()
	c.notifyUI()
}

// snapshot returns a copy of cursor state for UI painting.
func (c *cursorBridge) snapshot() (img *image.RGBA, hotX, hotY, x, y int, visible, hasShape bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.shapeImg, c.hotX, c.hotY, c.x, c.y, c.visible, c.hasShape
}

// hostCursor always keeps the system pointer visible. The remote cursor is the
// translucent ghost overlay, not the OS cursor (TeamViewer-style dual pointer).
func (c *cursorBridge) hostCursor() desktop.Cursor {
	return desktop.DefaultCursor
}

// ghostRGBA returns a translucent copy of the guest cursor for the lag overlay.
func (c *cursorBridge) ghostRGBA() (img *image.RGBA, hotX, hotY, x, y int, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.visible || !c.hasShape || c.shapeImg == nil {
		return nil, 0, 0, 0, 0, false
	}
	src := c.shapeImg
	b := src.Bounds()
	out := image.NewRGBA(b)
	const alphaScale = 0.55
	for i := 0; i < len(src.Pix); i += 4 {
		out.Pix[i+0] = src.Pix[i+0]
		out.Pix[i+1] = src.Pix[i+1]
		out.Pix[i+2] = src.Pix[i+2]
		out.Pix[i+3] = uint8(float64(src.Pix[i+3]) * alphaScale)
	}
	return out, c.hotX, c.hotY, c.x, c.y, true
}

func (c *cursorBridge) notifyUI() {
	c.mu.Lock()
	ui := c.ui
	c.mu.Unlock()
	if ui == nil {
		return
	}
	apply := func() {
		if ui.pad != nil {
			ui.pad.refreshGhost()
			ui.pad.Refresh()
		}
	}
	if fyne.CurrentApp() != nil {
		fyne.Do(apply)
		return
	}
	apply()
}

func rgbaToImage(rgba []byte, w, h int) *image.RGBA {
	if w <= 0 || h <= 0 || len(rgba) < w*h*4 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(img.Pix, rgba[:w*h*4])
	return img
}

// remoteGhost is a canvas overlay for the lagging remote cursor.
type remoteGhost struct {
	img *canvas.Image
}

func newRemoteGhost() *remoteGhost {
	img := canvas.NewImageFromImage(nil)
	img.FillMode = canvas.ImageFillOriginal
	img.ScaleMode = canvas.ImageScalePixels
	img.Hide()
	return &remoteGhost{img: img}
}

// layout places the ghost so the hotspot sits at pad-local (px, py).
func (g *remoteGhost) layout(px, py float32, hotX, hotY int, scale float32, w, h int, show bool) {
	if g == nil || g.img == nil {
		return
	}
	if !show || w <= 0 || h <= 0 {
		g.img.Hide()
		return
	}
	if scale <= 0 {
		scale = 1
	}
	dw := float32(w) * scale
	dh := float32(h) * scale
	g.img.Move(fyne.NewPos(px-float32(hotX)*scale, py-float32(hotY)*scale))
	g.img.Resize(fyne.NewSize(dw, dh))
	g.img.Show()
}
