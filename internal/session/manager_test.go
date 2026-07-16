// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package session_test

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maskraven/virt-viewer/internal/connector"
	"github.com/maskraven/virt-viewer/internal/protocol"
	"github.com/maskraven/virt-viewer/internal/session"
	"github.com/maskraven/virt-viewer/internal/ux"
)

// multiPipeDialer hands out pre-built client-side pipe ends for each DialSPICE.
// Server sides are started by the test via startLinkServer / main post-link.
type multiPipeDialer struct {
	mu    sync.Mutex
	conns []net.Conn
	idx   int
	// dials counts DialSPICE calls (including failures when exhausted).
	dials atomic.Int32
}

func (d *multiPipeDialer) DialSPICE(ctx context.Context, ep connector.Endpoint) (net.Conn, error) {
	d.dials.Add(1)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.idx >= len(d.conns) {
		return nil, fmt.Errorf("multiPipeDialer: no more conns (idx=%d)", d.idx)
	}
	c := d.conns[d.idx]
	d.idx++
	return c, nil
}

// runChildLinkServer completes one child link handshake and records LinkMess.
// After link OK the server side stays open until closed (no further messages).
func runChildLinkServer(t *testing.T, conn net.Conn, pubDER []byte, priv *rsa.PrivateKey, password []byte, capture **protocol.LinkMess, encryptCount *atomic.Int32) error {
	t.Helper()
	// Do not close conn here — session owns the client side; test closes server.
	hdr, err := protocol.ReadLinkHeader(conn)
	if err != nil {
		return fmt.Errorf("child read header: %w", err)
	}
	if err := hdr.Validate(); err != nil {
		return err
	}
	body := make([]byte, hdr.Size)
	if _, err := io.ReadFull(conn, body); err != nil {
		return err
	}
	mess, err := protocol.DecodeLinkMess(body)
	if err != nil {
		return err
	}
	if capture != nil {
		*capture = mess
	}

	reply := &protocol.LinkReply{
		Error:      protocol.LinkErrOK,
		PubKey:     pubDER,
		CommonCaps: protocol.Phase1CommonCaps(),
	}
	pkt, err := reply.EncodePacket()
	if err != nil {
		return err
	}
	if _, err := conn.Write(pkt); err != nil {
		return err
	}

	authBuf := make([]byte, 4+protocol.SpiceTicketCiphertextLen)
	if _, err := io.ReadFull(conn, authBuf); err != nil {
		return fmt.Errorf("child read auth: %w", err)
	}
	auth, err := protocol.DecodeAuthSpice(authBuf)
	if err != nil {
		return err
	}
	plain, err := rsa.DecryptOAEP(sha1.New(), nil, priv, auth.Ciphertext, nil)
	if err != nil {
		_ = protocol.WriteLinkResult(conn, protocol.LinkErrPermissionDenied)
		return fmt.Errorf("decrypt: %w", err)
	}
	if len(plain) > 0 && plain[len(plain)-1] == 0 {
		plain = plain[:len(plain)-1]
	}
	if !bytes.Equal(plain, password) {
		_ = protocol.WriteLinkResult(conn, protocol.LinkErrPermissionDenied)
		return fmt.Errorf("password mismatch")
	}
	if encryptCount != nil {
		encryptCount.Add(1)
	}
	return protocol.WriteLinkResult(conn, protocol.LinkErrOK)
}

