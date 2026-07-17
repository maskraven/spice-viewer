// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/maskraven/virt-viewer/internal/agent"
	"github.com/maskraven/virt-viewer/internal/channel"
	"github.com/maskraven/virt-viewer/internal/codec/h264"
	"github.com/maskraven/virt-viewer/internal/display"
	"github.com/maskraven/virt-viewer/internal/protocol"
	"github.com/maskraven/virt-viewer/internal/security"
	"github.com/maskraven/virt-viewer/internal/session"
	"github.com/maskraven/virt-viewer/internal/ux"
)

// eventBuf is the capacity of Client.Events.
const eventBuf = 16

// Client is a live SPICE session (main + display + inputs; cursor, playback,
// record, usbredir, and webdav best-effort).
//
// Create with Connect. There is no auto-reconnect for ticket sessions: on
// disconnect, open a new connection file and call Connect again.
//
// Hotkeys and Fullscreen from ConnectConfig are not retained on Client — the UI
// should keep the ConnectConfig (or those fields) for hotkey handling.
//
// The Connect dial/open context does not bound session lifetime after Connect
// returns; cancel that context only aborts dial/open. Use Close to tear down.
type Client struct {
	mu sync.Mutex

	sess   *session.Session
	title  string
	events chan Event

	eventsMu     sync.Mutex
	eventsClosed bool

	lifeCtx    context.Context
	lifeCancel context.CancelFunc

	display  *channel.Display
	inputs   *channel.Inputs
	cursor   *channel.Cursor
	playback *channel.Playback
	record   *channel.Record
	usb      []*channel.USBRedir
	webdav   *channel.WebDAV
	pubIn    *Inputs
	agent    *agent.Session

	mainMu sync.Mutex
	main   net.Conn

	closed     bool
	disconnect sync.Once
	wg         sync.WaitGroup

	// discMu protects discErr. Non-nil fatal errors always win over nil;
	// the first non-nil error is kept (Close must not erase a peer failure).
	discMu  sync.Mutex
	discErr error

	// fatalCh wakes the supervisor; discErr is the source of truth for the
	// Disconnected event error.
	fatalCh chan error
}

// ApplyPerformanceProfile updates preferred image compression / video codec
// on the live display channel (SPICE preference messages). Server may ignore
// if QEMU pins compression. Safe to call after Connect.
func (c *Client) ApplyPerformanceProfile(p PerformanceProfile) error {
	if c == nil {
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("spice: nil Client"))
	}
	c.mu.Lock()
	disp := c.display
	closed := c.closed
	c.mu.Unlock()
	if closed || disp == nil {
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("spice: display not ready"))
	}
	h264OK := h264.Available()
	disp.SetPreferences(p.ImageCompression(), p.VideoCodecs(h264OK))
	if err := disp.ApplyPreferences(); err != nil {
		return ux.New(ux.ClassTransport, ux.MsgTransport, err)
	}
	return nil
}

