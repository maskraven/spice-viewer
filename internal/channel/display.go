// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/maskraven/virt-viewer/internal/codec"
	"github.com/maskraven/virt-viewer/internal/display"
	"github.com/maskraven/virt-viewer/internal/protocol"
)

// Display is the Phase-1 SPICE display channel reader/handler.
//
// It consumes mini-header framed messages on a linked display connection,
// applies allowlisted draw ops to a compositor, and ignores unsupported types.
//
// Session may already own DisplayConn after OpenChannels — construct a Display
// with that conn and call Run (or HandleMessage for unit tests).
type Display struct {
	conn net.Conn
	comp *display.Compositor

	mu       sync.Mutex
	unknown  map[uint16]int // debug counts of ignored types
	initSent bool
}

// NewDisplay wraps a linked display-channel connection and compositor.
// conn must already be past link auth (mini-header mode).
// comp must be non-nil.
func NewDisplay(conn net.Conn, comp *display.Compositor) *Display {
	if comp == nil {
		comp = display.NewCompositor(nil)
	}
	return &Display{
		conn:    conn,
		comp:    comp,
		unknown: make(map[uint16]int),
	}
}

// Compositor returns the underlying compositor.
func (d *Display) Compositor() *display.Compositor {
	return d.comp
}

// SendInit writes SPICE_MSGC_DISPLAY_INIT (14 zero bytes = default caches).
// Safe to call once before or at the start of Run.
func (d *Display) SendInit() error {
	d.mu.Lock()
	if d.initSent {
		d.mu.Unlock()
		return nil
	}
	d.initSent = true
	d.mu.Unlock()

	body := make([]byte, protocol.DisplayInitBodySize)
	return protocol.WriteMessage(d.conn, protocol.MsgcDisplayInit, body)
}

// Run sends DISPLAY_INIT then reads and handles messages until ctx cancel,
// conn close, or a fatal decode/compositor error.
func (d *Display) Run(ctx context.Context) error {
	if d == nil || d.conn == nil {
		return fmt.Errorf("channel: display: nil conn")
	}
	if err := d.SendInit(); err != nil {
		return fmt.Errorf("channel: display init: %w", err)
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		msg, err := protocol.ReadMessage(d.conn)
		if err != nil {
			if err == io.EOF || isClosedConn(err) {
				return err
			}
			// context cancel often surfaces as read deadline / closed
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if err := d.HandleMessage(msg.Type, msg.Data); err != nil {
			return err
		}
	}
}

// HandleMessage dispatches one server→client display message by type.
// Exported for unit tests that inject wire bodies without a socket.
func (d *Display) HandleMessage(typ uint16, data []byte) error {
	// Common channel messages (shared with all channels).
	switch typ {
	case protocol.MsgSetAck:
		return d.handleSetAck(data)
	case protocol.MsgPing:
		return d.handlePing(data)
	case protocol.MsgNotify, protocol.MsgWaitForChannels,
		protocol.MsgMigrate, protocol.MsgMigrateData, protocol.MsgDisconnecting:
		return nil // ignore Phase 1
	}

	if !IsDisplayAllowed(typ) {
		d.noteUnknown(typ)
		return nil
	}

	switch typ {
	case protocol.MsgDisplayMode:
		return d.handleMode(data)
	case protocol.MsgDisplayMark:
		d.comp.Mark()
		return nil
	case protocol.MsgDisplayReset:
		d.comp.Reset()
		return nil
	case protocol.MsgDisplaySurfaceCreate:
		return d.handleSurfaceCreate(data)
	case protocol.MsgDisplaySurfaceDestroy:
		return d.handleSurfaceDestroy(data)
	case protocol.MsgDisplayDrawFill:
		return d.handleDrawFill(data)
	case protocol.MsgDisplayDrawCopy:
		return d.handleDrawCopy(data)
	default:
		// Allowlisted but not yet implemented (should not happen).
		d.noteUnknown(typ)
		return nil
	}
}

// IsDisplayAllowed reports whether typ is in the Phase-1 display allowlist
// (mode, mark, reset, draw_copy, draw_fill, surface create/destroy).
func IsDisplayAllowed(typ uint16) bool {
	switch typ {
	case protocol.MsgDisplayMode,
		protocol.MsgDisplayMark,
		protocol.MsgDisplayReset,
		protocol.MsgDisplayDrawFill,
		protocol.MsgDisplayDrawCopy,
		protocol.MsgDisplaySurfaceCreate,
		protocol.MsgDisplaySurfaceDestroy:
		return true
	default:
		return false
	}
}

func (d *Display) noteUnknown(typ uint16) {
	d.mu.Lock()
	d.unknown[typ]++
	n := d.unknown[typ]
	d.mu.Unlock()
	if n == 1 {
		log.Printf("channel/display: ignoring message type %d", typ)
	}
}

// UnknownCounts returns a copy of ignored-type counters (tests / diagnostics).
func (d *Display) UnknownCounts() map[uint16]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[uint16]int, len(d.unknown))
	for k, v := range d.unknown {
		out[k] = v
	}
	return out
}

