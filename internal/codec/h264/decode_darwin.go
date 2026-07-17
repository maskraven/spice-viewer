// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package h264

/*
#cgo LDFLAGS: -framework VideoToolbox -framework CoreMedia -framework CoreFoundation -framework CoreVideo

#include <VideoToolbox/VideoToolbox.h>
#include <CoreMedia/CoreMedia.h>
#include <CoreVideo/CoreVideo.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	CVPixelBufferRef pix;
	OSStatus status;
} decode_result_t;

static void decompress_callback(
	void *decompressionOutputRefCon,
	void *sourceFrameRefCon,
	OSStatus status,
	VTDecodeInfoFlags infoFlags,
	CVImageBufferRef imageBuffer,
	CMTime presentationTimeStamp,
	CMTime presentationDuration)
{
	decode_result_t *out = (decode_result_t *)sourceFrameRefCon;
	if (out == NULL) return;
	out->status = status;
	if (status == noErr && imageBuffer != NULL) {
		out->pix = CVPixelBufferRetain((CVPixelBufferRef)imageBuffer);
	}
}

// parse_annexb_param_sets extracts first SPS and PPS NAL payloads (without start code).
// Returns 0 on success. Caller frees *sps and *pps with free().
static int parse_annexb_param_sets(const uint8_t *data, size_t len,
	uint8_t **sps, size_t *sps_len, uint8_t **pps, size_t *pps_len)
{
	*sps = NULL; *pps = NULL; *sps_len = 0; *pps_len = 0;
	size_t i = 0;
	while (i + 4 < len) {
		size_t sc = 0;
		if (data[i] == 0 && data[i+1] == 0 && data[i+2] == 1) sc = 3;
		else if (data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1) sc = 4;
		else { i++; continue; }
		size_t nal_start = i + sc;
		size_t j = nal_start;
		while (j + 3 < len) {
			if (data[j] == 0 && data[j+1] == 0 && (data[j+2] == 1 || (j+3 < len && data[j+2] == 0 && data[j+3] == 1)))
				break;
			j++;
		}
		if (j > len) j = len;
		size_t nal_len = j - nal_start;
		if (nal_len < 1) { i = j; continue; }
		uint8_t ntype = data[nal_start] & 0x1f;
		if (ntype == 7 && *sps == NULL) {
			*sps = (uint8_t *)malloc(nal_len);
			if (!*sps) return -1;
			memcpy(*sps, data + nal_start, nal_len);
			*sps_len = nal_len;
		} else if (ntype == 8 && *pps == NULL) {
			*pps = (uint8_t *)malloc(nal_len);
			if (!*pps) return -1;
			memcpy(*pps, data + nal_start, nal_len);
			*pps_len = nal_len;
		}
		i = j;
		if (*sps && *pps) return 0;
	}
	return (*sps && *pps) ? 0 : -2;
}

// annexb_to_avcc converts Annex-B to length-prefixed AVCC (4-byte BE lengths).
// Caller frees *out with free().
static int annexb_to_avcc(const uint8_t *data, size_t len, uint8_t **out, size_t *out_len)
{
	// Worst case same size + a few length prefixes.
	uint8_t *buf = (uint8_t *)malloc(len + 64);
	if (!buf) return -1;
	size_t o = 0;
	size_t i = 0;
	while (i + 3 < len) {
		size_t sc = 0;
		if (data[i] == 0 && data[i+1] == 0 && data[i+2] == 1) sc = 3;
		else if (i + 4 <= len && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1) sc = 4;
		else { i++; continue; }
		size_t nal_start = i + sc;
		size_t j = nal_start;
		while (j + 3 < len) {
			if (data[j] == 0 && data[j+1] == 0 && (data[j+2] == 1 || (j+3 < len && data[j+2] == 0 && data[j+3] == 1)))
				break;
			j++;
		}
		if (j > len) j = len;
		size_t nal_len = j - nal_start;
		if (nal_len == 0) { i = j; continue; }
		// skip AUD
		uint8_t ntype = data[nal_start] & 0x1f;
		if (ntype == 9) { i = j; continue; }
		buf[o++] = (uint8_t)((nal_len >> 24) & 0xff);
		buf[o++] = (uint8_t)((nal_len >> 16) & 0xff);
		buf[o++] = (uint8_t)((nal_len >> 8) & 0xff);
		buf[o++] = (uint8_t)(nal_len & 0xff);
		memcpy(buf + o, data + nal_start, nal_len);
		o += nal_len;
		i = j;
	}
	*out = buf;
	*out_len = o;
	return o > 0 ? 0 : -2;
}

static OSStatus create_format_desc(const uint8_t *sps, size_t sps_len,
	const uint8_t *pps, size_t pps_len, CMVideoFormatDescriptionRef *fmt)
{
	const uint8_t *sets[2] = { sps, pps };
	const size_t sizes[2] = { sps_len, pps_len };
	return CMVideoFormatDescriptionCreateFromH264ParameterSets(
		kCFAllocatorDefault, 2, sets, sizes, 4, fmt);
}

static OSStatus create_session(CMVideoFormatDescriptionRef fmt, VTDecompressionSessionRef *sess)
{
	CFMutableDictionaryRef dst = CFDictionaryCreateMutable(NULL, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	// Request 32BGRA for easy copy to RGBA.
	int32_t pixelFormat = kCVPixelFormatType_32BGRA;
	CFNumberRef pf = CFNumberCreate(NULL, kCFNumberSInt32Type, &pixelFormat);
	CFDictionarySetValue(dst, kCVPixelBufferPixelFormatTypeKey, pf);
	CFRelease(pf);

	VTDecompressionOutputCallbackRecord cb;
	cb.decompressionOutputCallback = decompress_callback;
	cb.decompressionOutputRefCon = NULL;

	OSStatus st = VTDecompressionSessionCreate(
		kCFAllocatorDefault, fmt, NULL, dst, &cb, sess);
	CFRelease(dst);
	return st;
}

static OSStatus decode_frame(VTDecompressionSessionRef sess, CMVideoFormatDescriptionRef fmt,
	const uint8_t *avcc, size_t avcc_len, CVPixelBufferRef *out_pix)
{
	CMBlockBufferRef block = NULL;
	OSStatus st = CMBlockBufferCreateWithMemoryBlock(
		kCFAllocatorDefault, (void *)avcc, avcc_len, kCFAllocatorNull,
		NULL, 0, avcc_len, 0, &block);
	if (st != noErr) return st;

	CMSampleBufferRef sample = NULL;
	size_t sampleSize = avcc_len;
	st = CMSampleBufferCreateReady(kCFAllocatorDefault, block, fmt,
		1, 0, NULL, 1, &sampleSize, &sample);
	CFRelease(block);
	if (st != noErr) return st;

	decode_result_t res;
	res.pix = NULL;
	res.status = -1;
	VTDecodeFrameFlags flags = kVTDecodeFrame_EnableAsynchronousDecompression;
	// Prefer sync path for SPICE single-frame blit.
	flags = 0;
	VTDecodeInfoFlags info = 0;
	st = VTDecompressionSessionDecodeFrame(sess, sample, flags, &res, &info);
	CFRelease(sample);
	if (st != noErr) return st;
	// Wait for async completion if needed.
	VTDecompressionSessionWaitForAsynchronousFrames(sess);
	if (res.status != noErr) return res.status;
	if (res.pix == NULL) return -1;
	*out_pix = res.pix;
	return noErr;
}

// C helpers take uintptr_t so Go can pass C.CVPixelBufferRef (cgo maps CF
// types as uintptr) without a *struct vs uintptr mismatch.
static void copy_bgra_to_rgba(uintptr_t pix_u, uint8_t *dst, size_t dst_stride,
	size_t *out_w, size_t *out_h)
{
	CVPixelBufferRef pix = (CVPixelBufferRef)pix_u;
	if (!pix) { *out_w = 0; *out_h = 0; return; }
	CVPixelBufferLockBaseAddress(pix, kCVPixelBufferLock_ReadOnly);
	size_t w = CVPixelBufferGetWidth(pix);
	size_t h = CVPixelBufferGetHeight(pix);
	size_t src_stride = CVPixelBufferGetBytesPerRow(pix);
	uint8_t *base = (uint8_t *)CVPixelBufferGetBaseAddress(pix);
	*out_w = w;
	*out_h = h;
	for (size_t y = 0; y < h; y++) {
		uint8_t *s = base + y * src_stride;
		uint8_t *d = dst + y * dst_stride;
		for (size_t x = 0; x < w; x++) {
			// BGRA → RGBA
			d[x*4+0] = s[x*4+2];
			d[x*4+1] = s[x*4+1];
			d[x*4+2] = s[x*4+0];
			d[x*4+3] = s[x*4+3];
		}
	}
	CVPixelBufferUnlockBaseAddress(pix, kCVPixelBufferLock_ReadOnly);
}

static void pix_release(uintptr_t pix_u) {
	CVPixelBufferRef pix = (CVPixelBufferRef)pix_u;
	if (pix) CVPixelBufferRelease(pix);
}
static size_t pix_width(uintptr_t pix_u) {
	CVPixelBufferRef pix = (CVPixelBufferRef)pix_u;
	return pix ? CVPixelBufferGetWidth(pix) : 0;
}
static size_t pix_height(uintptr_t pix_u) {
	CVPixelBufferRef pix = (CVPixelBufferRef)pix_u;
	return pix ? CVPixelBufferGetHeight(pix) : 0;
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/maskraven/virt-viewer/internal/codec"
)

func available() bool { return true }

type vtDecoder struct {
	mu         sync.Mutex
	sess       C.VTDecompressionSessionRef
	formatDesc C.CMVideoFormatDescriptionRef
	// last SPS/PPS to rebuild session when parameter sets change
	sps, pps []byte
}

func newDecoder() (Decoder, error) {
	return &vtDecoder{}, nil
}

func (d *vtDecoder) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.destroyLocked()
}

func (d *vtDecoder) destroyLocked() {
	if d.sess != 0 {
		C.VTDecompressionSessionInvalidate(d.sess)
		C.CFRelease(C.CFTypeRef(d.sess))
		d.sess = 0
	}
	if d.formatDesc != 0 {
		C.CFRelease(C.CFTypeRef(d.formatDesc))
		d.formatDesc = 0
	}
}

func (d *vtDecoder) Decode(annexB []byte, w, h int) (*codec.RGBA, error) {
	if len(annexB) == 0 {
		return nil, FormatError("empty access unit")
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	// Extract SPS/PPS if present and (re)build session.
	var spsPtr, ppsPtr *C.uint8_t
	var spsLen, ppsLen C.size_t
	rc := C.parse_annexb_param_sets((*C.uint8_t)(unsafe.Pointer(&annexB[0])), C.size_t(len(annexB)),
		&spsPtr, &spsLen, &ppsPtr, &ppsLen)
	if rc == 0 && spsPtr != nil && ppsPtr != nil {
		sps := C.GoBytes(unsafe.Pointer(spsPtr), C.int(spsLen))
		pps := C.GoBytes(unsafe.Pointer(ppsPtr), C.int(ppsLen))
		C.free(unsafe.Pointer(spsPtr))
		C.free(unsafe.Pointer(ppsPtr))
		if !bytesEqual(sps, d.sps) || !bytesEqual(pps, d.pps) {
			if err := d.rebuildSession(sps, pps); err != nil {
				return nil, err
			}
		}
	} else if spsPtr != nil {
		C.free(unsafe.Pointer(spsPtr))
	} else if ppsPtr != nil {
		C.free(unsafe.Pointer(ppsPtr))
	}

	if d.sess == 0 || d.formatDesc == 0 {
		return nil, fmt.Errorf("%w: waiting for SPS/PPS (no format yet)", ErrDecode)
	}

	var avcc *C.uint8_t
	var avccLen C.size_t
	if C.annexb_to_avcc((*C.uint8_t)(unsafe.Pointer(&annexB[0])), C.size_t(len(annexB)), &avcc, &avccLen) != 0 {
		return nil, FormatError("annex-b to avcc failed")
	}
	defer C.free(unsafe.Pointer(avcc))

	var pix C.CVPixelBufferRef
	st := C.decode_frame(d.sess, d.formatDesc, avcc, avccLen, &pix)
	if st != 0 {
		return nil, fmt.Errorf("%w: VT status %d", ErrDecode, int(st))
	}
	pixU := C.uintptr_t(pix)
	defer C.pix_release(pixU)

	// Dimensions from pixel buffer.
	cw := C.pix_width(pixU)
	ch := C.pix_height(pixU)
	if cw == 0 || ch == 0 {
		return nil, FormatError("zero-size pixel buffer")
	}
	// Honor stream hints only as sanity (not scaling).
	_ = w
	_ = h

	out := &codec.RGBA{
		Width:  int(cw),
		Height: int(ch),
		Stride: int(cw) * 4,
		Pix:    make([]byte, int(cw)*int(ch)*4),
	}
	var ow, oh C.size_t
	C.copy_bgra_to_rgba(pixU, (*C.uint8_t)(unsafe.Pointer(&out.Pix[0])), C.size_t(out.Stride), &ow, &oh)
	return out, nil
}

func (d *vtDecoder) rebuildSession(sps, pps []byte) error {
	d.destroyLocked()
	var formatDesc C.CMVideoFormatDescriptionRef
	st := C.create_format_desc(
		(*C.uint8_t)(unsafe.Pointer(&sps[0])), C.size_t(len(sps)),
		(*C.uint8_t)(unsafe.Pointer(&pps[0])), C.size_t(len(pps)),
		&formatDesc,
	)
	if st != 0 {
		return fmt.Errorf("%w: format desc status %d", ErrUnavailable, int(st))
	}
	var sess C.VTDecompressionSessionRef
	st = C.create_session(formatDesc, &sess)
	if st != 0 {
		C.CFRelease(C.CFTypeRef(formatDesc))
		return fmt.Errorf("%w: VT session status %d", ErrUnavailable, int(st))
	}
	d.formatDesc = formatDesc
	d.sess = sess
	d.sps = append([]byte(nil), sps...)
	d.pps = append([]byte(nil), pps...)
	return nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
