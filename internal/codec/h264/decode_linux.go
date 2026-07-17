// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package h264

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/maskraven/spice-viewer/internal/codec"
)

// Linux H.264 uses a **user-provided** FFmpeg CLI on PATH (never bundled).
// See docs/phase3.md for install notes.

var (
	probeOnce sync.Once
	probeOK   bool
	ffmpegBin string
)

func available() bool {
	probeOnce.Do(func() {
		path, err := exec.LookPath("ffmpeg")
		if err != nil {
			return
		}
		// Prefer a binary that actually lists an h264 decoder.
		cmd := exec.Command(path, "-hide_banner", "-decoders")
		out, err := cmd.CombinedOutput()
		if err != nil {
			// Some minimal builds still decode; accept if binary exists.
			// But prefer failing closed when -decoders fails hard.
			if len(out) == 0 {
				return
			}
		}
		if !bytes.Contains(bytes.ToLower(out), []byte("h264")) {
			return
		}
		ffmpegBin = path
		probeOK = true
	})
	return probeOK
}

type ffmpegDecoder struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	// stderr is drained in a goroutine to avoid blocking ffmpeg.
	stderr io.ReadCloser
	errBuf bytes.Buffer
	errMu  sync.Mutex

	w, h   int
	closed bool
}

func newDecoder() (Decoder, error) {
	if !available() {
		return nil, fmt.Errorf("%w: system ffmpeg with h264 decoder not found on PATH", ErrUnavailable)
	}
	return &ffmpegDecoder{}, nil
}

func (d *ffmpegDecoder) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closeLocked()
}

func (d *ffmpegDecoder) closeLocked() {
	if d.closed {
		return
	}
	d.closed = true
	if d.stdin != nil {
		_ = d.stdin.Close()
		d.stdin = nil
	}
	if d.stdout != nil {
		_ = d.stdout.Close()
		d.stdout = nil
	}
	if d.stderr != nil {
		_ = d.stderr.Close()
		d.stderr = nil
	}
	if d.cmd != nil && d.cmd.Process != nil {
		_ = d.cmd.Process.Kill()
		_, _ = d.cmd.Process.Wait()
		d.cmd = nil
	}
}

func (d *ffmpegDecoder) Decode(annexB []byte, w, h int) (*codec.RGBA, error) {
	if len(annexB) == 0 {
		return nil, FormatError("empty access unit")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, ErrUnavailable
	}

	// Prefer STREAM_CREATE hints; fall back to SPS parse.
	if w > 0 && h > 0 {
		d.w, d.h = w, h
	} else if sw, sh, ok := spsDimensions(annexB); ok {
		d.w, d.h = sw, sh
	}
	if d.w <= 0 || d.h <= 0 {
		// Keep feeding parameter sets; cannot size rawvideo output yet.
		if d.cmd == nil {
			// Still start ffmpeg so SPS/PPS land in the continuous bitstream.
			if err := d.startLocked(); err != nil {
				return nil, err
			}
		}
		if err := d.writeAU(annexB); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: waiting for dimensions (SPS or STREAM_CREATE w/h)", ErrDecode)
	}

	if d.cmd == nil {
		if err := d.startLocked(); err != nil {
			return nil, err
		}
	}

	if err := d.writeAU(annexB); err != nil {
		return nil, err
	}

	frameSize := d.w * d.h * 4
	if frameSize <= 0 || frameSize > 64<<20 {
		return nil, FormatError("invalid frame size %dx%d", d.w, d.h)
	}

	// Soft-timeout: parameter-only AUs produce no frame.
	pix, err := d.readFrame(frameSize, 200*time.Millisecond)
	if err != nil {
		if err == errNoFrame {
			return nil, fmt.Errorf("%w: no frame output (params-only or buffered)", ErrDecode)
		}
		return nil, fmt.Errorf("%w: %v", ErrDecode, err)
	}
	return &codec.RGBA{
		Width:  d.w,
		Height: d.h,
		Stride: d.w * 4,
		Pix:    pix,
	}, nil
}

