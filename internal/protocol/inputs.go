// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/binary"
	"fmt"
)

// MakeScancodeCode builds the 32-bit KeyCode field for KEY_DOWN / KEY_UP.
//
// scancode uses the spice-gtk convention: low 8 bits are the PC XT / set-1
// make code; bit 0x100 marks an 0xe0 extended prefix (e.g. Right Ctrl = 0x11d).
// When release is true the break bit (0x80) is set on the make code.
//
// Wire layout (little-endian uint32):
//   - plain:    [code, 0, 0, 0]
//   - extended: [0xe0, code, 0, 0]
//
// Matches spice_make_scancode() in spice-gtk.
func MakeScancodeCode(scancode uint16, release bool) uint32 {
	sc := uint32(scancode) & 0x37f
	if release {
		sc |= 0x80
	}
	if sc < 0x100 {
		return sc
	}
	// LE bytes e0, (sc-0x100), 0, 0  →  uint32  0xe0 | ((sc-0x100)<<8)
	return 0xe0 | ((sc - 0x100) << 8)
}

// EncodeKeyDown encodes SPICE_MSGC_INPUTS_KEY_DOWN body (uint32 code).
func EncodeKeyDown(code uint32) []byte {
	buf := make([]byte, KeyCodeSize)
	binary.LittleEndian.PutUint32(buf, code)
	return buf
}

// EncodeKeyUp encodes SPICE_MSGC_INPUTS_KEY_UP body (uint32 code).
func EncodeKeyUp(code uint32) []byte {
	return EncodeKeyDown(code)
}

// DecodeKeyCode parses a KEY_DOWN / KEY_UP body.
func DecodeKeyCode(b []byte) (uint32, error) {
	if len(b) < KeyCodeSize {
		return 0, fmt.Errorf("spice: key code short: %d want %d", len(b), KeyCodeSize)
	}
	return binary.LittleEndian.Uint32(b[:KeyCodeSize]), nil
}

// EncodeKeyModifiers encodes SPICE_MSGC_INPUTS_KEY_MODIFIERS / server LED body.
func EncodeKeyModifiers(modifiers uint16) []byte {
	buf := make([]byte, KeyModifiersSize)
	binary.LittleEndian.PutUint16(buf, modifiers)
	return buf
}

// DecodeKeyModifiers parses flags16 keyboard modifiers / LED state.
func DecodeKeyModifiers(b []byte) (uint16, error) {
	if len(b) < KeyModifiersSize {
		return 0, fmt.Errorf("spice: key modifiers short: %d want %d", len(b), KeyModifiersSize)
	}
	return binary.LittleEndian.Uint16(b[:KeyModifiersSize]), nil
}

// MouseMotion is SpiceMsgcMouseMotion (relative; SERVER mouse mode).
type MouseMotion struct {
	DX           int32
	DY           int32
	ButtonsState uint16
}

// Encode serializes SpiceMsgcMouseMotion (10 bytes).
func (m MouseMotion) Encode() []byte {
	buf := make([]byte, MouseMotionSize)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(m.DX))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(m.DY))
	binary.LittleEndian.PutUint16(buf[8:10], m.ButtonsState)
	return buf
}

// DecodeMouseMotion parses a SpiceMsgcMouseMotion body.
func DecodeMouseMotion(b []byte) (MouseMotion, error) {
	if len(b) < MouseMotionSize {
		return MouseMotion{}, fmt.Errorf("spice: mouse motion short: %d want %d", len(b), MouseMotionSize)
	}
	return MouseMotion{
		DX:           int32(binary.LittleEndian.Uint32(b[0:4])),
		DY:           int32(binary.LittleEndian.Uint32(b[4:8])),
		ButtonsState: binary.LittleEndian.Uint16(b[8:10]),
	}, nil
}

// MousePosition is SpiceMsgcMousePosition (absolute; CLIENT mouse mode).
type MousePosition struct {
	X            uint32
	Y            uint32
	ButtonsState uint16
	DisplayID    uint8
}

// Encode serializes SpiceMsgcMousePosition (11 bytes).
func (m MousePosition) Encode() []byte {
	buf := make([]byte, MousePositionSize)
	binary.LittleEndian.PutUint32(buf[0:4], m.X)
	binary.LittleEndian.PutUint32(buf[4:8], m.Y)
	binary.LittleEndian.PutUint16(buf[8:10], m.ButtonsState)
	buf[10] = m.DisplayID
	return buf
}