func (d *Display) handleSetAck(data []byte) error {
	// SpiceMsgSetAck: generation u32, window u32 — reply with MsgcAckSync(generation)
	if len(data) < 8 || d.conn == nil {
		return nil
	}
	gen := binary.LittleEndian.Uint32(data[0:4])
	var body [4]byte
	binary.LittleEndian.PutUint32(body[:], gen)
	return protocol.WriteMessage(d.conn, protocol.MsgcAckSync, body[:])
}

func (d *Display) handlePing(data []byte) error {
	// Echo body as PONG.
	if d.conn == nil {
		return nil
	}
	return protocol.WriteMessage(d.conn, protocol.MsgcPong, data)
}

func (d *Display) handleMode(data []byte) error {
	// SpiceMsgDisplayMode: x, y, bits, width, height — all uint32 (legacy).
	// Modern servers prefer SURFACE_CREATE; MODE is accepted for allowlist completeness.
	if len(data) < 20 {
		return fmt.Errorf("channel: DISPLAY_MODE short: %d", len(data))
	}
	// width := binary.LittleEndian.Uint32(data[12:16])
	// height := binary.LittleEndian.Uint32(data[16:20])
	// No surface allocation here without format; SURFACE_CREATE is authoritative.
	return nil
}

func (d *Display) handleSurfaceCreate(data []byte) error {
	if len(data) < protocol.SurfaceCreateSize {
		return fmt.Errorf("channel: SURFACE_CREATE short: %d", len(data))
	}
	id := binary.LittleEndian.Uint32(data[0:4])
	w := binary.LittleEndian.Uint32(data[4:8])
	h := binary.LittleEndian.Uint32(data[8:12])
	format := binary.LittleEndian.Uint32(data[12:16])
	flags := binary.LittleEndian.Uint32(data[16:20])
	return d.comp.CreateSurface(id, int(w), int(h), format, flags)
}

func (d *Display) handleSurfaceDestroy(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("channel: SURFACE_DESTROY short: %d", len(data))
	}
	id := binary.LittleEndian.Uint32(data[0:4])
	return d.comp.DestroySurface(id)
}

func (d *Display) handleDrawFill(data []byte) error {
	base, off, err := decodeDisplayBase(data)
	if err != nil {
		return fmt.Errorf("channel: DRAW_FILL base: %w", err)
	}
	if off >= len(data) {
		return fmt.Errorf("channel: DRAW_FILL truncated after base")
	}
	brushType := data[off]
	off++
	var rgba [4]byte
	switch brushType {
	case protocol.BrushTypeNone:
		return nil
	case protocol.BrushTypeSolid:
		if off+4 > len(data) {
			return fmt.Errorf("channel: DRAW_FILL solid color short")
		}
		// SpiceColor LE: R, G, B, A in successive bytes
		c := binary.LittleEndian.Uint32(data[off : off+4])
		rgba[0] = uint8(c)
		rgba[1] = uint8(c >> 8)
		rgba[2] = uint8(c >> 16)
		rgba[3] = uint8(c >> 24)
		if rgba[3] == 0 {
			rgba[3] = 0xff // treat zero alpha solid as opaque
		}
		off += 4
	case protocol.BrushTypePattern:
		log.Printf("channel/display: DRAW_FILL pattern brush not implemented")
		return nil
	default:
		return fmt.Errorf("channel: DRAW_FILL unknown brush type %d", brushType)
	}

	// ropd u16 + QMask (1 + 8 + 4 = 13) — skip for Phase 1 solid put
	if off+2 > len(data) {
		return fmt.Errorf("channel: DRAW_FILL ropd short")
	}
	// ropd := binary.LittleEndian.Uint16(data[off:off+2])
	off += 2
	// QMask: flags u8, pos x u32, pos y u32, image_ptr u32
	if off+13 > len(data) {
		return fmt.Errorf("channel: DRAW_FILL qmask short")
	}
	// ignore mask
	_ = off

	return d.comp.Fill(base.SurfaceID, base.Box, rgba, base.Clips)
}

