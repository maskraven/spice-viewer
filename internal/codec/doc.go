// Package codec decodes SPICE image and video payloads (raw, LZ, Quic,
// JPEG, and later GLZ/MJPEG/H.264 subject to license decisions).
//
// Phase 1 (PR 08): raw bitmap decoder only (SPICE_IMAGE_TYPE_BITMAP).
// LZ lands in a follow-up PR.
//
// Import rules: no UI imports.
package codec
