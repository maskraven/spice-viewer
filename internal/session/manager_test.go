// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package session_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maskraven/virt-viewer/internal/codec/h264"
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

// childLinkOpts configures a mock child link server.
type childLinkOpts struct {
	pubDER       []byte
	priv         *rsa.PrivateKey
	password     []byte
	captureMess  **protocol.LinkMess
	encryptCount *atomic.Int32
	// ciphertexts, if non-nil, appends a copy of each accepted auth ciphertext.
	ciphertexts *struct {
		mu  sync.Mutex
		all [][]byte
	}
	// wrongPriv, if set, is used to assert that ciphertext does NOT decrypt with
	// a different channel's key (proves per-pubkey encrypt).
	wrongPriv *rsa.PrivateKey
}

// runChildLinkServer completes one child link handshake and records LinkMess.
// After link OK the server side stays open until closed (no further messages).
func runChildLinkServer(t *testing.T, conn net.Conn, opts childLinkOpts) error {
	t.Helper()
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
	if opts.captureMess != nil {
		*opts.captureMess = mess
	}

	reply := &protocol.LinkReply{
		Error:      protocol.LinkErrOK,
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
		return fmt.Errorf("child read auth: %w", err)
	}
	auth, err := protocol.DecodeAuthSpice(authBuf)
	if err != nil {
		return err
	}
	if opts.wrongPriv != nil {
		if _, err := rsa.DecryptOAEP(sha1.New(), nil, opts.wrongPriv, auth.Ciphertext, nil); err == nil {
			return fmt.Errorf("ciphertext decrypted with wrong channel key (reuse?)")
		}
	}
	plain, err := rsa.DecryptOAEP(sha1.New(), nil, opts.priv, auth.Ciphertext, nil)
	if err != nil {
		_ = protocol.WriteLinkResult(conn, protocol.LinkErrPermissionDenied)
		return fmt.Errorf("decrypt: %w", err)
	}
	if len(plain) > 0 && plain[len(plain)-1] == 0 {
		plain = plain[:len(plain)-1]
	}
	if !bytes.Equal(plain, opts.password) {
		_ = protocol.WriteLinkResult(conn, protocol.LinkErrPermissionDenied)
		return fmt.Errorf("password mismatch")
	}
	if opts.encryptCount != nil {
		opts.encryptCount.Add(1)
	}
	if opts.ciphertexts != nil {
		ct := append([]byte(nil), auth.Ciphertext...)
		opts.ciphertexts.mu.Lock()
		opts.ciphertexts.all = append(opts.ciphertexts.all, ct)
		opts.ciphertexts.mu.Unlock()
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

// defaultChannelList is display+inputs+cursor (3 children). Tests that need
// playback list it explicitly and allocate an extra pipe.
func defaultChannelList() []protocol.ChannelID {
	return []protocol.ChannelID{
		{Type: protocol.ChannelDisplay, ID: 0},
		{Type: protocol.ChannelInputs, ID: 0},
		{Type: protocol.ChannelCursor, ID: 0},
	}
}

func channelListWithPlayback() []protocol.ChannelID {
	return []protocol.ChannelID{
		{Type: protocol.ChannelDisplay, ID: 0},
		{Type: protocol.ChannelInputs, ID: 0},
		{Type: protocol.ChannelCursor, ID: 0},
		{Type: protocol.ChannelPlayback, ID: 0},
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
	// Distinct RSA keys per channel: each mock server only decrypts its own blob.
	mainDER, mainPriv := loadTicketKey(t)
	dispDER, dispPriv := genSpiceTicketKey(t)
	inDER, inPriv := genSpiceTicketKey(t)
	curDER, curPriv := genSpiceTicketKey(t)
	pbDER, pbPriv := genSpiceTicketKey(t)

	// Map for child servers: dial order 1..4 gets keys assigned when LinkMess type known.
	// Each server uses a key selected by channel type from this table.
	keyByType := map[uint8]struct {
		der  []byte
		priv *rsa.PrivateKey
	}{
		protocol.ChannelDisplay:  {dispDER, dispPriv},
		protocol.ChannelInputs:   {inDER, inPriv},
		protocol.ChannelCursor:   {curDER, curPriv},
		protocol.ChannelPlayback: {pbDER, pbPriv},
	}

	password := []byte("ticket-secret")
	const sessionID uint32 = 0x0a0b0c0d

	// Pipes: [0]=main, [1..4]=children (any order)
	const n = 5
	clients := make([]net.Conn, n)
	servers := make([]net.Conn, n)
	for i := 0; i < n; i++ {
		c, s := net.Pipe()
		clients[i], servers[i] = c, s
	}
	defer func() {
		for i := 0; i < n; i++ {
			_ = clients[i].Close()
			_ = servers[i].Close()
		}
	}()

	dialer := &multiPipeDialer{conns: clients}

	var (
		mainMess *protocol.LinkMess
		encrypts atomic.Int32
		cts      = &struct {
			mu  sync.Mutex
			all [][]byte
		}{}
	)

	childByType := struct {
		mu   sync.Mutex
		mess map[uint8]*protocol.LinkMess
	}{mess: make(map[uint8]*protocol.LinkMess)}

	errCh := make(chan error, 8)

	// Main: link + MAIN_INIT + list (vector key)
	go func() {
		if err := runMockLinkServerKeepOpen(t, servers[0], mockLinkOpts{
			pubDER:        mainDER,
			priv:          mainPriv,
			expectedPass:  password,
			checkPassword: true,
			captureMess:   &mainMess,
		}); err != nil {
			errCh <- fmt.Errorf("main link: %w", err)
			return
		}
		encrypts.Add(1)
		if err := serveMainPostLink(t, servers[0], sessionID, channelListWithPlayback()); err != nil {
			errCh <- fmt.Errorf("main post-link: %w", err)
			return
		}
		errCh <- nil
	}()

	// Child servers: decrypt only with that channel type's key; wrongPriv = main key.
	for i := 1; i < n; i++ {
		i := i
		go func() {
			errCh <- runChildLinkServerByType(t, servers[i], keyByType, mainPriv, password, &childByType, &encrypts, cts)
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.DialMain(ctx); err != nil {
		t.Fatalf("DialMain: %v", err)
	}
	if err := s.OpenChannels(ctx); err != nil {
		t.Fatalf("OpenChannels: %v", err)
	}
	if !s.ChannelsReady() {
		t.Fatal("expected ChannelsReady")
	}

	if s.ConnectionID() != sessionID {
		t.Fatalf("ConnectionID=%#x want %#x", s.ConnectionID(), sessionID)
	}
	if s.DisplayConn() == nil || s.InputsConn() == nil || s.CursorConn() == nil || s.PlaybackConn() == nil {
		t.Fatalf("missing child conns display=%v inputs=%v cursor=%v playback=%v",
			s.DisplayConn() != nil, s.InputsConn() != nil, s.CursorConn() != nil, s.PlaybackConn() != nil)
	}
	if s.CursorError() != nil {
		t.Fatalf("unexpected cursor error: %v", s.CursorError())
	}
	if s.PlaybackError() != nil {
		t.Fatalf("unexpected playback error: %v", s.PlaybackError())
	}

	if mainMess == nil || mainMess.ConnectionID != 0 {
		t.Fatalf("main mess=%+v", mainMess)
	}

	if dialer.dials.Load() != n {
		t.Fatalf("dials=%d want %d (main+display+inputs+cursor+playback)", dialer.dials.Load(), n)
	}
	if encrypts.Load() != n {
		t.Fatalf("encrypt/auth count=%d want %d", encrypts.Load(), n)
	}

	// All child ciphertexts must be distinct (fresh OAEP encrypt per channel).
	cts.mu.Lock()
	if len(cts.all) != n-1 {
		t.Fatalf("child ciphertexts=%d want %d", len(cts.all), n-1)
	}
	for i := 0; i < len(cts.all); i++ {
		for j := i + 1; j < len(cts.all); j++ {
			if bytes.Equal(cts.all[i], cts.all[j]) {
				t.Fatal("duplicate ciphertext across channels (must re-encrypt per pubkey)")
			}
		}
	}
	cts.mu.Unlock()

	childByType.mu.Lock()
	defer childByType.mu.Unlock()
	for _, typ := range []uint8{
		protocol.ChannelDisplay, protocol.ChannelInputs,
		protocol.ChannelCursor, protocol.ChannelPlayback,
	} {
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
	// Playback link should advertise VOLUME cap only (no OPUS).
	pbMess := childByType.mess[protocol.ChannelPlayback]
	if !protocol.HasCap(pbMess.ChannelCaps, protocol.PlaybackCapVolume) {
		t.Fatalf("playback missing VOLUME cap: %v", pbMess.ChannelCaps)
	}
	if protocol.HasCap(pbMess.ChannelCaps, protocol.PlaybackCapOpus) {
		t.Fatal("playback must not advertise OPUS (RAW preferred)")
	}

	// Display: MULTI_CODEC + MJPEG always; H.264 only when h264.Available().
	dispMess := childByType.mess[protocol.ChannelDisplay]
	wantDisp := protocol.DisplayChannelCaps(h264.Available())
	for _, bit := range []uint{
		protocol.DisplayCapSizedStream,
		protocol.DisplayCapMultiCodec,
		protocol.DisplayCapCodecMJPEG,
	} {
		if !protocol.HasCap(dispMess.ChannelCaps, bit) {
			t.Fatalf("display missing cap bit %d: %v", bit, dispMess.ChannelCaps)
		}
	}
	gotH264 := protocol.HasCap(dispMess.ChannelCaps, protocol.DisplayCapCodecH264)
	wantH264 := protocol.HasCap(wantDisp, protocol.DisplayCapCodecH264)
	if gotH264 != wantH264 {
		t.Fatalf("display CODEC_H264=%v want %v (h264.Available=%v caps=%v)",
			gotH264, wantH264, h264.Available(), dispMess.ChannelCaps)
	}

	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("server: %v", err)
		}
	}
}

// genSpiceTicketKey generates a 1024-bit RSA key whose SPKI DER is exactly
// SpiceLinkPubKeyBytes (162), as required by the Phase-1 link parser.
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

// runChildLinkServerByType reads LinkMess, picks pubkey by channel type, and validates auth.
func runChildLinkServerByType(
	t *testing.T,
	conn net.Conn,
	keyByType map[uint8]struct {
		der  []byte
		priv *rsa.PrivateKey
	},
	wrongPriv *rsa.PrivateKey,
	password []byte,
	childByType *struct {
		mu   sync.Mutex
		mess map[uint8]*protocol.LinkMess
	},
	encryptCount *atomic.Int32,
	cts *struct {
		mu  sync.Mutex
		all [][]byte
	},
) error {
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
	childByType.mu.Lock()
	childByType.mess[mess.ChannelType] = mess
	childByType.mu.Unlock()

	k, ok := keyByType[mess.ChannelType]
	if !ok {
		return fmt.Errorf("unexpected channel type %d", mess.ChannelType)
	}
	return finishChildLinkAfterMess(t, conn, k.der, k.priv, wrongPriv, password, encryptCount, cts)
}

// finishChildLinkAfterMess writes LinkReply and validates AuthSpice (LinkMess already read).
func finishChildLinkAfterMess(
	t *testing.T,
	conn net.Conn,
	pubDER []byte,
	priv *rsa.PrivateKey,
	wrongPriv *rsa.PrivateKey,
	password []byte,
	encryptCount *atomic.Int32,
	cts *struct {
		mu  sync.Mutex
		all [][]byte
	},
) error {
	t.Helper()
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
	auth, err := protocol.DecodeAuthSpice(authBuf)
	if err != nil {
		return err
	}
	if wrongPriv != nil {
		if _, err := rsa.DecryptOAEP(sha1.New(), nil, wrongPriv, auth.Ciphertext, nil); err == nil {
			return fmt.Errorf("ciphertext decrypted with wrong channel key")
		}
	}
	plain, err := rsa.DecryptOAEP(sha1.New(), nil, priv, auth.Ciphertext, nil)
	if err != nil {
		_ = protocol.WriteLinkResult(conn, protocol.LinkErrPermissionDenied)
		return fmt.Errorf("decrypt with channel key: %w", err)
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
	if cts != nil {
		ct := append([]byte(nil), auth.Ciphertext...)
		cts.mu.Lock()
		cts.all = append(cts.all, ct)
		cts.mu.Unlock()
	}
	return protocol.WriteLinkResult(conn, protocol.LinkErrOK)
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
	if s.ConnectionID() != 0 {
		t.Fatalf("ConnectionID should stay 0 after failed open, got %#x", s.ConnectionID())
	}
	if s.ChannelsReady() {
		t.Fatal("ChannelsReady after failed open")
	}
	// One-shot: retry must fail cleanly (not "already opened" success path).
	err2 := s.OpenChannels(ctx)
	if err2 == nil {
		t.Fatal("expected second OpenChannels to fail")
	}
	if !bytes.Contains([]byte(err2.Error()), []byte("previously failed")) {
		t.Fatalf("second open error = %v, want previously failed", err2)
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

// closeTrackConn records Close() order under a shared tracker once named.
type closeTrackConn struct {
	net.Conn
	name   atomic.Value // string
	tr     *closeOrderTracker
	closed atomic.Bool
}

type closeOrderTracker struct {
	mu  sync.Mutex
	seq []string
}

func (c *closeTrackConn) Close() error {
	if c.closed.Swap(true) {
		return net.ErrClosed
	}
	if n, ok := c.name.Load().(string); ok && n != "" {
		c.tr.mu.Lock()
		c.tr.seq = append(c.tr.seq, n)
		c.tr.mu.Unlock()
	}
	return c.Conn.Close()
}

func (c *closeTrackConn) SetName(n string) { c.name.Store(n) }

func TestClose_OrderAsserted(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("p")
	const sessionID uint32 = 11

	tr := &closeOrderTracker{}
	clients := make([]net.Conn, 4)
	servers := make([]net.Conn, 4)
	tracked := make([]*closeTrackConn, 4)
	for i := 0; i < 4; i++ {
		c, s := net.Pipe()
		tc := &closeTrackConn{Conn: c, tr: tr}
		tracked[i] = tc
		clients[i], servers[i] = tc, s
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
			errCh <- runChildLinkServer(t, servers[i], childLinkOpts{
				pubDER: pubDER, priv: priv, password: password,
			})
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

	// Label by identity after open (dial order ≠ channel type order for children).
	if tc, ok := s.MainConn().(*closeTrackConn); ok {
		tc.SetName("main")
	} else {
		t.Fatal("main not tracked")
	}
	if tc, ok := s.DisplayConn().(*closeTrackConn); ok {
		tc.SetName("display")
	} else {
		t.Fatal("display not tracked")
	}
	if tc, ok := s.InputsConn().(*closeTrackConn); ok {
		tc.SetName("inputs")
	} else {
		t.Fatal("inputs not tracked")
	}
	if tc, ok := s.CursorConn().(*closeTrackConn); ok {
		tc.SetName("cursor")
	} else {
		t.Fatal("cursor not tracked")
	}

	if err := s.Close(); err != nil {
		t.Logf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	want := []string{"inputs", "display", "cursor", "main"}
	tr.mu.Lock()
	got := append([]string(nil), tr.seq...)
	tr.mu.Unlock()
	if len(got) != len(want) {
		t.Fatalf("close order %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("close order %v want %v", got, want)
		}
	}

	if s.Linked() {
		t.Fatal("linked after close")
	}
	if s.DisplayConn() != nil || s.InputsConn() != nil || s.MainConn() != nil {
		t.Fatal("conns should be nil after close")
	}

	for i := 0; i < 4; i++ {
		_ = servers[i].Close()
	}
	for i := 0; i < 4; i++ {
		<-errCh
	}
	_ = tracked
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

func TestOpenChannels_MissingInputsInList_Fatal(t *testing.T) {
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
		errCh <- serveMainPostLink(t, s, 1, []protocol.ChannelID{
			{Type: protocol.ChannelDisplay, ID: 0},
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
		t.Fatal("expected missing inputs error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("INPUTS")) {
		t.Fatalf("error should mention INPUTS: %v", err)
	}
	if sess.ConnectionID() != 0 || sess.ChannelsReady() {
		t.Fatal("failed open must not publish connectionID/ready")
	}
	// One-shot failed state.
	if err2 := sess.OpenChannels(context.Background()); err2 == nil {
		t.Fatal("expected second open fail")
	}
	<-errCh
}

func TestOpenChannels_NoCursorInList_OK(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("p")
	const sessionID uint32 = 42

	// main + display + inputs only
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

	errCh := make(chan error, 4)
	go func() {
		if err := runMockLinkServerKeepOpen(t, servers[0], mockLinkOpts{
			pubDER: pubDER, priv: priv, expectedPass: password, checkPassword: true,
		}); err != nil {
			errCh <- err
			return
		}
		errCh <- serveMainPostLink(t, servers[0], sessionID, []protocol.ChannelID{
			{Type: protocol.ChannelDisplay, ID: 0},
			{Type: protocol.ChannelInputs, ID: 0},
			// No cursor or playback in list: both optional.
		})
	}()
	for i := 1; i < 3; i++ {
		i := i
		go func() {
			errCh <- runChildLinkServer(t, servers[i], childLinkOpts{
				pubDER: pubDER, priv: priv, password: password,
			})
		}()
	}

	sess, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Password: password,
		Dialer:   &multiPipeDialer{conns: clients},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	ctx := context.Background()
	if err := sess.DialMain(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sess.OpenChannels(ctx); err != nil {
		t.Fatal(err)
	}
	if !sess.ChannelsReady() {
		t.Fatal("ready")
	}
	if sess.DisplayConn() == nil || sess.InputsConn() == nil {
		t.Fatal("display/inputs required")
	}
	if sess.CursorConn() != nil {
		t.Fatal("cursor should be absent when not in list")
	}
	if sess.CursorError() != nil {
		t.Fatalf("CursorError should be nil when cursor not listed: %v", sess.CursorError())
	}
	if sess.PlaybackConn() != nil {
		t.Fatal("playback should be absent when not in list")
	}
	if sess.PlaybackError() != nil {
		t.Fatalf("PlaybackError should be nil when playback not listed: %v", sess.PlaybackError())
	}
	for i := 0; i < 3; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("server: %v", err)
		}
	}
}

func TestOpenChannels_ConcurrentRejected(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("p")

	// Hang main after link so first OpenChannels blocks in readMainInitAndChannels.
	c, s := net.Pipe()
	defer c.Close()
	defer s.Close()

	go func() {
		_ = runMockLinkServerKeepOpen(t, s, mockLinkOpts{
			pubDER: pubDER, priv: priv, expectedPass: password, checkPassword: true,
		})
		// Never send MAIN_INIT — first OpenChannels blocks on ReadMessage.
		buf := make([]byte, 1)
		_, _ = s.Read(buf)
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

	started := make(chan struct{})
	err1 := make(chan error, 1)
	go func() {
		close(started)
		err1 <- sess.OpenChannels(context.Background())
	}()
	<-started
	// Give first call time to take openInProgress.
	time.Sleep(30 * time.Millisecond)

	err2 := sess.OpenChannels(context.Background())
	if err2 == nil {
		t.Fatal("expected concurrent OpenChannels rejection")
	}
	if !bytes.Contains([]byte(err2.Error()), []byte("in progress")) {
		t.Fatalf("want in progress, got %v", err2)
	}

	// Unblock first OpenChannels via Close (cancels lifeCtx).
	_ = sess.Close()
	select {
	case <-err1:
	case <-time.After(3 * time.Second):
		t.Fatal("first OpenChannels did not return after Close")
	}
}

func channelListWithPhase3BestEffort() []protocol.ChannelID {
	return []protocol.ChannelID{
		{Type: protocol.ChannelDisplay, ID: 0},
		{Type: protocol.ChannelInputs, ID: 0},
		{Type: protocol.ChannelRecord, ID: 0},
		{Type: protocol.ChannelUSBRedir, ID: 0},
		{Type: protocol.ChannelUSBRedir, ID: 1},
		{Type: protocol.ChannelWebDAV, ID: 0},
	}
}

// TestOpenChannels_Phase3BestEffort_OpenNotFatal opens record + multi-usb + webdav
// successfully as best-effort; session remains ready.
func TestOpenChannels_Phase3BestEffort_OpenNotFatal(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("p")
	const sessionID uint32 = 77

	// main + display + inputs + record + usb0 + usb1 + webdav = 7
	n := 7
	clients := make([]net.Conn, n)
	servers := make([]net.Conn, n)
	for i := 0; i < n; i++ {
		c, s := net.Pipe()
		clients[i], servers[i] = c, s
	}
	defer func() {
		for i := 0; i < n; i++ {
			_ = clients[i].Close()
			_ = servers[i].Close()
		}
	}()

	errCh := make(chan error, n+1)
	go func() {
		if err := runMockLinkServerKeepOpen(t, servers[0], mockLinkOpts{
			pubDER: pubDER, priv: priv, expectedPass: password, checkPassword: true,
		}); err != nil {
			errCh <- err
			return
		}
		errCh <- serveMainPostLink(t, servers[0], sessionID, channelListWithPhase3BestEffort())
	}()
	for i := 1; i < n; i++ {
		i := i
		go func() {
			errCh <- runChildLinkServer(t, servers[i], childLinkOpts{
				pubDER: pubDER, priv: priv, password: password,
			})
		}()
	}

	sess, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Password: password,
		Dialer:   &multiPipeDialer{conns: clients},
		ShareDir: "/tmp/share-scaffold",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	ctx := context.Background()
	if err := sess.DialMain(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sess.OpenChannels(ctx); err != nil {
		t.Fatalf("OpenChannels must not fail when phase3 best-effort channels open: %v", err)
	}
	if !sess.ChannelsReady() {
		t.Fatal("ready")
	}
	if sess.DisplayConn() == nil || sess.InputsConn() == nil {
		t.Fatal("required channels")
	}
	if sess.RecordConn() == nil {
		t.Fatal("record should open")
	}
	if sess.RecordError() != nil {
		t.Fatalf("record err: %v", sess.RecordError())
	}
	usb := sess.USBConns()
	if len(usb) != 2 {
		t.Fatalf("usb conns=%d want 2", len(usb))
	}
	ids := map[uint8]bool{}
	for _, u := range usb {
		ids[u.ID] = true
		if u.Conn == nil {
			t.Fatal("nil usb conn")
		}
	}
	if !ids[0] || !ids[1] {
		t.Fatalf("usb ids %v", ids)
	}
	if sess.WebDAVConn() == nil {
		t.Fatal("webdav should open")
	}
	if sess.ShareDir() != "/tmp/share-scaffold" {
		t.Fatalf("shareDir=%q", sess.ShareDir())
	}
	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("server: %v", err)
		}
	}
}

// TestOpenChannels_Phase3RecordFail_NotFatal: record link denied; session still ready.
func TestOpenChannels_Phase3RecordFail_NotFatal(t *testing.T) {
	pubDER, priv := loadTicketKey(t)
	password := []byte("p")
	const sessionID uint32 = 88

	// main + display + inputs + record
	n := 4
	clients := make([]net.Conn, n)
	servers := make([]net.Conn, n)
	for i := 0; i < n; i++ {
		c, s := net.Pipe()
		clients[i], servers[i] = c, s
	}
	defer func() {
		for i := 0; i < n; i++ {
			_ = clients[i].Close()
			_ = servers[i].Close()
		}
	}()

	errCh := make(chan error, n+1)
	list := []protocol.ChannelID{
		{Type: protocol.ChannelDisplay, ID: 0},
		{Type: protocol.ChannelInputs, ID: 0},
		{Type: protocol.ChannelRecord, ID: 0},
	}
	go func() {
		if err := runMockLinkServerKeepOpen(t, servers[0], mockLinkOpts{
			pubDER: pubDER, priv: priv, expectedPass: password, checkPassword: true,
		}); err != nil {
			errCh <- err
			return
		}
		errCh <- serveMainPostLink(t, servers[0], sessionID, list)
	}()
	// Children: deny RECORD by type (dial order is nondeterministic).
	for i := 1; i < n; i++ {
		i := i
		go func() {
			errCh <- runChildLinkServerMaybeFailRecord(t, servers[i], pubDER, priv, password)
		}()
	}

	sess, err := session.New(session.Config{
		Endpoint: connector.Endpoint{Host: "h", Port: 1, AllowCleartext: true},
		Password: password,
		Dialer:   &multiPipeDialer{conns: clients},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.DialMain(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sess.OpenChannels(ctx); err != nil {
		t.Fatalf("record open failure must not be session-fatal: %v", err)
	}
	if !sess.ChannelsReady() {
		t.Fatal("ready")
	}
	if sess.DisplayConn() == nil || sess.InputsConn() == nil {
		t.Fatal("required channels")
	}
	if sess.RecordConn() != nil {
		t.Fatal("record conn should be nil after open failure")
	}
	if sess.RecordError() == nil {
		t.Fatal("expected record open error recorded")
	}
	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Logf("server %d: %v", i, err)
		}
	}
}

// runChildLinkServerMaybeFailRecord completes link OK for non-record; for record writes PermissionDenied.
func runChildLinkServerMaybeFailRecord(t *testing.T, conn net.Conn, pubDER []byte, priv *rsa.PrivateKey, password []byte) error {
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
	if mess.ChannelType == protocol.ChannelRecord {
		return protocol.WriteLinkResult(conn, protocol.LinkErrPermissionDenied)
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
		return fmt.Errorf("bad password")
	}
	return protocol.WriteLinkResult(conn, protocol.LinkErrOK)
}
