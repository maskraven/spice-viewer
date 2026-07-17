# v0.1 acceptance (Phase 1 Proxmox MVP)

This document records **acceptance status** for the Phase 1 cut line (product binary `spice-viewer`, library `pkg/spice` + `pkg/vvfile`). It is intentionally honest: automated gates that run in CI are marked pass/fail from the tree; **live Proxmox is not available in CI** and remains operator-owned.

Design baseline: [design-spice-viewer-go.md](design-spice-viewer-go.md) (Phase 1 — Proxmox MVP).  
Operator checklist and CONNECT/TLS/ticket notes: [proxmox.md](proxmox.md).

## Scope (Phase 1 / v0.1)

**In scope**

- Parse Proxmox/virt-viewer `.vv` (including opaque `host`, `proxy`, `ca`, `host-subject`, `delete-this-file`)
- HTTP CONNECT spiceproxy + TLS chain verify + DN pin
- AuthSpice ticket (RSA-OAEP-SHA1); multi-channel link with per-channel re-encrypt
- Main + display (raw + LZ) + inputs; cursor best-effort
- Fyne GUI + `--headless` NullDriver CLI
- Stable UX error classes (ticket / proxy / TLS / transport)
- No agent, no auto-reconnect on tickets, no cgo in the default binary

**Out of scope for v0.1**

- Live Proxmox in GitHub Actions
- USB redir, audio, video codecs, GLZ, vdagent
- Auto-reconnect, API-based spiceconfig fetch UI
- Full spice-gtk feature parity

## Automated status

| Check | How | Status |
|-------|-----|--------|
| Unit + package tests | `go test ./...` | **Pass** (required before tag; run locally/CI) |
| `go vet` | `go vet ./...` | **Pass** (CI) |
| Format | `gofmt -l .` | **Pass** (CI) |
| Import boundaries | `./scripts/check_imports.sh` | **Pass** (CI) |
| Ticket / CONNECT / DN fixtures | unit tests under `internal/security`, `internal/connector`, `pkg/vvfile`, `pkg/spice` | **Pass** (no live PVE) |
| QEMU SPICE interop | `//go:build integration` via `./scripts/run_integration.sh` | **Optional lab** (not default CI) |

> CI does **not** start Proxmox or a spiceproxy. Integration QEMU is documented in [scripts/README.md](../scripts/README.md) and is operator-run.

## Manual Proxmox lab

| Item | Status |
|------|--------|
| Open `.vv` from Console → SPICE | **Pending operator sign-off** |
| Display frames update | **Pending operator sign-off** |
| Keyboard input | **Pending operator sign-off** |
| Mouse input | **Pending operator sign-off** |
| Ticket expiry user message | **Pending operator sign-off** |
| `delete-this-file=1` | **Pending operator sign-off** |
| HiDPI usability (best-effort) | **Pending operator sign-off** |

