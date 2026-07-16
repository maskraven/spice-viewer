#!/usr/bin/env bash
# interop_qemu.sh — lab helper to run QEMU with SPICE ticketing for AuthSpice interop.
#
# Milestone 0: proves ticket crypto / link path against a real SPICE server when QEMU
# is available. If QEMU+SPICE is missing, golden vectors in testdata/vectors/ remain
# the M0 gate (see scripts/milestone0_memo.md).
#
# Usage:
#   ./scripts/interop_qemu.sh              # start QEMU SPICE lab (foreground)
#   ./scripts/interop_qemu.sh --print-vv   # print a sample .vv for direct cleartext lab
#   SPICE_PORT=5900 SPICE_PASSWORD=testpass ./scripts/interop_qemu.sh
#
# Requirements: qemu-system-x86_64 (or $QEMU) built with SPICE support.
# Optional: remote-viewer for manual cross-check.
#
# Safety: SPICE always binds addr=127.0.0.1 (localhost only). SPICE_PORT must be
# digits-only; SPICE_PASSWORD must not contain ',' or newlines (QEMU -spice CSV).
set -euo pipefail

SPICE_PORT="${SPICE_PORT:-5900}"
SPICE_PASSWORD="${SPICE_PASSWORD:-testpass}"
QEMU="${QEMU:-qemu-system-x86_64}"
MEMORY_MB="${MEMORY_MB:-256}"
# Empty machine is fine for SPICE bring-up smoke; supply DISK/ISO for full guest.
DISK="${DISK:-}"
ISO="${ISO:-}"

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

if [[ "${1:-}" == "--print-vv" ]]; then
  validate_env
  print_vv
  exit 0
fi

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  sed -n '2,20p' "$0"
  exit 0
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
EOF
  exit 2
fi

echo "Starting QEMU SPICE lab on 127.0.0.1:${SPICE_PORT} password=${SPICE_PASSWORD}"
echo "Sample .vv:"
print_vv
echo "---"
echo "Cross-check: remote-viewer spice://127.0.0.1:${SPICE_PORT}?password=${SPICE_PASSWORD}"
echo "Or: remote-viewer <( $0 --print-vv )"
echo "Stop with Ctrl-C."
echo "---"

# Always pin addr=127.0.0.1 after port= so the lab cannot listen on all interfaces.
# Legacy password= is used for broad QEMU bottle compatibility (password-secret needs
# a separate -object secret,id=… which is overkill for this smoke script).
args=(
  -machine q35,accel=tcg
  -m "$MEMORY_MB"
  -display none
  -vga qxl
  -spice "port=${SPICE_PORT},addr=127.0.0.1,password=${SPICE_PASSWORD},disable-ticketing=off"
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
