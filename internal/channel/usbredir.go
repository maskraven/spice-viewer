// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/maskraven/spice-viewer/internal/protocol"
)

// USBFilter is an optional host-side device filter hook for future libusb backends.
// The scaffold default is NilUSBFilter (allow-all / no-op). Full host stack is out of scope.
type USBFilter interface {
	// Allow reports whether a device (opaque descriptor) may be redirected.
	// idVendor/idProduct are USB ids; the rest is reserved for later backends.
	Allow(idVendor, idProduct uint16) bool
}

// NilUSBFilter allows every device (placeholder until a real filter lands).
type NilUSBFilter struct{}

// Allow implements USBFilter.
func (NilUSBFilter) Allow(idVendor, idProduct uint16) bool { return true }

// USBRedir is a Phase-3 best-effort USB redirection channel scaffold.
//
// Wire transport is SpiceVMC (DATA / COMPRESSED_DATA). Open failure is never
// session-fatal. This scaffold reads VMC frames and discards them (or hands
// them to an optional VMCHandler queue for a future host backend). No libusb
// is linked in the default binary.
type USBRedir struct {
	conn    net.Conn
	channel uint8 // SPICE channel_id (multi-redir support)
	handler VMCHandler
	filter  USBFilter

	mu      sync.Mutex
	unknown map[uint16]int
	lastErr error
	ack     protocol.AckState
	frames  int
	bytes   int
}

// USBRedirOpts configures optional filter/handler for NewUSBRedir.
type USBRedirOpts struct {
	// ChannelID is the SPICE channel_id from CHANNELS_LIST (0..n).
	ChannelID uint8
	// Handler receives VMC DATA copies; nil uses NullVMCHandler.
	Handler VMCHandler
	// Filter is reserved for host backends; nil uses NilUSBFilter.
	Filter USBFilter
}

// NewUSBRedir wraps a linked usbredir channel connection.
// conn may be nil for pure unit tests that only call HandleMessage.
func NewUSBRedir(conn net.Conn, opts USBRedirOpts) *USBRedir {
	h := opts.Handler
	if h == nil {
		h = &NullVMCHandler{}
	}
	f := opts.Filter
	if f == nil {
		f = NilUSBFilter{}
	}
	return &USBRedir{
		conn:    conn,
		channel: opts.ChannelID,
		handler: h,
		filter:  f,
		unknown: make(map[uint16]int),
	}
}

// ChannelID returns the SPICE channel_id for this usbredir instance.
func (u *USBRedir) ChannelID() uint8 {
	if u == nil {
		return 0
	}
	return u.channel
}

// Filter returns the configured USBFilter (never nil after NewUSBRedir).
func (u *USBRedir) Filter() USBFilter {
	if u == nil {
		return NilUSBFilter{}
	}
	return u.filter
}

// Stats returns accepted VMC frame count and total bytes (tests / diagnostics).
func (u *USBRedir) Stats() (frames, bytes int) {
	if u == nil {
		return 0, 0
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.frames, u.bytes
}

// LastError returns the most recent non-fatal handle error, if any.
func (u *USBRedir) LastError() error {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastErr
}

// Run reads mini-header messages until ctx cancel or connection close.
// Errors are best-effort (never session-fatal).
func (u *USBRedir) Run(ctx context.Context) error {
	if u == nil || u.conn == nil {
		return fmt.Errorf("channel: usbredir: nil conn")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		msg, err := protocol.ReadMessage(u.conn)
		if err != nil {
			if err == io.EOF || isClosedConn(err) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("channel/usbredir[%d]: read error (degraded): %v", u.channel, err)
			return err
		}
		if err := u.ack.AfterRead(u.conn); err != nil {
			log.Printf("channel/usbredir[%d]: ack: %v", u.channel, err)
		}
		if err := u.HandleMessage(msg.Type, msg.Data); err != nil {
			log.Printf("channel/usbredir[%d]: handle type %d: %v", u.channel, msg.Type, err)
		}
	}
}

// HandleMessage dispatches one server→client usbredir (VMC) message.
// Never panics; decode failures return an error for the caller to log.
func (u *USBRedir) HandleMessage(typ uint16, data []byte) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("channel: usbredir: panic recovered: %v", rec)
			log.Printf("%v", err)
		}
		if err != nil {
			u.mu.Lock()
			u.lastErr = err
			u.mu.Unlock()
		}
	}()

	switch typ {
	case protocol.MsgSetAck:
		if u.conn == nil {
			return nil
		}
		return u.ack.OnSetAck(u.conn, data)
	case protocol.MsgPing:
		if u.conn == nil {
			return nil
		}
		return protocol.WriteMessage(u.conn, protocol.MsgcPong, data)
	case protocol.MsgNotify, protocol.MsgWaitForChannels,
		protocol.MsgMigrate, protocol.MsgMigrateData, protocol.MsgDisconnecting:
		return nil
	case protocol.MsgSpiceVMCData:
		return u.handleData(data)
	case protocol.MsgSpiceVMCCompressedData:
		// Scaffold: parse and discard (no LZ4 host path; cap not advertised).
		c, err := ParseVMCCompressed(data)
		if err != nil {
			return fmt.Errorf("channel: usbredir VMC_COMPRESSED: %w", err)
		}
		log.Printf("channel/usbredir[%d]: discarding compressed VMC type=%d len=%d",
			u.channel, c.Type, len(c.Data))
		return nil
	default:
		u.noteUnknown(typ)
		return nil
	}
}

func (u *USBRedir) handleData(data []byte) error {
	payload := ParseVMCData(data)
	// Copy so the handler can retain without tying to the read buffer.
	cp := append([]byte(nil), payload...)
	u.mu.Lock()
	u.frames++
	u.bytes += len(cp)
	u.mu.Unlock()
	if u.handler != nil {
		u.handler.OnVMCData(cp)
	}
	return nil
}

func (u *USBRedir) noteUnknown(typ uint16) {
	u.mu.Lock()
	u.unknown[typ]++
	n := u.unknown[typ]
	u.mu.Unlock()
	if n == 1 {
		log.Printf("channel/usbredir[%d]: ignoring message type %d", u.channel, typ)
	}
}

// SendData writes a client→server VMC DATA frame (future host backend use).
func (u *USBRedir) SendData(payload []byte) error {
	if u == nil {
		return fmt.Errorf("channel: usbredir: nil")
	}
	return SendVMCData(u.conn, payload)
}
