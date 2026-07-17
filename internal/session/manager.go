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
	"github.com/maskraven/virt-viewer/internal/codec/h264"
	"github.com/maskraven/virt-viewer/internal/protocol"
	"github.com/maskraven/virt-viewer/internal/security"
	"github.com/maskraven/virt-viewer/internal/ux"
)

// maxParallelChildOpens bounds concurrent child dial+link work (design: 4–8).
const maxParallelChildOpens = 6

// openState is the OpenChannels lifecycle latch (not concurrent-safe re-entry).
// connectionID alone is never used as the success latch.
type openState int

const (
	openIdle openState = iota
	openInProgress
	openReady  // children open successfully
	openFailed // terminal until Close; MAIN_INIT/list already consumed on wire
)

// OpenChannels reads MAIN_INIT and CHANNELS_LIST on the linked main channel,
// then opens child channels in parallel:
//
//   - DISPLAY and INPUTS: required (failure is session-fatal)
//   - CURSOR: best-effort (failure logs a warning; session continues)
//   - PLAYBACK: best-effort (Phase 2; failure logs a warning; session continues)
//   - RECORD, USBREDIR (all listed ids), WEBDAV: best-effort (Phase 3 scaffolds)
//
// Port (non-WebDAV) is not opened. Best-effort open failures never fail the session.
//
// Prerequisites: main link complete (DialMain / LinkMain). Children use
// connection_id = session_id from MAIN_INIT and a fresh ticket encrypt per
// channel public key.
//
// OpenChannels is single-flight: concurrent calls are rejected. It is one-shot
// per Session — success or failure both prevent a second open (main has already
// consumed ATTACH_CHANNELS / CHANNELS_LIST; create a new Session to retry).
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
	switch s.openState {
	case openInProgress:
		s.mu.Unlock()
		return ux.New(ux.ClassInternal, ux.MsgInternal,
			fmt.Errorf("session: OpenChannels already in progress"))
	case openReady:
		s.mu.Unlock()
		return ux.New(ux.ClassInternal, ux.MsgInternal,
			fmt.Errorf("session: channels already opened"))
	case openFailed:
		s.mu.Unlock()
		return ux.New(ux.ClassInternal, ux.MsgInternal,
			fmt.Errorf("session: channel open previously failed; close and create a new session"))
	}
	s.openState = openInProgress
	main := s.mainConn
	lifeCtx := s.lifeCtx
	s.mu.Unlock()

	// Terminal failure unless we reach openReady.
	success := false
	defer func() {
		if success {
			return
		}
		s.mu.Lock()
		if s.openState == openInProgress {
			s.openState = openFailed
			// Do not publish partial children or connectionID on failure.
			s.displayConn = nil
			s.inputsConn = nil
			s.cursorConn = nil
			s.cursorErr = nil
			s.playbackConn = nil
			s.playbackErr = nil
			s.recordConn = nil
			s.recordErr = nil
			s.usbConns = nil
			s.usbErrs = nil
			s.webdavConn = nil
			s.webdavErr = nil
			s.connectionID = 0
			s.mainInit = nil
			s.channelList = nil
		}
		s.mu.Unlock()
	}()

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

	// Keep session_id local until children succeed (connectionID is not a success latch).
	connID := init.SessionID

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
		// usbSlot, when >= 0, stores success into usbResults[usbSlot].
		usbSlot int
	}

	var (
		displayConn, inputsConn, cursorConn, playbackConn net.Conn
		recordConn, webdavConn                            net.Conn
		cursorErr, playbackErr, recordErr, webdavErr      error
	)
	usbResults := make([]USBChannel, len(want.usbredir))
	usbErrs := make([]error, len(want.usbredir))

	jobs := []openJob{
		{ch: *want.display, fatal: true, result: &displayConn, usbSlot: -1},
		{ch: *want.inputs, fatal: true, result: &inputsConn, usbSlot: -1},
	}
	if want.cursor != nil {
		jobs = append(jobs, openJob{ch: *want.cursor, fatal: false, result: &cursorConn, errOut: &cursorErr, usbSlot: -1})
	}
	if want.playback != nil {
		jobs = append(jobs, openJob{ch: *want.playback, fatal: false, result: &playbackConn, errOut: &playbackErr, usbSlot: -1})
	}
	if want.record != nil {
		jobs = append(jobs, openJob{ch: *want.record, fatal: false, result: &recordConn, errOut: &recordErr, usbSlot: -1})
	}
	if want.webdav != nil {
		jobs = append(jobs, openJob{ch: *want.webdav, fatal: false, result: &webdavConn, errOut: &webdavErr, usbSlot: -1})
	}
	for i := range want.usbredir {
		jobs = append(jobs, openJob{ch: want.usbredir[i], fatal: false, errOut: &usbErrs[i], usbSlot: i})
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
					cancel() // stop siblings
				} else if job.errOut != nil {
					*job.errOut = err
				}
				return
			}

			// Refuse new opens after Close or sibling cancel.
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
					cancel()
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
					// Cancel sibling opens (including best-effort) on fatal failure.
					cancel()
				} else if job.errOut != nil {
					*job.errOut = err
				}
				return
			}

			// If a fatal sibling already failed, drop this conn immediately.
			mu.Lock()
			failed := fatalErr != nil
			mu.Unlock()
			if failed || ctx.Err() != nil {
				_ = conn.Close()
				if job.fatal {
					mu.Lock()
					if fatalErr == nil {
						fatalErr = mapTransportErr(context.Canceled)
					}
					mu.Unlock()
				} else if job.errOut != nil && *job.errOut == nil {
					*job.errOut = mapTransportErr(context.Canceled)
				}
				return
			}
			if job.usbSlot >= 0 {
				usbResults[job.usbSlot] = USBChannel{ID: job.ch.ID, Conn: conn}
				return
			}
			if job.result != nil {
				*job.result = conn
			}
		}()
	}
	wg.Wait()

	// Collect successful USB conns (skip slots that failed).
	var usbConns []USBChannel
	for i, u := range usbResults {
		if u.Conn != nil {
			usbConns = append(usbConns, u)
		} else if usbErrs[i] != nil {
			log.Printf("session: usbredir channel id=%d open failed (degraded): %v",
				want.usbredir[i].ID, usbErrs[i])
		}
	}

	if fatalErr != nil {
		// Tear down any children that succeeded before the fatal failure.
		closeConns(displayConn, inputsConn, cursorConn, playbackConn, recordConn, webdavConn)
		for _, u := range usbConns {
			if u.Conn != nil {
				_ = u.Conn.Close()
			}
		}
		return fatalErr
	}

	if cursorErr != nil {
		log.Printf("session: cursor channel open failed (degraded): %v", cursorErr)
	}
	if playbackErr != nil {
		log.Printf("session: playback channel open failed (degraded): %v", playbackErr)
	}
	if recordErr != nil {
		log.Printf("session: record channel open failed (degraded): %v", recordErr)
	}
	if webdavErr != nil {
		log.Printf("session: webdav channel open failed (degraded): %v", webdavErr)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		closeConns(displayConn, inputsConn, cursorConn, playbackConn, recordConn, webdavConn)
		for _, u := range usbConns {
			if u.Conn != nil {
				_ = u.Conn.Close()
			}
		}
		// defer marks openFailed
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("session: closed during child open"))
	}
	// Publish only on full success (required channels open; best-effort may be nil).
	s.connectionID = connID
	initCopy := init
	s.mainInit = &initCopy
	s.channelList = append([]protocol.ChannelID(nil), list.Channels...)
	s.displayConn = displayConn
	s.inputsConn = inputsConn
	s.cursorConn = cursorConn
	s.cursorErr = cursorErr
	s.playbackConn = playbackConn
	s.playbackErr = playbackErr
	s.recordConn = recordConn
	s.recordErr = recordErr
	s.usbConns = usbConns
	s.usbErrs = append([]error(nil), usbErrs...)
	s.webdavConn = webdavConn
	s.webdavErr = webdavErr
	s.openState = openReady
	success = true
	return nil
}