// serveMainPostLink sends MAIN_INIT then waits for ATTACH_CHANNELS, then CHANNELS_LIST.
func serveMainPostLink(t *testing.T, conn net.Conn, sessionID uint32, channels []protocol.ChannelID) error {
	t.Helper()
	init := protocol.MainInit{
		SessionID:           sessionID,
		DisplayChannelsHint: 1,
		SupportedMouseModes: 3,
		CurrentMouseMode:    1,
	}
	if err := protocol.WriteMessage(conn, protocol.MsgMainInit, init.Encode()); err != nil {
		return fmt.Errorf("write MAIN_INIT: %w", err)
	}

	// Expect ATTACH_CHANNELS (may interleave; loop a few messages).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		msg, err := protocol.ReadMessage(conn)
		if err != nil {
			return fmt.Errorf("read after MAIN_INIT: %w", err)
		}
		if msg.Type == protocol.MsgcMainAttachChannels {
			break
		}
		// Ignore other client messages (ACK_SYNC, etc.).
	}

	list := protocol.ChannelsList{Channels: channels}
	if err := protocol.WriteMessage(conn, protocol.MsgMainChannelsList, list.Encode()); err != nil {
		return fmt.Errorf("write CHANNELS_LIST: %w", err)
	}
	return nil
}

func defaultChannelList() []protocol.ChannelID {
	return []protocol.ChannelID{
		{Type: protocol.ChannelDisplay, ID: 0},
		{Type: protocol.ChannelInputs, ID: 0},
		{Type: protocol.ChannelCursor, ID: 0},
		{Type: protocol.ChannelPlayback, ID: 0}, // must not be opened
	}
}

// runMockLinkServerKeepOpen is like runMockLinkServer but does not close conn.
func runMockLinkServerKeepOpen(t *testing.T, conn net.Conn, opts mockLinkOpts) error {
	t.Helper()

	hdr, err := protocol.ReadLinkHeader(conn)
	if err != nil {
		return fmt.Errorf("read link header: %w", err)
	}
	if err := hdr.Validate(); err != nil {
		return err
	}
	body := make([]byte, hdr.Size)
	if _, err := io.ReadFull(conn, body); err != nil {
		return fmt.Errorf("read link mess body: %w", err)
	}
	mess, err := protocol.DecodeLinkMess(body)
	if err != nil {
		return err
	}
	if opts.captureMess != nil {
		*opts.captureMess = mess
	}
	if opts.hangAfterMess {
		buf := make([]byte, 1)
		_, _ = conn.Read(buf)
		return nil
	}
	if len(opts.pubDER) != protocol.SpiceLinkPubKeyBytes {
		return fmt.Errorf("mock: pubDER length %d want %d", len(opts.pubDER), protocol.SpiceLinkPubKeyBytes)
	}
	reply := &protocol.LinkReply{
		Error:      opts.replyError,
		PubKey:     opts.pubDER,
		CommonCaps: protocol.Phase1CommonCaps(),
	}
	pkt, err := reply.EncodePacket()
	if err != nil {
		return err
	}
	if _, err := conn.Write(pkt); err != nil {
		return err
	}

	authBuf := make([]byte, 4+protocol.SpiceTicketCiphertextLen)
	if _, err := io.ReadFull(conn, authBuf); err != nil {
		return fmt.Errorf("read auth: %w", err)
	}
	auth, err := protocol.DecodeAuthSpice(authBuf)
	if err != nil {
		return err
	}
	if auth.Mechanism != protocol.AuthMechanismSpice {
		return fmt.Errorf("mechanism %d want 1", auth.Mechanism)
	}

	result := protocol.LinkErrOK
	if opts.forceResult != nil {
		result = *opts.forceResult
	} else if opts.checkPassword {
		plain, err := rsa.DecryptOAEP(sha1.New(), nil, opts.priv, auth.Ciphertext, nil)
		if err != nil {
			result = protocol.LinkErrPermissionDenied
		} else {
			if len(plain) > 0 && plain[len(plain)-1] == 0 {
				plain = plain[:len(plain)-1]
			}
			if !bytes.Equal(plain, opts.expectedPass) {
				result = protocol.LinkErrPermissionDenied
			}
		}
	}
	return protocol.WriteLinkResult(conn, result)
}

