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

	"github.com/maskraven/spice-viewer/internal/codec"
	"github.com/maskraven/spice-viewer/internal/codec/h264"
	"github.com/maskraven/spice-viewer/internal/display"
	"github.com/maskraven/spice-viewer/internal/protocol"
)

// opaqueBlack is used when soft-skipping a failed DRAW_COPY (leave region black).
var opaqueBlack = [4]byte{0, 0, 0, 0xff}

// displayStream tracks an active SPICE video stream (MJPEG; H.264 when
// h264.Available — OS decoder on macOS/Windows, user FFmpeg on Linux).
type displayStream struct {
	surfaceID uint32
	id        uint32
	flags     uint8
	codec     uint8
	streamW   uint32
	streamH   uint32
	srcW      uint32
	srcH      uint32
	dest      image.Rectangle
	clips     []image.Rectangle // nil = unclipped
	h264      h264.Decoder      // non-nil for VideoCodecH264 streams
}

// imageCache holds decoded images for SPICE_IMAGE_TYPE_FROM_CACHE lookups.
// Without this, QEMU/SPICE reuses bitmaps via FROM_CACHE and the client
// soft-skips every update after the first cache-eligible frame — frozen UI.
type imageCache struct {
	max int
	// simple map + insertion order for FIFO eviction
	m     map[uint64]*codec.RGBA
	order []uint64
}

func newImageCache(max int) *imageCache {
	if max <= 0 {
		max = 256
	}
	return &imageCache{max: max, m: make(map[uint64]*codec.RGBA)}
}

func (c *imageCache) get(id uint64) *codec.RGBA {
	if c == nil {
		return nil
	}
	return c.m[id]
}

func (c *imageCache) put(id uint64, img *codec.RGBA) {
	if c == nil || img == nil {
		return
	}
	if _, ok := c.m[id]; ok {
		c.m[id] = cloneRGBA(img)
		return
	}
	for len(c.order) >= c.max {
		old := c.order[0]
		c.order = c.order[1:]
		delete(c.m, old)
	}
	c.m[id] = cloneRGBA(img)
	c.order = append(c.order, id)
}

func cloneRGBA(img *codec.RGBA) *codec.RGBA {
	if img == nil {
		return nil
	}
	pix := make([]byte, len(img.Pix))
	copy(pix, img.Pix)
	return &codec.RGBA{Width: img.Width, Height: img.Height, Stride: img.Stride, Pix: pix}
}

// Display is the SPICE display channel reader/handler.
//
// It consumes mini-header framed messages on a linked display connection,
// applies allowlisted draw ops and MJPEG streams to a compositor, and ignores
// unsupported types.
//
// Draw/decode failures (unsupported image codecs, bounds, short payloads) are
// soft-skipped: logged, optionally black-fill dest, and do not abort Run.
// Unrecoverable I/O (read errors, init write failure, nil conn) still terminate Run.
//
// Session may already own DisplayConn after OpenChannels — construct a Display
// with that conn and call Run (or HandleMessage for unit tests).
type Display struct {
	conn net.Conn
	comp *display.Compositor

	mu             sync.Mutex
	unknown        map[uint16]int // ignored message types
	imageSkipTypes map[uint8]int  // unsupported image types soft-skipped
	drawSkips      int            // other soft-skipped draw/surface ops
	streams        map[uint32]*displayStream
	imgCache       *imageCache
	glzWin         *codec.GLZWindow // Global LZ dictionary (GLZ_RGB / ZLIB_GLZ_RGB)
	ack            protocol.AckState
	initSent       bool

	// prefImage is SpiceImageCompression for PREFERRED_COMPRESSION (0 = skip).
	prefImage uint8
	// prefVideo is ordered SpiceVideoCodecType list for PREFERRED_VIDEO_CODEC_TYPE.
	prefVideo []uint8
}

// DisplayOpts configures post-INIT client preferences (performance profiles).
type DisplayOpts struct {
	// PreferredImageCompression is a SpiceImageCompression value; 0 = do not send.
	PreferredImageCompression uint8
	// PreferredVideoCodecs is preference order; empty = do not send.
	PreferredVideoCodecs []uint8
}

// NewDisplay wraps a linked display-channel connection and compositor.
// conn must already be past link auth (mini-header mode).
// comp must be non-nil.
func NewDisplay(conn net.Conn, comp *display.Compositor) *Display {
	return NewDisplayOpts(conn, comp, DisplayOpts{})
}