// Connect dials the SPICE peer described by cfg, completes main + child links,
// starts display/inputs/cursor/playback run loops, and returns a live Client.
//
// Password ownership:
//   - cfg.Password is deep-copied into the session; the caller's slice is not wiped.
//   - After session.New succeeds, only the session holds the ticket (local copy wiped).
//   - Client.Close → session.Close wipes the session-owned copy.
//
// CACertPEM is used only to build a TLS cert pool during dial setup; it is not
// retained or wiped by the Client (callers may wipe their own CACertPEM slice).
//
// Event order on success: EventConnected first, then any deferred EventError
// (e.g. cursor/playback open degrade). Disconnected is terminal; no auto-reconnect.
//
// Phase 1: AllowReconnect is ignored.
func Connect(ctx context.Context, cfg ConnectConfig) (*Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	ep, err := cfg.endpoint()
	if err != nil {
		return nil, err
	}

	// Deep-copy password for session.New; session makes its own private copy.
	var pw []byte
	if len(cfg.Password) > 0 {
		pw = make([]byte, len(cfg.Password))
		copy(pw, cfg.Password)
	}

	sess, err := session.New(session.Config{
		Endpoint:       ep,
		Password:       pw,
		AllowCleartext: cfg.AllowCleartext || ep.AllowCleartext,
		Dialer:         cfg.dialer,
		ShareDir:       cfg.ShareDir,
	})
	// Session owns the secret from here on success; always drop the local copy.
	security.Wipe(pw)
	if err != nil {
		return nil, err
	}

	if err := sess.DialMain(ctx); err != nil {
		_ = sess.Close()
		return nil, err
	}
	if err := sess.OpenChannels(ctx); err != nil {
		_ = sess.Close()
		return nil, err
	}

	lifeCtx, lifeCancel := context.WithCancel(context.Background())
	c := &Client{
		sess:       sess,
		title:      cfg.Title,
		events:     make(chan Event, eventBuf),
		lifeCtx:    lifeCtx,
		lifeCancel: lifeCancel,
		fatalCh:    make(chan error, 1),
	}

	bestEffortErrs, err := c.startChannels(cfg.Drivers, cfg.Profile)
	if err != nil {
		lifeCancel()
		_ = sess.Close()
		return nil, err
	}

	// Supervisor watches for fatal channel death → disconnect (no reconnect).
	c.wg.Add(1)
	go c.supervise()

	// Connected first; then deferred non-fatal errors (F5).
	c.emit(Event{Type: EventConnected})
	for _, e := range bestEffortErrs {
		if e != nil {
			c.emit(Event{Type: EventError, Err: e})
		}
	}
	return c, nil
}

