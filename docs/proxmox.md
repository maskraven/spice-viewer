# Proxmox Console (SPICE) with spice-viewer

This guide covers opening a Proxmox VE **Console → SPICE** connection file (`.vv`) with this client (`cmd/spice-viewer`), how the CONNECT / TLS / ticket path works, and how to troubleshoot common failures.

See also:

- [design-spice-viewer-go.md](design-spice-viewer-go.md) — full systems design
- [acceptance-v0.1.md](acceptance-v0.1.md) — Phase 1 / v0.1 acceptance status and tag readiness

## Quick start

1. In the Proxmox web UI, open the target VM → **Console** → choose **SPICE** (downloads a short-lived `pve-spice.vv` / similar).
2. Build the client (Go 1.22+):

   ```bash
   go build -o spice-viewer ./cmd/spice-viewer
   ```

3. Open the file (GUI is the default):

   ```bash
   ./spice-viewer ~/Downloads/pve-spice.vv
   ```

4. Headless (no display; NullDriver — CI / dogfood):

   ```bash
   ./spice-viewer --headless /path/to/pve-spice.vv
   ```

**Important:** Proxmox tickets expire quickly (often on the order of tens of seconds for multi-channel open, and minutes for the session). Download the `.vv` and open it immediately. Do not reuse an old connection file.

## What is in a Proxmox `.vv`?

A typical download looks like:

```ini
[virt-viewer]
type=spice
host=pvespiceproxy:687d1ec6:10016:pve::dcc9e35662ef0b1233e12ac02880ea7851f9218e
host-subject=OU=PVE Cluster Node,O=Proxmox Virtual Environment,CN=pve.example.com
tls-port=61002
password=PVE:shortlivedticketvalue…
proxy=http://proxmox.example.com:3128
ca=-----BEGIN CERTIFICATE-----\n…\n-----END CERTIFICATE-----\n
title=VM 10016 - debian-spice
delete-this-file=1
secure-attention=Ctrl+Alt+Ins
release-cursor=Ctrl+Alt+R
toggle-fullscreen=Shift+F11
```

| Key | Role |
|-----|------|
| `type` | Must be `spice` |
| `host` | **Opaque** spiceproxy CONNECT token (not a DNS name; may contain many colons) |
| `tls-port` | Destination port after CONNECT |
| `password` | Short-lived SPICE ticket (AuthSpice) |
| `proxy` | HTTP proxy that accepts CONNECT (Proxmox spiceproxy listener) |
| `ca` | PEM CA(s) for verifying the TLS leaf after CONNECT (often `\n`-escaped) |
| `host-subject` | OpenSSL-style DN pin for the leaf certificate |
| `delete-this-file` | When `1`, product binary deletes the file after parse (before dial) |
| hotkeys | Local chords; `secure-attention` injects guest **Ctrl+Alt+Del** (CAD), not Ins |

A sanitized fixture lives at [`testdata/vv/proxmox_sample.vv`](../testdata/vv/proxmox_sample.vv).

## Connect path (CONNECT / TLS / ticket)

End-to-end sequence implemented by this stack:

1. **Parse** `.vv` via `pkg/vvfile` (product path always sets `DeleteIfRequested: true`).
2. If `delete-this-file=1`, **remove the file on disk** immediately after secrets are copied — before any network dial — so a failed session still does not leave the ticket on disk.
3. **TCP** to the HTTP `proxy` host:port.
4. **HTTP CONNECT** to authority `host:tls-port` with the host token **verbatim** (never `net.SplitHostPort` on the multi-colon Proxmox token). Example request-target:

   ```http
   CONNECT pvespiceproxy:…:61002 HTTP/1.1
   ```

5. **TLS** over the tunnel:
   - Certificate chain is verified against the embedded `ca` PEM pool.
   - When `host-subject` is set (Proxmox mode), hostname/SNI checks are not used for the opaque token; the leaf DN is **pinned** with structured RDN comparison (not raw `pkix.Name.String()` equality).
   - `tls.Config.ServerName` is **not** set to the `pvespiceproxy:…` token.
