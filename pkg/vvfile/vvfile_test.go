// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package vvfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maskraven/virt-viewer/pkg/vvfile"
)

func TestParseProxmoxGoldenFixture(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "vv", "proxmox_sample.vv")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Ensure multi-colon host is present in golden file.
	if !strings.Contains(string(data), "pvespiceproxy:") {
		t.Fatal("fixture missing pvespiceproxy host")
	}

	f, err := vvfile.Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	wantHost := "pvespiceproxy:687d1ec6:10016:pve::dcc9e35662ef0b1233e12ac02880ea7851f9218e"
	if f.Host != wantHost {
		t.Errorf("Host = %q, want %q", f.Host, wantHost)
	}
	if strings.Count(f.Host, ":") < 4 {
		t.Errorf("Host should keep multi-colon Proxmox token, got %q", f.Host)
	}
	if f.Type != "spice" {
		t.Errorf("Type = %q", f.Type)
	}
	if f.TLSPort != 61002 {
		t.Errorf("TLSPort = %d", f.TLSPort)
	}
	if f.Port != 0 {
		t.Errorf("Port = %d, want 0", f.Port)
	}
	if string(f.Password) != "PVE:shortlivedticketvalue0123456789abcdef" {
		t.Errorf("Password = %q", f.Password)
	}
	if f.Proxy == nil || f.Proxy.String() != "http://proxmox.example.com:3128" {
		t.Errorf("Proxy = %v", f.Proxy)
	}
	if f.HostSubject != "OU=PVE Cluster Node,O=Proxmox Virtual Environment,CN=pve.example.com" {
		t.Errorf("HostSubject = %q", f.HostSubject)
	}
	if f.Title != "VM 10016 - debian-spice" {
		t.Errorf("Title = %q", f.Title)
	}
	if !f.DeleteThisFile {
		t.Error("DeleteThisFile want true")
	}
	if f.SecureAttention != "Ctrl+Alt+Ins" {
		t.Errorf("SecureAttention = %q", f.SecureAttention)
	}
	if f.ReleaseCursor != "Ctrl+Alt+R" {
		t.Errorf("ReleaseCursor = %q", f.ReleaseCursor)
	}
	if f.ToggleFullscreen != "Shift+F11" {
		t.Errorf("ToggleFullscreen = %q", f.ToggleFullscreen)
	}
	if f.Fullscreen {
		t.Error("Fullscreen want false")
	}

	// CA newlines unescaped from \n sequences.
	ca := string(f.CA)
	if !strings.Contains(ca, "-----BEGIN CERTIFICATE-----\n") {
		t.Errorf("CA missing unescaped BEGIN line, got %q", ca)
	}
	if strings.Contains(ca, `\n`) {
		t.Errorf("CA still contains literal \\n escapes: %q", ca)
	}
	if !strings.HasSuffix(strings.TrimRight(ca, "\n"), "-----END CERTIFICATE-----") {
		t.Errorf("CA END line missing, got %q", ca)
	}
}

func TestParseFileDeleteIfRequested(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.vv")
	content := minimalVV(t, map[string]string{
		"delete-this-file": "1",
		"password":         "ticket",
	})
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := vvfile.ParseFile(path, vvfile.ParseOptions{DeleteIfRequested: true})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if !f.Deleted {
		t.Errorf("Deleted = false, DeleteErr = %v", f.DeleteErr)
	}
	if f.DeleteErr != nil {
		t.Errorf("DeleteErr = %v", f.DeleteErr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should be removed, stat err = %v", err)
	}
	// Secrets still available after delete (no dial required).
	if string(f.Password) != "ticket" {
		t.Errorf("Password lost after delete: %q", f.Password)
	}
}

func TestParseFileZeroOptionsDoesNotDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keep.vv")
	content := minimalVV(t, map[string]string{
		"delete-this-file": "1",
		"password":         "ticket",
	})
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := vvfile.ParseFile(path, vvfile.ParseOptions{}) // zero value
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if f.Deleted {
		t.Error("Deleted should be false with zero-value options")
	}
	if f.DeleteErr != nil {
		t.Errorf("DeleteErr should be nil, got %v", f.DeleteErr)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should still exist: %v", err)
	}
	if !f.DeleteThisFile {
		t.Error("DeleteThisFile flag from file should still be true")
	}
}

func TestParseFileDeleteIfRequestedFalseWithFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keep2.vv")
	content := minimalVV(t, map[string]string{
		"delete-this-file": "1",
		"password":         "ticket",
	})
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := vvfile.ParseFile(path, vvfile.ParseOptions{DeleteIfRequested: false})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should still exist: %v", err)
	}
}

func TestPasswordOverMaxRejected(t *testing.T) {
	long := strings.Repeat("x", vvfile.MaxPasswordLen+1)
	content := minimalVV(t, map[string]string{"password": long})
	_, err := vvfile.Parse(strings.NewReader(content))
	if err == nil {
		t.Fatal("expected error for oversize password")
	}
	if !strings.Contains(err.Error(), "password") {
		t.Errorf("error should mention password: %v", err)
	}
	if !strings.Contains(err.Error(), "protocol limit") {
		t.Errorf("error should mention protocol limit: %v", err)
	}
}

