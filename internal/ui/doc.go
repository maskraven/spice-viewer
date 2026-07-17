// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

// Package ui is the Fyne GUI surface for remote-viewer.
//
// It implements spice.DisplayDriver (Present / desktop size / invalidate),
// keyboard/mouse grab, virt-viewer hotkeys, and daily-use controls:
//
//   - Main menu: File, View, Send Keys, Help
//   - Send Keys: Ctrl+Alt+Del, Ctrl+Alt+Fn, Super, Alt+Tab, Type text… (US QWERTY)
//   - Toolbar: CAD, ungrab, fullscreen, copy, paste, type text
//   - Status bar: compact single line — title · grab · mouse mode · agent
//   - Clipboard: toolbar Copy/Paste via vdagent; TypeText fallback when agent offline
//   - secure-attention chord → guest Ctrl+Alt+Del (not the local chord keys)
//   - release-cursor → ungrab; toggle-fullscreen → window fullscreen
//
// Import rules: may import pkg/spice, pkg/vvfile, and internal/ux.
// cmd/remote-viewer may import this package.
package ui
