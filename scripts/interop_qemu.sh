#!/usr/bin/env bash
# interop_qemu.sh — lab helper to run QEMU with SPICE ticketing for AuthSpice interop.
#
# Milestone 0: proves ticket crypto / link path against a real SPICE server when QEMU
# is available. If QEMU+SPICE is missing, golden vectors in testdata/vectors/ remain
# the M0 gate (see scripts/milestone0_memo.md).
#
# PR 15: record fixture hooks + integration harness support.
#
# Usage:
#   ./scripts/interop_qemu.sh                 # start QEMU SPICE lab (foreground)
#   ./scripts/interop_qemu.sh --print-vv      # print a sample .vv for direct cleartext lab
#   ./scripts/interop_qemu.sh --write-vv PATH # write sample .vv to PATH
#   ./scripts/interop_qemu.sh --check         # exit 0 if QEMU+SPICE available; else 2
#   ./scripts/interop_qemu.sh --record FILE   # start QEMU and record SPICE traffic to FILE
#   SPICE_PORT=5900 SPICE_PASSWORD=testpass ./scripts/interop_qemu.sh
#   SPICE_RECORD=testdata/records/lab.rec ./scripts/interop_qemu.sh
#
# Integration tests (//go:build integration):
#   Terminal 1: ./scripts/interop_qemu.sh
#   Terminal 2: ./scripts/run_integration.sh
#   See scripts/README.md and testdata/records/README.md.
#
# Requirements: qemu-system-x86_64 (or $QEMU) built with SPICE support.
# Optional: spice-viewer for manual cross-check.
#
# Safety: SPICE always binds addr=127.0.0.1 (localhost only). SPICE_PORT must be
# digits-only; SPICE_PASSWORD must not contain ',' or newlines (QEMU -spice CSV).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SPICE_PORT="${SPICE_PORT:-5900}"
SPICE_PASSWORD="${SPICE_PASSWORD:-testpass}"
QEMU="${QEMU:-qemu-system-x86_64}"
MEMORY_MB="${MEMORY_MB:-256}"
# Empty machine is fine for SPICE bring-up smoke; supply DISK/ISO for full guest.
DISK="${DISK:-}"
ISO="${ISO:-}"
# Optional SPICE traffic record path (also via --record FILE).
SPICE_RECORD="${SPICE_RECORD:-}"

die() { echo "error: $*" >&2; exit 1; }

# Fail-closed: only report SPICE when QEMU positively advertises the -spice option.
have_spice() {
  if ! command -v "$QEMU" >/dev/null 2>&1; then
    return 1
  fi
  # Positive capability: -spice help succeeds (SPICE-enabled builds).
  if "$QEMU" -spice help >/dev/null 2>&1; then
    return 0
  fi
  # Some builds print spice on -version but still lack a working -spice option.
  # Do not treat version text alone as sufficient.
  return 1
}

validate_env() {
  # Digits only — reject commas so values cannot inject -spice CSV keys.
  case "$SPICE_PORT" in
    ''|*[!0-9]*) die "SPICE_PORT must be digits-only (got: $SPICE_PORT)" ;;
  esac
  if [ "$SPICE_PORT" -lt 1 ] || [ "$SPICE_PORT" -gt 65535 ]; then
    die "SPICE_PORT must be in 1..65535 (got: $SPICE_PORT)"
  fi
  # QEMU -spice is a comma-separated option list; commas or newlines would inject keys
  # (e.g. addr=0.0.0.0) past our mandatory addr=127.0.0.1.
  case "$SPICE_PASSWORD" in
    *','*|*$'\n'*|*$'\r'*)
      die "SPICE_PASSWORD must not contain comma or newline (would break -spice option CSV)"
      ;;
  esac
  if [[ -n "$SPICE_RECORD" ]]; then
    case "$SPICE_RECORD" in
      *','*|*$'\n'*|*$'\r'*)
        die "SPICE_RECORD path must not contain comma or newline"
        ;;
    esac
  fi
}

print_vv() {
  cat <<EOF
[virt-viewer]
type=spice
host=127.0.0.1
port=${SPICE_PORT}
password=${SPICE_PASSWORD}
# Lab cleartext — no tls-port / ca / host-subject.
# For TLS lab, add tls-port, ca, host-subject and drop port.
title=M0 QEMU SPICE lab
delete-this-file=0
EOF
}

usage() {
  sed -n '2,30p' "$0"
}

cmd="${1:-}"

if [[ "$cmd" == "-h" || "$cmd" == "--help" ]]; then
  usage
  exit 0