// NewDisplayOpts is NewDisplay with preferred compression / video codec hints.
func NewDisplayOpts(conn net.Conn, comp *display.Compositor, opts DisplayOpts) *Display {
	if comp == nil {
		comp = display.NewCompositor(nil)
	}
	d := &Display{
		conn:           conn,
		comp:           comp,
		unknown:        make(map[uint16]int),
		imageSkipTypes: make(map[uint8]int),
		streams:        make(map[uint32]*displayStream),
		imgCache:       newImageCache(512),
		glzWin:         codec.NewGLZWindow(int(protocol.DisplayGlzWindowBytes)),
		prefImage:      opts.PreferredImageCompression,
	}
	if len(opts.PreferredVideoCodecs) > 0 {
		d.prefVideo = append([]uint8(nil), opts.PreferredVideoCodecs...)
	}
	return d
}

// SetPreferences updates preferred compression/video hints (sent on next
// SendInit/Run if not yet sent; call before Run for first connection).
func (d *Display) SetPreferences(imageCompression uint8, videoCodecs []uint8) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prefImage = imageCompression
	if len(videoCodecs) == 0 {
		d.prefVideo = nil
		return
	}
	d.prefVideo = append([]uint8(nil), videoCodecs...)
}

// Compositor returns the underlying compositor.
func (d *Display) Compositor() *display.Compositor {
	return d.comp
}

// SendInit writes SPICE_MSGC_DISPLAY_INIT with spice-gtk-compatible cache sizes.
// Zero cache sizes cause servers to rely on FROM_CACHE without us ever caching,
// which freezes the display after the first reusable blit.
// Safe to call once before or at the start of Run.
func (d *Display) SendInit() error {
	d.mu.Lock()
	if d.initSent {
		d.mu.Unlock()
		return nil
	}
	d.initSent = true
	d.mu.Unlock()

	// SpiceMsgcDisplayInit (packed, 14 bytes):
	//   pixmap_cache_id u8
	//   pixmap_cache_size i64   // spice-gtk: bytes/4
	//   glz_dictionary_id u8
	//   glz_dictionary_window_size i32 // spice-gtk: bytes/4
	body := make([]byte, protocol.DisplayInitBodySize)
	body[0] = 1 // pixmap_cache_id
	cacheUnits := protocol.DisplayPixmapCacheBytes / 4
	binary.LittleEndian.PutUint64(body[1:9], uint64(cacheUnits))
	body[9] = 1 // glz_dictionary_id
	glzUnits := protocol.DisplayGlzWindowBytes / 4
	binary.LittleEndian.PutUint32(body[10:14], uint32(glzUnits))
	if err := protocol.WriteMessage(d.conn, protocol.MsgcDisplayInit, body); err != nil {
		return err
	}
	return d.sendPreferences()
}

// ApplyPreferences re-sends preferred compression / video codec (after
// SetPreferences). Safe to call mid-session when the server supports the caps.
func (d *Display) ApplyPreferences() error {
	return d.sendPreferences()
}

// sendPreferences sends preferred compression / video codec when configured.
// Best-effort: failures are returned so init fails closed only if wire is dead.
func (d *Display) sendPreferences() error {
	if d == nil || d.conn == nil {
		return fmt.Errorf("channel: display: nil conn")
	}
	d.mu.Lock()
	img := d.prefImage
	vid := append([]uint8(nil), d.prefVideo...)
	d.mu.Unlock()

	if img != 0 && img != protocol.ImageCompressionInvalid {
		body := protocol.EncodePreferredCompression(img)
		if err := protocol.WriteMessage(d.conn, protocol.MsgcDisplayPreferredCompression, body); err != nil {
			return fmt.Errorf("channel: preferred compression: %w", err)
		}
	}
	if body := protocol.EncodePreferredVideoCodecType(vid); len(body) > 0 {
		if err := protocol.WriteMessage(d.conn, protocol.MsgcDisplayPreferredVideoCodecType, body); err != nil {
			return fmt.Errorf("channel: preferred video codec: %w", err)
		}
	}
	return nil
}

// Run sends DISPLAY_INIT (+ preferences) then reads and handles messages until
// ctx cancel or conn close. Soft-skipped draw errors do not stop the loop.
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
		// spice-gtk: count every inbound message toward ACK window *before* handle.
		if err := d.ack.AfterRead(d.conn); err != nil {
			return fmt.Errorf("channel: display ack: %w", err)
		}
		// HandleMessage soft-fails draw errors; only nil-conn / write path returns fatal.
		if err := d.HandleMessage(msg.Type, msg.Data); err != nil {
			return err
		}
	}
}

