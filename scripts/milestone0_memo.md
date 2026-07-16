# Milestone 0 decision memo — ticket crypto, CONNECT, subject-pin, QEMU lab

| Field | Value |
|-------|-------|
| **Status** | Complete (vectors + fixtures + lab script; no live QEMU required) |
| **Date** | 2026-07-17 |
| **Module path** | `github.com/maskraven/virt-viewer` |
| **License** | Apache-2.0 |
| **Phase 1** | Pure Go only |

This memo pins **upstream file paths and commit SHAs** used as normative references for PR 00–04 and session/channel work (PR 06–11). Claims in `docs/design-spice-viewer-go.md` about ticket crypto, CONNECT authority, and DN matching were re-proven against source.

---

## 1. Pinned upstream revisions

| Project | Commit SHA | Notes |
|---------|------------|-------|
| **spice-protocol** | `499cc8326a672e9e5747efc017319b19e1594b42` | GitLab `spice/spice-protocol` master (2025-08-26) |
| **spice-gtk** | `88ad5f14eb6db10dcd440bbe03cfd09af61a6e2c` | GitLab `spice/spice-gtk` master (2026-06-03) |
| **spice-common** | `71e45706981973014eaab3d4b533d35d79e19ffa` | GitLab `spice/spice-common` (ssl_verify; 2025-08-30) |
| **spice server** | `91d42c4d8de76ca00420fc112c17a82772bd1dd0` | GitLab `spice/spice` (reds ticket decrypt) |
| **virt-viewer** | `dbb35f4eb692813ddf7ef1f06c21b0266c7267ec` | GitLab `virt-viewer/virt-viewer` |
| **QEMU** (spice core opts) | `73ae0be3f14b…` tip at research time on `ui/spice-core.c` | Ticket set via `spice_server_set_ticket`; crypto is in spice-server |
| **Proxmox qemu-server** | `b69480d6110c005b9eb936c55c0438607d10975b` | `spiceproxy` API endpoint |
| **Proxmox pve-access-control** | `5ccd07d9302562b73374d331b63d25b04b86766c` | `remote_viewer_config`, `host-subject` |
| **Proxmox pve-common** | `f1c3703aab2e6734d450b84f34708ac57b23a3aa` | `PVE::Ticket` proxy ticket format |

Secondary confirmation (non-normative): Shaken Fist Kerbside link-protocol writeup; SPICE protocol HTML at spice-space.org (documents `SPICE_TICKET_PUBKEY_BYTES = 162` and EME-OAEP SHA-1).

---

## 2. Ticket encryption (AuthSpice)

### 2.1 Normative layout — spice-protocol

**File:** `spice/protocol.h` @ `499cc8326a672e9e5747efc017319b19e1594b42`

```c
#define SPICE_MAX_PASSWORD_LENGTH 60
#define SPICE_TICKET_KEY_PAIR_LENGTH 1024
#define SPICE_TICKET_PUBKEY_BYTES (SPICE_TICKET_KEY_PAIR_LENGTH / 8 + 34)  /* = 162 */

typedef struct SpiceLinkReply {
    uint32_t error;
    uint8_t pub_key[SPICE_TICKET_PUBKEY_BYTES];  /* X.509 SPKI DER */
    uint32_t num_common_caps;
    uint32_t num_channel_caps;
    uint32_t caps_offset;
} SpiceLinkReply;

typedef struct SpiceLinkEncryptedTicket {
    uint8_t encrypted_data[SPICE_TICKET_KEY_PAIR_LENGTH / 8];  /* 128 bytes */
} SpiceLinkEncryptedTicket;
```

**Confirmed:** 1024-bit RSA SPKI with e=65537 is **exactly 162 bytes** DER (measured with Go `x509.MarshalPKIXPublicKey` in this spike). Phase 1 should **reject** non-162 `pub_key` lengths.

Auth mechanism after capability negotiation: `SPICE_COMMON_CAP_AUTH_SPICE` → mechanism **1**. Phase 1 implements AuthSpice only (no SASL).

### 2.2 Client encrypt — spice-gtk

**File:** `src/spice-channel.c` → `spice_channel_send_spice_ticket` @ `88ad5f14…`

| Step | Behavior |
|------|----------|
| Password length | Reject if `strlen(password) > SPICE_MAX_PASSWORD_LENGTH` (**60**) |
| Pubkey parse | `BIO_write(..., pub_key, SPICE_TICKET_PUBKEY_BYTES)` + `d2i_PUBKEY_bio` |
| Plaintext | `strlen(password) + 1` — **includes trailing NUL** |
| Padding | `RSA_PKCS1_OAEP_PADDING` / `EVP_PKEY_CTX_set_rsa_padding(..., RSA_PKCS1_OAEP_PADDING)` |
| Hash (implicit) | OpenSSL default for OAEP is **SHA-1** for digest and MGF1 |
| Output | 128-byte ciphertext written raw (no length prefix beyond auth packet framing) |

Auth packet shape (with AUTH_SELECTION cap): `uint32 mechanism` + 128-byte ciphertext.