// startChannels constructs display/inputs/cursor/playback/record/usb/webdav
// handlers and starts Run loops. Returns non-nil best-effort open errors
// (cursor/playback/record/usb/webdav degrade).
func (c *Client) startChannels(drivers Drivers, profile PerformanceProfile) (bestEffortErrs []error, err error) {
	dispConn := c.sess.DisplayConn()
	inpConn := c.sess.InputsConn()
	if dispConn == nil || inpConn == nil {
		return nil, ux.New(ux.ClassInternal, ux.MsgInternal,
			fmt.Errorf("spice: missing display or inputs connection after OpenChannels"))
	}

	// Display driver: public DisplayDriver → internal display.Driver.
	var ddrv display.Driver
	if drivers.Display != nil {
		ddrv = AsDriver(drivers.Display)
	} else {
		ddrv = display.NewNullDriver()
	}
	comp := display.NewCompositor(ddrv)
	// Product profile → SPICE preferred compression / video codec messages.
	// h264.Available must match DisplayChannelCaps advertisement.
	h264OK := h264.Available()
	c.display = channel.NewDisplayOpts(dispConn, comp, channel.DisplayOpts{
		PreferredImageCompression: profile.ImageCompression(),
		PreferredVideoCodecs:      profile.VideoCodecs(h264OK),
	})

	// Inputs.
	c.inputs = channel.NewInputs(inpConn, 0)
	c.pubIn = &Inputs{in: c.inputs}
	if init, ok := c.sess.MainInit(); ok {
		c.inputs.SetMouseMode(init.SupportedMouseModes, init.CurrentMouseMode)
		// Prefer CLIENT mouse mode when the server supports it.
		if main := c.sess.MainConn(); main != nil {
			if _, reqErr := c.inputs.RequestPreferredMouseMode(main); reqErr != nil {
				// Non-fatal: stay on current mode.
				log.Printf("spice: mouse mode request: %v", reqErr)
			}
		}
	}

	// Cursor (best-effort; conn may be nil). Defer EventError to after Connected.
	curConn := c.sess.CursorConn()
	if curConn != nil {
		c.cursor = channel.NewCursor(curConn, asCursorDriver(drivers.Cursor))
	} else if e := c.sess.CursorError(); e != nil {
		bestEffortErrs = append(bestEffortErrs, e)
	}

	// Playback (best-effort; conn may be nil). Nil driver → NullPlayback.
	pbConn := c.sess.PlaybackConn()
	if pbConn != nil {
		pdrv := asPlaybackDriver(drivers.Playback)
		if pdrv == nil {
			pdrv = channel.NewNullPlayback()
		}
		c.playback = channel.NewPlayback(pbConn, pdrv)
	} else if e := c.sess.PlaybackError(); e != nil {
		bestEffortErrs = append(bestEffortErrs, e)
	}

	// Record (best-effort; conn may be nil). Nil driver → NullRecord.
	recConn := c.sess.RecordConn()
	if recConn != nil {
		rdrv := asRecordDriver(drivers.Record)
		if rdrv == nil {
			rdrv = channel.NewNullRecord()
		}
		c.record = channel.NewRecord(recConn, rdrv)
	} else if e := c.sess.RecordError(); e != nil {
		bestEffortErrs = append(bestEffortErrs, e)
	}

	// USB redir (best-effort; zero or more channel ids).
	for _, u := range c.sess.USBConns() {
		if u.Conn == nil {
			continue
		}
		ur := channel.NewUSBRedir(u.Conn, channel.USBRedirOpts{ChannelID: u.ID})
		c.usb = append(c.usb, ur)
	}
	for _, e := range c.sess.USBErrors() {
		if e != nil {
			bestEffortErrs = append(bestEffortErrs, e)
		}
	}

	// WebDAV (best-effort; conn may be nil).
	wdConn := c.sess.WebDAVConn()
	if wdConn != nil {
		c.webdav = channel.NewWebDAV(wdConn, channel.WebDAVOpts{
			ShareRoot: c.sess.ShareDir(),
		})
	} else if e := c.sess.WebDAVError(); e != nil {
		bestEffortErrs = append(bestEffortErrs, e)
	}

	// Run loops (display SendInit emits preferred compression for the profile).
	c.wg.Add(1)
	go c.runFatal("display", func() error {
		return c.display.Run(c.lifeCtx)
	})

	c.wg.Add(1)
	go c.runFatal("inputs", func() error {
		return c.inputs.Run(c.lifeCtx, inpConn)
	})

	if c.cursor != nil {
		c.wg.Add(1)
		go c.runBestEffort("cursor", func() error {
			return c.cursor.Run(c.lifeCtx)
		})
	}

	if c.playback != nil {
		c.wg.Add(1)
		go c.runBestEffort("playback", func() error {
			return c.playback.Run(c.lifeCtx)
		})
	}

	if c.record != nil {
		c.wg.Add(1)
		go c.runBestEffort("record", func() error {
			return c.record.Run(c.lifeCtx)
		})
	}

	for i := range c.usb {
		ur := c.usb[i]
		name := fmt.Sprintf("usbredir[%d]", ur.ChannelID())
		c.wg.Add(1)
		go c.runBestEffort(name, func() error {
			return ur.Run(c.lifeCtx)
		})
	}

	if c.webdav != nil {
		c.wg.Add(1)
		go c.runBestEffort("webdav", func() error {
			return c.webdav.Run(c.lifeCtx)
		})
	}

	// Main channel: keep-alive / mouse mode until EOF.
	if main := c.sess.MainConn(); main != nil {
		c.wg.Add(1)
		go c.runFatal("main", func() error {
			return c.runMain(c.lifeCtx, main)
		})
	}

	return bestEffortErrs, nil
}

// runFatal runs fn; on unexpected error signals session-fatal disconnect.
func (c *Client) runFatal(name string, fn func() error) {
	defer c.wg.Done()
	err := fn()
	if c.lifeCtx.Err() != nil {
		// Close / cancel — not a peer failure to surface.
		return
	}
	if err == nil {
		return
	}
	if isBenignClose(err) {
		c.signalFatal(fmt.Errorf("spice: %s channel closed: %w", name, err))
		return
	}
	c.signalFatal(fmt.Errorf("spice: %s channel: %w", name, err))
}

