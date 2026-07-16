// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

// Package ui is the Fyne GUI surface for remote-viewer.
//
// It implements spice.DisplayDriver (Present / desktop size / invalidate),
// keyboard grab, and virt-viewer hotkey semantics:
//
//   - secure-attention chord → inject guest Ctrl+Alt+Del (CAD), not the chord keys
//   - release-cursor → ungrab
//   - toggle-fullscreen → window fullscreen toggle
//
// Import rules: may import pkg/spice, pkg/vvfile, and internal/ux.
// cmd/remote-viewer may import this package.
package ui
