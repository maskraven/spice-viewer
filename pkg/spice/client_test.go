// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maskraven/spice-viewer/internal/connector"
	"github.com/maskraven/spice-viewer/internal/protocol"
	"github.com/maskraven/spice-viewer/internal/security"
)

// multiPipeDialer hands out pre-built client-side pipe ends for each DialSPICE.
type multiPipeDialer struct {
	mu    sync.Mutex
	conns []net.Conn
	idx   int
}

func (d *multiPipeDialer) DialSPICE(ctx context.Context, ep connector.Endpoint) (net.Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.idx >= len(d.conns) {
		return nil, fmt.Errorf("multiPipeDialer: exhausted (idx=%d)", d.idx)
	}
	c := d.conns[d.idx]
	d.idx++
	return c, nil
}

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

func genSpiceTicketKey(t *testing.T) (pubDER []byte, priv *rsa.PrivateKey) {
	t.Helper()
	for attempt := 0; attempt < 32; attempt++ {
		k, err := rsa.GenerateKey(rand.Reader, 1024)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		if len(der) == protocol.SpiceLinkPubKeyBytes {
			return der, k
		}
	}
	t.Fatalf("could not generate 1024-bit RSA SPKI of length %d", protocol.SpiceLinkPubKeyBytes)
	return nil, nil
}

func runMockLink(t *testing.T, conn net.Conn, pubDER []byte, priv *rsa.PrivateKey, password []byte) error {
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
		return err
	}
	if _, err := protocol.DecodeLinkMess(body); err != nil {
		return err
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
		return err
	}
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
		return fmt.Errorf("password mismatch")
	}
	return protocol.WriteLinkResult(conn, protocol.LinkErrOK)
}

func serveMainPostLink(t *testing.T, conn net.Conn, sessionID uint32, channels []protocol.ChannelID) error {
	t.Helper()
	init := protocol.MainInit{
		SessionID:           sessionID,
		DisplayChannelsHint: 1,
		SupportedMouseModes: protocol.MouseModeServer | protocol.MouseModeClient,
		CurrentMouseMode:    protocol.MouseModeServer,
	}
	if err := protocol.WriteMessage(conn, protocol.MsgMainInit, init.Encode()); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		msg, err := protocol.ReadMessage(conn)
		if err != nil {
			return err
		}
		if msg.Type == protocol.MsgcMainAttachChannels {
			break
		}
	}
	list := protocol.ChannelsList{Channels: channels}
	return protocol.WriteMessage(conn, protocol.MsgMainChannelsList, list.Encode())
}

// startMockSessionServers starts main+display+inputs(+cursor) mock link servers.
// Returns client-side conns for the dialer and a cleanup func.
func startMockSessionServers(t *testing.T, password []byte, withCursor bool) (clients []net.Conn, cleanup func()) {
	t.Helper()
	mainDER, mainPriv := loadTicketKey(t)
	dispDER, dispPriv := genSpiceTicketKey(t)
	inDER, inPriv := genSpiceTicketKey(t)

	n := 3
	if withCursor {
		n = 4
	}
	clients = make([]net.Conn, n)
	servers := make([]net.Conn, n)
	for i := 0; i < n; i++ {
		c, s := net.Pipe()
		clients[i], servers[i] = c, s
	}

	var curDER []byte
	var curPriv *rsa.PrivateKey
	if withCursor {
		curDER, curPriv = genSpiceTicketKey(t)
	}

	channels := []protocol.ChannelID{
		{Type: protocol.ChannelDisplay, ID: 0},
		{Type: protocol.ChannelInputs, ID: 0},
	}
	if withCursor {
		channels = append(channels, protocol.ChannelID{Type: protocol.ChannelCursor, ID: 0})
	}

	const sessionID uint32 = 0x11223344
	errCh := make(chan error, n+1)

	// Main
	go func() {
		if err := runMockLink(t, servers[0], mainDER, mainPriv, password); err != nil {
			errCh <- fmt.Errorf("main link: %w", err)
			return
		}
		if err := serveMainPostLink(t, servers[0], sessionID, channels); err != nil {
			errCh <- fmt.Errorf("main post: %w", err)
			return
		}
		// Keep main open for runMain (mouse mode request may arrive).
		// Drain until peer closes.
		for {
			_ = servers[0].SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			if _, err := protocol.ReadMessage(servers[0]); err != nil {
				errCh <- nil
				return
			}
		}
	}()

	// Children: any order; pick key by channel type from LinkMess.
	keyByType := map[uint8]struct {
		der  []byte
		priv *rsa.PrivateKey
	}{
		protocol.ChannelDisplay: {dispDER, dispPriv},
		protocol.ChannelInputs:  {inDER, inPriv},
	}
	if withCursor {
		keyByType[protocol.ChannelCursor] = struct {
			der  []byte
			priv *rsa.PrivateKey
		}{curDER, curPriv}
	}

	for i := 1; i < n; i++ {
		i := i
		go func() {
			errCh <- runChildByType(t, servers[i], keyByType, password)
		}()
	}

	cleanup = func() {
		for _, c := range clients {
			_ = c.Close()
		}
		for _, s := range servers {
			_ = s.Close()
		}
	}
	return clients, cleanup
}