### 2.3 Server decrypt — spice server

**File:** `server/reds.cpp` → `reds_handle_ticket` @ `91d42c4…`

- Decrypt with `RSA_PKCS1_OAEP_PADDING` (SHA-1 default).
- Compare with `strcmp(password, taTicket.password)` (NUL-terminated C string).
- Enforce ticket expiration on **every** channel link (not only main).
- Password empty while ticketing enabled → deny.

### 2.4 Password length: 60 vs 85

| Limit | Value | Source |
|-------|-------|--------|
| Protocol/API max | **60** | `SPICE_MAX_PASSWORD_LENGTH` in spice-protocol; spice-gtk client reject; Proxmox ticket comment “max length is 60 chars (spice limit)”; PVE password is SHA-1 hex = 40 chars |
| OAEP crypto max | **85** (+ NUL → 86 plaintext) | RSA-1024, SHA-1: `k - 2*hLen - 2 = 128 - 40 - 2 = 86` |

**Phase 1 decision for this repo:** keep design’s **85** as the hard crypto reject in `EncryptSpiceTicket`, and also enforce **≤60** at `.vv` parse for spice-protocol interop (or document that we accept up to 85 for lab keys). **Recommendation:** reject `password > 60` at `vvfile` parse (match spice-gtk + PVE), keep encrypt budget assert at 85 as defense in depth. Golden vectors include both 60 and 85 cases.

### 2.5 Golden vectors (M0 gate)

Delivered under `testdata/vectors/`:

| File | Purpose |
|------|---------|
| `ticket_rsa1024_spki.der` | Fixed 162-byte SPKI (as `SpiceLinkReply.pub_key`) |
| `ticket_rsa1024_public.pem` / `ticket_rsa1024_private.pem` | Same key PEM |
| `ticket_vectors.json` | Password → ciphertext (hex), deterministic OAEP seed |
| `README.md` | Seed + algorithm notes |

**Password `"testpass"`** + NUL → ciphertext present; decrypt verified with Go `rsa.DecryptOAEP(sha1.New(), …)`.

**OAEP parameters for production code:**

```go
// plaintext = append(password, 0)
rsa.EncryptOAEP(sha1.New(), rand.Reader, pub, plaintext, nil /* label */)
```

No `golang.org/x/crypto` required.

Generator (throwaway): `scratch/gen_ticket_vector.go`.

---

## 3. CONNECT authority (opaque multi-colon host)

### 3.1 Proxmox host token

**Files:**

- `pve-common/src/PVE/Ticket.pm` → `assemble_spice_ticket`, `verify_spice_connect_url` @ `f1c3703a…`
- `pve-access-control/src/PVE/AccessControl.pm` → `remote_viewer_config` @ `5ccd07d…`
- `qemu-server/src/PVE/API2/Qemu.pm` → `spiceproxy` POST @ `b69480d…`

Proxy ticket construction:

```text
plain = "pvespiceproxy:" + timestamp8hex + ":" + vmid + ":" + lc(node) [+ ":" + port]
proxyticket = plain + "::" + sha1_hex(plain, secret)   # 40 hex chars
```

`.vv` fields from `remote_viewer_config`:

| Key | Value |
|-----|--------|
| `host` | `$proxyticket` (opaque; breaks TLS hostname verify) |
| `proxy` | `http://$proxy:3128` |
| `tls-port` | QEMU SPICE port |
| `password` | 40-char SHA-1 hex one-time ticket (set on QEMU, TTL +30s) |
| `ca` | PEM with `\n` escapes |
| `host-subject` | from `pve-ssl.pem` subject, `/` → `,` |
| `delete-this-file` | `1` |

`verify_spice_connect_url` documents: viewer CONNECT target is **`$ticket:$port`** (host field + `:` + tls-port). Lifetime window roughly −20s…+40s around timestamp.

### 3.2 Client algorithm (normative for this project)

```text
connectTarget = host + ":" + decimal(port)   // host is literal .vv value
// TCP dial ONLY proxy URL host:port
// TLS ServerName MUST NOT be the pvespiceproxy token
```

**Do not** call `net.SplitHostPort` / `JoinHostPort` / URL hostname parsers on `host`.

### 3.3 CONNECT fixtures (M0 gate)

File: `testdata/vectors/connect_authority.json`

Exact expected line (design doc + fixture `design_doc_old_style_token`):

```http
CONNECT pvespiceproxy:687d1ec6:10016:pve::dcc9e35662ef0b1233e12ac02880ea7851f9218e:61002 HTTP/1.1
```

Also covers new-style tokens that embed the SPICE port before `::sig`.

---

## 4. TLS + host-subject DN verification

### 4.1 spice-gtk path

**Files:**

- `spice-gtk/src/spice-channel.c` — loads CA from session (`ca` bytes / `ca-file`), `spice_openssl_verify_new(...)`, optional SNI when host is not an IP.
- `spice-common/common/ssl_verify.c` @ `71e45706…` — `subject_to_x509_name`, `verify_subject`, `verify_hostname`.

