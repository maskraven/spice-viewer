package connector

import (
	"crypto/x509/pkix"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseDN_PVEStyle(t *testing.T) {
	dn := parseDN("OU=PVE Cluster Node,O=Proxmox Virtual Environment,CN=pve.example.com")
	if dn.CN != "pve.example.com" {
		t.Fatalf("CN=%q", dn.CN)
	}
	if len(dn.O) != 1 || dn.O[0] != "Proxmox Virtual Environment" {
		t.Fatalf("O=%v", dn.O)
	}
	if len(dn.OU) != 1 || dn.OU[0] != "PVE Cluster Node" {
		t.Fatalf("OU=%v", dn.OU)
	}
}

func TestParseDN_EscapedComma(t *testing.T) {
	dn := parseDN(`CN=a\,b,O=Org`)
	if dn.CN != "a,b" {
		t.Fatalf("CN=%q want a,b", dn.CN)
	}
	if len(dn.O) != 1 || dn.O[0] != "Org" {
		t.Fatalf("O=%v", dn.O)
	}
}

func TestSubjectMatches_OrderIndependent(t *testing.T) {
	cert := pkix.Name{
		CommonName:         "pve.example.com",
		Organization:       []string{"Proxmox Virtual Environment"},
		OrganizationalUnit: []string{"PVE Cluster Node"},
	}
	// OpenSSL-style string with RDN order different from pkix.Name.String().
	hostSubject := "OU=PVE Cluster Node,O=Proxmox Virtual Environment,CN=pve.example.com"
	if !subjectMatches(cert, hostSubject) {
		t.Fatal("expected match with reordered RDNs")
	}
	// pkix.Name.String() must NOT be required to equal host-subject.
	if cert.String() == hostSubject {
		t.Logf("note: String() happened to equal host-subject: %s", cert.String())
	}
}

func TestSubjectMatches_GoldenFromFixture(t *testing.T) {
	hs, err := os.ReadFile(filepath.Join(certsDir(t), "host-subject.txt"))
	if err != nil {
		t.Fatal(err)
	}
	hostSubject := strings.TrimSpace(string(hs))
	cert := pkix.Name{
		CommonName:         "pve.example.com",
		Organization:       []string{"Proxmox Virtual Environment"},
		OrganizationalUnit: []string{"PVE Cluster Node"},
	}
	if !subjectMatches(cert, hostSubject) {
		t.Fatalf("golden host-subject %q did not match cert subject", hostSubject)
	}
}

func TestSubjectMatches_Mismatch(t *testing.T) {
	cert := pkix.Name{
		CommonName:         "pve.example.com",
		Organization:       []string{"Proxmox Virtual Environment"},
		OrganizationalUnit: []string{"PVE Cluster Node"},
	}
	cases := []string{
		"OU=PVE Cluster Node,O=Proxmox Virtual Environment,CN=other.example.com",
		"OU=Wrong,O=Proxmox Virtual Environment,CN=pve.example.com",
		"OU=PVE Cluster Node,O=Other,CN=pve.example.com",
	}
	for _, hs := range cases {
		if subjectMatches(cert, hs) {
			t.Fatalf("unexpected match for %q", hs)
		}
	}
}

func TestSubjectMatches_MultiValuedOU(t *testing.T) {
	cert := pkix.Name{
		CommonName:         "node",
		Organization:       []string{"Org"},
		OrganizationalUnit: []string{"A", "B"},
	}
	if !subjectMatches(cert, "CN=node,O=Org,OU=B,OU=A") {
		t.Fatal("order-independent multi OU should match")
	}
	if subjectMatches(cert, "CN=node,O=Org,OU=A") {
		t.Fatal("missing OU value should not match")
	}
}

func TestSubjectMatches_OptionalC(t *testing.T) {
	cert := pkix.Name{
		CommonName: "n",
		Country:    []string{"US"},
	}
	if !subjectMatches(cert, "CN=n") {
		t.Fatal("C not in expected: should ignore cert C")
	}
	if !subjectMatches(cert, "CN=n,C=US") {
		t.Fatal("C match")
	}
	if subjectMatches(cert, "CN=n,C=DE") {
		t.Fatal("C mismatch")
	}
}

func certsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/connector -> repo root
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	return filepath.Join(root, "testdata", "certs")
}
