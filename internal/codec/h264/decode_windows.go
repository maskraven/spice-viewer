// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build windows && cgo

package h264

/*
// -luuid provides IID_IUnknown etc. (required for MinGW/cross-link).
#cgo LDFLAGS: -lmfplat -lmf -lmfuuid -lole32 -luuid -lwmcodecdspuuid

// Media Foundation H.264 decoder (CLSID_CMSH264DecoderMFT).
//
// Pipeline:
//   CoInitializeEx + MFStartup
//   CoCreateInstance(CLSID_CMSH264DecoderMFT) → IMFTransform
//   SetInputType(MFVideoFormat_H264)  [Annex-B with start codes]
//   SetOutputType(NV12 via GetOutputAvailableType)
//   ProcessInput(AU) → ProcessOutput (handle STREAM_CHANGE) → NV12 → RGBA
//
// Soft-fails (positive MF_ERR_* codes) leave the session usable; init failures
// surface as ErrUnavailable from Go.

#include <windows.h>
#include <mfapi.h>
#include <mfidl.h>
#include <mftransform.h>
#include <mferror.h>
#include <wmcodecdsp.h>
#include <codecapi.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// Return codes from mf_dec_decode (positive soft errors; 0 = OK).
// Native HRESULT failures are also mapped to MF_ERR_PROCESS (or returned as
// the raw HRESULT when useful for logging — those are negative as signed int).
enum {
	MF_OK            = 0,
	MF_ERR_NEED_MORE = 1, // params-only AU or decoder buffering
	MF_ERR_BAD_INPUT = 2,
	MF_ERR_PROCESS   = 3,
	MF_ERR_NO_MEM    = 4,
	MF_ERR_NO_FRAME  = 5
};

typedef struct mf_dec {
	IMFTransform *xf;
	int           streaming; // BEGIN_STREAMING + START_OF_STREAM sent
	DWORD         sample_size;
	UINT32        width;
	UINT32        height;
	UINT32        stride; // Y-plane bytes per row (may be > width)
	LONGLONG      pts;
} mf_dec;

static void mf_release(IUnknown *p) {
	if (p) p->lpVtbl->Release(p);
}

static int mf_set_nv12_output(mf_dec *d) {
	IMFMediaType *out = NULL;
	HRESULT hr;
	UINT32 i;

	// Prefer NV12 from the offered list (index order is not guaranteed).
	for (i = 0; ; i++) {
		IMFMediaType *cand = NULL;
		GUID subtype;
		hr = d->xf->lpVtbl->GetOutputAvailableType(d->xf, 0, i, &cand);
		if (FAILED(hr)) break;
		hr = cand->lpVtbl->GetGUID(cand, &MF_MT_SUBTYPE, &subtype);
		if (SUCCEEDED(hr) && IsEqualGUID(&subtype, &MFVideoFormat_NV12)) {
			out = cand;
			break;
		}
		mf_release((IUnknown *)cand);
	}
	if (!out) {
		// NV12 is required for our software color convert path.
		return E_FAIL;
	}

	hr = d->xf->lpVtbl->SetOutputType(d->xf, 0, out, 0);
	if (SUCCEEDED(hr)) {
		UINT64 fs = 0;
		UINT32 ss = 0, st = 0;
		if (SUCCEEDED(out->lpVtbl->GetUINT64(out, &MF_MT_FRAME_SIZE, &fs))) {
			d->width = (UINT32)(fs >> 32);
			d->height = (UINT32)(fs & 0xffffffffu);
		}
		if (SUCCEEDED(out->lpVtbl->GetUINT32(out, &MF_MT_DEFAULT_STRIDE, &st)) && st > 0) {
			// Stride may be signed in MF; use absolute value for row pitch.
			d->stride = st > 0x7fffffffU ? (UINT32)(-(INT32)st) : st;
		} else {
			d->stride = d->width;
		}
		if (SUCCEEDED(out->lpVtbl->GetUINT32(out, &MF_MT_SAMPLE_SIZE, &ss))) {
			d->sample_size = ss;
		} else if (d->width && d->height) {
			// NV12: Y plane (stride*h) + UV plane (stride*h/2)
			d->sample_size = d->stride * d->height * 3 / 2;
		}
	}
	mf_release((IUnknown *)out);
	return SUCCEEDED(hr) ? 0 : (int)hr;
}

static int mf_configure_input(mf_dec *d, UINT32 hint_w, UINT32 hint_h) {
	IMFMediaType *in = NULL;
	HRESULT hr = MFCreateMediaType(&in);
	if (FAILED(hr)) return (int)hr;

	hr = in->lpVtbl->SetGUID(in, &MF_MT_MAJOR_TYPE, &MFMediaType_Video);
	if (SUCCEEDED(hr))
		hr = in->lpVtbl->SetGUID(in, &MF_MT_SUBTYPE, &MFVideoFormat_H264);
	if (SUCCEEDED(hr))
		hr = in->lpVtbl->SetUINT32(in, &MF_MT_INTERLACE_MODE,
			MFVideoInterlace_MixedInterlaceOrProgressive);
	if (SUCCEEDED(hr) && hint_w > 0 && hint_h > 0) {
		UINT64 fs = ((UINT64)hint_w << 32) | (UINT64)hint_h;
		hr = in->lpVtbl->SetUINT64(in, &MF_MT_FRAME_SIZE, fs);
	}
	if (SUCCEEDED(hr))
		hr = d->xf->lpVtbl->SetInputType(d->xf, 0, in, 0);
	mf_release((IUnknown *)in);
	return SUCCEEDED(hr) ? 0 : (int)hr;
}

// CODECAPI_AVLowLatencyMode GUID (codecapi.h). Declared here so MinGW/cross
// builds work without MSVC __uuidof(CODECAPI_AVLowLatencyMode).
static const GUID kCODECAPI_AVLowLatencyMode = {
	0xb980ea2f, 0x5e73, 0x4b8d, {0xa5, 0xdf, 0x8e, 0x71, 0xb1, 0xf1, 0xa6, 0x61}};

static int mf_enable_low_latency(IMFTransform *xf) {
	IMFAttributes *attrs = NULL;
	HRESULT hr = xf->lpVtbl->GetAttributes(xf, &attrs);
	if (FAILED(hr) || !attrs) return 0; // optional
	// Best-effort (Windows 8+); ignore failures.
	attrs->lpVtbl->SetUINT32(attrs, &kCODECAPI_AVLowLatencyMode, TRUE);
	mf_release((IUnknown *)attrs);
	return 0;
}

static int mf_create_transform(mf_dec *d, UINT32 hint_w, UINT32 hint_h) {
	IUnknown *unk = NULL;
	HRESULT hr = CoCreateInstance(
		&CLSID_CMSH264DecoderMFT, NULL, CLSCTX_INPROC_SERVER,
		&IID_IUnknown, (void **)&unk);
	if (FAILED(hr)) return (int)hr;

	hr = unk->lpVtbl->QueryInterface(unk, &IID_IMFTransform, (void **)&d->xf);
	mf_release(unk);
	if (FAILED(hr) || !d->xf) return (int)hr;

	mf_enable_low_latency(d->xf);

	hr = mf_configure_input(d, hint_w, hint_h);
	if (hr != 0) return hr;

	// Placeholder output; real geometry arrives via STREAM_CHANGE.
	hr = mf_set_nv12_output(d);
	if (hr != 0) return hr;

	hr = d->xf->lpVtbl->ProcessMessage(d->xf, MFT_MESSAGE_NOTIFY_BEGIN_STREAMING, 0);
	if (FAILED(hr)) return (int)hr;
	hr = d->xf->lpVtbl->ProcessMessage(d->xf, MFT_MESSAGE_NOTIFY_START_OF_STREAM, 0);
	if (FAILED(hr)) return (int)hr;
	d->streaming = 1;
	return 0;
}

static void mf_destroy_transform(mf_dec *d) {
	if (!d || !d->xf) return;
	if (d->streaming) {
		d->xf->lpVtbl->ProcessMessage(d->xf, MFT_MESSAGE_NOTIFY_END_OF_STREAM, 0);
		d->xf->lpVtbl->ProcessMessage(d->xf, MFT_MESSAGE_COMMAND_FLUSH, 0);
		d->xf->lpVtbl->ProcessMessage(d->xf, MFT_MESSAGE_NOTIFY_END_STREAMING, 0);
		d->streaming = 0;
	}
	mf_release((IUnknown *)d->xf);
	d->xf = NULL;
	d->sample_size = 0;
	d->width = 0;
	d->height = 0;
	d->stride = 0;
}

// NV12 (BT.601 limited range) → tightly packed RGBA8888 (A=0xFF).
// y_stride is the Y-plane row pitch (bytes); UV plane follows at y_stride*h
// with the same row pitch (standard MF NV12 layout).
static void nv12_to_rgba(const uint8_t *nv12, UINT32 w, UINT32 h, UINT32 y_stride, uint8_t *rgba) {
	const uint8_t *yplane = nv12;
	const uint8_t *uvplane = nv12 + (size_t)y_stride * h;
	UINT32 x, y;
	if (y_stride < w) y_stride = w;
	for (y = 0; y < h; y++) {
		const uint8_t *Yrow = yplane + (size_t)y * y_stride;
		const uint8_t *UVrow = uvplane + (size_t)(y / 2) * y_stride;
		uint8_t *dst = rgba + (size_t)y * w * 4;
		for (x = 0; x < w; x++) {
			int Y = (int)Yrow[x] - 16;
			int U = (int)UVrow[(x & ~1u)] - 128;
			int V = (int)UVrow[(x & ~1u) + 1] - 128;
			if (Y < 0) Y = 0;
			// Fixed-point BT.601
			int R = (298 * Y + 409 * V + 128) >> 8;
			int G = (298 * Y - 100 * U - 208 * V + 128) >> 8;
			int B = (298 * Y + 516 * U + 128) >> 8;
			if (R < 0) R = 0; else if (R > 255) R = 255;
			if (G < 0) G = 0; else if (G > 255) G = 255;
			if (B < 0) B = 0; else if (B > 255) B = 255;
			dst[x * 4 + 0] = (uint8_t)R;
			dst[x * 4 + 1] = (uint8_t)G;
			dst[x * 4 + 2] = (uint8_t)B;
			dst[x * 4 + 3] = 0xFF;
		}
	}
}

// Ensure Annex-B start code at buffer start. Caller frees *out if *copied != 0.
static int ensure_annexb(const uint8_t *in, size_t len,
	const uint8_t **out, size_t *out_len, int *copied)
{
	*copied = 0;
	if (len == 0) return MF_ERR_BAD_INPUT;
	if ((len >= 3 && in[0] == 0 && in[1] == 0 && in[2] == 1) ||
	    (len >= 4 && in[0] == 0 && in[1] == 0 && in[2] == 0 && in[3] == 1)) {
		*out = in;
		*out_len = len;
		return 0;
	}
	uint8_t *buf = (uint8_t *)malloc(len + 4);
	if (!buf) return MF_ERR_NO_MEM;
	buf[0] = 0; buf[1] = 0; buf[2] = 0; buf[3] = 1;
	memcpy(buf + 4, in, len);
	*out = buf;
	*out_len = len + 4;
	*copied = 1;
	return 0;
}

static int mf_push_input(mf_dec *d, const uint8_t *data, size_t len) {
	IMFMediaBuffer *buf = NULL;
	IMFSample *sample = NULL;
	BYTE *lock = NULL;
	DWORD max_len = 0, cur = 0;
	HRESULT hr;

	hr = MFCreateMemoryBuffer((DWORD)len, &buf);
	if (FAILED(hr)) return (int)hr;
	hr = buf->lpVtbl->Lock(buf, &lock, &max_len, &cur);
	if (FAILED(hr)) { mf_release((IUnknown *)buf); return (int)hr; }
	memcpy(lock, data, len);
	buf->lpVtbl->Unlock(buf);
	buf->lpVtbl->SetCurrentLength(buf, (DWORD)len);

	hr = MFCreateSample(&sample);
	if (FAILED(hr)) { mf_release((IUnknown *)buf); return (int)hr; }
	hr = sample->lpVtbl->AddBuffer(sample, buf);
	mf_release((IUnknown *)buf);
	if (FAILED(hr)) { mf_release((IUnknown *)sample); return (int)hr; }

	// 100-ns units; monotonic timestamps keep the decoder happy.
	d->pts += 333333; // ~30 fps default step
	sample->lpVtbl->SetSampleTime(sample, d->pts);
	sample->lpVtbl->SetSampleDuration(sample, 333333);

	hr = d->xf->lpVtbl->ProcessInput(d->xf, 0, sample, 0);
	mf_release((IUnknown *)sample);
	return SUCCEEDED(hr) ? 0 : (int)hr;
}

static int mf_pull_rgba(mf_dec *d, uint8_t **rgba_out, int *w_out, int *h_out) {
	int attempts;
	*rgba_out = NULL;
	*w_out = 0;
	*h_out = 0;

	for (attempts = 0; attempts < 16; attempts++) {
		MFT_OUTPUT_DATA_BUFFER outb;
		MFT_OUTPUT_STREAM_INFO sinfo;
		IMFSample *sample = NULL;
		IMFMediaBuffer *mbuf = NULL;
		DWORD status = 0;
		HRESULT hr;
		int provides;

		memset(&outb, 0, sizeof(outb));
		memset(&sinfo, 0, sizeof(sinfo));
		d->xf->lpVtbl->GetOutputStreamInfo(d->xf, 0, &sinfo);
		provides = (sinfo.dwFlags & MFT_OUTPUT_STREAM_PROVIDES_SAMPLES) != 0;

		if (!provides) {
			DWORD need = d->sample_size;
			if (need == 0) need = sinfo.cbSize;
			if (need == 0) need = 1; // placeholder until STREAM_CHANGE

			hr = MFCreateSample(&sample);
			if (FAILED(hr)) return (int)hr;
			hr = MFCreateMemoryBuffer(need, &mbuf);
			if (FAILED(hr)) {
				mf_release((IUnknown *)sample);
				return (int)hr;
			}
			sample->lpVtbl->AddBuffer(sample, mbuf);
			mf_release((IUnknown *)mbuf);
			mbuf = NULL;
			outb.pSample = sample;
		}

		outb.dwStreamID = 0;
		hr = d->xf->lpVtbl->ProcessOutput(d->xf, 0, 1, &outb, &status);

		if (hr == MF_E_TRANSFORM_STREAM_CHANGE) {
			if (sample) mf_release((IUnknown *)sample);
			if (outb.pEvents) mf_release((IUnknown *)outb.pEvents);
			// Re-query output types after format change (MSDN stream-change flow).
			if (mf_set_nv12_output(d) != 0)
				return MF_ERR_PROCESS;
			continue;
		}

		if (hr == MF_E_TRANSFORM_NEED_MORE_INPUT) {
			if (sample) mf_release((IUnknown *)sample);
			if (outb.pEvents) mf_release((IUnknown *)outb.pEvents);
			return MF_ERR_NEED_MORE;
		}

		if (FAILED(hr)) {
			if (sample) mf_release((IUnknown *)sample);
			if (outb.pSample && outb.pSample != sample)
				mf_release((IUnknown *)outb.pSample);
			if (outb.pEvents) mf_release((IUnknown *)outb.pEvents);
			return (int)hr;
		}

		// Success — take sample from outb (MFT may replace our pointer).
		{
			IMFSample *got = outb.pSample ? outb.pSample : sample;
			IMFMediaBuffer *cont = NULL;
			BYTE *lock = NULL;
			DWORD max_len = 0, cur = 0;
			UINT32 w = d->width, h = d->height;
			UINT32 y_stride = d->stride ? d->stride : w;
			size_t need, rgba_sz;
			uint8_t *rgba;

			if (!got) {
				if (outb.pEvents) mf_release((IUnknown *)outb.pEvents);
				return MF_ERR_NO_FRAME;
			}

			hr = got->lpVtbl->ConvertToContiguousBuffer(got, &cont);
			if (FAILED(hr) || !cont) {
				mf_release((IUnknown *)got);
				if (outb.pEvents) mf_release((IUnknown *)outb.pEvents);
				return (int)hr;
			}
			hr = cont->lpVtbl->Lock(cont, &lock, &max_len, &cur);
			if (FAILED(hr) || !lock) {
				mf_release((IUnknown *)cont);
				mf_release((IUnknown *)got);
				if (outb.pEvents) mf_release((IUnknown *)outb.pEvents);
				return (int)hr;
			}

			if (w == 0 || h == 0) {
				// Geometry still unknown after STREAM_CHANGE — soft-skip.
				cont->lpVtbl->Unlock(cont);
				mf_release((IUnknown *)cont);
				mf_release((IUnknown *)got);
				if (outb.pEvents) mf_release((IUnknown *)outb.pEvents);
				return MF_ERR_NO_FRAME;
			}

			need = (size_t)y_stride * h * 3 / 2;
			if ((size_t)cur < need) {
				// Contiguous buffer may be tightly packed even when type stride
				// was padded — fall back to width-stride if sizes match.
				if ((size_t)cur >= (size_t)w * h * 3 / 2) {
					y_stride = w;
				} else {
					cont->lpVtbl->Unlock(cont);
					mf_release((IUnknown *)cont);
					mf_release((IUnknown *)got);
					if (outb.pEvents) mf_release((IUnknown *)outb.pEvents);
					return MF_ERR_PROCESS;
				}
			}

			rgba_sz = (size_t)w * h * 4;
			rgba = (uint8_t *)malloc(rgba_sz);
			if (!rgba) {
				cont->lpVtbl->Unlock(cont);
				mf_release((IUnknown *)cont);
				mf_release((IUnknown *)got);
				if (outb.pEvents) mf_release((IUnknown *)outb.pEvents);
				return MF_ERR_NO_MEM;
			}
			nv12_to_rgba(lock, w, h, y_stride, rgba);
			cont->lpVtbl->Unlock(cont);
			mf_release((IUnknown *)cont);
			mf_release((IUnknown *)got);
			if (outb.pEvents) mf_release((IUnknown *)outb.pEvents);

			*rgba_out = rgba;
			*w_out = (int)w;
			*h_out = (int)h;
			return MF_OK;
		}
	}
	return MF_ERR_PROCESS;
}

// --- exported to Go -------------------------------------------------------

static int g_mf_refcount = 0;
static int g_com_uninit = 0; // 1 if we must CoUninitialize on last shutdown

int mf_startup(void) {
	HRESULT hr;
	if (g_mf_refcount == 0) {
		hr = CoInitializeEx(NULL, COINIT_MULTITHREADED);
		if (hr == S_OK) {
			g_com_uninit = 1;
		} else if (hr == S_FALSE || hr == RPC_E_CHANGED_MODE) {
			// Already initialized on this thread, or different apartment —
			// COM is usable; do not uninit on shutdown.
			g_com_uninit = 0;
		} else {
			return (int)hr;
		}
		hr = MFStartup(MF_VERSION, MFSTARTUP_FULL);
		if (FAILED(hr)) {
			if (g_com_uninit) {
				CoUninitialize();
				g_com_uninit = 0;
			}
			return (int)hr;
		}
	}
	g_mf_refcount++;
	return 0;
}

void mf_shutdown(void) {
	if (g_mf_refcount <= 0) return;
	g_mf_refcount--;
	if (g_mf_refcount == 0) {
		MFShutdown();
		if (g_com_uninit) {
			CoUninitialize();
			g_com_uninit = 0;
		}
	}
}

mf_dec *mf_dec_create(void) {
	mf_dec *d = (mf_dec *)calloc(1, sizeof(*d));
	return d;
}

void mf_dec_destroy(mf_dec *d) {
	if (!d) return;
	mf_destroy_transform(d);
	free(d);
}

// Decode one Annex-B access unit into a malloc'd RGBA buffer (*rgba).
// Caller must free(*rgba) with mf_free. On non-zero return, *rgba is NULL.
int mf_dec_decode(mf_dec *d, const uint8_t *annexb, size_t len,
	int hint_w, int hint_h,
	uint8_t **rgba, int *out_w, int *out_h)
{
	const uint8_t *payload = NULL;
	size_t plen = 0;
	int copied = 0;
	int rc;

	*rgba = NULL;
	*out_w = 0;
	*out_h = 0;
	if (!d || !annexb || len == 0) return MF_ERR_BAD_INPUT;

	if (!d->xf) {
		rc = mf_create_transform(d,
			hint_w > 0 ? (UINT32)hint_w : 0,
			hint_h > 0 ? (UINT32)hint_h : 0);
		if (rc != 0) {
			mf_destroy_transform(d);
			// HRESULT (negative) or local error → process failure for Go.
			return MF_ERR_PROCESS;
		}
	}

	rc = ensure_annexb(annexb, len, &payload, &plen, &copied);
	if (rc != 0) return rc;

	rc = mf_push_input(d, payload, plen);
	if (copied) free((void *)payload);
	if (rc != 0) {
		// ProcessInput rejection — recreate transform on next AU.
		mf_destroy_transform(d);
		return MF_ERR_PROCESS;
	}

	rc = mf_pull_rgba(d, rgba, out_w, out_h);
	// Map raw HRESULT from pull path to a stable soft code.
	if (rc < 0)
		return MF_ERR_PROCESS;
	return rc;
}

void mf_free(void *p) {
	free(p);
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/maskraven/spice-viewer/internal/codec"
)

func available() bool { return true }

type mfDecoder struct {
	mu      sync.Mutex
	started bool
	dec     *C.mf_dec
}

func newDecoder() (Decoder, error) {
	if rc := C.mf_startup(); rc != 0 {
		return nil, fmt.Errorf("%w: MFStartup/CoInitialize 0x%x", ErrUnavailable, uint32(rc))
	}
	dec := C.mf_dec_create()
	if dec == nil {
		C.mf_shutdown()
		return nil, fmt.Errorf("%w: out of memory", ErrUnavailable)
	}
	return &mfDecoder{started: true, dec: dec}, nil
}

func (d *mfDecoder) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dec != nil {
		C.mf_dec_destroy(d.dec)
		d.dec = nil
	}
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
	if !d.started || d.dec == nil {
		return nil, ErrUnavailable
	}

	var rgba *C.uint8_t
	var ow, oh C.int
	rc := C.mf_dec_decode(
		d.dec,
		(*C.uint8_t)(unsafe.Pointer(&annexB[0])),
		C.size_t(len(annexB)),
		C.int(w),
		C.int(h),
		&rgba,
		&ow,
		&oh,
	)
	if rc != 0 {
		switch rc {
		case C.MF_ERR_NEED_MORE:
			return nil, fmt.Errorf("%w: need more input (params-only or buffered)", ErrDecode)
		case C.MF_ERR_BAD_INPUT:
			return nil, FormatError("bad access unit")
		case C.MF_ERR_NO_MEM:
			return nil, fmt.Errorf("%w: out of memory", ErrDecode)
		case C.MF_ERR_NO_FRAME:
			return nil, fmt.Errorf("%w: no frame produced", ErrDecode)
		case C.MF_ERR_PROCESS:
			return nil, fmt.Errorf("%w: ProcessInput/Output failed", ErrDecode)
		default:
			// Positive HRESULT from native path.
			return nil, fmt.Errorf("%w: Media Foundation HRESULT 0x%x", ErrDecode, uint32(rc))
		}
	}
	if rgba == nil || ow <= 0 || oh <= 0 {
		return nil, fmt.Errorf("%w: empty pixel buffer", ErrDecode)
	}
	defer C.mf_free(unsafe.Pointer(rgba))

	n := int(ow) * int(oh) * 4
	pix := C.GoBytes(unsafe.Pointer(rgba), C.int(n))
	return &codec.RGBA{
		Width:  int(ow),
		Height: int(oh),
		Stride: int(ow) * 4,
		Pix:    pix,
	}, nil
}
