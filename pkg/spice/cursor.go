// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice

import "github.com/maskraven/spice-viewer/internal/channel"

// CursorDriver receives best-effort cursor shape and position updates.
//
// UI backends implement this. Decode failures never kill the session.
// Nil CursorDriver on ConnectConfig.Drivers means shapes are discarded.
type CursorDriver interface {
	// SetCursor installs a client-side cursor shape (RGBA8888, stride = w*4).
	// The rgba slice must be treated as immutable after return.
	SetCursor(hotX, hotY int, rgba []byte, w, h int)
	// MoveCursor moves the hotspot to guest display coordinates.
	MoveCursor(x, y int)
	// HideCursor hides the client-side cursor.
	HideCursor()
	// ResetCursor restores the default cursor and clears shape state.
	ResetCursor()
}

// Compile-time check: channel.NullDriver implements CursorDriver.
var _ CursorDriver = (*channel.NullDriver)(nil)

// NullCursorDriver is a headless CursorDriver for tests.
// It is an alias of channel.NullDriver.
type NullCursorDriver = channel.NullDriver

// NewNullCursorDriver returns a NullCursorDriver.
func NewNullCursorDriver() *NullCursorDriver {
	return channel.NewNullDriver()
}

// asCursorDriver adapts a public CursorDriver to channel.Driver.
func asCursorDriver(d CursorDriver) channel.Driver {
	if d == nil {
		return nil
	}
	// channel.NullDriver and any type that already implements channel.Driver.
	if cd, ok := d.(channel.Driver); ok {
		return cd
	}
	return cursorDriverAdapter{d}
}

type cursorDriverAdapter struct {
	d CursorDriver
}

func (a cursorDriverAdapter) SetCursor(hotX, hotY int, rgba []byte, w, h int) {
	a.d.SetCursor(hotX, hotY, rgba, w, h)
}

func (a cursorDriverAdapter) MoveCursor(x, y int) { a.d.MoveCursor(x, y) }
func (a cursorDriverAdapter) HideCursor()         { a.d.HideCursor() }
func (a cursorDriverAdapter) ResetCursor()        { a.d.ResetCursor() }
