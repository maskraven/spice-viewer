// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

// PC XT / set-1 scancodes (spice-gtk form): low 8 bits are the make code;
// bit 0x100 marks an 0xe0 prefix (e.g. Delete = 0x153).
//
// Duplicated here so internal/ui does not import internal/channel (importlint).
const (
	scanE0 uint16 = 0x100

	scanLCtrl  uint16 = 0x1d
	scanRCtrl  uint16 = scanE0 | 0x1d
	scanLAlt   uint16 = 0x38
	scanRAlt   uint16 = scanE0 | 0x38
	scanLShift uint16 = 0x2a
	scanRShift uint16 = 0x36
	scanLGUI   uint16 = scanE0 | 0x5b
	scanRGUI   uint16 = scanE0 | 0x5c

	scanDelete uint16 = scanE0 | 0x53
	scanInsert uint16 = scanE0 | 0x52
	scanEscape uint16 = 0x01
	scanTab    uint16 = 0x0f
	scanEnter  uint16 = 0x1c
	scanSpace  uint16 = 0x39
	scanBack   uint16 = 0x0e

	scanF1  uint16 = 0x3b
	scanF2  uint16 = 0x3c
	scanF3  uint16 = 0x3d
	scanF4  uint16 = 0x3e
	scanF5  uint16 = 0x3f
	scanF6  uint16 = 0x40
	scanF7  uint16 = 0x41
	scanF8  uint16 = 0x42
	scanF9  uint16 = 0x43
	scanF10 uint16 = 0x44
	scanF11 uint16 = 0x57
	scanF12 uint16 = 0x58

	scanHome     uint16 = scanE0 | 0x47
	scanEnd      uint16 = scanE0 | 0x4f
	scanPageUp   uint16 = scanE0 | 0x49
	scanPageDown uint16 = scanE0 | 0x51
	scanUp       uint16 = scanE0 | 0x48
	scanDown     uint16 = scanE0 | 0x50
	scanLeft     uint16 = scanE0 | 0x4b
	scanRight    uint16 = scanE0 | 0x4d
)

// letterScancode returns the XT scancode for a–z / A–Z, or 0.
func letterScancode(r rune) uint16 {
	if r >= 'A' && r <= 'Z' {
		r = r - 'A' + 'a'
	}
	if r < 'a' || r > 'z' {
		return 0
	}
	return letterTable[r-'a']
}

// digitScancode returns the top-row digit scancode for '0'–'9', or 0.
func digitScancode(r rune) uint16 {
	if r < '0' || r > '9' {
		return 0
	}
	return digitTable[r-'0']
}

var letterTable = [26]uint16{
	0x1e, 0x30, 0x2e, 0x20, 0x12, 0x21, 0x22, 0x23, 0x17, 0x24, // a-j
	0x25, 0x26, 0x32, 0x31, 0x18, 0x19, 0x10, 0x13, 0x1f, 0x14, // k-t
	0x16, 0x2f, 0x11, 0x2d, 0x15, 0x2c, // u-z
}

var digitTable = [10]uint16{
	0x0b, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, // 0-9
}