func TestOpenChannels_MultiChannel_SessionIDAndReEncrypt(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("ticket-secret")
	const sessionID uint32 = 0x0a0b0c0d

	// Pipes: [0]=main, [1]=display, [2]=inputs, [3]=cursor
	clients := make([]net.Conn, 4)
	servers := make([]net.Conn, 4)
	for i := 0; i < 4; i++ {
		c, s := net.Pipe()
		clients[i], servers[i] = c, s
	}
	defer func() {
		for i := 0; i < 4; i++ {
			_ = clients[i].Close()
			_ = servers[i].Close()
		}
	}()

	dialer := &multiPipeDialer{conns: clients}

	var (
		mainMess *protocol.LinkMess
		encrypts atomic.Int32
	)
	// Child servers are bound to fixed dial order: dial 0 main, then parallel children
	// may dial in any order among 1..3. Capture by channel type instead.

	childByType := struct {
		mu   sync.Mutex
		mess map[uint8]*protocol.LinkMess
	}{mess: make(map[uint8]*protocol.LinkMess)}

	errCh := make(chan error, 8)

	// Main: link + MAIN_INIT + list
	go func() {
		if err := runMockLinkServerKeepOpen(t, servers[0], mockLinkOpts{
			pubDER:        pubDER,
			priv:          priv,
			expectedPass:  password,
			checkPassword: true,
			captureMess:   &mainMess,
		}); err != nil {
			errCh <- fmt.Errorf("main link: %w", err)
			return
		}
		encrypts.Add(1) // main encrypt
		if err := serveMainPostLink(t, servers[0], sessionID, defaultChannelList()); err != nil {
			errCh <- fmt.Errorf("main post-link: %w", err)
			return
		}
		errCh <- nil
	}()

	// Child servers on pipes 1..3 (dial order = open order; parallel)
	for i := 1; i < 4; i++ {
		i := i
		go func() {
			var captured *protocol.LinkMess
			err := runChildLinkServer(t, servers[i], pubDER, priv, password, &captured, &encrypts)
			if captured != nil {
				childByType.mu.Lock()
				childByType.mess[captured.ChannelType] = captured
				childByType.mu.Unlock()
			}
			errCh <- err
		}()
	}

	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "127.0.0.1", Port: 5900, AllowCleartext: true},
		Password: password,
		Dialer:   dialer,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.DialMain(ctx); err != nil {
		t.Fatalf("DialMain: %v", err)
	}
	if err := s.OpenChannels(ctx); err != nil {
		t.Fatalf("OpenChannels: %v", err)
	}

	if s.ConnectionID() != sessionID {
		t.Fatalf("ConnectionID=%#x want %#x", s.ConnectionID(), sessionID)
	}
	if s.DisplayConn() == nil || s.InputsConn() == nil || s.CursorConn() == nil {
		t.Fatalf("missing child conns display=%v inputs=%v cursor=%v",
			s.DisplayConn() != nil, s.InputsConn() != nil, s.CursorConn() != nil)
	}
	if s.CursorError() != nil {
		t.Fatalf("unexpected cursor error: %v", s.CursorError())
	}

	// Main mess connection_id=0
	if mainMess == nil || mainMess.ConnectionID != 0 {
		t.Fatalf("main mess=%+v", mainMess)
	}

	// Children: session_id + correct types; no playback opened (only 4 dials total)
	if dialer.dials.Load() != 4 {
		t.Fatalf("dials=%d want 4 (main+display+inputs+cursor)", dialer.dials.Load())
	}
	// encrypts: main + 3 children
	if encrypts.Load() != 4 {
		t.Fatalf("encrypt/auth count=%d want 4", encrypts.Load())
	}

	childByType.mu.Lock()
	defer childByType.mu.Unlock()
	for _, typ := range []uint8{protocol.ChannelDisplay, protocol.ChannelInputs, protocol.ChannelCursor} {
		m := childByType.mess[typ]
		if m == nil {
			t.Fatalf("missing LinkMess for channel type %d", typ)
		}
		if m.ConnectionID != sessionID {
			t.Fatalf("type %d connection_id=%#x want %#x", typ, m.ConnectionID, sessionID)
		}
		if m.ChannelID != 0 {
			t.Fatalf("type %d channel_id=%d", typ, m.ChannelID)
		}
	}
	if _, ok := childByType.mess[protocol.ChannelPlayback]; ok {
		t.Fatal("playback must not be opened")
	}

	// Drain server errors
	for i := 0; i < 4; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("server: %v", err)
		}
	}
}

