// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package ui

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation
#include <ApplicationServices/ApplicationServices.h>
#include <CoreFoundation/CoreFoundation.h>

// force_show_cursor makes the system pointer visible immediately.
// Fyne/GLFW only re-evaluates Cursorable.Cursor() on mouse motion, so after
// ungrab the arrow would stay hidden until the user moves the mouse.
static void force_show_cursor(void) {
	CGDisplayShowCursor(kCGDirectMainDisplay);
	CGAssociateMouseAndMouseCursorPosition(true);
}
*/
import "C"

// forceHostCursorVisible shows the OS pointer without waiting for a mouse move.
func forceHostCursorVisible() {
	C.force_show_cursor()
}
