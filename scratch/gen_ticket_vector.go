// Throwaway generator for M0 golden ticket vectors. Not production code.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"os"
)

// fixedReader returns endless zeros after exhausting seed — used only for
// deterministic OAEP seed generation in golden vectors.
type fixedReader struct {
	seed []byte
	i    int
}

func (r *fixedReader) Read(p []byte) (int, error) {
	for i := range p {
		if r.i < len(r.seed) {
			p[i] = r.seed[r.i]
			r.i++
		} else {
			// Continue with a simple LCG so we never block; documented seed drives first bytes.
			r.seed = append(r.seed, byte(r.i*17+3))
			p[i] = r.seed[r.i]
			r.i++
		}
	}
	return len(p), nil
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	// Construct a fixed 1024-bit RSA private key from well-known primes for
	// reproducible vectors (NOT a secure key — lab/test only).
	// Generated once with rsa.GenerateKey then frozen via hardcoded PEM would also work;
	// here we generate with a deterministic PRNG seeded from a fixed byte string.
	seed := make([]byte, 256)
	for i := range seed {
		seed[i] = byte(i*37 + 11)
	}
	prng := &fixedReader{seed: seed}

	key, err := rsa.GenerateKey(prng, 1024)
	must(err)

	// Ensure e is 65537 (Go default)
	if key.E != 65537 {
		fmt.Fprintf(os.Stderr, "unexpected exponent %d\n", key.E)
		os.Exit(1)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	must(err)
	fmt.Fprintf(os.Stderr, "SPKI DER length: %d (want 162)\n", len(pubDER))
	if len(pubDER) != 162 {
		fmt.Fprintf(os.Stderr, "WARNING: DER length is %d not 162\n", len(pubDER))
	}

	privDER := x509.MarshalPKCS1PrivateKey(key)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDER})
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	password := []byte("testpass")
	plaintext := append(append([]byte{}, password...), 0) // NUL-terminated

	// OAEP seed: first 20 bytes for SHA-1 (hLen). Use fixed seed for golden ciphertext.
	oaepSeed := []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14,
	}
	// EncryptOAEP reads exactly hLen bytes from random for the seed
	oaepRand := &fixedReader{seed: oaepSeed}

	ct, err := rsa.EncryptOAEP(sha1.New(), oaepRand, &key.PublicKey, plaintext, nil)
	must(err)
	fmt.Fprintf(os.Stderr, "ciphertext length: %d (want 128)\n", len(ct))

	// Verify decrypt
	pt, err := rsa.DecryptOAEP(sha1.New(), rand.Reader, key, ct, nil)
	must(err)
	if string(pt) != string(plaintext) {
		fmt.Fprintf(os.Stderr, "decrypt mismatch: %q vs %q\n", pt, plaintext)
		os.Exit(1)
	}
	// Also verify strcmp-style (NUL-stripped)
	n := 0
	for n < len(pt) && pt[n] != 0 {
		n++
	}
	if string(pt[:n]) != string(password) {
		fmt.Fprintf(os.Stderr, "password extract fail\n")
		os.Exit(1)
	}

	// Empty password + NUL
	emptyPT := []byte{0}
	oaepRand2 := &fixedReader{seed: oaepSeed}
	ctEmpty, err := rsa.EncryptOAEP(sha1.New(), oaepRand2, &key.PublicKey, emptyPT, nil)
	must(err)

	// Max 60-byte password (spice-protocol SPICE_MAX_PASSWORD_LENGTH) + NUL
	max60 := make([]byte, 60)
	for i := range max60 {
		max60[i] = byte('A' + (i % 26))
	}
	max60PT := append(append([]byte{}, max60...), 0)
	oaepRand3 := &fixedReader{seed: oaepSeed}
	ct60, err := rsa.EncryptOAEP(sha1.New(), oaepRand3, &key.PublicKey, max60PT, nil)
	must(err)

	// 85-byte password (OAEP budget) + NUL = 86
	max85 := make([]byte, 85)
	for i := range max85 {
		max85[i] = byte('a' + (i % 26))
	}
	max85PT := append(append([]byte{}, max85...), 0)
	oaepRand4 := &fixedReader{seed: oaepSeed}
	ct85, err := rsa.EncryptOAEP(sha1.New(), oaepRand4, &key.PublicKey, max85PT, nil)
	must(err)

	// Write files
	must(os.WriteFile("testdata/vectors/ticket_rsa1024_private.pem", privPEM, 0o644))
	must(os.WriteFile("testdata/vectors/ticket_rsa1024_public.pem", pubPEM, 0o644))
	must(os.WriteFile("testdata/vectors/ticket_rsa1024_spki.der", pubDER, 0o644))

	// modulus hex for documentation
	nHex := hex.EncodeToString(key.N.Bytes())
	eHex := fmt.Sprintf("%x", key.E)

	meta := fmt.Sprintf(`# Golden AuthSpice ticket vectors (Milestone 0)
#
# Algorithm: RSAES-OAEP, hash=SHA-1, MGF1=SHA-1, label=empty
# Key: 1024-bit RSA, public exponent 65537
# SPKI DER length: %d (SPICE_TICKET_PUBKEY_BYTES = 1024/8+34 = 162)
# Ciphertext length: 128 (SPICE_TICKET_KEY_PAIR_LENGTH/8)
# Plaintext: password bytes + single trailing NUL (matches spice-gtk strlen+1)
#
# OAEP seed (hex, 20 bytes SHA-1 hLen) used for deterministic ciphertext:
#   %s
#
# WARNING: this key is TEST-ONLY, generated with a deterministic PRNG.
# Never use for real sessions.
#
# modulus (hex):
# %s
# exponent (hex): %s
#
# Files:
#   ticket_rsa1024_private.pem  PKCS#1 RSA PRIVATE KEY
#   ticket_rsa1024_public.pem   SPKI PUBLIC KEY PEM
#   ticket_rsa1024_spki.der     raw 162-byte SPKI DER (as on SpiceLinkReply.pub_key)
#   ticket_vectors.json         password → ciphertext cases
`, len(pubDER), hex.EncodeToString(oaepSeed), nHex, eHex)
	must(os.WriteFile("testdata/vectors/README.md", []byte(meta), 0o644))

	// Also emit N and D as big.Int for debugging
	_ = big.NewInt(0)
	_ = io.Discard

	json := fmt.Sprintf(`{
  "description": "AuthSpice RSAES-OAEP SHA-1 golden vectors for github.com/maskraven/virt-viewer",
  "algorithm": "RSAES-OAEP",
  "hash": "SHA-1",
  "mgf1_hash": "SHA-1",
  "label": "",
  "key_bits": 1024,
  "public_exponent": 65537,
  "spki_der_len": %d,
  "ciphertext_len": 128,
  "spki_der_hex": "%s",
  "oaep_seed_hex": "%s",
  "notes": [
    "spice-protocol SPICE_TICKET_PUBKEY_BYTES = 162",
    "spice-protocol SPICE_MAX_PASSWORD_LENGTH = 60 (client/server API limit)",
    "OAEP-SHA1 max plaintext for 1024-bit = 86 bytes including NUL → password ≤ 85",
    "spice-gtk encrypts with strlen(password)+1 (includes NUL)",
    "OpenSSL RSA_PKCS1_OAEP_PADDING defaults to SHA-1 for hash and MGF1"
  ],
  "vectors": [
    {
      "name": "testpass",
      "password_utf8": "testpass",
      "password_hex": "%s",
      "plaintext_hex": "%s",
      "ciphertext_hex": "%s"
    },
    {
      "name": "empty_password",
      "password_utf8": "",
      "password_hex": "",
      "plaintext_hex": "00",
      "ciphertext_hex": "%s"
    },
    {
      "name": "max_spice_protocol_60",
      "password_utf8": "%s",
      "password_hex": "%s",
      "plaintext_hex": "%s",
      "ciphertext_hex": "%s",
      "comment": "SPICE_MAX_PASSWORD_LENGTH=60 from spice/protocol.h"
    },
    {
      "name": "max_oaep_budget_85",
      "password_utf8": "%s",
      "password_hex": "%s",
      "plaintext_hex": "%s",
      "ciphertext_hex": "%s",
      "comment": "crypto budget 85+NUL; exceeds spice-protocol 60 but valid OAEP"
    }
  ]
}
`, len(pubDER),
		hex.EncodeToString(pubDER),
		hex.EncodeToString(oaepSeed),
		hex.EncodeToString(password),
		hex.EncodeToString(plaintext),
		hex.EncodeToString(ct),
		hex.EncodeToString(ctEmpty),
		string(max60),
		hex.EncodeToString(max60),
		hex.EncodeToString(max60PT),
		hex.EncodeToString(ct60),
		string(max85),
		hex.EncodeToString(max85),
		hex.EncodeToString(max85PT),
		hex.EncodeToString(ct85),
	)
	must(os.WriteFile("testdata/vectors/ticket_vectors.json", []byte(json), 0o644))
	fmt.Fprintln(os.Stderr, "wrote testdata/vectors/*")
}
