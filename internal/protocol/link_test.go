// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMagic(t *testing.T) {
	if Magic != "REDQ" {
		t.Fatalf("Magic = %q", Magic)
	}
	// LE uint32 of bytes 'R','E','D','Q'
	want := uint32('R') | uint32('E')<<8 | uint32('D')<<16 | uint32('Q')<<24
	if MagicUint32 != want {
		t.Fatalf("MagicUint32 = 0x%08x want 0x%08x", MagicUint32, want)
	}
	hdr := LinkHeader{Magic: MagicUint32, Major: VersionMajor, Minor: VersionMinor, Size: 0}
	enc := hdr.Encode()
	if string(enc[:4]) != Magic {
		t.Fatalf("encoded magic bytes %q", enc[:4])
	}
	dec, err := DecodeLinkHeader(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != hdr {
		t.Fatalf("round-trip header: %+v vs %+v", dec, hdr)
	}
	if err := dec.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLinkMess_RoundTrip(t *testing.T) {
	m := NewMainLinkMess([]uint32{0x1})
	if m.ConnectionID != 0 || m.ChannelType != ChannelMain {
		t.Fatalf("main mess: %+v", m)
	}
	if !HasCap(m.CommonCaps, CommonCapAuthSpice) {
		t.Fatal("missing AUTH_SPICE cap")
	}
	if !HasCap(m.CommonCaps, CommonCapMiniHeader) {
		t.Fatal("missing MINI_HEADER cap")
	}
	if !HasCap(m.CommonCaps, CommonCapProtocolAuthSelection) {
		t.Fatal("missing PROTOCOL_AUTH_SELECTION cap")
	}

	pkt := m.EncodePacket()
	if len(pkt) < LinkHeaderSize+LinkMessFixedSize {
		t.Fatalf("packet short: %d", len(pkt))
	}
	hdr, err := DecodeLinkHeader(pkt[:LinkHeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	if err := hdr.Validate(); err != nil {
		t.Fatal(err)
	}
	if int(hdr.Size) != len(pkt)-LinkHeaderSize {
		t.Fatalf("size %d body %d", hdr.Size, len(pkt)-LinkHeaderSize)
	}
	got, err := DecodeLinkMess(pkt[LinkHeaderSize:])
	if err != nil {
		t.Fatal(err)
	}
	if got.ConnectionID != m.ConnectionID || got.ChannelType != m.ChannelType || got.ChannelID != m.ChannelID {
		t.Fatalf("fields: %+v", got)
	}
	if got.CapsOffset != CapsOffsetMess {
		t.Fatalf("caps_offset %d want %d", got.CapsOffset, CapsOffsetMess)
	}
	if len(got.CommonCaps) != len(m.CommonCaps) || got.CommonCaps[0] != m.CommonCaps[0] {
		t.Fatalf("common caps: %v vs %v", got.CommonCaps, m.CommonCaps)
	}
	if len(got.ChannelCaps) != 1 || got.ChannelCaps[0] != 0x1 {
		t.Fatalf("channel caps: %v", got.ChannelCaps)
	}
}

func TestLinkReply_RoundTrip(t *testing.T) {
	pub := make([]byte, SpiceLinkPubKeyBytes)
	for i := range pub {
		pub[i] = byte(i)
	}
	r := &LinkReply{
		Error:       LinkErrOK,
		PubKey:      pub,
		CommonCaps:  Phase1CommonCaps(),
		ChannelCaps: nil,
	}
	pkt, err := r.EncodePacket()
	if err != nil {
		t.Fatal(err)
	}
	hdr, err := DecodeLinkHeader(pkt[:LinkHeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	if err := hdr.Validate(); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeLinkReply(pkt[LinkHeaderSize:])
	if err != nil {
		t.Fatal(err)
	}
	if got.Error != LinkErrOK {
		t.Fatalf("error %d", got.Error)
	}
	if !bytes.Equal(got.PubKey, pub) {
		t.Fatal("pub_key mismatch")
	}
	if got.CapsOffset != CapsOffsetReply {
		t.Fatalf("caps_offset %d want %d", got.CapsOffset, CapsOffsetReply)
	}
	if !HasCap(got.CommonCaps, CommonCapMiniHeader) {
		t.Fatal("missing mini-header on reply")
	}

	// Wrong pub_key length rejected on encode.
	if _, err := (&LinkReply{PubKey: make([]byte, 161)}).EncodeBody(); err == nil {
		t.Fatal("expected pub_key length error")
	}
}

func TestReadWriteLinkReply(t *testing.T) {
	pub := make([]byte, SpiceLinkPubKeyBytes)
	r := &LinkReply{Error: LinkErrOK, PubKey: pub, CommonCaps: CapsFromBits(CommonCapMiniHeader)}
	pkt, err := r.EncodePacket()
	if err != nil {
		t.Fatal(err)
	}
	got, hdr, err := ReadLinkReply(bytes.NewReader(pkt))
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Major != VersionMajor {
		t.Fatalf("major %d", hdr.Major)
	}
	if got.Error != LinkErrOK {
		t.Fatalf("error %d", got.Error)
	}
}

func TestAuthSpice_Mechanism1Layout(t *testing.T) {
	ct := make([]byte, SpiceTicketCiphertextLen)
	for i := range ct {
		ct[i] = byte(i ^ 0x5a)
	}
	pkt, err := EncodeAuthSpice(ct)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkt) != 4+128 {
		t.Fatalf("auth packet len %d", len(pkt))
	}
	mech := binary.LittleEndian.Uint32(pkt[0:4])
	if mech != AuthMechanismSpice || AuthMechanismSpice != 1 {
		t.Fatalf("mechanism = %d want AuthMechanismSpice=1", mech)
	}
	if !bytes.Equal(pkt[4:], ct) {
		t.Fatal("ciphertext mismatch")
	}

	got, err := DecodeAuthSpice(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mechanism != 1 {
		t.Fatalf("decoded mechanism %d", got.Mechanism)
	}
	if !bytes.Equal(got.Ciphertext, ct) {
		t.Fatal("decoded ciphertext mismatch")
	}

	if _, err := EncodeAuthSpice(ct[:127]); err == nil {
		t.Fatal("expected short ciphertext error")
	}
	if _, err := DecodeAuthSpice(pkt[:100]); err == nil {
		t.Fatal("expected short packet error")
	}

	var buf bytes.Buffer
	if err := WriteAuthSpice(&buf, ct); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), pkt) {
		t.Fatal("WriteAuthSpice mismatch")
	}
}

func TestLinkResult(t *testing.T) {
	enc := EncodeLinkResult(LinkErrOK)
	code, err := DecodeLinkResult(enc)
	if err != nil || code != LinkErrOK {
		t.Fatalf("ok: %d %v", code, err)
	}
	enc = EncodeLinkResult(LinkErrPermissionDenied)
	code, err = DecodeLinkResult(enc)
	if err != nil || code != LinkErrPermissionDenied {
		t.Fatalf("denied: %d %v", code, err)
	}
	var buf bytes.Buffer
	if err := WriteLinkResult(&buf, LinkErrOK); err != nil {
		t.Fatal(err)
	}
	code, err = ReadLinkResult(&buf)
	if err != nil || code != LinkErrOK {
		t.Fatalf("read: %d %v", code, err)
	}
}

func TestChannelConstants(t *testing.T) {
	// spice-protocol enums: MAIN=1 … WEBDAV=11
	cases := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"MAIN", ChannelMain, 1},
		{"DISPLAY", ChannelDisplay, 2},
		{"INPUTS", ChannelInputs, 3},
		{"CURSOR", ChannelCursor, 4},
		{"PLAYBACK", ChannelPlayback, 5},
		{"RECORD", ChannelRecord, 6},
		{"USBREDIR", ChannelUSBRedir, 9},
		{"PORT", ChannelPort, 10},
		{"WEBDAV", ChannelWebDAV, 11},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestCapsHelpers(t *testing.T) {
	c := CapsFromBits(CommonCapProtocolAuthSelection, CommonCapAuthSpice, CommonCapMiniHeader)
	// bits 0,1,3 → 0b1011 = 0xb
	if len(c) != 1 || c[0] != 0xb {
		t.Fatalf("Phase1-like caps = %v want [0xb]", c)
	}
	p1 := Phase1CommonCaps()
	if p1[0] != 0xb {
		t.Fatalf("Phase1CommonCaps = %v", p1)
	}
	if !HasCap(p1, CommonCapAuthSpice) || HasCap(p1, CommonCapAuthSASL) {
		t.Fatal("cap checks failed")
	}
	inter := IntersectCaps(p1, CapsFromBits(CommonCapMiniHeader, CommonCapAuthSASL))
	if !HasCap(inter, CommonCapMiniHeader) || HasCap(inter, CommonCapAuthSpice) {
		t.Fatalf("intersect = %v", inter)
	}
}

func TestChildLinkMess(t *testing.T) {
	m := NewChildLinkMess(0xabc, ChannelDisplay, 0, nil)
	if m.ConnectionID != 0xabc || m.ChannelType != ChannelDisplay {
		t.Fatalf("%+v", m)
	}
	body := m.EncodeBody()
	got, err := DecodeLinkMess(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConnectionID != 0xabc {
		t.Fatalf("connection_id %d", got.ConnectionID)
	}
}
