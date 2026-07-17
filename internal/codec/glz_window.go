// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"sync"
)

// glzEntry is one decoded GLZ image stored in the dictionary window.
// Pix is stream-order RGBA (not necessarily top-down display order).
type glzEntry struct {
	id          uint64
	winHeadDist uint32
	width       int
	height      int
	pix         []byte // len == width*height*4
}

// GLZWindow is the client-side Global LZ dictionary.
// Matches may reference earlier images by (image_id - image_dist).
// Entries are retained until the encoder's win_head_dist or maxBytes forces eviction.
type GLZWindow struct {
	mu       sync.Mutex
	maxBytes int
	curBytes int
	byID     map[uint64]*glzEntry
	// oldest is the smallest id still possibly retained (spice-gtk style).
	oldest uint64
	// hasOldest tracks whether oldest has been initialized from a real id.
	hasOldest bool
}

// NewGLZWindow creates a dictionary with a soft byte budget.
// maxBytes <= 0 defaults to protocol DisplayGlzWindowBytes (16 MiB).
func NewGLZWindow(maxBytes int) *GLZWindow {
	if maxBytes <= 0 {
		maxBytes = 16 << 20
	}
	return &GLZWindow{
		maxBytes: maxBytes,
		byID:     make(map[uint64]*glzEntry),
	}
}

// Reset clears all dictionary entries.
func (w *GLZWindow) Reset() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.byID = make(map[uint64]*glzEntry)
	w.curBytes = 0
	w.oldest = 0
	w.hasOldest = false
}

// Len returns the number of images currently in the window (for tests).
func (w *GLZWindow) Len() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.byID)
}

// Bytes returns approximate retained pixel bytes (for tests).
func (w *GLZWindow) Bytes() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.curBytes
}

func (w *GLZWindow) addLocked(e *glzEntry) {
	if e == nil || e.pix == nil {
		return
	}
	if old, ok := w.byID[e.id]; ok {
		w.curBytes -= len(old.pix)
		delete(w.byID, e.id)
	}
	w.byID[e.id] = e
	w.curBytes += len(e.pix)
	if !w.hasOldest || e.id < w.oldest {
		w.oldest = e.id
		w.hasOldest = true
	}
	// Encoder window head: free ids strictly below (id - win_head_dist).
	// When win_head_dist > id, oldest target is 0 (keep everything from start).
	var releaseBefore uint64
	if e.winHeadDist > 0 && e.id >= uint64(e.winHeadDist) {
		releaseBefore = e.id - uint64(e.winHeadDist)
	}
	w.releaseBeforeLocked(releaseBefore)
	w.enforceMaxBytesLocked()
}

// releaseBeforeLocked frees entries with id < releaseBefore.
func (w *GLZWindow) releaseBeforeLocked(releaseBefore uint64) {
	if !w.hasOldest {
		return
	}
	for w.oldest < releaseBefore {
		if e, ok := w.byID[w.oldest]; ok {
			w.curBytes -= len(e.pix)
			delete(w.byID, w.oldest)
		}
		w.oldest++
	}
	if len(w.byID) == 0 {
		w.hasOldest = false
		w.oldest = 0
		w.curBytes = 0
	}
}

func (w *GLZWindow) enforceMaxBytesLocked() {
	if w.maxBytes <= 0 || w.curBytes <= w.maxBytes {
		return
	}
	// Evict from oldest id upward until under budget.
	for w.curBytes > w.maxBytes && len(w.byID) > 0 {
		if !w.hasOldest {
			// Recover oldest from map if tracking was lost.
			var minID uint64
			first := true
			for id := range w.byID {
				if first || id < minID {
					minID = id
					first = false
				}
			}
			if first {
				return
			}
			w.oldest = minID
			w.hasOldest = true
		}
		if e, ok := w.byID[w.oldest]; ok {
			w.curBytes -= len(e.pix)
			delete(w.byID, w.oldest)
		}
		w.oldest++
		// Skip gaps (out-of-order / already freed).
		for w.hasOldest && len(w.byID) > 0 {
			if _, ok := w.byID[w.oldest]; ok {
				break
			}
			// If oldest advanced past everything, recompute.
			var minID uint64
			first := true
			for id := range w.byID {
				if first || id < minID {
					minID = id
					first = false
				}
			}
			if first {
				w.hasOldest = false
				w.oldest = 0
				break
			}
			if w.oldest < minID {
				w.oldest = minID
			} else if _, ok := w.byID[w.oldest]; !ok {
				w.oldest = minID
			}
			break
		}
	}
	if len(w.byID) == 0 {
		w.hasOldest = false
		w.oldest = 0
		w.curBytes = 0
	}
}

// bits returns a pointer-like slice into a window image's pixels at pixel offset.
// The returned slice aliases the dictionary; callers must only read it during decode.
func (w *GLZWindow) bits(imageID uint64, dist uint32, pixelOfs int) ([]byte, error) {
	if w == nil {
		return nil, errGLZ("missing dictionary for cross-image ref")
	}
	if dist == 0 {
		return nil, errGLZ("bits called with dist 0")
	}
	if uint64(dist) > imageID {
		return nil, errGLZ("image_dist %d > image_id %d", dist, imageID)
	}
	refID := imageID - uint64(dist)
	w.mu.Lock()
	e := w.byID[refID]
	w.mu.Unlock()
	if e == nil {
		return nil, errGLZ("dictionary miss id=%d (from %d dist %d)", refID, imageID, dist)
	}
	nPix := e.width * e.height
	if pixelOfs < 0 || pixelOfs >= nPix {
		return nil, errGLZ("ref pixel offset %d out of range for id=%d (%d pixels)",
			pixelOfs, refID, nPix)
	}
	return e.pix[pixelOfs*4:], nil
}

// lookupPix returns the full pixel buffer for id (for tests).
func (w *GLZWindow) lookupPix(id uint64) []byte {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	e := w.byID[id]
	if e == nil {
		return nil
	}
	return e.pix
}
