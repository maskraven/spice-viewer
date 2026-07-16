// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package vvfile

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Password and payload limits.
//
// MaxPasswordLen is SPICE_MAX_PASSWORD_LENGTH from spice-protocol (60).
// RSA-OAEP-SHA1 with a 1024-bit SPICE link key allows up to 85 bytes of
// plaintext including a trailing NUL (design “OAEP budget”), but QEMU,
// spice-gtk, and Proxmox ticket encryption use the protocol max of 60.
// Keep this aligned with internal/security MaxSpicePasswordLen so parse
// and EncryptSpiceTicket reject the same oversize tickets.
const (
	MaxPasswordLen    = 60
	MaxHostLen        = 512
	MaxHostSubjectLen = 1024
	MaxCAPEMSize      = 256 << 10 // 256 KiB
	MaxFileSize       = 1 << 20   // 1 MiB
)

// File is a parsed virt-viewer connection file ([virt-viewer] section).
// Unknown keys are ignored for forward compatibility.
type File struct {
	Type             string
	Host             string
	HostSubject      string
	Title            string
	TLSPort          int
	Port             int
	Password         []byte // secret; caller should wipe when done
	Proxy            *url.URL
	CA               []byte // PEM (newlines unescaped)
	DeleteThisFile   bool
	SecureAttention  string
	ReleaseCursor    string
	ToggleFullscreen string
	Fullscreen       bool

	// Deleted is true when ParseFile successfully removed the path after
	// copying secrets (DeleteIfRequested && delete-this-file=1).
	Deleted bool
	// DeleteErr is set when a delete was attempted but os.Remove failed.
	// The File is still returned; callers may warn the user.
	DeleteErr error
}

// ParseOptions controls ParseFile side effects. Zero value is safe: no delete.
type ParseOptions struct {
	// DeleteIfRequested: if true AND the file has delete-this-file=1,
	// remove path after secrets are copied, before return.
	DeleteIfRequested bool
}

// Parse reads a .vv document from r. It never deletes files.
func Parse(r io.Reader) (*File, error) {
	limited := io.LimitReader(r, MaxFileSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("vvfile: read: %w", err)
	}
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("vvfile: file exceeds max size %d bytes", MaxFileSize)
	}
	return parseBytes(data)
}

// ParseFile opens path, parses it, and optionally deletes the file when
// opts.DeleteIfRequested is true and the file requested delete-this-file=1.
// Deletion happens only after successful parse and secret copy. A failed
// remove does not fail the parse: File is returned with DeleteErr set.
func ParseFile(path string, opts ParseOptions) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("vvfile: open %q: %w", path, err)
	}
	data, err := readLimited(f, MaxFileSize)
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, fmt.Errorf("vvfile: close %q: %w", path, closeErr)
	}

	file, err := parseBytes(data)
	if err != nil {
		return nil, err
	}

	if opts.DeleteIfRequested && file.DeleteThisFile {
		if remErr := removeWithRetry(path); remErr != nil {
			file.DeleteErr = remErr
		} else {
			file.Deleted = true
		}
	}
	return file, nil
}

func readLimited(r io.Reader, max int) ([]byte, error) {
	limited := io.LimitReader(r, int64(max)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("vvfile: read: %w", err)
	}
	if len(data) > max {
		return nil, fmt.Errorf("vvfile: file exceeds max size %d bytes", max)
	}
	return data, nil
}

func removeWithRetry(path string) error {
	err := os.Remove(path)
	if err == nil {
		return nil
	}
	// Permanent / non-retryable: already gone, or wrong type (e.g. directory).
	if os.IsNotExist(err) || isNotFileRemoveErr(err) {
		return err
	}
	// Browsers on Windows may briefly lock the download; retry once on
	// permission/lock-style failures only (skip sleep for other errors).
	if !errors.Is(err, os.ErrPermission) && !isBusyRemoveErr(err) {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	if err2 := os.Remove(path); err2 == nil {
		return nil
	}
	return err
}

// isNotFileRemoveErr reports errors that will not succeed on retry
// (directory not empty / is a directory).
func isNotFileRemoveErr(err error) bool {
	// Unix: EISDIR / ENOTEMPTY surface as PathError with those semantics.
	// Match common substrings without importing x/sys.
	msg := err.Error()
	return strings.Contains(msg, "directory not empty") ||
		strings.Contains(msg, "is a directory")
}

// isBusyRemoveErr is best-effort detection of transient file locks.
func isBusyRemoveErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "busy") ||
		strings.Contains(msg, "locked") ||
		strings.Contains(msg, "being used")
}

