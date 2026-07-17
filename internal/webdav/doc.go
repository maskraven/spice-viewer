// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

// Package webdav holds optional framing helpers for the SPICE WebDAV share
// channel (Phase 3 scaffold).
//
// The channel wire path is Port + SpiceVMC (see internal/channel/webdav.go and
// internal/protocol). Full phodav / spice-webdavd share UX is not implemented
// here; types exist so a future mux can parse client_id-framed payloads without
// inventing new packages.
//
// Import rules: no UI imports; no network I/O in this package.
package webdav
