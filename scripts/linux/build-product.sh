#!/usr/bin/env bash
# Build Linux product for the *current* GOARCH (native CGO).
# Usage: VERSION=v1.0.0-beta ./scripts/linux/build-product.sh
#
# Prefer native runners (amd64 and arm64 separately). Do not cross-compile Fyne.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-dev}"
VER_CLEAN="${VERSION#v}"
export CGO_ENABLED=1
ARCH="$(go env GOARCH)"
DIST="$ROOT/dist/linux"
mkdir -p "$DIST"

echo "==> go build linux/${ARCH} (version ${VERSION})"
LDFLAGS="-s -w -X main.Version=${VERSION}"
go build -trimpath -ldflags "$LDFLAGS" -o "$DIST/spice-viewer" ./cmd/spice-viewer

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp "$DIST/spice-viewer" "$STAGE/spice-viewer"
cp LICENSE README.md CHANGELOG.md "$STAGE/" 2>/dev/null || true
mkdir -p "$STAGE/packaging"
cp packaging/spice-viewer.desktop packaging/spice-viewer.xml "$STAGE/packaging/" 2>/dev/null || true
cp -a packaging/icons "$STAGE/packaging/" 2>/dev/null || true

TARBALL="$DIST/spice-viewer_${VER_CLEAN}_linux_${ARCH}.tar.gz"
tar -C "$STAGE" -czf "$TARBALL" .
echo "Built $TARBALL"
ls -la "$DIST"
