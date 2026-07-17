// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

// KeyInjector is the subset of spice.Inputs needed for key sequence injection.
// Tests can implement this without a live SPICE session.
type KeyInjector interface {
	KeyDown(scancode uint16) error
	KeyUp(scancode uint16) error
}

// cadDownOrder is kept for package-local CAD tests that inspect the sequence.
// Prefer InjectCAD / StandardSendKeys for new code.
var cadDownOrder = []uint16{scanLCtrl, scanLAlt, scanDelete}
