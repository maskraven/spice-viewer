# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0-beta] — 2026-07-17

First public **1.0 beta** of **spice-viewer**: Proxmox-first SPICE client with multi-platform packages.

### Changed

- **Product rename**: CLI and packages are **`spice-viewer`** (was `remote-viewer`).
  Paths: `cmd/spice-viewer`, desktop/MIME/icons, macOS **SPICE Viewer.app**, Windows setup.
  Go module path remains `github.com/maskraven/virt-viewer` for import stability.
- **Release CI**: multi-arch packages — Linux **amd64 + arm64** (native runners),
  macOS **universal**, Windows **amd64**; draft GitHub Release on `v*` tags.

### Packaging (all platforms)

#### Added

- **Linux**: hicolor icons, polished `.desktop` / MIME, nFPM **contents** (deb/rpm) with postinst caches,
  Fyne runtime deps, `ffmpeg` recommend, conflict with distro `virt-viewer`
- **macOS**: `SPICE Viewer.app` + UDZO **`.dmg`** scripts (universal arm64+amd64), `.vv` UTI in Info.plist
- **Windows**: GUI subsystem exe (`-H windowsgui`), zip, NSIS installer (`.vv` ProgID), HKCU associate script
- **CI**: `.github/workflows/release.yml` native matrix (ubuntu / macos / windows) → draft GitHub Release
- Docs: [packaging/README.md](packaging/README.md)

### Phase 3 — Parity stretch (in progress)

#### Added

- **Performance profiles** (product layer on SPICE prefs, not a protocol profile type):
  - CLI `--profile=default|lan|wan|quality`; GUI **Profile** menu
  - Sends `PREFERRED_COMPRESSION` + `PREFERRED_VIDEO_CODEC_TYPE` after DISPLAY_INIT
  - Server may ignore when QEMU pins image-compression / WAN options
