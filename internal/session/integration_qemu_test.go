// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package session_test

// QEMU live integration tests (//go:build integration).
//
// These tests are skipped unless a SPICE-capable QEMU lab is reachable.
// Default unit CI runs without -tags=integration, so this file is not built.
//
// How to run:
//
//	# Terminal 1 — start a local password SPICE server (binds 127.0.0.1 only):
//	./scripts/interop_qemu.sh
//
//	# Terminal 2 — run integration tests:
//	./scripts/run_integration.sh
//	# or:
//	go test -tags=integration ./internal/session/ -count=1 -run TestQEMU
//
// Environment (defaults match scripts/interop_qemu.sh):
//
//	SPICE_HOST      default 127.0.0.1
//	SPICE_PORT      default 5900
//	SPICE_PASSWORD  default testpass
//
// Optional recording of QEMU SPICE traffic for fixture capture:
//
//	SPICE_RECORD=testdata/records/lab-$(date +%Y%m%d).rec ./scripts/interop_qemu.sh
//	# see testdata/records/README.md
//
// CI: unit job does not enable this tag. See scripts/README.md and
// .github/workflows/ci.yml comments for the documented integration path.

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/maskraven/spice-viewer/internal/connector"
	"github.com/maskraven/spice-viewer/internal/session"
)

func qemuLabConfig(t *testing.T) (host string, port int, password []byte) {
	t.Helper()
	host = envOr("SPICE_HOST", "127.0.0.1")
	portStr := envOr("SPICE_PORT", "5900")
	p, err := strconv.Atoi(portStr)
	if err != nil || p < 1 || p > 65535 {
		t.Fatalf("invalid SPICE_PORT %q", portStr)
	}
	password = []byte(envOr("SPICE_PASSWORD", "testpass"))
	return host, p, password
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func requireQEMUReachable(t *testing.T, host string, port int) {
	t.Helper()
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Skipf("QEMU SPICE lab not reachable at %s (%v); start with ./scripts/interop_qemu.sh", addr, err)
	}
	_ = conn.Close()
}

// TestQEMU_MainLinkOK dials a live QEMU SPICE server and completes main-channel
// link + AuthSpice. Requires scripts/interop_qemu.sh (or equivalent) running.
func TestQEMU_MainLinkOK(t *testing.T) {
	host, port, password := qemuLabConfig(t)
	requireQEMUReachable(t, host, port)

	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{
			Host:           host,
			Port:           port,
			AllowCleartext: true,
		},
		Password:       password,
		AllowCleartext: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = s.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := s.DialMain(ctx); err != nil {
		t.Fatalf("DialMain against QEMU lab: %v", err)
	}
	if !s.Linked() {
		t.Fatal("expected Linked() after successful DialMain")
	}
}

// TestQEMU_BadPassword_Fails exercises ticket failure against a live server.
func TestQEMU_BadPassword_Fails(t *testing.T) {
	host, port, _ := qemuLabConfig(t)
	requireQEMUReachable(t, host, port)

	s, err := session.New(session.Config{
		Endpoint: connector.Endpoint{
			Host:           host,
			Port:           port,
			AllowCleartext: true,
		},
		Password:       []byte("wrong-password-not-the-lab-secret"),
		AllowCleartext: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = s.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err = s.DialMain(ctx)
	if err == nil {
		t.Fatal("expected DialMain to fail with wrong password")
	}
}