func TestOpenChannels_CursorFailureNonFatal(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("p")
	const sessionID uint32 = 99

	// main, display, inputs succeed; cursor dial fails
	clients := make([]net.Conn, 3)
	servers := make([]net.Conn, 3)
	for i := 0; i < 3; i++ {
		c, s := net.Pipe()
		clients[i], servers[i] = c, s
	}
	defer func() {
		for i := 0; i < 3; i++ {
			_ = clients[i].Close()
			_ = servers[i].Close()
		}
	}()

	// Scripted: dial0 main, dial1 display, dial2 inputs, dial3 cursor → fail
	// Parallel children make dial order nondeterministic. Fail only when
	// channel type would be cursor by denying the 4th dial.
	// Better: list has cursor; use dialer that fails on 4th call.
	d := &multiPipeDialer{conns: clients}
	// After 3 successful dials, further DialSPICE fails → whichever child is
	// last may fail. For cursor non-fatal we need display+inputs OK and cursor fail.
	// Use a dialer that inspects nothing but fails after N successes with typed map.
	// Simpler approach: CHANNELS_LIST without cursor first proves optional;
	// plus a test where cursor link returns PermissionDenied.

	// Approach: only open display+inputs+cursor servers, but force cursor link result denied.
	// 4 pipes again; cursor server returns bad result.
	for i := 0; i < 3; i++ {
		_ = clients[i].Close()
		_ = servers[i].Close()
	}
	clients = make([]net.Conn, 4)
	servers = make([]net.Conn, 4)
	for i := 0; i < 4; i++ {
		c, s := net.Pipe()
		clients[i], servers[i] = c, s
	}
	defer func() {
		for i := 0; i < 4; i++ {
			_ = clients[i].Close()
			_ = servers[i].Close()
		}
	}()
	d = &multiPipeDialer{conns: clients}

	errCh := make(chan error, 8)
	go func() {
		if err := runMockLinkServerKeepOpen(t, servers[0], mockLinkOpts{
			pubDER: pubDER, priv: priv, expectedPass: password, checkPassword: true,
		}); err != nil {
			errCh <- err
			return
		}
		errCh <- serveMainPostLink(t, servers[0], sessionID, defaultChannelList())
	}()

	// Children 1..3: first two OK (display/inputs), one fails ticket — but assignment is by dial order not type.
	// Force failure by channel type: each child server reads mess and fails if type==CURSOR.
	for i := 1; i < 4; i++ {
		i := i
		go func() {
			errCh <- runChildLinkServerMaybeFailCursor(t, servers[i], pubDER, priv, password, true)
		}()
	}

	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Password: password,
		Dialer:   d,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.DialMain(ctx); err != nil {
		t.Fatalf("DialMain: %v", err)
	}
	if err := s.OpenChannels(ctx); err != nil {
		t.Fatalf("OpenChannels should succeed with cursor fail: %v", err)
	}
	if s.DisplayConn() == nil || s.InputsConn() == nil {
		t.Fatal("display/inputs required")
	}
	if s.CursorConn() != nil {
		t.Fatal("cursor conn should be nil after failure")
	}
	if s.CursorError() == nil {
		t.Fatal("expected CursorError")
	}
	var uxErr *ux.Error
	if !errors.As(s.CursorError(), &uxErr) || uxErr.Class != ux.ClassTicket {
		t.Fatalf("cursor err class: %v", s.CursorError())
	}

	for i := 0; i < 4; i++ {
		if err := <-errCh; err != nil {
			// cursor server may report decrypt/auth failure path — ignore non-nil for cursor path
			// runChildLinkServerMaybeFailCursor returns nil after writing denied result
			t.Logf("server %d: %v", i, err)
		}
	}
}

