// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel

import "github.com/maskraven/spice-viewer/internal/protocol"

// PC XT / set-1 scancodes for common keys, using the spice-gtk convention:
// low 8 bits are the make code; bit 0x100 marks an 0xe0 prefix
// (e.g. Right Ctrl = 0x11d → wire bytes e0 1d).
//
// Values match QEMU/SPICE inputs expectations (same table spice-gtk uses for
// PC XT set-1). Extended keys are OR'd with ScanE0.
const (
	ScanE0 uint16 = 0x100 // 0xe0 prefix marker (spice-gtk)

	// Letters (US QWERTY).
	ScanA uint16 = 0x1e
	ScanB uint16 = 0x30
	ScanC uint16 = 0x2e
	ScanD uint16 = 0x20
	ScanE uint16 = 0x12
	ScanF uint16 = 0x21
	ScanG uint16 = 0x22
	ScanH uint16 = 0x23
	ScanI uint16 = 0x17
	ScanJ uint16 = 0x24
	ScanK uint16 = 0x25
	ScanL uint16 = 0x26
	ScanM uint16 = 0x32
	ScanN uint16 = 0x31
	ScanO uint16 = 0x18
	ScanP uint16 = 0x19
	ScanQ uint16 = 0x10
	ScanR uint16 = 0x13
	ScanS uint16 = 0x1f
	ScanT uint16 = 0x14
	ScanU uint16 = 0x16
	ScanV uint16 = 0x2f
	ScanW uint16 = 0x11
	ScanX uint16 = 0x2d
	ScanY uint16 = 0x15
	ScanZ uint16 = 0x2c

	// Digits (top row).
	Scan1 uint16 = 0x02
	Scan2 uint16 = 0x03
	Scan3 uint16 = 0x04
	Scan4 uint16 = 0x05
	Scan5 uint16 = 0x06
	Scan6 uint16 = 0x07
	Scan7 uint16 = 0x08
	Scan8 uint16 = 0x09
	Scan9 uint16 = 0x0a
	Scan0 uint16 = 0x0b

	// Control / whitespace / punctuation.
	ScanEscape    uint16 = 0x01
	ScanBackspace uint16 = 0x0e
	ScanTab       uint16 = 0x0f
	ScanEnter     uint16 = 0x1c
	ScanSpace     uint16 = 0x39
	ScanCapsLock  uint16 = 0x3a
	ScanMinus     uint16 = 0x0c // -
	ScanEquals    uint16 = 0x0d // =
	ScanLBracket  uint16 = 0x1a // [
	ScanRBracket  uint16 = 0x1b // ]
	ScanBackslash uint16 = 0x2b
	ScanSemicolon uint16 = 0x27
	ScanQuote     uint16 = 0x28
	ScanGrave     uint16 = 0x29 // `
	ScanComma     uint16 = 0x33
	ScanDot       uint16 = 0x34
	ScanSlash     uint16 = 0x35

	// Modifiers.
	ScanLShift uint16 = 0x2a
	ScanRShift uint16 = 0x36
	ScanLCtrl  uint16 = 0x1d
	ScanRCtrl  uint16 = ScanE0 | 0x1d
	ScanLAlt   uint16 = 0x38
	ScanRAlt   uint16 = ScanE0 | 0x38 // AltGr
	ScanLGUI   uint16 = ScanE0 | 0x5b // Left Meta / Win
	ScanRGUI   uint16 = ScanE0 | 0x5c
	ScanMenu   uint16 = ScanE0 | 0x5d // App / Menu

	// Function keys.
	ScanF1  uint16 = 0x3b
	ScanF2  uint16 = 0x3c
	ScanF3  uint16 = 0x3d
	ScanF4  uint16 = 0x3e
	ScanF5  uint16 = 0x3f
	ScanF6  uint16 = 0x40
	ScanF7  uint16 = 0x41
	ScanF8  uint16 = 0x42
	ScanF9  uint16 = 0x43
	ScanF10 uint16 = 0x44
	ScanF11 uint16 = 0x57
	ScanF12 uint16 = 0x58

	// Navigation (extended).
	ScanInsert   uint16 = ScanE0 | 0x52
	ScanDelete   uint16 = ScanE0 | 0x53
	ScanHome     uint16 = ScanE0 | 0x47
	ScanEnd      uint16 = ScanE0 | 0x4f
	ScanPageUp   uint16 = ScanE0 | 0x49
	ScanPageDown uint16 = ScanE0 | 0x51
	ScanUp       uint16 = ScanE0 | 0x48
	ScanLeft     uint16 = ScanE0 | 0x4b
	ScanDown     uint16 = ScanE0 | 0x50
	ScanRight    uint16 = ScanE0 | 0x4d

	// Keypad / locks.
	ScanNumLock    uint16 = 0x45
	ScanScrollLock uint16 = 0x46
	ScanKP7        uint16 = 0x47
	ScanKP8        uint16 = 0x48
	ScanKP9        uint16 = 0x49
	ScanKPMinus    uint16 = 0x4a
	ScanKP4        uint16 = 0x4b
	ScanKP5        uint16 = 0x4c
	ScanKP6        uint16 = 0x4d
	ScanKPPlus     uint16 = 0x4e
	ScanKP1        uint16 = 0x4f
	ScanKP2        uint16 = 0x50
	ScanKP3        uint16 = 0x51
	ScanKP0        uint16 = 0x52
	ScanKPDot      uint16 = 0x53
	ScanKPEnter    uint16 = ScanE0 | 0x1c
	ScanKPSlash    uint16 = ScanE0 | 0x35
	ScanKPStar     uint16 = 0x37
)

// MakeScancodeCode is a thin alias of protocol.MakeScancodeCode for channel users.
func MakeScancodeCode(scancode uint16, release bool) uint32 {
	return protocol.MakeScancodeCode(scancode, release)
}

// LetterScancode returns the XT scancode for a lowercase or uppercase English
// letter ('a'–'z' / 'A'–'Z'). Returns 0 if r is not a letter.
func LetterScancode(r rune) uint16 {
	if r >= 'A' && r <= 'Z' {
		r = r - 'A' + 'a'
	}
	if r < 'a' || r > 'z' {
		return 0
	}
	return letterTable[r-'a']
}

// DigitScancode returns the top-row digit scancode for '0'–'9'. Returns 0 otherwise.
func DigitScancode(r rune) uint16 {
	if r < '0' || r > '9' {
		return 0
	}
	return digitTable[r-'0']
}

var letterTable = [26]uint16{
	ScanA, ScanB, ScanC, ScanD, ScanE, ScanF, ScanG, ScanH, ScanI, ScanJ,
	ScanK, ScanL, ScanM, ScanN, ScanO, ScanP, ScanQ, ScanR, ScanS, ScanT,
	ScanU, ScanV, ScanW, ScanX, ScanY, ScanZ,
}

// digitTable maps '0'..'9' → scancode (note: '0' is 0x0b, '1' is 0x02, …).
var digitTable = [10]uint16{
	Scan0, Scan1, Scan2, Scan3, Scan4, Scan5, Scan6, Scan7, Scan8, Scan9,
}
