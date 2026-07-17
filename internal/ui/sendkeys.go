// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"fmt"
	"strings"
	"unicode"
)

// SendKeyPreset is a named key sequence for the Send Keys menu
// (remote-viewer / virt-viewer style guest inject without agent).
type SendKeyPreset struct {
	// Label is the menu item text (e.g. "Ctrl+Alt+Del").
	Label string
	// Keys is the press order of XT scancodes (spice-gtk form).
	Keys []uint16
}

// StandardSendKeys are the sequences exposed in the Send Keys menu for daily use.
// Ctrl+Alt+Del is first (secure attention / Windows login).
func StandardSendKeys() []SendKeyPreset {
	return []SendKeyPreset{
		{Label: "Ctrl+Alt+Del", Keys: []uint16{scanLCtrl, scanLAlt, scanDelete}},
		{Label: "Ctrl+Alt+Backspace", Keys: []uint16{scanLCtrl, scanLAlt, scanBack}},
		{Label: "Ctrl+Alt+End", Keys: []uint16{scanLCtrl, scanLAlt, scanEnd}},
		{Label: "Ctrl+Alt+F1", Keys: []uint16{scanLCtrl, scanLAlt, scanF1}},
		{Label: "Ctrl+Alt+F2", Keys: []uint16{scanLCtrl, scanLAlt, scanF2}},
		{Label: "Ctrl+Alt+F3", Keys: []uint16{scanLCtrl, scanLAlt, scanF3}},
		{Label: "Ctrl+Alt+F4", Keys: []uint16{scanLCtrl, scanLAlt, scanF4}},
		{Label: "Ctrl+Alt+F5", Keys: []uint16{scanLCtrl, scanLAlt, scanF5}},
		{Label: "Ctrl+Alt+F6", Keys: []uint16{scanLCtrl, scanLAlt, scanF6}},
		{Label: "Ctrl+Alt+F7", Keys: []uint16{scanLCtrl, scanLAlt, scanF7}},
		{Label: "Ctrl+Alt+F8", Keys: []uint16{scanLCtrl, scanLAlt, scanF8}},
		{Label: "Ctrl+Alt+F9", Keys: []uint16{scanLCtrl, scanLAlt, scanF9}},
		{Label: "Ctrl+Alt+F10", Keys: []uint16{scanLCtrl, scanLAlt, scanF10}},
		{Label: "Ctrl+Alt+F11", Keys: []uint16{scanLCtrl, scanLAlt, scanF11}},
		{Label: "Ctrl+Alt+F12", Keys: []uint16{scanLCtrl, scanLAlt, scanF12}},
		{Label: "Print Screen", Keys: []uint16{scanPrint}},
		{Label: "Windows / Super", Keys: []uint16{scanLGUI}},
		{Label: "Ctrl+Esc (Start menu)", Keys: []uint16{scanLCtrl, scanEscape}},
		{Label: "Alt+Tab", Keys: []uint16{scanLAlt, scanTab}},
		{Label: "Alt+F4", Keys: []uint16{scanLAlt, scanF4}},
		{Label: "Ctrl+Shift+Esc (Task Manager)", Keys: []uint16{scanLCtrl, scanLShift, scanEscape}},
	}
}

// InjectSequence presses keys in order then releases in reverse order.
// This is the primitive behind CAD and the Send Keys menu.
func InjectSequence(inj KeyInjector, keys []uint16) error {
	if inj == nil {
		return fmt.Errorf("ui: inject sequence: nil inputs")
	}
	if len(keys) == 0 {
		return fmt.Errorf("ui: inject sequence: empty")
	}
	var first error
	for _, sc := range keys {
		if err := inj.KeyDown(sc); err != nil && first == nil {
			first = err
		}
	}
	for i := len(keys) - 1; i >= 0; i-- {
		if err := inj.KeyUp(keys[i]); err != nil && first == nil {
			first = err
		}
	}
	if first != nil {
		return fmt.Errorf("ui: inject sequence: %w", first)
	}
	return nil
}

// InjectCAD sends Ctrl+Alt+Del (secure attention) to the guest.
func InjectCAD(inj KeyInjector) error {
	return InjectSequence(inj, []uint16{scanLCtrl, scanLAlt, scanDelete})
}

// CADScancodes returns the press order for Ctrl+Alt+Del (for tests).
func CADScancodes() []uint16 {
	return []uint16{scanLCtrl, scanLAlt, scanDelete}
}

// TypeText types s into the guest using a US-QWERTY scancode map.
// Supported: a–z, A–Z (with Shift), 0–9, space, tab, enter, backspace,
// and common punctuation. Unsupported runes return an error naming the rune.
//
// This does not require vdagent; it is best-effort for passwords/commands
// when clipboard redirection is unavailable (Phase 1).
func TypeText(inj KeyInjector, s string) error {
	if inj == nil {
		return fmt.Errorf("ui: type text: nil inputs")
	}
	for _, r := range s {
		keys, err := asciiChord(r)
		if err != nil {
			return err
		}
		if err := InjectSequence(inj, keys); err != nil {
			return err
		}
	}
	return nil
}

