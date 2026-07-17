// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import "runtime"

// physicalScanToXT converts a Fyne/GLFW Physical.ScanCode into a spice-gtk
// XT scancode when possible. Returns 0 when unknown (caller keeps KeyName map).
//
// spice-gtk uses host hardware keycodes → XT tables (evdev/X11, Win, OSX).
// GLFW scancodes are platform-specific; we apply conservative maps so keys
// Fyne does not name are still forwarded when we recognize the host code.
func physicalScanToXT(scan int) uint16 {
	if scan <= 0 {
		return 0
	}
	switch runtime.GOOS {
	case "windows":
		return winScanToXT(scan)
	case "darwin":
		return darwinKeycodeToXT(scan)
	case "linux":
		return linuxKeycodeToXT(scan)
	default:
		return 0
	}
}

// winScanToXT: GLFW on Windows passes the native PC scancode (set-1 make).
// Extended keys may appear as 0xe0xx or with high bits; normalize to spice form.
func winScanToXT(scan int) uint16 {
	// Already spice-style with E0 marker in bit 8.
	if scan&0x100 != 0 {
		code := uint16(scan & 0x37f)
		if code&0xff != 0 {
			return code
		}
	}
	// 0xe0xx form (high byte e0).
	if scan > 0xff && (scan>>8)&0xff == 0xe0 {
		return scanE0 | uint16(scan&0x7f)
	}
	// Plain set-1 make (1..0x58 common range, plus ISO/media).
	if scan >= 1 && scan <= 0x7f {
		return uint16(scan)
	}
	return 0
}

// linuxKeycodeToXT: X11 keycodes are often evdev+8. Convert to XT via a small
// table for the common alphanumeric + punctuation block (US positions).
// Full spice-gtk table is large; unknown codes return 0 → KeyName fallback already tried.
func linuxKeycodeToXT(scan int) uint16 {
	// X11 keycodes 9.. are typical; map a practical subset.
	// Values aligned with common xorgevdev2xtkbd / QEMU for main block.
	if scan < 8 {
		return 0
	}
	// Try treating (scan-8) as Linux evdev code then map a few critical ones.
	ev := scan - 8
	if sc, ok := linuxEvdevXT[ev]; ok {
		return sc
	}
	return 0
}

// Minimal Linux evdev → XT for keys often missing from Fyne names.
// Complete tables can replace this later without API change.
var linuxEvdevXT = map[int]uint16{
	1:  scanEscape, // KEY_ESC
	14: scanBack,   // KEY_BACKSPACE
	15: scanTab,
	28: scanEnter,
	29: scanLCtrl,
	42: scanLShift,
	54: scanRShift,
	56: scanLAlt,
	57: scanSpace,
	58: scanCaps,
	97: scanRCtrl,  // KEY_RIGHTCTRL
	100: scanRAlt,  // KEY_RIGHTALT
	125: scanLGUI,  // KEY_LEFTMETA
	126: scanRGUI,
	127: scanMenu,
	99:  scanPrint, // KEY_SYSRQ / Print often
	70:  scanScrollLock,
	69:  scanNumLock,
	// Main punctuation (US) as evdev codes
	12: scanMinus,     // KEY_MINUS
	13: scanEqual,     // KEY_EQUAL
	26: scanLBracket,  // KEY_LEFTBRACE
	27: scanRBracket,  // KEY_RIGHTBRACE
	39: scanSemicolon, // KEY_SEMICOLON
	40: scanQuote,     // KEY_APOSTROPHE
	41: scanGrave,     // KEY_GRAVE
	43: scanBackslash, // KEY_BACKSLASH
	51: scanComma,     // KEY_COMMA
	52: scanDot,       // KEY_DOT
	53: scanSlash,     // KEY_SLASH
	86: scanISO102,    // KEY_102ND
}