func parseBytes(data []byte) (*File, error) {
	keys, err := parseINISection(data, "virt-viewer")
	if err != nil {
		return nil, err
	}

	out := &File{}
	for k, v := range keys {
		switch k {
		case "type":
			out.Type = v
		case "host":
			out.Host = v
		case "host-subject":
			out.HostSubject = v
		case "title":
			out.Title = v
		case "tls-port":
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 || n > 65535 {
				return nil, fmt.Errorf("vvfile: invalid tls-port %q", v)
			}
			out.TLSPort = n
		case "port":
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 || n > 65535 {
				return nil, fmt.Errorf("vvfile: invalid port %q", v)
			}
			out.Port = n
		case "password":
			out.Password = []byte(v)
		case "proxy":
			if strings.TrimSpace(v) == "" {
				continue
			}
			u, err := url.Parse(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("vvfile: invalid proxy URL: %w", err)
			}
			if u.Scheme == "" || u.Host == "" {
				return nil, fmt.Errorf("vvfile: invalid proxy URL %q (need scheme and host)", v)
			}
			out.Proxy = u
		case "ca":
			out.CA = []byte(unescapeNewlines(v))
		case "delete-this-file":
			out.DeleteThisFile = isTruthy(v)
		case "secure-attention":
			out.SecureAttention = v
		case "release-cursor":
			out.ReleaseCursor = v
		case "toggle-fullscreen":
			out.ToggleFullscreen = v
		case "fullscreen":
			out.Fullscreen = isTruthy(v)
		default:
			// enable-usbredir, secure-channels, version, unknown: ignore
		}
	}

	if err := validate(out); err != nil {
		return nil, err
	}
	return out, nil
}

func validate(f *File) error {
	if f.Type == "" {
		return errors.New("vvfile: missing required key type")
	}
	if !strings.EqualFold(f.Type, "spice") {
		return fmt.Errorf("vvfile: type must be spice, got %q", f.Type)
	}
	if f.Host == "" {
		return errors.New("vvfile: missing required key host")
	}
	if len(f.Host) > MaxHostLen {
		return fmt.Errorf("vvfile: host length %d exceeds max %d", len(f.Host), MaxHostLen)
	}
	if len(f.HostSubject) > MaxHostSubjectLen {
		return fmt.Errorf("vvfile: host-subject length %d exceeds max %d", len(f.HostSubject), MaxHostSubjectLen)
	}
	if len(f.Password) == 0 {
		return errors.New("vvfile: missing required key password")
	}
	if len(f.Password) > MaxPasswordLen {
		return fmt.Errorf("vvfile: password length %d exceeds protocol limit %d (SPICE_MAX_PASSWORD_LENGTH)", len(f.Password), MaxPasswordLen)
	}
	if f.TLSPort == 0 && f.Port == 0 {
		return errors.New("vvfile: tls-port or port is required")
	}
	// TLS path (Proxmox and normal secure viewers): tls-port set ⇒ CA required.
	if f.TLSPort != 0 {
		if len(f.CA) == 0 {
			return errors.New("vvfile: ca is required when tls-port is set")
		}
	}
	if len(f.CA) > MaxCAPEMSize {
		return fmt.Errorf("vvfile: ca PEM size %d exceeds max %d", len(f.CA), MaxCAPEMSize)
	}
	return nil
}

// unescapeNewlines turns Proxmox-style CA PEM escapes into real newlines.
// Handles single (\n) and double (\\n) backslash-n sequences.
func unescapeNewlines(s string) string {
	// Double-escaped first so "\\n" does not become "\" + newline.
	s = strings.ReplaceAll(s, `\\n`, "\n")
	s = strings.ReplaceAll(s, `\n`, "\n")
	return s
}

func isTruthy(v string) bool {
	v = strings.TrimSpace(v)
	switch v {
	case "1", "true", "TRUE", "True", "yes", "YES", "Yes", "on", "ON", "On":
		return true
	default:
		return false
	}
}

// parseINISection extracts key/value pairs from the first named section only.
// Keys are lowercased. Duplicate keys within that section: last value wins.
// Later sections with the same name are ignored. Lines outside the section
// are ignored.
func parseINISection(data []byte, section string) (map[string]string, error) {
	want := strings.ToLower(section)
	sc := bufio.NewScanner(bytes.NewReader(data))
	// Allow long CA lines (still bounded by MaxFileSize overall).
	sc.Buffer(make([]byte, 0, 64*1024), MaxFileSize)

	keys := make(map[string]string)
	inSection := false
	found := false
	// After the first matching section ends, do not re-enter another match.
	done := false

	for sc.Scan() {
		// Scanner strips the line ending; keep value trailing spaces.
		// Only strip a trailing CR that can remain from CRLF files.
		raw := strings.TrimSuffix(sc.Text(), "\r")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			if found && inSection {
				// Leaving the first matching section — stop accepting keys.
				inSection = false
				done = true
				continue
			}
			if done {
				inSection = false
				continue
			}
			inSection = name == want
			if inSection {
				found = true
			}
			continue
		}
		if !inSection {
			continue
		}
		eq := strings.IndexByte(raw, '=')
		if eq < 0 {
			// Ignore non key=value lines inside section.
			continue
		}
		k := strings.ToLower(strings.TrimSpace(raw[:eq]))
		// Preserve value as-is after '=' (including trailing whitespace),
		// except a single optional leading space after '=' (hand-edited files)
		// and a leftover CR already stripped from the line.
		v := raw[eq+1:]
		if len(v) > 0 && v[0] == ' ' {
			v = v[1:]
		}
		if k == "" {
			continue
		}
		keys[k] = v
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("vvfile: scan: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("vvfile: missing [%s] section", section)
	}
	return keys, nil
}
