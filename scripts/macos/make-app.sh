#!/usr/bin/env bash
# Build a macOS .app bundle from a remote-viewer binary.
# Usage: VERSION=0.2.0 ./scripts/macos/make-app.sh <binary> [out_dir]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VERSION="${VERSION:-dev}"
BINARY="${1:?usage: make-app.sh <binary> [out_dir]}"
OUT_DIR="${2:-$ROOT/dist}"
APP_NAME="Remote Viewer"
APP="$OUT_DIR/${APP_NAME}.app"

if [[ ! -f "$BINARY" ]]; then
	echo "make-app: binary not found: $BINARY" >&2
	exit 1
fi

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

# Version stamp in Info.plist (@VERSION@ → VERSION, strip leading v).
VER_CLEAN="${VERSION#v}"
sed -e "s/@VERSION@/${VER_CLEAN}/g" \
	"$ROOT/packaging/macos/Info.plist.in" \
	>"$APP/Contents/Info.plist"

cp "$BINARY" "$APP/Contents/MacOS/remote-viewer"
chmod +x "$APP/Contents/MacOS/remote-viewer"

if [[ -f "$ROOT/packaging/macos/AppIcon.icns" ]]; then
	cp "$ROOT/packaging/macos/AppIcon.icns" "$APP/Contents/Resources/AppIcon.icns"
fi

printf 'APPL????' >"$APP/Contents/PkgInfo"
echo "Built $APP (version ${VER_CLEAN})"
