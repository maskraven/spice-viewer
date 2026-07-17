// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"strings"
	"testing"
)

func TestStandardSendKeys_IncludesCAD(t *testing.T) {
	presets := StandardSendKeys()
	if len(presets) < 5 {
		t.Fatalf("expected several presets, got %d", len(presets))
	}
	if presets[0].Label != "Ctrl+Alt+Del" {
		t.Fatalf("first preset should be CAD, got %q", presets[0].Label)
	}
	if !uint16SliceEq(presets[0].Keys, CADScancodes()) {
		t.Fatalf("CAD keys = %v", presets[0].Keys)
	}
}

func TestInjectSequence_Order(t *testing.T) {
	f := &fakeInputs{}
	keys := []uint16{scanLCtrl, scanLAlt, scanF1}
	if err := InjectSequence(f, keys); err != nil {
		t.Fatal(err)
	}
	if !uint16SliceEq(f.downs, keys) {
		t.Errorf("downs = %v", f.downs)
	}
	wantUp := []uint16{scanF1, scanLAlt, scanLCtrl}
	if !uint16SliceEq(f.ups, wantUp) {
		t.Errorf("ups = %v", f.ups)
	}
}

func TestTypeText_ASCII(t *testing.T) {
	f := &fakeInputs{}
	if err := TypeText(f, "Ab1!"); err != nil {
		t.Fatal(err)
	}
	// A = Shift+a, b, 1, ! = Shift+1
	// downs should include shift for A and !
	if len(f.downs) < 4 {
		t.Fatalf("expected multiple downs, got %v", f.downs)
	}
	// First chord: Shift, a
	if f.downs[0] != scanLShift || f.downs[1] != letterScancode('a') {
		t.Fatalf("A chord downs prefix = %v %v", f.downs[0], f.downs[1])
	}
}

func TestTypeText_Unsupported(t *testing.T) {
	f := &fakeInputs{}
	err := TypeText(f, "café")
	if err == nil {
		t.Fatal("expected error for non-ASCII")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error = %v", err)
	}
}

func TestTypeText_EmptyOK(t *testing.T) {
	f := &fakeInputs{}
	if err := TypeText(f, ""); err != nil {
		t.Fatal(err)
	}
	if len(f.downs) != 0 {
		t.Fatalf("downs = %v", f.downs)
	}
}

func TestInjectCAD_UsesSequence(t *testing.T) {
	f := &fakeInputs{}
	if err := InjectCAD(f); err != nil {
		t.Fatal(err)
	}
	if !uint16SliceEq(f.downs, []uint16{scanLCtrl, scanLAlt, scanDelete}) {
		t.Fatalf("downs = %v", f.downs)
	}
}
