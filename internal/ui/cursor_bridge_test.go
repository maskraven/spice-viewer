// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"testing"

	"fyne.io/fyne/v2/driver/desktop"
)

func TestPadGuestRoundTrip(t *testing.T) {
	// Square guest in a wider pad → horizontal letterbox bands.
	const gw, gh = 100, 100
	const padW, padH float32 = 200, 100
	// Content is 100×100 centered: ox=50, oy=0, scale=1.
	x, y := padToGuest(50, 0, padW, padH, gw, gh)
	if x != 0 || y != 0 {
		t.Fatalf("top-left content: got %d,%d", x, y)
	}
	x, y = padToGuest(149, 99, padW, padH, gw, gh)
	if x != 99 || y != 99 {
		t.Fatalf("bottom-right-ish: got %d,%d", x, y)
	}
	px, py := guestToPad(0, 0, padW, padH, gw, gh)
	if px != 50 || py != 0 {
		t.Fatalf("guestToPad origin: got %v,%v", px, py)
	}
	px, py = guestToPad(50, 50, padW, padH, gw, gh)
	if px != 100 || py != 50 {
		t.Fatalf("guestToPad center: got %v,%v", px, py)
	}
}

func TestCursorBridge_HostAlwaysDefault(t *testing.T) {
	c := newCursorBridge()
	if cur := c.hostCursor(); cur != desktop.DefaultCursor {
		t.Fatalf("host cursor: %v", cur)
	}
	pix := make([]byte, 4*4*4)
	for i := 0; i < len(pix); i += 4 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = 255, 0, 0, 255
	}
	c.SetCursor(1, 1, pix, 4, 4)
	if cur := c.hostCursor(); cur != desktop.DefaultCursor {
		t.Fatalf("with shape still default: %v", cur)
	}
	img, hx, hy, _, _, ok := c.ghostRGBA()
	if !ok || img == nil || hx != 1 || hy != 1 {
		t.Fatalf("ghostRGBA ok=%v hx=%d hy=%d", ok, hx, hy)
	}
	if img.Pix[3] >= 255 {
		t.Fatalf("expected reduced alpha, got %d", img.Pix[3])
	}
}

func TestCursorBridge_HideReset(t *testing.T) {
	c := newCursorBridge()
	pix := make([]byte, 16)
	for i := range pix {
		pix[i] = 0xff
	}
	c.SetCursor(0, 0, pix, 2, 2)
	c.MoveCursor(10, 20)
	img, _, _, x, y, vis, has := c.snapshot()
	if !vis || !has || x != 10 || y != 20 || img == nil {
		t.Fatalf("after set/move: vis=%v has=%v pos=%d,%d img=%v", vis, has, x, y, img != nil)
	}
	c.HideCursor()
	_, _, _, _, _, vis, _ = c.snapshot()
	if vis {
		t.Fatal("expected hidden")
	}
	c.ResetCursor()
	_, _, _, _, _, vis, has = c.snapshot()
	if vis || has {
		t.Fatal("expected reset clear")
	}
}
