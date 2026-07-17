// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"sync"
	"sync/atomic"
	"time"
)

// Live open handling: when the GUI is already running, additional .vv paths
// open a new session window in this process (multi-window). No second process
// and no NSApplication.delegate replacement.

// LiveOpenHandler is called with a filesystem path to a connection file.
type LiveOpenHandler func(path string)

var (
	liveOpenEnabled atomic.Bool
	liveOpenMu      sync.Mutex
	liveOpenFn      LiveOpenHandler

	liveOpenDebounceMu sync.Mutex
	liveOpenLastPath   string
	liveOpenLastAt     time.Time
)

// SetLiveOpenHandler registers the callback for open-document events while
// the app is running. Safe to call before EnableLiveOpens.
func SetLiveOpenHandler(fn LiveOpenHandler) {
	liveOpenMu.Lock()
	liveOpenFn = fn
	liveOpenMu.Unlock()
}

// EnableLiveOpens allows AE / document-open events to invoke the live handler
// instead of only filling the cold-start pending path buffer.
func EnableLiveOpens() {
	liveOpenEnabled.Store(true)
	rearmLiveOpenPlatform()
}

// DisableLiveOpens stops live open delivery (e.g. during shutdown).
func DisableLiveOpens() {
	liveOpenEnabled.Store(false)
}

// deliverLiveOpen is called from platform code (macOS AE) with a path.
// Cold start: live opens disabled — pending path is used by ResolveConnectionPath.
// Live: debounced invoke of the handler.
func deliverLiveOpen(path string) {
	path = NormalizePathArg(path)
	if path == "" || !liveOpenEnabled.Load() {
		return
	}
	liveOpenDebounceMu.Lock()
	if path == liveOpenLastPath && time.Since(liveOpenLastAt) < 800*time.Millisecond {
		liveOpenDebounceMu.Unlock()
		return
	}
	liveOpenLastPath = path
	liveOpenLastAt = time.Now()
	liveOpenDebounceMu.Unlock()

	liveOpenMu.Lock()
	fn := liveOpenFn
	liveOpenMu.Unlock()
	if fn != nil {
		fn(path)
	}
}

// rearmLiveOpenPlatform re-installs OS hooks after the GUI toolkit starts.
// No-op on platforms that always start a new process per file open.
func rearmLiveOpenPlatform() {
	rearmLiveOpenPlatformImpl()
}
