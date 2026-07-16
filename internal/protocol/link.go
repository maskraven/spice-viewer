// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// LinkHeader is SpiceLinkHeader: magic, major, minor, size (of body following).
type LinkHeader struct {
	Magic uint32
	Major uint32
	Minor uint32
	Size  uint32
}

// Encode writes the 16-byte link header in little-endian wire order.
func (h LinkHeader) Encode() []byte {
	buf := make([]byte, LinkHeaderSize)
	binary.LittleEndian.PutUint32(buf[0:4], h.Magic)
	binary.LittleEndian.PutUint32(buf[4:8], h.Major)
	binary.LittleEndian.PutUint32(buf[8:12], h.Minor)
	binary.LittleEndian.PutUint32(buf[12:16], h.Size)
	return buf
}

// DecodeLinkHeader parses a 16-byte SpiceLinkHeader.
func DecodeLinkHeader(b []byte) (LinkHeader, error) {
	if len(b) < LinkHeaderSize {
		return LinkHeader{}, fmt.Errorf("spice: link header short: %d", len(b))
	}
	return LinkHeader{
		Magic: binary.LittleEndian.Uint32(b[0:4]),
		Major: binary.LittleEndian.Uint32(b[4:8]),
		Minor: binary.LittleEndian.Uint32(b[8:12]),
		Size:  binary.LittleEndian.Uint32(b[12:16]),
	}, nil
}

// ReadLinkHeader reads a link header from r.
func ReadLinkHeader(r io.Reader) (LinkHeader, error) {
	var buf [LinkHeaderSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return LinkHeader{}, err
	}
	return DecodeLinkHeader(buf[:])
}

// Validate checks magic and protocol major version.
func (h LinkHeader) Validate() error {
	if h.Magic != MagicUint32 {
		return fmt.Errorf("spice: invalid magic 0x%08x want 0x%08x (REDQ)", h.Magic, MagicUint32)
	}
	if h.Major != VersionMajor {
		return fmt.Errorf("spice: major version %d want %d", h.Major, VersionMajor)
	}
	return nil
}

// LinkMess is SpiceLinkMess (body after LinkHeader), including capability words.
type LinkMess struct {
	ConnectionID   uint32
	ChannelType    uint8
	ChannelID      uint8
	NumCommonCaps  uint32
	NumChannelCaps uint32
	CapsOffset     uint32
	CommonCaps     []uint32
	ChannelCaps    []uint32
}

// EncodeBody serializes the SpiceLinkMess body (no link header).
// CapsOffset is set to CapsOffsetMess (18) so caps follow the fixed fields.
func (m *LinkMess) EncodeBody() []byte {
	m.NumCommonCaps = uint32(len(m.CommonCaps))
	m.NumChannelCaps = uint32(len(m.ChannelCaps))
	m.CapsOffset = CapsOffsetMess

	bodyLen := LinkMessFixedSize + 4*len(m.CommonCaps) + 4*len(m.ChannelCaps)
	buf := make([]byte, bodyLen)
	binary.LittleEndian.PutUint32(buf[0:4], m.ConnectionID)
	buf[4] = m.ChannelType
	buf[5] = m.ChannelID
	binary.LittleEndian.PutUint32(buf[6:10], m.NumCommonCaps)
	binary.LittleEndian.PutUint32(buf[10:14], m.NumChannelCaps)
	binary.LittleEndian.PutUint32(buf[14:18], m.CapsOffset)

	off := LinkMessFixedSize
	for _, c := range m.CommonCaps {
		binary.LittleEndian.PutUint32(buf[off:off+4], c)
		off += 4
	}
	for _, c := range m.ChannelCaps {
		binary.LittleEndian.PutUint32(buf[off:off+4], c)
		off += 4
	}
	return buf
}

// EncodePacket returns link header + SpiceLinkMess body ready to write.
func (m *LinkMess) EncodePacket() []byte {
	body := m.EncodeBody()
	hdr := LinkHeader{
		Magic: MagicUint32,
		Major: VersionMajor,
		Minor: VersionMinor,
		Size:  uint32(len(body)),
	}
	return append(hdr.Encode(), body...)
}

// DecodeLinkMess parses a SpiceLinkMess body (after link header).
func DecodeLinkMess(body []byte) (*LinkMess, error) {
	if len(body) < LinkMessFixedSize {
		return nil, fmt.Errorf("spice: link mess short: %d", len(body))
	}
	m := &LinkMess{
		ConnectionID:   binary.LittleEndian.Uint32(body[0:4]),
		ChannelType:    body[4],
		ChannelID:      body[5],
		NumCommonCaps:  binary.LittleEndian.Uint32(body[6:10]),
		NumChannelCaps: binary.LittleEndian.Uint32(body[10:14]),
		CapsOffset:     binary.LittleEndian.Uint32(body[14:18]),
	}
	caps, err := decodeCaps(body, m.CapsOffset, m.NumCommonCaps, m.NumChannelCaps)
	if err != nil {
		return nil, err
	}
	m.CommonCaps = caps.common
	m.ChannelCaps = caps.channel
	return m, nil
}