// runChildLinkServerMaybeFailCursor completes link OK for non-cursor; for cursor writes PermissionDenied.
func runChildLinkServerMaybeFailCursor(t *testing.T, conn net.Conn, pubDER []byte, priv *rsa.PrivateKey, password []byte, failCursor bool) error {
	t.Helper()
	hdr, err := protocol.ReadLinkHeader(conn)
	if err != nil {
		return err
	}
	body := make([]byte, hdr.Size)
	if _, err := io.ReadFull(conn, body); err != nil {
		return err
	}
	mess, err := protocol.DecodeLinkMess(body)
	if err != nil {
		return err
	}
	reply := &protocol.LinkReply{Error: protocol.LinkErrOK, PubKey: pubDER, CommonCaps: protocol.Phase1CommonCaps()}
	pkt, err := reply.EncodePacket()
	if err != nil {
		return err
	}
	if _, err := conn.Write(pkt); err != nil {
		return err
	}
	authBuf := make([]byte, 4+protocol.SpiceTicketCiphertextLen)
	if _, err := io.ReadFull(conn, authBuf); err != nil {
		return err
	}
	if failCursor && mess.ChannelType == protocol.ChannelCursor {
		return protocol.WriteLinkResult(conn, protocol.LinkErrPermissionDenied)
	}
	// Validate password for required channels
	auth, err := protocol.DecodeAuthSpice(authBuf)
	if err != nil {
		return err
	}
	plain, err := rsa.DecryptOAEP(sha1.New(), nil, priv, auth.Ciphertext, nil)
	if err != nil {
		_ = protocol.WriteLinkResult(conn, protocol.LinkErrPermissionDenied)
		return err
	}
	if len(plain) > 0 && plain[len(plain)-1] == 0 {
		plain = plain[:len(plain)-1]
	}
	if !bytes.Equal(plain, password) {
		_ = protocol.WriteLinkResult(conn, protocol.LinkErrPermissionDenied)
		return fmt.Errorf("bad password")
	}
	return protocol.WriteLinkResult(conn, protocol.LinkErrOK)
}

func TestOpenChannels_DisplayFailureFatal(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("p")
	const sessionID uint32 = 7

	clients := make([]net.Conn, 4)
	servers := make([]net.Conn, 4)
	for i := 0; i < 4; i++ {
		c, s := net.Pipe()
		clients[i], servers[i] = c, s
	}
	defer func() {
		for i := 0; i < 4; i++ {
			_ = clients[i].Close()
			_ = servers[i].Close()
		}
	}()

	errCh := make(chan error, 8)
	go func() {
		if err := runMockLinkServerKeepOpen(t, servers[0], mockLinkOpts{
			pubDER: pubDER, priv: priv, expectedPass: password, checkPassword: true,
		}); err != nil {
			errCh <- err
			return
		}
		errCh <- serveMainPostLink(t, servers[0], sessionID, defaultChannelList())
	}()
	for i := 1; i < 4; i++ {
		i := i
		go func() {
			errCh <- runChildLinkServerMaybeFailDisplay(t, servers[i], pubDER, priv, password)
		}()
	}

	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Password: password,
		Dialer:   &multiPipeDialer{conns: clients},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.DialMain(ctx); err != nil {
		t.Fatal(err)
	}
	err = s.OpenChannels(ctx)
	if err == nil {
		t.Fatal("expected fatal display error")
	}
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) || uxErr.Class != ux.ClassTicket {
		t.Fatalf("want Ticket class, got %v", err)
	}
	if s.DisplayConn() != nil || s.InputsConn() != nil {
		t.Fatal("partial children should not be retained on fatal failure")
	}

	// Drain servers (some may error on early close)
	for i := 0; i < 4; i++ {
		<-errCh
	}
}

