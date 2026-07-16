// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

// Constants from spice-protocol spice/protocol.h and spice/enums.h
// @ 499cc8326a672e9e5747efc017319b19e1594b42.

const (
	// Magic is SPICE_MAGIC — four bytes "REDQ" on the wire (LE uint32 0x51444552).
	Magic = "REDQ"

	// MagicUint32 is SPICE_MAGIC as little-endian uint32 ('R' | 'E'<<8 | 'D'<<16 | 'Q'<<24).
	MagicUint32 uint32 = 0x51444552

	VersionMajor = 2
	VersionMinor = 2

	// SpiceLinkPubKeyBytes is SPICE_TICKET_PUBKEY_BYTES (162).
	SpiceLinkPubKeyBytes = 162

	// SpiceTicketKeyPairBits / SpiceTicketCiphertextLen from SPICE_TICKET_KEY_PAIR_LENGTH.
	SpiceTicketKeyPairBits   = 1024
	SpiceTicketCiphertextLen = SpiceTicketKeyPairBits / 8 // 128

	// SpiceMaxPasswordLength is SPICE_MAX_PASSWORD_LENGTH.
	SpiceMaxPasswordLength = 60

	// Link header is magic + major + minor + size (16 bytes).
	LinkHeaderSize = 16

	// SpiceLinkMess fixed fields size (connection_id through caps_offset).
	// Caps follow at CapsOffset when they are packed immediately after.
	LinkMessFixedSize = 18

	// SpiceLinkReply fixed fields size (error + pub_key + caps counts + offset).
	LinkReplyFixedSize = 4 + SpiceLinkPubKeyBytes + 4 + 4 + 4 // 178

	// CapsOffsetMess is the usual caps_offset for SpiceLinkMess (caps right after fixed).
	CapsOffsetMess = LinkMessFixedSize

	// CapsOffsetReply is the usual caps_offset for SpiceLinkReply.
	CapsOffsetReply = LinkReplyFixedSize

	// AuthMechanismSpice is the AuthSpice mechanism id (SPICE_COMMON_CAP_AUTH_SPICE path).
	AuthMechanismSpice uint32 = 1

	// MaxMessageBody is an upper bound for post-link message body size (DoS guard).
	MaxMessageBody = 10 << 20 // 10 MiB
)

// Channel type (SpiceChannelType).
const (
	ChannelMain uint8 = iota + 1
	ChannelDisplay
	ChannelInputs
	ChannelCursor
	ChannelPlayback
	ChannelRecord
	ChannelTunnel // obsolete
	ChannelSmartcard
	ChannelUSBRedir
	ChannelPort
	ChannelWebDAV
)

// Link error codes (SpiceLinkErr).
const (
	LinkErrOK uint32 = iota
	LinkErrError
	LinkErrInvalidMagic
	LinkErrInvalidData
	LinkErrVersionMismatch
	LinkErrNeedSecured
	LinkErrNeedUnsecured
	LinkErrPermissionDenied
	LinkErrBadConnectionID
	LinkErrChannelNotAvailable
)

// Common capability bit indices (enum order in spice/protocol.h).
const (
	CommonCapProtocolAuthSelection = 0 // SPICE_COMMON_CAP_PROTOCOL_AUTH_SELECTION
	CommonCapAuthSpice             = 1 // SPICE_COMMON_CAP_AUTH_SPICE
	CommonCapAuthSASL              = 2 // SPICE_COMMON_CAP_AUTH_SASL
	CommonCapMiniHeader            = 3 // SPICE_COMMON_CAP_MINI_HEADER
)

// Common messages (server → client).
const (
	MsgMigrate         uint16 = 1
	MsgMigrateData     uint16 = 2
	MsgSetAck          uint16 = 3
	MsgPing            uint16 = 4
	MsgWaitForChannels uint16 = 5
	MsgDisconnecting   uint16 = 6
	MsgNotify          uint16 = 7
	MsgFirstAvail      uint16 = 101
)

// Common client messages (client → server).
const (
	MsgcAckSync          uint16 = 1
	MsgcAck              uint16 = 2
	MsgcPong             uint16 = 3
	MsgcMigrateFlushMark uint16 = 4
	MsgcMigrateData      uint16 = 5
	MsgcDisconnecting    uint16 = 6
	MsgcFirstAvail       uint16 = 101
)

// MiniHeaderSize is sizeof(SpiceMiniDataHeader): type u16 + size u32.
const MiniHeaderSize = 6

// DataHeaderSize is sizeof(SpiceDataHeader): serial u64 + type u16 + size u32 + sub_list u32.
const DataHeaderSize = 18
