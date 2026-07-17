// Package channel implements SPICE channel types (main, display, inputs,
// cursor, playback, and later record/usbredir/port/webdav).
//
// Phase 1:
//   - Open-policy helpers used by session.Channel manager (PR 07)
//   - Display channel reader: allowlist (mode/mark/reset/draw_fill/draw_copy/
//     surface create|destroy), wire decode, compositor (PR 08)
//   - Inputs channel: mouse modes, PC XT scancodes, UI inject API (PR 10)
//   - Best-effort cursor channel: set/hide/move/reset, non-fatal decode (PR 11)
//
// Phase 2:
//   - Best-effort playback channel: MODE/START/STOP/DATA (RAW S16LE PCM),
//     VOLUME/MUTE; open/runtime failures are non-fatal (PR 19)
//
// Cursor and playback open and runtime decode failures are non-fatal: session
// continues (server-drawn cursor in the framebuffer; silent audio if degraded).
//
// QEMU typing smoke / live SPICE inputs integration is intentionally deferred
// (//go:build integration or session/UI smoke in a later PR). Unit tests cover
// wire encoding, mode switch, flood coalesce+ACK flush, and concurrent inject
// framing without a guest.
//
// Import rules: no UI imports.
package channel
