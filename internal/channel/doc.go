// Package channel implements SPICE channel types (main, display, inputs,
// cursor, and later playback/record/usbredir/port/webdav).
//
// Phase 1 PR 07: open-policy helpers used by session.Channel manager.
// Phase 1 PR 08: display channel reader — allowlist (mode/mark/reset/
// draw_fill/draw_copy/surface create|destroy), wire decode, compositor.
//
// Import rules: no UI imports.
package channel
