// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/maskraven/spice-viewer/internal/protocol"
)

// Driver receives best-effort cursor shape and position updates.
//
// UI backends implement this. Decode/shape failures never panic; callers of
// Cursor.HandleMessage / Cursor.Run treat returned errors as log-only — the
// session continues (server-drawn cursor in the framebuffer is sufficient).
type Driver interface {
	// SetCursor installs a client-side cursor shape (RGBA8888, stride = w*4).
	// The rgba slice must be treated as immutable after return (callers may
	// retain a reference; copy if mutation is required).
	SetCursor(hotX, hotY int, rgba []byte, w, h int)
	// MoveCursor moves the hotspot to guest display coordinates (server mouse mode).
	MoveCursor(x, y int)
	// HideCursor hides the client-side cursor.
	HideCursor()
	// ResetCursor restores the default / ungrabbbed cursor and clears shape state.
	ResetCursor()
}

// maxCursorCacheEntries bounds CACHE_ME entries against a chatty/hostile server.
const maxCursorCacheEntries = 64

// NullDriver is a headless Driver for tests. Methods are concurrency-safe.
type NullDriver struct {
	mu sync.Mutex

	HotX, HotY int
	Width      int
	Height     int
	RGBA       []byte // copy of last SetCursor buffer

	X, Y int // last MoveCursor

	Visible bool // false after Hide/Reset; true after Set(visible)/Move

	SetCount   int
	MoveCount  int
	HideCount  int
	ResetCount int
}

// NewNullDriver returns an empty NullDriver (hidden, no shape).
func NewNullDriver() *NullDriver {
	return &NullDriver{}
}

// SetCursor implements Driver.
func (n *NullDriver) SetCursor(hotX, hotY int, rgba []byte, w, h int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.SetCount++
	n.HotX, n.HotY = hotX, hotY
	n.Width, n.Height = w, h
	if len(rgba) > 0 {
		n.RGBA = append([]byte(nil), rgba...)
	} else {
		n.RGBA = nil
	}
	n.Visible = true
}

// MoveCursor implements Driver.
func (n *NullDriver) MoveCursor(x, y int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.MoveCount++
	n.X, n.Y = x, y
	n.Visible = true
}

// HideCursor implements Driver.
func (n *NullDriver) HideCursor() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.HideCount++
	n.Visible = false
}

// ResetCursor implements Driver.
func (n *NullDriver) ResetCursor() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ResetCount++
	n.Visible = false
	n.Width, n.Height = 0, 0
	n.HotX, n.HotY = 0, 0
	n.RGBA = nil
}

// Snapshot returns a copy of the last shape and position (tests).
func (n *NullDriver) Snapshot() (rgba []byte, w, h, hotX, hotY, x, y int, visible bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.RGBA != nil {
		rgba = append([]byte(nil), n.RGBA...)
	}
	return rgba, n.Width, n.Height, n.HotX, n.HotY, n.X, n.Y, n.Visible
}

// Counts returns Set/Move/Hide/Reset call counters (tests; concurrency-safe).
func (n *NullDriver) Counts() (set, move, hide, reset int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.SetCount, n.MoveCount, n.HideCount, n.ResetCount
}

// Compile-time check.
var _ Driver = (*NullDriver)(nil)

// cachedShape is one CACHE_ME entry (decoded RGBA).
type cachedShape struct {
	hotX, hotY int
	w, h       int
	rgba       []byte
}

// Cursor is the Phase-1 best-effort SPICE cursor channel reader/handler.
//
// Open failure is already non-fatal at the session layer. Runtime decode
// errors returned from HandleMessage are for logging only — Run logs them and
// continues so a bad shape never kills the session.
type Cursor struct {
	conn   net.Conn
	driver Driver

	mu      sync.Mutex
	unknown map[uint16]int
	cache   map[uint64]cachedShape
	// lastErr is the most recent non-fatal decode error (for diagnostics).
	lastErr error
	ack     protocol.AckState
}

// NewCursor wraps a linked cursor-channel connection and a Driver.
// conn may be nil for pure unit tests that only call HandleMessage.
// driver may be nil (messages are decoded/discarded).
func NewCursor(conn net.Conn, driver Driver) *Cursor {
	return &Cursor{
		conn:    conn,
		driver:  driver,
		unknown: make(map[uint16]int),
		cache:   make(map[uint64]cachedShape),
	}
}

// LastError returns the most recent non-fatal handle error, if any.
// Updated by HandleMessage (including the path used by Run).
func (c *Cursor) LastError() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

