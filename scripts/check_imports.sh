#!/usr/bin/env bash
# check_imports.sh — enforce package import boundaries for virt-viewer.
#
# Rules (from docs/design-spice-viewer-go.md, hardened for CI):
#   cmd/remote-viewer  → pkg/*, internal/ui, internal/ux only
#   pkg/spice          → may import internal/* except UI / GUI toolkits
#   pkg/vvfile         → stdlib only (no internal/, no UI)
#   Non-UI packages    → no internal/ui and no GUI toolkits
#     (connector, protocol, session, channel, codec, display, security, ux)
#   internal/ui        → pkg/spice, pkg/vvfile, internal/ux (plus stdlib)
#
# Exit 0 if clean; non-zero if any violation is found.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

MODULE="github.com/maskraven/virt-viewer"
failed=0

# Non-UI packages that must not import UI (design table + architectural peers).
# Keep this list in sync with package doc comments and the design doc.
NO_UI_PACKAGES=(
  connector
  protocol
  session
  channel
  codec
  display
  security
  ux
)

# List import paths used by packages matching a pattern (go list style).
# Includes production, test, and external-test imports so _test.go cannot
# smuggle forbidden dependencies past CI.
imports_of() {
  local pattern="$1"
  local out
  if ! out="$(go list -f '{{range .Imports}}{{println .}}{{end}}{{range .TestImports}}{{println .}}{{end}}{{range .XTestImports}}{{println .}}{{end}}' "$pattern")"; then
    echo "FAIL: go list failed for pattern: $pattern" >&2
    exit 1
  fi
  printf '%s\n' "$out" | sort -u
}

# True if import path is a UI package we treat as forbidden for non-UI stacks.
is_ui_import() {
  local imp="$1"
  case "$imp" in
    "${MODULE}/internal/ui" | "${MODULE}/internal/ui/"*) return 0 ;;
    # Common GUI toolkits — keep core/library free of them.
    fyne.io/* | gioui.org/* | github.com/andlabs/ui | github.com/gotk3/*) return 0 ;;
  esac
  return 1
}

# --- cmd/remote-viewer: only pkg/*, internal/ui, internal/ux ---
echo "==> checking cmd/remote-viewer imports"
while IFS= read -r imp; do
  [ -z "$imp" ] && continue
  case "$imp" in
    "${MODULE}/pkg/"* | \
    "${MODULE}/internal/ui" | "${MODULE}/internal/ui/"* | \
    "${MODULE}/internal/ux" | "${MODULE}/internal/ux/"*)
      continue
      ;;
    "${MODULE}/"*)
      echo "FAIL: cmd/remote-viewer must not import $imp (only pkg/*, internal/ui, internal/ux)"
      failed=1
      ;;
  esac
done < <(imports_of ./cmd/...)

# --- pkg/spice: may use internal/* but not UI / GUI toolkits ---
echo "==> checking pkg/spice imports (no UI)"
while IFS= read -r imp; do
  [ -z "$imp" ] && continue
  if is_ui_import "$imp"; then
    echo "FAIL: pkg/spice must not import UI package $imp"
    failed=1
  fi
done < <(imports_of ./pkg/spice/...)

# --- pkg/vvfile: no internal, no UI ---
echo "==> checking pkg/vvfile imports"
while IFS= read -r imp; do
  [ -z "$imp" ] && continue
  case "$imp" in
    "${MODULE}/internal/"* | "${MODULE}/internal")
      echo "FAIL: pkg/vvfile must not import internal packages ($imp)"
      failed=1
      ;;
  esac
  if is_ui_import "$imp"; then
    echo "FAIL: pkg/vvfile must not import UI package $imp"
    failed=1
  fi
done < <(imports_of ./pkg/vvfile/...)

# --- Non-UI internal packages: no UI ---
for core in "${NO_UI_PACKAGES[@]}"; do
  if [ ! -d "internal/$core" ]; then
    continue
  fi
  echo "==> checking internal/$core imports (no UI)"
  while IFS= read -r imp; do
    [ -z "$imp" ] && continue
    if is_ui_import "$imp"; then
      echo "FAIL: internal/$core must not import UI package $imp"
      failed=1
    fi
  done < <(imports_of "./internal/$core/...")
done

# --- internal/ui: only pkg/spice, pkg/vvfile, internal/ux among module paths ---
echo "==> checking internal/ui imports"
while IFS= read -r imp; do
  [ -z "$imp" ] && continue
  case "$imp" in
    "${MODULE}/pkg/spice" | "${MODULE}/pkg/spice/"* | \
    "${MODULE}/pkg/vvfile" | "${MODULE}/pkg/vvfile/"* | \
    "${MODULE}/internal/ux" | "${MODULE}/internal/ux/"* | \
    "${MODULE}/internal/ui" | "${MODULE}/internal/ui/"*)
      continue
      ;;
    "${MODULE}/"*)
      echo "FAIL: internal/ui must not import $imp (only pkg/spice, pkg/vvfile, internal/ux)"
      failed=1
      ;;
  esac
done < <(imports_of ./internal/ui/...)

if [ "$failed" -ne 0 ]; then
  echo "import boundary check FAILED"
  exit 1
fi
echo "import boundary check OK"
exit 0
