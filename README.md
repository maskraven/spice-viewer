# virt-viewer (Go)

A greenfield, library-first [SPICE](https://www.spice-space.org/) remote display client written in Go.

## Status

**Pre-v0.1 / scaffold.** This repository is under active development. There is no released protocol implementation yet. Package layout and CI are in place; connect, session, and UI work follow in later milestones.

## Goals (product)

- **Proxmox-first**: open a downloaded `pve-spice.vv` file and establish a working session through Proxmox’s HTTP CONNECT spiceproxy, TLS with embedded CA + `host-subject` verification, and SPICE ticket authentication.
- **Single binary** for macOS, Linux, and Windows (`cmd/remote-viewer`).
- **Library-first** (`pkg/spice`, `pkg/vvfile`) so CLI, GUI, and tooling share one session stack.
- **Phase 1 pure Go only** (Apache-2.0); no spice-common C, FFmpeg, or libusb.

See [docs/design-spice-viewer-go.md](docs/design-spice-viewer-go.md) for the full systems design.

## Module

```text
github.com/maskraven/virt-viewer
```

## Building

Requires Go 1.22+.

```bash
go build -o remote-viewer ./cmd/remote-viewer
./remote-viewer -h
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

## Layout (scaffold)

| Path | Role |
|------|------|
| `cmd/remote-viewer` | Product binary (stub until protocol lands) |
| `pkg/spice` | Public library root |
| `pkg/vvfile` | Public `.vv` parse API |
| `internal/connector` | TCP / TLS / HTTP CONNECT dialer |
| `internal/protocol` | Wire framing, link messages, enums |
| `internal/session` | Session lifecycle, channel manager |
| `internal/channel` | SPICE channels (main, display, inputs, …) |
| `internal/codec` | Image/video codecs |
| `internal/display` | Compositor / surfaces |
| `internal/security` | Ticket crypto, zeroize helpers |
| `internal/ux` | Error classification (CLI + GUI) |
| `internal/ui` | GUI backend (Fyne planned) |
| `testdata/` | Fixtures (`.vv`, certs, vectors, records) |
| `scripts/` | CI and interop helpers |
| `third_party/` | Notes on external references |

## License

Apache License 2.0. See [LICENSE](LICENSE).
