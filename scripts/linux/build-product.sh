#!/usr/bin/env bash
# Build Linux product archives + deb/rpm via Goreleaser snapshot (native Linux).
# Usage: VERSION=0.2.0 ./scripts/linux/build-product.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-dev}"
export CGO_ENABLED=1

if ! command -v goreleaser >/dev/null 2>&1; then
	echo "goreleaser not found; building binary + tarball only" >&2
	DIST="$ROOT/dist/linux"
	mkdir -p "$DIST"
	LDFLAGS="-s -w -X main.Version=${VERSION}"
	go build -trimpath -ldflags "$LDFLAGS" -o "$DIST/spice-viewer" ./cmd/spice-viewer
	tar -C "$DIST" -czf "$DIST/spice-viewer_${VERSION#v}_linux_$(go env GOARCH).tar.gz" \
		spice-viewer
	# Bundle desktop integration next to the archive for manual install
	cp -a packaging/spice-viewer.desktop packaging/spice-viewer.xml packaging/icons \
		packaging/scripts "$DIST/" 2>/dev/null || true
	echo "Built $DIST (install goreleaser for deb/rpm)"
	ls -la "$DIST"
	exit 0
fi

# Snapshot release: produces tar.gz + deb/rpm for current GOOS/GOARCH when CGO works.
goreleaser release --snapshot --clean --skip=publish
echo "Goreleaser dist/ contents:"
ls -la dist/ || true
