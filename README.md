# virt-viewer (Go)

A greenfield, library-first [SPICE](https://www.spice-space.org/) remote display client written in Go.

**Module:** `github.com/maskraven/virt-viewer` · **License:** [Apache-2.0](LICENSE) · **CLI:** `remote-viewer`

## Status — Phase 2 + Phase 3 stretch on top of v0.1

**v0.1** Proxmox MVP is complete. **Phase 2** adds guest agent clipboard, richer codecs, audio playback hooks, and packaging. **Phase 3** (in progress) adds GLZ, H.264, host audio on macOS/Windows, and best-effort channel scaffolds — see [docs/phase3.md](docs/phase3.md).

| Area | State |
|------|--------|
| Parse Proxmox / virt-viewer `.vv` | Implemented (`pkg/vvfile`) |
| HTTP CONNECT spiceproxy + TLS CA + `host-subject` pin | Implemented (`internal/connector`) |
| AuthSpice ticket, multi-channel session | Implemented (`pkg/spice`, session/channel) |
| Display raw + LZ + **Quic + JPEG + MJPEG** + **GLZ** | Implemented (`internal/codec`) |
| **H.264 streams** | OS decoder on **Windows/macOS**; **user-provided FFmpeg** on Linux (never bundled) — [phase3.md](docs/phase3.md#h264-decision) |
| Inputs, cursor, **playback (best-effort)** | Implemented (GUI host sink on macOS/Windows via `internal/audio`; Linux audio stub; headless NullPlayback) |
| **vdagent**: clipboard text + monitors config | Implemented (`internal/agent`) — needs `spice-vdagent` in guest |
| Record / USB redir / WebDAV | **Scaffolds only** (best-effort open; no full USB host / real mic) |
| GUI: Send Keys, Edit copy/paste, grab, hotkeys | Implemented (Fyne) |
| Headless (`--headless`) | Implemented |
| Packaging (`.desktop`, MIME, goreleaser) | Scaffold under `packaging/` + `.goreleaser.yaml` |
| Live Proxmox lab acceptance | **Pending operator sign-off** (not in CI) |

There is **no** auto-reconnect for short-lived tickets: after expiry or disconnect, open Console again in Proxmox for a new `.vv`.

**H.264 policy:** macOS uses VideoToolbox and Windows uses Media Foundation (system APIs). Linux expects a distro **FFmpeg** with H.264 decode on `PATH` — we do **not** ship FFmpeg in the default binary. Without it, H.264 streams soft-skip and other codecs still work. Details: [docs/phase3.md](docs/phase3.md).

Changelog: [CHANGELOG.md](CHANGELOG.md).

## Goals (product)

- **Proxmox-first**: open a downloaded `pve-spice.vv` and establish a session through Proxmox’s HTTP CONNECT spiceproxy, TLS with embedded CA + `host-subject` verification, and SPICE ticket authentication.
- **Single binary** for macOS, Linux, and Windows (`cmd/remote-viewer`).
- **Library-first** (`pkg/spice`, `pkg/vvfile`) so CLI, GUI, and tooling share one session stack.
- **Phase 1 pure Go only** (Apache-2.0); no spice-common C, FFmpeg, or libusb.

## Documentation

| Doc | Contents |
|-----|----------|
| [docs/proxmox.md](docs/proxmox.md) | Using Console `.vv`, CONNECT/TLS/ticket, troubleshooting, ticket-expiry messages, manual checklist |
| [docs/phase2.md](docs/phase2.md) | Clipboard/agent, codecs, audio, packaging APIs; Phase 3 landed vs open |
| [docs/phase3.md](docs/phase3.md) | GLZ, H.264 (OS on Win/Mac, FFmpeg on Linux), host audio, channel scaffolds, acceptance |
| [docs/acceptance-v0.1.md](docs/acceptance-v0.1.md) | Automated vs manual gates (Phase 1 + Phase 3 stretch), tag readiness for v0.1.0 |
| [docs/design-spice-viewer-go.md](docs/design-spice-viewer-go.md) | Full systems design |
| [scripts/milestone0_memo.md](scripts/milestone0_memo.md) | Ticket crypto, CONNECT authority, DN pin decisions |

## Install / build

Requires **Go 1.22+**.

```bash
git clone https://github.com/maskraven/virt-viewer.git
cd virt-viewer
go build -o remote-viewer ./cmd/remote-viewer
./remote-viewer -h
./remote-viewer -version
```

Optional release version stamp:

```bash
go build -ldflags "-X main.Version=v0.1.0" -o remote-viewer ./cmd/remote-viewer
```

Fyne pulls platform GUI dependencies on first build (OpenGL / OS window stack). Headless CI and dogfood use `--headless` and do not require a display for the NullDriver path.

## Quick start

### GUI (default)

1. In Proxmox: VM → **Console** → **SPICE** (downloads a short-lived `.vv`).
2. Open immediately:

```bash
./remote-viewer ~/Downloads/pve-spice.vv
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
./remote-viewer --headless /path/to/pve-spice.vv
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

Manual checklist (open `.vv`, frames, keyboard, mouse, ticket message, `delete-this-file`, HiDPI): [docs/proxmox.md](docs/proxmox.md#manual-acceptance-checklist-template).  
Status: automated tests are the CI bar; **manual Proxmox lab remains pending operator sign-off**.

## Layout

| Path | Role |
|------|------|
| `cmd/remote-viewer` | Product binary (GUI default; `--headless`) |
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
| `docs/` | Design, Proxmox guide, acceptance |
| `third_party/` | Notes on external references |

## License

Apache License 2.0. See [LICENSE](LICENSE).
