package main

import (
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
)

func main() {
	privPEM, _ := os.ReadFile("testdata/vectors/ticket_rsa1024_private.pem")
	block, _ := pem.Decode(privPEM)
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		panic(err)
	}
	spki, _ := os.ReadFile("testdata/vectors/ticket_rsa1024_spki.der")
	if len(spki) != 162 {
		panic(fmt.Sprintf("spki len %d", len(spki)))
	}
	pub, err := x509.ParsePKIXPublicKey(spki)
	if err != nil {
		panic(err)
	}
	if pub.(*rsa.PublicKey).N.Cmp(key.N) != 0 {
		panic("pub mismatch")
	}
	raw, _ := os.ReadFile("testdata/vectors/ticket_vectors.json")
	var doc struct {
		SPKIDERHex string `json:"spki_der_hex"`
		Vectors    []struct {
			Name          string `json:"name"`
			PlaintextHex  string `json:"plaintext_hex"`
			CiphertextHex string `json:"ciphertext_hex"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		panic(err)
	}
	if doc.SPKIDERHex != hex.EncodeToString(spki) {
		panic("spki hex mismatch")
	}
	for _, v := range doc.Vectors {
		ct, _ := hex.DecodeString(v.CiphertextHex)
		pt, err := rsa.DecryptOAEP(sha1.New(), nil, key, ct, nil)
		if err != nil {
			panic(v.Name + ": " + err.Error())
		}
		expect, _ := hex.DecodeString(v.PlaintextHex)
		if string(pt) != string(expect) {
			panic(fmt.Sprintf("%s: got %x want %x", v.Name, pt, expect))
		}
		fmt.Println("OK", v.Name)
	}
}