// darwinKeycodeToXT: macOS virtual keycodes (Carbon/USB) → XT.
// Subset of the common map used by spice-gtk osx2xtkbd / QEMU.
func darwinKeycodeToXT(scan int) uint16 {
	if sc, ok := darwinXT[scan]; ok {
		return sc
	}
	return 0
}

// macOS keycode → XT (physical US positions). Indices are kVK_* codes.
var darwinXT = map[int]uint16{
	0x00: letterScancode('a'),
	0x01: letterScancode('s'),
	0x02: letterScancode('d'),
	0x03: letterScancode('f'),
	0x04: letterScancode('h'),
	0x05: letterScancode('g'),
	0x06: letterScancode('z'),
	0x07: letterScancode('x'),
	0x08: letterScancode('c'),
	0x09: letterScancode('v'),
	0x0b: letterScancode('b'),
	0x0c: letterScancode('q'),
	0x0d: letterScancode('w'),
	0x0e: letterScancode('e'),
	0x0f: letterScancode('r'),
	0x10: letterScancode('y'),
	0x11: letterScancode('t'),
	0x12: digitScancode('1'),
	0x13: digitScancode('2'),
	0x14: digitScancode('3'),
	0x15: digitScancode('4'),
	0x16: digitScancode('6'),
	0x17: digitScancode('5'),
	0x18: scanEqual,
	0x19: digitScancode('9'),
	0x1a: digitScancode('7'),
	0x1b: scanMinus,
	0x1c: digitScancode('8'),
	0x1d: digitScancode('0'),
	0x1e: scanRBracket,
	0x1f: letterScancode('o'),
	0x20: letterScancode('u'),
	0x21: scanLBracket,
	0x22: letterScancode('i'),
	0x23: letterScancode('p'),
	0x24: scanEnter, // Return
	0x25: letterScancode('l'),
	0x26: letterScancode('j'),
	0x27: scanQuote,
	0x28: letterScancode('k'),
	0x29: scanSemicolon,
	0x2a: scanBackslash,
	0x2b: scanComma,
	0x2c: scanSlash,
	0x2d: letterScancode('n'),
	0x2e: letterScancode('m'),
	0x2f: scanDot, // period — critical
	0x30: scanTab,
	0x31: scanSpace,
	0x32: scanGrave,
	0x33: scanBack, // Delete (backspace)
	0x35: scanEscape,
	0x37: scanLGUI, // Command
	0x38: scanLShift,
	0x39: scanCaps,
	0x3a: scanLAlt,  // Option
	0x3b: scanLCtrl,
	0x3c: scanRShift,
	0x3d: scanRAlt,
	0x3e: scanRCtrl,
	0x36: scanRGUI, // Right Command
	// Arrows / nav (mac keycodes)
	0x7b: scanLeft,
	0x7c: scanRight,
	0x7d: scanDown,
	0x7e: scanUp,
	0x72: scanInsert, // Help / Ins on some boards
	0x73: scanHome,
	0x74: scanPageUp,
	0x75: scanDelete, // Forward delete
	0x77: scanEnd,
	0x79: scanPageDown,
	// F-keys
	0x7a: scanF1,
	0x78: scanF2,
	0x63: scanF3,
	0x76: scanF4,
	0x60: scanF5,
	0x61: scanF6,
	0x62: scanF7,
	0x64: scanF8,
	0x65: scanF9,
	0x6d: scanF10,
	0x67: scanF11,
	0x6f: scanF12,
	// Keypad
	0x52: scanKP0,
	0x53: scanKP1,
	0x54: scanKP2,
	0x55: scanKP3,
	0x56: scanKP4,
	0x57: scanKP5,
	0x58: scanKP6,
	0x59: scanKP7,
	0x5b: scanKP8,
	0x5c: scanKP9,
	0x45: scanKPPlus,
	0x4e: scanKPMinus,
	0x43: scanKPStar,
	0x4b: scanKPSlash,
	0x4c: scanKPEnter,
	0x41: scanKPDot,
}
