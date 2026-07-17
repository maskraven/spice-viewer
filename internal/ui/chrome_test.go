// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
)

func TestInChromeHitZone(t *testing.T) {
	cases := []struct {
		y    float32
		want bool
	}{
		{0, true},
		{5, true},
		{chromeHitZoneY, true},
		{chromeHitZoneY + 0.1, false},
		{50, false},
		{-1, true}, // above top still "in zone" for safety
	}
	for _, tc := range cases {
		if got := inChromeHitZone(tc.y); got != tc.want {
			t.Errorf("inChromeHitZone(%v) = %v; want %v", tc.y, got, tc.want)
		}
	}
}

func TestTopCenterLayout(t *testing.T) {
	l := topCenterLayout{topMargin: 6}
	child := canvasObjectStub{min: fyne.NewSize(200, 40), show: true}
	objects := []fyne.CanvasObject{&child}
	l.Layout(objects, fyne.NewSize(1000, 600))
	if child.pos.Y != 6 {
		t.Errorf("Y = %v; want 6", child.pos.Y)
	}
	wantX := float32((1000 - 200) / 2)
	if child.pos.X != wantX {
		t.Errorf("X = %v; want %v", child.pos.X, wantX)
	}
	if child.size != child.min {
		t.Errorf("size = %v; want min %v", child.size, child.min)
	}
	ms := l.MinSize(objects)
	if ms.Width != 200 || ms.Height != 46 {
		t.Errorf("MinSize = %v; want 200x46", ms)
	}
}

func TestPointInRect(t *testing.T) {
	origin := fyne.NewPos(100, 6)
	size := fyne.NewSize(400, 36)
	if !pointInRect(fyne.NewPos(150, 20), origin, size) {
		t.Error("expected hit inside")
	}
	if pointInRect(fyne.NewPos(50, 20), origin, size) {
		t.Error("expected miss left")
	}
	if pointInRect(fyne.NewPos(150, 80), origin, size) {
		t.Error("expected miss below")
	}
	if pointInRect(fyne.NewPos(100, 6), origin, size) {
		// inclusive origin
	} else {
		t.Error("origin corner should be inside")
	}
	if pointInRect(fyne.NewPos(500, 6), origin, size) {
		t.Error("right edge exclusive")
	}
}

func TestControlChrome_PinAndMenuOpen(t *testing.T) {
	c := &controlChrome{}
	if c.isPinned() {
		t.Fatal("default unpinned")
	}
	if c.isVisible() {
		t.Fatal("nil pill is not visible")
	}

	c.mu.Lock()
	c.pinned = true
	c.menuOpen = true
	c.mu.Unlock()
	if !c.isPinned() {
		t.Fatal("expected pinned")
	}

	// scheduleHide must no-op when pinned/menuOpen — exercised via cancel path.
	c.cancelHide()
	c.setMenuOpen(false)
	c.mu.Lock()
	if c.menuOpen {
		c.mu.Unlock()
		t.Fatal("menuOpen should clear")
	}
	c.mu.Unlock()

	// interactionBottom with nil pill falls back to hit zone.
	if got := c.interactionBottom(); got != chromeHitZoneY {
		t.Errorf("interactionBottom nil pill = %v; want %v", got, chromeHitZoneY)
	}
}

// canvasObjectStub is a minimal CanvasObject for layout tests.
type canvasObjectStub struct {
	min  fyne.Size
	size fyne.Size
	pos  fyne.Position
	show bool
}

func (c *canvasObjectStub) MinSize() fyne.Size      { return c.min }
func (c *canvasObjectStub) Move(p fyne.Position)    { c.pos = p }
func (c *canvasObjectStub) Position() fyne.Position { return c.pos }
func (c *canvasObjectStub) Resize(s fyne.Size)      { c.size = s }
func (c *canvasObjectStub) Size() fyne.Size         { return c.size }
func (c *canvasObjectStub) Hide()                   { c.show = false }
func (c *canvasObjectStub) Show()                   { c.show = true }
func (c *canvasObjectStub) Visible() bool           { return c.show }
func (c *canvasObjectStub) Refresh()                {}