// Run reads mini-header messages until ctx cancel or connection close.
//
// Decode errors are logged and do not stop the loop (best-effort). I/O errors
// and context cancel stop Run; the caller must treat that as cursor degrade
// only — never as a session-fatal condition. On channel death the driver is
// reset so a stale client cursor is not left on screen.
func (c *Cursor) Run(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("channel: cursor: nil conn")
	}
	for {
		if err := ctx.Err(); err != nil {
			c.degradeDriver()
			return err
		}
		msg, err := protocol.ReadMessage(c.conn)
		if err != nil {
			c.degradeDriver()
			if err == io.EOF || isClosedConn(err) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Read framing errors: log and stop this channel only.
			log.Printf("channel/cursor: read error (degraded): %v", err)
			return err
		}
		if err := c.ack.AfterRead(c.conn); err != nil {
			log.Printf("channel/cursor: ack: %v", err)
		}
		if err := c.HandleMessage(msg.Type, msg.Data); err != nil {
			// Non-fatal: keep reading so one bad shape cannot kill the channel loop.
			// lastErr already set inside HandleMessage.
			log.Printf("channel/cursor: handle type %d: %v", msg.Type, err)
		}
	}
}

// HandleMessage dispatches one server→client cursor message by type.
//
// Decode failures return an error for the caller to log and record in
// LastError; they never panic. On shape decode failure for SET/INIT the
// driver is hidden (degrade to server-drawn / default). Unknown types are
// ignored (nil error).
func (c *Cursor) HandleMessage(typ uint16, data []byte) (err error) {
	// Panic guard: best-effort must never take down the process.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("channel: cursor: panic recovered: %v", r)
			log.Printf("%v", err)
		}
		if err != nil {
			c.mu.Lock()
			c.lastErr = err
			c.mu.Unlock()
		}
	}()

	// Common channel messages.
	switch typ {
	case protocol.MsgSetAck:
		return c.handleSetAck(data)
	case protocol.MsgPing:
		return c.handlePing(data)
	case protocol.MsgNotify, protocol.MsgWaitForChannels,
		protocol.MsgMigrate, protocol.MsgMigrateData, protocol.MsgDisconnecting:
		return nil
	}

	switch typ {
	case protocol.MsgCursorInit:
		return c.handleInit(data)
	case protocol.MsgCursorReset:
		return c.handleReset()
	case protocol.MsgCursorSet:
		return c.handleSet(data)
	case protocol.MsgCursorMove:
		return c.handleMove(data)
	case protocol.MsgCursorHide:
		c.hide()
		return nil
	case protocol.MsgCursorTrail:
		// Phase 1: ignore trail aesthetics.
		return nil
	case protocol.MsgCursorInvalOne:
		return c.handleInvalOne(data)
	case protocol.MsgCursorInvalAll:
		c.clearCache()
		return nil
	default:
		c.noteUnknown(typ)
		return nil
	}
}

// IsCursorMessage reports whether typ is a known cursor-channel server message.
func IsCursorMessage(typ uint16) bool {
	switch typ {
	case protocol.MsgCursorInit, protocol.MsgCursorReset, protocol.MsgCursorSet,
		protocol.MsgCursorMove, protocol.MsgCursorHide, protocol.MsgCursorTrail,
		protocol.MsgCursorInvalOne, protocol.MsgCursorInvalAll:
		return true
	default:
		return false
	}
}

func (c *Cursor) noteUnknown(typ uint16) {
	c.mu.Lock()
	c.unknown[typ]++
	n := c.unknown[typ]
	c.mu.Unlock()
	if n == 1 {
		log.Printf("channel/cursor: ignoring message type %d", typ)
	}
}

// UnknownCounts returns a copy of ignored-type counters (tests / diagnostics).
func (c *Cursor) UnknownCounts() map[uint16]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[uint16]int, len(c.unknown))
	for k, v := range c.unknown {
		out[k] = v
	}
	return out
}

func (c *Cursor) handleSetAck(data []byte) error {
	if c.conn == nil {
		return nil
	}
	return c.ack.OnSetAck(c.conn, data)
}

func (c *Cursor) handlePing(data []byte) error {
	if c.conn == nil {
		return nil
	}
	return protocol.WriteMessage(c.conn, protocol.MsgcPong, data)
}

func (c *Cursor) handleReset() error {
	c.clearCache()
	if c.driver != nil {
		c.driver.ResetCursor()
	}
	return nil
}