func runChildLinkServerMaybeFailDisplay(t *testing.T, conn net.Conn, pubDER []byte, priv *rsa.PrivateKey, password []byte) error {
	t.Helper()
	hdr, err := protocol.ReadLinkHeader(conn)
	if err != nil {
		return err
	}
	body := make([]byte, hdr.Size)
	if _, err := io.ReadFull(conn, body); err != nil {
		return err
	}
	mess, err := protocol.DecodeLinkMess(body)
	if err != nil {
		return err
	}
	reply := &protocol.LinkReply{Error: protocol.LinkErrOK, PubKey: pubDER, CommonCaps: protocol.Phase1CommonCaps()}
	pkt, _ := reply.EncodePacket()
	if _, err := conn.Write(pkt); err != nil {
		return err
	}
	authBuf := make([]byte, 4+protocol.SpiceTicketCiphertextLen)
	if _, err := io.ReadFull(conn, authBuf); err != nil {
		return err
	}
	if mess.ChannelType == protocol.ChannelDisplay {
		return protocol.WriteLinkResult(conn, protocol.LinkErrPermissionDenied)
	}
	return protocol.WriteLinkResult(conn, protocol.LinkErrOK)
}

func TestOpenChannels_InputsFailureFatal(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("p")
	const sessionID uint32 = 8

	clients := make([]net.Conn, 4)
	servers := make([]net.Conn, 4)
	for i := 0; i < 4; i++ {
		c, s := net.Pipe()
		clients[i], servers[i] = c, s
	}
	defer func() {
		for i := 0; i < 4; i++ {
			_ = clients[i].Close()
			_ = servers[i].Close()
		}
	}()

	errCh := make(chan error, 8)
	go func() {
		if err := runMockLinkServerKeepOpen(t, servers[0], mockLinkOpts{
			pubDER: pubDER, priv: priv, expectedPass: password, checkPassword: true,
		}); err != nil {
			errCh <- err
			return
		}
		errCh <- serveMainPostLink(t, servers[0], sessionID, defaultChannelList())
	}()
	for i := 1; i < 4; i++ {
		i := i
		go func() {
			errCh <- runChildLinkServerFailType(t, servers[i], pubDER, protocol.ChannelInputs)
		}()
	}

	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Password: password,
		Dialer:   &multiPipeDialer{conns: clients},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.DialMain(ctx); err != nil {
		t.Fatal(err)
	}
	err = s.OpenChannels(ctx)
	if err == nil {
		t.Fatal("expected inputs fatal")
	}
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) || uxErr.Class != ux.ClassTicket {
		t.Fatalf("got %v", err)
	}
	for i := 0; i < 4; i++ {
		<-errCh
	}
}

func runChildLinkServerFailType(t *testing.T, conn net.Conn, pubDER []byte, failType uint8) error {
	t.Helper()
	hdr, err := protocol.ReadLinkHeader(conn)
	if err != nil {
		return err
	}
	body := make([]byte, hdr.Size)
	if _, err := io.ReadFull(conn, body); err != nil {
		return err
	}
	mess, err := protocol.DecodeLinkMess(body)
	if err != nil {
		return err
	}
	reply := &protocol.LinkReply{Error: protocol.LinkErrOK, PubKey: pubDER, CommonCaps: protocol.Phase1CommonCaps()}
	pkt, _ := reply.EncodePacket()
	if _, err := conn.Write(pkt); err != nil {
		return err
	}
	authBuf := make([]byte, 4+protocol.SpiceTicketCiphertextLen)
	if _, err := io.ReadFull(conn, authBuf); err != nil {
		return err
	}
	if mess.ChannelType == failType {
		return protocol.WriteLinkResult(conn, protocol.LinkErrPermissionDenied)
	}
	return protocol.WriteLinkResult(conn, protocol.LinkErrOK)
}