// HandleMessage dispatches one server→client display message by type.
// Exported for unit tests that inject wire bodies without a socket.
//
// Draw and surface op failures soft-return nil (see DrawSkipCount / ImageSkipCounts).
// Fatal returns are reserved for rare local write failures (SET_ACK/PING replies).
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
		d.handleMode(data)
		return nil
	case protocol.MsgDisplayMark:
		d.comp.Mark()
		return nil
	case protocol.MsgDisplayReset:
		d.comp.Reset()
		if d.glzWin != nil {
			d.glzWin.Reset()
		}
		return nil
	case protocol.MsgDisplaySurfaceCreate:
		d.handleSurfaceCreate(data)
		return nil
	case protocol.MsgDisplaySurfaceDestroy:
		d.handleSurfaceDestroy(data)
		return nil
	case protocol.MsgDisplayDrawFill:
		d.handleDrawFill(data)
		return nil
	case protocol.MsgDisplayDrawCopy:
		d.handleDrawCopy(data)
		return nil
	case protocol.MsgDisplayCopyBits:
		d.handleCopyBits(data)
		return nil
	case protocol.MsgDisplayStreamCreate:
		d.handleStreamCreate(data)
		return nil
	case protocol.MsgDisplayStreamData:
		d.handleStreamData(data, false)
		return nil
	case protocol.MsgDisplayStreamDataSized:
		d.handleStreamData(data, true)
		return nil
	case protocol.MsgDisplayStreamClip:
		d.handleStreamClip(data)
		return nil
	case protocol.MsgDisplayStreamDestroy:
		d.handleStreamDestroy(data)
		return nil
	case protocol.MsgDisplayStreamDestroyAll:
		d.handleStreamDestroyAll()
		return nil
	case protocol.MsgDisplayInvalList:
		d.handleInvalList(data)
		return nil
	case protocol.MsgDisplayInvalAllPixmaps:
		d.mu.Lock()
		d.imgCache = newImageCache(512)
		d.mu.Unlock()
		return nil
	case protocol.MsgDisplayInvalPalette, protocol.MsgDisplayInvalAllPalettes:
		return nil // no palette cache yet
	default:
		// Allowlisted but not yet implemented (should not happen).
		d.noteUnknown(typ)
		return nil
	}
}

// handleInvalList removes cached pixmaps by id (SPICE_MSG_DISPLAY_INVAL_LIST).
// Wire: uint16 count, then {type u8, id u64} * count (spice-gtk ResourceList).
func (d *Display) handleInvalList(data []byte) {
	if len(data) < 2 {
		return
	}
	n := int(binary.LittleEndian.Uint16(data[0:2]))
	off := 2
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := 0; i < n; i++ {
		if off+1+8 > len(data) {
			return
		}
		typ := data[off]
		off++
		id := binary.LittleEndian.Uint64(data[off : off+8])
		off += 8
		// SPICE_RES_TYPE_PIXMAP = 0
		if typ == 0 && d.imgCache != nil {
			delete(d.imgCache.m, id)
		}
	}
}

