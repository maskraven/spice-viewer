// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/maskraven/spice-viewer/internal/protocol"
)

// WebDAV is a Phase-3 best-effort folder-share channel scaffold.
//
// Wire: Port channel (PORT_INIT / PORT_EVENT) over SpiceVMC DATA frames.
// Open failure is never session-fatal. When ShareRoot is empty, frames are
// accepted and discarded safely. When ShareRoot is set, a minimal loop still
// runs; full phodav share UX is intentionally partial (hook for later).
type WebDAV struct {
	conn      net.Conn
	shareRoot string
	handler   VMCHandler

	mu        sync.Mutex
	unknown   map[uint16]int
	lastErr   error
	ack       protocol.AckState
	portName  string
	portOpen  bool
	initSeen  bool
	dataCount int
	dataBytes int
}

// WebDAVOpts configures NewWebDAV.
type WebDAVOpts struct {
	// ShareRoot is an optional host directory to share (empty = discard-only).
	ShareRoot string
	// Handler receives VMC DATA copies; nil discards via NullVMCHandler.
	Handler VMCHandler
}

// NewWebDAV wraps a linked webdav channel connection.
// conn may be nil for pure unit tests that only call HandleMessage.
func NewWebDAV(conn net.Conn, opts WebDAVOpts) *WebDAV {
	h := opts.Handler
	if h == nil {
		h = &NullVMCHandler{}
	}
	return &WebDAV{
		conn:      conn,
		shareRoot: opts.ShareRoot,
		handler:   h,
		unknown:   make(map[uint16]int),
	}
}

// ShareRoot returns the configured share path (may be empty).
func (w *WebDAV) ShareRoot() string {
	if w == nil {
		return ""
	}
	return w.shareRoot
}

// PortName returns the name from the last PORT_INIT, if any.
func (w *WebDAV) PortName() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.portName
}

// Stats returns VMC data frame count and bytes (tests / diagnostics).
func (w *WebDAV) Stats() (frames, bytes int) {
	if w == nil {
		return 0, 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dataCount, w.dataBytes
}

// LastError returns the most recent non-fatal handle error, if any.
func (w *WebDAV) LastError() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastErr
}

// Run reads mini-header messages until ctx cancel or connection close.
// Errors are best-effort (never session-fatal).
func (w *WebDAV) Run(ctx context.Context) error {
	if w == nil || w.conn == nil {
		return fmt.Errorf("channel: webdav: nil conn")
	}
	if w.shareRoot != "" {
		log.Printf("channel/webdav: share root configured (%s); full share UX is partial scaffold", w.shareRoot)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		msg, err := protocol.ReadMessage(w.conn)
		if err != nil {
			if err == io.EOF || isClosedConn(err) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("channel/webdav: read error (degraded): %v", err)
			return err
		}
		if err := w.ack.AfterRead(w.conn); err != nil {
			log.Printf("channel/webdav: ack: %v", err)
		}
		if err := w.HandleMessage(msg.Type, msg.Data); err != nil {
			log.Printf("channel/webdav: handle type %d: %v", msg.Type, err)
		}
	}
}

// HandleMessage dispatches one server→client webdav/port/vmc message.
// Never panics.
func (w *WebDAV) HandleMessage(typ uint16, data []byte) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("channel: webdav: panic recovered: %v", rec)
			log.Printf("%v", err)
		}
		if err != nil {
			w.mu.Lock()
			w.lastErr = err
			w.mu.Unlock()
		}
	}()

	switch typ {
	case protocol.MsgSetAck:
		if w.conn == nil {
			return nil
		}
		return w.ack.OnSetAck(w.conn, data)
	case protocol.MsgPing:
		if w.conn == nil {
			return nil
		}
		return protocol.WriteMessage(w.conn, protocol.MsgcPong, data)
	case protocol.MsgNotify, protocol.MsgWaitForChannels,
		protocol.MsgMigrate, protocol.MsgMigrateData, protocol.MsgDisconnecting:
		return nil
	case protocol.MsgPortInit:
		return w.handlePortInit(data)
	case protocol.MsgPortEvent:
		return w.handlePortEvent(data)
	case protocol.MsgSpiceVMCData:
		return w.handleVMCData(data)
	case protocol.MsgSpiceVMCCompressedData:
		c, err := ParseVMCCompressed(data)
		if err != nil {
			return fmt.Errorf("channel: webdav VMC_COMPRESSED: %w", err)
		}
		log.Printf("channel/webdav: discarding compressed VMC type=%d len=%d", c.Type, len(c.Data))
		return nil
	default:
		w.noteUnknown(typ)
		return nil
	}
}

func (w *WebDAV) handlePortInit(data []byte) error {
	init, err := protocol.DecodePortInit(data)
	if err != nil {
		return fmt.Errorf("channel: PORT_INIT: %w", err)
	}
	w.mu.Lock()
	w.portName = init.Name
	w.portOpen = init.Opened
	w.initSeen = true
	w.mu.Unlock()
	log.Printf("channel/webdav: PORT_INIT name=%q opened=%v shareRoot=%q",
		init.Name, init.Opened, w.shareRoot)

	// When a share root is configured, advertise OPENED so the guest may start
	// phodav traffic. Without a root we still OPENED to keep the channel alive
	// for discard-mode scaffolding (guest may send frames; we drop them).
	if w.conn != nil {
		if err := protocol.WriteMessage(w.conn, protocol.MsgcPortEvent,
			protocol.EncodePortEvent(protocol.PortEventOpened)); err != nil {
			return fmt.Errorf("channel: PORT_EVENT OPENED: %w", err)
		}
	}
	return nil
}

func (w *WebDAV) handlePortEvent(data []byte) error {
	ev, err := protocol.DecodePortEvent(data)
	if err != nil {
		return fmt.Errorf("channel: PORT_EVENT: %w", err)
	}
	w.mu.Lock()
	switch ev {
	case protocol.PortEventOpened:
		w.portOpen = true
	case protocol.PortEventClosed:
		w.portOpen = false
	}
	w.mu.Unlock()
	return nil
}

func (w *WebDAV) handleVMCData(data []byte) error {
	payload := ParseVMCData(data)
	cp := append([]byte(nil), payload...)
	w.mu.Lock()
	w.dataCount++
	w.dataBytes += len(cp)
	w.mu.Unlock()
	if w.handler != nil {
		w.handler.OnVMCData(cp)
	}
	// Full share protocol (phodav client_id framing) is deferred; optional
	// internal/webdav types exist for a future mux. Scaffold discards safely.
	return nil
}

func (w *WebDAV) noteUnknown(typ uint16) {
	w.mu.Lock()
	w.unknown[typ]++
	n := w.unknown[typ]
	w.mu.Unlock()
	if n == 1 {
		log.Printf("channel/webdav: ignoring message type %d", typ)
	}
}

// InitSeen reports whether PORT_INIT was handled (tests).
func (w *WebDAV) InitSeen() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.initSeen
}
