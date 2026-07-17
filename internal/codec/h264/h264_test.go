// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package h264

import (
	"errors"
	"runtime"
	"testing"
)

func TestAvailableMatchesGOOS(t *testing.T) {
	av := Available()
	switch runtime.GOOS {
	case "darwin", "windows":
		if !av {
			t.Fatalf("Available() = false on %s; expected true (OS decoder)", runtime.GOOS)
		}
	case "linux":
		// True only when system ffmpeg with h264 is on PATH (not bundled).
		t.Logf("linux Available() = %v (depends on host ffmpeg)", av)
	default:
		if av {
			t.Fatalf("Available() = true on %s; expected false stub", runtime.GOOS)
		}
	}
}

func TestNewAndClose(t *testing.T) {
	d, err := New()
	switch runtime.GOOS {
	case "darwin", "windows":
		// continue below
	case "linux":
		if !Available() {
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("New without ffmpeg: want ErrUnavailable, got %v", err)
			}
			return
		}
		// ffmpeg present — exercise New/Close/Decode like OS backends
	default:
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("New on stub: want ErrUnavailable, got %v", err)
		}
		return
	}
	if err != nil {
		// Windows MFStartup / Linux ffmpeg start can fail in some CI sandboxes.
		if errors.Is(err, ErrUnavailable) {
			t.Skipf("decoder init unavailable in this environment: %v", err)
		}
		t.Fatalf("New: %v", err)
	}
	defer d.Close()

	// Empty AU
	_, err = d.Decode(nil, 0, 0)
	if err == nil {
		t.Fatal("Decode(nil) should fail")
	}
	// Junk without SPS/PPS should soft-fail with ErrDecode, not panic.
	_, err = d.Decode([]byte{0, 0, 0, 1, 0x65, 0xff}, 16, 16)
	if err == nil {
		t.Log("unexpected success on junk; ok if decoder is lenient")
	} else if !errors.Is(err, ErrDecode) && !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unexpected error kind: %v", err)
	}
}

func TestDecodeOnceStub(t *testing.T) {
	if Available() {
		t.Skip("real backend present")
	}
	_, err := DecodeOnce([]byte{0, 0, 1, 0x67}, 0, 0)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}
