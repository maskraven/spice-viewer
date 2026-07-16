// Package codec decodes SPICE image and video payloads (raw, LZ, Quic,
// JPEG, and later GLZ/MJPEG/H.264 subject to license decisions).
//
// Supported today:
//   - SPICE_IMAGE_TYPE_BITMAP (raw 32BIT / RGBA)
//   - SPICE_IMAGE_TYPE_LZ_RGB (RGB16/24/32/RGBA FastLZ-derived stream)
//
// Import rules: no UI imports.
package codec
