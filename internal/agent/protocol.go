// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"encoding/binary"
	"fmt"
)

const Protocol = 1

const (
	MsgMouseState           uint32 = 1
	MsgMonitorsConfig       uint32 = 2
	MsgReply                uint32 = 3
	MsgClipboard            uint32 = 4
	MsgDisplayConfig        uint32 = 5
	MsgAnnounceCapabilities uint32 = 6
	MsgClipboardGrab        uint32 = 7
	MsgClipboardRequest     uint32 = 8
	MsgClipboardRelease     uint32 = 9
	MsgMaxClipboard         uint32 = 15
)

const (
	SelectionClipboard uint8 = 0
)

const (
	ClipboardNone     uint32 = 0
	ClipboardUTF8Text uint32 = 1
)

const (
	CapMonitorsConfig       uint = 1
	CapReply                uint = 2
	CapClipboard            uint = 3
	CapClipboardByDemand    uint = 5
	CapClipboardSelection   uint = 6
	CapSparseMonitorsConfig uint = 7
	CapGuestLineEndLF       uint = 8
	CapMaxClipboard         uint = 10
)

func DefaultClientCaps() []uint32 {
	return capsFromBits(CapMonitorsConfig, CapReply, CapClipboard, CapClipboardByDemand,
		CapClipboardSelection, CapSparseMonitorsConfig, CapGuestLineEndLF, CapMaxClipboard)
}

func capsFromBits(bits ...uint) []uint32 {
	var max uint
	for _, b := range bits {
		if b > max {
			max = b
		}
	}
	words := make([]uint32, max/32+1)
	for _, b := range bits {
		words[b/32] |= 1 << (b % 32)
	}
	return words
}

func HasCap(caps []uint32, n uint) bool {
	w := n / 32
	if int(w) >= len(caps) {
		return false
	}
	return caps[w]&(1<<(n%32)) != 0
}

type Message struct {
	Protocol uint32
	Type     uint32
	Opaque   uint64
	Data     []byte
}

const HeaderSize = 20
const MaxAgentMessage = 4 << 20

func (m Message) Encode() []byte {
	buf := make([]byte, HeaderSize+len(m.Data))
	binary.LittleEndian.PutUint32(buf[0:4], m.Protocol)
	binary.LittleEndian.PutUint32(buf[4:8], m.Type)
	binary.LittleEndian.PutUint64(buf[8:16], m.Opaque)
	binary.LittleEndian.PutUint32(buf[16:20], uint32(len(m.Data)))
	copy(buf[20:], m.Data)
	return buf
}

func DecodeMessage(b []byte) (Message, error) {
	if len(b) < HeaderSize {
		return Message{}, fmt.Errorf("agent: message short header: %d", len(b))
	}
	size := binary.LittleEndian.Uint32(b[16:20])
	if size > MaxAgentMessage {
		return Message{}, fmt.Errorf("agent: message too large: %d", size)
	}
	if len(b) < HeaderSize+int(size) {
		return Message{}, fmt.Errorf("agent: message short body: have %d want %d", len(b)-HeaderSize, size)
	}
	data := make([]byte, size)
	copy(data, b[HeaderSize:HeaderSize+int(size)])
	return Message{
		Protocol: binary.LittleEndian.Uint32(b[0:4]),
		Type:     binary.LittleEndian.Uint32(b[4:8]),
		Opaque:   binary.LittleEndian.Uint64(b[8:16]),
		Data:     data,
	}, nil
}

func EncodeAnnounceCapabilities(request bool, caps []uint32) []byte {
	buf := make([]byte, 4+4*len(caps))
	if request {
		binary.LittleEndian.PutUint32(buf[0:4], 1)
	}
	for i, c := range caps {
		binary.LittleEndian.PutUint32(buf[4+4*i:8+4*i], c)
	}
	return buf
}

