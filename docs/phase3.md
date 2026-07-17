# Phase 3 — Parity stretch

Phase 3 extends the Proxmox-capable viewer toward spice-gtk parity where it still
matters for daily Windows/macOS use. Phase 1–2 remain pure Go on the critical
path; Phase 3 adds **optional platform backends** behind clear interfaces and
build tags so the default binary stays Apache-2.0 and does not link FFmpeg or
GPL spice-common.

## Goals

| Area | Target | Default binary |
|------|--------|----------------|
| **GLZ** | Pure Go dictionary decoder | Implemented (`codec.GLZWindow`); no cgo |
| **H.264 streams** | OS decoder (Win/Mac); user FFmpeg (Linux) | Never bundle FFmpeg in default release |
| **Record** | Mic / capture channel + driver | Best-effort open; NullRecord default |
| **USB redirection** | Channel + host filter hooks | Scaffold; host backend optional/platform |
| **WebDAV / port** | Folder share channel scaffold | Best-effort; no full FUSE share required |
| **Host audio sink** | Real playback beyond NullPlayback | Platform sink package; Null default for headless |

## H.264 decision

| Platform | Backend | Shipping |
|----------|---------|----------|
| **macOS** | VideoToolbox (`VTDecompressionSession`) | Built into the binary via system frameworks |
| **Windows** | Media Foundation `CLSID_CMSH264DecoderMFT` | Built into the binary via system APIs (cgo) |
| **Linux** | **User-provided FFmpeg** (libavcodec / `ffmpeg` CLI or dynload) | **Not bundled** — install via distro packages |

**Default product policy**