// IsDisplayAllowed reports whether typ is in the display allowlist
// (mode, mark, reset, draw_copy, draw_fill, surface create/destroy, streams).
func IsDisplayAllowed(typ uint16) bool {
	switch typ {
	case protocol.MsgDisplayMode,
		protocol.MsgDisplayMark,
		protocol.MsgDisplayReset,
		protocol.MsgDisplayDrawFill,
		protocol.MsgDisplayDrawCopy,
		protocol.MsgDisplayCopyBits,
		protocol.MsgDisplaySurfaceCreate,
		protocol.MsgDisplaySurfaceDestroy,
		protocol.MsgDisplayStreamCreate,
		protocol.MsgDisplayStreamData,
		protocol.MsgDisplayStreamDataSized,
		protocol.MsgDisplayStreamClip,
		protocol.MsgDisplayStreamDestroy,
		protocol.MsgDisplayStreamDestroyAll,
		// Palette invalidations — no-op without palette images, but must not spam logs.
		protocol.MsgDisplayInvalList,
		protocol.MsgDisplayInvalAllPixmaps,
		protocol.MsgDisplayInvalPalette,
		protocol.MsgDisplayInvalAllPalettes:
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

func (d *Display) noteImageSkip(imgType uint8, reason string) {
	d.mu.Lock()
	d.imageSkipTypes[imgType]++
	n := d.imageSkipTypes[imgType]
	d.drawSkips++
	d.mu.Unlock()
	if n == 1 {
		log.Printf("channel/display: skipping unsupported image type %d: %s", imgType, reason)
	}
}

func (d *Display) noteDrawSkip(reason string) {
	d.mu.Lock()
	d.drawSkips++
	n := d.drawSkips
	d.mu.Unlock()
	if n == 1 || n == 10 || n == 100 {
		log.Printf("channel/display: skipping draw/surface op (%d): %s", n, reason)
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

// ImageSkipCounts returns soft-skipped image type → count (e.g. Quic/JPEG until later PRs).
func (d *Display) ImageSkipCounts() map[uint8]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[uint8]int, len(d.imageSkipTypes))
	for k, v := range d.imageSkipTypes {
		out[k] = v
	}
	return out
}

// DrawSkipCount returns the number of soft-skipped draw/surface operations.
func (d *Display) DrawSkipCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.drawSkips
}

func (d *Display) handleSetAck(data []byte) error {
	// SpiceMsgSetAck: generation u32, window u32 — ACK_SYNC + enable windowed ACK.
	if d.conn == nil {
		return nil
	}
	return d.ack.OnSetAck(d.conn, data)
}

func (d *Display) handlePing(data []byte) error {
	// Echo body as PONG.
	if d.conn == nil {
		return nil
	}
	return protocol.WriteMessage(d.conn, protocol.MsgcPong, data)
}

func (d *Display) handleMode(data []byte) {
	// SpiceMsgDisplayMode: x, y, bits, width, height — all uint32 (legacy).
	// Modern servers prefer SURFACE_CREATE; MODE is accepted for allowlist completeness.
	if len(data) < 20 {
		d.noteDrawSkip(fmt.Sprintf("DISPLAY_MODE short: %d", len(data)))
		return
	}
}

func (d *Display) handleSurfaceCreate(data []byte) {
	if len(data) < protocol.SurfaceCreateSize {
		d.noteDrawSkip(fmt.Sprintf("SURFACE_CREATE short: %d", len(data)))
		return
	}
	id := binary.LittleEndian.Uint32(data[0:4])
	w := binary.LittleEndian.Uint32(data[4:8])
	h := binary.LittleEndian.Uint32(data[8:12])
	format := binary.LittleEndian.Uint32(data[12:16])
	flags := binary.LittleEndian.Uint32(data[16:20])
	if err := d.comp.CreateSurface(id, int(w), int(h), format, flags); err != nil {
		d.noteDrawSkip(err.Error())
	}
}

func (d *Display) handleSurfaceDestroy(data []byte) {
	if len(data) < 4 {
		d.noteDrawSkip(fmt.Sprintf("SURFACE_DESTROY short: %d", len(data)))
		return
	}
	id := binary.LittleEndian.Uint32(data[0:4])
	if err := d.comp.DestroySurface(id); err != nil {
		d.noteDrawSkip(err.Error())
	}
}

func (d *Display) handleDrawFill(data []byte) {
	base, off, err := decodeDisplayBase(data)
	if err != nil {
		d.noteDrawSkip(fmt.Sprintf("DRAW_FILL base: %v", err))
		return
	}
	if off >= len(data) {
		d.noteDrawSkip("DRAW_FILL truncated after base")
		return
	}
	brushType := data[off]
	off++
	var rgba [4]byte
	switch brushType {
	case protocol.BrushTypeNone:
		return
	case protocol.BrushTypeSolid:
		if off+4 > len(data) {
			d.noteDrawSkip("DRAW_FILL solid color short")
			return
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
		d.noteDrawSkip("DRAW_FILL pattern brush not implemented")
		return
	default:
		d.noteDrawSkip(fmt.Sprintf("DRAW_FILL unknown brush type %d", brushType))
		return
	}

	// ropd u16 + QMask (1 + 8 + 4 = 13) — skip for Phase 1 solid put
	if off+2 > len(data) {
		d.noteDrawSkip("DRAW_FILL ropd short")
		return
	}
	off += 2
	// QMask: flags u8, pos x u32, pos y u32, image_ptr u32
	if off+13 > len(data) {
		d.noteDrawSkip("DRAW_FILL qmask short")
		return
	}

	if err := d.comp.Fill(base.SurfaceID, base.Box, rgba, base.Clips); err != nil {
		d.noteDrawSkip(err.Error())
	}
}

func (d *Display) handleDrawCopy(data []byte) {
	base, off, err := decodeDisplayBase(data)
	if err != nil {
		d.noteDrawSkip(fmt.Sprintf("DRAW_COPY base: %v", err))
		return
	}
	if off+4 > len(data) {
		d.noteDrawSkip("DRAW_COPY img ptr short")
		return
	}
	imgPtr := binary.LittleEndian.Uint32(data[off : off+4])
	off += 4

	srcArea, err := decodeRect(data, off)
	if err != nil {
		d.noteDrawSkip(fmt.Sprintf("DRAW_COPY src_area: %v", err))
		return
	}
	off += 16

	// ropd u16, scale_mode u8, QMask 13 bytes
	if off+2+1+13 > len(data) {
		d.noteDrawSkip("DRAW_COPY trailer short")
		return
	}

	// Issue 7: require room for at least a SpiceImage descriptor.
	if int(imgPtr) >= len(data) || len(data)-int(imgPtr) < protocol.SpiceImageDescSize {
		d.noteDrawSkip(fmt.Sprintf("DRAW_COPY img_ptr %d invalid for body %d", imgPtr, len(data)))
		return
	}

	img, err := d.resolveImage(data[imgPtr:])
	if err != nil {
		var uerr *codec.UnsupportedImageError
		if errors.As(err, &uerr) {
			d.noteImageSkip(uerr.Type, err.Error())
		} else {
			typ := data[imgPtr+8]
			if errors.Is(err, codec.ErrUnsupportedImage) {
				d.noteImageSkip(typ, err.Error())
			} else {
				d.noteDrawSkip(fmt.Sprintf("DRAW_COPY image: %v", err))
			}
		}
		// spice-gtk leaves previous pixels on draw failure — do NOT black-fill
		// (black fill causes mouse trails and permanent damage).
		return
	}

	srcOrigin := image.Pt(srcArea.Min.X, srcArea.Min.Y)
	// If src_area is empty, use full image at 0,0
	if srcArea.Empty() {
		srcOrigin = image.Pt(0, 0)
	}
	if err := d.comp.Copy(base.SurfaceID, base.Box, img, srcOrigin, base.Clips); err != nil {
		d.noteDrawSkip(err.Error())
		// No softBlackFill — preserve last good pixels (spice-gtk behavior).
	}
}

// resolveImage decodes a SpiceImage or resolves FROM_CACHE / SURFACE references.
// CACHE_ME images are stored for later FROM_CACHE draws (critical for live UI).
// GLZ / ZLIB_GLZ use the channel-scoped dictionary window (stateful).
func (d *Display) resolveImage(desc []byte) (*codec.RGBA, error) {
	if len(desc) < protocol.SpiceImageDescSize {
		return nil, fmt.Errorf("image descriptor short: %d", len(desc))
	}
	id := binary.LittleEndian.Uint64(desc[0:8])
	typ := desc[8]
	flags := desc[9]
	width := binary.LittleEndian.Uint32(desc[10:14])
	height := binary.LittleEndian.Uint32(desc[14:18])
	payload := desc[protocol.SpiceImageDescSize:]

	switch typ {
	case protocol.ImageTypeFromCache, protocol.ImageTypeFromCacheLossless:
		d.mu.Lock()
		img := d.imgCache.get(id)
		d.mu.Unlock()
		if img == nil {
			return nil, &codec.UnsupportedImageError{Type: typ}
		}
		return img, nil
	case protocol.ImageTypeSurface:
		// Payload after descriptor: surface_id u32
		if len(desc) < protocol.SpiceImageDescSize+4 {
			return nil, fmt.Errorf("surface image short")
		}
		sid := binary.LittleEndian.Uint32(desc[protocol.SpiceImageDescSize : protocol.SpiceImageDescSize+4])
		surf := d.comp.Surface(sid)
		if surf == nil {
			return nil, fmt.Errorf("surface image: surface %d missing", sid)
		}
		// Snapshot surface pixels as RGBA for blit.
		return surfaceToRGBA(surf), nil
	case protocol.ImageTypeGLZRGB, protocol.ImageTypeZlibGLZRGB:
		win := d.glzWin
		if win == nil {
			win = codec.NewGLZWindow(int(protocol.DisplayGlzWindowBytes))
			d.glzWin = win
		}
		zlibWrapped := typ == protocol.ImageTypeZlibGLZRGB
		img, err := win.Decode(payload, zlibWrapped, width, height)
		if err != nil {
			return nil, err
		}
		if flags&protocol.ImageFlagCacheMe != 0 || flags&protocol.ImageFlagCacheReplaceMe != 0 {
			d.mu.Lock()
			d.imgCache.put(id, img)
			d.mu.Unlock()
		}
		return img, nil
	}

	img, err := codec.DecodeSpiceImage(desc)
	if err != nil {
		return nil, err
	}
	if flags&protocol.ImageFlagCacheMe != 0 || flags&protocol.ImageFlagCacheReplaceMe != 0 {
		d.mu.Lock()
		d.imgCache.put(id, img)
		d.mu.Unlock()
	}
	return img, nil
}

// handleCopyBits implements SPICE_MSG_DISPLAY_COPY_BITS (scroll / self-blit).
// Wire: DisplayBase + src_position (x u32, y u32).
func (d *Display) handleCopyBits(data []byte) {
	base, off, err := decodeDisplayBase(data)
	if err != nil {
		d.noteDrawSkip(fmt.Sprintf("COPY_BITS base: %v", err))
		return
	}
	if off+8 > len(data) {
		d.noteDrawSkip("COPY_BITS src pos short")
		return
	}
	srcX := int(binary.LittleEndian.Uint32(data[off : off+4]))
	srcY := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
	srcOrigin := image.Pt(srcX, srcY)
	if err := d.comp.CopyBits(base.SurfaceID, base.Box, srcOrigin, base.Clips); err != nil {
		d.noteDrawSkip(err.Error())
	}
}

func surfaceToRGBA(s *display.Surface) *codec.RGBA {
	if s == nil {
		return nil
	}
	// Surface Pix is already RGBA8888 layout used by compositor.
	pix := make([]byte, len(s.Pix))
	copy(pix, s.Pix)
	return &codec.RGBA{Width: s.Width, Height: s.Height, Stride: s.Stride, Pix: pix}
}

// softBlackFill is retained for tests that want explicit black fill; production
// DRAW_COPY skips no longer call it (black trails). Prefer leaving prior pixels.
func (d *Display) softBlackFill(base displayBase) {
	if base.Box.Empty() {
		return
	}
	_ = d.comp.Fill(base.SurfaceID, base.Box, opaqueBlack, base.Clips)
}

// handleStreamCreate parses SpiceMsgDisplayStreamCreate and records the stream.
//
// Wire (packed): surface_id u32, id u32, flags u8, codec_type u8, stamp u64,
// stream_width/height u32, src_width/height u32, dest Rect, Clip.
func (d *Display) handleStreamCreate(data []byte) {
	if len(data) < protocol.StreamCreateFixedSize+1 { // +1 for clip type at minimum
		d.noteDrawSkip(fmt.Sprintf("STREAM_CREATE short: %d", len(data)))
		return
	}
	surfaceID := binary.LittleEndian.Uint32(data[0:4])
	id := binary.LittleEndian.Uint32(data[4:8])
	flags := data[8]
	codecType := data[9]
	// stamp at [10:18] ignored
	streamW := binary.LittleEndian.Uint32(data[18:22])
	streamH := binary.LittleEndian.Uint32(data[22:26])
	srcW := binary.LittleEndian.Uint32(data[26:30])
	srcH := binary.LittleEndian.Uint32(data[30:34])
	dest, err := decodeRect(data, 34)
	if err != nil {
		d.noteDrawSkip(fmt.Sprintf("STREAM_CREATE dest: %v", err))
		return
	}
	// Clip starts at offset 50.
	clips, _, err := decodeClipAt(data, protocol.StreamCreateFixedSize)
	if err != nil {
		d.noteDrawSkip(fmt.Sprintf("STREAM_CREATE clip: %v", err))
		return
	}

	var h264Dec h264.Decoder
	switch codecType {
	case protocol.VideoCodecMJPEG:
		// stdlib jpeg path
	case protocol.VideoCodecH264:
		// macOS VideoToolbox / Windows Media Foundation / Linux user FFmpeg.
		// Never bundle FFmpeg; Available() is false when the backend is missing.
		if !h264.Available() {
			d.noteDrawSkip(fmt.Sprintf("STREAM_CREATE H.264 unsupported on this platform (stream %d)", id))
			return
		}
		dec, err := h264.New()
		if err != nil {
			d.noteDrawSkip(fmt.Sprintf("STREAM_CREATE H.264 init: %v (stream %d)", err, id))
			return
		}
		h264Dec = dec
	default:
		d.noteDrawSkip(fmt.Sprintf("STREAM_CREATE unsupported codec %d (stream %d)", codecType, id))
		// Prefer not to store unsupported codecs — DATA will note "unknown stream".
		return
	}

	st := &displayStream{
		surfaceID: surfaceID,
		id:        id,
		flags:     flags,
		codec:     codecType,
		streamW:   streamW,
		streamH:   streamH,
		srcW:      srcW,
		srcH:      srcH,
		dest:      dest,
		clips:     clips,
		h264:      h264Dec,
	}
	d.mu.Lock()
	if d.streams == nil {
		d.streams = make(map[uint32]*displayStream)
	}
	// Replace existing stream id: close prior H.264 decoder if any.
	if old := d.streams[id]; old != nil && old.h264 != nil {
		old.h264.Close()
	}
	d.streams[id] = st
	d.mu.Unlock()
}

// handleStreamData presents one stream frame. sized=true for STREAM_DATA_SIZED.
//
// STREAM_DATA: id u32, mm_time u32, data_size u32, data[]
// STREAM_DATA_SIZED: id u32, mm_time u32, width u32, height u32, dest Rect, data_size u32, data[]
func (d *Display) handleStreamData(data []byte, sized bool) {
	if len(data) < protocol.StreamDataHeaderSize+4 {
		d.noteDrawSkip(fmt.Sprintf("STREAM_DATA short: %d", len(data)))
		return
	}
	id := binary.LittleEndian.Uint32(data[0:4])
	// multi_media_time at [4:8] ignored for Phase 2 present-immediately path
	off := protocol.StreamDataHeaderSize

	d.mu.Lock()
	st := d.streams[id]
	d.mu.Unlock()
	if st == nil {
		d.noteDrawSkip(fmt.Sprintf("STREAM_DATA unknown stream %d", id))
		return
	}

	dest := st.dest
	if sized {
		if off+4+4+16+4 > len(data) {
			d.noteDrawSkip("STREAM_DATA_SIZED header short")
			return
		}
		// width/height at off; dest after
		off += 8
		r, err := decodeRect(data, off)
		if err != nil {
			d.noteDrawSkip(fmt.Sprintf("STREAM_DATA_SIZED dest: %v", err))
			return
		}
		dest = r
		off += 16
	}

	if off+4 > len(data) {
		d.noteDrawSkip("STREAM_DATA data_size short")
		return
	}
	dataSize := binary.LittleEndian.Uint32(data[off : off+4])
	off += 4
	if dataSize == 0 || int64(dataSize) > protocol.MaxSurfaceBytes {
		d.noteDrawSkip(fmt.Sprintf("STREAM_DATA bad data_size %d", dataSize))
		return
	}
	if off+int(dataSize) > len(data) {
		d.noteDrawSkip(fmt.Sprintf("STREAM_DATA truncated: have %d need %d", len(data)-off, dataSize))
		return
	}
	frame := data[off : off+int(dataSize)]

	var img *codec.RGBA
	var err error
	switch st.codec {
	case protocol.VideoCodecMJPEG:
		img, err = codec.DecodeJPEGBytes(frame)
		if err != nil {
			d.noteDrawSkip(fmt.Sprintf("STREAM_DATA mjpeg: %v", err))
			return
		}
	case protocol.VideoCodecH264:
		if st.h264 == nil {
			d.noteDrawSkip(fmt.Sprintf("STREAM_DATA H.264 stream %d has no decoder", id))
			return
		}
		img, err = st.h264.Decode(frame, int(st.streamW), int(st.streamH))
		if err != nil {
			d.noteDrawSkip(fmt.Sprintf("STREAM_DATA h264: %v", err))
			return
		}
	default:
		d.noteDrawSkip(fmt.Sprintf("STREAM_DATA codec %d unsupported", st.codec))
		return
	}
	// BOTTOM-UP stream: flip when TOP_DOWN flag is clear.
	if st.flags&protocol.StreamFlagTopDown == 0 {
		flipRGBA(img)
	}

	// If dest is empty, use stream dimensions at origin.
	if dest.Empty() {
		dest = image.Rect(0, 0, int(st.streamW), int(st.streamH))
		if dest.Empty() {
			dest = image.Rect(0, 0, img.Width, img.Height)
		}
	}
	// Scale is not implemented: blit top-left of frame into dest size (clip to img).
	srcOrigin := image.Pt(0, 0)
	if err := d.comp.Copy(st.surfaceID, dest, img, srcOrigin, st.clips); err != nil {
		d.noteDrawSkip(fmt.Sprintf("STREAM_DATA present: %v", err))
	}
}

func (d *Display) handleStreamClip(data []byte) {
	if len(data) < 4+1 {
		d.noteDrawSkip(fmt.Sprintf("STREAM_CLIP short: %d", len(data)))
		return
	}
	id := binary.LittleEndian.Uint32(data[0:4])
	clips, _, err := decodeClipAt(data, 4)
	if err != nil {
		d.noteDrawSkip(fmt.Sprintf("STREAM_CLIP: %v", err))
		return
	}
	d.mu.Lock()
	st := d.streams[id]
	if st != nil {
		st.clips = clips
	}
	d.mu.Unlock()
	if st == nil {
		d.noteDrawSkip(fmt.Sprintf("STREAM_CLIP unknown stream %d", id))
	}
}

func (d *Display) handleStreamDestroy(data []byte) {
	if len(data) < 4 {
		d.noteDrawSkip(fmt.Sprintf("STREAM_DESTROY short: %d", len(data)))
		return
	}
	id := binary.LittleEndian.Uint32(data[0:4])
	d.mu.Lock()
	if st := d.streams[id]; st != nil && st.h264 != nil {
		st.h264.Close()
	}
	delete(d.streams, id)
	d.mu.Unlock()
}

func (d *Display) handleStreamDestroyAll() {
	d.mu.Lock()
	for _, st := range d.streams {
		if st != nil && st.h264 != nil {
			st.h264.Close()
		}
	}
	d.streams = make(map[uint32]*displayStream)
	d.mu.Unlock()
}

// flipRGBA vertically flips img in place (stream bottom-up frames).
func flipRGBA(img *codec.RGBA) {
	if img == nil || img.Height < 2 {
		return
	}
	row := make([]byte, img.Stride)
	for y := 0; y < img.Height/2; y++ {
		top := y * img.Stride
		bot := (img.Height - 1 - y) * img.Stride
		copy(row, img.Pix[top:top+img.Stride])
		copy(img.Pix[top:top+img.Stride], img.Pix[bot:bot+img.Stride])
		copy(img.Pix[bot:bot+img.Stride], row)
	}
}

// decodeClipAt reads a SpiceClip at off; returns clips and new offset.
// Nil clips = CLIP_NONE; non-nil empty = CLIP_RECTS with 0 rects.
func decodeClipAt(data []byte, off int) ([]image.Rectangle, int, error) {
	if off >= len(data) {
		return nil, off, fmt.Errorf("clip short")
	}
	clipType := data[off]
	off++
	switch clipType {
	case protocol.ClipTypeNone:
		return nil, off, nil
	case protocol.ClipTypeRects:
		if off+4 > len(data) {
			return nil, 0, fmt.Errorf("clip num_rects short")
		}
		n := binary.LittleEndian.Uint32(data[off : off+4])
		off += 4
		if n > 4096 {
			return nil, 0, fmt.Errorf("clip num_rects %d too large", n)
		}
		need := off + int(n)*16
		if need > len(data) {
			return nil, 0, fmt.Errorf("clip rects truncated")
		}
		clips := make([]image.Rectangle, n)
		for i := uint32(0); i < n; i++ {
			r, err := decodeRect(data, off)
			if err != nil {
				return nil, 0, err
			}
			clips[i] = r
			off += 16
		}
		return clips, off, nil
	default:
		return nil, 0, fmt.Errorf("invalid clip type %d", clipType)
	}
}

// displayBase is the decoded SpiceMsgDisplayBase prefix of draw messages.
type displayBase struct {
	SurfaceID uint32
	Box       image.Rectangle
	// Clips: nil = CLIP_NONE (unclipped); non-nil empty = CLIP_RECTS with 0 rects (draw nothing).
	Clips []image.Rectangle
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
	clips, off, err := decodeClipAt(data, 20)
	if err != nil {
		return displayBase{}, 0, err
	}
	b.Clips = clips
	return b, off, nil
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
