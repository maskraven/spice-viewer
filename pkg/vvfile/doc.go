// Package vvfile parses virt-viewer connection files (.vv).
//
// This is the only public .vv API. cmd/remote-viewer and internal/ui
// should use this package rather than internal parsers.
//
// Parse reads an io.Reader. ParseFile opens a path and, when
// ParseOptions.DeleteIfRequested is true and the file sets
// delete-this-file=1, removes the path after secrets are copied.
// Zero-value ParseOptions never deletes (safe library default).
//
// Import rules: stdlib only; no internal/* or UI imports.
package vvfile
