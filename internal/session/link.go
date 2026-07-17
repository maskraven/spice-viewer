// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/maskraven/spice-viewer/internal/protocol"
	"github.com/maskraven/spice-viewer/internal/security"
	"github.com/maskraven/spice-viewer/internal/ux"
)

// defaultLinkTimeout is applied when the caller's context has no deadline,
// matching DefaultDialer's 30s dial timeout so a silent peer cannot hang forever.
const defaultLinkTimeout = 30 * time.Second

// linkParams describes a SPICE channel link handshake.
type linkParams struct {
	ConnectionID uint32
	ChannelType  uint8
	ChannelID    uint8
	ChannelCaps  []uint32
}

// linkChannel runs the full SPICE link sequence on conn for any channel type:
//
//  1. Write SpiceLinkMess (connection_id, type, id, Phase1 caps)
//  2. Read SpiceLinkReply; parse 162-byte DER pubkey
//  3. EncryptSpiceTicket (fresh ciphertext per channel pubkey); AuthSpice mechanism=1
//  4. Read link result; non-OK → ClassTicket
//
// After success, subsequent I/O on conn uses mini-header framing.
//
// ctx cancel and deadline are honored for the entire handshake (bindConnToContext).
// When ctx has no deadline, a defaultLinkTimeout is applied for this phase only.
func linkChannel(ctx context.Context, conn net.Conn, password []byte, p linkParams) error {
	if err := ctx.Err(); err != nil {
		return mapTransportErr(err)
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultLinkTimeout)
		defer cancel()
	}

	release, err := bindConnToContext(ctx, conn)
	if err != nil {
		return mapTransportErr(fmt.Errorf("session: bind link context: %w", err))
	}
	defer release()

	var mess *protocol.LinkMess
	if p.ChannelType == protocol.ChannelMain {
		if p.ConnectionID != 0 {
			return ux.New(ux.ClassInternal, ux.MsgInternal,
				fmt.Errorf("session: main link mess connection_id must be 0"))
		}
		mess = protocol.NewMainLinkMess(p.ChannelCaps)
	} else {
		mess = protocol.NewChildLinkMess(p.ConnectionID, p.ChannelType, p.ChannelID, p.ChannelCaps)
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
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: parse link public key: %w", err))
	}

	// Fresh ticket encrypt per channel public key (never reuse ciphertext).
	ct, err := security.EncryptSpiceTicket(pub, password)
	if err != nil {
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
	return nil
}

// linkMainChannel is the main-channel link (connection_id=0, MAIN, id=0).
func linkMainChannel(ctx context.Context, conn net.Conn, password []byte) error {
	return linkChannel(ctx, conn, password, linkParams{
		ConnectionID: 0,
		ChannelType:  protocol.ChannelMain,
		ChannelID:    0,
	})
}

// bindConnToContext applies ctx deadline to conn and interrupts blocking I/O when
// ctx is cancelled (same pattern as connector.bindConnToContext for CONNECT).
// release must be called exactly once: stops the cancel watcher and clears the
// deadline so the long-lived channel is not bound to the link context.
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