func (c *Cursor) handleInit(data []byte) error {
	// SpiceMsgCursorInit: Point16 + trail_length u16 + trail_frequency u16 + visible u8 + Cursor
	// Fixed prefix = 9 bytes (packed).
	// SPICE: INIT clears shape cache then applies pointer state.
	if len(data) < 9 {
		return fmt.Errorf("channel: CURSOR_INIT short: %d", len(data))
	}
	c.clearCache()
	x := int(int16(binary.LittleEndian.Uint16(data[0:2])))
	y := int(int16(binary.LittleEndian.Uint16(data[2:4])))
	// trail_length / trail_frequency ignored (bytes 4..7)
	visible := data[8] != 0
	shape, err := c.decodeCursor(data[9:])
	if err != nil {
		// Degrade: hide client cursor; server-drawn path remains.
		c.hide()
		return fmt.Errorf("channel: CURSOR_INIT: %w", err)
	}
	if shape != nil && c.driver != nil {
		c.driver.SetCursor(shape.hotX, shape.hotY, shape.rgba, shape.w, shape.h)
	}
	if c.driver != nil {
		c.driver.MoveCursor(x, y)
		if !visible || shape == nil {
			c.driver.HideCursor()
		}
	}
	return nil
}

func (c *Cursor) handleSet(data []byte) error {
	// SpiceMsgCursorSet: Point16 + visible u8 + Cursor  (prefix 5 bytes)
	if len(data) < 5 {
		return fmt.Errorf("channel: CURSOR_SET short: %d", len(data))
	}
	x := int(int16(binary.LittleEndian.Uint16(data[0:2])))
	y := int(int16(binary.LittleEndian.Uint16(data[2:4])))
	visible := data[4] != 0
	shape, err := c.decodeCursor(data[5:])
	if err != nil {
		// Degrade on bad shape: hide rather than keep a possibly stale client cursor.
		c.hide()
		return fmt.Errorf("channel: CURSOR_SET: %w", err)
	}
	if shape != nil && c.driver != nil {
		c.driver.SetCursor(shape.hotX, shape.hotY, shape.rgba, shape.w, shape.h)
	}
	if c.driver != nil {
		// Position is relevant in server mouse mode; always record.
		c.driver.MoveCursor(x, y)
		if !visible || shape == nil {
			c.driver.HideCursor()
		}
	}
	return nil
}

func (c *Cursor) handleMove(data []byte) error {
	// SpiceMsgCursorMove: Point16 (4 bytes). Implies visible=1.
	if len(data) < 4 {
		return fmt.Errorf("channel: CURSOR_MOVE short: %d", len(data))
	}
	x := int(int16(binary.LittleEndian.Uint16(data[0:2])))
	y := int(int16(binary.LittleEndian.Uint16(data[2:4])))
	if c.driver != nil {
		c.driver.MoveCursor(x, y)
	}
	return nil
}

func (c *Cursor) handleInvalOne(data []byte) error {
	if len(data) < 8 {
		return fmt.Errorf("channel: CURSOR_INVAL_ONE short: %d", len(data))
	}
	id := binary.LittleEndian.Uint64(data[0:8])
	c.mu.Lock()
	delete(c.cache, id)
	c.mu.Unlock()
	return nil
}

func (c *Cursor) hide() {
	if c.driver != nil {
		c.driver.HideCursor()
	}
}

// degradeDriver hides/resets client cursor state when the channel dies or
// cannot be trusted (design: runtime failure → hide / use default).
func (c *Cursor) degradeDriver() {
	if c.driver != nil {
		c.driver.ResetCursor()
	}
}

func (c *Cursor) clearCache() {
	c.mu.Lock()
	c.cache = make(map[uint64]cachedShape)
	c.mu.Unlock()
}

// storeCache inserts a CACHE_ME entry, evicting an arbitrary entry if full.
func (c *Cursor) storeCache(id uint64, shape cachedShape) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) >= maxCursorCacheEntries {
		if _, ok := c.cache[id]; !ok {
			// Evict one arbitrary entry (map iteration order is fine for a bound).
			for k := range c.cache {
				delete(c.cache, k)
				break
			}
		}
	}
	c.cache[id] = shape
}

