# scripts

Helper scripts for CI, local development, and QEMU SPICE interop.

| Script | Purpose |
|--------|---------|
| `check_imports.sh` | Enforce package import boundaries (see design doc) |
| `interop_qemu.sh` | Local QEMU SPICE password lab + record fixture hooks |
| `run_integration.sh` | Run `//go:build integration` tests against the lab |
| `milestone0_memo.md` | Milestone 0 crypto / CONNECT / DN gate notes |
| `linux/build-product.sh` | Linux tar.gz + deb/rpm (GoReleaser) |
| `macos/build-product.sh` | Universal macOS `.app` + `.dmg` + zip |
| `windows/build-product.ps1` | Windows GUI `.exe` + zip + optional NSIS |

Product packaging details: [packaging/README.md](../packaging/README.md).

## QEMU interop lab

```bash
# Capability check (exit 0 = QEMU+SPICE present)
./scripts/interop_qemu.sh --check

# Start cleartext password SPICE on 127.0.0.1:5900 (default password testpass)
./scripts/interop_qemu.sh

# Print / write a sample .vv for remote-viewer or tests
./scripts/interop_qemu.sh --print-vv
./scripts/interop_qemu.sh --write-vv /tmp/lab.vv

# Record SPICE traffic (fixture hook; see testdata/records/README.md)
./scripts/interop_qemu.sh --record testdata/records/lab-link.rec
SPICE_RECORD=testdata/records/lab-link.rec ./scripts/interop_qemu.sh
```

Environment:

| Variable | Default | Notes |
|----------|---------|--------|
| `SPICE_PORT` | `5900` | Digits only; lab binds `127.0.0.1` only |
| `SPICE_PASSWORD` | `testpass` | No commas/newlines (QEMU `-spice` CSV) |
| `SPICE_RECORD` | empty | Optional dump path |
| `QEMU` | `qemu-system-x86_64` | Override binary |
| `DISK` / `ISO` | empty | Optional guest media; without them VM is paused (`-S`) |

## Integration tests (`//go:build integration`)

Live tests live in packages such as `internal/session` and are **not** built
during default `go test ./...` (no build tag). They dial a real SPICE server.

### How to run

```bash
# Terminal 1 — lab
./scripts/interop_qemu.sh

# Terminal 2 — tests
./scripts/run_integration.sh

# Equivalent manual invocation
go test -tags=integration -count=1 ./internal/session/ -run TestQEMU
```

Override endpoint:

```bash
SPICE_HOST=127.0.0.1 SPICE_PORT=5900 SPICE_PASSWORD=testpass \
  ./scripts/run_integration.sh
```

If nothing is listening, tests **Skip** with a pointer to this script (they do
not fail the unit suite when the tag is off).

### CI policy

| Job | Command | QEMU required |
|-----|---------|---------------|
| Default unit (`ci.yml` `test`) | `go test ./...` | No |
| Local / optional integration | `./scripts/run_integration.sh` | Yes |

The GitHub Actions unit job intentionally omits `-tags=integration` so PRs stay
hermetic. Operators and maintainers run the integration path against a lab host
before releases that touch session/link/auth.

## Log redaction

Unit tests in `internal/security` (`redact_test.go`) fail if a password would
appear in log output through `security.Redact` / `RedactingWriter`. Use those
helpers for any debug logging of session configuration.
