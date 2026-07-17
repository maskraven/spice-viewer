// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidFynePreferencesJSON(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"   \n", false},
		{"{", false},
		{"{}", true},
		{"{\n}\n", true},
		{`{"pointer_mode":"hidden"}`, true},
		{`{"a":1}`, true},
		{"null", false},
		{"[]", false},
	}
	for _, c := range cases {
		if got := validFynePreferencesJSON([]byte(c.in)); got != c.want {
			t.Errorf("validFynePreferencesJSON(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestSanitizeFynePreferencesRepairsEmpty(t *testing.T) {
	// Point HOME (or equivalent) at a temp dir so we do not touch real prefs.
	tmp := t.TempDir()
	switch {
	case os.Getenv("HOME") != "" || true:
		t.Setenv("HOME", tmp)
		// XDG for linux path when GOOS is linux in CI; harmless on darwin.
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
		t.Setenv("APPDATA", filepath.Join(tmp, "AppData", "Roaming"))
	}

	root := fynePreferencesRoot()
	if root == "" {
		t.Fatal("empty fynePreferencesRoot")
	}
	appID := "com.maskraven.spice-viewer-test"
	dir := filepath.Join(root, appID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "preferences.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Orphan PID-style dir.
	orphan := filepath.Join(root, appID+".p99999")
	if err := os.MkdirAll(filepath.Join(orphan), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "preferences.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	sanitizeFynePreferences(appID)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !validFynePreferencesJSON(data) {
		t.Fatalf("prefs still invalid: %q", data)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan PID prefs dir still present: %v", err)
	}
}
