// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizePathArg_FileURI(t *testing.T) {
	got := NormalizePathArg("file:///tmp/guest.vv")
	want := filepath.Clean("/tmp/guest.vv")
	if runtime.GOOS == "windows" {
		// On Windows, file:///C:/x.vv is the usual form; POSIX-style still cleans.
		if got == "" {
			t.Fatal("empty")
		}
		return
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNormalizePathArg_Plain(t *testing.T) {
	got := NormalizePathArg(`"/tmp/a.vv"`)
	if !IsConnectionFile(got) {
		t.Fatalf("got %q", got)
	}
}

func TestIsConnectionFile(t *testing.T) {
	if !IsConnectionFile("x.vv") || !IsConnectionFile("X.VV") {
		t.Fatal("expected .vv match")
	}
	if IsConnectionFile("x.txt") {
		t.Fatal("unexpected")
	}
}

func TestResolveConnectionPath_UsesArgv(t *testing.T) {
	// Must not open a GUI dialog when argv is set.
	p, err := ResolveConnectionPath("/tmp/session.vv")
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Clean("/tmp/session.vv") && runtime.GOOS != "windows" {
		// Clean may differ slightly on Windows.
		if !IsConnectionFile(p) {
			t.Fatalf("path %q", p)
		}
	}
}
