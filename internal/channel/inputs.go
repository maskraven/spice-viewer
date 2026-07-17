// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/maskraven/spice-viewer/internal/protocol"
)

// Inputs is a client-side SPICE inputs channel helper.
//
// It tracks mouse mode (CLIENT absolute vs SERVER relative), button mask, and
// LED modifiers, and encodes client→server messages for keyboard and mouse.
// UI layers call KeyDown/KeyUp, MouseMove, and MouseButton; the session layer
// feeds server messages via HandleMessage and mouse-mode updates via SetMouseMode.
//
// Inputs is safe for concurrent use: all state updates and writes to the
// inputs writer are serialized under an internal mutex (mini-header framing
// cannot interleave).
//
// Wire encoding follows spice-protocol (spice.proto InputsChannel) and does not
// require QEMU for unit tests — pass any io.Writer (e.g. bytes.Buffer).
//
// QEMU typing smoke / live SPICE integration is deferred to a later PR
// (//go:build integration or session/UI smoke). This package remains pure
// unit-testable without a guest.
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

	// Pending relative motion (SERVER mode), spice-gtk style coalesce.
	pendingDX int32
	pendingDY int32

	// Pending absolute position (CLIENT mode); havePendingPos means "dirty".
	pendingX       uint32
	pendingY       uint32
	havePendingPos bool

	ack protocol.AckState
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
// Callers must SetMouseMode first (from MAIN_INIT). If supported is empty,
// returns (0, error) and does not write.
//
// The active mode only changes when the server answers with MAIN_MOUSE_MODE
// (or MAIN_INIT); call SetMouseMode when that arrives.
func (in *Inputs) RequestPreferredMouseMode(mainW io.Writer) (uint32, error) {
	if in == nil {
		return 0, fmt.Errorf("channel: nil Inputs")
	}
	if mainW == nil {
		return 0, fmt.Errorf("channel: nil main writer")
	}
	in.mu.Lock()
	supported := in.supported
	in.mu.Unlock()

	want := protocol.PreferMouseMode(supported)
	if want == 0 {
		return 0, fmt.Errorf("channel: no supported mouse modes (call SetMouseMode first)")
	}
	body := protocol.EncodeMouseModeRequest(uint16(want))
	// mainW is a separate connection; serialize only via caller's use of main.
	if err := protocol.WriteMessage(mainW, protocol.MsgcMainMouseModeRequest, body); err != nil {
		return 0, err
	}
	return want, nil
}

// HandleMessage processes a server→client inputs-channel message.
// Unknown types are ignored (Phase 1).
//
// On MOTION_ACK, decrements the unacked motion budget and flushes any pending
// coalesced relative motion and/or absolute position (spice-gtk behavior).
func (in *Inputs) HandleMessage(msg protocol.Message) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	switch msg.Type {
	case protocol.MsgSetAck:
		if in.w == nil {
			return nil
		}
		return in.ack.OnSetAck(in.w, msg.Data)
	case protocol.MsgPing:
		if in.w == nil {
			return nil
		}
		return protocol.WriteMessage(in.w, protocol.MsgcPong, msg.Data)
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
		defer in.mu.Unlock()
		in.motionCount -= protocol.InputMotionAckBunch
		if in.motionCount < 0 {
			in.motionCount = 0
		}
		// Flush pending relative then absolute (spice-gtk inputs_handle_ack).
		if err := in.flushMotionLocked(); err != nil {
			return err
		}
		return in.flushPositionLocked()

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
	typ := protocol.MsgcInputsKeyDown
	body := protocol.EncodeKeyDown(code)
	if release {
		typ = protocol.MsgcInputsKeyUp
		body = protocol.EncodeKeyUp(code)
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.writeMsgLocked(typ, body)
}

// MouseMove injects mouse movement according to the current mouse mode:
//   - CLIENT mode: x,y are absolute desktop coordinates (uint32 range)
//   - SERVER mode: x,y are relative deltas (int32); zero deltas are ignored
//
// Negative absolute coordinates are clamped to 0 in CLIENT mode.
//
// Flood control matches spice-gtk: at most InputMotionAckBunch*2 unacked
// motion/position messages. Excess samples are coalesced (relative deltas
// summed; absolute keeps the latest position) and flushed on MOTION_ACK.
func (in *Inputs) MouseMove(x, y int32) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	in.mu.Lock()
	defer in.mu.Unlock()

	// current_mouse_mode is a single mode value (SERVER=1 or CLIENT=2), not a mask.
	if in.mode == protocol.MouseModeClient {
		ax, ay := x, y
		if ax < 0 {
			ax = 0
		}
		if ay < 0 {
			ay = 0
		}
		in.pendingX = uint32(ax)
		in.pendingY = uint32(ay)
		in.havePendingPos = true
		return in.flushPositionLocked()
	}

	// SERVER (default): relative motion; skip no-ops (spice-gtk).
	if x == 0 && y == 0 {
		return nil
	}
	in.pendingDX += x
	in.pendingDY += y
	return in.flushMotionLocked()
}

