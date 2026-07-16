// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package security

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fixedReader returns seed bytes then extends deterministically (same as scratch/gen_ticket_vector.go).
// OAEP-SHA1 consumes the first hLen (20) bytes as the seed for golden ciphertexts.
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
			r.seed = append(r.seed, byte(r.i*17+3))
			p[i] = r.seed[r.i]
			r.i++
		}
	}
	return len(p), nil
}

type ticketVectorsDoc struct {
	SPKIDERHex    string `json:"spki_der_hex"`
	OAEPSeedHex   string `json:"oaep_seed_hex"`
	CiphertextLen int    `json:"ciphertext_len"`
	SPKIDERLen    int    `json:"spki_der_len"`
	Vectors       []struct {
		Name          string `json:"name"`
		PasswordUTF8  string `json:"password_utf8"`
		PasswordHex   string `json:"password_hex"`
		PlaintextHex  string `json:"plaintext_hex"`
		CiphertextHex string `json:"ciphertext_hex"`
	} `json:"vectors"`
}

func vectorsDir(t *testing.T) string {
	t.Helper()
	// testdata/vectors is at module root; locate via this source file.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/security -> repo root
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	dir := filepath.Join(root, "testdata", "vectors")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("testdata/vectors not found at %s: %v", dir, err)
	}
	return dir
}

func loadVectors(t *testing.T) (ticketVectorsDoc, []byte, *rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	dir := vectorsDir(t)

	raw, err := os.ReadFile(filepath.Join(dir, "ticket_vectors.json"))
	if err != nil {
		t.Fatalf("read ticket_vectors.json: %v", err)
	}
	var doc ticketVectorsDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse ticket_vectors.json: %v", err)
	}
	if doc.SPKIDERLen != SpiceLinkPubKeyDERLen {
		t.Fatalf("vector spki_der_len %d want %d", doc.SPKIDERLen, SpiceLinkPubKeyDERLen)
	}
	if doc.CiphertextLen != SpiceTicketCiphertextLen {
		t.Fatalf("vector ciphertext_len %d want %d", doc.CiphertextLen, SpiceTicketCiphertextLen)
	}

	spki, err := os.ReadFile(filepath.Join(dir, "ticket_rsa1024_spki.der"))
	if err != nil {
		t.Fatalf("read spki der: %v", err)
	}
	if len(spki) != SpiceLinkPubKeyDERLen {
		t.Fatalf("spki der len %d want %d", len(spki), SpiceLinkPubKeyDERLen)
	}
	if hex.EncodeToString(spki) != doc.SPKIDERHex {
		t.Fatal("spki der file does not match ticket_vectors.json spki_der_hex")
	}

	privPEM, err := os.ReadFile(filepath.Join(dir, "ticket_rsa1024_private.pem"))
	if err != nil {
		t.Fatalf("read private pem: %v", err)
	}
	block, _ := pem.Decode(privPEM)
	if block == nil {
		t.Fatal("decode private pem: no block")
	}
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}

	pub, err := ParseLinkPublicKey(spki)
	if err != nil {
		t.Fatalf("ParseLinkPublicKey: %v", err)
	}
	if pub.N.Cmp(priv.N) != 0 {
		t.Fatal("public key modulus does not match private key")
	}
	return doc, spki, priv, pub
}

// TestTicketVectors_DecryptFailClosed verifies every golden ciphertext decrypts
// to password+NUL. Fail-closed: any vector failure fails the test (CI gate).
func TestTicketVectors_DecryptFailClosed(t *testing.T) {
	doc, _, priv, _ := loadVectors(t)

	for _, v := range doc.Vectors {
		v := v
		t.Run(v.Name, func(t *testing.T) {
			ct, err := hex.DecodeString(v.CiphertextHex)
			if err != nil {
				t.Fatalf("ciphertext hex: %v", err)
			}
			if len(ct) != SpiceTicketCiphertextLen {
				t.Fatalf("ciphertext len %d want %d", len(ct), SpiceTicketCiphertextLen)
			}
			pt, err := rsa.DecryptOAEP(sha1.New(), nil, priv, ct, nil)
			if err != nil {
				t.Fatalf("DecryptOAEP: %v", err)
			}
			wantPT, err := hex.DecodeString(v.PlaintextHex)
			if err != nil {
				t.Fatalf("plaintext hex: %v", err)
			}
			if !bytes.Equal(pt, wantPT) {
				t.Fatalf("plaintext mismatch:\n got %x\nwant %x", pt, wantPT)
			}
			// NUL terminator: last byte must be 0; password is prefix without NUL.
			if len(pt) == 0 || pt[len(pt)-1] != 0 {
				t.Fatalf("plaintext missing trailing NUL: %x", pt)
			}
			password := pt[:len(pt)-1]
			if string(password) != v.PasswordUTF8 {
				t.Fatalf("password extract: got %q want %q", password, v.PasswordUTF8)
			}
		})
	}
}

