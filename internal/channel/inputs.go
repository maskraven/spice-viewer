// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel

import (
	"fmt"
	"io"
	"sync"

	"github.com/maskraven/virt-viewer/internal/protocol"
)

// Inputs is a client-side SPICE inputs channel helper.
//
// It tracks mouse mode (CLIENT absolute vs SERVER relative), button mask, and
// LED modifiers, and encodes client→server messages for keyboard and mouse.
// UI layers call KeyDown/KeyUp, MouseMove, and MouseButton; the session layer
// feeds server messages via HandleMessage and mouse-mode updates via SetMouseMode.
//
// Inputs is safe for concurrent use.
//
// Wire encoding follows spice-protocol (spice.proto InputsChannel) and does not
// require QEMU for unit tests — pass any io.Writer (e.g. bytes.Buffer).
//
// Optional integration against a live QEMU SPICE server can be exercised under a
// separate build tag in later PRs; this package stays pure unit-testable.
type Inputs struct {
	mu sync.Mutex

	w io.Writer

	// Mouse mode: protocol.MouseModeClient or protocol.MouseModeServer.
	supported uint32
	mode      uint32

	buttons   uint16 // buttons_state mask
	modifiers uint16 // LED state from server (caps/num/scroll)
	displayID uint8  // display channel id for absolute position

	// motionCount tracks unacked motion/position messages for flood control
	// (server sends MOTION_ACK every InputMotionAckBunch messages).
	motionCount int
}

// NewInputs builds an Inputs helper writing mini-header framed messages to w.
// Default mouse mode is SERVER until SetMouseMode is called from MAIN_INIT.
// displayID is the display channel instance used for absolute position messages.
func NewInputs(w io.Writer, displayID uint8) *Inputs {
	return &Inputs{
		w:         w,
		mode:      protocol.MouseModeServer,
		displayID: displayID,
	}
}

