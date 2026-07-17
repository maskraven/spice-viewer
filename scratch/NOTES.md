# Scratch — Milestone 0 throwaways

Not production packages. Safe to delete after PR 04 consumes golden vectors.

| File | Purpose |
|------|---------|
| `gen_ticket_vector.go` | Deterministic RSA-1024 + OAEP-SHA1 vector generator |
| `verify_vectors.go` | One-shot decrypt check of `testdata/vectors/ticket_vectors.json` |

Run from repo root:

```bash
go run scratch/gen_ticket_vector.go
go run scratch/verify_vectors.go
```

Upstream clones used during research lived under `/tmp/spice-research` and are not part of this repo.
