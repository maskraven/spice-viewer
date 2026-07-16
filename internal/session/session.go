// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/maskraven/virt-viewer/internal/connector"
	"github.com/maskraven/virt-viewer/internal/security"
	"github.com/maskraven/virt-viewer/internal/ux"
)

// Config holds parameters for a SPICE session main-channel link (PR 06).
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

// Session owns SPICE session lifecycle. Phase 1 PR 06: main channel link only
// (no child channels, no MAIN_INIT handling required after link OK).
//
// No auto-reconnect for short-lived Proxmox tickets.
type Session struct {
	endpoint connector.Endpoint
	password []byte // private copy; wiped on Close
	dialer   connector.Dialer

	mu       sync.Mutex
	mainConn net.Conn
	linked   bool // true after successful link; mini-header framing applies
	closed   bool
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
	return &Session{
		endpoint: ep,
		password: pw,
		dialer:   d,
	}, nil
}

// DialMain dials the configured endpoint and completes main-channel link auth.
// On success the session owns the connection in mini-header mode (post-link).
// Prefer stopping after a successful link result in PR 06 (no MAIN_INIT required).
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
	// Snapshot password under lock for encrypt; do not hold lock during I/O.
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

// Close tears down the main connection and wipes the session password copy.
// Safe to call multiple times.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.linked = false
	var err error
	if s.mainConn != nil {
		err = s.mainConn.Close()
		s.mainConn = nil
	}
	security.Wipe(s.password)
	s.password = nil
	return err
}
