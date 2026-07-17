// Package codec decodes SPICE image and video payloads (raw, LZ, Quic,
// JPEG / JPEG_ALPHA, and MJPEG stream frames; later GLZ/H.264 subject to
// license decisions).
//
// Supported today:
//   - SPICE_IMAGE_TYPE_BITMAP (raw 32BIT / RGBA)
//   - SPICE_IMAGE_TYPE_LZ_RGB (RGB16/24/32/RGBA FastLZ-derived stream)
//   - SPICE_IMAGE_TYPE_QUIC (RGB24 / RGB32)
//   - SPICE_IMAGE_TYPE_JPEG
//   - SPICE_IMAGE_TYPE_JPEG_ALPHA (JPEG + LZ XXXA alpha)
//   - DecodeJPEGBytes for MJPEG stream frames
//
// Import rules: no UI imports.
package codec
