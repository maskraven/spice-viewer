// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/maskraven/virt-viewer/pkg/spice"
)

// Default virt-viewer / Proxmox hotkey chords (used when the connection file
// omits a binding).
const (
	DefaultSecureAttention  = "Ctrl+Alt+Ins"
	DefaultReleaseCursor    = "Ctrl+Alt+R"
	DefaultToggleFullscreen = "Shift+F11"
	// DefaultToggleChrome shows/hides the top-center control bar.
	DefaultToggleChrome = "Ctrl+Alt+M"
)

// Modifier bits for a hotkey chord.
const (
	ModCtrl uint8 = 1 << iota
	ModAlt
	ModShift
	ModSuper
)

// Chord is a parsed local accelerator (modifiers + one non-modifier key).
type Chord struct {
	Mods uint8
	// Key is a normalized name: "ins", "r", "f11", "del", …
	Key string
	// Scan is the XT scancode for Key when known (0 if unmapped; match still uses Key).
	Scan uint16
}

// String returns a canonical chord form (e.g. "Ctrl+Alt+Ins").
func (c Chord) String() string {
	if c.Key == "" {
		return ""
	}
	var parts []string
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
	parts = append(parts, displayKeyName(c.Key))
	return strings.Join(parts, "+")
}

// Empty reports whether the chord has no key.
func (c Chord) Empty() bool {
	return c.Key == ""
}

// Matches reports whether pressed modifier bits and key name equal this chord.
// Left/right modifiers are not distinguished (either side sets the bit).
func (c Chord) Matches(mods uint8, key string) bool {
	if c.Empty() {
		return false
	}
	return c.Mods == mods && c.Key == normalizeKeyName(key)
}

// ParseChord parses a virt-viewer style chord string such as "Ctrl+Alt+Ins"
// or "Shift+F11". Empty input returns a zero Chord and nil error.
func ParseChord(s string) (Chord, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Chord{}, nil
	}
	parts := strings.Split(s, "+")
	var c Chord
	var keyPart string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return Chord{}, fmt.Errorf("ui: empty segment in hotkey %q", s)
		}
		switch normalizeToken(p) {
		case "ctrl", "control", "ctl":
			c.Mods |= ModCtrl
		case "alt", "opt", "option":
			c.Mods |= ModAlt
		case "shift":
			c.Mods |= ModShift
		case "super", "meta", "win", "windows", "cmd", "command":
			c.Mods |= ModSuper
		default:
			if keyPart != "" {
				return Chord{}, fmt.Errorf("ui: multiple non-modifier keys in hotkey %q", s)
			}
			keyPart = p
		}
	}
	if keyPart == "" {
		return Chord{}, fmt.Errorf("ui: hotkey %q has no key", s)
	}
	c.Key = normalizeKeyName(keyPart)
	c.Scan = keyNameScancode(c.Key)
	return c, nil
}

// Bindings holds the virt-viewer client hotkeys (local; not sent to the guest).
type Bindings struct {
	SecureAttention  Chord
	ReleaseCursor    Chord
	ToggleFullscreen Chord
	ToggleChrome     Chord
}

// BindingsFromConfig parses hotkeys from spice.HotkeyConfig, applying defaults
// when a field is empty. Invalid non-empty chords return an error.
// ToggleChrome always uses DefaultToggleChrome (not yet in connection files).
func BindingsFromConfig(h spice.HotkeyConfig) (Bindings, error) {
	sa := h.SecureAttention
	if strings.TrimSpace(sa) == "" {
		sa = DefaultSecureAttention
	}
	rc := h.ReleaseCursor
	if strings.TrimSpace(rc) == "" {
		rc = DefaultReleaseCursor
	}
	tf := h.ToggleFullscreen
	if strings.TrimSpace(tf) == "" {
		tf = DefaultToggleFullscreen
	}

	var b Bindings
	var err error
	if b.SecureAttention, err = ParseChord(sa); err != nil {
		return Bindings{}, err
	}
	if b.ReleaseCursor, err = ParseChord(rc); err != nil {
		return Bindings{}, err
	}
	if b.ToggleFullscreen, err = ParseChord(tf); err != nil {
		return Bindings{}, err
	}
	if b.ToggleChrome, err = ParseChord(DefaultToggleChrome); err != nil {
		return Bindings{}, err
	}
	return b, nil
}

// Action is a client-local hotkey action (not forwarded to the guest).
type Action int

const (
	ActionNone Action = iota
	ActionSecureAttention
	ActionReleaseCursor
	ActionToggleFullscreen
	ActionToggleChrome
)

