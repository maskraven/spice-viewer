// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/maskraven/virt-viewer/internal/protocol"
	"github.com/maskraven/virt-viewer/internal/security"
	"github.com/maskraven/virt-viewer/internal/ux"
)

// linkMainChannel runs the full main-channel link sequence on conn:
//
//  1. Write SpiceLinkMess (connection_id=0, MAIN, channel_id=0, Phase1 caps)
//  2. Read SpiceLinkReply; parse 162-byte DER pubkey
//  3. EncryptSpiceTicket; send AuthSpice mechanism=1
//  4. Read link result; non-OK → ClassTicket
//
// After success, subsequent I/O on conn uses mini-header framing (not consumed here).
func linkMainChannel(ctx context.Context, conn net.Conn, password []byte) error {
	if err := ctx.Err(); err != nil {
		return mapTransportErr(err)
	}

	// Honor ctx deadline on the connection for the duration of the handshake.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer func() { _ = conn.SetDeadline(time.Time{}) }()
	}

	mess := protocol.NewMainLinkMess(nil)
	if mess.ConnectionID != 0 {
		return ux.New(ux.ClassInternal, ux.MsgInternal,
			fmt.Errorf("session: main link mess connection_id must be 0"))
	}
	if err := protocol.WriteLinkMess(conn, mess); err != nil {
		return mapTransportErr(fmt.Errorf("session: write link mess: %w", err))
	}

	reply, _, err := protocol.ReadLinkReply(conn)
	if err != nil {
		return mapTransportErr(fmt.Errorf("session: read link reply: %w", err))
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
		return mapTransportErr(fmt.Errorf("session: write auth spice: %w", err))
	}

	result, err := protocol.ReadLinkResult(conn)
	if err != nil {
		return mapTransportErr(fmt.Errorf("session: read link result: %w", err))
	}
	if result != protocol.LinkErrOK {
		return mapLinkResult(result, fmt.Errorf("session: link result %d", result))
	}

	// Link OK: mini-header framing applies for subsequent messages (PR 06 stops here).
	// Caller may later use protocol.ReadMessage / WriteMessage on the connection.
	return nil
}
