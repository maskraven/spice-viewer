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

	"github.com/maskraven/virt-viewer/internal/protocol"
)

// PlaybackDriver receives decoded playback audio (Phase 2: RAW S16LE PCM).
//
// UI backends implement this to feed an audio sink (or discard). All methods
// may be called from the playback Run goroutine; implementations must be
// concurrency-safe if shared with the UI thread.
//
// Decode failures and non-RAW modes never panic; the channel continues and
// open/runtime failures are best-effort (session continues without audio).
type PlaybackDriver interface {
	// Start configures the sink for a new stream.
	// format is protocol.AudioFmt* (Phase 2 only AudioFmtS16 is played).
	// channels is the number of interleaved channels; frequency is samples/s.
	Start(channels int, format uint16, frequency int)
	// Stop ends the current stream (may be called multiple times).
	Stop()
	// WritePCM delivers interleaved raw PCM for the active stream.
	// samples is little-endian signed 16-bit for AudioFmtS16.
	// timeMs is the SPICE multimedia timestamp from PLAYBACK_DATA.
	// The samples slice must be treated as immutable after return (copy if needed).
	WritePCM(samples []byte, timeMs uint32)
	// SetVolume sets per-channel volume (0..65535). Nil/empty clears to default.
	SetVolume(volumes []uint16)
	// SetMute mutes or unmutes playback.
	SetMute(mute bool)
}

// NullPlayback is a headless PlaybackDriver that discards samples.
// Methods are concurrency-safe; counters/state are for tests.
type NullPlayback struct {
	mu sync.Mutex

	Channels  int
	Format    uint16
	Frequency int
	Playing   bool
	Mute      bool
	Volumes   []uint16

	StartCount int
	StopCount  int
	WriteCount int
	WriteBytes int
	LastTime   uint32
	LastPCM    []byte // copy of last WritePCM buffer
}

// NewNullPlayback returns an empty NullPlayback.
func NewNullPlayback() *NullPlayback {
	return &NullPlayback{}
}

// Start implements PlaybackDriver.
func (n *NullPlayback) Start(channels int, format uint16, frequency int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.StartCount++
	n.Channels = channels
	n.Format = format
	n.Frequency = frequency
	n.Playing = true
}

// Stop implements PlaybackDriver.
func (n *NullPlayback) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.StopCount++
	n.Playing = false
}

// WritePCM implements PlaybackDriver.
func (n *NullPlayback) WritePCM(samples []byte, timeMs uint32) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.WriteCount++
	n.WriteBytes += len(samples)
	n.LastTime = timeMs
	if len(samples) > 0 {
		n.LastPCM = append([]byte(nil), samples...)
	} else {
		n.LastPCM = nil
	}
}

// SetVolume implements PlaybackDriver.
func (n *NullPlayback) SetVolume(volumes []uint16) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(volumes) == 0 {
		n.Volumes = nil
		return
	}
	n.Volumes = append([]uint16(nil), volumes...)
}

// SetMute implements PlaybackDriver.
func (n *NullPlayback) SetMute(mute bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Mute = mute
}

// Snapshot returns a copy of playback state (tests).
func (n *NullPlayback) Snapshot() (channels int, format uint16, freq int, playing, mute bool, vols []uint16, writes, bytes int, lastTime uint32, pcm []byte) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.Volumes != nil {
		vols = append([]uint16(nil), n.Volumes...)
	}
	if n.LastPCM != nil {
		pcm = append([]byte(nil), n.LastPCM...)
	}
	return n.Channels, n.Format, n.Frequency, n.Playing, n.Mute, vols, n.WriteCount, n.WriteBytes, n.LastTime, pcm
}

// Compile-time check.
var _ PlaybackDriver = (*NullPlayback)(nil)

// Playback is the Phase-2 best-effort SPICE playback channel reader/handler.
//
// Open failure is non-fatal at the session layer. Runtime decode errors are
// logged and do not stop the loop so a bad frame never kills the session.
// Only AudioDataModeRaw + AudioFmtS16 samples are delivered to the driver;
// Opus/CELT frames are ignored (server should send RAW when those caps are
// not advertised).
type Playback struct {
	conn   net.Conn
	driver PlaybackDriver

	mu      sync.Mutex
	unknown map[uint16]int
	lastErr error
	ack     protocol.AckState

	// Stream state (protected by mu).
	mode      uint16
	channels  uint32
	format    uint16
	frequency uint32
	started   bool
	mute      bool
}

