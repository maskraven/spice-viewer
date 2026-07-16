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

	"github.com/maskraven/virt-viewer/internal/connector"
	"github.com/maskraven/virt-viewer/internal/protocol"
	"github.com/maskraven/virt-viewer/internal/security"
	"github.com/maskraven/virt-viewer/internal/session"
	"github.com/maskraven/virt-viewer/internal/ux"
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
		// try PKCS8
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

// mockLinkServer speaks the server side of the SPICE link protocol on conn.
// expectedPassword is compared after OAEP decrypt (without trailing NUL).
// result is the link-result code written after auth (LinkErrOK on success path
// only if password matches when checkPassword is true).
type mockLinkOpts struct {
	pubDER         []byte
	priv           *rsa.PrivateKey
	expectedPass   []byte
	checkPassword  bool
	forceResult    *uint32 // if set, write this result after auth regardless
	badPubKeyLen   int     // if >0, send pubkey of this length (invalid)
	captureMess    **protocol.LinkMess
	replyError     uint32 // SpiceLinkReply.error
	hangAfterReply bool
	skipAuthRead   bool
}

func runMockLinkServer(t *testing.T, conn net.Conn, opts mockLinkOpts) error {
	t.Helper()
	defer conn.Close()

	// Read link header + body
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

	pub := opts.pubDER
	if opts.badPubKeyLen > 0 {
		pub = make([]byte, opts.badPubKeyLen)
		copy(pub, opts.pubDER)
	}
	// LinkReply.EncodeBody requires exact 162; for bad length we write manually.
	if len(pub) == protocol.SpiceLinkPubKeyBytes {
		reply := &protocol.LinkReply{
			Error:      opts.replyError,
			PubKey:     pub,
			CommonCaps: protocol.Phase1CommonCaps(),
		}
		pkt, err := reply.EncodePacket()
		if err != nil {
			return err
		}
		if _, err := conn.Write(pkt); err != nil {
			return err
		}
	} else {
		// Manual short/long pubkey packet for negative tests.
		// Fixed fields: error + pub_key[N] + counts + offset — not valid SPICE,
		// but client ParseLinkPublicKey runs after DecodeLinkReply which expects 162.
		// So we still send a 162-byte field of garbage for "wrong key" vs wrong length:
		// Wrong length is enforced only when the DER length != 162 after decode,
		// and DecodeLinkReply always copies 162 bytes. To test ParseLinkPublicKey
		// length, we send valid framing with 162 bytes of non-SPKI data, OR we
		// need session to check before decode... ParseLinkPublicKey checks der len.
		// DecodeLinkReply always yields 162-byte PubKey. So wrong-length test must
		// call ParseLinkPublicKey directly or we inject via a custom reply that
		// the client reads as 162 zeros that fail parse as PKIX — that's "bad key"
		// not "wrong length". For wrong length unit test, call ParseLinkPublicKey
		// and/or test linkMain via a conn that returns a handcrafted body...
		// Simplest: use LinkReply with 162-byte invalid DER for parse fail, and
		// separate TestParseLinkPublicKeyWrongLength in session or security.
		return fmt.Errorf("mock: use 162-byte pub or write custom")
	}

	if opts.hangAfterReply {
		time.Sleep(50 * time.Millisecond)
		return nil
	}
	if opts.skipAuthRead {
		return nil
	}

	// Read AuthSpice: mechanism u32 + 128 ciphertext
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
			// strip trailing NUL
			if len(plain) > 0 && plain[len(plain)-1] == 0 {
				plain = plain[:len(plain)-1]
			}
			if !bytes.Equal(plain, opts.expectedPass) {
				result = protocol.LinkErrPermissionDenied
			}
		}
	}

	if err := protocol.WriteLinkResult(conn, result); err != nil {
		return err
	}
	return nil
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

	// connection_id=0 on main mess
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
	// Valid framing with 162-byte non-SPKI payload → ParseLinkPublicKey fails.
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
		// Garbage 162-byte "pubkey"
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

	s, err := session.New(session.Config{
		Password: []byte("x"),
	})
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
	// Not Ticket — key parse failure
	if uxErr.Class == ux.ClassTicket {
		t.Fatalf("wrong class Ticket for pubkey parse: %v", err)
	}
	_ = <-errCh
}

func TestParseLinkPublicKey_WrongLength(t *testing.T) {
	// Required by PR 06: wrong pubkey length → error (security helper).
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
	// Explicit unit assertion: main mess always connection_id=0.
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
	// Mutate caller slice; session must have its own copy.
	orig[0] = 'X'
	// We cannot read session password; verify Close does not panic and Linked false.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.Linked() {
		t.Fatal("linked after close")
	}
	// Double close OK
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Caller still has their (mutated) bytes; Wipe only session copy.
	if orig[0] != 'X' {
		t.Fatal("caller slice should be untouched by Close")
	}
}

func TestDialMain_MapsProxyError(t *testing.T) {
	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Dialer:   errDialer{err: fmt.Errorf("connector: CONNECT refused: 403 Forbidden")},
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
	base := fmt.Errorf("%w: subject does not match host-subject", connector.ErrTLSVerify)
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

func TestDialMain_CleartextDenied_Config(t *testing.T) {
	// Real dialer + cleartext denied before network.
	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "127.0.0.1", Port: 9}, // no AllowCleartext, no TLS
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