**Subject pin algorithm (spice-gtk):**

1. Parse `cert-subject` / host-subject as `TYPE=value` pairs split on unescaped `,` (`\,` and `\\` supported).
2. Build `X509_NAME` entry list **in order**.
3. Require same RDN **count** as leaf certificate subject.
4. `X509_NAME_cmp(cert, expected) == 0` (order-sensitive, all RDNs).

### 4.2 Design algorithm (Phase 1 Go)

Manual verify after `InsecureSkipVerify: true` only to disable Go hostname check:

1. Parse chain; verify leaf with `x509.VerifyOptions{Roots: .vv ca pool, …}` (no `DNSName` in pin mode).
2. `subjectMatches`: parse expected DN; compare CN (fold) + O + OU (+ optional C/ST/L). **Not** `pkix.Name.String()` equality.

### 4.3 Findings vs spice-gtk (document for implementers)

| Case | Design subset match | spice-gtk `X509_NAME_cmp` |
|------|---------------------|---------------------------|
| Identical RDNs, same order | match | match |
| Extra RDN on cert only | **match** (more permissive) | **reject** (count mismatch) |
| Swapped RDN order in host-subject string | **match** | **reject** |
| CN mismatch | reject | reject |

Proxmox writes **full** subject from `pve-ssl.pem` into `host-subject`, so production PVE files usually align both algorithms. Fixtures: `testdata/vectors/dn_subject_fixtures.json`.

**Phase 1 recommendation:** implement design subset match; add a test that records divergence cases. If a real PVE cert fails only due to ordering, switch to ordered full-RDN compare for pin mode.

**SNI:** Proxmox mode must leave `tls.Config.ServerName` empty (token is not a DNS name). spice-gtk sets SNI only when host is not a parseable IP; for `pvespiceproxy:…` it would attempt SNI with the token — OpenSSL may ignore invalid names, but Go should not set ServerName to the token.

---

## 5. virt-viewer `.vv` documentation

**Files:**

- `man/remote-viewer.pod` @ `dbb35f4…` — documents `type`, `host`, `port`, `tls-port`, `password`, `proxy`, `ca`, `host-subject`, `delete-this-file`, hotkeys, etc.
- `src/virt-viewer-file.c` — key list and getters/setters.

Semantics relevant to Phase 1 match the design matrix (honor `delete-this-file`, required type=spice, etc.).

---

## 6. QEMU lab path

**Script:** `scripts/interop_qemu.sh`

- Starts `qemu-system-x86_64` with `-spice port=…,password=…` when SPICE-capable QEMU is installed.
- `--print-vv` emits a cleartext lab connection file.
- Exit code **2** if QEMU/SPICE unavailable — **not a failed M0**: use golden vectors instead.

**Live QEMU on this spike host:** not required / may be absent. M0 acceptance satisfied by golden ticket vectors + CONNECT fixtures + this memo.

**Manual interop checklist (when QEMU available):**

1. `./scripts/interop_qemu.sh` (or with `DISK=` guest image).
2. `remote-viewer spice://127.0.0.1:5900?password=testpass` — baseline.
3. Future `go test` / headless client: main channel link + AuthSpice decrypt OK.

---

## 7. M0 exit criteria checklist

| # | Criterion | Status |
|---|-----------|--------|
| 1 | Memo cites upstream paths + commit SHAs (spice-protocol, spice-gtk, virt-viewer, Proxmox) | **Yes** (this document) |
| 2 | Live QEMU link OK **or** golden ticket vectors (fixed RSA + password → ciphertext) | **Yes** — `testdata/vectors/ticket_vectors.json` (+ DER/PEM) |
| 3 | CONNECT mock fixture with multi-colon host exact line | **Yes** — `testdata/vectors/connect_authority.json` |
| 4 | DN matching findings vs spice-gtk documented | **Yes** — §4 + `dn_subject_fixtures.json` |

---

## 8. Implications for later PRs

| PR | Use of M0 outputs |
|----|-------------------|
| PR 03 connector | `connect_authority.json` exact CONNECT line tests; TLS pin mode ServerName empty |
| PR 04 protocol/security | Load `ticket_vectors.json`; fail-closed decrypt test; 162-byte SPKI parse; NUL + OAEP-SHA1 |
| PR 06+ session | Re-encrypt ticket per channel; parallel open vs PVE 30s TTL |
| vvfile | Prefer password ≤60 (protocol); never log secrets |

---

## 9. References (URLs)

- https://gitlab.freedesktop.org/spice/spice-protocol
- https://gitlab.freedesktop.org/spice/spice-gtk
- https://gitlab.freedesktop.org/spice/spice-common
- https://gitlab.freedesktop.org/spice/spice
- https://gitlab.com/virt-viewer/virt-viewer
- https://github.com/proxmox/qemu-server
- https://github.com/proxmox/pve-access-control
- https://github.com/proxmox/pve-common
- https://www.spice-space.org/spice-protocol.html
