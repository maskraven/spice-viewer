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
	"time"

	"github.com/maskraven/virt-viewer/internal/protocol"
)

// RecordDriver supplies host microphone PCM for the guest (Phase 3 scaffold).
//
// UI backends implement this for real capture later. All methods may be called
// from the record Run goroutine; implementations must be concurrency-safe if
// shared with the UI thread.
//
// NullRecord is the default: it acknowledges START (MODE=RAW + START_MARK) and
// does not produce PCM frames (guest mic stays silent). Silence-frame injection
// is intentionally omitted to avoid a timer goroutine; a future real driver will
// push samples via the channel SendPCM path.
type RecordDriver interface {
	// Start configures the capture source for a new stream.
	// format is protocol.AudioFmt* (scaffold only plays RAW S16).
	Start(channels int, format uint16, frequency int)
	// Stop ends capture (may be called multiple times).
	Stop()
	// SetVolume sets per-channel capture gain (0..65535). Nil/empty clears.
	SetVolume(volumes []uint16)
	// SetMute mutes or unmutes capture.
	SetMute(mute bool)
}

// NullRecord is a headless RecordDriver that never produces PCM.
// Methods are concurrency-safe; counters/state are for tests.
//
// Product choice: on RECORD_START the channel still replies MODE=RAW and
// START_MARK so the server does not stall; no RECORD_DATA is sent (silent mic).
type NullRecord struct {
	mu sync.Mutex

	Channels  int
	Format    uint16
	Frequency int
	Recording bool
	Mute      bool
	Volumes   []uint16

	StartCount int
	StopCount  int
}

// NewNullRecord returns an empty NullRecord.
func NewNullRecord() *NullRecord {
	return &NullRecord{}
}

// Start implements RecordDriver.
func (n *NullRecord) Start(channels int, format uint16, frequency int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.StartCount++
	n.Channels = channels
	n.Format = format
	n.Frequency = frequency
	n.Recording = true
}

// Stop implements RecordDriver.
func (n *NullRecord) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.StopCount++
	n.Recording = false
}

// SetVolume implements RecordDriver.
func (n *NullRecord) SetVolume(volumes []uint16) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(volumes) == 0 {
		n.Volumes = nil
		return
	}
	n.Volumes = append([]uint16(nil), volumes...)
}

// SetMute implements RecordDriver.
func (n *NullRecord) SetMute(mute bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Mute = mute
}

// Snapshot returns a copy of record state (tests).
func (n *NullRecord) Snapshot() (channels int, format uint16, freq int, recording, mute bool, vols []uint16, starts, stops int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.Volumes != nil {
		vols = append([]uint16(nil), n.Volumes...)
	}
	return n.Channels, n.Format, n.Frequency, n.Recording, n.Mute, vols, n.StartCount, n.StopCount
}

// Compile-time check.
var _ RecordDriver = (*NullRecord)(nil)

// Record is the Phase-3 best-effort SPICE record channel reader/handler.
//
// Open failure is non-fatal at the session layer. Runtime errors are logged and
// never panic. On RECORD_START the client replies MODE=RAW + START_MARK.
// NullRecord does not emit RECORD_DATA (silent); real drivers may call SendPCM.
type Record struct {
	conn   net.Conn
	driver RecordDriver

	mu      sync.Mutex
	unknown map[uint16]int
	lastErr error
	ack     protocol.AckState

	channels  uint32
	format    uint16
	frequency uint32
	started   bool
	mute      bool
	modeSent  bool
}

// NewRecord wraps a linked record-channel connection and a RecordDriver.
// conn may be nil for pure unit tests that only call HandleMessage.
// driver may be nil (START still answers MODE/START_MARK when conn is set).
func NewRecord(conn net.Conn, driver RecordDriver) *Record {
	return &Record{
		conn:    conn,
		driver:  driver,
		unknown: make(map[uint16]int),
	}
}

// LastError returns the most recent non-fatal handle error, if any.
func (r *Record) LastError() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}

// Started reports whether a START is active (between START and STOP).
func (r *Record) Started() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started
}

// Run reads mini-header messages until ctx cancel or connection close.
//
// Decode errors are logged and do not stop the loop (best-effort). I/O errors
// and context cancel stop Run; the caller must treat that as record degrade
// only — never as a session-fatal condition.
func (r *Record) Run(ctx context.Context) error {
	if r == nil || r.conn == nil {
		return fmt.Errorf("channel: record: nil conn")
	}
	for {
		if err := ctx.Err(); err != nil {
			r.stopDriver()
			return err
		}
		msg, err := protocol.ReadMessage(r.conn)
		if err != nil {
			r.stopDriver()
			if err == io.EOF || isClosedConn(err) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("channel/record: read error (degraded): %v", err)
			return err
		}
		if err := r.ack.AfterRead(r.conn); err != nil {
			log.Printf("channel/record: ack: %v", err)
		}
		if err := r.HandleMessage(msg.Type, msg.Data); err != nil {
			log.Printf("channel/record: handle type %d: %v", msg.Type, err)
		}
	}
}

// HandleMessage dispatches one server→client record message by type.
//
// Decode failures return an error for the caller to log; they never panic.
// Unknown types are ignored (nil error).
func (r *Record) HandleMessage(typ uint16, data []byte) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("channel: record: panic recovered: %v", rec)
			log.Printf("%v", err)
		}
		if err != nil {
			r.mu.Lock()
			r.lastErr = err
			r.mu.Unlock()
		}
	}()

	switch typ {
	case protocol.MsgSetAck:
		return r.handleSetAck(data)
	case protocol.MsgPing:
		return r.handlePing(data)
	case protocol.MsgNotify, protocol.MsgWaitForChannels,
		protocol.MsgMigrate, protocol.MsgMigrateData, protocol.MsgDisconnecting:
		return nil
	}

	switch typ {
	case protocol.MsgRecordStart:
		return r.handleStart(data)
	case protocol.MsgRecordStop:
		return r.handleStop()
	case protocol.MsgRecordVolume:
		return r.handleVolume(data)
	case protocol.MsgRecordMute:
		return r.handleMute(data)
	default:
		r.noteUnknown(typ)
		return nil
	}
}

