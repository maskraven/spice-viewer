// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// fyneKeyName maps a Fyne key name to our hotkey/scancode key id.
func fyneKeyName(name fyne.KeyName) string {
	switch name {
	case desktop.KeyControlLeft, desktop.KeyControlRight:
		return "ctrl"
	case desktop.KeyAltLeft, desktop.KeyAltRight:
		return "alt"
	case desktop.KeyShiftLeft, desktop.KeyShiftRight:
		return "shift"
	case desktop.KeySuperLeft, desktop.KeySuperRight:
		return "super"
	case fyne.KeyInsert:
		return "ins"
	case fyne.KeyDelete:
		return "del"
	case fyne.KeyEscape:
		return "escape"
	case fyne.KeyReturn:
		return "enter"
	case fyne.KeyEnter:
		return "kpenter"
	case fyne.KeyTab:
		return "tab"
	case fyne.KeySpace:
		return "space"
	case fyne.KeyBackspace:
		return "backspace"
	case fyne.KeyHome:
		return "home"
	case fyne.KeyEnd:
		return "end"
	case fyne.KeyPageUp:
		return "pageup"
	case fyne.KeyPageDown:
		return "pagedown"
	case fyne.KeyUp:
		return "up"
	case fyne.KeyDown:
		return "down"
	case fyne.KeyLeft:
		return "left"
	case fyne.KeyRight:
		return "right"
	case desktop.KeyCapsLock:
		return "capslock"
	case desktop.KeyMenu:
		return "menu"
	case desktop.KeyPrintScreen:
		return "print"
	case fyne.KeyF1:
		return "f1"
	case fyne.KeyF2:
		return "f2"
	case fyne.KeyF3:
		return "f3"
	case fyne.KeyF4:
		return "f4"
	case fyne.KeyF5:
		return "f5"
	case fyne.KeyF6:
		return "f6"
	case fyne.KeyF7:
		return "f7"
	case fyne.KeyF8:
		return "f8"
	case fyne.KeyF9:
		return "f9"
	case fyne.KeyF10:
		return "f10"
	case fyne.KeyF11:
		return "f11"
	case fyne.KeyF12:
		return "f12"
	default:
		s := string(name)
		if len(s) == 1 {
			return strings.ToLower(s)
		}
		return normalizeKeyName(s)
	}
}

// resolveKeyScancode maps a Fyne key event to a spice-gtk XT scancode.
//
// Strategy (aligned with virt-viewer / spice-gtk intent — send physical keys):
//  1. Exhaustive KeyName → XT table (all keys Fyne names, US physical positions)
//  2. Single-char name / punctuation fallback
//  3. Platform Physical.ScanCode when still unknown (best-effort; OS-specific)
//
// Shift is a separate KEY_DOWN/UP; we never synthesize shift from a glyph.
func resolveKeyScancode(ev *fyne.KeyEvent) uint16 {
	if ev == nil {
		return 0
	}
	if sc := fyneKeyScancode(ev.Name); sc != 0 {
		return sc
	}
	// Unknown KeyName: try single-character string (some drivers report raw chars).
	if s := string(ev.Name); len(s) == 1 {
		if sc := keyNameScancode(strings.ToLower(s)); sc != 0 {
			return sc
		}
		if sc := punctScancode(rune(s[0])); sc != 0 {
			return sc
		}
	}
	// Physical host scancode (GLFW); platform tables when available.
	if sc := physicalScanToXT(ev.Physical.ScanCode); sc != 0 {
		return sc
	}
	return 0
}

