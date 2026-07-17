// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package session_test

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/maskraven/spice-viewer/internal/connector"
	"github.com/maskraven/spice-viewer/internal/protocol"
	"github.com/maskraven/spice-viewer/internal/security"
	"github.com/maskraven/spice-viewer/internal/session"
	"github.com/maskraven/spice-viewer/internal/ux"
)

func vectorsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	dir := filepath.Join(root, "testdata", "vectors")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("testdata/vectors not found at %s: %v", dir, err)
	}
	return dir
}

func loadTicketKey(t *testing.T) (pubDER []byte, priv *rsa.PrivateKey) {
	t.Helper()
	dir := vectorsDir(t)
	der, err := os.ReadFile(filepath.Join(dir, "ticket_rsa1024_spki.der"))
	if err != nil {
		t.Fatal(err)
	}
	if len(der) != security.SpiceLinkPubKeyDERLen {
		t.Fatalf("spki der len %d", len(der))
	}
	pemBytes, err := os.ReadFile(filepath.Join(dir, "ticket_rsa1024_private.pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatal("no PEM block")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		k, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			t.Fatalf("parse private key: %v / %v", err, err2)
		}
		var ok bool
		key, ok = k.(*rsa.PrivateKey)
		if !ok {
			t.Fatalf("not RSA private key: %T", k)
		}
	}
	return der, key
}

// pipeDialer returns a fixed connection from DialSPICE (for mock link servers).
type pipeDialer struct {
	conn net.Conn
	err  error
	mu   sync.Mutex
	used bool
}

func (d *pipeDialer) DialSPICE(ctx context.Context, ep connector.Endpoint) (net.Conn, error) {
	if d.err != nil {
		return nil, d.err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.used {
		return nil, fmt.Errorf("pipeDialer: already used")
	}
	d.used = true
	return d.conn, nil
}

// errDialer always fails DialSPICE with err.
type errDialer struct{ err error }

func (d errDialer) DialSPICE(ctx context.Context, ep connector.Endpoint) (net.Conn, error) {
	return nil, d.err
}

// mockLinkOpts configures the server side of the SPICE link protocol.
type mockLinkOpts struct {
	pubDER        []byte
	priv          *rsa.PrivateKey
	expectedPass  []byte
	checkPassword bool
	forceResult   *uint32 // if set, write this result after auth regardless
	captureMess   **protocol.LinkMess
	replyError    uint32
	// hangAfterMess: read LinkMess then block until ctx-like peer closes / test ends.
	// Used for cancel/timeout tests (never sends a reply).
	hangAfterMess bool
}

func runMockLinkServer(t *testing.T, conn net.Conn, opts mockLinkOpts) error {
	t.Helper()
	defer conn.Close()

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
		// Block until client closes or deadline unblocks the peer read.
		// Keep conn open: a short sleep is not enough for cancel tests.
		buf := make([]byte, 1)
		_, _ = conn.Read(buf) // wait for EOF / deadline from peer
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

func TestDialMain_LinkOK(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("testpass")

	client, server := net.Pipe()
	defer client.Close()

	var gotMess *protocol.LinkMess
	errCh := make(chan error, 1)
	go func() {
		errCh <- runMockLinkServer(t, server, mockLinkOpts{
			pubDER:        pubDER,
			priv:          priv,
			expectedPass:  password,
			checkPassword: true,
			captureMess:   &gotMess,
		})
	}()

	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{
			Host:           "127.0.0.1",
			Port:           5900,
			AllowCleartext: true,
		},
		Password: password,
		Dialer:   &pipeDialer{conn: client},
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
	if !s.Linked() {
		t.Fatal("expected Linked")
	}
	if s.MainConn() == nil {
		t.Fatal("expected MainConn")
	}

	if gotMess == nil {
		t.Fatal("server did not capture LinkMess")
	}
	if gotMess.ConnectionID != 0 {
		t.Fatalf("connection_id=%d want 0", gotMess.ConnectionID)
	}
	if gotMess.ChannelType != protocol.ChannelMain || gotMess.ChannelID != 0 {
		t.Fatalf("channel type/id = %d/%d", gotMess.ChannelType, gotMess.ChannelID)
	}
	if !protocol.HasCap(gotMess.CommonCaps, protocol.CommonCapAuthSpice) {
		t.Fatal("missing AuthSpice cap")
	}
	if !protocol.HasCap(gotMess.CommonCaps, protocol.CommonCapMiniHeader) {
		t.Fatal("missing MiniHeader cap")
	}

	if err := <-errCh; err != nil {
		t.Fatalf("mock server: %v", err)
	}
}

func TestDialMain_BadPassword_TicketClass(t *testing.T) {
	pubDER, priv := loadTicketKey(t)

	client, server := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runMockLinkServer(t, server, mockLinkOpts{
			pubDER:        pubDER,
			priv:          priv,
			expectedPass:  []byte("correct"),
			checkPassword: true,
		})
	}()

	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Password: []byte("wrong-password"),
		Dialer:   &pipeDialer{conn: client},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.DialMain(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) || uxErr.Class != ux.ClassTicket {
		t.Fatalf("class = %v, err = %v", uxErr, err)
	}
	if ux.UserMessage(err) != ux.MsgTicket {
		t.Fatalf("message = %q", ux.UserMessage(err))
	}
	_ = <-errCh
}

func TestDialMain_BadLinkResult_TicketClass(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	denied := protocol.LinkErrPermissionDenied

	client, server := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runMockLinkServer(t, server, mockLinkOpts{
			pubDER:      pubDER,
			priv:        priv,
			forceResult: &denied,
		})
	}()

	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Password: []byte("testpass"),
		Dialer:   &pipeDialer{conn: client},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.DialMain(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) || uxErr.Class != ux.ClassTicket {
		t.Fatalf("class = %v, err = %v", uxErr, err)
	}
	_ = <-errCh
}