func runChildByType(t *testing.T, conn net.Conn, keyByType map[uint8]struct {
	der  []byte
	priv *rsa.PrivateKey
}, password []byte) error {
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
	k, ok := keyByType[mess.ChannelType]
	if !ok {
		return fmt.Errorf("unexpected channel type %d", mess.ChannelType)
	}
	return runMockLinkWithKey(t, conn, k.der, k.priv, password, true /* already read mess */)
}

// runMockLinkWithKey writes reply+auth after mess already consumed.
func runMockLinkWithKey(t *testing.T, conn net.Conn, pubDER []byte, priv *rsa.PrivateKey, password []byte, messDone bool) error {
	t.Helper()
	if !messDone {
		return runMockLink(t, conn, pubDER, priv, password)
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
		return err
	}
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
		return fmt.Errorf("password mismatch")
	}
	return protocol.WriteLinkResult(conn, protocol.LinkErrOK)
}

func TestConnect_PasswordOwnershipAndEvents(t *testing.T) {
	password := []byte("ticket-secret-own")
	clients, cleanup := startMockSessionServers(t, password, true)
	defer cleanup()

	callerPW := append([]byte(nil), password...)
	cfg := ConnectConfig{
		Host:           "127.0.0.1",
		Port:           5900,
		AllowCleartext: true,
		Password:       callerPW,
		Title:          "test-vm",
		Drivers: Drivers{
			Display: NewNullDriver(),
			Cursor:  NewNullCursorDriver(),
		},
		dialer: &multiPipeDialer{conns: clients},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := Connect(ctx, cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Caller slice is not wiped by Connect (session holds its own copy).
	if !bytes.Equal(cfg.Password, password) {
		t.Fatalf("Connect wiped or mutated caller password: %q", cfg.Password)
	}
	// Mutating caller after Connect must be safe (session already linked).
	cfg.Password[0] = 'X'
	if c.Title() != "test-vm" {
		t.Errorf("Title: %q", c.Title())
	}
	if c.Inputs() == nil {
		t.Fatal("Inputs() nil")
	}

	// First event is Connected (F5).
	select {
	case ev := <-c.Events():
		if ev.Type != EventConnected {
			t.Fatalf("first event: %v err=%v", ev.Type, ev.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for EventConnected")
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Clean Close → Disconnected with nil Err; events channel closed after.
	var sawDisc bool
	deadline := time.After(2 * time.Second)
	for !sawDisc {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				if !sawDisc {
					t.Fatal("events closed before EventDisconnected")
				}
				break
			}
			if ev.Type == EventDisconnected {
				sawDisc = true
				if ev.Err != nil {
					t.Fatalf("clean Close Disconnected err=%v", ev.Err)
				}
			}
		case <-deadline:
			t.Fatal("timeout waiting for EventDisconnected")
		}
		if sawDisc {
			// Drain until channel closed.
			for {
				if _, ok := <-c.Events(); !ok {
					break
				}
			}
			break
		}
	}

	// Double Close is safe.
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestConnect_FatalPreservedAcrossClose(t *testing.T) {
	// F4: recordDisconnectErr + Close must not erase a fatal with nil.
	password := []byte("ticket-fatal")
	clients, cleanup := startMockSessionServers(t, password, false) // no cursor
	defer cleanup()

	cfg := ConnectConfig{
		Host:           "127.0.0.1",
		Port:           5900,
		AllowCleartext: true,
		Password:       append([]byte(nil), password...),
		Drivers:        Drivers{Display: NewNullDriver()},
		dialer:         &multiPipeDialer{conns: clients},
	}
	c, err := Connect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	// Drain Connected.
	select {
	case ev := <-c.Events():
		if ev.Type != EventConnected {
			t.Fatalf("want Connected, got %v", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no Connected")
	}

	// Inject fatal as if a channel failed, then Close concurrently.
	fatal := fmt.Errorf("spice: display channel closed: EOF")
	c.recordDisconnectErr(fatal)
	select {
	case c.fatalCh <- fatal:
	default:
	}
	c.lifeCancel()

	// Close must not overwrite discErr with clean nil.
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Find Disconnected; must carry the fatal (classified possibly as Transport).
	var disc Event
	found := false
	for {
		ev, ok := <-c.Events()
		if !ok {
			break
		}
		if ev.Type == EventDisconnected {
			disc = ev
			found = true
		}
	}
	if !found {
		t.Fatal("missing EventDisconnected")
	}
	if disc.Err == nil {
		t.Fatal("Close erased fatal disconnect error (F4 regression)")
	}
	if !strings.Contains(disc.Err.Error(), "display channel") &&
		!strings.Contains(disc.Err.Error(), "Connection lost") {
		t.Fatalf("unexpected disc err: %v", disc.Err)
	}
}

func TestRecordDisconnectErr_FirstNonNilWins(t *testing.T) {
	c := &Client{}
	c.recordDisconnectErr(nil) // no-op
	if c.disconnectErr() != nil {
		t.Fatal("nil should not set discErr")
	}
	e1 := fmt.Errorf("first")
	e2 := fmt.Errorf("second")
	c.recordDisconnectErr(e1)
	c.recordDisconnectErr(e2)
	c.recordDisconnectErr(nil)
	if c.disconnectErr() != e1 {
		t.Fatalf("got %v want first", c.disconnectErr())
	}
}

func TestConnect_CallerPasswordIntactOnDialFailure(t *testing.T) {
	pw := []byte("keep-me")
	cfg := ConnectConfig{
		Host:           "127.0.0.1",
		Port:           1,
		AllowCleartext: true,
		Password:       pw,
		dialer:         errDialer{err: fmt.Errorf("dial refused")},
	}
	_, err := Connect(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected dial error")
	}
	if !bytes.Equal(cfg.Password, []byte("keep-me")) {
		t.Fatalf("caller password mutated: %q", cfg.Password)
	}
}

type errDialer struct{ err error }

func (d errDialer) DialSPICE(ctx context.Context, ep connector.Endpoint) (net.Conn, error) {
	return nil, d.err
}

func TestConnectConfig_EndpointCleartextRequiresFlag(t *testing.T) {
	cfg := ConnectConfig{
		Host: "127.0.0.1",
		Port: 5900,
		// AllowCleartext false
	}
	_, err := cfg.endpoint()
	if err == nil {
		t.Fatal("expected error without AllowCleartext")
	}
}

func TestConnectConfig_EndpointTLS(t *testing.T) {
	// Valid enough PEM structure may still fail Append if DER is junk —
	// use a real self-signed from testdata/certs if available.
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	caPath := filepath.Join(root, "testdata", "certs", "ca.pem")
	ca, err := os.ReadFile(caPath)
	if err != nil {
		t.Skip("no testdata/certs/ca.pem")
	}
	cfg := ConnectConfig{
		Host:        "pvespiceproxy:token",
		TLSPort:     61000,
		HostSubject: "CN=pve.example.com",
		CACertPEM:   ca,
		Password:    []byte("x"),
	}
	ep, err := cfg.endpoint()
	if err != nil {
		t.Fatal(err)
	}
	if ep.Port != 61000 {
		t.Errorf("port %d", ep.Port)
	}
	if ep.TLS == nil || ep.TLS.HostSubject != "CN=pve.example.com" {
		t.Errorf("TLS params: %+v", ep.TLS)
	}
	if ep.TLS.RootCAs == nil {
		t.Error("RootCAs nil")
	}
}