// IsRecordMessage reports whether typ is a known record-channel server message.
func IsRecordMessage(typ uint16) bool {
	switch typ {
	case protocol.MsgRecordStart, protocol.MsgRecordStop,
		protocol.MsgRecordVolume, protocol.MsgRecordMute:
		return true
	default:
		return false
	}
}

// SendPCM sends one RECORD_DATA packet (RAW S16LE samples) when started and not muted.
// NullRecord never calls this; real mic drivers use it from a capture loop.
// samples must be treated as immutable after return (this method copies for the wire).
func (r *Record) SendPCM(samples []byte, timeMs uint32) error {
	if r == nil || r.conn == nil {
		return fmt.Errorf("channel: record: nil conn")
	}
	r.mu.Lock()
	started := r.started
	mute := r.mute
	r.mu.Unlock()
	if !started || mute || len(samples) == 0 {
		return nil
	}
	body := protocol.EncodeRecordData(protocol.RecordData{
		Time: timeMs,
		Data: samples,
	})
	return protocol.WriteMessage(r.conn, protocol.MsgcRecordData, body)
}

func (r *Record) noteUnknown(typ uint16) {
	r.mu.Lock()
	r.unknown[typ]++
	n := r.unknown[typ]
	r.mu.Unlock()
	if n == 1 {
		log.Printf("channel/record: ignoring message type %d", typ)
	}
}

// UnknownCounts returns a copy of ignored-type counters (tests / diagnostics).
func (r *Record) UnknownCounts() map[uint16]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[uint16]int, len(r.unknown))
	for k, v := range r.unknown {
		out[k] = v
	}
	return out
}

func (r *Record) handleSetAck(data []byte) error {
	if r.conn == nil {
		return nil
	}
	return r.ack.OnSetAck(r.conn, data)
}

func (r *Record) handlePing(data []byte) error {
	if r.conn == nil {
		return nil
	}
	return protocol.WriteMessage(r.conn, protocol.MsgcPong, data)
}

func (r *Record) handleStart(data []byte) error {
	s, err := protocol.DecodeRecordStart(data)
	if err != nil {
		return fmt.Errorf("channel: RECORD_START: %w", err)
	}
	if s.Channels == 0 || s.Channels > 16 {
		return fmt.Errorf("channel: RECORD_START: invalid channels %d", s.Channels)
	}
	if s.Frequency == 0 || s.Frequency > 192000 {
		return fmt.Errorf("channel: RECORD_START: invalid frequency %d", s.Frequency)
	}

	r.mu.Lock()
	r.channels = s.Channels
	r.format = s.Format
	r.frequency = s.Frequency
	r.started = true
	r.modeSent = false
	r.mu.Unlock()

	if r.driver != nil {
		r.driver.Start(int(s.Channels), s.Format, int(s.Frequency))
	}

	// Acknowledge with MODE=RAW and START_MARK. NullRecord sends no PCM after this.
	if err := r.sendModeAndMark(); err != nil {
		return err
	}
	return nil
}

func (r *Record) sendModeAndMark() error {
	if r.conn == nil {
		// Unit tests without a conn still succeed at driver level.
		r.mu.Lock()
		r.modeSent = true
		r.mu.Unlock()
		return nil
	}
	// time field: use a coarse wall clock ms; server uses it for A/V sync.
	now := uint32(time.Now().UnixMilli() & 0xffffffff)
	modeBody := protocol.EncodeRecordMode(protocol.RecordMode{
		Time: now,
		Mode: protocol.AudioDataModeRaw,
	})
	if err := protocol.WriteMessage(r.conn, protocol.MsgcRecordMode, modeBody); err != nil {
		return fmt.Errorf("channel: RECORD_MODE: %w", err)
	}
	mark := protocol.EncodeRecordStartMark(now)
	if err := protocol.WriteMessage(r.conn, protocol.MsgcRecordStartMark, mark); err != nil {
		return fmt.Errorf("channel: RECORD_START_MARK: %w", err)
	}
	r.mu.Lock()
	r.modeSent = true
	r.mu.Unlock()
	return nil
}

// ModeSent reports whether MODE+START_MARK were written after the last START (tests).
func (r *Record) ModeSent() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.modeSent
}

func (r *Record) handleStop() error {
	r.mu.Lock()
	r.started = false
	r.modeSent = false
	r.mu.Unlock()
	r.stopDriver()
	return nil
}

func (r *Record) handleVolume(data []byte) error {
	v, err := protocol.DecodeRecordVolume(data)
	if err != nil {
		return fmt.Errorf("channel: RECORD_VOLUME: %w", err)
	}
	if r.driver != nil {
		r.driver.SetVolume(v.Volumes)
	}
	return nil
}

func (r *Record) handleMute(data []byte) error {
	mute, err := protocol.DecodeRecordMute(data)
	if err != nil {
		return fmt.Errorf("channel: RECORD_MUTE: %w", err)
	}
	r.mu.Lock()
	r.mute = mute
	r.mu.Unlock()
	if r.driver != nil {
		r.driver.SetMute(mute)
	}
	return nil
}

func (r *Record) stopDriver() {
	if r.driver != nil {
		r.driver.Stop()
	}
}
