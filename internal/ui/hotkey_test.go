// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"testing"

	"github.com/maskraven/virt-viewer/pkg/spice"
)

func TestParseChord_ProxmoxDefaults(t *testing.T) {
	cases := []struct {
		in       string
		wantMods uint8
		wantKey  string
		wantScan uint16
	}{
		{"Ctrl+Alt+Ins", ModCtrl | ModAlt, "ins", scanInsert},
		{"ctrl+alt+ins", ModCtrl | ModAlt, "ins", scanInsert},
		{"Ctrl+Alt+R", ModCtrl | ModAlt, "r", letterScancode('r')},
		{"Shift+F11", ModShift, "f11", scanF11},
		{"  Shift + F11  ", ModShift, "f11", scanF11},
		{"Control+Alt+Insert", ModCtrl | ModAlt, "ins", scanInsert},
	}
	for _, tc := range cases {
		c, err := ParseChord(tc.in)
		if err != nil {
			t.Fatalf("ParseChord(%q): %v", tc.in, err)
		}
		if c.Mods != tc.wantMods || c.Key != tc.wantKey {
			t.Errorf("ParseChord(%q) = mods=%b key=%q; want mods=%b key=%q",
				tc.in, c.Mods, c.Key, tc.wantMods, tc.wantKey)
		}
		if c.Scan != tc.wantScan {
			t.Errorf("ParseChord(%q).Scan = %#x; want %#x", tc.in, c.Scan, tc.wantScan)
		}
	}
}

func TestParseChord_EmptyAndErrors(t *testing.T) {
	c, err := ParseChord("")
	if err != nil || !c.Empty() {
		t.Fatalf("empty: chord=%+v err=%v", c, err)
	}
	if _, err := ParseChord("Ctrl+Alt"); err == nil {
		t.Fatal("expected error for modifiers-only")
	}
	if _, err := ParseChord("Ctrl+A+B"); err == nil {
		t.Fatal("expected error for two keys")
	}
}

func TestBindingsFromConfig_Defaults(t *testing.T) {
	b, err := BindingsFromConfig(spice.HotkeyConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if b.SecureAttention.String() != "Ctrl+Alt+Ins" {
		t.Errorf("SA = %q", b.SecureAttention.String())
	}
	if b.ReleaseCursor.String() != "Ctrl+Alt+R" {
		t.Errorf("RC = %q", b.ReleaseCursor.String())
	}
	if b.ToggleFullscreen.String() != "Shift+F11" {
		t.Errorf("TF = %q", b.ToggleFullscreen.String())
	}
}

func TestBindingsFromConfig_Custom(t *testing.T) {
	b, err := BindingsFromConfig(spice.HotkeyConfig{
		SecureAttention:  "Ctrl+Alt+F1",
		ReleaseCursor:    "Ctrl+Alt+Z",
		ToggleFullscreen: "F11",
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.Match(ModCtrl|ModAlt, "f1") != ActionSecureAttention {
		t.Error("want secure-attention on Ctrl+Alt+F1")
	}
	if b.Match(ModCtrl|ModAlt, "z") != ActionReleaseCursor {
		t.Error("want release-cursor")
	}
	if b.Match(0, "f11") != ActionToggleFullscreen {
		t.Error("want toggle-fullscreen")
	}
	// Ins alone must not fire CAD when custom binding is F1.
	if b.Match(ModCtrl|ModAlt, "ins") != ActionNone {
		t.Error("custom SA must not match Ins")
	}
}

func TestBindings_Match_SecureAttentionPriority(t *testing.T) {
	// Unrealistic overlap: same chord for two actions — first match wins (SA).
	b := Bindings{
		SecureAttention:  mustChord(t, "Ctrl+Alt+R"),
		ReleaseCursor:    mustChord(t, "Ctrl+Alt+R"),
		ToggleFullscreen: mustChord(t, "Shift+F11"),
	}
	if b.Match(ModCtrl|ModAlt, "r") != ActionSecureAttention {
		t.Fatal("SA should win on overlap")
	}
}

func TestChord_Matches(t *testing.T) {
	c := mustChord(t, "Ctrl+Alt+Ins")
	if !c.Matches(ModCtrl|ModAlt, "Ins") {
		t.Error("case-insensitive key")
	}
	if c.Matches(ModCtrl|ModAlt, "r") {
		t.Error("wrong key")
	}
	if c.Matches(ModCtrl, "ins") {
		t.Error("missing Alt")
	}
	if c.Matches(ModCtrl|ModAlt|ModShift, "ins") {
		t.Error("extra Shift should not match exact mods")
	}
}

func mustChord(t *testing.T, s string) Chord {
	t.Helper()
	c, err := ParseChord(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