// Match returns the action for the given modifier mask and key name.
// Order: SecureAttention, ReleaseCursor, ToggleFullscreen, ToggleChrome.
func (b Bindings) Match(mods uint8, key string) Action {
	key = normalizeKeyName(key)
	if b.SecureAttention.Matches(mods, key) {
		return ActionSecureAttention
	}
	if b.ReleaseCursor.Matches(mods, key) {
		return ActionReleaseCursor
	}
	if b.ToggleFullscreen.Matches(mods, key) {
		return ActionToggleFullscreen
	}
	if b.ToggleChrome.Matches(mods, key) {
		return ActionToggleChrome
	}
	return ActionNone
}

func normalizeToken(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// normalizeKeyName maps aliases to a stable key id used for matching.
func normalizeKeyName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	low := strings.ToLower(s)
	switch low {
	case "ins", "insert":
		return "ins"
	case "del", "delete":
		return "del"
	case "esc", "escape":
		return "escape"
	case "ret", "return", "enter":
		return "enter"
	case "pgup", "pageup", "page_up", "page-up":
		return "pageup"
	case "pgdn", "pgdown", "pagedown", "page_down", "page-down":
		return "pagedown"
	case "spc", "space", "spacebar":
		return "space"
	case "bs", "backspace":
		return "backspace"
	case "left", "right", "up", "down", "home", "end", "tab":
		return low
	}
	// F1–F12
	if len(low) >= 2 && (low[0] == 'f' || low[0] == 'F') {
		n := low[1:]
		if isAllDigits(n) {
			return "f" + n
		}
	}
	// Single letter or digit
	if len(low) == 1 {
		r := rune(low[0])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return low
		}
	}
	return low
}

func displayKeyName(key string) string {
	switch key {
	case "ins":
		return "Ins"
	case "del":
		return "Del"
	case "escape":
		return "Escape"
	case "enter":
		return "Enter"
	case "pageup":
		return "PageUp"
	case "pagedown":
		return "PageDown"
	case "space":
		return "Space"
	case "backspace":
		return "BackSpace"
	}
	if len(key) >= 2 && key[0] == 'f' && isAllDigits(key[1:]) {
		return "F" + key[1:]
	}
	if len(key) == 1 {
		return strings.ToUpper(key)
	}
	if key == "" {
		return ""
	}
	return strings.ToUpper(key[:1]) + key[1:]
}

func keyNameScancode(key string) uint16 {
	switch key {
	case "ins":
		return scanInsert
	case "del":
		return scanDelete
	case "escape":
		return scanEscape
	case "enter":
		return scanEnter
	case "kpenter":
		return scanKPEnter
	case "tab":
		return scanTab
	case "space":
		return scanSpace
	case "backspace":
		return scanBack
	case "capslock":
		return scanCaps
	case "menu":
		return scanMenu
	case "print", "printscreen":
		return scanPrint
	case "home":
		return scanHome
	case "end":
		return scanEnd
	case "pageup":
		return scanPageUp
	case "pagedown":
		return scanPageDown
	case "up":
		return scanUp
	case "down":
		return scanDown
	case "left":
		return scanLeft
	case "right":
		return scanRight
	case "f1":
		return scanF1
	case "f2":
		return scanF2
	case "f3":
		return scanF3
	case "f4":
		return scanF4
	case "f5":
		return scanF5
	case "f6":
		return scanF6
	case "f7":
		return scanF7
	case "f8":
		return scanF8
	case "f9":
		return scanF9
	case "f10":
		return scanF10
	case "f11":
		return scanF11
	case "f12":
		return scanF12
	}
	if len(key) == 1 {
		r := rune(key[0])
		if sc := letterScancode(r); sc != 0 {
			return sc
		}
		if sc := digitScancode(r); sc != 0 {
			return sc
		}
		// US punctuation: "." "," "/" "-" etc. (Fyne KeyPeriod = ".")
		return punctScancode(r)
	}
	return 0
}

// punctScancode returns the base XT scancode for US-layout punctuation.
// Shifted glyphs (e.g. '>') map to the same physical key as unshifted ('.').
func punctScancode(r rune) uint16 {
	if sc, ok := unshiftedPunct[r]; ok {
		return sc
	}
	if sc, ok := shiftedDigitPunct[r]; ok {
		return sc
	}
	return 0
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ModsFromKeyName returns the modifier bit for a key name, or 0 if not a modifier.
func ModsFromKeyName(key string) uint8 {
	switch normalizeKeyName(key) {
	case "ctrl", "control", "lctrl", "rctrl", "leftctrl", "rightctrl":
		return ModCtrl
	case "alt", "lalt", "ralt", "leftalt", "rightalt":
		return ModAlt
	case "shift", "lshift", "rshift", "leftshift", "rightshift":
		return ModShift
	case "super", "meta", "win", "cmd", "lsuper", "rsuper":
		return ModSuper
	default:
		return 0
	}
}
