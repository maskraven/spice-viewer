// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package ui

import "syscall"

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procShowCursor   = user32.NewProc("ShowCursor")
	procSetCursor    = user32.NewProc("SetCursor")
	procLoadCursor   = user32.NewProc("LoadCursorW")
)

// forceHostCursorVisible shows the OS pointer without waiting for a mouse move.
// ShowCursor uses a display counter; call until visible.
func forceHostCursorVisible() {
	// IDC_ARROW = 32512
	arrow, _, _ := procLoadCursor.Call(0, uintptr(32512))
	if arrow != 0 {
		_, _, _ = procSetCursor.Call(arrow)
	}
	// Ensure show counter is non-negative (cursor visible).
	for i := 0; i < 16; i++ {
		ret, _, _ := procShowCursor.Call(1) // TRUE = show
		// ret is the display counter after the call; >= 0 means visible.
		if int32(ret) >= 0 {
			break
		}
	}
}
