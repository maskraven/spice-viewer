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

	// MaxMessageBody is an upper bound for post-link message body size (DoS guard
	// on the wire framing path). This is intentionally lower than MaxSurfaceBytes:
	// surfaces are local buffers (up to 64 MiB each, with a compositor-wide total
	// cap), while a single SPICE message is limited to 10 MiB so one framed packet
	// cannot force a huge allocation. Servers tile large updates; raw frames that
	// exceed MaxMessageBody cannot arrive on the network path even if the surface
	// is larger. Codec bounds still allow up to MaxSurfaceBytes for injected /
	// offline decode paths.
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

// Display channel server messages (spice/enums.h @ 499cc8326a672e9e5747efc017319b19e1594b42).
const (
	MsgDisplayMode             uint16 = 101 // SPICE_MSG_DISPLAY_MODE
	MsgDisplayMark             uint16 = 102 // SPICE_MSG_DISPLAY_MARK
	MsgDisplayReset            uint16 = 103 // SPICE_MSG_DISPLAY_RESET
	MsgDisplayCopyBits         uint16 = 104
	MsgDisplayInvalList        uint16 = 105
	MsgDisplayInvalAllPixmaps  uint16 = 106
	MsgDisplayInvalPalette     uint16 = 107
	MsgDisplayInvalAllPalettes uint16 = 108
	MsgDisplayStreamCreate     uint16 = 122
	MsgDisplayStreamData       uint16 = 123
	MsgDisplayStreamClip       uint16 = 124
	MsgDisplayStreamDestroy    uint16 = 125
	MsgDisplayStreamDestroyAll uint16 = 126
	MsgDisplayDrawFill         uint16 = 302 // SPICE_MSG_DISPLAY_DRAW_FILL
	MsgDisplayDrawOpaque       uint16 = 303
	MsgDisplayDrawCopy         uint16 = 304 // SPICE_MSG_DISPLAY_DRAW_COPY
	MsgDisplayDrawBlend        uint16 = 305
	MsgDisplayDrawBlackness    uint16 = 306
	MsgDisplayDrawWhiteness    uint16 = 307
	MsgDisplayDrawInvers       uint16 = 308
	MsgDisplayDrawRop3         uint16 = 309
	MsgDisplayDrawStroke       uint16 = 310
	MsgDisplayDrawText         uint16 = 311
	MsgDisplayDrawTransparent  uint16 = 312
	MsgDisplayDrawAlphaBlend   uint16 = 313
	MsgDisplaySurfaceCreate    uint16 = 314 // SPICE_MSG_DISPLAY_SURFACE_CREATE
	MsgDisplaySurfaceDestroy   uint16 = 315 // SPICE_MSG_DISPLAY_SURFACE_DESTROY
	MsgDisplayStreamDataSized  uint16 = 316
	MsgDisplayMonitorsConfig   uint16 = 317
	MsgDisplayDrawComposite    uint16 = 318
)

// Display channel client messages.
const (
	MsgcDisplayInit uint16 = 101 // SPICE_MSGC_DISPLAY_INIT
)

// DisplayInitBodySize is sizeof(SpiceMsgcDisplayInit): 14 bytes.
// pixmap_cache_id u8 + pixmap_cache_size i64 + glz_dictionary_id u8 + glz_dictionary_window_size i32.
const DisplayInitBodySize = 14

// Video codec types (SpiceVideoCodecType).
const (
	VideoCodecMJPEG uint8 = 1 // SPICE_VIDEO_CODEC_TYPE_MJPEG
	VideoCodecVP8   uint8 = 2
	VideoCodecH264  uint8 = 3
	VideoCodecVP9   uint8 = 4
	VideoCodecH265  uint8 = 5
)

// Stream flags (SpiceStreamFlags).
const (
	StreamFlagTopDown uint8 = 1 << 0 // SPICE_STREAM_FLAGS_TOP_DOWN
)

// StreamCreateFixedSize is the fixed prefix of SpiceMsgDisplayStreamCreate
// before Clip: surface_id u32 + id u32 + flags u8 + codec u8 + stamp u64 +
// stream_w/h u32×2 + src_w/h u32×2 + dest Rect 16 = 50.
const StreamCreateFixedSize = 50

// StreamDataHeaderSize is sizeof(SpiceStreamDataHeader): id u32 + multi_media_time u32.
const StreamDataHeaderSize = 8

// Image types (SpiceImageType).
const (
	ImageTypeBitmap            uint8 = 0
	ImageTypeQuic              uint8 = 1
	ImageTypeLZPLT             uint8 = 100
	ImageTypeLZRGB             uint8 = 101
	ImageTypeGLZRGB            uint8 = 102
	ImageTypeFromCache         uint8 = 103
	ImageTypeSurface           uint8 = 104
	ImageTypeJPEG              uint8 = 105
	ImageTypeFromCacheLossless uint8 = 106
	ImageTypeZlibGLZRGB        uint8 = 107
	ImageTypeJPEGAlpha         uint8 = 108
)

