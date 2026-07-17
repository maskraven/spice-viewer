// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package ui

import "syscall"

var (
	user32         = syscall.NewLazyDLL("user32.dll")
	procShowCursor = user32.NewProc("ShowCursor")
	procSetCursor  = user32.NewProc("SetCursor")
	procLoadCursor = user32.NewProc("LoadCursorW")
)

// forceHostCursorVisible shows the OS pointer without waiting for a mouse move.
// ShowCursor uses a display counter; call until visible.
func forceHostCursorVisible() {
	// IDC_ARROW = 32512
	arrow, _, _ := procLoadCursor.Call(0, uintptr(32512))
	if arrow != 0 {
		_, _, _ = procSetCursor.Call(arrow)
	}
	for i := 0; i < 16; i++ {
		ret, _, _ := procShowCursor.Call(1) // TRUE = show
		if int32(ret) >= 0 {
			break
		}
	}
}

// forceHostCursorHidden hides the OS pointer without waiting for a mouse move.
func forceHostCursorHidden() {
	for i := 0; i < 16; i++ {
		ret, _, _ := procShowCursor.Call(0) // FALSE = hide
		// Counter < 0 means the cursor is not drawn.
		if int32(ret) < 0 {
			break
		}
	}
}