**Do not invent a sign-off.** When a lab run completes, fill the template in [proxmox.md](proxmox.md#manual-acceptance-checklist-template) and update the table above with date, operator, host OS, and PVE version.

## Phase 1 definition-of-done map

From the design doc global Phase 1 DoD:

| Criterion | Evidence / status |
|-----------|-------------------|
| `spice-viewer pve-spice.vv` works against Proxmox VE (display + inputs) | Manual — **pending operator sign-off** |
| CONNECT authority + TLS subject + ticket documented and tested | Docs: `proxmox.md`; tests: connector/security/vvfile — **automated pass** |
| Multi-channel MAIN_INIT session_id; ticket re-encrypt per channel | Session/channel unit + mock tests — **automated pass** |
| Cursor best-effort; desktop usable without cursor | Implementation + unit tests — **automated pass**; Proxmox feel — **pending** |
| No auto-reconnect on ticket sessions | Documented; `AllowReconnect` ignored — **by design** |
| Secrets not logged; file deleted when requested | Redaction tests + product `DeleteIfRequested` — **automated pass** |
| Agent off; no cgo default | Build tags / package layout — **by design** |
| `pkg/spice` + null driver automation | Headless CLI + NullDriver — **automated pass** |
| CI unit tests + lint + import boundaries | `.github/workflows` — **pass on green CI** |
| Milestone 0 memo | `scripts/milestone0_memo.md` — **present** |

## Tag readiness (v0.1.0)

No git tag is created by this documentation PR. When cutting **v0.1.0**:

1. **Required (automated)**  
   - Green CI on the release commit: `gofmt`, `go vet`, `go test ./...`, `./scripts/check_imports.sh`  
   - `README.md` / `CHANGELOG.md` describe v0.1 status accurately  
   - Module path `github.com/maskraven/virt-viewer`, Apache-2.0 `LICENSE`

2. **Strongly recommended (manual)**  
   - At least one signed Proxmox checklist on macOS **or** Linux (see `proxmox.md`)  
   - Optional: local `./scripts/run_integration.sh` against QEMU password SPICE

3. **Tag command (operator)**  

   ```bash
   git tag -a v0.1.0 -m "v0.1.0: Phase 1 Proxmox MVP (spice-viewer)"
   git push origin v0.1.0
   ```

4. **Version string**  
   - Development default is `dev` (`cmd/spice-viewer` `Version` var).  
   - Release builds may set:  
     `go build -ldflags "-X main.Version=v0.1.0" -o spice-viewer ./cmd/spice-viewer`

5. **Do not tag if**  
   - `go test ./...` fails  
   - Docs claim Proxmox lab sign-off without an operator entry  

## Phase 3 acceptance (parity stretch)

Phase 3 builds on v0.1/Phase 2 toward spice-gtk parity **without** claiming full USB host, real mic capture, Linux host audio, or live Proxmox sign-off. Design and install notes: [phase3.md](phase3.md).

### Automated vs manual

| Area | Automated (CI / unit) | Manual / operator |
|------|----------------------|-------------------|
| **H.264 macOS** (VideoToolbox) | Package builds / soft-skip surface | Guest H.264 stream presents frames (no FFmpeg) |
| **H.264 Windows** (Media Foundation) | cgo path + `CGO_ENABLED=0` soft-skip | Product cgo build: MFT → RGBA on H.264 stream |
| **H.264 Linux** (user FFmpeg) | Available only when probe finds h264; soft-skip otherwise | Install distro FFmpeg → decode works; without FFmpeg session continues |
| **GLZ** | Pure-Go decode unit tests | Guest GLZ images present; errors soft-skip |
| **Record / USB / WebDAV** | Scaffold open + never session-fatal | Best-effort open when listed — **not** full host USB, real mic PCM, or complete share UX |
| **Host audio** | `internal/audio` unit tests; headless NullPlayback | GUI playback on **macOS/Windows** with guest RAW PCM; **Linux** still stub (silent) |
| **Live Proxmox** | Not in CI | **Still operator-owned** (tables above + [proxmox.md](proxmox.md)) |

### Explicitly not done

- Full USB host stack (libusb / platform redir)
- Real microphone capture (`RecordDriver` host backend)
- Linux host audio beyond the ALSA/Pulse stub
- Full WebDAV/phodav folder-share parity
- Operator-signed Proxmox lab rows in this document

Default gates remain: `go test ./...`, `go vet`, `gofmt`, `./scripts/check_imports.sh`. Platform H.264 and host audio need real OS APIs or FFmpeg on `PATH` and are not fully exercised in GitHub Actions.

## Related paths

| Path | Role |
|------|------|
| `cmd/spice-viewer` | Product binary (GUI default, `--headless`) |
| `pkg/spice` | Public session API |
| `pkg/vvfile` | Public `.vv` parse API |
| `internal/connector` | CONNECT + TLS + DN pin |
| `internal/ux` | Stable error classes / messages |
| `testdata/vv/proxmox_sample.vv` | Sanitized Proxmox-shaped fixture |
| `scripts/milestone0_memo.md` | Crypto / CONNECT / DN decisions |
| `docs/phase3.md` | Phase 3 backends, FFmpeg install, channel scaffolds |