// NewPlayback wraps a linked playback-channel connection and a PlaybackDriver.
// conn may be nil for pure unit tests that only call HandleMessage.
// driver may be nil (messages are decoded/discarded).
func NewPlayback(conn net.Conn, driver PlaybackDriver) *Playback {
	return &Playback{
		conn:    conn,
		driver:  driver,
		unknown: make(map[uint16]int),
		mode:    protocol.AudioDataModeInvalid,
	}
}

// LastError returns the most recent non-fatal handle error, if any.
func (p *Playback) LastError() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastErr
}

// Mode returns the last PLAYBACK_MODE value (0 if unset).
func (p *Playback) Mode() uint16 {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mode
}

// Started reports whether a START is active (between START and STOP).
func (p *Playback) Started() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.started
}

// Run reads mini-header messages until ctx cancel or connection close.
//
// Decode errors are logged and do not stop the loop (best-effort). I/O errors
// and context cancel stop Run; the caller must treat that as playback degrade
// only — never as a session-fatal condition.
func (p *Playback) Run(ctx context.Context) error {
	if p == nil || p.conn == nil {
		return fmt.Errorf("channel: playback: nil conn")
	}
	for {
		if err := ctx.Err(); err != nil {
			p.stopDriver()
			return err
		}
		msg, err := protocol.ReadMessage(p.conn)
		if err != nil {
			p.stopDriver()
			if err == io.EOF || isClosedConn(err) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("channel/playback: read error (degraded): %v", err)
			return err
		}
		if err := p.ack.AfterRead(p.conn); err != nil {
			log.Printf("channel/playback: ack: %v", err)
		}
		if err := p.HandleMessage(msg.Type, msg.Data); err != nil {
			log.Printf("channel/playback: handle type %d: %v", msg.Type, err)
		}
	}
}

// HandleMessage dispatches one server→client playback message by type.
//
// Decode failures return an error for the caller to log; they never panic.
// Unknown types are ignored (nil error).
func (p *Playback) HandleMessage(typ uint16, data []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("channel: playback: panic recovered: %v", r)
			log.Printf("%v", err)
		}
		if err != nil {
			p.mu.Lock()
			p.lastErr = err
			p.mu.Unlock()
		}
	}()

	// Common channel messages.
	switch typ {
	case protocol.MsgSetAck:
		return p.handleSetAck(data)
	case protocol.MsgPing:
		return p.handlePing(data)
	case protocol.MsgNotify, protocol.MsgWaitForChannels,
		protocol.MsgMigrate, protocol.MsgMigrateData, protocol.MsgDisconnecting:
		return nil
	}

	switch typ {
	case protocol.MsgPlaybackMode:
		return p.handleMode(data)
	case protocol.MsgPlaybackStart:
		return p.handleStart(data)
	case protocol.MsgPlaybackStop:
		return p.handleStop()
	case protocol.MsgPlaybackData:
		return p.handleData(data)
	case protocol.MsgPlaybackVolume:
		return p.handleVolume(data)
	case protocol.MsgPlaybackMute:
		return p.handleMute(data)
	case protocol.MsgPlaybackLatency:
		// Phase 2: accept and ignore (no sink buffering control yet).
		_, err := protocol.DecodePlaybackLatency(data)
		return err
	default:
		p.noteUnknown(typ)
		return nil
	}
}

// IsPlaybackMessage reports whether typ is a known playback-channel server message.
func IsPlaybackMessage(typ uint16) bool {
	switch typ {
	case protocol.MsgPlaybackData, protocol.MsgPlaybackMode, protocol.MsgPlaybackStart,
		protocol.MsgPlaybackStop, protocol.MsgPlaybackVolume, protocol.MsgPlaybackMute,
		protocol.MsgPlaybackLatency:
		return true
	default:
		return false
	}
}

func (p *Playback) noteUnknown(typ uint16) {
	p.mu.Lock()
	p.unknown[typ]++
	n := p.unknown[typ]
	p.mu.Unlock()
	if n == 1 {
		log.Printf("channel/playback: ignoring message type %d", typ)
	}
}

