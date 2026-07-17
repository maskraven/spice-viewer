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
	case fyne.KeyReturn, fyne.KeyEnter:
		return "enter"
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

// fyneKeyScancode maps a Fyne key to an XT scancode for guest injection.
func fyneKeyScancode(name fyne.KeyName) uint16 {
	switch name {
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
	case fyne.KeyInsert:
		return scanInsert
	case fyne.KeyDelete:
		return scanDelete
	case fyne.KeyEscape:
		return scanEscape
	case fyne.KeyReturn, fyne.KeyEnter:
		return scanEnter
	case fyne.KeyTab:
		return scanTab
	case fyne.KeySpace:
		return scanSpace
	case fyne.KeyBackspace:
		return scanBack
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
	// US punctuation (physical keys; shift is a separate event).
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
	default:
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
