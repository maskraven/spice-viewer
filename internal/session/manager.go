// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/maskraven/virt-viewer/internal/channel"
	"github.com/maskraven/virt-viewer/internal/protocol"
	"github.com/maskraven/virt-viewer/internal/security"
	"github.com/maskraven/virt-viewer/internal/ux"
)

// maxParallelChildOpens bounds concurrent child dial+link work (design: 4–8).
const maxParallelChildOpens = 6

// OpenChannels reads MAIN_INIT and CHANNELS_LIST on the linked main channel,
// then opens Phase-1 child channels in parallel:
//
//   - DISPLAY and INPUTS: required (failure is session-fatal)
//   - CURSOR: best-effort (failure logs a warning; session continues)
//
// Playback, record, usbredir, port, and webdav are never opened in Phase 1.
//
// Prerequisites: main link complete (DialMain / LinkMain). Children use
// connection_id = session_id from MAIN_INIT and a fresh ticket encrypt per
// channel public key.
func (s *Session) OpenChannels(ctx context.Context) error {
	if s == nil {
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: nil Session"))
	}

	s.openWG.Add(1)
	defer s.openWG.Done()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: closed"))
	}
	if !s.linked || s.mainConn == nil {
		s.mu.Unlock()
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: main not linked"))
	}
	if s.connectionID != 0 {
		s.mu.Unlock()
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: channels already opened"))
	}
	main := s.mainConn
	lifeCtx := s.lifeCtx
	s.mu.Unlock()

	// Combine caller ctx with session lifetime (Close cancels lifeCtx).
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(lifeCtx, cancel)
	defer stop()

	init, list, err := readMainInitAndChannels(ctx, main)
	if err != nil {
		return err
	}
	if init.SessionID == 0 {
		// session_id 0 is reserved for "new main" in SpiceLinkMess; treat as invalid.
		return ux.New(ux.ClassInternal, ux.MsgInternal,
			fmt.Errorf("session: MAIN_INIT session_id is 0"))
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: closed during main init"))
	}
	s.connectionID = init.SessionID
	s.mainInit = &init
	s.channelList = append([]protocol.ChannelID(nil), list.Channels...)
	connID := s.connectionID
	s.mu.Unlock()

	want := selectPhase1Channels(list.Channels)
	if want.display == nil {
		return ux.New(ux.ClassInternal, ux.MsgInternal,
			fmt.Errorf("session: CHANNELS_LIST missing DISPLAY channel"))
	}
	if want.inputs == nil {
		return ux.New(ux.ClassInternal, ux.MsgInternal,
			fmt.Errorf("session: CHANNELS_LIST missing INPUTS channel"))
	}

	type openJob struct {
		ch     protocol.ChannelID
		fatal  bool // display/inputs
		result *net.Conn
		errOut *error
	}

	var (
		displayConn, inputsConn, cursorConn net.Conn
		cursorErr                           error
	)

	jobs := []openJob{
		{ch: *want.display, fatal: true, result: &displayConn},
		{ch: *want.inputs, fatal: true, result: &inputsConn},
	}
	if want.cursor != nil {
		jobs = append(jobs, openJob{ch: *want.cursor, fatal: false, result: &cursorConn, errOut: &cursorErr})
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		fatalErr error
	)

	for _, job := range jobs {
		job := job
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Bound concurrency.
			select {
			case s.openSem <- struct{}{}:
				defer func() { <-s.openSem }()
			case <-ctx.Done():
				err := mapTransportErr(ctx.Err())
				if job.fatal {
					mu.Lock()
					if fatalErr == nil {
						fatalErr = err
					}
					mu.Unlock()
				} else if job.errOut != nil {
					*job.errOut = err
				}
				return
			}

			// Refuse new opens after Close.
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || ctx.Err() != nil {
				err := mapTransportErr(context.Canceled)
				if ctx.Err() != nil {
					err = mapTransportErr(ctx.Err())
				}
				if job.fatal {
					mu.Lock()
					if fatalErr == nil {
						fatalErr = err
					}
					mu.Unlock()
				} else if job.errOut != nil {
					*job.errOut = err
				}
				return
			}

			conn, err := s.dialAndLinkChild(ctx, connID, job.ch)
			if err != nil {
				if job.fatal {
					mu.Lock()
					if fatalErr == nil {
						fatalErr = err
					}
					mu.Unlock()
				} else if job.errOut != nil {
					*job.errOut = err
				}
				return
			}
			*job.result = conn
		}()
	}
	wg.Wait()

	if fatalErr != nil {
		// Tear down any children that succeeded before the fatal failure.
		for _, c := range []net.Conn{displayConn, inputsConn, cursorConn} {
			if c != nil {
				_ = c.Close()
			}
		}
		return fatalErr
	}

	if cursorErr != nil {
		log.Printf("session: cursor channel open failed (degraded): %v", cursorErr)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		for _, c := range []net.Conn{displayConn, inputsConn, cursorConn} {
			if c != nil {
				_ = c.Close()
			}
		}
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: closed during child open"))
	}
	s.displayConn = displayConn
	s.inputsConn = inputsConn
	s.cursorConn = cursorConn
	s.cursorErr = cursorErr
	return nil
}

