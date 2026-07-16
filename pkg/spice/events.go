// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice

// EventType classifies client lifecycle notifications.
type EventType int

const (
	// EventConnected is emitted once after main+children are open and run loops start.
	EventConnected EventType = iota + 1
	// EventDisconnected is emitted when the session ends (user Close or fatal drop).
	// Err may carry a classified transport/ticket error; nil means clean close.
	EventDisconnected
	// EventError reports a non-fatal or pre-disconnect error (e.g. cursor degrade).
	// Fatal channel failures also emit EventDisconnected.
	EventError
)

// Event is a lifecycle notification delivered on Client.Events.
//
// Channel is buffered; slow consumers may drop only if the buffer fills
// (emit is best-effort non-blocking for Error; Connected/Disconnected wait briefly).
type Event struct {
	Type EventType
	Err  error // optional; set for Disconnected (fatal) and Error
}

func (t EventType) String() string {
	switch t {
	case EventConnected:
		return "connected"
	case EventDisconnected:
		return "disconnected"
	case EventError:
		return "error"
	default:
		return "unknown"
	}
}
