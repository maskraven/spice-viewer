# Phase 2 — Desktop comfort

Phase 2 builds on the v0.1 Proxmox MVP. This document is the operator/developer guide for features added after PR 16.

## Features

| Feature | Package | Guest requirement | Notes |
|---------|---------|-------------------|--------|
| Clipboard (UTF-8) | `internal/agent`, `pkg/spice` | `spice-vdagent` (Linux) or SPICE tools (Windows) | Edit → Copy/Paste; auto-request on guest grab |
| Guest resolution | `agent.SendMonitorsConfig` | agent monitors-config cap | UI sends pad size on resize when agent is on |
| Quic images | `internal/codec` | none | RGB24/32; other variants soft-skip |
| JPEG / JPEG-Alpha | `internal/codec` | none | stdlib `image/jpeg` |
| MJPEG streams | `internal/channel` display | none | STREAM_CREATE + DATA |
| Audio playback | `internal/channel` playback | none (server may open channel) | RAW S16LE → `PlaybackDriver`; default discard |
| Packaging | `packaging/`, `.goreleaser.yaml` | n/a | MIME `application/x-virt-viewer`, `.desktop` |

## Clipboard setup (Proxmox / QEMU)

1. VM has a SPICE display and a **virtio-serial / spicevmc** channel for the agent (Proxmox usually adds this when SPICE is selected).
2. Inside the guest:
   - **Linux:** install and run `spice-vdagent` (and often `spice-vdagentd`).
   - **Windows:** install SPICE guest tools with vdagent.
3. Open a fresh Console `.vv` with `remote-viewer`.
4. Status bar should show **`agent=on`** when capabilities are exchanged.
5. Use **Edit → Paste to guest** / **Copy from guest**.

Without the agent, **Paste** falls back to US-QWERTY keystroke typing (limited).

## Library API (clipboard / resize)

```go
client, err := spice.Connect(ctx, cfg)
// ...
_ = client.SetHostClipboard("text for guest")
_ = client.RequestGuestClipboard() // result arrives as EventClipboard
_ = client.SetGuestDisplaySize(1920, 1080)
active := client.AgentActive()

for ev := range client.Events() {
    switch ev.Type {
    case spice.EventClipboard:
        // ev.ClipboardText
    case spice.EventAgent:
        // ev.AgentActive
    }
}
```

## Audio hook

```go
cfg.Drivers.Playback = mySink // implements spice.PlaybackDriver
// WritePCM receives interleaved S16LE samples
```

Default is silent discard (`NullPlayback`).

## Packaging

```bash
# Snapshot build (needs goreleaser + platform GUI deps for Fyne)
goreleaser release --snapshot --clean
```

See `packaging/README.md` for manual MIME/desktop install on Linux.

## Still Phase 3

- GLZ / H.264 (license-aware)
- USB redirection, WebDAV, record (mic)
- Bundled host audio device backends