func (d *ffmpegDecoder) startLocked() error {
	// Low-latency Annex-B → raw RGBA. Dimensions come from the bitstream
	// (and our read size from STREAM_CREATE / SPS).
	cmd := exec.Command(ffmpegBin,
		"-hide_banner", "-loglevel", "error",
		"-probesize", "32", "-analyzeduration", "0",
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-f", "h264", "-i", "pipe:0",
		"-f", "rawvideo", "-pix_fmt", "rgba",
		"pipe:1",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("%w: stdin pipe: %v", ErrUnavailable, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("%w: stdout pipe: %v", ErrUnavailable, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return fmt.Errorf("%w: stderr pipe: %v", ErrUnavailable, err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return fmt.Errorf("%w: start ffmpeg: %v", ErrUnavailable, err)
	}
	d.cmd = cmd
	d.stdin = stdin
	d.stdout = stdout
	d.stderr = stderr
	go d.drainStderr(stderr)
	return nil
}

func (d *ffmpegDecoder) drainStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		d.errMu.Lock()
		if d.errBuf.Len() < 4096 {
			d.errBuf.WriteString(line)
			d.errBuf.WriteByte('\n')
		}
		d.errMu.Unlock()
	}
}

func (d *ffmpegDecoder) stderrText() string {
	d.errMu.Lock()
	defer d.errMu.Unlock()
	return strings.TrimSpace(d.errBuf.String())
}

func (d *ffmpegDecoder) writeAU(annexB []byte) error {
	if d.stdin == nil {
		return ErrUnavailable
	}
	// Ensure Annex-B start code present for demuxer.
	var payload []byte
	if hasStartCode(annexB) {
		payload = annexB
	} else {
		payload = make([]byte, 0, 4+len(annexB))
		payload = append(payload, 0, 0, 0, 1)
		payload = append(payload, annexB...)
	}
	if _, err := d.stdin.Write(payload); err != nil {
		msg := d.stderrText()
		if msg != "" {
			return fmt.Errorf("%w: ffmpeg stdin: %v (%s)", ErrDecode, err, msg)
		}
		return fmt.Errorf("%w: ffmpeg stdin: %v", ErrDecode, err)
	}
	// Try to flush if the pipe supports it.
	type flusher interface{ Flush() error }
	if f, ok := d.stdin.(flusher); ok {
		_ = f.Flush()
	}
	return nil
}

var errNoFrame = fmt.Errorf("no frame")

func (d *ffmpegDecoder) readFrame(frameSize int, timeout time.Duration) ([]byte, error) {
	if d.stdout == nil {
		return nil, ErrUnavailable
	}
	// Prefer SetReadDeadline on the pipe so we never abandon a partial ReadFull
	// (which would desync the rawvideo byte stream).
	if f, ok := d.stdout.(*os.File); ok {
		_ = f.SetReadDeadline(time.Now().Add(timeout))
		defer func() { _ = f.SetReadDeadline(time.Time{}) }()
	}
	buf := make([]byte, frameSize)
	_, err := io.ReadFull(d.stdout, buf)
	if err != nil {
		if isTimeout(err) {
			return nil, errNoFrame
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			msg := d.stderrText()
			if msg != "" {
				return nil, fmt.Errorf("ffmpeg stdout closed: %s", msg)
			}
			return nil, err
		}
		return nil, err
	}
	return buf, nil
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
		return true
	}
	// Go 1.20+ path errors / deadline wrappers.
	return strings.Contains(err.Error(), "i/o timeout") ||
		strings.Contains(err.Error(), "deadline exceeded")
}

func hasStartCode(b []byte) bool {
	if len(b) >= 3 && b[0] == 0 && b[1] == 0 && b[2] == 1 {
		return true
	}
	if len(b) >= 4 && b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 1 {
		return true
	}
	return false
}

// spsDimensions walks Annex-B NALs and parses the first SPS for coded size.
func spsDimensions(annexB []byte) (w, h int, ok bool) {
	for _, nal := range splitAnnexB(annexB) {
		if len(nal) < 2 {
			continue
		}
		if nal[0]&0x1f != 7 { // SPS
			continue
		}
		return parseSPSSize(nal[1:])
	}
	return 0, 0, false
}

func splitAnnexB(data []byte) [][]byte {
	var nals [][]byte
	i := 0
	for i < len(data) {
		sc := 0
		if i+2 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			sc = 3
		} else if i+3 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			sc = 4
		} else {
			i++
			continue
		}
		start := i + sc
		j := start
		for j < len(data) {
			if j+2 < len(data) && data[j] == 0 && data[j+1] == 0 && data[j+2] == 1 {
				break
			}
			if j+3 < len(data) && data[j] == 0 && data[j+1] == 0 && data[j+2] == 0 && data[j+3] == 1 {
				break
			}
			j++
		}
		if j > start {
			nals = append(nals, data[start:j])
		}
		i = j
	}
	return nals
}