// phase1Want holds the first listed instance of each Phase-1 channel type.
type phase1Want struct {
	display *protocol.ChannelID
	inputs  *protocol.ChannelID
	cursor  *protocol.ChannelID
}

// selectPhase1Channels picks DISPLAY, INPUTS, and optional CURSOR from the list.
// Unsupported types (playback, record, usbredir, port, webdav, …) are ignored.
func selectPhase1Channels(list []protocol.ChannelID) phase1Want {
	var w phase1Want
	for i := range list {
		ch := list[i]
		if !channel.IsPhase1Open(ch.Type) {
			continue
		}
		switch ch.Type {
		case protocol.ChannelDisplay:
			if w.display == nil {
				c := ch
				w.display = &c
			}
		case protocol.ChannelInputs:
			if w.inputs == nil {
				c := ch
				w.inputs = &c
			}
		case protocol.ChannelCursor:
			if w.cursor == nil {
				c := ch
				w.cursor = &c
			}
		}
	}
	return w
}

// dialAndLinkChild dials the same endpoint and completes a child-channel link
// with connection_id = session_id and a fresh ticket encrypt.
func (s *Session) dialAndLinkChild(ctx context.Context, connectionID uint32, ch protocol.ChannelID) (net.Conn, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: closed"))
	}
	dialer := s.dialer
	ep := s.endpoint
	pw := append([]byte(nil), s.password...)
	s.mu.Unlock()
	defer security.Wipe(pw)

	conn, err := dialer.DialSPICE(ctx, ep)
	if err != nil {
		return nil, mapDialError(err)
	}

	err = linkChannel(ctx, conn, pw, linkParams{
		ConnectionID: connectionID,
		ChannelType:  ch.Type,
		ChannelID:    ch.ID,
	})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// readMainInitAndChannels consumes mini-header messages on main until both
// MAIN_INIT and CHANNELS_LIST have been received. After MAIN_INIT it sends
// SPICE_MSGC_MAIN_ATTACH_CHANNELS (required before the server emits the list).
//
// Unknown / non-critical main messages (SET_ACK, PING, NOTIFY, …) are skipped
// so a chatty server cannot block child opens.
func readMainInitAndChannels(ctx context.Context, main net.Conn) (protocol.MainInit, protocol.ChannelsList, error) {
	var (
		init       protocol.MainInit
		list       protocol.ChannelsList
		haveInit   bool
		haveList   bool
		attachSent bool
	)

	release, err := bindConnToContext(ctx, main)
	if err != nil {
		return init, list, mapTransportErr(fmt.Errorf("session: bind main read: %w", err))
	}
	defer release()

	for !haveInit || !haveList {
		if err := ctx.Err(); err != nil {
			return init, list, mapTransportErr(err)
		}
		msg, err := protocol.ReadMessage(main)
		if err != nil {
			return init, list, mapLinkIOErr(ctx, fmt.Errorf("session: read main message: %w", err))
		}

		switch msg.Type {
		case protocol.MsgMainInit:
			if haveInit {
				return init, list, ux.New(ux.ClassInternal, ux.MsgInternal,
					fmt.Errorf("session: duplicate MAIN_INIT"))
			}
			init, err = protocol.DecodeMainInit(msg.Data)
			if err != nil {
				return init, list, ux.New(ux.ClassConfig, ux.MsgConfigProtocol,
					fmt.Errorf("session: decode MAIN_INIT: %w", err))
			}
			haveInit = true
			// Server must receive ATTACH_CHANNELS before CHANNELS_LIST.
			if err := protocol.WriteMessage(main, protocol.MsgcMainAttachChannels, nil); err != nil {
				return init, list, mapLinkIOErr(ctx, fmt.Errorf("session: write ATTACH_CHANNELS: %w", err))
			}
			attachSent = true

		case protocol.MsgMainChannelsList:
			if !haveInit {
				return init, list, ux.New(ux.ClassInternal, ux.MsgInternal,
					fmt.Errorf("session: CHANNELS_LIST before MAIN_INIT"))
			}
			list, err = protocol.DecodeChannelsList(msg.Data)
			if err != nil {
				return init, list, ux.New(ux.ClassConfig, ux.MsgConfigProtocol,
					fmt.Errorf("session: decode CHANNELS_LIST: %w", err))
			}
			haveList = true

		case protocol.MsgSetAck:
			// Optional: respond with ACK_SYNC so servers that gate on it proceed.
			if len(msg.Data) >= 4 {
				gen := msg.Data[0:4] // generation uint32 LE
				_ = protocol.WriteMessage(main, protocol.MsgcAckSync, gen)
			}

		case protocol.MsgPing:
			// Echo pong body if present (id + time); ignore write errors mid-setup.
			if len(msg.Data) >= 12 {
				_ = protocol.WriteMessage(main, protocol.MsgcPong, msg.Data[:12])
			}

		default:
			// Ignore migrate/agent/notify/unknown during open (Phase 1).
			_ = msg
		}
	}

	if !attachSent {
		// Defensive: always true if haveInit; keep compiler happy.
		_ = attachSent
	}
	return init, list, nil
}