func TestOpenChannels_MissingDisplayInList_Fatal(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("p")

	c, s := net.Pipe()
	defer c.Close()
	defer s.Close()

	errCh := make(chan error, 1)
	go func() {
		if err := runMockLinkServerKeepOpen(t, s, mockLinkOpts{
			pubDER: pubDER, priv: priv, expectedPass: password, checkPassword: true,
		}); err != nil {
			errCh <- err
			return
		}
		// List without DISPLAY
		errCh <- serveMainPostLink(t, s, 1, []protocol.ChannelID{
			{Type: protocol.ChannelInputs, ID: 0},
			{Type: protocol.ChannelCursor, ID: 0},
		})
	}()

	sess, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Password: password,
		Dialer:   &multiPipeDialer{conns: []net.Conn{c}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	if err := sess.DialMain(context.Background()); err != nil {
		t.Fatal(err)
	}
	err = sess.OpenChannels(context.Background())
	if err == nil {
		t.Fatal("expected missing display error")
	}
	if !errors.As(err, new(*ux.Error)) {
		t.Fatalf("got %v", err)
	}
	<-errCh
}

func TestClose_OrderNoPanic(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("p")
	const sessionID uint32 = 11

	clients := make([]net.Conn, 4)
	servers := make([]net.Conn, 4)
	for i := 0; i < 4; i++ {
		c, s := net.Pipe()
		clients[i], servers[i] = c, s
	}

	errCh := make(chan error, 8)
	go func() {
		if err := runMockLinkServerKeepOpen(t, servers[0], mockLinkOpts{
			pubDER: pubDER, priv: priv, expectedPass: password, checkPassword: true,
		}); err != nil {
			errCh <- err
			return
		}
		errCh <- serveMainPostLink(t, servers[0], sessionID, defaultChannelList())
	}()
	for i := 1; i < 4; i++ {
		i := i
		go func() {
			errCh <- runChildLinkServer(t, servers[i], pubDER, priv, password, nil, nil)
		}()
	}

	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Password: password,
		Dialer:   &multiPipeDialer{conns: clients},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := s.DialMain(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.OpenChannels(ctx); err != nil {
		t.Fatal(err)
	}

	// Close should not panic; double Close ok.
	if err := s.Close(); err != nil {
		// net.Pipe close may return error if already closed by peer; ignore class.
		t.Logf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if s.Linked() {
		t.Fatal("linked after close")
	}
	if s.DisplayConn() != nil || s.InputsConn() != nil || s.MainConn() != nil {
		t.Fatal("conns should be nil after close")
	}

	// Close server sides
	for i := 0; i < 4; i++ {
		_ = servers[i].Close()
	}
	for i := 0; i < 4; i++ {
		<-errCh
	}
}

func TestClose_PartialSessionNoPanic(t *testing.T) {
	// Only DialMain completed — no children.
	pubDER, priv := loadTicketKey(t)
	c, s := net.Pipe()
	defer s.Close()

	go func() {
		_ = runMockLinkServerKeepOpen(t, s, mockLinkOpts{
			pubDER: pubDER, priv: priv, expectedPass: []byte("x"), checkPassword: true,
		})
	}()

	sess, err := session.New(session.Config{
		Password: []byte("x"),
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Dialer:   &multiPipeDialer{conns: []net.Conn{c}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.DialMain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := sess.Close(); err != nil {
		t.Logf("Close: %v", err)
	}
	// New session never dialed
	sess2, _ := session.New(session.Config{Password: []byte("y")})
	if err := sess2.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenChannels_RequiresMainLinked(t *testing.T) {
	s, err := session.New(session.Config{Password: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.OpenChannels(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}
