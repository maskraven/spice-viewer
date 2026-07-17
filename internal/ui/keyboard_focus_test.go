// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

func TestMousePad_ImplementsKeyableAndFocusable(t *testing.T) {
	var _ fyne.Focusable = (*mousePad)(nil)
	var _ desktop.Keyable = (*mousePad)(nil)
}

func TestShouldHoldKeyboardFocus(t *testing.T) {
	ui := &sessionUI{}
	if ui.shouldHoldKeyboardFocus() {
		t.Fatal("ungrabbed session must not hold keyboard focus")
	}
	ui.grab.Grab()
	if !ui.shouldHoldKeyboardFocus() {
		t.Fatal("grabbed session without dialog should hold keyboard focus")
	}
	ui.darkOverlay = &darkOverlay{}
	if ui.shouldHoldKeyboardFocus() {
		t.Fatal("Type/Keys dialog open must not hold pad keyboard focus")
	}
	ui.darkOverlay = nil
	ui.grab.Release()
	if ui.shouldHoldKeyboardFocus() {
		t.Fatal("after ungrab must not hold keyboard focus")
	}
}

func TestFocusKeyboardTarget_NoPanicsWithoutWindow(t *testing.T) {
	ui := &sessionUI{}
	ui.grab.Grab()
	// pad/win nil: must be a no-op.
	ui.focusKeyboardTarget(true)
	ui.focusKeyboardTarget(false)
	ui.scheduleKeyboardFocusReclaim()
}