// runBestEffort runs fn; errors are emitted as EventError, never session-fatal.
func (c *Client) runBestEffort(name string, fn func() error) {
	defer c.wg.Done()
	err := fn()
	if err == nil || c.lifeCtx.Err() != nil || isBenignClose(err) {
		return
	}
	log.Printf("spice: %s channel (degraded): %v", name, err)
	c.emit(Event{Type: EventError, Err: err})
}

// recordDisconnectErr stores err for the eventual EventDisconnected.
// Non-nil always wins over nil; the first non-nil error is kept.
func (c *Client) recordDisconnectErr(err error) {
	if err == nil {
		return
	}
	c.discMu.Lock()
	defer c.discMu.Unlock()
	if c.discErr == nil {
		c.discErr = err
	}
}

func (c *Client) disconnectErr() error {
	c.discMu.Lock()
	defer c.discMu.Unlock()
	return c.discErr
}

func (c *Client) signalFatal(err error) {
	c.recordDisconnectErr(err)
	select {
	case c.fatalCh <- err:
	default:
	}
	c.lifeCancel()
}

// supervise waits for fatal channel death or lifeCtx cancel (Close), then
// emits a single EventDisconnected. discErr is preserved across Close races:
// a pending fatal is never overwritten by a clean Close (F4).
// No auto-reconnect.
func (c *Client) supervise() {
	defer c.wg.Done()
	select {
	case <-c.lifeCtx.Done():
		select {
		case err := <-c.fatalCh:
			c.recordDisconnectErr(err)
		default:
		}
		c.finishDisconnect()
	case err := <-c.fatalCh:
		c.recordDisconnectErr(err)
		c.lifeCancel()
		c.finishDisconnect()
	}
}

// finishDisconnect tears down the session and emits EventDisconnected once.
// Safe from both supervise and Close (fallback); Once ensures a single emit.
// The disconnect error is always taken from discErr (never a Close-passed nil).
func (c *Client) finishDisconnect() {
	c.disconnect.Do(func() {
		// Final drain in case signalFatal raced with lifeCtx.Done.
		select {
		case err := <-c.fatalCh:
			c.recordDisconnectErr(err)
		default:
		}

		if c.sess != nil {
			_ = c.sess.Close()
		}

		err := c.disconnectErr()
		if err != nil {
			classified := ux.Classify(err)
			if classified != nil && classified.Class == ux.ClassInternal {
				err = ux.New(ux.ClassTransport, ux.MsgTransport, err)
			}
			c.emit(Event{Type: EventDisconnected, Err: err})
		} else {
			c.emit(Event{Type: EventDisconnected})
		}
	})
}

