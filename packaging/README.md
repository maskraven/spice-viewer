# Packaging

## Linux desktop integration

- `remote-viewer.desktop` — FreeDesktop application entry; associates `*.vv` via MIME.
- `remote-viewer.xml` — MIME type `application/x-virt-viewer` for `.vv` files.

Install (manual):

```bash
sudo install -Dm644 remote-viewer.desktop /usr/share/applications/
sudo install -Dm644 remote-viewer.xml /usr/share/mime/packages/
sudo update-desktop-database
sudo update-mime-database /usr/share/mime
```

## Releases

See `.goreleaser.yaml` at repo root. Example:

```bash
goreleaser release --snapshot --clean
```

Note: Fyne builds often need `CGO_ENABLED=1` and platform GUI libraries.