// UnknownCounts returns a copy of ignored-type counters (tests / diagnostics).
func (p *Playback) UnknownCounts() map[uint16]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[uint16]int, len(p.unknown))
	for k, v := range p.unknown {
		out[k] = v
	}
	return out
}

func (p *Playback) handleSetAck(data []byte) error {
	if p.conn == nil {
		return nil
	}
	return p.ack.OnSetAck(p.conn, data)
}

func (p *Playback) handlePing(data []byte) error {
	if p.conn == nil {
		return nil
	}
	return protocol.WriteMessage(p.conn, protocol.MsgcPong, data)
}

func (p *Playback) handleMode(data []byte) error {
	m, err := protocol.DecodePlaybackMode(data)
	if err != nil {
		return fmt.Errorf("channel: PLAYBACK_MODE: %w", err)
	}
	p.mu.Lock()
	p.mode = m.Mode
	p.mu.Unlock()
	if m.Mode != protocol.AudioDataModeRaw && m.Mode != protocol.AudioDataModeInvalid {
		// Not an error: server may send OPUS if we advertised the cap; Phase 2
		// only plays RAW. Log once-style via lastErr path if DATA arrives.
		log.Printf("channel/playback: mode=%d (only RAW=%d is played)", m.Mode, protocol.AudioDataModeRaw)
	}
	return nil
}

func (p *Playback) handleStart(data []byte) error {
	s, err := protocol.DecodePlaybackStart(data)
	if err != nil {
		return fmt.Errorf("channel: PLAYBACK_START: %w", err)
	}
	if s.Channels == 0 || s.Channels > 16 {
		return fmt.Errorf("channel: PLAYBACK_START: invalid channels %d", s.Channels)
	}
	if s.Frequency == 0 || s.Frequency > 192000 {
		return fmt.Errorf("channel: PLAYBACK_START: invalid frequency %d", s.Frequency)
	}

	p.mu.Lock()
	p.channels = s.Channels
	p.format = s.Format
	p.frequency = s.Frequency
	p.started = true
	p.mu.Unlock()

	if p.driver != nil {
		p.driver.Start(int(s.Channels), s.Format, int(s.Frequency))
	}
	return nil
}

func (p *Playback) handleStop() error {
	p.mu.Lock()
	p.started = false
	p.mu.Unlock()
	p.stopDriver()
	return nil
}

func (p *Playback) handleData(data []byte) error {
	pkt, err := protocol.DecodePlaybackData(data)
	if err != nil {
		return fmt.Errorf("channel: PLAYBACK_DATA: %w", err)
	}

	p.mu.Lock()
	started := p.started
	mode := p.mode
	format := p.format
	mute := p.mute
	p.mu.Unlock()

	if !started {
		// Spec: data only between START and STOP; ignore out-of-order.
		return nil
	}
	if mute {
		return nil
	}
	if mode != protocol.AudioDataModeRaw {
		// Compressed frames not decoded in Phase 2.
		return nil
	}
	if format != protocol.AudioFmtS16 {
		return fmt.Errorf("channel: PLAYBACK_DATA: unsupported format %d (want S16)", format)
	}
	if p.driver != nil && len(pkt.Data) > 0 {
		// Copy so the driver can retain without tying to the read buffer.
		samples := append([]byte(nil), pkt.Data...)
		p.driver.WritePCM(samples, pkt.Time)
	}
	return nil
}

func (p *Playback) handleVolume(data []byte) error {
	v, err := protocol.DecodePlaybackVolume(data)
	if err != nil {
		return fmt.Errorf("channel: PLAYBACK_VOLUME: %w", err)
	}
	if p.driver != nil {
		p.driver.SetVolume(v.Volumes)
	}
	return nil
}

func (p *Playback) handleMute(data []byte) error {
	mute, err := protocol.DecodePlaybackMute(data)
	if err != nil {
		return fmt.Errorf("channel: PLAYBACK_MUTE: %w", err)
	}
	p.mu.Lock()
	p.mute = mute
	p.mu.Unlock()
	if p.driver != nil {
		p.driver.SetMute(mute)
	}
	return nil
}

func (p *Playback) stopDriver() {
	if p.driver != nil {
		p.driver.Stop()
	}
}
