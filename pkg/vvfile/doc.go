// Package vvfile parses virt-viewer connection files (.vv).
//
// This is the only public .vv API. cmd/spice-viewer and internal/ui
// should use this package rather than internal parsers.
//
// Parse reads an io.Reader. ParseFile opens a path and, when
// ParseOptions.DeleteIfRequested is true and the file sets
// delete-this-file=1, removes the path after secrets are copied.
// Zero-value ParseOptions never deletes (safe library default).
//
// Password length is capped at MaxPasswordLen (60,
// SPICE_MAX_PASSWORD_LENGTH). The RSA-OAEP-SHA1 link budget is 85
// bytes including NUL, but spice-gtk/QEMU/PVE and ticket encrypt use 60.
//
// Import rules: stdlib only; no internal/* or UI imports.
package vvfile
