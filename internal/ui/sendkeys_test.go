// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
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

func TestReleaseModifiers_SendsKeyUps(t *testing.T) {
	f := &fakeInputs{}
	ReleaseModifiers(f)
	if len(f.ups) < 8 {
		t.Fatalf("expected modifier KeyUps, got %v", f.ups)
	}
	// Ensure Shift is included (stuck Shift + Ctrl+C → Chrome Inspect).
	foundShift := false
	for _, sc := range f.ups {
		if sc == scanLShift || sc == scanRShift {
			foundShift = true
			break
		}
	}
	if !foundShift {
		t.Fatal("ReleaseModifiers must KeyUp Shift")
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

func TestTypeText_SkipsUnsupportedRunes(t *testing.T) {
	f := &fakeInputs{}
	// é is skipped; "caf" is still typed (best-effort paste).
	if err := TypeText(f, "café"); err != nil {
		t.Fatal(err)
	}
	if len(f.downs) < 3 {
		t.Fatalf("expected caf typed, downs=%v", f.downs)
	}
	// Pure non-US text: nothing typed → error.
	f2 := &fakeInputs{}
	err := TypeText(f2, "日本語")
	if err == nil {
		t.Fatal("expected error when no supported characters")
	}
}

func TestFoldClipboardText_SmartQuotes(t *testing.T) {
	got := foldClipboardText("\u201chello\u201d \u2013 test\u2026")
	want := `"hello" - test...`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTypeTextBestEffort_Counts(t *testing.T) {
	f := &fakeInputs{}
	n, err := TypeTextBestEffort(f, "a\u2019b") // a ' b after fold
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("typed %d want 3", n)
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

func TestTypeText_Newlines(t *testing.T) {
	// LF, CRLF, and CR-only must each inject Enter between a and b.
	for _, s := range []string{"a\nb", "a\r\nb", "a\rb"} {
		f := &fakeInputs{}
		if err := TypeText(f, s); err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		// a, Enter, b
		want := []uint16{letterScancode('a'), scanEnter, letterScancode('b')}
		if !uint16SliceEq(f.downs, want) {
			t.Fatalf("%q downs = %v want %v", s, f.downs, want)
		}
	}
}

func TestFoldClipboardText_Newlines(t *testing.T) {
	if got := foldClipboardText("a\r\nb\rc"); got != "a\nb\nc" {
		t.Fatalf("got %q", got)
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