// DecodeMousePosition parses a SpiceMsgcMousePosition body.
func DecodeMousePosition(b []byte) (MousePosition, error) {
	if len(b) < MousePositionSize {
		return MousePosition{}, fmt.Errorf("spice: mouse position short: %d want %d", len(b), MousePositionSize)
	}
	return MousePosition{
		X:            binary.LittleEndian.Uint32(b[0:4]),
		Y:            binary.LittleEndian.Uint32(b[4:8]),
		ButtonsState: binary.LittleEndian.Uint16(b[8:10]),
		DisplayID:    b[10],
	}, nil
}

// MouseButtonEvent is SpiceMsgcMousePress / SpiceMsgcMouseRelease.
type MouseButtonEvent struct {
	Button       uint8
	ButtonsState uint16
}

// Encode serializes press/release body (3 bytes: enum8 + flags16).
func (m MouseButtonEvent) Encode() []byte {
	buf := make([]byte, MouseButtonEventSize)
	buf[0] = m.Button
	binary.LittleEndian.PutUint16(buf[1:3], m.ButtonsState)
	return buf
}

// DecodeMouseButtonEvent parses a press/release body.
func DecodeMouseButtonEvent(b []byte) (MouseButtonEvent, error) {
	if len(b) < MouseButtonEventSize {
		return MouseButtonEvent{}, fmt.Errorf("spice: mouse button event short: %d want %d", len(b), MouseButtonEventSize)
	}
	return MouseButtonEvent{
		Button:       b[0],
		ButtonsState: binary.LittleEndian.Uint16(b[1:3]),
	}, nil
}

// MainMouseMode is SpiceMsgMainMouseMode (server → client mode change).
//
// Wire: flags16 supported_modes + flags16 current_mode (4 bytes).
// Note: MAIN_INIT still carries mouse modes as uint32 fields.
type MainMouseMode struct {
	Supported uint16
	Current   uint16
}

// Encode serializes SPICE_MSG_MAIN_MOUSE_MODE body.
func (m MainMouseMode) Encode() []byte {
	buf := make([]byte, MainMouseModeSize)
	binary.LittleEndian.PutUint16(buf[0:2], m.Supported)
	binary.LittleEndian.PutUint16(buf[2:4], m.Current)
	return buf
}

// DecodeMainMouseMode parses SPICE_MSG_MAIN_MOUSE_MODE body.
func DecodeMainMouseMode(b []byte) (MainMouseMode, error) {
	if len(b) < MainMouseModeSize {
		return MainMouseMode{}, fmt.Errorf("spice: MAIN_MOUSE_MODE short: %d want %d", len(b), MainMouseModeSize)
	}
	return MainMouseMode{
		Supported: binary.LittleEndian.Uint16(b[0:2]),
		Current:   binary.LittleEndian.Uint16(b[2:4]),
	}, nil
}

// EncodeMouseModeRequest encodes SPICE_MSGC_MAIN_MOUSE_MODE_REQUEST body (flags16).
func EncodeMouseModeRequest(mode uint16) []byte {
	buf := make([]byte, MainMouseModeReqSize)
	binary.LittleEndian.PutUint16(buf, mode)
	return buf
}

// DecodeMouseModeRequest parses SPICE_MSGC_MAIN_MOUSE_MODE_REQUEST body.
func DecodeMouseModeRequest(b []byte) (uint16, error) {
	if len(b) < MainMouseModeReqSize {
		return 0, fmt.Errorf("spice: mouse mode request short: %d want %d", len(b), MainMouseModeReqSize)
	}
	return binary.LittleEndian.Uint16(b[:MainMouseModeReqSize]), nil
}

// PreferMouseMode chooses CLIENT when the server supports it, else SERVER.
// supported is a bit mask of MouseModeServer|MouseModeClient (from MAIN_INIT
// or MAIN_MOUSE_MODE). Returns 0 if neither is available.
func PreferMouseMode(supported uint32) uint32 {
	if supported&MouseModeClient != 0 {
		return MouseModeClient
	}
	if supported&MouseModeServer != 0 {
		return MouseModeServer
	}
	return 0
}

// ButtonMaskFor returns the buttons_state bit for a mouse button id, or 0.
func ButtonMaskFor(button uint8) uint16 {
	switch button {
	case MouseButtonLeft:
		return MouseButtonMaskLeft
	case MouseButtonMiddle:
		return MouseButtonMaskMiddle
	case MouseButtonRight:
		return MouseButtonMaskRight
	case MouseButtonUp:
		return MouseButtonMaskUp
	case MouseButtonDown:
		return MouseButtonMaskDown
	case MouseButtonSide:
		return MouseButtonMaskSide
	case MouseButtonExtra:
		return MouseButtonMaskExtra
	default:
		return 0
	}
}