// asciiChord maps a single rune to a scancode press sequence (shift + key when needed).
func asciiChord(r rune) ([]uint16, error) {
	switch r {
	case ' ':
		return []uint16{scanSpace}, nil
	case '\t':
		return []uint16{scanTab}, nil
	case '\n', '\r':
		return []uint16{scanEnter}, nil
	case '\b':
		return []uint16{scanBack}, nil
	}

	// Letters
	if r >= 'a' && r <= 'z' {
		return []uint16{letterScancode(r)}, nil
	}
	if r >= 'A' && r <= 'Z' {
		return []uint16{scanLShift, letterScancode(unicode.ToLower(r))}, nil
	}
	// Digits (top row)
	if r >= '0' && r <= '9' {
		return []uint16{digitScancode(r)}, nil
	}

	// Shifted number-row punctuation (US QWERTY).
	if sc, ok := shiftedDigitPunct[r]; ok {
		return []uint16{scanLShift, sc}, nil
	}
	// Unshifted punctuation.
	if sc, ok := unshiftedPunct[r]; ok {
		return []uint16{sc}, nil
	}
	return nil, fmt.Errorf("ui: type text: unsupported character %q (US QWERTY only; use agent clipboard in a later release)", r)
}

// XT scancodes for punctuation (US layout).
const (
	scanMinus     uint16 = 0x0c // -
	scanEqual     uint16 = 0x0d // =
	scanLBracket  uint16 = 0x1a // [
	scanRBracket  uint16 = 0x1b // ]
	scanSemicolon uint16 = 0x27 // ;
	scanQuote     uint16 = 0x28 // '
	scanGrave     uint16 = 0x29 // `
	scanBackslash uint16 = 0x2b // \
	scanComma     uint16 = 0x33 // ,
	scanDot       uint16 = 0x34 // .
	scanSlash     uint16 = 0x35 // /
	scanPrint     uint16 = 0x37 // Print Screen (SysRq make on XT; best-effort)
)

var unshiftedPunct = map[rune]uint16{
	'-':  scanMinus,
	'=':  scanEqual,
	'[':  scanLBracket,
	']':  scanRBracket,
	';':  scanSemicolon,
	'\'': scanQuote,
	'`':  scanGrave,
	'\\': scanBackslash,
	',':  scanComma,
	'.':  scanDot,
	'/':  scanSlash,
}

// Shifted number-row / punctuation → base key scancode (with Shift held).
var shiftedDigitPunct = map[rune]uint16{
	'!': digitScancode('1'),
	'@': digitScancode('2'),
	'#': digitScancode('3'),
	'$': digitScancode('4'),
	'%': digitScancode('5'),
	'^': digitScancode('6'),
	'&': digitScancode('7'),
	'*': digitScancode('8'),
	'(': digitScancode('9'),
	')': digitScancode('0'),
	'_': scanMinus,
	'+': scanEqual,
	'{': scanLBracket,
	'}': scanRBracket,
	':': scanSemicolon,
	'"': scanQuote,
	'~': scanGrave,
	'|': scanBackslash,
	'<': scanComma,
	'>': scanDot,
	'?': scanSlash,
}

// FormatSendKeyHelp returns a short multi-line help string for the Help menu.
func FormatSendKeyHelp(bind Bindings) string {
	var b strings.Builder
	b.WriteString("Guest input\n")
	b.WriteString("  Click the display to grab keyboard/mouse.\n")
	b.WriteString("  Send Keys menu injects combinations the host may intercept.\n\n")
	b.WriteString("Client hotkeys (not sent to guest)\n")
	b.WriteString(fmt.Sprintf("  Secure attention (CAD):  %s\n", chordLabel(bind.SecureAttention)))
	b.WriteString(fmt.Sprintf("  Release cursor:          %s\n", chordLabel(bind.ReleaseCursor)))
	b.WriteString(fmt.Sprintf("  Toggle fullscreen:       %s\n", chordLabel(bind.ToggleFullscreen)))
	b.WriteString("\nTip: use Send Keys → Ctrl+Alt+Del for Windows login screens.")
	return b.String()
}

func chordLabel(c Chord) string {
	if c.Key == "" {
		return "(disabled)"
	}
	parts := make([]string, 0, 4)
	if c.Mods&ModCtrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if c.Mods&ModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if c.Mods&ModShift != 0 {
		parts = append(parts, "Shift")
	}
	if c.Mods&ModSuper != 0 {
		parts = append(parts, "Super")
	}
	parts = append(parts, strings.ToUpper(c.Key[:1])+c.Key[1:])
	return strings.Join(parts, "+")
}