func (d *Display) handleDrawCopy(data []byte) error {
	base, off, err := decodeDisplayBase(data)
	if err != nil {
		return fmt.Errorf("channel: DRAW_COPY base: %w", err)
	}
	if off+4 > len(data) {
		return fmt.Errorf("channel: DRAW_COPY img ptr short")
	}
	imgPtr := binary.LittleEndian.Uint32(data[off : off+4])
	off += 4

	srcArea, err := decodeRect(data, off)
	if err != nil {
		return fmt.Errorf("channel: DRAW_COPY src_area: %w", err)
	}
	off += 16

	// ropd u16, scale_mode u8, QMask 13 bytes
	if off+2+1+13 > len(data) {
		return fmt.Errorf("channel: DRAW_COPY trailer short")
	}
	// skip ropd, scale_mode, qmask
	_ = off

	if int(imgPtr) > len(data) {
		return fmt.Errorf("channel: DRAW_COPY img ptr %d out of range %d", imgPtr, len(data))
	}
	img, err := codec.DecodeSpiceImage(data[imgPtr:])
	if err != nil {
		return fmt.Errorf("channel: DRAW_COPY image: %w", err)
	}

	srcOrigin := image.Pt(srcArea.Min.X, srcArea.Min.Y)
	// If src_area is empty, use full image at 0,0
	if srcArea.Empty() {
		srcOrigin = image.Pt(0, 0)
		// dest size still from base.Box
	}
	return d.comp.Copy(base.SurfaceID, base.Box, img, srcOrigin, base.Clips)
}

// displayBase is the decoded SpiceMsgDisplayBase prefix of draw messages.
type displayBase struct {
	SurfaceID uint32
	Box       image.Rectangle
	Clips     []image.Rectangle // nil if clip_type=NONE
}

func decodeDisplayBase(data []byte) (displayBase, int, error) {
	// surface_id u32 + rect 16 + clip_type u8
	if len(data) < 4+16+1 {
		return displayBase{}, 0, fmt.Errorf("display base short: %d", len(data))
	}
	var b displayBase
	b.SurfaceID = binary.LittleEndian.Uint32(data[0:4])
	box, err := decodeRect(data, 4)
	if err != nil {
		return displayBase{}, 0, err
	}
	b.Box = box
	off := 20
	clipType := data[off]
	off++
	switch clipType {
	case protocol.ClipTypeNone:
		return b, off, nil
	case protocol.ClipTypeRects:
		if off+4 > len(data) {
			return displayBase{}, 0, fmt.Errorf("clip num_rects short")
		}
		n := binary.LittleEndian.Uint32(data[off : off+4])
		off += 4
		if n > 4096 {
			return displayBase{}, 0, fmt.Errorf("clip num_rects %d too large", n)
		}
		need := off + int(n)*16
		if need > len(data) {
			return displayBase{}, 0, fmt.Errorf("clip rects truncated")
		}
		b.Clips = make([]image.Rectangle, n)
		for i := uint32(0); i < n; i++ {
			r, err := decodeRect(data, off)
			if err != nil {
				return displayBase{}, 0, err
			}
			b.Clips[i] = r
			off += 16
		}
		return b, off, nil
	default:
		return displayBase{}, 0, fmt.Errorf("invalid clip type %d", clipType)
	}
}

// decodeRect reads a SpiceRect (top, left, bottom, right) at off.
func decodeRect(data []byte, off int) (image.Rectangle, error) {
	if off+16 > len(data) {
		return image.Rectangle{}, fmt.Errorf("rect short")
	}
	top := int(binary.LittleEndian.Uint32(data[off : off+4]))
	left := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
	bottom := int(binary.LittleEndian.Uint32(data[off+8 : off+12]))
	right := int(binary.LittleEndian.Uint32(data[off+12 : off+16]))
	return image.Rect(left, top, right, bottom), nil
}

func isClosedConn(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	return strings.Contains(err.Error(), "closed network connection")
}