fi

if [[ "$cmd" == "--check" ]]; then
  if have_spice; then
    echo "ok: QEMU SPICE available ($QEMU)"
    exit 0
  fi
  echo "missing: QEMU with SPICE not available (binary: $QEMU)" >&2
  exit 2
fi

if [[ "$cmd" == "--print-vv" ]]; then
  validate_env
  print_vv
  exit 0
fi

if [[ "$cmd" == "--write-vv" ]]; then
  shift || true
  out="${1:-}"
  [[ -n "$out" ]] || die "--write-vv requires PATH"
  validate_env
  mkdir -p "$(dirname "$out")"
  print_vv >"$out"
  echo "wrote $out" >&2
  exit 0
fi

if [[ "$cmd" == "--record" ]]; then
  shift || true
  SPICE_RECORD="${1:-}"
  [[ -n "$SPICE_RECORD" ]] || die "--record requires FILE path"
fi

validate_env

if ! have_spice; then
  cat >&2 <<EOF
QEMU with SPICE not available (binary: $QEMU).

M0 gate status:
  - Live QEMU link: NOT available on this host
  - Golden ticket vectors: testdata/vectors/ticket_vectors.json (use these)
  - CONNECT fixtures: testdata/vectors/connect_authority.json

Install tips (macOS): brew install qemu  (SPICE support varies by bottle)
          (Linux):  qemu-system-x86 with spice / spice-server packages

Then re-run: $0
Integration tests: ./scripts/run_integration.sh (after this lab is up)
EOF
  exit 2
fi

# Resolve record path relative to repo root when not absolute.
if [[ -n "$SPICE_RECORD" && "$SPICE_RECORD" != /* ]]; then
  SPICE_RECORD="${ROOT}/${SPICE_RECORD}"
fi

if [[ -n "$SPICE_RECORD" ]]; then
  mkdir -p "$(dirname "$SPICE_RECORD")"
  # spice-server worker record (when supported by the linked spice-server).
  export SPICE_WORKER_RECORD_FILENAME="$SPICE_RECORD"
  echo "Recording SPICE traffic to: $SPICE_RECORD"
  echo "  (SPICE_WORKER_RECORD_FILENAME + -spice file=… hook)"
  echo "  Scrub secrets before committing; see testdata/records/README.md"
fi

echo "Starting QEMU SPICE lab on 127.0.0.1:${SPICE_PORT} password=${SPICE_PASSWORD}"
echo "Sample .vv:"
print_vv
echo "---"
echo "Cross-check: spice-viewer spice://127.0.0.1:${SPICE_PORT}?password=${SPICE_PASSWORD}"
echo "Or: spice-viewer <( $0 --print-vv )"
echo "Integration: SPICE_PORT=${SPICE_PORT} SPICE_PASSWORD=… ./scripts/run_integration.sh"
echo "Stop with Ctrl-C."
echo "---"

# Always pin addr=127.0.0.1 after port= so the lab cannot listen on all interfaces.
# Legacy password= is used for broad QEMU bottle compatibility (password-secret needs
# a separate -object secret,id=… which is overkill for this smoke script).
spice_opts="port=${SPICE_PORT},addr=127.0.0.1,password=${SPICE_PASSWORD},disable-ticketing=off"
if [[ -n "$SPICE_RECORD" ]]; then
  # QEMU -spice file= dumps traffic when the build supports it (fixture hook).
  spice_opts="${spice_opts},file=${SPICE_RECORD}"
fi

args=(
  -machine q35,accel=tcg
  -m "$MEMORY_MB"
  -display none
  -vga qxl
  -spice "$spice_opts"
  -device virtio-serial-pci
  -chardev spicevmc,id=vdagent,name=vdagent
  -device virtserialport,chardev=vdagent,name=com.redhat.spice.0
)

if [[ -n "$DISK" ]]; then
  args+=(-drive "file=${DISK},if=virtio,format=qcow2")
fi
if [[ -n "$ISO" ]]; then
  args+=(-cdrom "$ISO" -boot d)
fi
# If no disk/ISO, still run so a client can complete link+auth (may idle without guest).
if [[ -z "$DISK" && -z "$ISO" ]]; then
  args+=(-kernel /dev/null)
  # Fallback: use -machine none is too limited for qxl; prefer empty drive + -nographic already set.
  # Many QEMU builds need something bootable; use -S to pause CPU if no media.
  args+=(-S)
  echo "Note: no DISK/ISO set — VM paused (-S). Link/auth still exercisable."
fi

exec "$QEMU" "${args[@]}"
