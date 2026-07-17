# Third-party notes

This tree is reserved for notes and, if needed later, vendored references used during interop work. Phase 1 remains pure Go under Apache-2.0; do not link GPL components (e.g. spice-common) into the product binary without a separate license decision.

## Normative / reference sources (pin commits in Milestone 0)

- spice-protocol headers (wire structs, message IDs, caps)
- spice-gtk client link code + QEMU SPICE server auth (ticket OAEP)
- virt-viewer connection file (`.vv`) documentation
- Proxmox spiceproxy / public `pve-spice.vv` examples

Do not copy GPL source into this repository as build inputs for the Apache-2.0 binary without an explicit future decision.