// runMain reads post-open main-channel messages (ping, mouse mode, agent).
func (c *Client) runMain(ctx context.Context, main net.Conn) error {
	c.mainMu.Lock()
	c.main = main
	c.mainMu.Unlock()
	c.agent = agent.New(&lockedMain{c: c}, c)
	var mainAck protocol.AckState

	// spice-gtk channel-main: if MAIN_INIT.agent_connected is set, send
	// SPICE_MSGC_MAIN_AGENT_START immediately. The server does not always
	// re-send AGENT_CONNECTED when the guest agent was already up.
	if init, ok := c.sess.MainInit(); ok && init.AgentConnected != 0 {
		if err := c.startAgent(init.AgentTokens); err != nil {
			log.Printf("spice: agent start from MAIN_INIT: %v", err)
			c.emit(Event{Type: EventError, Err: err})
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		msg, err := protocol.ReadMessage(main)
		if err != nil {
			if err == io.EOF || isBenignClose(err) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		// spice-gtk channel ACK window (required or server stops after N msgs).
		if err := mainAck.AfterRead(&lockedMain{c: c}); err != nil {
			return err
		}
		switch msg.Type {
		case protocol.MsgSetAck:
			_ = mainAck.OnSetAck(&lockedMain{c: c}, msg.Data)
		case protocol.MsgPing:
			if len(msg.Data) >= 12 {
				_ = c.writeMain(protocol.MsgcPong, msg.Data[:12])
			}
		case protocol.MsgMainMouseMode:
			if c.inputs != nil {
				_ = c.inputs.HandleMainMouseMode(msg)
			}
		case protocol.MsgMainAgentConnected, protocol.MsgMainAgentConnectedTokens:
			tokens := uint32(0)
			if len(msg.Data) >= 4 {
				tokens = uint32(msg.Data[0]) | uint32(msg.Data[1])<<8 | uint32(msg.Data[2])<<16 | uint32(msg.Data[3])<<24
			}
			if err := c.startAgent(tokens); err != nil {
				log.Printf("spice: agent start: %v", err)
				c.emit(Event{Type: EventError, Err: err})
			}
		case protocol.MsgMainAgentDisconnected:
			if c.agent != nil {
				c.agent.HandleMainAgentDisconnected()
			}
			c.emit(Event{Type: EventAgent, AgentActive: false})
		case protocol.MsgMainAgentData:
			if c.agent != nil {
				was := c.agent.Active()
				if err := c.agent.HandleAgentData(msg.Data); err != nil {
					log.Printf("spice: agent data: %v", err)
				}
				// Capabilities exchange completes Active(); notify UI then.
				if !was && c.agent.Active() {
					c.emit(Event{Type: EventAgent, AgentActive: true})
				}
			}
		case protocol.MsgMainAgentToken:
			n := uint32(1)
			if len(msg.Data) >= 4 {
				n = uint32(msg.Data[0]) | uint32(msg.Data[1])<<8 | uint32(msg.Data[2])<<16 | uint32(msg.Data[3])<<24
			}
			if c.agent != nil {
				c.agent.AddTokens(n)
			}
		default:
			// NOTIFY, migrate, …
		}
	}
}

// startAgent sends AGENT_START + capability announce (spice-gtk agent_start).
// Safe if already started (HandleMainAgentConnected is idempotent).
func (c *Client) startAgent(tokens uint32) error {
	if c == nil || c.agent == nil {
		return fmt.Errorf("spice: agent not ready")
	}
	return c.agent.HandleMainAgentConnected(tokens)
}

type lockedMain struct{ c *Client }

func (l *lockedMain) Write(p []byte) (int, error) {
	l.c.mainMu.Lock()
	defer l.c.mainMu.Unlock()
	if l.c.main == nil {
		return 0, fmt.Errorf("spice: main closed")
	}
	return l.c.main.Write(p)
}

func (c *Client) writeMain(typ uint16, body []byte) error {
	c.mainMu.Lock()
	defer c.mainMu.Unlock()
	if c.main == nil {
		return fmt.Errorf("spice: main closed")
	}
	return protocol.WriteMessage(c.main, typ, body)
}

// GuestGrabbed implements agent.ClipboardHandler.
func (c *Client) GuestGrabbed(selection uint8, types []uint32) {
	for _, t := range types {
		if t == agent.ClipboardUTF8Text {
			if err := c.agent.RequestGuestClipboard(); err != nil {
				log.Printf("spice: clipboard request: %v", err)
			}
			return
		}
	}
}

// GuestData implements agent.ClipboardHandler.
func (c *Client) GuestData(selection uint8, typ uint32, data []byte) {
	if typ != agent.ClipboardUTF8Text {
		return
	}
	c.emit(Event{Type: EventClipboard, ClipboardText: string(data)})
}

// GuestReleased implements agent.ClipboardHandler.
func (c *Client) GuestReleased(selection uint8) {}

// SetHostClipboard pushes UTF-8 text to the guest via vdagent (Phase 2).
func (c *Client) SetHostClipboard(text string) error {
	if c == nil || c.agent == nil {
		return fmt.Errorf("spice: agent not available")
	}
	return c.agent.SetHostClipboard(text)
}

// RequestGuestClipboard asks the guest for clipboard text via vdagent.
func (c *Client) RequestGuestClipboard() error {
	if c == nil || c.agent == nil {
		return fmt.Errorf("spice: agent not available")
	}
	return c.agent.RequestGuestClipboard()
}

// SetGuestDisplaySize asks the guest to resize via agent monitors config.
func (c *Client) SetGuestDisplaySize(width, height uint32) error {
	if c == nil || c.agent == nil {
		return fmt.Errorf("spice: agent not available")
	}
	return c.agent.SendMonitorsConfig(width, height)
}

// AgentActive reports whether the guest agent is connected with capabilities.
func (c *Client) AgentActive() bool {
	if c == nil || c.agent == nil {
		return false
	}
	return c.agent.Active()
}

// Events returns the lifecycle event channel.
//
// On a successful Connect the first event is EventConnected; EventError may
// follow for non-fatal degrade (e.g. cursor open failure). EventDisconnected
// is terminal. The channel is closed after Close completes. No auto-reconnect.
func (c *Client) Events() <-chan Event {
	if c == nil {
		ch := make(chan Event)
		close(ch)
		return ch
	}
	return c.events
}

// Inputs returns the keyboard/mouse inject surface.
func (c *Client) Inputs() *Inputs {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pubIn
}

// Title returns the connection title from ConnectConfig (may be empty).
// Hotkeys/Fullscreen are not stored on Client; keep ConnectConfig for those.
func (c *Client) Title() string {
	if c == nil {
		return ""
	}
	return c.title
}

// Wait blocks until the client disconnects or ctx is done.
// Returns the disconnect error if any, or ctx.Err().
//
// Wait drains Events; do not also consume Events() in another goroutine.
func (c *Client) Wait(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("spice: nil Client")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-c.events:
			if !ok {
				return nil
			}
			if ev.Type == EventDisconnected {
				return ev.Err
			}
		}
	}
}

