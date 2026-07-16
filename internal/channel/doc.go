// Package channel implements SPICE channel types (main, display, inputs,
// cursor, and later playback/record/usbredir/port/webdav).
//
// Phase 1:
//   - Open-policy helpers used by session.Channel manager (PR 07)
//   - Inputs channel: mouse modes, PC XT scancodes, UI inject API (PR 10)
//
// Display draw ops live in later display PRs.
//
// QEMU typing smoke / live SPICE inputs integration is intentionally deferred
// (//go:build integration or session/UI smoke in a later PR). Unit tests cover
// wire encoding, mode switch, flood coalesce+ACK flush, and concurrent inject
// framing without a guest.
//
// Import rules: no UI imports.
package channel
