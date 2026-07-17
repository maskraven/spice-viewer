// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

// padContainScale returns the ImageFillContain scale and top-left offset of the
// guest desktop inside a pad of size padW×padH for a guest of gw×gh pixels.
func padContainScale(padW, padH float32, gw, gh int) (scale, ox, oy float32) {
	if gw <= 0 || gh <= 0 || padW <= 0 || padH <= 0 {
		return 1, 0, 0
	}
	scale = padW / float32(gw)
	if s := padH / float32(gh); s < scale {
		scale = s
	}
	if scale <= 0 {
		scale = 1
	}
	contentW := float32(gw) * scale
	contentH := float32(gh) * scale
	ox = (padW - contentW) / 2
	oy = (padH - contentH) / 2
	return scale, ox, oy
}

// padToGuest maps pad-local pointer coordinates to guest surface pixels using
// the same contain letterboxing as guestView (canvas.ImageFillContain).
func padToGuest(lx, ly, padW, padH float32, gw, gh int) (x, y int32) {
	if gw <= 0 || gh <= 0 || padW <= 0 || padH <= 0 {
		return 0, 0
	}
	scale, ox, oy := padContainScale(padW, padH, gw, gh)
	// Clamp into the content rect so clicks in letterbox bands map to edges.
	if lx < ox {
		lx = ox
	}
	if ly < oy {
		ly = oy
	}
	maxX := ox + float32(gw)*scale
	maxY := oy + float32(gh)*scale
	if lx > maxX {
		lx = maxX
	}
	if ly > maxY {
		ly = maxY
	}
	x = int32((lx - ox) / scale)
	y = int32((ly - oy) / scale)
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if gw > 0 && x >= int32(gw) {
		x = int32(gw) - 1
	}
	if gh > 0 && y >= int32(gh) {
		y = int32(gh) - 1
	}
	return x, y
}

// guestToPad maps a guest pixel (typically cursor hotspot) into pad-local
// coordinates for overlay placement.
func guestToPad(gx, gy int, padW, padH float32, gw, gh int) (px, py float32) {
	if gw <= 0 || gh <= 0 || padW <= 0 || padH <= 0 {
		return 0, 0
	}
	scale, ox, oy := padContainScale(padW, padH, gw, gh)
	return ox + float32(gx)*scale, oy + float32(gy)*scale
}
