// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build windows && cgo

package h264

/*
#cgo LDFLAGS: -lmfplat -lmf -lmfuuid -lole32

// Media Foundation H.264 path.
// Full MFT pipeline (IMFTransform) is large; this Phase-3 cut:
//  1. Declares Available() true on Windows so stream caps can be advertised.
//  2. Implements Decode via a deferred MFT session that is wired when
//     ole32/mfplat init succeeds.
//
// Until the MFT graph is complete, Decode returns ErrDecode with a clear
// message after Available() has been true — display still soft-skips frames
// rather than crashing. Init failure flips Available-style New() to
// ErrUnavailable for that process.
//
// Implementation note: a complete IMFSourceReader or H.264 decoder MFT path
// will replace decode_mft_placeholder below without changing the Go API.

#include <windows.h>
#include <mfapi.h>
#include <mfidl.h>
#include <mftransform.h>
#include <mferror.h>
#include <stdint.h>

static int mf_startup(void) {
	HRESULT hr = MFStartup(MF_VERSION, MFSTARTUP_FULL);
	return FAILED(hr) ? (int)hr : 0;
}

static void mf_shutdown(void) {
	MFShutdown();
}
*/
import "C"

import (
	"fmt"
	"sync"

	"github.com/maskraven/virt-viewer/internal/codec"
)

func available() bool { return true }

type mfDecoder struct {
	mu      sync.Mutex
	started bool
	// placeholder until full MFT graph: we accept SPS/PPS and count frames
	haveParams bool
	frames     int
}

func newDecoder() (Decoder, error) {
	if rc := C.mf_startup(); rc != 0 {
		return nil, fmt.Errorf("%w: MFStartup 0x%x", ErrUnavailable, uint32(rc))
	}
	return &mfDecoder{started: true}, nil
}

func (d *mfDecoder) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started {
		C.mf_shutdown()
		d.started = false
	}
}

func (d *mfDecoder) Decode(annexB []byte, w, h int) (*codec.RGBA, error) {
	if len(annexB) == 0 {
		return nil, FormatError("empty access unit")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.started {
		return nil, ErrUnavailable
	}

	// Detect in-band parameter sets (NAL type 7/8) so callers can tell we
	// saw a valid stream start even before full MFT output lands.
	if hasNALType(annexB, 7) && hasNALType(annexB, 8) {
		d.haveParams = true
	}
	d.frames++

	// Full IMFTransform output not yet wired — soft-fail per frame so the
	// SPICE session keeps running (same policy as unsupported GLZ).
	_ = w
	_ = h
	if !d.haveParams {
		return nil, fmt.Errorf("%w: waiting for SPS/PPS", ErrDecode)
	}
	return nil, fmt.Errorf("%w: Media Foundation pixel output not yet wired (frame %d); see docs/phase3.md", ErrDecode, d.frames)
}

// hasNALType scans Annex-B for a NAL unit of the given type (low 5 bits).
func hasNALType(data []byte, nalType byte) bool {
	i := 0
	for i+3 < len(data) {
		sc := 0
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			sc = 3
		} else if i+4 <= len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			sc = 4
		} else {
			i++
			continue
		}
		nalStart := i + sc
		if nalStart >= len(data) {
			break
		}
		if data[nalStart]&0x1f == nalType {
			return true
		}
		// advance to next start code
		j := nalStart + 1
		for j+3 < len(data) {
			if data[j] == 0 && data[j+1] == 0 && (data[j+2] == 1 || (j+3 < len(data) && data[j+2] == 0 && data[j+3] == 1)) {
				break
			}
			j++
		}
		i = j
	}
	return false
}

// Ensure codec import used when full MFT lands.
var _ = codec.RGBA{}
