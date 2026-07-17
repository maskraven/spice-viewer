// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"fmt"
	"testing"

	"github.com/maskraven/spice-viewer/pkg/spice"
)

// fakeInputs records key events for CAD tests without a display or SPICE link.
type fakeInputs struct {
	downs []uint16
	ups   []uint16
	fail  error
}

func (f *fakeInputs) KeyDown(sc uint16) error {
	if f.fail != nil {
		return f.fail
	}
	f.downs = append(f.downs, sc)
	return nil
}

func (f *fakeInputs) KeyUp(sc uint16) error {
	if f.fail != nil {
		return f.fail
	}
	f.ups = append(f.ups, sc)
	return nil
}

func TestInjectCAD_Order(t *testing.T) {
	f := &fakeInputs{}
	if err := InjectCAD(f); err != nil {
		t.Fatal(err)
	}
	wantDown := []uint16{scanLCtrl, scanLAlt, scanDelete}
	wantUp := []uint16{scanDelete, scanLAlt, scanLCtrl}
	if !uint16SliceEq(f.downs, wantDown) {
		t.Errorf("downs = %v; want %v", f.downs, wantDown)
	}
	if !uint16SliceEq(f.ups, wantUp) {
		t.Errorf("ups = %v; want %v", f.ups, wantUp)
	}
	// Explicit CAD constants: Ctrl=0x1d, Alt=0x38, Del=0x153
	if scanLCtrl != 0x1d || scanLAlt != 0x38 || scanDelete != 0x153 {
		t.Errorf("CAD scancodes: ctrl=%#x alt=%#x del=%#x", scanLCtrl, scanLAlt, scanDelete)
	}
}

func TestInjectCAD_NilAndError(t *testing.T) {
	if err := InjectCAD(nil); err == nil {
		t.Fatal("expected nil injector error")
	}
	f := &fakeInputs{fail: fmt.Errorf("closed")}
	if err := InjectCAD(f); err == nil {
		t.Fatal("expected inject error")
	}
}

func TestSecureAttention_DoesNotSendChordKey(t *testing.T) {
	// Chord is Ins; injected sequence must use Delete, never Insert.
	b, err := BindingsFromConfig(spice.HotkeyConfig{
		SecureAttention: "Ctrl+Alt+Ins",
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.Match(ModCtrl|ModAlt, "ins") != ActionSecureAttention {
		t.Fatal("chord should match")
	}
	f := &fakeInputs{}
	if err := InjectCAD(f); err != nil {
		t.Fatal(err)
	}
	for _, sc := range f.downs {
		if sc == scanInsert {
			t.Fatal("CAD must not press Insert")
		}
	}
	foundDel := false
	for _, sc := range f.downs {
		if sc == scanDelete {
			foundDel = true
		}
	}
	if !foundDel {
		t.Fatal("CAD must press Delete")
	}
}

func TestCADScancodes(t *testing.T) {
	sc := CADScancodes()
	if len(sc) != 3 || sc[0] != scanLCtrl || sc[1] != scanLAlt || sc[2] != scanDelete {
		t.Fatalf("CADScancodes = %v", sc)
	}
}

func uint16SliceEq(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
