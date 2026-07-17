// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"

	"github.com/maskraven/spice-viewer/pkg/spice"
)

func TestKeyNameScancode_PeriodAndPunct(t *testing.T) {
	// Regression: "." was letter/digit-only and returned 0 → not sent to guest.
	if sc := keyNameScancode("."); sc != scanDot {
		t.Fatalf("keyNameScancode(.) = %#x; want scanDot %#x", sc, scanDot)
	}
	if sc := fyneKeyScancode(fyne.KeyPeriod); sc != scanDot {
		t.Fatalf("fyneKeyScancode(KeyPeriod) = %#x; want %#x", sc, scanDot)
	}
	if sc := resolveKeyScancode(&fyne.KeyEvent{Name: fyne.KeyPeriod}); sc != scanDot {
		t.Fatalf("resolveKeyScancode(KeyPeriod) = %#x; want %#x", sc, scanDot)
	}
	for r, want := range map[rune]uint16{
		',':  scanComma,
		'/':  scanSlash,
		'-':  scanMinus,
		'=':  scanEqual,
		';':  scanSemicolon,
		'\'': scanQuote,
		'`':  scanGrave,
		'\\': scanBackslash,
		'[':  scanLBracket,
		']':  scanRBracket,
		'>':  scanDot, // shifted form of period key
	} {
		if sc := punctScancode(r); sc != want {
			t.Errorf("punctScancode(%q) = %#x; want %#x", r, sc, want)
		}
	}
}

func TestFyneKeyScancode_CoreKeys(t *testing.T) {
	cases := map[fyne.KeyName]uint16{
		fyne.KeyReturn:          scanEnter,
		fyne.KeyEnter:           scanKPEnter,
		fyne.KeyPeriod:          scanDot,
		desktop.KeyCapsLock:     scanCaps,
		desktop.KeyMenu:         scanMenu,
		desktop.KeyPrintScreen:  scanPrint,
		desktop.KeyControlLeft:  scanLCtrl,
		desktop.KeyControlRight: scanRCtrl,
		fyne.KeyA:               letterScancode('a'),
		fyne.Key0:               digitScancode('0'),
		fyne.KeyAsterisk:        scanKPStar,
		fyne.KeyPlus:            scanKPPlus,
	}
	for name, want := range cases {
		if sc := fyneKeyScancode(name); sc != want {
			t.Errorf("fyneKeyScancode(%q) = %#x; want %#x", name, sc, want)
		}
	}
	// Print Screen must be E0|0x37, not KP * (0x37).
	if scanPrint == scanKPStar {
		t.Fatal("scanPrint must not equal KP*")
	}
	if scanPrint != scanE0|0x37 {
		t.Fatalf("scanPrint = %#x; want %#x", scanPrint, scanE0|0x37)
	}
}

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
	if b.ToggleChrome.String() != "Ctrl+Alt+M" {
		t.Errorf("TC = %q", b.ToggleChrome.String())
	}
	if b.Match(ModCtrl|ModAlt, "m") != ActionToggleChrome {
		t.Error("want toggle-chrome on Ctrl+Alt+M")
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