// LinkReply is SpiceLinkReply (body after LinkHeader).
type LinkReply struct {
	Error          uint32
	PubKey         []byte // SPICE_TICKET_PUBKEY_BYTES (162)
	NumCommonCaps  uint32
	NumChannelCaps uint32
	CapsOffset     uint32
	CommonCaps     []uint32
	ChannelCaps    []uint32
}

// EncodeBody serializes the SpiceLinkReply body.
func (r *LinkReply) EncodeBody() ([]byte, error) {
	if len(r.PubKey) != SpiceLinkPubKeyBytes {
		return nil, fmt.Errorf("spice: pub_key length %d want %d", len(r.PubKey), SpiceLinkPubKeyBytes)
	}
	r.NumCommonCaps = uint32(len(r.CommonCaps))
	r.NumChannelCaps = uint32(len(r.ChannelCaps))
	r.CapsOffset = CapsOffsetReply

	bodyLen := LinkReplyFixedSize + 4*len(r.CommonCaps) + 4*len(r.ChannelCaps)
	buf := make([]byte, bodyLen)
	binary.LittleEndian.PutUint32(buf[0:4], r.Error)
	copy(buf[4:4+SpiceLinkPubKeyBytes], r.PubKey)
	off := 4 + SpiceLinkPubKeyBytes
	binary.LittleEndian.PutUint32(buf[off:off+4], r.NumCommonCaps)
	binary.LittleEndian.PutUint32(buf[off+4:off+8], r.NumChannelCaps)
	binary.LittleEndian.PutUint32(buf[off+8:off+12], r.CapsOffset)
	off = LinkReplyFixedSize
	for _, c := range r.CommonCaps {
		binary.LittleEndian.PutUint32(buf[off:off+4], c)
		off += 4
	}
	for _, c := range r.ChannelCaps {
		binary.LittleEndian.PutUint32(buf[off:off+4], c)
		off += 4
	}
	return buf, nil
}

// EncodePacket returns link header + SpiceLinkReply body.
func (r *LinkReply) EncodePacket() ([]byte, error) {
	body, err := r.EncodeBody()
	if err != nil {
		return nil, err
	}
	hdr := LinkHeader{
		Magic: MagicUint32,
		Major: VersionMajor,
		Minor: VersionMinor,
		Size:  uint32(len(body)),
	}
	return append(hdr.Encode(), body...), nil
}

// DecodeLinkReply parses a SpiceLinkReply body (after link header).
func DecodeLinkReply(body []byte) (*LinkReply, error) {
	if len(body) < LinkReplyFixedSize {
		return nil, fmt.Errorf("spice: link reply short: %d", len(body))
	}
	r := &LinkReply{
		Error: binary.LittleEndian.Uint32(body[0:4]),
	}
	r.PubKey = make([]byte, SpiceLinkPubKeyBytes)
	copy(r.PubKey, body[4:4+SpiceLinkPubKeyBytes])
	off := 4 + SpiceLinkPubKeyBytes
	r.NumCommonCaps = binary.LittleEndian.Uint32(body[off : off+4])
	r.NumChannelCaps = binary.LittleEndian.Uint32(body[off+4 : off+8])
	r.CapsOffset = binary.LittleEndian.Uint32(body[off+8 : off+12])

	caps, err := decodeCaps(body, r.CapsOffset, r.NumCommonCaps, r.NumChannelCaps)
	if err != nil {
		return nil, err
	}
	r.CommonCaps = caps.common
	r.ChannelCaps = caps.channel
	return r, nil
}

// ReadLinkReply reads header + body and decodes a SpiceLinkReply.
func ReadLinkReply(r io.Reader) (*LinkReply, LinkHeader, error) {
	hdr, err := ReadLinkHeader(r)
	if err != nil {
		return nil, hdr, err
	}
	if err := hdr.Validate(); err != nil {
		return nil, hdr, err
	}
	if hdr.Size > 4096 {
		return nil, hdr, fmt.Errorf("spice: link reply size %d too large", hdr.Size)
	}
	body := make([]byte, hdr.Size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, hdr, err
	}
	reply, err := DecodeLinkReply(body)
	return reply, hdr, err
}

// WriteLinkMess writes a full SpiceLinkMess packet (header + body).
func WriteLinkMess(w io.Writer, m *LinkMess) error {
	_, err := w.Write(m.EncodePacket())
	return err
}

