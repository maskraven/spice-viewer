# Packaging

Product packaging for **spice-viewer** on Linux, macOS, and Windows.

| Platform | Artifacts | How to build |
|----------|-----------|--------------|
| **Linux** | `tar.gz`, **deb**, **rpm** | `scripts/linux/build-product.sh` or GoReleaser on Linux |
| **macOS** | **`.app`**, **`.dmg`**, app zip | `scripts/macos/build-product.sh` on a Mac |
| **Windows** | **`.exe`** (GUI), **zip**, optional **NSIS setup** | `scripts/windows/build-product.ps1` on Windows |

CI: [`.github/workflows/release.yml`](../.github/workflows/release.yml) builds all three on tag push (`v*`) and attaches a **draft** GitHub Release.

**Important:** Fyne and OS codecs need **native** `CGO_ENABLED=1` builds:

- macOS H.264 → VideoToolbox (build on macOS)
- Windows H.264 → Media Foundation (build on Windows)
- Linux H.264 → system **FFmpeg** on `PATH` (never bundled)

Do not treat a pure cross-compile from another OS as a full product binary.

---

## Linux desktop integration

| File | Role |
|------|------|
| `spice-viewer.desktop` | FreeDesktop app entry; `MimeType=application/x-virt-viewer` |
| `spice-viewer.xml` | MIME type for `*.vv` |
| `icons/hicolor/*/apps/spice-viewer.png` | Theme icons (`Icon=spice-viewer`) |
| `scripts/postinstall.sh` / `postremove.sh` | Update desktop/MIME/icon caches (nFPM) |

### Manual install (tarball)

```bash
sudo install -Dm755 spice-viewer /usr/bin/spice-viewer
sudo install -Dm644 packaging/spice-viewer.desktop /usr/share/applications/
sudo install -Dm644 packaging/spice-viewer.xml /usr/share/mime/packages/
sudo cp -a packaging/icons/hicolor/* /usr/share/icons/hicolor/
sudo update-desktop-database
sudo update-mime-database /usr/share/mime
gtk-update-icon-cache -q /usr/share/icons/hicolor 2>/dev/null || true
```

Prefer this client as the default for `.vv`:

```bash
xdg-mime default spice-viewer.desktop application/x-virt-viewer
```

### deb / rpm

Package name and binary: **`spice-viewer`** (no path clash with distro `virt-viewer` / `remote-viewer`).

- **Recommends** `ffmpeg` for H.264 (soft-skip without it).
- Runtime deps: OpenGL + X11/Wayland client libraries (see `.goreleaser.yaml` nFPM).

```bash
# On Linux with CGO deps + goreleaser:
VERSION=v0.2.0 ./scripts/linux/build-product.sh
# or:
goreleaser release --snapshot --clean
```

---

## macOS (`.app` + `.dmg`)

| File | Role |
|------|------|
| `macos/Info.plist.in` | Bundle metadata + **`.vv` UTI** / document type |
| `macos/AppIcon.icns` | App icon |
| `macos/entitlements.plist` | Hardened-runtime exceptions (for future notarization) |
| `scripts/macos/make-app.sh` | Binary → `SPICE Viewer.app` |
| `scripts/macos/make-dmg.sh` | App → UDZO DMG with `/Applications` link |
| `scripts/macos/build-product.sh` | Universal arm64+amd64 + app + dmg + zip |

```bash
# On macOS with Xcode CLT:
export CGO_ENABLED=1
export MACOSX_DEPLOYMENT_TARGET=11.0
VERSION=v0.2.0 ./scripts/macos/build-product.sh
# → dist/macos/SPICE Viewer.app
# → dist/macos/Spice-Viewer-0.2.0-macos.dmg
# → dist/macos/Spice-Viewer-0.2.0-macos-app.zip
```

Unsigned builds work after right-click → Open (or clearing quarantine).  
**Notarization** requires an Apple Developer ID (optional; see `entitlements.plist`). Goreleaser Pro is **not** required — we use `hdiutil` scripts.

---

## Windows (`.exe` + zip + NSIS)

| File | Role |
|------|------|
| `windows/icon.ico` | Product icon |
| `windows/winres.json` | go-winres input (VERSIONINFO + icon) |
| `windows/installer.nsi` | NSIS setup: Program Files, Start Menu, **`.vv` association** |
| `windows/associate-hkcu.ps1` | Per-user association for portable zip (no admin) |
| `scripts/windows/build-product.ps1` | Build GUI exe + zip + optional setup |

```powershell
# On Windows with Go + MinGW (or MSVC) for cgo:
$env:CGO_ENABLED = "1"
.\scripts\windows\build-product.ps1 -Version v0.2.0
# → dist\windows\spice-viewer.exe   (-H windowsgui, no console flash)
# → dist\windows\spice-viewer_*_windows_amd64.zip
# → dist\windows\spice-viewer-setup-*-amd64.exe  (if NSIS installed)
```

Portable association (current user):

```powershell
powershell -ExecutionPolicy Bypass -File associate-hkcu.ps1 -ExePath .\spice-viewer.exe
```

Unsigned builds may trigger SmartScreen; verify release checksums.

---

## GoReleaser

Root [`.goreleaser.yaml`](../.goreleaser.yaml):

- **Unix** builds: linux + darwin archives; Linux **nFPM** deb/rpm with desktop/MIME/icons
- **Windows** build id uses `-H windowsgui` (prefer building on Windows)
- Releases are **draft** by default

```bash
goreleaser release --snapshot --clean
```

---

## Signing (optional, later)

| Platform | Without signing | With signing |
|----------|-----------------|--------------|
| macOS | Right-click Open / remove quarantine | Developer ID + `notarytool` + staple |
| Windows | SmartScreen click-through | Authenticode (e.g. Azure Trusted Signing) |
| Linux | Normal package install | Distro or own apt/rpm repo keys |

Signing is not required for open-source dogfood; recommended before wide desktop distribution.
