// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"testing"

	"github.com/maskraven/virt-viewer/pkg/spice"
)

// TestHotkeyActions_WithoutDisplay exercises secure-attention → CAD,
// release-cursor → ungrab, and toggle-fullscreen flag flip without Fyne.
func TestHotkeyActions_WithoutDisplay(t *testing.T) {
	b, err := BindingsFromConfig(spice.HotkeyConfig{
		SecureAttention:  "Ctrl+Alt+Ins",
		ReleaseCursor:    "Ctrl+Alt+R",
		ToggleFullscreen: "Shift+F11",
	})
	if err != nil {
		t.Fatal(err)
	}

	var g Grab
	g.Grab()
	fs := false
	inj := &fakeInputs{}

	handle := func(mods uint8, key string) {
		switch b.Match(mods, key) {
		case ActionSecureAttention:
			if err := InjectCAD(inj); err != nil {
				t.Fatalf("CAD: %v", err)
			}
		case ActionReleaseCursor:
			g.Release()
		case ActionToggleFullscreen:
			fs = !fs
		}
	}

	// secure-attention: Ctrl+Alt+Ins → CAD (not Ins)
	handle(ModCtrl|ModAlt, "ins")
	if len(inj.downs) != 3 || inj.downs[2] != scanDelete {
		t.Fatalf("CAD downs = %v", inj.downs)
	}
	if g.Active() {
		// grab should still be active after CAD
	} else {
		t.Fatal("CAD must not ungrab")
	}

	// release-cursor
	handle(ModCtrl|ModAlt, "r")
	if g.Active() {
		t.Fatal("release-cursor must ungrab")
	}

	// toggle-fullscreen
	handle(ModShift, "f11")
	if !fs {
		t.Fatal("toggle-fullscreen should set fs")
	}
	handle(ModShift, "f11")
	if fs {
		t.Fatal("toggle-fullscreen should clear fs")
	}
}
