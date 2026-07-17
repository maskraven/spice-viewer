// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build (!darwin && !windows) || noaudio

package audio

import "fmt"

// Available is false when no host backend is linked (Linux stub, other GOOS,
// or -tags=noaudio).
func Available() bool {
	return false
}

// openHostSink always fails on stub platforms so OpenDefault returns nil.
func openHostSink() (*Sink, error) {
	return nil, fmt.Errorf("audio: no host playback backend for this platform/build")
}
