// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/maskraven/virt-viewer/internal/connector"
	"github.com/maskraven/virt-viewer/internal/protocol"
	"github.com/maskraven/virt-viewer/internal/security"
	"github.com/maskraven/virt-viewer/internal/ux"
)

// closeWaitTimeout is how long Close waits for in-flight opens to finish.
const closeWaitTimeout = 3 * time.Second

// Config holds parameters for a SPICE session.
//
// Password is deep-copied into the Session on New; the caller's slice is not
// wiped. Session.Close wipes the private copy.
type Config struct {
	// Endpoint describes how to reach the SPICE peer (direct or CONNECT+TLS).
	Endpoint connector.Endpoint
	// Password is the SPICE ticket / password (UTF-8 bytes, no trailing NUL).
	// Copied on New; never logged.
	Password []byte
	// AllowCleartext, if true, forces Endpoint.AllowCleartext when dialing.
	// Endpoint.AllowCleartext alone is also honored.
	AllowCleartext bool
	// Dialer establishes transport connections. nil uses connector.NewDialer().
	Dialer connector.Dialer
}

// Session owns SPICE session lifecycle: main link, MAIN_INIT session_id,
// CHANNELS_LIST, and parallel child channel links (display, inputs, cursor).
//
// No auto-reconnect for short-lived Proxmox tickets.
// Phase 1 does not open playback/record/usbredir/port/webdav.
type Session struct {
	endpoint connector.Endpoint
	password []byte // private copy; wiped on Close
	dialer   connector.Dialer

	// Session-scoped cancel: Close cancels to stop new opens.
	lifeCtx    context.Context
	lifeCancel context.CancelFunc

	mu       sync.Mutex
	mainConn net.Conn
	linked   bool // true after successful main link; mini-header framing applies
	closed   bool

	// connectionID is session_id from MAIN_INIT; published only after successful
	// OpenChannels (not used as the open success latch — see openState).
	connectionID uint32
	mainInit     *protocol.MainInit
	channelList  []protocol.ChannelID

	displayConn net.Conn
	inputsConn  net.Conn
	cursorConn  net.Conn
	cursorErr   error // non-nil if best-effort cursor open failed

	// openState serializes OpenChannels and distinguishes idle/ready/failed.
	openState openState

	// openSem limits concurrent child dial+link operations.
	openSem chan struct{}
	// openWG tracks in-flight OpenChannels work for Close wait.
	openWG sync.WaitGroup
}

// New builds a Session from cfg. Password is copied; cfg.Password is left intact.
func New(cfg Config) (*Session, error) {
	ep := cfg.Endpoint
	if cfg.AllowCleartext {
		ep.AllowCleartext = true
	}
	d := cfg.Dialer
	if d == nil {
		d = connector.NewDialer()
	}
	var pw []byte
	if len(cfg.Password) > 0 {
		pw = make([]byte, len(cfg.Password))
		copy(pw, cfg.Password)
	}
	lifeCtx, lifeCancel := context.WithCancel(context.Background())
	return &Session{
		endpoint:   ep,
		password:   pw,
		dialer:     d,
		lifeCtx:    lifeCtx,
		lifeCancel: lifeCancel,
		openSem:    make(chan struct{}, maxParallelChildOpens),
	}, nil
}

// DialMain dials the configured endpoint and completes main-channel link auth.
// On success the session owns the connection in mini-header mode (post-link).
// Child channels are not opened; call OpenChannels after DialMain.
func (s *Session) DialMain(ctx context.Context) error {
	if s == nil {
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: nil Session"))
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: closed"))
	}
	if s.mainConn != nil {
		s.mu.Unlock()
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: main already connected"))
	}
	dialer := s.dialer
	ep := s.endpoint
	s.mu.Unlock()

	conn, err := dialer.DialSPICE(ctx, ep)
	if err != nil {
		return mapDialError(err)
	}

	if err := s.LinkMain(ctx, conn); err != nil {
		_ = conn.Close()
		return err
	}
	return nil
}

// LinkMain performs the SPICE main-channel link handshake on an already-open
// connection (connection_id=0, channel_type=MAIN, channel_id=0).
//
// On success, s owns conn: subsequent channel I/O uses mini-header framing.
// On failure, the caller remains responsible for closing conn (DialMain closes it).
func (s *Session) LinkMain(ctx context.Context, conn net.Conn) error {
	if s == nil {
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: nil Session"))
	}
	if conn == nil {
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: nil conn"))
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: closed"))
	}
	if s.mainConn != nil {
		s.mu.Unlock()
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: main already connected"))
	}
	pw := append([]byte(nil), s.password...)
	s.mu.Unlock()
	defer security.Wipe(pw)

	if err := linkMainChannel(ctx, conn, pw); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: closed during link"))
	}
	if s.mainConn != nil {
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: main already connected"))
	}
	s.mainConn = conn
	s.linked = true
	return nil
}

// MainConn returns the linked main-channel connection, or nil if not linked.
// Callers must not Close it directly; use Session.Close.
func (s *Session) MainConn() net.Conn {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mainConn
}

// Linked reports whether main-channel link authentication completed successfully.
func (s *Session) Linked() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.linked
}

// ConnectionID returns the SPICE session_id from MAIN_INIT after a successful
// OpenChannels (0 if open has not succeeded). A failed OpenChannels leaves 0.
func (s *Session) ConnectionID() uint32 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connectionID
}

// ChannelsReady reports whether OpenChannels completed successfully
// (display+inputs linked; cursor may be degraded).
func (s *Session) ChannelsReady() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.openState == openReady
}

// DisplayConn returns the linked display channel connection, or nil.
func (s *Session) DisplayConn() net.Conn {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.displayConn
}

// InputsConn returns the linked inputs channel connection, or nil.
func (s *Session) InputsConn() net.Conn {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inputsConn
}

// CursorConn returns the linked cursor channel connection, or nil.
func (s *Session) CursorConn() net.Conn {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursorConn
}

// CursorError returns the best-effort cursor open error, if any.
func (s *Session) CursorError() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursorErr
}

// Close tears down channels in design order and wipes the session password.
//
//  1. cancel session context (stop new opens)
//  2. wait for in-flight OpenChannels (timeout) so mid-link work exits before
//     we close sockets it may still be using
//  3. close inputs → display → cursor → main
//  4. wipe password
//
// Waiting before channel close (vs after) is intentional: cancel unblocks open
// work via lifeCtx; closing sockets only after openWG settles avoids racing
// link handshakes that still hold the conn. Matches docs Close() ordering.
//
// Safe to call multiple times; does not panic if partially open.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.linked = false
	s.openState = openIdle // session is dead; not re-openable either way
	cancel := s.lifeCancel
	inputs := s.inputsConn
	display := s.displayConn
	cursor := s.cursorConn
	main := s.mainConn
	s.inputsConn = nil
	s.displayConn = nil
	s.cursorConn = nil
	s.mainConn = nil
	s.connectionID = 0
	s.mu.Unlock()

	// 1. cancel: stop accepting new channel opens
	if cancel != nil {
		cancel()
	}

	// 2. wait for in-flight OpenChannels (bounded) before tearing sockets.
	done := make(chan struct{})
	go func() {
		s.openWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(closeWaitTimeout):
		log.Printf("session: close: timed out waiting for channel opens")
	}

	// 3. close order: inputs → display → cursor → main
	var firstErr error
	for _, c := range []net.Conn{inputs, display, cursor, main} {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// 4. wipe password
	s.mu.Lock()
	security.Wipe(s.password)
	s.password = nil
	s.mu.Unlock()
	return firstErr
}
