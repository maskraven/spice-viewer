// Package display implements the compositor and surface pipeline that
// turns decoded channel updates into frames for UI drivers.
//
// Phase 1 (PR 08): multi-surface store, bounds checks, solid FILL and
// raw COPY blits, Driver sink (Present / Invalidate), NullDriver hash.
//
// Import rules: no UI imports.
package display
