// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maskraven/virt-viewer/internal/ux"
	"github.com/maskraven/virt-viewer/pkg/vvfile"
)

func TestParseArgs_Version(t *testing.T) {
	opts, err := parseArgs([]string{"-version"}, ioDiscard())
	if err != nil {
		t.Fatal(err)
	}
	if !opts.version {
		t.Fatal("expected version=true")
	}
	if opts.headless {
		t.Fatal("headless should be false by default")
	}
	if opts.path != "" {
		t.Fatalf("path = %q, want empty", opts.path)
	}
}

func TestParseArgs_HeadlessAndPath(t *testing.T) {
	opts, err := parseArgs([]string{"--headless", "pve-spice.vv"}, ioDiscard())
	if err != nil {
		t.Fatal(err)
	}
	if !opts.headless {
		t.Fatal("expected headless=true")
	}
	if opts.path != "pve-spice.vv" {
		t.Fatalf("path = %q", opts.path)
	}
}

func TestParseArgs_PathBeforeFlags(t *testing.T) {
	opts, err := parseArgs([]string{"-headless", "/tmp/x.vv"}, ioDiscard())
	if err != nil {
		t.Fatal(err)
	}
	if !opts.headless || opts.path != "/tmp/x.vv" {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestParseArgs_TooManyArgs(t *testing.T) {
	_, err := parseArgs([]string{"a.vv", "b.vv"}, ioDiscard())
	if err == nil {
		t.Fatal("expected error for extra args")
	}
}

func TestParseArgs_Help(t *testing.T) {
	var stderr bytes.Buffer
	_, err := parseArgs([]string{"-h"}, &stderr)
	if err != flag.ErrHelp {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	out := stderr.String()
	if !strings.Contains(out, "Usage: remote-viewer") {
		t.Fatalf("help missing usage: %q", out)
	}
	if !strings.Contains(out, "-headless") {
		t.Fatalf("help missing -headless: %q", out)
	}
	if !strings.Contains(out, "-version") {
		t.Fatalf("help missing -version: %q", out)
	}
}

func TestRun_Version(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-version"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr=%q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != Version {
		t.Fatalf("stdout = %q, want %q", stdout.String(), Version)
	}
}

func TestRun_NoArgsPrintsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage: remote-viewer") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRun_BadVVMapsUserMessage_Headless(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.vv")
	content := "[virt-viewer]\ntype=vnc\nhost=h\npassword=p\nport=1\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--headless", path}, &stdout, &stderr)
	if code != exitFail {
		t.Fatalf("exit = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), ux.MsgConfigNotSpice) {
		t.Fatalf("expected user message %q in stderr=%q", ux.MsgConfigNotSpice, stderr.String())
	}
}

func TestRun_MissingFileUserMessage_Headless(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such.vv")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--headless", path}, &stdout, &stderr)
	if code != exitFail {
		t.Fatalf("exit = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), ux.MsgConfigEndpoint) {
		t.Fatalf("expected %q in stderr=%q", ux.MsgConfigEndpoint, stderr.String())
	}
}

// GUI path should also map parse errors before opening a window.
func TestRun_BadVVMapsUserMessage_GUI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.vv")
	content := "[virt-viewer]\ntype=vnc\nhost=h\npassword=p\nport=1\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{path}, &stdout, &stderr)
	if code != exitFail {
		t.Fatalf("exit = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), ux.MsgConfigNotSpice) {
		t.Fatalf("expected user message %q in stderr=%q", ux.MsgConfigNotSpice, stderr.String())
	}
}

func TestOpenConnectionFile_DeleteIfRequested(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "del.vv")
	body := strings.Join([]string{
		"[virt-viewer]",
		"type=spice",
		"host=127.0.0.1",
		"port=5900",
		"password=test-ticket-password",
		"delete-this-file=1",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := openConnectionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Deleted {
		t.Fatalf("Deleted = false; product DeleteIfRequested should remove file (DeleteErr=%v)", f.DeleteErr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists after product parse: %v", err)
	}
	if f.Host != "127.0.0.1" {
		t.Fatalf("Host = %q", f.Host)
	}
}

func TestOpenConnectionFile_MapsFieldTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.vv")
	pw := strings.Repeat("x", vvfile.MaxPasswordLen+1)
	body := fmt.Sprintf("[virt-viewer]\ntype=spice\nhost=h\nport=1\npassword=%s\n", pw)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := openConnectionFile(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if ux.UserMessage(err) != ux.MsgConfigFieldTooLarge {
		t.Fatalf("UserMessage = %q, want %q (err=%v)", ux.UserMessage(err), ux.MsgConfigFieldTooLarge, err)
	}
	var ue *ux.Error
	if !errors.As(err, &ue) || ue.Class != ux.ClassConfig {
		t.Fatalf("class = %#v", err)
	}
}

func TestMapVVError_Classes(t *testing.T) {
	cases := []struct {
		err error
		msg string
		cls ux.Class
	}{
		{errors.New(`vvfile: type must be spice, got "vnc"`), ux.MsgConfigNotSpice, ux.ClassConfig},
		{errors.New("vvfile: password length 99 exceeds protocol limit 60"), ux.MsgConfigFieldTooLarge, ux.ClassConfig},
		{errors.New("vvfile: missing required key host"), ux.MsgConfigEndpoint, ux.ClassConfig},
		{errors.New("vvfile: open \"x\": no such file"), ux.MsgConfigEndpoint, ux.ClassConfig},
		{ux.New(ux.ClassTicket, ux.MsgTicket, errors.New("auth")), ux.MsgTicket, ux.ClassTicket},
	}
	for _, tc := range cases {
		got := mapVVError(tc.err)
		if ux.UserMessage(got) != tc.msg {
			t.Errorf("mapVVError(%v): msg = %q, want %q", tc.err, ux.UserMessage(got), tc.msg)
		}
		cl := ux.Classify(got)
		if cl == nil || cl.Class != tc.cls {
			t.Errorf("mapVVError(%v): class = %#v, want %s", tc.err, cl, tc.cls)
		}
	}
}

func TestWipeBytes(t *testing.T) {
	b := []byte("secret")
	wipeBytes(b)
	for i, c := range b {
		if c != 0 {
			t.Fatalf("b[%d] = %d, want 0", i, c)
		}
	}
	wipeBytes(nil)
}

func ioDiscard() *bytes.Buffer {
	return &bytes.Buffer{}
}
