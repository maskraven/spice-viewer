// Package channel implements SPICE channel types (main, display, inputs,
// cursor, and later playback/record/usbredir/port/webdav).
//
// Phase 1: open-policy helpers (PR 07) and best-effort cursor channel (PR 11).
// Display draw ops and inputs scancodes are intentionally not implemented yet
// in this package on the PR 11 branch (see sibling PRs).
//
// Cursor open and runtime decode failures are non-fatal: session continues with
// server-drawn cursor in the framebuffer.
//
// Import rules: no UI imports.
package channel
