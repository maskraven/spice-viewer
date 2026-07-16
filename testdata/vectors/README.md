# Golden AuthSpice ticket vectors (Milestone 0)
#
# Algorithm: RSAES-OAEP, hash=SHA-1, MGF1=SHA-1, label=empty
# Key: 1024-bit RSA, public exponent 65537
# SPKI DER length: 162 (SPICE_TICKET_PUBKEY_BYTES = 1024/8+34 = 162)
# Ciphertext length: 128 (SPICE_TICKET_KEY_PAIR_LENGTH/8)
# Plaintext: password bytes + single trailing NUL (matches spice-gtk strlen+1)
#
# OAEP seed (hex, 20 bytes SHA-1 hLen) used for deterministic ciphertext:
#   0102030405060708090a0b0c0d0e0f1011121314
#
# WARNING: this key is TEST-ONLY, generated with a deterministic PRNG.
# Never use for real sessions.
#
# modulus (hex):
# af8a6abae05d9f006374bbc7bdfec4896179172262fb1495ebe17e3b70992d418a82e3cdedbaac1f1a56b511ddc2b97b7bd9008d8f5c6cf39507cf11050736710396baa7a084ef83d2ddd5db03e68e521829391b57782f9ae9edd477768ecc0b896b31c34081ee7c8837346b35802ceef6514996f0f1fa89dda512344a9733b7
# exponent (hex): 10001
#
# Files:
#   ticket_rsa1024_private.pem  PKCS#1 RSA PRIVATE KEY
#   ticket_rsa1024_public.pem   SPKI PUBLIC KEY PEM
#   ticket_rsa1024_spki.der     raw 162-byte SPKI DER (as on SpiceLinkReply.pub_key)
#   ticket_vectors.json         password → ciphertext cases

## Fixture index

| File | Purpose |
|------|---------|
| `ticket_vectors.json` | AuthSpice OAEP-SHA1 password → ciphertext |
| `ticket_rsa1024_spki.der` | 162-byte SPKI DER (SpiceLinkReply.pub_key shape) |
| `ticket_rsa1024_*.pem` | Same key in PEM form |
| `connect_authority.json` | Opaque CONNECT authority lines for pvespiceproxy hosts |
| `dn_subject_fixtures.json` | host-subject match cases vs spice-gtk X509_NAME_cmp |

See `scripts/milestone0_memo.md` for pinned upstream SHAs.
