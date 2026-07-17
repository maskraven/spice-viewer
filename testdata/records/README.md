# SPICE record fixtures

This directory holds **optional** recorded SPICE traffic for offline interop
debugging and future replay harnesses. Unit CI does not require any real
record files; `.gitkeep` keeps the path in git.

## How to record

1. Start a local password SPICE lab and write a record file:

   ```bash
   # From repo root
   ./scripts/interop_qemu.sh --record testdata/records/lab-link.rec
   ```

   Equivalent env form:

   ```bash
   SPICE_RECORD=testdata/records/lab-link.rec ./scripts/interop_qemu.sh
   ```

2. In another terminal, exercise the client (integration tests or spice-viewer):

   ```bash
   ./scripts/run_integration.sh
   # or
   spice-viewer <( ./scripts/interop_qemu.sh --print-vv )
   ```

3. Stop QEMU (Ctrl-C). The record path is set via:

   - `SPICE_WORKER_RECORD_FILENAME` (spice-server worker dump when supported)
   - QEMU `-spice …,file=<path>` (QEMU SPICE dump hook when supported)

   Not every QEMU bottle enables both hooks. If the file is empty or missing,
   re-run with a SPICE-enabled QEMU package and check `qemu-system-x86_64 -spice help`.

## Placeholders

| Path | Purpose |
|------|---------|
| `.gitkeep` | Keep empty directory in git |
| `*.rec` / `*.spice` | Local captures (gitignored patterns recommended; do not commit secrets) |

Suggested local naming:

```text
testdata/records/lab-YYYYMMDD-link.rec
testdata/records/lab-YYYYMMDD-display.rec
```

## Secrets policy

Record files may contain:

- Ticket / password material on the wire (AuthSpice ciphertext and related state)
- Display pixel data from the guest
- TLS-unrelated cleartext SPICE for the local lab path

**Do not commit** raw captures that embed production tickets, `.vv` passwords,
or private guest content. Scrub or keep records local-only.

For password logging in the Go client, unit tests in
`internal/security/redact_test.go` enforce that secrets never appear in logs.

## Integration tests (`//go:build integration`)

```bash
# Terminal 1
./scripts/interop_qemu.sh

# Terminal 2
./scripts/run_integration.sh
```

Environment defaults match the lab script (`SPICE_HOST=127.0.0.1`,
`SPICE_PORT=5900`, `SPICE_PASSWORD=testpass`).

Default CI runs `go test ./...` **without** `-tags=integration`. See
`scripts/README.md` and `.github/workflows/ci.yml`.