// decodeCursor parses a SpiceCursor blob (flags16 + optional header + data).
// Returns (nil, nil) when FLAGS_NONE (no shape).
func (c *Cursor) decodeCursor(b []byte) (*cachedShape, error) {
	if len(b) < 2 {
		return nil, fmt.Errorf("cursor blob short: %d", len(b))
	}
	flags := binary.LittleEndian.Uint16(b[0:2])
	off := 2

	if flags&protocol.CursorFlagNone != 0 {
		return nil, nil
	}

	if flags&protocol.CursorFlagFromCache != 0 {
		// Only unique is required; remaining header fields are invalid on wire
		// but demarshaller still emits full header when !NONE — we follow
		// spice.proto: when FROM_CACHE, header is present (unique used as key).
		if len(b) < off+protocol.CursorHeaderWireSize {
			// Minimal: unique only (8 bytes) accepted for FROM_CACHE.
			if len(b) < off+8 {
				return nil, fmt.Errorf("FROM_CACHE unique short")
			}
			id := binary.LittleEndian.Uint64(b[off : off+8])
			return c.lookupCache(id)
		}
		id := binary.LittleEndian.Uint64(b[off : off+8])
		return c.lookupCache(id)
	}

	if len(b) < off+protocol.CursorHeaderWireSize {
		return nil, fmt.Errorf("cursor header short: %d", len(b)-off)
	}
	unique := binary.LittleEndian.Uint64(b[off : off+8])
	typ := b[off+8]
	width := int(binary.LittleEndian.Uint16(b[off+9 : off+11]))
	height := int(binary.LittleEndian.Uint16(b[off+11 : off+13]))
	hotX := int(binary.LittleEndian.Uint16(b[off+13 : off+15]))
	hotY := int(binary.LittleEndian.Uint16(b[off+15 : off+17]))
	off += protocol.CursorHeaderWireSize
	data := b[off:]

	if width <= 0 || height <= 0 || width > protocol.MaxCursorSide || height > protocol.MaxCursorSide {
		return nil, fmt.Errorf("cursor size %dx%d out of range", width, height)
	}
	if width*height > protocol.MaxCursorPixels {
		return nil, fmt.Errorf("cursor pixels %d exceeds max %d", width*height, protocol.MaxCursorPixels)
	}
	if hotX > width {
		hotX = width
	}
	if hotY > height {
		hotY = height
	}

	rgba, err := decodeCursorPixels(typ, width, height, data)
	if err != nil {
		return nil, err
	}
	shape := &cachedShape{hotX: hotX, hotY: hotY, w: width, h: height, rgba: rgba}
	if flags&protocol.CursorFlagCacheMe != 0 {
		c.storeCache(unique, cachedShape{
			hotX: hotX, hotY: hotY, w: width, h: height,
			rgba: append([]byte(nil), rgba...),
		})
	}
	return shape, nil
}

func (c *Cursor) lookupCache(id uint64) (*cachedShape, error) {
	c.mu.Lock()
	s, ok := c.cache[id]
	c.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("cursor cache miss id=%#x", id)
	}
	// Copy pixels so a Driver that mutates cannot corrupt the cache.
	out := cachedShape{
		hotX: s.hotX, hotY: s.hotY, w: s.w, h: s.h,
		rgba: append([]byte(nil), s.rgba...),
	}
	return &out, nil
}

// decodeCursorPixels converts on-wire cursor data to RGBA8888.
// Phase 1: ALPHA (required path) and MONO (simple). Other types return error.
func decodeCursorPixels(typ uint8, w, h int, data []byte) ([]byte, error) {
	switch typ {
	case protocol.CursorTypeAlpha:
		need := w * h * 4
		if len(data) < need {
			return nil, fmt.Errorf("ALPHA data short: %d want %d", len(data), need)
		}
		// Wire: premultiplied ARGB8888 little-endian → bytes B,G,R,A.
		// Driver expects RGBA8888: swap R and B.
		out := make([]byte, need)
		for i := 0; i < need; i += 4 {
			out[i+0] = data[i+2] // R
			out[i+1] = data[i+1] // G
			out[i+2] = data[i+0] // B
			out[i+3] = data[i+3] // A
		}
		return out, nil

	case protocol.CursorTypeMono:
		// AND mask then XOR mask; stride = ceil(w/8). Bits are MSB-first.
		bpl := (w + 7) / 8
		need := 2 * bpl * h
		if len(data) < need {
			return nil, fmt.Errorf("MONO data short: %d want %d", len(data), need)
		}
		andMask := data[:bpl*h]
		xorMask := data[bpl*h : need]
		out := make([]byte, w*h*4)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				byteI := y*bpl + x/8
				bit := uint8(0x80 >> (uint(x) % 8))
				andBit := andMask[byteI]&bit != 0
				xorBit := xorMask[byteI]&bit != 0
				off := (y*w + x) * 4
				switch {
				case andBit && !xorBit:
					// transparent
					out[off+0], out[off+1], out[off+2], out[off+3] = 0, 0, 0, 0
				case !andBit && !xorBit:
					// black
					out[off+0], out[off+1], out[off+2], out[off+3] = 0, 0, 0, 0xff
				case !andBit && xorBit:
					// white
					out[off+0], out[off+1], out[off+2], out[off+3] = 0xff, 0xff, 0xff, 0xff
				default:
					// AND=1 XOR=1: invert — approximate as opaque gray
					out[off+0], out[off+1], out[off+2], out[off+3] = 0x80, 0x80, 0x80, 0xff
				}
			}
		}
		return out, nil

	default:
		return nil, fmt.Errorf("cursor type %d not implemented", typ)
	}
}