// AuthSpice is the post-reply authentication message when AUTH_SELECTION is negotiated:
//
//	uint32_le mechanism=1 || ciphertext[128]
type AuthSpice struct {
	Mechanism  uint32
	Ciphertext []byte // 128 bytes
}

// EncodeAuthSpice builds mechanism=1 || ciphertext[128].
func EncodeAuthSpice(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) != SpiceTicketCiphertextLen {
		return nil, fmt.Errorf("spice: auth ciphertext length %d want %d",
			len(ciphertext), SpiceTicketCiphertextLen)
	}
	buf := make([]byte, 4+SpiceTicketCiphertextLen)
	binary.LittleEndian.PutUint32(buf[0:4], AuthMechanismSpice)
	copy(buf[4:], ciphertext)
	return buf, nil
}

// DecodeAuthSpice parses mechanism + 128-byte ciphertext.
func DecodeAuthSpice(b []byte) (*AuthSpice, error) {
	if len(b) != 4+SpiceTicketCiphertextLen {
		return nil, fmt.Errorf("spice: auth spice message length %d want %d",
			len(b), 4+SpiceTicketCiphertextLen)
	}
	mech := binary.LittleEndian.Uint32(b[0:4])
	ct := make([]byte, SpiceTicketCiphertextLen)
	copy(ct, b[4:])
	return &AuthSpice{Mechanism: mech, Ciphertext: ct}, nil
}

// WriteAuthSpice writes the AuthSpice packet (mechanism=1 + ciphertext).
func WriteAuthSpice(w io.Writer, ciphertext []byte) error {
	pkt, err := EncodeAuthSpice(ciphertext)
	if err != nil {
		return err
	}
	_, err = w.Write(pkt)
	return err
}

// LinkResult is the uint32 link result after ticket auth (SpiceLinkErr).
type LinkResult uint32

// EncodeLinkResult serializes a link result word.
func EncodeLinkResult(code uint32) []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, code)
	return buf
}

// DecodeLinkResult parses a 4-byte link result.
func DecodeLinkResult(b []byte) (uint32, error) {
	if len(b) < 4 {
		return 0, fmt.Errorf("spice: link result short: %d", len(b))
	}
	return binary.LittleEndian.Uint32(b[0:4]), nil
}

// ReadLinkResult reads a 4-byte link result from r.
func ReadLinkResult(r io.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return DecodeLinkResult(buf[:])
}

// WriteLinkResult writes a 4-byte link result.
func WriteLinkResult(w io.Writer, code uint32) error {
	_, err := w.Write(EncodeLinkResult(code))
	return err
}

type capPair struct {
	common  []uint32
	channel []uint32
}

// decodeCaps reads common then channel capability words starting at capsOffset
// (offset from start of body, i.e. from connection_id / error).
func decodeCaps(body []byte, capsOffset, numCommon, numChannel uint32) (capPair, error) {
	need := int(capsOffset) + 4*int(numCommon) + 4*int(numChannel)
	if need > len(body) || int(capsOffset) > len(body) {
		return capPair{}, fmt.Errorf("spice: caps out of range: offset=%d common=%d channel=%d body=%d",
			capsOffset, numCommon, numChannel, len(body))
	}
	// Caps may leave padding between fixed struct and caps_offset; use offset.
	off := int(capsOffset)
	var common []uint32
	if numCommon > 0 {
		common = make([]uint32, numCommon)
		for i := uint32(0); i < numCommon; i++ {
			common[i] = binary.LittleEndian.Uint32(body[off : off+4])
			off += 4
		}
	}
	var channel []uint32
	if numChannel > 0 {
		channel = make([]uint32, numChannel)
		for i := uint32(0); i < numChannel; i++ {
			channel[i] = binary.LittleEndian.Uint32(body[off : off+4])
			off += 4
		}
	}
	return capPair{common: common, channel: channel}, nil
}

// NewMainLinkMess builds a main-channel SpiceLinkMess (connection_id=0)
// advertising Phase 1 common caps.
func NewMainLinkMess(channelCaps []uint32) *LinkMess {
	return &LinkMess{
		ConnectionID: 0,
		ChannelType:  ChannelMain,
		ChannelID:    0,
		CommonCaps:   Phase1CommonCaps(),
		ChannelCaps:  append([]uint32(nil), channelCaps...),
	}
}

// NewChildLinkMess builds a non-main channel SpiceLinkMess with session connection_id.
func NewChildLinkMess(connectionID uint32, channelType, channelID uint8, channelCaps []uint32) *LinkMess {
	return &LinkMess{
		ConnectionID: connectionID,
		ChannelType:  channelType,
		ChannelID:    channelID,
		CommonCaps:   Phase1CommonCaps(),
		ChannelCaps:  append([]uint32(nil), channelCaps...),
	}
}
