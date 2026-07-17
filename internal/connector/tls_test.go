package connector

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func loadPEMCerts(t *testing.T, path string) []*x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var certs []*x509.Certificate
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		certs = append(certs, c)
	}
	if len(certs) == 0 {
		t.Fatalf("no certs in %s", path)
	}
	return certs
}

func loadRootPool(t *testing.T, caPath string) *x509.CertPool {
	t.Helper()
	pemBytes, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		t.Fatal("failed to parse CA PEM")
	}
	return pool
}

func rawChain(certs []*x509.Certificate) [][]byte {
	out := make([][]byte, len(certs))
	for i, c := range certs {
		out[i] = c.Raw
	}
	return out
}

func TestVerifyPeerCertificate_SuccessChainAndDN(t *testing.T) {
	dir := certsDir(t)
	roots := loadRootPool(t, filepath.Join(dir, "ca.pem"))
	chain := loadPEMCerts(t, filepath.Join(dir, "server-chain.pem"))
	hs, err := os.ReadFile(filepath.Join(dir, "host-subject.txt"))
	if err != nil {
		t.Fatal(err)
	}
	hostSubject := string(hs[:len(hs)-1]) // trim trailing newline carefully
	// host-subject.txt ends with \n
	if len(hs) > 0 && hs[len(hs)-1] == '\n' {
		hostSubject = string(hs[:len(hs)-1])
	} else {
		hostSubject = string(hs)
	}

	if err := verifyPeerCertificate(rawChain(chain), roots, hostSubject); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyPeerCertificate_WrongSubject(t *testing.T) {
	dir := certsDir(t)
	roots := loadRootPool(t, filepath.Join(dir, "ca.pem"))
	chain := loadPEMCerts(t, filepath.Join(dir, "server-wrong-subject-chain.pem"))
	hostSubject := "OU=PVE Cluster Node,O=Proxmox Virtual Environment,CN=pve.example.com"
	err := verifyPeerCertificate(rawChain(chain), roots, hostSubject)
	if err == nil {
		t.Fatal("expected subject mismatch")
	}
	if !errors.Is(err, ErrTLSVerify) {
		t.Fatalf("want ErrTLSVerify, got %v", err)
	}
	if !errors.Is(err, ErrTLSSubjectMismatch) {
		t.Fatalf("want ErrTLSSubjectMismatch, got %v", err)
	}
}

func TestVerifyPeerCertificate_UntrustedCA(t *testing.T) {
	dir := certsDir(t)
	roots := loadRootPool(t, filepath.Join(dir, "ca.pem"))
	chain := loadPEMCerts(t, filepath.Join(dir, "untrusted-server.pem"))
	hostSubject := "OU=PVE Cluster Node,O=Proxmox Virtual Environment,CN=pve.example.com"
	err := verifyPeerCertificate(rawChain(chain), roots, hostSubject)
	if err == nil {
		t.Fatal("expected chain failure")
	}
	if !errors.Is(err, ErrTLSVerify) {
		t.Fatalf("want ErrTLSVerify, got %v", err)
	}
}

func TestVerifyPeerCertificate_EmptyChain(t *testing.T) {
	roots := x509.NewCertPool()
	err := verifyPeerCertificate(nil, roots, "CN=x")
	if err == nil || !errors.Is(err, ErrTLSVerify) {
		t.Fatalf("got %v", err)
	}
}

func TestBuildTLSConfig_PinModeNoServerName(t *testing.T) {
	dir := certsDir(t)
	roots := loadRootPool(t, filepath.Join(dir, "ca.pem"))
	cfg, err := buildTLSConfig(&TLSParams{
		RootCAs:     roots,
		HostSubject: "CN=pve.example.com,O=Proxmox Virtual Environment,OU=PVE Cluster Node",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.InsecureSkipVerify {
		t.Fatal("pin mode requires InsecureSkipVerify for hostname skip")
	}
	if cfg.ServerName != "" {
		t.Fatalf("ServerName must be empty in pin mode, got %q", cfg.ServerName)
	}
	if cfg.VerifyPeerCertificate == nil {
		t.Fatal("missing VerifyPeerCertificate")
	}
}

func TestBuildTLSConfig_DNSMode(t *testing.T) {
	dir := certsDir(t)
	roots := loadRootPool(t, filepath.Join(dir, "ca.pem"))
	cfg, err := buildTLSConfig(&TLSParams{
		RootCAs:    roots,
		ServerName: "pve.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InsecureSkipVerify {
		t.Fatal("DNS mode must not skip verify")
	}
	if cfg.ServerName != "pve.example.com" {
		t.Fatalf("ServerName=%q", cfg.ServerName)
	}
}

func TestBuildTLSConfig_DNSModeSystemRoots(t *testing.T) {
	cfg, err := buildTLSConfig(&TLSParams{
		ServerName: "example.com",
		// RootCAs nil → system trust store
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RootCAs != nil {
		t.Fatal("expected nil RootCAs for system roots")
	}
	if cfg.ServerName != "example.com" {
		t.Fatalf("ServerName=%q", cfg.ServerName)
	}
	if cfg.InsecureSkipVerify {
		t.Fatal("DNS mode must not skip verify")
	}
}

func TestBuildTLSConfig_MissingRootCAs(t *testing.T) {
	_, err := buildTLSConfig(&TLSParams{HostSubject: "CN=x"})
	if !errors.Is(err, ErrMissingRootCAs) {
		t.Fatalf("nil pool pin mode: got %v", err)
	}
	_, err = buildTLSConfig(&TLSParams{
		RootCAs:     x509.NewCertPool(),
		HostSubject: "CN=x",
	})
	if !errors.Is(err, ErrMissingRootCAs) {
		t.Fatalf("empty pool pin mode: got %v", err)
	}
}

func TestBuildTLSConfig_PinModeClearsCallerServerName(t *testing.T) {
	dir := certsDir(t)
	roots := loadRootPool(t, filepath.Join(dir, "ca.pem"))
	cfg, err := buildTLSConfig(&TLSParams{
		RootCAs:     roots,
		HostSubject: "CN=pve.example.com",
		ServerName:  "must-not-appear",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "" {
		t.Fatalf("pin mode must clear ServerName, got %q", cfg.ServerName)
	}
}