- Do **not** ship FFmpeg (or GPL/LGPL binary blobs) inside releases.
- On Linux, H.264 works only when a compatible FFmpeg is installed on the host
  and discoverable (see [Linux: install FFmpeg](#linux-install-ffmpeg)).
- Without FFmpeg on Linux: soft-skip H.264 streams (session continues; MJPEG/raw/LZ still work).
- Capability rule: **do not advertise H.264** when `h264.Available()` is false.

Rationale:

- Windows + macOS use royalty-free-to-the-app OS stacks (no extra install).
- Linux has no single “built-in” H.264 API portable across distros; FFmpeg is
  universal and already on most desktops if the user installs it.
- Keeps our Apache-2.0 binary free of a vendored FFmpeg copy and lets the
  operator choose a distro-legal package (including non-free variants if needed).

SPICE still owns framing (`STREAM_CREATE` / `STREAM_DATA` / clip). The decoder
backend only turns H.264 access units into pixels.

Package layout:

```text
internal/codec/h264/
  h264.go                 // Decoder interface, Available(), New()
  decode_darwin.go        // VideoToolbox (+ cgo)
  decode_windows.go       // Media Foundation CLSID_CMSH264DecoderMFT (+ cgo) → NV12→RGBA
  decode_windows_nocgo.go // Windows CGO_ENABLED=0 fallback (Available true, soft-skip)
  decode_linux.go         // User FFmpeg CLI subprocess (PATH probe); Available only if found
  decode_stub.go          // other GOOS (!darwin && !windows && !linux)
```

Linux backend starts one stateful `ffmpeg` process per stream decoder:

```text
ffmpeg -hide_banner -loglevel error \
  -probesize 32 -analyzeduration 0 \
  -fflags nobuffer -flags low_delay \
  -f h264 -i pipe:0 \
  -f rawvideo -pix_fmt rgba pipe:1
```

Dimensions come from `STREAM_CREATE` hints or a minimal SPS parse when hints are 0.

### Windows Media Foundation path

cgo build (`windows && cgo`) in `decode_windows.go`:

1. `CoInitializeEx` + `MFStartup` (process refcounted)
2. `CoCreateInstance(CLSID_CMSH264DecoderMFT)` → `IMFTransform`
3. Input type: `MFMediaType_Video` / `MFVideoFormat_H264` (Annex-B with start codes; optional frame size from `STREAM_CREATE`)
4. Output type: preferred `MFVideoFormat_NV12` via `GetOutputAvailableType` / `SetOutputType`
5. Low-latency best-effort: `CODECAPI_AVLowLatencyMode`
6. Per access unit: `ProcessInput` → `ProcessOutput` loop, handling `MF_E_TRANSFORM_STREAM_CHANGE` (re-negotiate NV12 geometry)
7. Software NV12 → RGBA8888 (BT.601 limited range); soft-skip on need-more-input / bad frames

`CGO_ENABLED=0` uses `decode_windows_nocgo.go` (`Available()==true`, Decode soft-fails) so pure-Go cross-compiles still advertise the cap consistently with cgo product builds.

### Linux: install FFmpeg

Install a system FFmpeg that provides **H.264 decode** (libavcodec with
`h264`). Package names differ by distro; pick the one your distribution
documents for multimedia.

#### Debian / Ubuntu

```bash
sudo apt update
sudo apt install ffmpeg
```

Optional (development / linking against libs if building from source with
`pkg-config`):

```bash
sudo apt install libavcodec-dev libavutil-dev libswscale-dev pkg-config
```

Some Ubuntu flavors also ship restricted codecs via:

```bash
sudo apt install ubuntu-restricted-extras   # optional; pulls many codecs
```

#### Fedora

```bash
sudo dnf install ffmpeg
```

If `ffmpeg` is missing from default repos, enable RPM Fusion (per
[rpmfusion.org](https://rpmfusion.org/Configuration)), then:

```bash
sudo dnf install ffmpeg
```

#### Arch / Manjaro

```bash
sudo pacman -S ffmpeg
```

#### openSUSE

```bash
sudo zypper install ffmpeg
```

(Packman repos may be needed for full codec sets on some versions.)

#### Flatpak / immutable desktops

If the app is installed as a Flatpak, it may **not** see host FFmpeg. Prefer a
native package build, or document that H.264 is unavailable inside a
sandbox without an FFmpeg extension. AppImage/tarball builds expect host
`ffmpeg` on `PATH` or `libavcodec` via dynamic loader paths.

#### Verify

```bash
ffmpeg -hide_banner -decoders 2>/dev/null | grep -i h264
# expect a line containing h264

# optional: library presence
pkg-config --exists libavcodec && echo "libavcodec OK"
```

If decode still fails after install:

1. Confirm `remote-viewer` is the native binary (not a sandbox without host libs).
2. Check status/logs for `h264: OS decoder unavailable` / FFmpeg probe errors.
3. Ensure the guest is actually sending H.264 streams (many Proxmox/QXL guests
   use LZ/JPEG/MJPEG only — no FFmpeg needed for those).

#### What we will *not* do

- Bundle a private `ffmpeg` binary inside the release tarball by default.
- Require FFmpeg for basic Proxmox console (raw/LZ/JPEG/MJPEG/agent still work).
- Auto-download FFmpeg at runtime.

## GLZ

Pure Go only (Apache-2.0). No spice-common / cgo link.

| Piece | Location |
|-------|----------|
| Bitstream + RGB16/24/32/RGBA | `internal/codec/glz_decode.go` |
| Dictionary window (image id) | `internal/codec/glz_window.go` |
| `Decode` / ZLIB unwrap | `internal/codec/glz.go` |
| Display wiring | `Display.glzWin` + `resolveImage` for `GLZ_RGB` / `ZLIB_GLZ_RGB` |

Dictionary window sized via `DISPLAY_INIT` `glz_dictionary_window_size`
(`protocol.DisplayGlzWindowBytes`, ~16 MiB). Encoder `win_head_dist` plus a
local byte budget drive eviction. Decode failures soft-skip the draw without
black-filling dest (Phase 2 trail policy). Stateless `DecodeSpiceImage` still
returns `UnsupportedImageError` for GLZ — callers must use `GLZWindow`.

## Record / USB / WebDAV

| Channel | Open policy | Status | Product behavior |
|---------|-------------|--------|------------------|
| **Record** | Best-effort | **Scaffold landed** | `RecordDriver` / `NullRecord` default; on `RECORD_START` client sends `MODE=RAW` + `START_MARK`; **no PCM frames** from NullRecord (silent mic; silence timer omitted). Caps: `VOLUME` only (no OPUS). |
| **USB redir** | Best-effort | **Scaffold landed** | Opens every listed usbredir channel id; SpiceVMC `DATA` accept/discard (+ optional `VMCHandler` queue); `USBFilter` hook (default allow-all). **No libusb** in default binary. Compressed VMC discarded (LZ4 cap not advertised). |
| **WebDAV** | Best-effort | **Scaffold landed** | Port+VMC message loop; `PORT_INIT` → client `PORT_EVENT_OPENED`; optional `ConnectConfig.ShareDir` / CLI `--share-dir` (partial share UX). `internal/webdav` client_id framing helpers only. |
| **Port** (non-WebDAV) | Never | Not opened | No dedicated consumer yet |

**Policy:** open failures and runtime decode errors are **never session-fatal**. Session continues without mic/USB/share when channels are absent or fail.

### Package layout (channels)

```text
internal/protocol/record.go   // RECORD_* encode/decode
internal/protocol/vmc.go      // SpiceVMC DATA / COMPRESSED + PORT_INIT/EVENT
internal/channel/record.go    // Record + NullRecord
internal/channel/vmc.go       // shared VMC parse/send helpers
internal/channel/usbredir.go  // USB redir scaffold
internal/channel/webdav.go    // WebDAV scaffold
internal/webdav/              // optional client_id frame helpers
pkg/spice/record.go           // public RecordDriver / NullRecord
```

## Host audio

`PlaybackDriver` remains the public hook for RAW S16LE PCM from the playback
channel. Phase 3 adds `internal/audio`:

| Platform | Backend | Shipping |
|----------|---------|----------|
| **macOS** | [ebitengine/oto/v3](https://github.com/ebitengine/oto) (Core Audio via purego) | Built in; no cgo |
| **Windows** | oto (WASAPI via purego) | Built in; no cgo |
| **Linux** | Stub (`Available() == false`) | Silent until ALSA/Pulse path lands |
| **Headless** | `NullPlayback` | Explicit in `--headless` |

**Product policy**

- GUI (`internal/ui.RunGUI`): when `Drivers.Playback` is nil, call
  `audio.OpenDefault()`; if non-nil, attach it. Init failure → log and continue
  with NullPlayback semantics (session never fails for audio).
- `--headless`: always `spice.NewNullPlayback()` (no host device).
- Build tag `noaudio` forces the stub on every GOOS (silent CI builds).

Package layout:

```text
internal/audio/
  doc.go
  sink.go          // OpenDefault, Sink, PCM ring buffer, volume helpers
  sink_oto.go      // darwin/windows oto backend
  sink_stub.go     // linux / other / -tags=noaudio
  sink_test.go     // start/stop/write without requiring hardware
```

## Out of scope (still)

- GPL hybrid spice-common
- Bundled FFmpeg in default release
- Full USB host stack parity with spice-gtk
- Multi-monitor agent stretch goals beyond Phase 2 monitors-config

## Acceptance (measurable)

1. On macOS (VideoToolbox) or Windows (Media Foundation MFT → RGBA) with H.264 guest stream: frames present without FFmpeg.
2. Linux without FFmpeg: soft-skip H.264; session + other codecs still work.
3. Linux with distro FFmpeg installed and discoverable: H.264 streams decode (when backend lands).
4. Record/USB/WebDAV open as best-effort when listed; session survives absence.
5. `docs/phase3.md` + CHANGELOG list platform backends and gaps vs spice-gtk.
6. Manual Proxmox checklist remains operator-owned for live sign-off.
7. GUI on macOS/Windows: host audio plays when the guest sends RAW PCM; headless stays silent.

## PR-style work units

1. **proto+docs** — constants, phase3 guide, H.264 decision
2. **h264** — OS decoder + display stream wire-up
3. **glz** — pure Go decoder + image path
4. **channels** — record / usbredir / webdav + session open
5. **audio** — host playback sink package
6. **acceptance** — README/CHANGELOG/checklist updates