// Close cancels run loops, closes the session (wiping the session-owned
// password), and closes the Events channel. Safe to call multiple times.
//
// If a fatal peer error was already recorded, EventDisconnected still carries
// that error (Close does not overwrite it with a clean nil). No auto-reconnect.
// After Close, the Client must not be reused.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	// Cancel run loops; supervise emits Disconnected (preserving discErr).
	if c.lifeCancel != nil {
		c.lifeCancel()
	}

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		log.Printf("spice: Close: timed out waiting for run loops")
	}

	// Fallback if supervise did not run (e.g. timeout): still emit once.
	// finishDisconnect drains fatalCh and uses discErr — never forces nil over fatal.
	c.finishDisconnect()

	var err error
	// sess.Close is idempotent; finishDisconnect already closed it on the happy path.
	if c.sess != nil {
		// Prefer returning the first close error if any; usually nil after Once path.
		if e := c.sess.Close(); e != nil && err == nil {
			err = e
		}
	}

	c.eventsMu.Lock()
	if !c.eventsClosed {
		c.eventsClosed = true
		close(c.events)
	}
	c.eventsMu.Unlock()

	return err
}

// emit sends ev without panicking if the events channel is closed.
func (c *Client) emit(ev Event) {
	if c == nil {
		return
	}
	c.eventsMu.Lock()
	defer c.eventsMu.Unlock()
	if c.eventsClosed || c.events == nil {
		return
	}
	select {
	case c.events <- ev:
	default:
		if ev.Type == EventDisconnected || ev.Type == EventConnected {
			select {
			case c.events <- ev:
			case <-time.After(50 * time.Millisecond):
				log.Printf("spice: dropping event %s (buffer full)", ev.Type)
			}
		}
	}
}

// isBenignClose reports EOF / net.ErrClosed style errors from channel teardown.
func isBenignClose(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF || err == net.ErrClosed {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "use of closed network connection") ||
		strings.Contains(s, "closed network connection")
}