func TestPasswordExactMaxAccepted(t *testing.T) {
	exact := strings.Repeat("y", vvfile.MaxPasswordLen)
	content := minimalVV(t, map[string]string{"password": exact})
	f, err := vvfile.Parse(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(f.Password) != vvfile.MaxPasswordLen {
		t.Errorf("len(Password) = %d", len(f.Password))
	}
}

func TestOversizedCARejected(t *testing.T) {
	// Single INI line with \n escapes; after unescape, PEM exceeds MaxCAPEMSize.
	// Payload of MaxCAPEMSize 'A's alone is enough (headers add more).
	escaped := "-----BEGIN CERTIFICATE-----\\n" + strings.Repeat("A", vvfile.MaxCAPEMSize) + "\\n-----END CERTIFICATE-----\\n"
	var b strings.Builder
	b.WriteString("[virt-viewer]\n")
	b.WriteString("type=spice\n")
	b.WriteString("host=example.com\n")
	b.WriteString("tls-port=5900\n")
	b.WriteString("password=ticket\n")
	b.WriteString("ca=")
	b.WriteString(escaped)
	b.WriteByte('\n')
	_, err := vvfile.Parse(strings.NewReader(b.String()))
	if err == nil {
		t.Fatal("expected error for oversized ca")
	}
	if !strings.Contains(err.Error(), "ca") {
		t.Errorf("error should mention ca: %v", err)
	}
}

func TestCANewlineUnescaping(t *testing.T) {
	// Single \n escapes (Proxmox style).
	content := `[virt-viewer]
type=spice
host=h
tls-port=1
password=p
ca=-----BEGIN CERTIFICATE-----\nLINEONE\nLINETWO\n-----END CERTIFICATE-----\n
`
	f, err := vvfile.Parse(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := "-----BEGIN CERTIFICATE-----\nLINEONE\nLINETWO\n-----END CERTIFICATE-----\n"
	if string(f.CA) != want {
		t.Errorf("CA =\n%q\nwant\n%q", f.CA, want)
	}

	// Double-escaped \\n also becomes newlines.
	content2 := "[virt-viewer]\ntype=spice\nhost=h\ntls-port=1\npassword=p\nca=BEGIN\\\\nEND\n"
	f2, err := vvfile.Parse(strings.NewReader(content2))
	if err != nil {
		t.Fatalf("Parse double: %v", err)
	}
	if string(f2.CA) != "BEGIN\nEND" {
		t.Errorf("double-escape CA = %q", f2.CA)
	}
}

func TestTypeMustBeSpice(t *testing.T) {
	content := minimalVV(t, map[string]string{"type": "vnc", "password": "p"})
	_, err := vvfile.Parse(strings.NewReader(content))
	if err == nil || !strings.Contains(err.Error(), "spice") {
		t.Fatalf("want type error, got %v", err)
	}
}

func TestHostRequired(t *testing.T) {
	content := `[virt-viewer]
type=spice
tls-port=1
password=p
ca=-----BEGIN-----\n-----END-----\n
`
	_, err := vvfile.Parse(strings.NewReader(content))
	if err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("want host error, got %v", err)
	}
}

func TestTLSRequiresCA(t *testing.T) {
	content := `[virt-viewer]
type=spice
host=h
tls-port=61000
password=p
`
	_, err := vvfile.Parse(strings.NewReader(content))
	if err == nil || !strings.Contains(err.Error(), "ca") {
		t.Fatalf("want ca error, got %v", err)
	}
}

func TestCleartextPortWithoutCA(t *testing.T) {
	content := `[virt-viewer]
type=spice
host=127.0.0.1
port=5900
password=labticket
`
	f, err := vvfile.Parse(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Port != 5900 || f.TLSPort != 0 {
		t.Errorf("Port=%d TLSPort=%d", f.Port, f.TLSPort)
	}
}

func TestMissingPortRejected(t *testing.T) {
	content := `[virt-viewer]
type=spice
host=h
password=p
`
	_, err := vvfile.Parse(strings.NewReader(content))
	if err == nil {
		t.Fatal("expected error when both ports missing")
	}
}

func TestUnknownKeysIgnored(t *testing.T) {
	content := minimalVV(t, map[string]string{
		"password":        "p",
		"enable-usbredir": "1",
		"secure-channels": "main",
		"version":         "9.9",
		"future-key":      "x",
	})
	f, err := vvfile.Parse(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if string(f.Password) != "p" {
		t.Errorf("Password = %q", f.Password)
	}
}

func TestParseFileNoDeleteWhenFlagZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodelete.vv")
	content := minimalVV(t, map[string]string{
		"delete-this-file": "0",
		"password":         "ticket",
	})
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := vvfile.ParseFile(path, vvfile.ParseOptions{DeleteIfRequested: true})
	if err != nil {
		t.Fatal(err)
	}
	if f.Deleted || f.DeleteThisFile {
		t.Errorf("should not delete when flag is 0: Deleted=%v DeleteThisFile=%v", f.Deleted, f.DeleteThisFile)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should remain: %v", err)
	}
}

func TestParseFileOpenMissing(t *testing.T) {
	_, err := vvfile.ParseFile(filepath.Join(t.TempDir(), "nope.vv"), vvfile.ParseOptions{})
	if err == nil {
		t.Fatal("expected open error")
	}
}

func minimalVV(t *testing.T, overrides map[string]string) string {
	t.Helper()
	m := map[string]string{
		"type":     "spice",
		"host":     "pvespiceproxy:abc:1:pve::deadbeef",
		"tls-port": "61002",
		"password": "ticket",
		"ca":       `-----BEGIN CERTIFICATE-----\nMII=\n-----END CERTIFICATE-----\n`,
	}
	for k, v := range overrides {
		m[k] = v
	}
	var b strings.Builder
	b.WriteString("[virt-viewer]\n")
	// Stable-ish order for readability.
	order := []string{"type", "host", "tls-port", "port", "password", "ca", "proxy", "host-subject", "title", "delete-this-file", "fullscreen"}
	seen := map[string]bool{}
	for _, k := range order {
		if v, ok := m[k]; ok {
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(v)
			b.WriteByte('\n')
			seen[k] = true
		}
	}
	for k, v := range m {
		if seen[k] {
			continue
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	return b.String()
}
