// Package vvfile parses virt-viewer connection files (.vv).
//
// This is the only public .vv API. cmd/remote-viewer and internal/ui
// should use this package rather than internal parsers.
//
// Import rules: stdlib only ideally; no UI imports.
package vvfile
