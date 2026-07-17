// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package ui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework Foundation
#include "open_handler_darwin.h"
*/
import "C"

//export spiceViewerGoOnOpenDocument
func spiceViewerGoOnOpenDocument(cpath *C.char) {
	if cpath == nil {
		return
	}
	deliverLiveOpen(C.GoString(cpath))
}

func rearmLiveOpenPlatformImpl() {
	// Re-claim the Apple Event handler after Fyne/GLFW is running.
	// Does not replace NSApplication.delegate.
	C.spiceViewerInstallOpenHandler()
}
