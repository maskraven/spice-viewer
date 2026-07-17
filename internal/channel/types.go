// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package channel

import "github.com/maskraven/virt-viewer/internal/protocol"

// Phase-1 channel type aliases (spice ChannelType values).
const (
	TypeMain     = protocol.ChannelMain
	TypeDisplay  = protocol.ChannelDisplay
	TypeInputs   = protocol.ChannelInputs
	TypeCursor   = protocol.ChannelCursor
	TypePlayback = protocol.ChannelPlayback
	TypeRecord   = protocol.ChannelRecord
	TypeUSBRedir = protocol.ChannelUSBRedir
	TypePort     = protocol.ChannelPort
	TypeWebDAV   = protocol.ChannelWebDAV
)

// OpenPolicy describes how session treats a channel type during open.
type OpenPolicy int

const (
	// PolicyNever means Phase 1 must not open this channel type.
	PolicyNever OpenPolicy = iota
	// PolicyRequired means open failure is session-fatal.
	PolicyRequired
	// PolicyBestEffort means open failure degrades; session continues.
	PolicyBestEffort
)

// PolicyFor returns the open policy for a SPICE channel type.
//
// Required: display, inputs.
// Best-effort: cursor, playback (Phase 2); record, usbredir, webdav (Phase 3).
// Main is opened via DialMain, not the child open path.
// Port (non-WebDAV) remains PolicyNever until a dedicated consumer lands.
func PolicyFor(channelType uint8) OpenPolicy {
	switch channelType {
	case protocol.ChannelDisplay, protocol.ChannelInputs:
		return PolicyRequired
	case protocol.ChannelCursor, protocol.ChannelPlayback,
		protocol.ChannelRecord, protocol.ChannelUSBRedir, protocol.ChannelWebDAV:
		return PolicyBestEffort
	case protocol.ChannelMain:
		// Main is opened separately (DialMain), not via child open path.
		return PolicyNever
	default:
		return PolicyNever
	}
}

// IsPhase1Open reports whether session should attempt to open this channel type
// after CHANNELS_LIST (required or best-effort). Named for historical Phase-1
// use; Phase 2/3 also open playback/record/usbredir/webdav as best-effort via
// the same helper.
func IsPhase1Open(channelType uint8) bool {
	p := PolicyFor(channelType)
	return p == PolicyRequired || p == PolicyBestEffort
}

// IsFatalOpen reports whether a failed open of this type is session-fatal.
func IsFatalOpen(channelType uint8) bool {
	return PolicyFor(channelType) == PolicyRequired
}
