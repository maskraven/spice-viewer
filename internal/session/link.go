// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/maskraven/virt-viewer/internal/protocol"
	"github.com/maskraven/virt-viewer/internal/security"
	"github.com/maskraven/virt-viewer/internal/ux"
)

// defaultLinkTimeout is applied when the caller's context has no deadline,
// matching DefaultDialer's 30s dial timeout so a silent peer cannot hang forever.
const defaultLinkTimeout = 30 * time.Second

// linkMainChannel runs the full main-channel link sequence on conn:
//
//  1. Write SpiceLinkMess (connection_id=0, MAIN, channel_id=0, Phase1 caps)
//  2. Read SpiceLinkReply; parse 162-byte DER pubkey
//  3. EncryptSpiceTicket; send AuthSpice mechanism=1
//  4. Read link result; non-OK → ClassTicket
//
// After success, subsequent I/O on conn uses mini-header framing (not consumed here).
//
// ctx cancel and deadline are honored for the entire handshake (bindConnToContext).
// When ctx has no deadline, a defaultLinkTimeout is applied for this phase only.
func linkMainChannel(ctx context.Context, conn net.Conn, password []byte) error {
	if err := ctx.Err(); err != nil {
		return mapTransportErr(err)
	}

	// Default handshake timeout when caller passed Background / no deadline.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultLinkTimeout)
		defer cancel()
	}

	release, err := bindConnToContext(ctx, conn)
	if err != nil {
		return mapTransportErr(fmt.Errorf("session: bind link context: %w", err))
	}
	// Always clear deadline / cancel watcher so a linked main conn is long-lived.
	defer release()

	mess := protocol.NewMainLinkMess(nil)
	if mess.ConnectionID != 0 {
		return ux.New(ux.ClassInternal, ux.MsgInternal,
			fmt.Errorf("session: main link mess connection_id must be 0"))
	}
	if err := protocol.WriteLinkMess(conn, mess); err != nil {
		return mapLinkIOErr(ctx, fmt.Errorf("session: write link mess: %w", err))
	}

	reply, _, err := protocol.ReadLinkReply(conn)
	if err != nil {
		return mapLinkIOErr(ctx, fmt.Errorf("session: read link reply: %w", err))
	}
	if reply.Error != protocol.LinkErrOK {
		return mapLinkResult(reply.Error, fmt.Errorf("session: link reply error %d", reply.Error))
	}

	pub, err := security.ParseLinkPublicKey(reply.PubKey)
	if err != nil {
		// Wrong length / non-RSA: not a ticket expiry — surface as Internal with detail.
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: parse link public key: %w", err))
	}

	ct, err := security.EncryptSpiceTicket(pub, password)
	if err != nil {
		// Oversize password / encrypt failure: Config-class (not ambiguous ticket fail).
		return ux.New(ux.ClassConfig, ux.MsgConfigFieldTooLarge, fmt.Errorf("session: encrypt ticket: %w", err))
	}

	if err := protocol.WriteAuthSpice(conn, ct); err != nil {
		return mapLinkIOErr(ctx, fmt.Errorf("session: write auth spice: %w", err))
	}

	result, err := protocol.ReadLinkResult(conn)
	if err != nil {
		return mapLinkIOErr(ctx, fmt.Errorf("session: read link result: %w", err))
	}
	if result != protocol.LinkErrOK {
		return mapLinkResult(result, fmt.Errorf("session: link result %d", result))
	}

	// Link OK: mini-header framing applies for subsequent messages (PR 06 stops here).
	// Caller may later use protocol.ReadMessage / WriteMessage on the connection.
	return nil
}

// bindConnToContext applies ctx deadline to conn and interrupts blocking I/O when
// ctx is cancelled (same pattern as connector.bindConnToContext for CONNECT).
// release must be called exactly once: stops the cancel watcher and clears the
// deadline so the long-lived main channel is not bound to the link context.
func bindConnToContext(ctx context.Context, conn net.Conn) (release func(), err error) {
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("session: set deadline: %w", err)
		}
	}

	var mu sync.Mutex
	released := false
	done := make(chan struct{})
	var once sync.Once

	go func() {
		select {
		case <-ctx.Done():
			mu.Lock()
			if !released {
				// Unblock in-flight Read/Write only while link is still active.
				_ = conn.SetDeadline(time.Unix(1, 0))
			}
			mu.Unlock()
		case <-done:
		}
	}()

	release = func() {
		once.Do(func() {
			mu.Lock()
			released = true
			close(done)
			_ = conn.SetDeadline(time.Time{})
			mu.Unlock()
		})
	}
	return release, nil
}
