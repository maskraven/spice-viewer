#!/usr/bin/env bash
# Build universal (arm64+amd64) remote-viewer, then .app and .dmg.
# Must run on macOS with Xcode CLT. CGO required (Fyne + VideoToolbox).
# Usage: VERSION=0.2.0 ./scripts/macos/build-product.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-dev}"
export CGO_ENABLED=1
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-11.0}"

DIST="$ROOT/dist/macos"
mkdir -p "$DIST"
LDFLAGS="-s -w -X main.Version=${VERSION}"

echo "==> building darwin/arm64"
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" \
	-o "$DIST/remote-viewer-arm64" ./cmd/remote-viewer

echo "==> building darwin/amd64"
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" \
	-o "$DIST/remote-viewer-amd64" ./cmd/remote-viewer

echo "==> lipo universal"
lipo -create -output "$DIST/remote-viewer" \
	"$DIST/remote-viewer-arm64" "$DIST/remote-viewer-amd64"
lipo -info "$DIST/remote-viewer"

VERSION="$VERSION" "$ROOT/scripts/macos/make-app.sh" "$DIST/remote-viewer" "$DIST"
VERSION="$VERSION" "$ROOT/scripts/macos/make-dmg.sh" "$DIST/Remote Viewer.app" \
	"$DIST/Remote-Viewer-${VERSION#v}-macos.dmg"

# Also ship a zip of the .app for users who prefer not to open a DMG.
(
	cd "$DIST"
	rm -f "Remote-Viewer-${VERSION#v}-macos-app.zip"
	zip -qry "Remote-Viewer-${VERSION#v}-macos-app.zip" "Remote Viewer.app"
)

echo "macOS product artifacts in $DIST"
ls -la "$DIST"/*.{dmg,zip} 2>/dev/null || true