// parseSPSSize parses H.264 SPS RBSP (without NAL header) for coded width/height.
// Handles common profiles used by SPICE streams; not a full SPS decoder.
func parseSPSSize(rbsp []byte) (w, h int, ok bool) {
	data := unescapeEBSP(rbsp)
	br := &bitReader{data: data}
	if br.remaining() < 24 {
		return 0, 0, false
	}
	profileIDC := br.u(8)
	_ = br.u(8) // constraint flags
	_ = br.u(8) // level_idc
	if _, ok := br.ue(); !ok {
		return 0, 0, false
	}
	// High profiles: chroma_format_idc and bit depths.
	switch profileIDC {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		chromaFormat, ok := br.ue()
		if !ok {
			return 0, 0, false
		}
		if chromaFormat == 3 {
			_ = br.u(1) // separate_colour_plane_flag
		}
		if _, ok = br.ue(); !ok { // bit_depth_luma_minus8
			return 0, 0, false
		}
		if _, ok = br.ue(); !ok { // bit_depth_chroma_minus8
			return 0, 0, false
		}
		_ = br.u(1) // qpprime_y_zero_transform_bypass_flag
		seqScaling := br.u(1)
		if seqScaling == 1 {
			// Skip scaling lists — uncommon for SPICE; abort soft if present.
			return 0, 0, false
		}
	}
	if _, ok := br.ue(); !ok { // log2_max_frame_num_minus4
		return 0, 0, false
	}
	pocType, ok := br.ue()
	if !ok {
		return 0, 0, false
	}
	switch pocType {
	case 0:
		if _, ok = br.ue(); !ok {
			return 0, 0, false
		}
	case 1:
		_ = br.u(1)
		if _, ok = br.se(); !ok {
			return 0, 0, false
		}
		if _, ok = br.se(); !ok {
			return 0, 0, false
		}
		n, ok := br.ue()
		if !ok {
			return 0, 0, false
		}
		for i := 0; i < int(n); i++ {
			if _, ok = br.se(); !ok {
				return 0, 0, false
			}
		}
	}
	if _, ok = br.ue(); !ok { // max_num_ref_frames
		return 0, 0, false
	}
	_ = br.u(1) // gaps_in_frame_num_value_allowed_flag
	picWMB, ok := br.ue()
	if !ok {
		return 0, 0, false
	}
	picHMap, ok := br.ue()
	if !ok {
		return 0, 0, false
	}
	frameMBSOnly := br.u(1)
	if frameMBSOnly == 0 {
		_ = br.u(1) // mb_adaptive_frame_field_flag
	}
	_ = br.u(1) // direct_8x8_inference_flag
	frameCropping := br.u(1)
	var cropL, cropR, cropT, cropB uint
	if frameCropping == 1 {
		cropL, ok = br.ue()
		if !ok {
			return 0, 0, false
		}
		cropR, ok = br.ue()
		if !ok {
			return 0, 0, false
		}
		cropT, ok = br.ue()
		if !ok {
			return 0, 0, false
		}
		cropB, ok = br.ue()
		if !ok {
			return 0, 0, false
		}
	}

	width := int((picWMB + 1) * 16)
	height := int((picHMap + 1) * 16)
	if frameMBSOnly == 0 {
		height *= 2
	}
	// Crop units assume 4:2:0 (crop * 2 for height/width offsets).
	width -= int(cropL+cropR) * 2
	height -= int(cropT+cropB) * 2
	if width <= 0 || height <= 0 || width > 8192 || height > 8192 {
		return 0, 0, false
	}
	return width, height, true
}

func unescapeEBSP(in []byte) []byte {
	if len(in) == 0 {
		return in
	}
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		// Remove emulation prevention 0x03 after 0x0000.
		if i+2 < len(in) && in[i] == 0 && in[i+1] == 0 && in[i+2] == 3 {
			out = append(out, 0, 0)
			i += 2
			continue
		}
		out = append(out, in[i])
	}
	return out
}

type bitReader struct {
	data []byte
	bit  int // bit offset from start
}

func (b *bitReader) remaining() int {
	return len(b.data)*8 - b.bit
}

func (b *bitReader) u(n int) uint {
	var v uint
	for i := 0; i < n; i++ {
		if b.bit >= len(b.data)*8 {
			return 0
		}
		byteIdx := b.bit / 8
		bitIdx := 7 - (b.bit % 8)
		v = (v << 1) | uint((b.data[byteIdx]>>bitIdx)&1)
		b.bit++
	}
	return v
}

func (b *bitReader) ue() (uint, bool) {
	zeros := 0
	for {
		if b.remaining() < 1 {
			return 0, false
		}
		if b.u(1) == 0 {
			zeros++
			if zeros > 31 {
				return 0, false
			}
			continue
		}
		break
	}
	if zeros == 0 {
		return 0, true
	}
	if b.remaining() < zeros {
		return 0, false
	}
	suf := b.u(zeros)
	return (1 << zeros) - 1 + suf, true
}

func (b *bitReader) se() (int, bool) {
	v, ok := b.ue()
	if !ok {
		return 0, false
	}
	// signed exp-Golomb: 0,1,-1,2,-2,...
	if v%2 == 0 {
		return -int(v / 2), true
	}
	return int((v + 1) / 2), true
}
