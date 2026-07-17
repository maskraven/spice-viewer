// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package ui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework Foundation
#include "open_handler_darwin.h"
#include <stdlib.h>
*/
import "C"

import (
	"unsafe"
)

func init() {
	// Register open-document handler before main so Launch Services can deliver
	// Finder double-click events (kAEOpenDocuments) without relying on argv.
	C.spiceViewerInstallOpenHandler()
}

// waitForLaunchDocument waits briefly for a Finder open-document Apple Event.
func waitForLaunchDocument() string {
	C.spiceViewerPumpOpenEvents(C.double(0.9))
	p := C.spiceViewerTakePendingPath()
	if p == nil {
		C.spiceViewerPumpOpenEvents(C.double(0.4))
		p = C.spiceViewerTakePendingPath()
	}
	if p == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(p))
	return NormalizePathArg(C.GoString(p))
}

// ResolveConnectionPath returns argv path, Finder double-click path, or native picker.
func ResolveConnectionPath(argvPath string) (string, error) {
	if p := NormalizePathArg(argvPath); p != "" {
		return p, nil
	}
	// Double-click .vv: path arrives via Apple Event, not argv (only -psn_).
	if p := waitForLaunchDocument(); p != "" {
		return p, nil
	}
	// Dock / empty launch: system file chooser.
	return PickConnectionFile()
}
