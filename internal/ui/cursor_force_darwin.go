// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package ui

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation
#include <ApplicationServices/ApplicationServices.h>
#include <CoreFoundation/CoreFoundation.h>

// Fyne/GLFW only re-evaluates Cursorable.Cursor() on mouse motion. We force
// OS show/hide so mode switches and grab/ungrab take effect immediately.

static void force_show_cursor(void) {
	CGDisplayShowCursor(kCGDirectMainDisplay);
	CGAssociateMouseAndMouseCursorPosition(true);
}

static void force_hide_cursor(void) {
	// Hide is refcounted; call a few times so a prior Show leaves us hidden.
	for (int i = 0; i < 8; i++) {
		CGDisplayHideCursor(kCGDirectMainDisplay);
	}
	CGAssociateMouseAndMouseCursorPosition(true);
}
*/
import "C"

// forceHostCursorVisible shows the OS pointer without waiting for a mouse move.
func forceHostCursorVisible() {
	C.force_show_cursor()
}

// forceHostCursorHidden hides the OS pointer without waiting for a mouse move.
func forceHostCursorHidden() {
	C.force_hide_cursor()
}