// MousePosition sends an absolute position (CLIENT mode message) regardless of
// the current mode. Prefer MouseMove for mode-aware injection.
// Coalesces when over the motion flood limit; flushed on MOTION_ACK.
func (in *Inputs) MousePosition(x, y uint32) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	in.pendingX = x
	in.pendingY = y
	in.havePendingPos = true
	return in.flushPositionLocked()
}

// MouseMotion sends a relative motion (SERVER mode message) regardless of mode.
// Zero deltas are ignored. Coalesces when over the flood limit.
func (in *Inputs) MouseMotion(dx, dy int32) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	if dx == 0 && dy == 0 {
		return nil
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	in.pendingDX += dx
	in.pendingDY += dy
	return in.flushMotionLocked()
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
	defer in.mu.Unlock()
	if pressed {
		in.buttons |= mask
	} else {
		in.buttons &^= mask
	}
	body := protocol.MouseButtonEvent{Button: button, ButtonsState: in.buttons}.Encode()
	typ := protocol.MsgcInputsMousePress
	if !pressed {
		typ = protocol.MsgcInputsMouseRelease
	}
	return in.writeMsgLocked(typ, body)
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
	defer in.mu.Unlock()
	return in.writeMsgLocked(protocol.MsgcInputsKeyModifiers, protocol.EncodeKeyModifiers(modifiers))
}

// writeMsgLocked writes a mini-header framed message. Caller must hold in.mu.
func (in *Inputs) writeMsgLocked(typ uint16, body []byte) error {
	if in.w == nil {
		return fmt.Errorf("channel: inputs writer is nil")
	}
	return protocol.WriteMessage(in.w, typ, body)
}

// flushMotionLocked sends pending relative motion if under the flood limit.
// Caller must hold in.mu. Matches spice-gtk mouse_motion().
func (in *Inputs) flushMotionLocked() error {
	if in.pendingDX == 0 && in.pendingDY == 0 {
		return nil
	}
	if in.motionCount >= protocol.InputMotionAckBunch*2 {
		// Keep pending deltas for MOTION_ACK flush.
		return nil
	}
	body := protocol.MouseMotion{
		DX:           in.pendingDX,
		DY:           in.pendingDY,
		ButtonsState: in.buttons,
	}.Encode()
	if err := in.writeMsgLocked(protocol.MsgcInputsMouseMotion, body); err != nil {
		return err
	}
	in.motionCount++
	in.pendingDX = 0
	in.pendingDY = 0
	return nil
}

// flushPositionLocked sends pending absolute position if under the flood limit.
// Caller must hold in.mu. Matches spice-gtk mouse_position().
func (in *Inputs) flushPositionLocked() error {
	if !in.havePendingPos {
		return nil
	}
	if in.motionCount >= protocol.InputMotionAckBunch*2 {
		// Keep latest absolute sample for MOTION_ACK flush.
		return nil
	}
	body := protocol.MousePosition{
		X:            in.pendingX,
		Y:            in.pendingY,
		ButtonsState: in.buttons,
		DisplayID:    in.displayID,
	}.Encode()
	if err := in.writeMsgLocked(protocol.MsgcInputsMousePosition, body); err != nil {
		return err
	}
	in.motionCount++
	in.havePendingPos = false
	return nil
}

// Run reads server→client mini-header messages from r until ctx cancel or EOF.
//
// r is typically the same net.Conn used as the writer in NewInputs. Fatal I/O
// errors and context cancel stop the loop (session-fatal for the inputs channel).
// HandleMessage errors (e.g. short key-modifier bodies) are returned as fatal.
func (in *Inputs) Run(ctx context.Context, r io.Reader) error {
	if in == nil {
		return fmt.Errorf("channel: nil Inputs")
	}
	if r == nil {
		return fmt.Errorf("channel: inputs: nil reader")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		msg, err := protocol.ReadMessage(r)
		if err != nil {
			if err == io.EOF || isClosedConn(err) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		// Writer for acks: Inputs.w is the channel conn.
		if w, ok := r.(io.Writer); ok {
			if err := in.ack.AfterRead(w); err != nil {
				return err
			}
		} else if in.w != nil {
			if err := in.ack.AfterRead(in.w); err != nil {
				return err
			}
		}
		if err := in.HandleMessage(msg); err != nil {
			return err
		}
	}
}
