// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice

import (
	"fmt"

	"github.com/maskraven/virt-viewer/internal/channel"
)

// Inputs is the public keyboard/mouse inject surface for a live Client.
//
// Methods are safe for concurrent use. A nil receiver returns an error.
// After Client.Close, writes fail (connection closed).
type Inputs struct {
	in *channel.Inputs
}

// KeyDown injects a key-press using a PC XT set-1 scancode (spice-gtk form).
func (i *Inputs) KeyDown(scancode uint16) error {
	if i == nil || i.in == nil {
		return fmt.Errorf("spice: inputs not available")
	}
	return i.in.KeyDown(scancode)
}

// KeyUp injects a key-release using a PC XT set-1 scancode.
func (i *Inputs) KeyUp(scancode uint16) error {
	if i == nil || i.in == nil {
		return fmt.Errorf("spice: inputs not available")
	}
	return i.in.KeyUp(scancode)
}

// MouseMove injects mouse movement according to the current mouse mode
// (absolute in CLIENT mode, relative in SERVER mode).
func (i *Inputs) MouseMove(x, y int32) error {
	if i == nil || i.in == nil {
		return fmt.Errorf("spice: inputs not available")
	}
	return i.in.MouseMove(x, y)
}

// MouseButton injects a button press (pressed=true) or release (pressed=false).
// button is a SPICE mouse button id (see spice-protocol / internal/protocol).
func (i *Inputs) MouseButton(button uint8, pressed bool) error {
	if i == nil || i.in == nil {
		return fmt.Errorf("spice: inputs not available")
	}
	return i.in.MouseButton(button, pressed)
}

// MouseWheel injects wheel motion (positive = away from user / "up").
func (i *Inputs) MouseWheel(delta int) error {
	if i == nil || i.in == nil {
		return fmt.Errorf("spice: inputs not available")
	}
	return i.in.MouseWheel(delta)
}

// MouseMode returns the active SPICE mouse mode bit (CLIENT or SERVER).
func (i *Inputs) MouseMode() uint32 {
	if i == nil || i.in == nil {
		return 0
	}
	return i.in.MouseMode()
}