func TestLinkMain_WrongPubKey_Error(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		hdr, err := protocol.ReadLinkHeader(server)
		if err != nil {
			errCh <- err
			return
		}
		body := make([]byte, hdr.Size)
		if _, err := io.ReadFull(server, body); err != nil {
			errCh <- err
			return
		}
		garbage := bytes.Repeat([]byte{0x41}, protocol.SpiceLinkPubKeyBytes)
		reply := &protocol.LinkReply{
			Error:      protocol.LinkErrOK,
			PubKey:     garbage,
			CommonCaps: protocol.Phase1CommonCaps(),
		}
		pkt, err := reply.EncodePacket()
		if err != nil {
			errCh <- err
			return
		}
		_, err = server.Write(pkt)
		errCh <- err
	}()

	s, err := session.New(session.Config{Password: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.LinkMain(context.Background(), client)
	if err == nil {
		t.Fatal("expected error for bad pubkey")
	}
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) {
		t.Fatalf("want ux.Error, got %v", err)
	}
	if uxErr.Class == ux.ClassTicket {
		t.Fatalf("wrong class Ticket for pubkey parse: %v", err)
	}
	_ = <-errCh
}

func TestParseLinkPublicKey_WrongLength(t *testing.T) {
	_, err := security.ParseLinkPublicKey([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("length")) {
		t.Fatalf("error should mention length: %v", err)
	}
	_, err = security.ParseLinkPublicKey(bytes.Repeat([]byte{0}, 161))
	if err == nil {
		t.Fatal("expected error for 161")
	}
	_, err = security.ParseLinkPublicKey(bytes.Repeat([]byte{0}, 163))
	if err == nil {
		t.Fatal("expected error for 163")
	}
}

func TestMainLinkMess_ConnectionIDZero(t *testing.T) {
	m := protocol.NewMainLinkMess(nil)
	if m.ConnectionID != 0 {
		t.Fatalf("connection_id=%d", m.ConnectionID)
	}
	body := m.EncodeBody()
	id := binary.LittleEndian.Uint32(body[0:4])
	if id != 0 {
		t.Fatalf("wire connection_id=%d", id)
	}
}

func TestPasswordCopyAndWipeOnClose(t *testing.T) {
	orig := []byte("ticket-secret")
	s, err := session.New(session.Config{
		Password: orig,
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	orig[0] = 'X'
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.Linked() {
		t.Fatal("linked after close")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if orig[0] != 'X' {
		t.Fatal("caller slice should be untouched by Close")
	}
}

func TestDialMain_MapsProxyError(t *testing.T) {
	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Dialer:   errDialer{err: fmt.Errorf("%w: refused: 403 Forbidden", connector.ErrCONNECT)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.DialMain(context.Background())
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) || uxErr.Class != ux.ClassProxy {
		t.Fatalf("got %v", err)
	}
}

func TestDialMain_MapsProxyDialError(t *testing.T) {
	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Dialer:   errDialer{err: fmt.Errorf("%w: %v", connector.ErrProxyDial, errors.New("connection refused"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.DialMain(context.Background())
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) || uxErr.Class != ux.ClassProxy {
		t.Fatalf("got %v", err)
	}
}

func TestDialMain_MapsTLSSubject(t *testing.T) {
	base := fmt.Errorf("%w: %w", connector.ErrTLSVerify, connector.ErrTLSSubjectMismatch)
	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Dialer:   errDialer{err: base},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.DialMain(context.Background())
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) || uxErr.Class != ux.ClassTLSSubject {
		t.Fatalf("got %v", err)
	}
}

func TestDialMain_MapsTLSHandshake(t *testing.T) {
	base := fmt.Errorf("%w: %w", connector.ErrTLSHandshake, errors.New("remote error"))
	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Dialer:   errDialer{err: base},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.DialMain(context.Background())
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) || uxErr.Class != ux.ClassTLSTrust {
		t.Fatalf("got %v", err)
	}
}

func TestDialMain_CleartextDenied_Config(t *testing.T) {
	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "127.0.0.1", Port: 9},
		Password: []byte("x"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.DialMain(context.Background())
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) || uxErr.Class != ux.ClassConfig {
		t.Fatalf("got %v", err)
	}
	if uxErr.Message != ux.MsgConfigEndpoint {
		t.Fatalf("message = %q, want %q", uxErr.Message, ux.MsgConfigEndpoint)
	}
	if !errors.Is(err, connector.ErrCleartextDenied) {
		t.Fatalf("want ErrCleartextDenied wrapped, got %v", err)
	}
}

func TestLinkMain_ConnectionIDZeroCaptured(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("p")

	client, server := net.Pipe()
	defer client.Close()

	var gotMess *protocol.LinkMess
	errCh := make(chan error, 1)
	go func() {
		errCh <- runMockLinkServer(t, server, mockLinkOpts{
			pubDER:        pubDER,
			priv:          priv,
			expectedPass:  password,
			checkPassword: true,
			captureMess:   &gotMess,
		})
	}()

	s, err := session.New(session.Config{Password: password})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.LinkMain(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	if gotMess == nil || gotMess.ConnectionID != 0 {
		t.Fatalf("mess=%+v", gotMess)
	}
	_ = <-errCh
}

func TestLinkMain_PasswordTooLong_Config(t *testing.T) {
	pubDER, _ := loadTicketKey(t)
	// MaxSpicePasswordLen is 60; oversize must not surface as Ticket.
	password := bytes.Repeat([]byte("a"), security.MaxSpicePasswordLen+1)

	client, server := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		// Server will send reply; client fails at encrypt before auth write.
		defer server.Close()
		hdr, err := protocol.ReadLinkHeader(server)
		if err != nil {
			errCh <- err
			return
		}
		body := make([]byte, hdr.Size)
		if _, err := io.ReadFull(server, body); err != nil {
			errCh <- err
			return
		}
		reply := &protocol.LinkReply{
			Error:      protocol.LinkErrOK,
			PubKey:     pubDER,
			CommonCaps: protocol.Phase1CommonCaps(),
		}
		pkt, err := reply.EncodePacket()
		if err != nil {
			errCh <- err
			return
		}
		_, _ = server.Write(pkt)
		// Client may not send auth; wait for close.
		buf := make([]byte, 1)
		_, _ = server.Read(buf)
		errCh <- nil
	}()

	s, err := session.New(session.Config{Password: password})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.LinkMain(context.Background(), client)
	if err == nil {
		t.Fatal("expected oversize password error")
	}
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) || uxErr.Class != ux.ClassConfig {
		t.Fatalf("class = %v, err = %v", uxErr, err)
	}
	if uxErr.Message != ux.MsgConfigFieldTooLarge {
		t.Fatalf("message = %q", uxErr.Message)
	}
	_ = client.Close()
	_ = <-errCh
}

func TestLinkMain_ContextCancelMidRead(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runMockLinkServer(t, server, mockLinkOpts{hangAfterMess: true})
	}()

	s, err := session.New(session.Config{Password: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after mess is written and client is blocked on ReadLinkReply.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err = s.LinkMain(ctx, client)
	if err == nil {
		t.Fatal("expected cancel error")
	}
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) || uxErr.Class != ux.ClassTransport {
		t.Fatalf("class = %v, err = %v", uxErr, err)
	}
	// Cancel unblocks I/O via SetDeadline; mapLinkIOErr prefers ctx.Err().
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled in chain, got %v", err)
	}
	// Unblock hangAfterMess mock (server reads until peer close).
	_ = client.Close()
	_ = <-errCh
}

func TestLinkMain_InvalidMagic_ConfigProtocol(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		// Consume client mess
		hdr, err := protocol.ReadLinkHeader(server)
		if err != nil {
			errCh <- err
			return
		}
		body := make([]byte, hdr.Size)
		if _, err := io.ReadFull(server, body); err != nil {
			errCh <- err
			return
		}
		// Reply with wrong magic (not REDQ).
		bad := make([]byte, protocol.LinkHeaderSize)
		binary.LittleEndian.PutUint32(bad[0:4], 0xdeadbeef)
		binary.LittleEndian.PutUint32(bad[4:8], protocol.VersionMajor)
		binary.LittleEndian.PutUint32(bad[8:12], protocol.VersionMinor)
		binary.LittleEndian.PutUint32(bad[12:16], 0)
		_, err = server.Write(bad)
		errCh <- err
	}()

	s, err := session.New(session.Config{Password: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.LinkMain(context.Background(), client)
	if err == nil {
		t.Fatal("expected protocol error")
	}
	var uxErr *ux.Error
	if !errors.As(err, &uxErr) || uxErr.Class != ux.ClassConfig {
		t.Fatalf("class = %v, err = %v", uxErr, err)
	}
	if uxErr.Message != ux.MsgConfigProtocol {
		t.Fatalf("message = %q", uxErr.Message)
	}
	if !errors.Is(err, protocol.ErrInvalidMagic) {
		t.Fatalf("want ErrInvalidMagic, got %v", err)
	}
	_ = <-errCh
}
