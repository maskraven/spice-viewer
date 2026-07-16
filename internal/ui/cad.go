// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import "fmt"

// KeyInjector is the subset of spice.Inputs needed for CAD injection.
// Tests can implement this without a live SPICE session.
type KeyInjector interface {
	KeyDown(scancode uint16) error
	KeyUp(scancode uint16) error
}

// CAD scancodes: Left Ctrl, Left Alt, Delete (extended).
// Secure-attention fires this sequence regardless of the local chord keys
// (e.g. Ctrl+Alt+Ins still sends Ctrl+Alt+Del to the guest).
var cadDownOrder = []uint16{scanLCtrl, scanLAlt, scanDelete}

// InjectCAD sends Ctrl+Alt+Del (secure attention) to the guest via inj.
// Keys are pressed in order and released in reverse order.
// A nil injector returns an error.
func InjectCAD(inj KeyInjector) error {
	if inj == nil {
		return fmt.Errorf("ui: CAD inject: nil inputs")
	}
	var first error
	for _, sc := range cadDownOrder {
		if err := inj.KeyDown(sc); err != nil && first == nil {
			first = err
		}
	}
	for i := len(cadDownOrder) - 1; i >= 0; i-- {
		if err := inj.KeyUp(cadDownOrder[i]); err != nil && first == nil {
			first = err
		}
	}
	if first != nil {
		return fmt.Errorf("ui: CAD inject: %w", first)
	}
	return nil
}

// CADScancodes returns the press order for Ctrl+Alt+Del (for tests).
func CADScancodes() []uint16 {
	out := make([]uint16, len(cadDownOrder))
	copy(out, cadDownOrder)
	return out
}