// TestTicketVectors_EncryptKnown re-encrypts with the fixed OAEP seed for passwords
// within MaxSpicePasswordLen and checks ciphertext matches goldens.
func TestTicketVectors_EncryptKnown(t *testing.T) {
	doc, _, _, pub := loadVectors(t)
	seed, err := hex.DecodeString(doc.OAEPSeedHex)
	if err != nil {
		t.Fatalf("oaep_seed_hex: %v", err)
	}
	if len(seed) != sha1.Size {
		t.Fatalf("oaep seed len %d want %d", len(seed), sha1.Size)
	}

	for _, v := range doc.Vectors {
		v := v
		t.Run(v.Name, func(t *testing.T) {
			password := []byte(v.PasswordUTF8)
			if len(password) > MaxSpicePasswordLen {
				// Protocol-max encrypt path cannot produce OAEP-budget-only vectors.
				// Decrypt path is covered by TestTicketVectors_DecryptFailClosed.
				t.Skipf("password len %d > MaxSpicePasswordLen %d; decrypt-only vector",
					len(password), MaxSpicePasswordLen)
			}
			r := &fixedReader{seed: append([]byte(nil), seed...)}
			ct, err := EncryptSpiceTicketWithRandom(r, pub, password)
			if err != nil {
				t.Fatalf("EncryptSpiceTicketWithRandom: %v", err)
			}
			want, err := hex.DecodeString(v.CiphertextHex)
			if err != nil {
				t.Fatalf("ciphertext hex: %v", err)
			}
			if !bytes.Equal(ct, want) {
				t.Fatalf("ciphertext mismatch:\n got %x\nwant %x", ct, want)
			}
		})
	}
}

func TestParseLinkPublicKey_RejectWrongLength(t *testing.T) {
	_, spki, _, _ := loadVectors(t)

	if _, err := ParseLinkPublicKey(nil); err == nil {
		t.Fatal("expected error for nil der")
	}
	if _, err := ParseLinkPublicKey(spki[:161]); err == nil {
		t.Fatal("expected error for 161-byte der")
	}
	if _, err := ParseLinkPublicKey(append(spki, 0)); err == nil {
		t.Fatal("expected error for 163-byte der")
	}
	// Wrong length even if inner bytes look like DER.
	junk := make([]byte, SpiceLinkPubKeyDERLen)
	if _, err := ParseLinkPublicKey(junk); err == nil {
		t.Fatal("expected parse error for invalid DER of correct length")
	}
	pub, err := ParseLinkPublicKey(spki)
	if err != nil {
		t.Fatalf("valid SPKI: %v", err)
	}
	if pub == nil {
		t.Fatal("nil pub")
	}
}

func TestEncryptSpiceTicket_RejectOverMax(t *testing.T) {
	_, _, _, pub := loadVectors(t)

	tooLong := make([]byte, MaxSpicePasswordLen+1)
	for i := range tooLong {
		tooLong[i] = 'a'
	}
	if _, err := EncryptSpiceTicket(pub, tooLong); err == nil {
		t.Fatalf("expected reject for password len %d", len(tooLong))
	}

	// OAEP budget case (85) exceeds protocol max (60).
	oaepMax := make([]byte, MaxOAEPPasswordLen)
	for i := range oaepMax {
		oaepMax[i] = 'b'
	}
	if _, err := EncryptSpiceTicket(pub, oaepMax); err == nil {
		t.Fatalf("expected reject for OAEP-budget password len %d (protocol max %d)",
			len(oaepMax), MaxSpicePasswordLen)
	}
}

func TestEncryptSpiceTicket_NULTerminatorAndRoundTrip(t *testing.T) {
	_, _, priv, pub := loadVectors(t)

	password := []byte("testpass")
	ct, err := EncryptSpiceTicket(pub, password)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(ct) != SpiceTicketCiphertextLen {
		t.Fatalf("ct len %d", len(ct))
	}
	pt, err := rsa.DecryptOAEP(sha1.New(), nil, priv, ct, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(pt, append(password, 0)) {
		t.Fatalf("want password+NUL, got %x", pt)
	}

	// Empty password is password "" + NUL only.
	ctEmpty, err := EncryptSpiceTicket(pub, nil)
	if err != nil {
		t.Fatalf("encrypt empty: %v", err)
	}
	ptEmpty, err := rsa.DecryptOAEP(sha1.New(), nil, priv, ctEmpty, nil)
	if err != nil {
		t.Fatalf("decrypt empty: %v", err)
	}
	if !bytes.Equal(ptEmpty, []byte{0}) {
		t.Fatalf("empty: want single NUL, got %x", ptEmpty)
	}

	// Max protocol password (60).
	max60 := make([]byte, MaxSpicePasswordLen)
	for i := range max60 {
		max60[i] = byte('A' + (i % 26))
	}
	ct60, err := EncryptSpiceTicket(pub, max60)
	if err != nil {
		t.Fatalf("encrypt max60: %v", err)
	}
	pt60, err := rsa.DecryptOAEP(sha1.New(), nil, priv, ct60, nil)
	if err != nil {
		t.Fatalf("decrypt max60: %v", err)
	}
	if !bytes.Equal(pt60, append(max60, 0)) {
		t.Fatalf("max60 plaintext mismatch")
	}
}

func TestConstants(t *testing.T) {
	if SpiceLinkPubKeyDERLen != 162 {
		t.Fatalf("SpiceLinkPubKeyDERLen = %d", SpiceLinkPubKeyDERLen)
	}
	if MaxSpicePasswordLen != 60 {
		t.Fatalf("MaxSpicePasswordLen = %d want 60 (SPICE_MAX_PASSWORD_LENGTH)", MaxSpicePasswordLen)
	}
	if MaxOAEPPasswordLen != 85 {
		t.Fatalf("MaxOAEPPasswordLen = %d want 85", MaxOAEPPasswordLen)
	}
	if SpiceTicketCiphertextLen != 128 {
		t.Fatalf("SpiceTicketCiphertextLen = %d", SpiceTicketCiphertextLen)
	}
}
