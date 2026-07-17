// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package security

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"fmt"
	"io"
)

// Spice ticket encryption (AuthSpice) constants from spice-protocol.
//
// spice/protocol.h @ 499cc8326a672e9e5747efc017319b19e1594b42:
//
//	SPICE_MAX_PASSWORD_LENGTH = 60
//	SPICE_TICKET_KEY_PAIR_LENGTH = 1024
//	SPICE_TICKET_PUBKEY_BYTES = 1024/8 + 34 = 162
//
// Client encrypt: spice-gtk spice_channel_send_spice_ticket (OAEP + SHA-1,
// plaintext = password + trailing NUL). Server decrypt: spice reds_handle_ticket.
const (
	// SpiceLinkPubKeyDERLen is SPICE_TICKET_PUBKEY_BYTES: on-wire SPKI DER length
	// for the historical 1024-bit RSA server key in SpiceLinkReply.pub_key.
	SpiceLinkPubKeyDERLen = 162

	// MaxSpicePasswordLen is SPICE_MAX_PASSWORD_LENGTH. spice-gtk rejects longer
	// passwords; spice-server ticket compare uses C strings under this limit.
	// EncryptSpiceTicket enforces this for interop with spice-gtk/server.
	MaxSpicePasswordLen = 60

	// MaxOAEPPasswordLen is the RSA-1024 OAEP-SHA1 plaintext budget excluding the
	// trailing NUL (k - 2*hLen - 2 - 1 = 128 - 40 - 2 - 1 = 85). Documented for
	// crypto budget awareness; production paths use MaxSpicePasswordLen.
	// Golden vectors may include an 85-byte password for decrypt-only checks.
	MaxOAEPPasswordLen = 85

	// SpiceTicketCiphertextLen is the RSA-1024 ciphertext size (128 bytes).
	SpiceTicketCiphertextLen = 128
)

// ParseLinkPublicKey parses a SpiceLinkReply pub_key field (PKIX / SPKI DER RSA).
// Phase 1 rejects any length other than SpiceLinkPubKeyDERLen (162).
func ParseLinkPublicKey(der []byte) (*rsa.PublicKey, error) {
	if len(der) != SpiceLinkPubKeyDERLen {
		return nil, fmt.Errorf("spice: pub_key length %d want %d", len(der), SpiceLinkPubKeyDERLen)
	}
	pubAny, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("spice: parse link public key: %w", err)
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("spice: link public key is %T, want *rsa.PublicKey", pubAny)
	}
	return pub, nil
}

// EncryptSpiceTicket encrypts a SPICE ticket password for AuthSpice.
//
// Plaintext is password bytes plus a trailing NUL (matches spice-gtk strlen+1).
// Ciphertext is RSAES-OAEP with SHA-1 hash and MGF1-SHA1, empty label, 128 bytes.
//
// Rejects passwords longer than MaxSpicePasswordLen (60) to match spice-gtk
// and SPICE_MAX_PASSWORD_LENGTH. The OAEP crypto budget is MaxOAEPPasswordLen (85);
// see package constants.
func EncryptSpiceTicket(pub *rsa.PublicKey, password []byte) ([]byte, error) {
	return EncryptSpiceTicketWithRandom(rand.Reader, pub, password)
}

// EncryptSpiceTicketWithRandom is EncryptSpiceTicket with an explicit random source.
// Tests pass a fixed OAEP seed reader to match golden ciphertext vectors.
func EncryptSpiceTicketWithRandom(random io.Reader, pub *rsa.PublicKey, password []byte) ([]byte, error) {
	if pub == nil {
		return nil, fmt.Errorf("spice: nil public key")
	}
	if len(password) > MaxSpicePasswordLen {
		return nil, fmt.Errorf("spice: password length %d exceeds max %d (SPICE_MAX_PASSWORD_LENGTH)",
			len(password), MaxSpicePasswordLen)
	}
	// Defense in depth: OAEP budget (should never trip if MaxSpicePasswordLen is lower).
	if len(password) > MaxOAEPPasswordLen {
		return nil, fmt.Errorf("spice: password length %d exceeds OAEP budget %d",
			len(password), MaxOAEPPasswordLen)
	}

	// plaintext = password || 0x00  (spice-gtk: strlen(password)+1)
	buf := make([]byte, len(password)+1)
	copy(buf, password)
	// trailing byte is already zero

	ct, err := rsa.EncryptOAEP(sha1.New(), random, pub, buf, nil /* label */)
	if err != nil {
		return nil, fmt.Errorf("spice: encrypt ticket: %w", err)
	}
	if len(ct) != SpiceTicketCiphertextLen {
		return nil, fmt.Errorf("spice: ciphertext length %d want %d", len(ct), SpiceTicketCiphertextLen)
	}
	return ct, nil
}
