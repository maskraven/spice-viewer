// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package security_test

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/maskraven/spice-viewer/internal/security"
)

func TestRedact_PasswordNeverAppears(t *testing.T) {
	password := []byte("super-secret-ticket-PVE:abc123XYZ")
	msg := fmt.Sprintf("dialing with password=%s host=pve", password)
	out := security.Redact(msg, password)
	if strings.Contains(out, string(password)) {
		t.Fatalf("password leaked into redacted log line: %q", out)
	}
	if !strings.Contains(out, security.Redacted) {
		t.Fatalf("expected %q placeholder in %q", security.Redacted, out)
	}
	if security.ContainsSecret(out, password) {
		t.Fatalf("ContainsSecret true after Redact: %q", out)
	}
}

func TestRedact_EmptySecretNoOp(t *testing.T) {
	s := "hello"
	if got := security.Redact(s, nil, []byte{}); got != s {
		t.Fatalf("got %q want %q", got, s)
	}
}

func TestRedact_LongerSecretFirst(t *testing.T) {
	// If short secret applied first, "password-long" becomes "[REDACTED]-long".
	long := []byte("password-long")
	short := []byte("password")
	msg := "use password-long please"
	out := security.Redact(msg, short, long)
	if strings.Contains(out, "password") {
		t.Fatalf("residual secret material: %q", out)
	}
	if out != "use "+security.Redacted+" please" {
		t.Fatalf("got %q", out)
	}
}

func TestRedactString(t *testing.T) {
	pw := "lab-password-42"
	out := security.RedactString("auth failed for "+pw, pw)
	if strings.Contains(out, pw) {
		t.Fatalf("password leaked: %q", out)
	}
}

func TestRedactPEM_StripsCA(t *testing.T) {
	pem := "-----BEGIN CERTIFICATE-----\nMIIDummyNotReal\n-----END CERTIFICATE-----\n"
	msg := "loaded ca=\n" + pem + "done"
	out := security.RedactPEM(msg)
	if strings.Contains(out, "BEGIN CERTIFICATE") || strings.Contains(out, "MIIDummy") {
		t.Fatalf("PEM leaked: %q", out)
	}
	if !strings.Contains(out, security.Redacted) {
		t.Fatalf("missing redaction token: %q", out)
	}
}

func TestRedactLogLine_PasswordAndPEM(t *testing.T) {
	password := []byte("ticket-value-99")
	pem := "-----BEGIN CERTIFICATE-----\nABC\n-----END CERTIFICATE-----\n"
	msg := fmt.Sprintf("cfg password=%s ca=%s", password, pem)
	out := security.RedactLogLine(msg, password)
	if security.ContainsSecret(out, password) {
		t.Fatalf("password in log: %q", out)
	}
	if strings.Contains(out, "BEGIN CERTIFICATE") || strings.Contains(out, "ABC") {
		t.Fatalf("PEM in log: %q", out)
	}
}

// TestRedactingWriter_PasswordNeverInLogs is the acceptance gate for PR 15:
// a standard library logger that writes through RedactingWriter must never
// emit the session password. This test fails if redaction is broken.
func TestRedactingWriter_PasswordNeverInLogs(t *testing.T) {
	password := []byte("PVE:never-log-this-ticket-value-9f3a")
	var buf bytes.Buffer
	w := security.NewRedactingWriter(&buf, password)
	defer w.WipeSecrets()

	logger := log.New(w, "", 0)
	// Simulate accidental secret logging (debug dumps, error wrapping, etc.).
	logger.Printf("session: dial host=127.0.0.1 password=%s", password)
	logger.Printf("debug: ticket=%s", string(password))
	logger.Print("raw dump: " + string(password))

	got := buf.String()
	if security.ContainsSecret(got, password) {
		t.Fatalf("password appeared in logs (redaction failed):\n%s", got)
	}
	if strings.Contains(got, string(password)) {
		t.Fatalf("password substring in logs:\n%s", got)
	}
	// Sanity: something was written and redacted.
	if !strings.Contains(got, security.Redacted) {
		t.Fatalf("expected redacted output, got %q", got)
	}
	// Control: unredacted path would fail the checks above; document intent.
	leaky := fmt.Sprintf("password=%s", password)
	if !security.ContainsSecret(leaky, password) {
		t.Fatal("ContainsSecret control failed — test harness broken")
	}
}

func TestRedactingWriter_WriteLength(t *testing.T) {
	password := []byte("secret")
	var buf bytes.Buffer
	w := security.NewRedactingWriter(&buf, password)
	defer w.WipeSecrets()
	in := []byte("secret message")
	n, err := w.Write(in)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(in) {
		t.Fatalf("Write returned %d want %d", n, len(in))
	}
}

func TestContainsSecret(t *testing.T) {
	if !security.ContainsSecret("a secret here", []byte("secret")) {
		t.Fatal("expected true")
	}
	if security.ContainsSecret("clean", []byte("secret")) {
		t.Fatal("expected false")
	}
	if security.ContainsSecret("x", nil, []byte{}) {
		t.Fatal("empty secrets should not match")
	}
}