// closeConns closes non-nil connections, ignoring errors.
func closeConns(conns ...net.Conn) {
	for _, c := range conns {
		if c != nil {
			_ = c.Close()
		}
	}
}

// phase1Want holds openable channel instances selected from CHANNELS_LIST.
// USB redir keeps every listed channel id (multi-redir).
type phase1Want struct {
	display  *protocol.ChannelID
	inputs   *protocol.ChannelID
	cursor   *protocol.ChannelID
	playback *protocol.ChannelID
	record   *protocol.ChannelID
	webdav   *protocol.ChannelID
	usbredir []protocol.ChannelID
}

// selectPhase1Channels picks required DISPLAY/INPUTS and optional best-effort
// channels (cursor, playback, record, usbredir×N, webdav). Port is ignored.
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
		case protocol.ChannelPlayback:
			if w.playback == nil {
				c := ch
				w.playback = &c
			}
		case protocol.ChannelRecord:
			if w.record == nil {
				c := ch
				w.record = &c
			}
		case protocol.ChannelWebDAV:
			if w.webdav == nil {
				c := ch
				w.webdav = &c
			}
		case protocol.ChannelUSBRedir:
			w.usbredir = append(w.usbredir, ch)
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

	// Advertise per-channel caps so the server enables preferred features.
	// Inputs: KEY_SCANCODE (spice-gtk always sets SPICE_INPUTS_CAP_KEY_SCANCODE).
	// Playback: VOLUME only (no OPUS/CELT) so the server prefers RAW PCM, which
	// Phase 2 decodes without cgo/native audio codecs.
	// Record: VOLUME only (no OPUS/CELT); client answers MODE=RAW on START.
	// USB/WebDAV: no VMC LZ4 cap (scaffold accepts raw DATA only).
	// Display: MULTI_CODEC + MJPEG always; CODEC_H264 only when h264.Available()
	// (OS decoder on macOS/Windows; user FFmpeg on Linux — never advertise when false).
	var channelCaps []uint32
	switch ch.Type {
	case protocol.ChannelDisplay:
		channelCaps = protocol.DisplayChannelCaps(h264.Available())
	case protocol.ChannelInputs:
		channelCaps = protocol.CapsFromBits(protocol.InputsCapKeyScancode)
	case protocol.ChannelPlayback:
		channelCaps = protocol.CapsFromBits(protocol.PlaybackCapVolume)
	case protocol.ChannelRecord:
		channelCaps = protocol.CapsFromBits(protocol.RecordCapVolume)
	}

	err = linkChannel(ctx, conn, pw, linkParams{
		ConnectionID: connectionID,
		ChannelType:  ch.Type,
		ChannelID:    ch.ID,
		ChannelCaps:  channelCaps,
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
		init     protocol.MainInit
		list     protocol.ChannelsList
		haveInit bool
		haveList bool
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

	return init, list, nil
}