- **Right Control** releases grab (in addition to Ctrl+Alt+R); not injected while grabbed
- **Record / USB redir / WebDAV channel scaffolds** (best-effort; never session-fatal):
  - Record: `NullRecord` default; `MODE=RAW` + `START_MARK` on START; no PCM from null driver
  - USB redir: multi-id open; SpiceVMC DATA discard loop; optional filter hook; no libusb
  - WebDAV: Port+VMC loop; optional `--share-dir` / `ConnectConfig.ShareDir`; partial share UX
  - Protocol constants + `internal/protocol/{record,vmc}.go`; shared `internal/channel/vmc.go`
  - See [docs/phase3.md](docs/phase3.md#record--usb--webdav)

- **Host audio sink** (`internal/audio`):
  - GUI `OpenDefault()` → host playback on **macOS/Windows** via ebitengine/oto (purego; RAW S16LE)
  - **Linux** stub (`Available()==false`) until ALSA/Pulse lands
  - `--headless` keeps `NullPlayback`; init failure is never session-fatal
  - See [docs/phase3.md](docs/phase3.md#host-audio)
- **H.264 display streams** (`internal/codec/h264`):
  - **macOS**: VideoToolbox (`VTDecompressionSession`) via cgo
  - **Windows**: Media Foundation `CLSID_CMSH264DecoderMFT` via cgo — Annex-B input, NV12→RGBA output (`Available()=true`; `CGO_ENABLED=0` soft-skips)
  - **Linux**: user-provided **FFmpeg** CLI on `PATH` (never bundled; `Available()` only when probe finds h264)
  - Display channel advertises `MULTI_CODEC` + `CODEC_MJPEG` always; `CODEC_H264` only when `h264.Available()`
  - STREAM_CREATE/DATA soft-skips decode failures; install notes in [docs/phase3.md](docs/phase3.md#linux-install-ffmpeg)
- **GLZ** (pure Go): `codec.GLZWindow` dictionary decoder for `GLZ_RGB` / `ZLIB_GLZ_RGB`;
  display `resolveImage` path; soft-skip on decode error (no black fill). No cgo/spice-common.

#### Documentation

- Phase 3 landing status: [docs/phase3.md](docs/phase3.md) acceptance (automated vs manual),
  [docs/acceptance-v0.1.md](docs/acceptance-v0.1.md#phase-3-acceptance-parity-stretch),
  [docs/phase2.md](docs/phase2.md) “landed vs still open”, README H.264 backend policy

#### Planned (Phase 3+)

- Real mic capture (`RecordDriver` host backend) and USB host stack (libusb/platform)
- Full WebDAV/phodav share UX beyond message-loop scaffold
- Linux host audio (ALSA/Pulse) beyond the current stub
- Live Proxmox operator sign-off (still pending; not claimed done)

### Phase 2 — Desktop comfort

#### Added

- **vdagent** (`internal/agent`, `pkg/spice`): capability announce, UTF-8 clipboard grab/request/data, monitors config (guest resize)
  - GUI **Edit → Copy from guest / Paste to guest**; status `agent=on/off`
  - Requires guest `spice-vdagent` (or Windows SPICE tools)
- **Codecs**: Quic (RGB24/32), JPEG / JPEG-Alpha, MJPEG display streams
- **Audio**: playback channel best-effort (RAW S16LE PCM → `PlaybackDriver`; default NullPlayback)
- **UI**: Send Keys menu, Type text…, toolbar CAD/Ungrab/Fullscreen/Paste
- **Packaging scaffold**: `.goreleaser.yaml`, `packaging/spice-viewer.desktop`, MIME `application/x-virt-viewer`

## [0.1.0] — Phase 1 Proxmox MVP

First cut line for a library-first SPICE client aimed at Proxmox Console `.vv` files.

### Added

- **Product binary** `cmd/spice-viewer`: open a virt-viewer / Proxmox `.vv` file
  - GUI default (Fyne): display present, keyboard grab, hotkeys (secure-attention → guest CAD, release-cursor, toggle-fullscreen)
  - `--headless`: NullDriver session for CI and dogfood
  - Honors `delete-this-file=1` before dial
  - Stable stderr messages via `internal/ux` (ticket expiry, proxy, TLS, transport)
- **Public libraries**
  - `pkg/vvfile` — parse Proxmox-shaped connection files (opaque host, CA `\n` escapes, password bounds)
  - `pkg/spice` — `Connect`, `ConnectConfigFromVV`, events, password ownership, NullDriver
- **Stack (internal)**
  - HTTP CONNECT spiceproxy with opaque multi-colon host authority
  - TLS chain verify + `host-subject` DN pin
  - AuthSpice RSA-OAEP-SHA1 tickets; multi-channel link (main, display, inputs; cursor best-effort)
  - Display raw + LZ image decode; inputs mouse modes and scancodes
  - Security helpers: ticket encrypt, wipe, log redaction
- **Docs**
  - `docs/design-spice-viewer-go.md` — systems design
  - `docs/proxmox.md` — operator guide, troubleshooting, manual checklist
  - `docs/acceptance-v0.1.md` — automated vs manual acceptance; tag readiness
  - `scripts/milestone0_memo.md` — normative upstream pins for crypto/CONNECT/DN
- **CI / harness**
  - GitHub Actions: gofmt, vet, `go test ./...`, import boundary script
  - Optional QEMU integration (`//go:build integration`, not default CI)
  - Fixtures under `testdata/` (`.vv`, certs, ticket vectors)

### Security

- Tickets and passwords are not logged; session Close wipes session-owned password copies
- Product path deletes on-disk `.vv` when requested before CONNECT
- No auto-reconnect for short-lived Proxmox tickets (avoids silent re-auth with a dead ticket)

### Known limitations

- **Manual Proxmox lab** is pending operator sign-off (no live Proxmox in CI)
- No vdagent, audio, USB redirection, GLZ, or video streaming in this release
- HiDPI is best-effort present-path scaling only
- Default link-time `Version` is `dev` unless set with `-ldflags`

### Tag readiness (historical v0.1)

See [docs/acceptance-v0.1.md](docs/acceptance-v0.1.md#tag-readiness-v010).

[Unreleased]: https://github.com/maskraven/virt-viewer/compare/v1.0.0-beta...HEAD
[1.0.0-beta]: https://github.com/maskraven/virt-viewer/releases/tag/v1.0.0-beta
[0.1.0]: https://github.com/maskraven/virt-viewer/releases/tag/v0.1.0