// fyneKeyScancode maps a Fyne key to an XT scancode for guest injection.
// Covers every KeyName Fyne documents so live typing is not silently dropped.
func fyneKeyScancode(name fyne.KeyName) uint16 {
	switch name {
	// --- Modifiers (L/R distinct like spice-gtk) ---
	case desktop.KeyControlLeft:
		return scanLCtrl
	case desktop.KeyControlRight:
		return scanRCtrl
	case desktop.KeyAltLeft:
		return scanLAlt
	case desktop.KeyAltRight:
		return scanRAlt
	case desktop.KeyShiftLeft:
		return scanLShift
	case desktop.KeyShiftRight:
		return scanRShift
	case desktop.KeySuperLeft:
		return scanLGUI
	case desktop.KeySuperRight:
		return scanRGUI
	case desktop.KeyMenu:
		return scanMenu
	case desktop.KeyCapsLock:
		return scanCaps
	case desktop.KeyPrintScreen:
		return scanPrint

	// --- Control / whitespace ---
	case fyne.KeyInsert:
		return scanInsert
	case fyne.KeyDelete:
		return scanDelete
	case fyne.KeyEscape:
		return scanEscape
	case fyne.KeyReturn:
		return scanEnter
	case fyne.KeyEnter: // KP_Enter
		return scanKPEnter
	case fyne.KeyTab:
		return scanTab
	case fyne.KeySpace:
		return scanSpace
	case fyne.KeyBackspace:
		return scanBack

	// --- Navigation ---
	case fyne.KeyHome:
		return scanHome
	case fyne.KeyEnd:
		return scanEnd
	case fyne.KeyPageUp:
		return scanPageUp
	case fyne.KeyPageDown:
		return scanPageDown
	case fyne.KeyUp:
		return scanUp
	case fyne.KeyDown:
		return scanDown
	case fyne.KeyLeft:
		return scanLeft
	case fyne.KeyRight:
		return scanRight

	// --- Function keys ---
	case fyne.KeyF1:
		return scanF1
	case fyne.KeyF2:
		return scanF2
	case fyne.KeyF3:
		return scanF3
	case fyne.KeyF4:
		return scanF4
	case fyne.KeyF5:
		return scanF5
	case fyne.KeyF6:
		return scanF6
	case fyne.KeyF7:
		return scanF7
	case fyne.KeyF8:
		return scanF8
	case fyne.KeyF9:
		return scanF9
	case fyne.KeyF10:
		return scanF10
	case fyne.KeyF11:
		return scanF11
	case fyne.KeyF12:
		return scanF12

	// --- Letters (KeyA..KeyZ) ---
	case fyne.KeyA:
		return letterScancode('a')
	case fyne.KeyB:
		return letterScancode('b')
	case fyne.KeyC:
		return letterScancode('c')
	case fyne.KeyD:
		return letterScancode('d')
	case fyne.KeyE:
		return letterScancode('e')
	case fyne.KeyF:
		return letterScancode('f')
	case fyne.KeyG:
		return letterScancode('g')
	case fyne.KeyH:
		return letterScancode('h')
	case fyne.KeyI:
		return letterScancode('i')
	case fyne.KeyJ:
		return letterScancode('j')
	case fyne.KeyK:
		return letterScancode('k')
	case fyne.KeyL:
		return letterScancode('l')
	case fyne.KeyM:
		return letterScancode('m')
	case fyne.KeyN:
		return letterScancode('n')
	case fyne.KeyO:
		return letterScancode('o')
	case fyne.KeyP:
		return letterScancode('p')
	case fyne.KeyQ:
		return letterScancode('q')
	case fyne.KeyR:
		return letterScancode('r')
	case fyne.KeyS:
		return letterScancode('s')
	case fyne.KeyT:
		return letterScancode('t')
	case fyne.KeyU:
		return letterScancode('u')
	case fyne.KeyV:
		return letterScancode('v')
	case fyne.KeyW:
		return letterScancode('w')
	case fyne.KeyX:
		return letterScancode('x')
	case fyne.KeyY:
		return letterScancode('y')
	case fyne.KeyZ:
		return letterScancode('z')

	// --- Top-row digits ---
	case fyne.Key0:
		return digitScancode('0')
	case fyne.Key1:
		return digitScancode('1')
	case fyne.Key2:
		return digitScancode('2')
	case fyne.Key3:
		return digitScancode('3')
	case fyne.Key4:
		return digitScancode('4')
	case fyne.Key5:
		return digitScancode('5')
	case fyne.Key6:
		return digitScancode('6')
	case fyne.Key7:
		return digitScancode('7')
	case fyne.Key8:
		return digitScancode('8')
	case fyne.Key9:
		return digitScancode('9')

	// --- US punctuation (physical keys; shift is separate) ---
	case fyne.KeyPeriod:
		return scanDot
	case fyne.KeyComma:
		return scanComma
	case fyne.KeySlash:
		return scanSlash
	case fyne.KeyMinus:
		return scanMinus
	case fyne.KeyEqual:
		return scanEqual
	case fyne.KeySemicolon:
		return scanSemicolon
	case fyne.KeyApostrophe:
		return scanQuote
	case fyne.KeyBackTick:
		return scanGrave
	case fyne.KeyBackslash:
		return scanBackslash
	case fyne.KeyLeftBracket:
		return scanLBracket
	case fyne.KeyRightBracket:
		return scanRBracket

	// --- Keypad operators (Fyne documents these as keypad keys) ---
	case fyne.KeyAsterisk:
		return scanKPStar
	case fyne.KeyPlus:
		return scanKPPlus

	default:
		// Fall back to name-string path (letters/digits/punct aliases).
		return keyNameScancode(fyneKeyName(name))
	}
}

// isModifierKey reports whether name is a modifier.
func isModifierKey(name fyne.KeyName) bool {
	switch name {
	case desktop.KeyControlLeft, desktop.KeyControlRight,
		desktop.KeyAltLeft, desktop.KeyAltRight,
		desktop.KeyShiftLeft, desktop.KeyShiftRight,
		desktop.KeySuperLeft, desktop.KeySuperRight:
		return true
	default:
		return false
	}
}
