// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice_test

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/maskraven/virt-viewer/pkg/spice"
	"github.com/maskraven/virt-viewer/pkg/vvfile"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestConnectConfigFromVV_ProxmoxSample(t *testing.T) {
	path := filepath.Join(repoRoot(t), "testdata", "vv", "proxmox_sample.vv")
	f, err := vvfile.ParseFile(path, vvfile.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := spice.ConnectConfigFromVV(f)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Host != f.Host {
		t.Errorf("Host: got %q want %q", cfg.Host, f.Host)
	}
	if cfg.TLSPort != f.TLSPort {
		t.Errorf("TLSPort: got %d want %d", cfg.TLSPort, f.TLSPort)
	}
	if cfg.Port != f.Port {
		t.Errorf("Port: got %d want %d", cfg.Port, f.Port)
	}
	if cfg.HostSubject != f.HostSubject {
		t.Errorf("HostSubject: got %q want %q", cfg.HostSubject, f.HostSubject)
	}
	if cfg.Title != f.Title {
		t.Errorf("Title: got %q want %q", cfg.Title, f.Title)
	}
	if cfg.Fullscreen != f.Fullscreen {
		t.Errorf("Fullscreen: got %v want %v", cfg.Fullscreen, f.Fullscreen)
	}
	if cfg.Hotkeys.SecureAttention != f.SecureAttention {
		t.Errorf("SecureAttention: got %q want %q", cfg.Hotkeys.SecureAttention, f.SecureAttention)
	}
	if cfg.Hotkeys.ReleaseCursor != f.ReleaseCursor {
		t.Errorf("ReleaseCursor: got %q want %q", cfg.Hotkeys.ReleaseCursor, f.ReleaseCursor)
	}
	if cfg.Hotkeys.ToggleFullscreen != f.ToggleFullscreen {
		t.Errorf("ToggleFullscreen: got %q want %q", cfg.Hotkeys.ToggleFullscreen, f.ToggleFullscreen)
	}
	if !bytes.Equal(cfg.Password, f.Password) {
		t.Errorf("Password mismatch")
	}
	if !bytes.Equal(cfg.CACertPEM, f.CA) {
		t.Errorf("CACertPEM mismatch")
	}
	if f.Proxy != nil {
		if cfg.ProxyURL != f.Proxy.String() {
			t.Errorf("ProxyURL: got %q want %q", cfg.ProxyURL, f.Proxy.String())
		}
	}
	// Drivers left zero.
	if cfg.Drivers.Display != nil || cfg.Drivers.Cursor != nil {
		t.Errorf("Drivers should be zero from FromVV")
	}
	// No auto-reconnect default.
	if cfg.AllowReconnect {
		t.Errorf("AllowReconnect should be false by default")
	}
}

func TestConnectConfigFromVV_FieldMappingSynthetic(t *testing.T) {
	proxy, err := url.Parse("http://proxy.example:3128")
	if err != nil {
		t.Fatal(err)
	}
	f := &vvfile.File{
		Type:             "spice",
		Host:             "pvespiceproxy:token",
		Port:             5900,
		TLSPort:          61000,
		Password:         []byte("ticket-abc"),
		Proxy:            proxy,
		CA:               []byte("-----BEGIN CERTIFICATE-----\nMII=\n-----END CERTIFICATE-----\n"),
		HostSubject:      "CN=pve.example.com",
		Title:            "VM 1",
		SecureAttention:  "Ctrl+Alt+Ins",
		ReleaseCursor:    "Ctrl+Alt+R",
		ToggleFullscreen: "Shift+F11",
		Fullscreen:       true,
	}

	cfg, err := spice.ConnectConfigFromVV(f)
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Host", cfg.Host, f.Host},
		{"Port", cfg.Port, f.Port},
		{"TLSPort", cfg.TLSPort, f.TLSPort},
		{"ProxyURL", cfg.ProxyURL, "http://proxy.example:3128"},
		{"HostSubject", cfg.HostSubject, f.HostSubject},
		{"Title", cfg.Title, f.Title},
		{"Fullscreen", cfg.Fullscreen, true},
		{"SecureAttention", cfg.Hotkeys.SecureAttention, f.SecureAttention},
		{"ReleaseCursor", cfg.Hotkeys.ReleaseCursor, f.ReleaseCursor},
		{"ToggleFullscreen", cfg.Hotkeys.ToggleFullscreen, f.ToggleFullscreen},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %#v want %#v", c.name, c.got, c.want)
		}
	}
	if !bytes.Equal(cfg.Password, []byte("ticket-abc")) {
		t.Errorf("Password: %q", cfg.Password)
	}
	if !bytes.Equal(cfg.CACertPEM, f.CA) {
		t.Errorf("CACertPEM mismatch")
	}
}

func TestConnectConfigFromVV_PasswordAndCADeepCopy(t *testing.T) {
	f := &vvfile.File{
		Type:     "spice",
		Host:     "h",
		TLSPort:  1,
		Password: []byte("secret"),
		CA:       []byte("CA-BYTES"),
	}
	cfg, err := spice.ConnectConfigFromVV(f)
	if err != nil {
		t.Fatal(err)
	}

	// Mutate source file secrets; cfg must retain originals.
	f.Password[0] = 'X'
	f.CA[0] = 'Z'
	if string(cfg.Password) != "secret" {
		t.Fatalf("Password not deep-copied: %q", cfg.Password)
	}
	if string(cfg.CACertPEM) != "CA-BYTES" {
		t.Fatalf("CACertPEM not deep-copied: %q", cfg.CACertPEM)
	}

	// Mutate cfg; file already mutated independently (copies are distinct).
	cfg.Password[0] = 'Y'
	if f.Password[0] == 'Y' {
		t.Fatal("cfg.Password shares backing array with f.Password")
	}
}

func TestConnectConfigFromVV_NilFile(t *testing.T) {
	_, err := spice.ConnectConfigFromVV(nil)
	if err == nil {
		t.Fatal("expected error for nil file")
	}
}

func TestConnectConfigFromVV_NonSpiceType(t *testing.T) {
	f := &vvfile.File{
		Type:     "vnc",
		Host:     "h",
		Port:     5900,
		Password: []byte("p"),
	}
	_, err := spice.ConnectConfigFromVV(f)
	if err == nil {
		t.Fatal("expected error for non-spice type")
	}
}

func TestConnectConfigFromVV_EmptyProxy(t *testing.T) {
	f := &vvfile.File{
		Type:     "spice",
		Host:     "h",
		Port:     5900,
		Password: []byte("p"),
	}
	cfg, err := spice.ConnectConfigFromVV(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProxyURL != "" {
		t.Errorf("ProxyURL: got %q", cfg.ProxyURL)
	}
}

func TestConnectConfigFromVV_FixtureFileExists(t *testing.T) {
	// Sanity: sample fixture is present for mapping test above.
	path := filepath.Join(repoRoot(t), "testdata", "vv", "proxmox_sample.vv")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
