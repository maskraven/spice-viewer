// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

// Package agent implements the SPICE guest agent protocol (vdagent) carried
// over the main channel as AGENT_DATA chunks.
//
// Phase 2: capability announce, clipboard text, monitors config (resolution).
// Import rules: no UI toolkit imports.
package agent