// SetWriter replaces the underlying writer (e.g. after reconnect). Optional.
func (in *Inputs) SetWriter(w io.Writer) {
	if in == nil {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	in.w = w
}

// SetDisplayID sets the display channel id sent with absolute mouse positions.
func (in *Inputs) SetDisplayID(id uint8) {
	if in == nil {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	in.displayID = id
}

// SetMouseMode records supported modes and the active mode (from MAIN_INIT
// uint32 fields or SPICE_MSG_MAIN_MOUSE_MODE). Prefer CLIENT when requesting
// a mode change via RequestPreferredMouseMode.
func (in *Inputs) SetMouseMode(supported, current uint32) {
	if in == nil {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	in.supported = supported
	if current != 0 {
		in.mode = current
	}
}

// MouseMode returns the current active mouse mode bit (CLIENT or SERVER).
func (in *Inputs) MouseMode() uint32 {
	if in == nil {
		return 0
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.mode
}

// SupportedMouseModes returns the server-advertised mode mask.
func (in *Inputs) SupportedMouseModes() uint32 {
	if in == nil {
		return 0
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.supported
}

// Modifiers returns the last keyboard LED state from the server.
func (in *Inputs) Modifiers() uint16 {
	if in == nil {
		return 0
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.modifiers
}

// Buttons returns the current buttons_state mask.
func (in *Inputs) Buttons() uint16 {
	if in == nil {
		return 0
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.buttons
}

// RequestPreferredMouseMode writes SPICE_MSGC_MAIN_MOUSE_MODE_REQUEST on mainW
// preferring CLIENT when supported. Returns the requested mode (0 if none).
//
// The active mode only changes when the server answers with MAIN_MOUSE_MODE
// (or MAIN_INIT); call SetMouseMode when that arrives.
func (in *Inputs) RequestPreferredMouseMode(mainW io.Writer) (uint32, error) {
	if in == nil {
		return 0, fmt.Errorf("channel: nil Inputs")
	}
	in.mu.Lock()
	supported := in.supported
	in.mu.Unlock()

	want := protocol.PreferMouseMode(supported)
	if want == 0 {
		// Fall back to CLIENT request even without a prior mask (server may accept).
		want = protocol.MouseModeClient
	}
	if mainW == nil {
		return 0, fmt.Errorf("channel: nil main writer")
	}
	body := protocol.EncodeMouseModeRequest(uint16(want))
	if err := protocol.WriteMessage(mainW, protocol.MsgcMainMouseModeRequest, body); err != nil {
		return 0, err
	}
	return want, nil
}

// HandleMessage processes a server→client inputs-channel message.
// Unknown types are ignored (Phase 1).
func (in *Inputs) HandleMessage(msg protocol.Message) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	switch msg.Type {
	case protocol.MsgInputsInit, protocol.MsgInputsKeyModifiers:
		mods, err := protocol.DecodeKeyModifiers(msg.Data)
		if err != nil {
			return err
		}
		in.mu.Lock()
		in.modifiers = mods
		in.mu.Unlock()
		return nil

	case protocol.MsgInputsMouseMotionAck:
		in.mu.Lock()
		in.motionCount -= protocol.InputMotionAckBunch
		if in.motionCount < 0 {
			in.motionCount = 0
		}
		in.mu.Unlock()
		return nil

	default:
		return nil
	}
}

// HandleMainMouseMode applies SPICE_MSG_MAIN_MOUSE_MODE from the main channel.
func (in *Inputs) HandleMainMouseMode(msg protocol.Message) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	if msg.Type != protocol.MsgMainMouseMode {
		return fmt.Errorf("channel: unexpected main message type %d", msg.Type)
	}
	mm, err := protocol.DecodeMainMouseMode(msg.Data)
	if err != nil {
		return err
	}
	in.SetMouseMode(uint32(mm.Supported), uint32(mm.Current))
	return nil
}

// KeyDown injects a key-press using a PC XT set-1 scancode (spice-gtk form).
func (in *Inputs) KeyDown(scancode uint16) error {
	return in.sendKey(scancode, false)
}

// KeyUp injects a key-release using a PC XT set-1 scancode (spice-gtk form).
func (in *Inputs) KeyUp(scancode uint16) error {
	return in.sendKey(scancode, true)
}

func (in *Inputs) sendKey(scancode uint16, release bool) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	code := protocol.MakeScancodeCode(scancode, release)
	body := protocol.EncodeKeyDown(code)
	typ := protocol.MsgcInputsKeyDown
	if release {
		typ = protocol.MsgcInputsKeyUp
		body = protocol.EncodeKeyUp(code)
	}
	in.mu.Lock()
	w := in.w
	in.mu.Unlock()
	if w == nil {
		return fmt.Errorf("channel: inputs writer is nil")
	}
	return protocol.WriteMessage(w, typ, body)
}

// MouseMove injects mouse movement according to the current mouse mode:
//   - CLIENT mode: x,y are absolute desktop coordinates (uint32 range)
//   - SERVER mode: x,y are relative deltas (int32)
//
// Negative absolute coordinates are clamped to 0 in CLIENT mode.
func (in *Inputs) MouseMove(x, y int32) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	in.mu.Lock()
	mode := in.mode
	buttons := in.buttons
	displayID := in.displayID
	// Drop motion if too many unacked (same policy as spice-gtk: 2× bunch).
	if in.motionCount >= protocol.InputMotionAckBunch*2 {
		in.mu.Unlock()
		return nil
	}
	in.motionCount++
	w := in.w
	in.mu.Unlock()

	if w == nil {
		return fmt.Errorf("channel: inputs writer is nil")
	}

	// current_mouse_mode is a single mode value (SERVER=1 or CLIENT=2), not a mask.
	if mode == protocol.MouseModeClient {
		ax, ay := x, y
		if ax < 0 {
			ax = 0
		}
		if ay < 0 {
			ay = 0
		}
		body := protocol.MousePosition{
			X:            uint32(ax),
			Y:            uint32(ay),
			ButtonsState: buttons,
			DisplayID:    displayID,
		}.Encode()
		return protocol.WriteMessage(w, protocol.MsgcInputsMousePosition, body)
	}

	body := protocol.MouseMotion{
		DX:           x,
		DY:           y,
		ButtonsState: buttons,
	}.Encode()
	return protocol.WriteMessage(w, protocol.MsgcInputsMouseMotion, body)
}

// MousePosition sends an absolute position (CLIENT mode message) regardless of
// the current mode. Prefer MouseMove for mode-aware injection.
func (in *Inputs) MousePosition(x, y uint32) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	in.mu.Lock()
	buttons := in.buttons
	displayID := in.displayID
	if in.motionCount >= protocol.InputMotionAckBunch*2 {
		in.mu.Unlock()
		return nil
	}
	in.motionCount++
	w := in.w
	in.mu.Unlock()
	if w == nil {
		return fmt.Errorf("channel: inputs writer is nil")
	}
	body := protocol.MousePosition{
		X: x, Y: y, ButtonsState: buttons, DisplayID: displayID,
	}.Encode()
	return protocol.WriteMessage(w, protocol.MsgcInputsMousePosition, body)
}

// MouseMotion sends a relative motion (SERVER mode message) regardless of mode.
func (in *Inputs) MouseMotion(dx, dy int32) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	in.mu.Lock()
	buttons := in.buttons
	if in.motionCount >= protocol.InputMotionAckBunch*2 {
		in.mu.Unlock()
		return nil
	}
	in.motionCount++
	w := in.w
	in.mu.Unlock()
	if w == nil {
		return fmt.Errorf("channel: inputs writer is nil")
	}
	body := protocol.MouseMotion{DX: dx, DY: dy, ButtonsState: buttons}.Encode()
	return protocol.WriteMessage(w, protocol.MsgcInputsMouseMotion, body)
}

// MouseButton injects a button press (pressed=true) or release (pressed=false).
// button is one of protocol.MouseButtonLeft/Middle/Right/Up/Down/….
// Wheel events should use MouseButtonUp/Down as press+release pairs (or MouseWheel).
func (in *Inputs) MouseButton(button uint8, pressed bool) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	mask := protocol.ButtonMaskFor(button)

	in.mu.Lock()
	if pressed {
		in.buttons |= mask
	} else {
		in.buttons &^= mask
	}
	buttons := in.buttons
	w := in.w
	in.mu.Unlock()

	if w == nil {
		return fmt.Errorf("channel: inputs writer is nil")
	}
	body := protocol.MouseButtonEvent{Button: button, ButtonsState: buttons}.Encode()
	typ := protocol.MsgcInputsMousePress
	if !pressed {
		typ = protocol.MsgcInputsMouseRelease
	}
	return protocol.WriteMessage(w, typ, body)
}

// MouseWheel sends a single wheel click (press+release of UP or DOWN button).
// delta > 0 scrolls up; delta < 0 scrolls down; |delta| is the number of clicks.
func (in *Inputs) MouseWheel(delta int) error {
	if delta == 0 {
		return nil
	}
	btn := protocol.MouseButtonUp
	n := delta
	if delta < 0 {
		btn = protocol.MouseButtonDown
		n = -delta
	}
	for i := 0; i < n; i++ {
		if err := in.MouseButton(btn, true); err != nil {
			return err
		}
		if err := in.MouseButton(btn, false); err != nil {
			return err
		}
	}
	return nil
}

// SendKeyModifiers sends SPICE_MSGC_INPUTS_KEY_MODIFIERS (client LED state).
func (in *Inputs) SendKeyModifiers(modifiers uint16) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	in.mu.Lock()
	w := in.w
	in.mu.Unlock()
	if w == nil {
		return fmt.Errorf("channel: inputs writer is nil")
	}
	return protocol.WriteMessage(w, protocol.MsgcInputsKeyModifiers, protocol.EncodeKeyModifiers(modifiers))
}
