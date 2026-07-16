#!/usr/bin/env bash
# run_integration.sh — run //go:build integration tests against a QEMU SPICE lab.
#
# Prerequisites:
#   1. QEMU with SPICE support installed.
#   2. Lab server running, e.g.:
#        ./scripts/interop_qemu.sh
#      or password/port matching SPICE_* env vars below.
#
# Usage:
#   ./scripts/run_integration.sh
#   SPICE_PORT=5901 SPICE_PASSWORD=lab ./scripts/run_integration.sh
#   ./scripts/run_integration.sh ./internal/session/   # package filter
#
# Default unit CI does NOT enable -tags=integration (no QEMU on runners).
# See scripts/README.md and testdata/records/README.md.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export SPICE_HOST="${SPICE_HOST:-127.0.0.1}"
export SPICE_PORT="${SPICE_PORT:-5900}"
export SPICE_PASSWORD="${SPICE_PASSWORD:-testpass}"

PACKAGES="${*:-./internal/session/}"

echo "==> integration tags: packages=$PACKAGES"
echo "    SPICE_HOST=$SPICE_HOST SPICE_PORT=$SPICE_PORT"
echo "    (password redacted; default testpass if unset)"

# Soft preflight: TCP reachability so failures are clearer than dial timeouts.
if command -v nc >/dev/null 2>&1; then
  if ! nc -z -w 2 "$SPICE_HOST" "$SPICE_PORT" 2>/dev/null; then
    echo "warning: nothing listening on ${SPICE_HOST}:${SPICE_PORT}" >&2
    echo "         start lab: ./scripts/interop_qemu.sh" >&2
    echo "         continuing — tests will Skip if still unreachable" >&2
  fi
elif command -v timeout >/dev/null 2>&1; then
  if ! timeout 2 bash -c "echo >/dev/tcp/${SPICE_HOST}/${SPICE_PORT}" 2>/dev/null; then
    echo "warning: nothing listening on ${SPICE_HOST}:${SPICE_PORT}" >&2
    echo "         start lab: ./scripts/interop_qemu.sh" >&2
  fi
fi

# -count=1 disables cache so lab up/down is always re-probed.
exec go test -tags=integration -count=1 -timeout=60s $PACKAGES
