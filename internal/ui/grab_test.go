// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import "testing"

func TestGrab_ReleaseCursorSemantics(t *testing.T) {
	var g Grab
	if g.Active() {
		t.Fatal("initial grab should be false")
	}
	g.Grab()
	if !g.Active() {
		t.Fatal("expected active after Grab")
	}
	// release-cursor hotkey path
	g.Release()
	if g.Active() {
		t.Fatal("expected inactive after Release")
	}
	g.Release() // idempotent
	if g.Active() {
		t.Fatal("still inactive")
	}
}
