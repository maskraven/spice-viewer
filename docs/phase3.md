# Phase 3 — Parity stretch

Phase 3 extends the Proxmox-capable viewer toward spice-gtk parity where it still
matters for daily Windows/macOS use. Phase 1–2 remain pure Go on the critical
path; Phase 3 adds **optional platform backends** behind clear interfaces and
build tags so the default binary stays Apache-2.0 and does not link FFmpeg or
GPL spice-common.

## Goals

| Area | Target | Default binary |
|------|--------|----------------|
| **GLZ** | Pure Go dictionary decoder | Soft-skip until decode lands; no cgo |
| **H.264 streams** | OS decoder (Win/Mac); user FFmpeg (Linux) | Never bundle FFmpeg in default release |
| **Record** | Mic / capture channel + driver | Best-effort open; NullRecord default |
| **USB redirection** | Channel + host filter hooks | Scaffold; host backend optional/platform |
| **WebDAV / port** | Folder share channel scaffold | Best-effort; no full FUSE share required |
| **Host audio sink** | Real playback beyond NullPlayback | Platform sink package; Null default for headless |

## H.264 decision

| Platform | Backend | Shipping |
|----------|---------|----------|
| **macOS** | VideoToolbox (`VTDecompressionSession`) | Built into the binary via system frameworks |
| **Windows** | Media Foundation H.264 MFT | Built into the binary via system APIs |
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
  h264.go            // Decoder interface, Available(), New()
  decode_darwin.go   // VideoToolbox (+ cgo)
  decode_windows.go  // Media Foundation (+ cgo)
  decode_linux.go    // User FFmpeg (dynlink or subprocess); soft-skip if missing
  decode_stub.go     // other GOOS / no backend
```

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

Pure Go only (Apache-2.0). Dictionary window sized via `DISPLAY_INIT`
`glz_dictionary_window_size` (already sent). No spice-common link.

Until decode is complete, `SPICE_IMAGE_TYPE_GLZ_RGB` / `ZLIB_GLZ_RGB` continue
to soft-skip without black-filling dest (display freeze/trail policy from Phase 2).

## Record / USB / WebDAV

| Channel | Open policy | Product behavior |
|---------|-------------|------------------|
| Record | Best-effort | `RecordDriver` for PCM capture; NullRecord discards server mode requests |
| USB redir | Best-effort | Protocol scaffold + filter; no forced libusb |
| WebDAV / Port | Best-effort | Message loop scaffold; full share UX later |

Never session-fatal if these fail to open.

## Host audio

`PlaybackDriver` remains the public hook. Phase 3 adds
`internal/audio` platform sinks that implement it (Core Audio / WASAPI via thin
backends or a pure-Go library). Default product path may still use NullPlayback
until the UI opts in.

## Out of scope (still)

- GPL hybrid spice-common
- Bundled FFmpeg in default release
- Full USB host stack parity with spice-gtk
- Multi-monitor agent stretch goals beyond Phase 2 monitors-config

## Acceptance (measurable)

1. On macOS or Windows with H.264 guest stream: frames present without FFmpeg.
2. Linux without FFmpeg: soft-skip H.264; session + other codecs still work.
3. Linux with distro FFmpeg installed and discoverable: H.264 streams decode (when backend lands).
4. Record/USB/WebDAV open as best-effort when listed; session survives absence.
5. `docs/phase3.md` + CHANGELOG list platform backends and gaps vs spice-gtk.
6. Manual Proxmox checklist remains operator-owned for live sign-off.

## PR-style work units

1. **proto+docs** — constants, phase3 guide, H.264 decision
2. **h264** — OS decoder + display stream wire-up
3. **glz** — pure Go decoder + image path
4. **channels** — record / usbredir / webdav + session open
5. **audio** — host playback sink package
6. **acceptance** — README/CHANGELOG/checklist updates