6. **SPICE link** on each channel: AuthSpice RSA-OAEP-SHA1 ticket encryption with the server-provided pubkey; password is never logged.
7. **Main** channel completes link → `MAIN_INIT` / `CHANNELS_LIST` → **parallel** child channels (display, inputs, cursor best-effort). Each child re-encrypts the same ticket with that channel’s fresh pubkey.

There is **no auto-reconnect** for ticket/password sessions in Phase 1. After disconnect or expiry, download a new Console file and open it again.

Library entrypoints:

- `vvfile.ParseFile` / `vvfile.Parse`
- `spice.ConnectConfigFromVV` → `spice.Connect`

## Ticket expiry messaging

Stable user-facing strings live in `internal/ux` and are printed by the CLI (and intended for GUI mapping):

| Situation | Class | Message |
|-----------|-------|---------|
| Bad / expired ticket on link | `Ticket` | **Ticket invalid or expired — open Console again in Proxmox** |
| CONNECT / spiceproxy unreachable | `Proxy` | **Cannot reach Proxmox spiceproxy** |
| Leaf DN ≠ `host-subject` | `TLSSubject` | **Certificate subject does not match connection file** |
| CA / chain trust failure | `TLSTrust` | **Cannot validate server certificate** |
| Mid-session drop / EOF | `Transport` | **Connection lost — re-open Console for a new ticket** |
| Bad `.vv` type / fields | `Config` | e.g. **Not a SPICE connection file** |

Example CLI output:

```text
spice-viewer: Ticket invalid or expired — open Console again in Proxmox
spice-viewer: detail: …
```

**What to do:** In the Proxmox UI, open **Console → SPICE** again (fresh `.vv`), then run `spice-viewer` on the new file. Do not expect silent reconnect.

## `delete-this-file` behavior

| Consumer | Honors `delete-this-file=1`? |
|----------|------------------------------|
| `cmd/spice-viewer` (GUI and `--headless`) | **Yes** (always) |
| Library `vvfile.ParseFile` with zero `ParseOptions` | **No** (safe default) |
| Library with `ParseOptions{DeleteIfRequested: true}` | **Yes** |

If deletion fails (permissions, already removed), the CLI prints a warning and continues with the in-memory secrets. Keep a copy of the `.vv` only in secure labs; production tickets should not be archived.

## Hotkeys (from `.vv`)

| `.vv` key | Default example | Behavior |
|-----------|-----------------|----------|
| `secure-attention` | `Ctrl+Alt+Ins` | Injects guest **Ctrl+Alt+Del** (CAD), not Ins |
| `release-cursor` | `Ctrl+Alt+R` | Ungrabs mouse/keyboard |
| `toggle-fullscreen` | `Shift+F11` | Toggles window fullscreen |

## HiDPI note

Guest framebuffer pixels are stored at guest-native resolution. The Fyne present path scales them to the widget/window size using the canvas scale factor (**best-effort HiDPI**). Expect:

- Smooth scaling when the window is larger than the guest desktop
- No guest-side DPI change (agent/vdagent is out of Phase 1)
- Mouse coordinates mapped through the same present surface

Operators on retina / 200% displays should still see a usable desktop; if the image looks soft, resize the window closer to 1:1 guest resolution.

## Troubleshooting

### Cannot open / “Not a SPICE connection file”

- Confirm the download is a virt-viewer SPICE file (`type=spice`), not VNC or noVNC.
- File may be truncated; re-download from Console.

### “Cannot reach Proxmox spiceproxy” (`Proxy`)

- Host running `spice-viewer` must reach the `proxy=` URL (often the PVE node on the spiceproxy port, e.g. 3128).
- Check firewall, VPN, and that spiceproxy is enabled for the cluster/node.
- HTTPS proxies are not supported in Phase 1 (HTTP CONNECT only).

### “Certificate subject does not match connection file” (`TLSSubject`)

