// Package channel implements SPICE channel types (main, display, inputs,
// cursor, and later playback/record/usbredir/port/webdav).
//
// Phase 1:
//   - Open-policy helpers used by session.Channel manager (PR 07)
//   - Display channel reader: allowlist (mode/mark/reset/draw_fill/draw_copy/
//     surface create|destroy), wire decode, compositor (PR 08)
//   - Inputs channel: mouse modes, PC XT scancodes, UI inject API (PR 10)
//   - Best-effort cursor channel: set/hide/move/reset, non-fatal decode (PR 11)
//
// Cursor open and runtime decode failures are non-fatal: session continues with
// server-drawn cursor in the framebuffer.
//
// QEMU typing smoke / live SPICE inputs integration is intentionally deferred
// (//go:build integration or session/UI smoke in a later PR). Unit tests cover
// wire encoding, mode switch, flood coalesce+ACK flush, and concurrent inject
// framing without a guest.
//
// Import rules: no UI imports.
package channel
