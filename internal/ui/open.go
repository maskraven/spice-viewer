// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ncruces/zenity"
)

// NormalizePathArg converts desktop/LaunchServices-style path args into a
// local filesystem path. Handles file:// URIs (common on Linux) and cleans
// Windows/macOS paths. Empty input returns empty.
func NormalizePathArg(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Strip optional surrounding quotes (some shells / launchers).
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
		}
	}
	if strings.HasPrefix(strings.ToLower(s), "file:") {
		u, err := url.Parse(s)
		if err == nil {
			// url.Path is unescaped; on Windows Path may be /C:/Users/...
			p := u.Path
			if runtime.GOOS == "windows" && strings.HasPrefix(p, "/") && len(p) >= 3 && p[2] == ':' {
				p = p[1:] // /C:/... → C:/...
			}
			if p != "" {
				s = p
			} else if u.Opaque != "" {
				s = u.Opaque
			}
			// Host for UNC: file://server/share → //server/share
			if runtime.GOOS == "windows" && u.Host != "" {
				s = `\\` + u.Host + strings.ReplaceAll(p, "/", `\`)
			}
		}
	}
	return filepath.Clean(s)
}

// IsConnectionFile reports whether path looks like a virt-viewer .vv file.
func IsConnectionFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".vv")
}

// PickConnectionFile shows the OS native file chooser for a .vv connection file.
//
// Platform notes:
//   - macOS: no extension filter (custom UTI greys out *.vv filters in chooseFile)
//   - Windows: COM common-item dialog with *.vv filter (works with Explorer associations)
//   - Linux: zenity/kdialog/portal when available; *.vv filter when the helper supports it
//
// Returns empty path and nil error when the user cancels.
func PickConnectionFile() (string, error) {
	opts := []zenity.Option{
		zenity.Title("Open SPICE connection"),
	}
	// Windows / Linux: extension filters work and help users.
	// macOS: ofType filters often hide custom-UTI .vv documents — leave unfiltered.
	if runtime.GOOS != "darwin" {
		opts = append(opts, zenity.FileFilters{
			{
				Name:     "virt-viewer connection (*.vv)",
				Patterns: []string{"*.vv"},
				CaseFold: true,
			},
			{
				Name:     "All files",
				Patterns: []string{"*"},
			},
		})
	}

	path, err := zenity.SelectFile(opts...)
	if err != nil {
		if errors.Is(err, zenity.ErrCanceled) {
			return "", nil
		}
		// Linux without zenity/kdialog: clear recovery path.
		if runtime.GOOS == "linux" {
			return "", fmt.Errorf("ui: native file dialog unavailable (%v); install zenity or pass a .vv path on the command line", err)
		}
		return "", err
	}
	path = NormalizePathArg(path)
	if path == "" {
		return "", nil
	}
	if !IsConnectionFile(path) {
		return "", fmt.Errorf("ui: please choose a .vv connection file (got %s)", filepath.Base(path))
	}
	return path, nil
}
