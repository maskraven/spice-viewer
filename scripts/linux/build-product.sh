#!/usr/bin/env bash
# Build Linux product for the *current* GOARCH (native CGO).
# Usage: VERSION=v0.2.0 ./scripts/linux/build-product.sh
#
# Prefer native runners (amd64 and arm64 separately). Do not cross-compile Fyne.
# With goreleaser installed, also emits deb/rpm via --single-target.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-dev}"
VER_CLEAN="${VERSION#v}"
export CGO_ENABLED=1
ARCH="$(go env GOARCH)"
DIST="$ROOT/dist/linux"
mkdir -p "$DIST"

if command -v goreleaser >/dev/null 2>&1; then
	echo "==> goreleaser --single-target (linux/${ARCH})"
	if [[ "$VERSION" == dev || "$VERSION" == *snapshot* ]]; then
		goreleaser release --snapshot --clean --skip=publish --single-target
	else
		# When VERSION is set without a git tag, still use snapshot naming.
		goreleaser release --snapshot --clean --skip=publish --single-target
	fi
	# Collect into dist/linux
	find dist -maxdepth 1 -type f \( -name '*.tar.gz' -o -name '*.deb' -o -name '*.rpm' -o -name 'checksums.txt' \) \
		-exec cp -f {} "$DIST/" \; 2>/dev/null || true
	if ls "$DIST"/*.tar.gz >/dev/null 2>&1 || ls "$DIST"/*.deb >/dev/null 2>&1; then
		echo "Built $DIST"
		ls -la "$DIST"
		exit 0
	fi
	echo "goreleaser produced no packages; falling back to tarball" >&2
fi

echo "==> go build linux/${ARCH}"
LDFLAGS="-s -w -X main.Version=${VERSION}"
go build -trimpath -ldflags "$LDFLAGS" -o "$DIST/spice-viewer" ./cmd/spice-viewer

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp "$DIST/spice-viewer" "$STAGE/spice-viewer"
cp LICENSE README.md CHANGELOG.md "$STAGE/" 2>/dev/null || true
mkdir -p "$STAGE/packaging"
cp packaging/spice-viewer.desktop packaging/spice-viewer.xml "$STAGE/packaging/" 2>/dev/null || true
cp -a packaging/icons "$STAGE/packaging/" 2>/dev/null || true

tar -C "$STAGE" -czf "$DIST/spice-viewer_${VER_CLEAN}_linux_${ARCH}.tar.gz" .
echo "Built $DIST/spice-viewer_${VER_CLEAN}_linux_${ARCH}.tar.gz"
ls -la "$DIST"