- Cluster certs or node CN may have changed relative to the `.vv` `host-subject`.
- Re-download Console (fresh subject string). If it still fails, compare leaf DN to `host-subject` (ordering of RDNs is normalized via structured match — still requires equivalent attributes).

### “Cannot validate server certificate” (`TLSTrust`)

- Embedded `ca` PEM must chain to the spiceproxy TLS leaf.
- Custom or rotated cluster CA: re-download `.vv` from a current UI session.
- Do not strip or rewrite the `ca=` field when copying fixtures.

### “Ticket invalid or expired” (`Ticket`)

- Most common: waited too long after download, or ticket TTL raced slow multi-channel open.
- Re-open **Console → SPICE** and launch `spice-viewer` immediately.
- Child channel auth failure uses the same message (ticket checked on every channel link).

### “Connection lost — re-open Console for a new ticket” (`Transport`)

- Network blip, VM stop/migrate, or proxy idle timeout.
- No auto-reconnect: get a new `.vv`.

### Display blank / partial / wrong colors

- Phase 1 supports raw and LZ image types on the display channel; exotic codecs (GLZ, video) are out of scope.
- Prefer a Linux guest with QXL / `qxl-vga` under Proxmox for acceptance.
- Headless mode never paints a window (`--headless` uses NullDriver).

### Keyboard or mouse not working

- Confirm inputs channel came up (session should fail if required channels cannot auth).
- Try click-to-grab; use `release-cursor` hotkey to ungrab.
- CAD: use the configured `secure-attention` chord (sends Ctrl+Alt+Del to the guest).

### File disappeared after one attempt

- Expected when `delete-this-file=1` (Proxmox default). Re-download from Console for every attempt.

## Manual acceptance checklist (template)

Use this on a real Proxmox lab. Checkboxes are for the **operator** — do not mark manual items done without a live lab.

### Prerequisites

- [ ] Proxmox VE node reachable; VM powered on with SPICE/QXL display
- [ ] Built `spice-viewer` from this tree (`go build -o spice-viewer ./cmd/spice-viewer`)
- [ ] Host can reach spiceproxy (`proxy=` in the `.vv`)

### Session path

- [ ] **Open `.vv`**: Console → SPICE download; `./spice-viewer pve-spice.vv` starts without config error
- [ ] **Display frames**: guest desktop (or boot console) is visible and updates
- [ ] **Keyboard**: typing reaches the guest (login prompt or editor)
- [ ] **Mouse**: pointer motion/clicks reach the guest; grab/ungrab works
- [ ] **Ticket expiry message**: wait for ticket to expire *or* reuse a stale `.vv` → user-facing text matches **Ticket invalid or expired — open Console again in Proxmox** (or transport disconnect guidance after an established session)
- [ ] **`delete-this-file`**: with `delete-this-file=1`, file is gone after parse/open attempt; session still used in-memory ticket for that run
- [ ] **HiDPI note**: on a scaled display, desktop remains usable (best-effort scale); document any severe artifacts

### Optional product checks

- [ ] Fullscreen hotkey toggles
- [ ] Release-cursor hotkey ungrabs
- [ ] Secure-attention injects Ctrl+Alt+Del in guest
- [ ] `--headless` connects and stays up until Ctrl+C / disconnect (no GUI)

### Sign-off

| Gate | Status |
|------|--------|
| Automated unit tests (`go test ./...`) | See [acceptance-v0.1.md](acceptance-v0.1.md) |
| Manual Proxmox lab | **Pending operator sign-off** (no live Proxmox in CI) |

Operator: ________________  Date: ________________  Host OS: ________________  PVE version: ________________

## Security hygiene

- Never commit real tickets, CA private keys, or production `.vv` files.
- Prefer the product binary’s delete-on-parse behavior; wipe lab copies after use.
- Logs must not contain the password or full ticket ciphertext (`internal/security` redaction tests cover this).
- Phase 1 does not implement USB redirection, smartcard, or vdagent clipboard.
