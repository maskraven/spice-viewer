// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"fmt"
	"strings"
	"unicode"
)

// SendKeyPreset is a named key sequence for the Send Keys menu
// (spice-viewer / virt-viewer style guest inject without agent).
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
		{Label: "Ctrl+Esc", Keys: []uint16{scanLCtrl, scanEscape}},
		{Label: "Alt+Tab", Keys: []uint16{scanLAlt, scanTab}},
		{Label: "Alt+F4", Keys: []uint16{scanLAlt, scanF4}},
		{Label: "Ctrl+Shift+Esc", Keys: []uint16{scanLCtrl, scanLShift, scanEscape}},
	}
}

// modifierScancodes are keys that stick in the guest if a KeyUp is dropped
// (e.g. after TypeText / paste inject). Stuck Shift + Ctrl+C becomes
// Ctrl+Shift+C in Chrome → Inspect / DevTools.
var modifierScancodes = []uint16{
	scanLShift, scanRShift,
	scanLCtrl, scanRCtrl,
	scanLAlt, scanRAlt,
	scanLGUI, scanRGUI,
}

// ReleaseModifiers force-sends KeyUp for all modifier scancodes (best-effort).
// Safe to call even if the guest did not have them down.
func ReleaseModifiers(inj KeyInjector) {
	if inj == nil {
		return
	}
	for _, sc := range modifierScancodes {
		_ = inj.KeyUp(sc)
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
		// Best-effort: do not leave Shift/Ctrl down after a failed chord.
		ReleaseModifiers(inj)
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
// when clipboard redirection is unavailable.
func TypeText(inj KeyInjector, s string) error {
	n, err := TypeTextBestEffort(inj, s)
	if err != nil {
		return err
	}
	if n == 0 && s != "" {
		return fmt.Errorf("ui: type text: no supported characters in input")
	}
	return nil
}

// TypeTextBestEffort types s, folding common Unicode (smart quotes, NBSP) to
// ASCII and skipping remaining unsupported runes. Returns the number of runes
// successfully typed. err is non-nil only on inject I/O failure (not on skip).
func TypeTextBestEffort(inj KeyInjector, s string) (typed int, err error) {
	if inj == nil {
		return 0, fmt.Errorf("ui: type text: nil inputs")
	}
	s = foldClipboardText(s)
	// Clear any leftover Shift/Ctrl from prior injects before typing.
	ReleaseModifiers(inj)
	for _, r := range s {
		keys, cerr := asciiChord(r)
		if cerr != nil {
			continue // skip unsupported; do not abort whole paste
		}
		if ierr := InjectSequence(inj, keys); ierr != nil {
			ReleaseModifiers(inj)
			return typed, ierr
		}
		typed++
	}
	ReleaseModifiers(inj)
	return typed, nil
}

// foldClipboardText normalizes host clipboard / Type-dialog text for US
// keystroke paste (macOS curly quotes, NBSP, and all newline forms → Enter).
func foldClipboardText(s string) string {
	if s == "" {
		return s
	}
	// Preserve line breaks: CRLF and lone CR both become LF (Enter scancode).
	// Do not drop CR — that discarded newlines when the widget used \r only.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\u2018', '\u2019', '\u201a', '\u2032': // ‘ ’ ‚ ′
			b.WriteByte('\'')
		case '\u201c', '\u201d', '\u201e', '\u2033': // “ ” „ ″
			b.WriteByte('"')
		case '\u2013', '\u2014', '\u2212': // – — −
			b.WriteByte('-')
		case '\u00a0', '\u202f', '\u2007': // NBSP / narrow NBSP
			b.WriteByte(' ')
		case '\u2026': // …
			b.WriteString("...")
		case '\u2028', '\u2029', '\u0085': // LS / PS / NEL → Enter
			b.WriteByte('\n')
		case '\u200b', '\u200c', '\u200d', '\ufeff': // zero-width / BOM
			// skip
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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

// unshiftedPunct: US layout base keys (constants live in scancode.go).
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
	b.WriteString(fmt.Sprintf("  Toggle control bar:      %s\n", chordLabel(bind.ToggleChrome)))
	b.WriteString("\nControl bar: move the pointer to the top edge of the display\n")
	b.WriteString("to reveal the floating toolbar; Pin keeps it visible.\n")
	b.WriteString("  Right Ctrl — also releases grab (not sent to guest while grabbed)\n")
	b.WriteString("\nMultiple sessions: File → Open Connection… or open another .vv\n")
	b.WriteString("(macOS: same app, new window; Windows/Linux may also start a new process).\n")
	b.WriteString("\nPointer: local host cursor stays visible; a translucent remote\n")
	b.WriteString("ghost follows from the SPICE cursor channel (may lag).\n")
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