// Bitmap formats (SpiceBitmapFmt).
const (
	BitmapFmtInvalid uint8 = 0
	BitmapFmt1BitLE  uint8 = 1
	BitmapFmt1BitBE  uint8 = 2
	BitmapFmt4BitLE  uint8 = 3
	BitmapFmt4BitBE  uint8 = 4
	BitmapFmt8Bit    uint8 = 5
	BitmapFmt16Bit   uint8 = 6
	BitmapFmt24Bit   uint8 = 7
	BitmapFmt32Bit   uint8 = 8
	BitmapFmtRGBA    uint8 = 9
	BitmapFmt8BitA   uint8 = 10
)

// Bitmap flags (SpiceBitmapFlags).
const (
	BitmapFlagPalCacheMe   uint8 = 1 << 0
	BitmapFlagPalFromCache uint8 = 1 << 1
	BitmapFlagTopDown      uint8 = 1 << 2
)

// Surface formats (SpiceSurfaceFmt).
const (
	SurfaceFmtInvalid uint32 = 0
	SurfaceFmt1A      uint32 = 1
	SurfaceFmt8A      uint32 = 8
	SurfaceFmt16555   uint32 = 16
	SurfaceFmt32xRGB  uint32 = 32
	SurfaceFmt16565   uint32 = 80
	SurfaceFmt32ARGB  uint32 = 96
)

// Surface flags (SpiceSurfaceFlags).
const (
	SurfaceFlagPrimary uint32 = 1 << 0
)

// Clip types (SpiceClipType).
const (
	ClipTypeNone  uint8 = 0
	ClipTypeRects uint8 = 1
)

// Brush types (SpiceBrushType).
const (
	BrushTypeNone    uint8 = 0
	BrushTypeSolid   uint8 = 1
	BrushTypePattern uint8 = 2
)

// ROP descriptors (SpiceRopd) — Phase 1 uses OP_PUT.
const (
	RopdInversSrc   uint16 = 1 << 0
	RopdInversBrush uint16 = 1 << 1
	RopdInversDest  uint16 = 1 << 2
	RopdOpPut       uint16 = 1 << 3
	RopdOpOr        uint16 = 1 << 4
	RopdOpAnd       uint16 = 1 << 5
	RopdOpXor       uint16 = 1 << 6
	RopdOpBlackness uint16 = 1 << 7
	RopdOpWhiteness uint16 = 1 << 8
	RopdOpInvers    uint16 = 1 << 9
	RopdInversRes   uint16 = 1 << 10
)

// Surface size bounds (design: max side 8192; max bytes 64 MiB per surface).
// Compositor also enforces MaxSurfaces and MaxTotalSurfaceBytes across all surfaces.
const (
	MaxSurfaceSide       = 8192
	MaxSurfaceBytes      = 64 << 20  // 64 MiB per surface
	MaxSurfaces          = 32        // max concurrent surfaces in one compositor
	MaxTotalSurfaceBytes = 128 << 20 // 128 MiB total pixel memory across surfaces
)

// SpiceImageDescSize is sizeof(SpiceImageDescriptor): id u64 + type u8 + flags u8 + w u32 + h u32.
const SpiceImageDescSize = 18

// SurfaceCreateSize is sizeof(SpiceMsgSurfaceCreate): 5 × uint32.
const SurfaceCreateSize = 20

// Cursor channel server messages (spice/enums.h; start at MsgFirstAvail).
// Wire layout from spice-common spice.proto / generated demarshallers.
const (
	MsgCursorInit     uint16 = 101 // SPICE_MSG_CURSOR_INIT
	MsgCursorReset    uint16 = 102 // SPICE_MSG_CURSOR_RESET
	MsgCursorSet      uint16 = 103 // SPICE_MSG_CURSOR_SET
	MsgCursorMove     uint16 = 104 // SPICE_MSG_CURSOR_MOVE
	MsgCursorHide     uint16 = 105 // SPICE_MSG_CURSOR_HIDE
	MsgCursorTrail    uint16 = 106
	MsgCursorInvalOne uint16 = 107
	MsgCursorInvalAll uint16 = 108
)

// Cursor types (SpiceCursorType) — wire is enum8 in CursorHeader.
const (
	CursorTypeAlpha   uint8 = 0
	CursorTypeMono    uint8 = 1
	CursorTypeColor4  uint8 = 2
	CursorTypeColor8  uint8 = 3
	CursorTypeColor16 uint8 = 4
	CursorTypeColor24 uint8 = 5
	CursorTypeColor32 uint8 = 6
)

// Cursor flags (SpiceCursorFlags) — flags16 on the wire.
const (
	CursorFlagNone      uint16 = 1 << 0 // no cursor shape
	CursorFlagCacheMe   uint16 = 1 << 1
	CursorFlagFromCache uint16 = 1 << 2
)

// CursorHeaderWireSize is sizeof(SpiceCursorHeader) on the wire:
// unique u64 + type u8 + width/height/hot_x/hot_y u16×4 = 17.
const CursorHeaderWireSize = 17

// MaxCursorSide / MaxCursorPixels guard best-effort shape decode.
const (
	MaxCursorSide   = 256
	MaxCursorPixels = MaxCursorSide * MaxCursorSide
)
