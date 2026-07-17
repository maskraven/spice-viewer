// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package ui

// ResolveConnectionPath returns a usable .vv path for GUI launch.
//
// Windows / Linux best practice:
//   - Double-click / "Open with" passes the path as argv (or file:// URI on
//     some Linux DEs) — use that and never show a picker.
//   - Empty launch (Start Menu / .desktop without %f) → native file chooser.
func ResolveConnectionPath(argvPath string) (string, error) {
	if p := NormalizePathArg(argvPath); p != "" {
		return p, nil
	}
	return PickConnectionFile()
}
