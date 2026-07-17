#!/usr/bin/env bash
# Pack a .app into a UDZO DMG with an /Applications symlink.
# Usage: VERSION=0.2.0 ./scripts/macos/make-dmg.sh <App.app> [out.dmg]
set -euo pipefail

VERSION="${VERSION:-dev}"
APP="${1:?usage: make-dmg.sh <App.app> [out.dmg]}"
VER_CLEAN="${VERSION#v}"
OUT="${2:-dist/Spice-Viewer-${VER_CLEAN}-macos.dmg}"

if [[ ! -d "$APP" ]]; then
	echo "make-dmg: app not found: $APP" >&2
	exit 1
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"

mkdir -p "$(dirname "$OUT")"
rm -f "$OUT"
hdiutil create \
	-volname "SPICE Viewer ${VER_CLEAN}" \
	-srcfolder "$STAGE" \
	-ov -format UDZO \
	"$OUT"
echo "Built $OUT"
