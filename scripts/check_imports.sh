#!/usr/bin/env bash
# check_imports.sh — enforce package import boundaries for virt-viewer.
#
# Rules (from docs/design-spice-viewer-go.md):
#   cmd/remote-viewer  → pkg/*, internal/ui, internal/ux only
#   pkg/spice          → may import internal/*
#   pkg/vvfile         → stdlib only (no internal/, no UI)
#   internal/protocol, connector, codec, session, channel → no UI
#   internal/ui        → pkg/spice, pkg/vvfile, internal/ux (plus stdlib)
#
# Exit 0 if clean; non-zero if any violation is found.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

MODULE="github.com/maskraven/virt-viewer"
failed=0

# List import paths used by packages matching a pattern (relative or module path).
# Args: package pattern (go list style, e.g. ./cmd/...)
imports_of() {
  local pattern="$1"
  go list -f '{{range .Imports}}{{println .}}{{end}}' "$pattern" 2>/dev/null | sort -u
}

# True if import path is a UI package we treat as forbidden for core stacks.
is_ui_import() {
  local imp="$1"
  case "$imp" in
    "${MODULE}/internal/ui" | "${MODULE}/internal/ui/"*) return 0 ;;
    # Common GUI toolkits — keep core free of them.
    fyne.io/* | gioui.org/* | github.com/andlabs/ui | github.com/gotk3/*) return 0 ;;
  esac
  return 1
}

# --- cmd/remote-viewer: only pkg/*, internal/ui, internal/ux ---
echo "==> checking cmd/remote-viewer imports"
while IFS= read -r imp; do
  [ -z "$imp" ] && continue
  # stdlib / toolchain: no dots in first path element typically; allow all non-module
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

# --- internal core stacks: no UI ---
for core in connector protocol session channel codec; do
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
