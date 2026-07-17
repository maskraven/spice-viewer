# spice-viewer

A greenfield, library-first [SPICE](https://www.spice-space.org/) remote display client written in Go.

**Module:** `github.com/maskraven/spice-viewer` · **License:** [Apache-2.0](LICENSE) · **CLI:** `spice-viewer`

## Status — **1.0-beta**

**spice-viewer 1.0-beta** ships a Proxmox-first SPICE client with multi-platform packages (Linux amd64/arm64, macOS universal, Windows amd64). Phase 2/3 features (agent clipboard, GLZ, H.264, host audio on macOS/Windows, packaging) are included; some channels remain scaffolds.

| Area | State |
|------|--------|
| Parse Proxmox / virt-viewer `.vv` | Implemented (`pkg/vvfile`) |
| HTTP CONNECT spiceproxy + TLS CA + `host-subject` pin | Implemented (`internal/connector`) |
| AuthSpice ticket, multi-channel session | Implemented (`pkg/spice`, session/channel) |
| Display raw + LZ + **Quic + JPEG + MJPEG** + **GLZ** | Implemented (`internal/codec`) |
| **H.264 streams** | OS decoder on **Windows/macOS**; **user-provided FFmpeg** on Linux (never bundled) |
| Inputs, cursor, **playback (best-effort)** | Implemented (GUI host sink on macOS/Windows via `internal/audio`; Linux audio stub; headless NullPlayback) |
| **vdagent**: clipboard text + monitors config | Implemented (`internal/agent`) — needs `spice-vdagent` in guest |
| Record / USB redir / WebDAV | **Scaffolds only** (best-effort open; no full USB host / real mic) |
| GUI: Send Keys, Edit copy/paste, grab, hotkeys | Implemented (Fyne) |
| Headless (`--headless`) | Implemented |
| Packaging | **Linux** deb/rpm/tar.gz · **macOS** `.app`/`.dmg` · **Windows** exe/zip/NSIS — see [packaging/README.md](packaging/README.md) |
| Live Proxmox lab acceptance | **Pending operator sign-off** (not in CI) |

There is **no** auto-reconnect for short-lived tickets: after expiry or disconnect, open Console again in Proxmox for a new `.vv`.

**H.264 policy:** macOS uses VideoToolbox and Windows uses Media Foundation (system APIs). Linux expects a distro **FFmpeg** with H.264 decode on `PATH` — we do **not** ship FFmpeg in the default binary. Without it, H.264 streams soft-skip and other codecs still work.

Changelog: [CHANGELOG.md](CHANGELOG.md).

## Goals (product)

- **Proxmox-first**: open a downloaded `pve-spice.vv` and establish a session through Proxmox’s HTTP CONNECT spiceproxy, TLS with embedded CA + `host-subject` verification, and SPICE ticket authentication.
- **Single binary** for macOS, Linux, and Windows (`cmd/spice-viewer`).
- **Library-first** (`pkg/spice`, `pkg/vvfile`) so CLI, GUI, and tooling share one session stack.
- **Phase 1 pure Go only** (Apache-2.0); no spice-common C, FFmpeg, or libusb.

## Install / build

Requires **Go 1.22+**. Product packages need **native** `CGO_ENABLED=1` builds (Fyne; VideoToolbox / Media Foundation on macOS / Windows).

```bash
git clone https://github.com/maskraven/spice-viewer.git
cd spice-viewer
go build -o spice-viewer ./cmd/spice-viewer
./spice-viewer -h
./spice-viewer -version
```

Optional release version stamp:

```bash
go build -ldflags "-X main.Version=v1.0.1-beta" -o spice-viewer ./cmd/spice-viewer
```

### Product packages (all platforms)

| OS | Command (on that OS) | Output |
|----|----------------------|--------|
| Linux | `./scripts/linux/build-product.sh` | `tar.gz`, deb, rpm (GoReleaser) |
| macOS | `VERSION=v1.0.1-beta ./scripts/macos/build-product.sh` | `.app`, `.dmg`, app zip |
| Windows | `.\scripts\windows\build-product.ps1 -Version v1.0.1-beta` | `.exe`, zip, optional NSIS setup |

Details, MIME/`.vv` associations, and signing notes: **[packaging/README.md](packaging/README.md)**.  
Tag releases (`v*`) run [`.github/workflows/release.yml`](.github/workflows/release.yml):

| OS | Arches |
|----|--------|
| Linux | **amd64** + **arm64** (separate native runners) |
| macOS | **universal** (arm64 + amd64) |
| Windows | **amd64** |

Artifacts attach to a **draft** GitHub Release.

Fyne pulls platform GUI dependencies on first build (OpenGL / OS window stack). Headless CI and dogfood use `--headless` and do not require a display for the NullDriver path.

## Quick start

### GUI (default)

1. In Proxmox: VM → **Console** → **SPICE** (downloads a short-lived `.vv`).
2. Open immediately:

```bash
./spice-viewer ~/Downloads/pve-spice.vv
```

Product semantics: if the file sets `delete-this-file=1`, it is removed **after parse, before dial**.

**Daily use (GUI)**

| Feature | How |
|---------|-----|
| Grab keyboard/mouse | Click the guest display |
| Release grab | **Ctrl+Alt+R** / View → Ungrab / toolbar |
| **Ctrl+Alt+Del** | **Send Keys** menu or toolbar (host may steal the real chord) |
| Other chords | **Send Keys** (Ctrl+Alt+Fn, Super, Alt+Tab, Task Manager, …) |
| **Copy/paste** | **Edit → Copy from guest / Paste to guest** (needs **spice-vdagent** in the VM) |
| Paste without agent | Edit → Type text… or Paste falls back to US-QWERTY keystrokes |
| Fullscreen | **Shift+F11** / toolbar |
| Status bar | title · grab · mouse mode · **agent=on/off** |

Guest clipboard requires: SPICE agent channel on the VM + `spice-vdagent` (Linux) or SPICE guest tools (Windows).

### Headless

```bash
./spice-viewer --headless /path/to/pve-spice.vv
```

Connects with a null display/cursor driver, prints `connected`, and waits until disconnect or Ctrl+C. Useful for CI-style smoke tests against a lab endpoint.

### Library sketch

```go
f, err := vvfile.ParseFile(path, vvfile.ParseOptions{DeleteIfRequested: true})
// ...
cfg, err := spice.ConnectConfigFromVV(f)
client, err := spice.Connect(ctx, cfg)
defer client.Close()
```

## Testing and checks

```bash
go test ./...
go vet ./...
gofmt -l .
./scripts/check_imports.sh
```

CI runs the same gates on every push and pull request.

### Integration tests (`//go:build integration`)

Live QEMU SPICE interop tests are **not** part of default `go test ./...`.
They require a local password SPICE lab:

```bash
# Terminal 1 — start lab (127.0.0.1:5900, password testpass)
./scripts/interop_qemu.sh

# Terminal 2
./scripts/run_integration.sh
# or: go test -tags=integration -count=1 ./internal/session/ -run TestQEMU
```

Record fixture hooks and secret-handling notes: [testdata/records/README.md](testdata/records/README.md),
[scripts/README.md](scripts/README.md). Log redaction is unit-tested in
`internal/security` (password must never appear in logs).

### Proxmox acceptance

Status: automated tests are the CI bar; **manual Proxmox lab remains pending operator sign-off**.

## Layout

| Path | Role |
|------|------|
| `cmd/spice-viewer` | Product binary (GUI default; `--headless`) |
| `pkg/spice` | Public library root (Connect, events, drivers) |
| `pkg/vvfile` | Public `.vv` parse API |
| `internal/connector` | TCP / TLS / HTTP CONNECT dialer |
| `internal/protocol` | Wire framing, link messages, enums |
| `internal/session` | Session lifecycle, channel manager |
| `internal/channel` | SPICE channels (main, display, inputs, cursor) |
| `internal/codec` | Image codecs (raw, LZ, Quic, JPEG, GLZ) + `h264` |
| `internal/audio` | Host playback sink (macOS/Windows; Linux stub) |
| `internal/display` | Compositor / surfaces |
| `internal/security` | Ticket crypto, zeroize, redaction |
| `internal/ux` | Error classification (CLI + GUI) |
| `internal/ui` | Fyne GUI backend |
| `testdata/` | Fixtures (`.vv`, certs, vectors, records) |
| `scripts/` | CI and interop helpers |
| `third_party/` | Notes on external references |

## License

Apache License 2.0. See [LICENSE](LICENSE).
