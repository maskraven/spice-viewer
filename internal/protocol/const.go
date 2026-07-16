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

// Main channel server messages (spice/enums.h; start at MsgFirstAvail).
const (
	MsgMainMigrateBegin      uint16 = 101
	MsgMainMigrateCancel     uint16 = 102
	MsgMainInit              uint16 = 103 // SPICE_MSG_MAIN_INIT
	MsgMainChannelsList      uint16 = 104 // SPICE_MSG_MAIN_CHANNELS_LIST
	MsgMainMouseMode         uint16 = 105
	MsgMainMultiMediaTime    uint16 = 106
	MsgMainAgentConnected    uint16 = 107
	MsgMainAgentDisconnected uint16 = 108
	MsgMainAgentData         uint16 = 109
	MsgMainAgentToken        uint16 = 110
)

// Main channel client messages.
const (
	MsgcMainClientInfo          uint16 = 101
	MsgcMainMigrateConnected    uint16 = 102
	MsgcMainMigrateConnectError uint16 = 103
	MsgcMainAttachChannels      uint16 = 104 // SPICE_MSGC_MAIN_ATTACH_CHANNELS
	MsgcMainMouseModeRequest    uint16 = 105
	MsgcMainAgentStart          uint16 = 106
	MsgcMainAgentData           uint16 = 107
	MsgcMainAgentToken          uint16 = 108
)

// Inputs channel server messages (spice/enums.h).
const (
	MsgInputsInit           uint16 = 101 // SPICE_MSG_INPUTS_INIT
	MsgInputsKeyModifiers   uint16 = 102 // SPICE_MSG_INPUTS_KEY_MODIFIERS
	MsgInputsMouseMotionAck uint16 = 111 // SPICE_MSG_INPUTS_MOUSE_MOTION_ACK
)

// Inputs channel client messages.
const (
	MsgcInputsKeyDown       uint16 = 101 // SPICE_MSGC_INPUTS_KEY_DOWN
	MsgcInputsKeyUp         uint16 = 102 // SPICE_MSGC_INPUTS_KEY_UP
	MsgcInputsKeyModifiers  uint16 = 103 // SPICE_MSGC_INPUTS_KEY_MODIFIERS
	MsgcInputsKeyScancode   uint16 = 104 // SPICE_MSGC_INPUTS_KEY_SCANCODE (cap)
	MsgcInputsMouseMotion   uint16 = 111 // SPICE_MSGC_INPUTS_MOUSE_MOTION
	MsgcInputsMousePosition uint16 = 112 // SPICE_MSGC_INPUTS_MOUSE_POSITION
	MsgcInputsMousePress    uint16 = 113 // SPICE_MSGC_INPUTS_MOUSE_PRESS
	MsgcInputsMouseRelease  uint16 = 114 // SPICE_MSGC_INPUTS_MOUSE_RELEASE
)

// Mouse modes (SPICE_MOUSE_MODE_*; bit flags, also used as current mode value).
const (
	MouseModeServer uint32 = 1 << 0 // SPICE_MOUSE_MODE_SERVER
	MouseModeClient uint32 = 1 << 1 // SPICE_MOUSE_MODE_CLIENT
)

// Keyboard LED / modifier bits (flags16 keyboard_modifier_flags).
const (
	ScrollLockModifier uint16 = 1 << 0 // SPICE_SCROLL_LOCK_MODIFIER
	NumLockModifier    uint16 = 1 << 1 // SPICE_NUM_LOCK_MODIFIER
	CapsLockModifier   uint16 = 1 << 2 // SPICE_CAPS_LOCK_MODIFIER
)

// Mouse button IDs (enum8 mouse_button).
const (
	MouseButtonInvalid uint8 = 0
	MouseButtonLeft    uint8 = 1
	MouseButtonMiddle  uint8 = 2
	MouseButtonRight   uint8 = 3
	MouseButtonUp      uint8 = 4 // scroll wheel up
	MouseButtonDown    uint8 = 5 // scroll wheel down
	MouseButtonSide    uint8 = 6
	MouseButtonExtra   uint8 = 7
)

// Mouse button mask bits (flags16 mouse_button_mask).
const (
	MouseButtonMaskLeft   uint16 = 1 << 0
	MouseButtonMaskMiddle uint16 = 1 << 1
	MouseButtonMaskRight  uint16 = 1 << 2
	MouseButtonMaskUp     uint16 = 1 << 3
	MouseButtonMaskDown   uint16 = 1 << 4
	MouseButtonMaskSide   uint16 = 1 << 5
	MouseButtonMaskExtra  uint16 = 1 << 6
)

// InputMotionAckBunch is SPICE_INPUT_MOTION_ACK_BUNCH (server acks every N motions).
const InputMotionAckBunch = 4

// Inputs channel capability bit indices.
const (
	InputsCapKeyScancode = 0 // SPICE_INPUTS_CAP_KEY_SCANCODE
)

// Wire body sizes (packed, little-endian; from spice.proto).
const (
	KeyCodeSize          = 4  // uint32 code
	KeyModifiersSize     = 2  // flags16
	MouseMotionSize      = 10 // int32 dx + int32 dy + flags16 buttons
	MousePositionSize    = 11 // uint32 x + uint32 y + flags16 buttons + uint8 display_id
	MouseButtonEventSize = 3  // enum8 button + flags16 buttons_state
	MainMouseModeSize    = 4  // flags16 supported + flags16 current
	MainMouseModeReqSize = 2  // flags16 mode
)

// MainInitSize is sizeof(SpiceMsgMainInit): 8 × uint32.
const MainInitSize = 32

// MiniHeaderSize is sizeof(SpiceMiniDataHeader): type u16 + size u32.
const MiniHeaderSize = 6

// DataHeaderSize is sizeof(SpiceDataHeader): serial u64 + type u16 + size u32 + sub_list u32.
const DataHeaderSize = 18
