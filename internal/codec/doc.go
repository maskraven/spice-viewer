// Package codec decodes SPICE image and video payloads (raw, LZ, Quic,
// JPEG / JPEG_ALPHA, GLZ, MJPEG streams, and H.264 via internal/codec/h264).
//
// Supported today:
//   - SPICE_IMAGE_TYPE_BITMAP (raw 32BIT / RGBA)
//   - SPICE_IMAGE_TYPE_LZ_RGB (RGB16/24/32/RGBA FastLZ-derived stream)
//   - SPICE_IMAGE_TYPE_GLZ_RGB / ZLIB_GLZ_RGB via GLZWindow (stateful dictionary)
//   - SPICE_IMAGE_TYPE_QUIC (RGB24 / RGB32)
//   - SPICE_IMAGE_TYPE_JPEG
//   - SPICE_IMAGE_TYPE_JPEG_ALPHA (JPEG + LZ XXXA alpha)
//   - DecodeJPEGBytes for MJPEG stream frames
//   - H.264 streams: see package h264 (OS decoders / user FFmpeg on Linux)
//
// DecodeSpiceImage does not decode GLZ (needs a shared GLZWindow). The display
// channel calls GLZWindow.Decode after detecting GLZ image types.
//
// Import rules: no UI imports.
package codec
