// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package security

import (
	"bytes"
	"io"
	"strings"
	"sync"
)

// Redacted is the replacement token for secret material in log lines.
const Redacted = "[REDACTED]"

// pemBegin / pemEnd mark PEM blocks (CA material from .vv files).
const (
	pemBegin = "-----BEGIN "
	pemEnd   = "-----END "
)

// Redact replaces every occurrence of each non-empty secret in s with Redacted.
// Empty secrets are ignored. Longer secrets are applied first so a secret that
// is a prefix of another does not leave a residual suffix.
//
// Use for passwords, tickets, and any other caller-known secret bytes before
// writing log lines. Prefer RedactingWriter when a logger writes unstructured
// text that may include secrets.
func Redact(s string, secrets ...[]byte) string {
	if s == "" || len(secrets) == 0 {
		return s
	}
	// Sort by length descending (simple insertion; secret count is tiny).
	ordered := make([][]byte, 0, len(secrets))
	for _, sec := range secrets {
		if len(sec) == 0 {
			continue
		}
		ordered = append(ordered, sec)
	}
	for i := 1; i < len(ordered); i++ {
		j := i
		for j > 0 && len(ordered[j-1]) < len(ordered[j]) {
			ordered[j-1], ordered[j] = ordered[j], ordered[j-1]
			j--
		}
	}
	out := s
	for _, sec := range ordered {
		out = strings.ReplaceAll(out, string(sec), Redacted)
	}
	return out
}

// RedactString is Redact for string secrets (still converts to bytes for the
// same replacement rules). Prefer []byte secrets in production paths.
func RedactString(s string, secrets ...string) string {
	if len(secrets) == 0 {
		return s
	}
	bs := make([][]byte, len(secrets))
	for i, sec := range secrets {
		bs[i] = []byte(sec)
	}
	return Redact(s, bs...)
}

// RedactPEM replaces PEM certificate/key blocks in s with Redacted.
// Full CA PEM from .vv files must never appear in logs (design: Observability).
func RedactPEM(s string) string {
	if !strings.Contains(s, pemBegin) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	rest := s
	for {
		start := strings.Index(rest, pemBegin)
		if start < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:start])
		b.WriteString(Redacted)
		afterBegin := rest[start+len(pemBegin):]
		endMarker := strings.Index(afterBegin, pemEnd)
		if endMarker < 0 {
			// Truncated PEM: drop remainder after BEGIN.
			break
		}
		afterEnd := afterBegin[endMarker+len(pemEnd):]
		// Skip label + trailing dashes on the END line (e.g. "CERTIFICATE-----").
		nl := strings.IndexByte(afterEnd, '\n')
		if nl >= 0 {
			rest = afterEnd[nl+1:]
		} else {
			// End of string after END line.
			rest = ""
		}
	}
	return b.String()
}

// RedactLogLine applies Redact for secrets and strips PEM blocks.
// Ticket ciphertexts (raw binary) are not string-safe; callers that hex-encode
// ciphertext should pass the hex form as a secret.
func RedactLogLine(s string, secrets ...[]byte) string {
	return RedactPEM(Redact(s, secrets...))
}

// RedactingWriter wraps an io.Writer and redacts secrets from each Write.
// Concurrent Writes are serialized so multi-Write lines stay consistent.
//
// Typical use:
//
//	w := security.NewRedactingWriter(os.Stderr, password)
//	log.SetOutput(w)
type RedactingWriter struct {
	mu      sync.Mutex
	w       io.Writer
	secrets [][]byte
}

// NewRedactingWriter returns a Writer that redacts secrets (and PEM blocks)
// before forwarding to w. Secrets are deep-copied.
func NewRedactingWriter(w io.Writer, secrets ...[]byte) *RedactingWriter {
	cp := make([][]byte, 0, len(secrets))
	for _, s := range secrets {
		if len(s) == 0 {
			continue
		}
		dup := make([]byte, len(s))
		copy(dup, s)
		cp = append(cp, dup)
	}
	return &RedactingWriter{w: w, secrets: cp}
}

// Write implements io.Writer. The buffer is redacted as a complete chunk;
// callers should write whole log lines when possible.
func (r *RedactingWriter) Write(p []byte) (int, error) {
	if r == nil || r.w == nil {
		return 0, io.ErrClosedPipe
	}
	// Always report original length on success so log packages do not retry.
	n := len(p)
	line := RedactLogLine(string(p), r.secrets...)
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := io.WriteString(r.w, line)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// WipeSecrets zeros stored secret copies. Safe to call multiple times.
func (r *RedactingWriter) WipeSecrets() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.secrets {
		Wipe(r.secrets[i])
		r.secrets[i] = nil
	}
	r.secrets = nil
}

// ContainsSecret reports whether s contains any non-empty secret as a substring.
// Tests use this to fail closed when a password leaks into log output.
func ContainsSecret(s string, secrets ...[]byte) bool {
	if s == "" {
		return false
	}
	b := []byte(s)
	for _, sec := range secrets {
		if len(sec) == 0 {
			continue
		}
		if bytes.Contains(b, sec) {
			return true
		}
	}
	return false
}
