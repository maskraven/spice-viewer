// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import "sync"

// Grab tracks keyboard/mouse grab state for the session window.
//
// Click-to-grab: pointer press while ungrabbbed enters grab.
// release-cursor hotkey (and Escape as a safety fallback when configured)
// leaves grab.
type Grab struct {
	mu     sync.Mutex
	active bool
}

// Active reports whether input is currently grabbed.
func (g *Grab) Active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active
}

// Grab enters grab mode. Idempotent.
func (g *Grab) Grab() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active = true
}

// Release leaves grab mode. Idempotent. Called by release-cursor hotkey.
func (g *Grab) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active = false
}

// Toggle flips grab state; returns the new active value.
func (g *Grab) Toggle() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active = !g.active
	return g.active
}
