// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin && !windows

package ui

// forceHostCursorVisible is a no-op on platforms without a dedicated
// show-cursor API. Cursor still updates on the next mouse move (Fyne/GLFW).
func forceHostCursorVisible() {}

// forceHostCursorHidden is a no-op; Cursorable.Cursor() still returns HiddenCursor.
func forceHostCursorHidden() {}