func DecodeAnnounceCapabilities(b []byte) (request bool, caps []uint32, err error) {
	if len(b) < 4 {
		return false, nil, fmt.Errorf("agent: announce short")
	}
	request = binary.LittleEndian.Uint32(b[0:4]) != 0
	rest := b[4:]
	if len(rest)%4 != 0 {
		return false, nil, fmt.Errorf("agent: announce caps length %d", len(rest))
	}
	n := len(rest) / 4
	caps = make([]uint32, n)
	for i := 0; i < n; i++ {
		caps[i] = binary.LittleEndian.Uint32(rest[4*i : 4*i+4])
	}
	return request, caps, nil
}

func EncodeClipboardGrab(selection uint8, types []uint32, withSelection bool) []byte {
	if withSelection {
		buf := make([]byte, 1+4*len(types))
		buf[0] = selection
		for i, t := range types {
			binary.LittleEndian.PutUint32(buf[1+4*i:5+4*i], t)
		}
		return buf
	}
	buf := make([]byte, 4*len(types))
	for i, t := range types {
		binary.LittleEndian.PutUint32(buf[4*i:4*i+4], t)
	}
	return buf
}

func DecodeClipboardGrab(b []byte, withSelection bool) (selection uint8, types []uint32, err error) {
	off := 0
	if withSelection {
		if len(b) < 1 {
			return 0, nil, fmt.Errorf("agent: clipboard grab short")
		}
		selection = b[0]
		off = 1
	}
	rest := b[off:]
	if len(rest)%4 != 0 {
		return 0, nil, fmt.Errorf("agent: clipboard grab types len %d", len(rest))
	}
	n := len(rest) / 4
	types = make([]uint32, n)
	for i := 0; i < n; i++ {
		types[i] = binary.LittleEndian.Uint32(rest[4*i : 4*i+4])
	}
	return selection, types, nil
}

func EncodeClipboardRequest(selection uint8, typ uint32, withSelection bool) []byte {
	if withSelection {
		buf := make([]byte, 5)
		buf[0] = selection
		binary.LittleEndian.PutUint32(buf[1:5], typ)
		return buf
	}
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, typ)
	return buf
}

func DecodeClipboardRequest(b []byte, withSelection bool) (selection uint8, typ uint32, err error) {
	if withSelection {
		if len(b) < 5 {
			return 0, 0, fmt.Errorf("agent: clipboard request short")
		}
		return b[0], binary.LittleEndian.Uint32(b[1:5]), nil
	}
	if len(b) < 4 {
		return 0, 0, fmt.Errorf("agent: clipboard request short")
	}
	return 0, binary.LittleEndian.Uint32(b[0:4]), nil
}

func EncodeClipboardData(selection uint8, typ uint32, data []byte, withSelection bool) []byte {
	if withSelection {
		buf := make([]byte, 1+4+len(data))
		buf[0] = selection
		binary.LittleEndian.PutUint32(buf[1:5], typ)
		copy(buf[5:], data)
		return buf
	}
	buf := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(buf[0:4], typ)
	copy(buf[4:], data)
	return buf
}

func DecodeClipboardData(b []byte, withSelection bool) (selection uint8, typ uint32, data []byte, err error) {
	if withSelection {
		if len(b) < 5 {
			return 0, 0, nil, fmt.Errorf("agent: clipboard data short")
		}
		return b[0], binary.LittleEndian.Uint32(b[1:5]), append([]byte(nil), b[5:]...), nil
	}
	if len(b) < 4 {
		return 0, 0, nil, fmt.Errorf("agent: clipboard data short")
	}
	return 0, binary.LittleEndian.Uint32(b[0:4]), append([]byte(nil), b[4:]...), nil
}

func EncodeMonitorsConfig(width, height uint32) []byte {
	const monSize = 20
	buf := make([]byte, 8+monSize)
	binary.LittleEndian.PutUint32(buf[0:4], 1)
	binary.LittleEndian.PutUint32(buf[8:12], height)
	binary.LittleEndian.PutUint32(buf[12:16], width)
	binary.LittleEndian.PutUint32(buf[16:20], 32)
	return buf
}
