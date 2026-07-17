# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Phase 2 — Desktop comfort

#### Added

- **vdagent** (`internal/agent`, `pkg/spice`): capability announce, UTF-8 clipboard grab/request/data, monitors config (guest resize)
  - GUI **Edit → Copy from guest / Paste to guest**; status `agent=on/off`
  - Requires guest `spice-vdagent` (or Windows SPICE tools)
- **Codecs**: Quic (RGB24/32), JPEG / JPEG-Alpha, MJPEG display streams
- **Audio**: playback channel best-effort (RAW S16LE PCM → `PlaybackDriver`; default NullPlayback)
- **UI**: Send Keys menu, Type text…, toolbar CAD/Ungrab/Fullscreen/Paste
- **Packaging scaffold**: `.goreleaser.yaml`, `packaging/remote-viewer.desktop`, MIME `application/x-virt-viewer`

#### Planned (Phase 3+)

- GLZ / H.264 (license-aware)
- USB redirection, WebDAV, record channel
- Host audio sink beyond NullPlayback (platform packages)
- Live Proxmox operator sign-off updates in `docs/acceptance-v0.1.md`

## [0.1.0] — Phase 1 Proxmox MVP

First cut line for a library-first SPICE client aimed at Proxmox Console `.vv` files.

### Added

- **Product binary** `cmd/remote-viewer`: open a virt-viewer / Proxmox `.vv` file
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

### Tag readiness

See [docs/acceptance-v0.1.md](docs/acceptance-v0.1.md#tag-readiness-v010). Creating the `v0.1.0` git tag is an operator step after automated gates (and preferably a signed Proxmox checklist) are green.

[Unreleased]: https://github.com/maskraven/virt-viewer/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/maskraven/virt-viewer/releases/tag/v0.1.0
