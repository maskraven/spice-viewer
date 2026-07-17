// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"fmt"
	"io"
	"log"
	"sync"
)

const DefaultTokens = 10

type ClipboardHandler interface {
	GuestGrabbed(selection uint8, types []uint32)
	GuestData(selection uint8, typ uint32, data []byte)
	GuestReleased(selection uint8)
}

type Session struct {
	mu          sync.Mutex
	w           io.Writer
	tokens      uint32
	peerCaps    []uint32
	haveCaps    bool
	selOK       bool
	guestTypes  []uint32
	pendingText []byte
	havePending bool
	handler     ClipboardHandler
	started     bool
	active      bool
}

func New(w io.Writer, h ClipboardHandler) *Session {
	return &Session{w: w, handler: h}
}

func (s *Session) Active() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active && s.haveCaps
}

func (s *Session) HandleMainAgentConnected(tokens uint32) error {
	if s == nil {
		return fmt.Errorf("agent: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	if tokens == 0 {
		tokens = DefaultTokens
	}
	if err := s.writeMainLocked(106, encodeU32(DefaultTokens)); err != nil {
		return err
	}
	s.tokens = tokens
	s.started = true
	s.active = true
	body := EncodeAnnounceCapabilities(true, DefaultClientCaps())
	return s.sendAgentLocked(Message{Protocol: Protocol, Type: MsgAnnounceCapabilities, Data: body})
}

func (s *Session) HandleMainAgentDisconnected() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = false
	s.haveCaps = false
	s.started = false
	s.pendingText = nil
	s.havePending = false
	s.guestTypes = nil
}

func (s *Session) AddTokens(n uint32) {
	if s == nil || n == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens += n
}

func (s *Session) HandleAgentData(payload []byte) error {
	if s == nil {
		return fmt.Errorf("agent: nil session")
	}
	msg, err := DecodeMessage(payload)
	if err != nil {
		return err
	}
	_ = s.writeMain(108, encodeU32(1))

	var cbGrab, cbData, cbRel bool
	var sel uint8
	var types []uint32
	var typ uint32
	var data []byte
	var h ClipboardHandler

	s.mu.Lock()
	h = s.handler
	switch msg.Type {
	case MsgAnnounceCapabilities:
		req, caps, err := DecodeAnnounceCapabilities(msg.Data)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.peerCaps = caps
		s.haveCaps = true
		s.selOK = HasCap(caps, CapClipboardSelection)
		if req {
			body := EncodeAnnounceCapabilities(false, DefaultClientCaps())
			err = s.sendAgentLocked(Message{Protocol: Protocol, Type: MsgAnnounceCapabilities, Data: body})
			s.mu.Unlock()
			return err
		}
		s.mu.Unlock()
		return nil
	case MsgClipboardGrab:
		sel, types, err = DecodeClipboardGrab(msg.Data, s.selOK)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.guestTypes = types
		cbGrab = true
		s.mu.Unlock()
	case MsgClipboardRequest:
		sel, typ, err = DecodeClipboardRequest(msg.Data, s.selOK)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		if typ != ClipboardUTF8Text || !s.havePending {
			s.mu.Unlock()
			return nil
		}
		data = append([]byte(nil), s.pendingText...)
		s.havePending = false
		s.pendingText = nil
		err = s.sendAgentLocked(Message{
			Protocol: Protocol, Type: MsgClipboard,
			Data: EncodeClipboardData(sel, ClipboardUTF8Text, data, s.selOK),
		})
		s.mu.Unlock()
		return err
	case MsgClipboard:
		sel, typ, data, err = DecodeClipboardData(msg.Data, s.selOK)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		cbData = true
		s.mu.Unlock()
	case MsgClipboardRelease:
		if s.selOK && len(msg.Data) >= 1 {
			sel = msg.Data[0]
		}
		cbRel = true
		s.mu.Unlock()
	case MsgMaxClipboard, MsgReply, MsgMonitorsConfig, MsgDisplayConfig, MsgMouseState:
		s.mu.Unlock()
		return nil
	default:
		s.mu.Unlock()
		log.Printf("agent: ignoring message type %d (%d bytes)", msg.Type, len(msg.Data))
		return nil
	}
	if h == nil {
		return nil
	}
	if cbGrab {
		h.GuestGrabbed(sel, types)
	} else if cbData {
		h.GuestData(sel, typ, data)
	} else if cbRel {
		h.GuestReleased(sel)
	}
	return nil
}

func (s *Session) SetHostClipboard(text string) error {
	if s == nil {
		return fmt.Errorf("agent: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || !s.haveCaps {
		return fmt.Errorf("agent: not connected")
	}
	if !HasCap(s.peerCaps, CapClipboard) && !HasCap(s.peerCaps, CapClipboardByDemand) {
		return fmt.Errorf("agent: guest has no clipboard capability")
	}
	s.pendingText = []byte(text)
	s.havePending = true
	return s.sendAgentLocked(Message{
		Protocol: Protocol, Type: MsgClipboardGrab,
		Data: EncodeClipboardGrab(SelectionClipboard, []uint32{ClipboardUTF8Text}, s.selOK),
	})
}

func (s *Session) RequestGuestClipboard() error {
	if s == nil {
		return fmt.Errorf("agent: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || !s.haveCaps {
		return fmt.Errorf("agent: not connected")
	}
	return s.sendAgentLocked(Message{
		Protocol: Protocol, Type: MsgClipboardRequest,
		Data: EncodeClipboardRequest(SelectionClipboard, ClipboardUTF8Text, s.selOK),
	})
}

func (s *Session) SendMonitorsConfig(width, height uint32) error {
	if s == nil {
		return fmt.Errorf("agent: nil session")
	}
	if width == 0 || height == 0 {
		return fmt.Errorf("agent: invalid monitors size %dx%d", width, height)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || !s.haveCaps {
		return fmt.Errorf("agent: not connected")
	}
	if !HasCap(s.peerCaps, CapMonitorsConfig) {
		return fmt.Errorf("agent: guest has no monitors config capability")
	}
	return s.sendAgentLocked(Message{
		Protocol: Protocol, Type: MsgMonitorsConfig,
		Data: EncodeMonitorsConfig(width, height),
	})
}

func encodeU32(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}

func (s *Session) sendAgentLocked(msg Message) error {
	if s.tokens == 0 {
		return fmt.Errorf("agent: no tokens for send")
	}
	if err := s.writeMainLocked(107, msg.Encode()); err != nil {
		return err
	}
	s.tokens--
	return nil
}

func (s *Session) writeMain(typ uint16, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeMainLocked(typ, body)
}

func (s *Session) writeMainLocked(typ uint16, body []byte) error {
	if s.w == nil {
		return fmt.Errorf("agent: nil writer")
	}
	return writeMini(s.w, typ, body)
}
