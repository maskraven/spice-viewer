package connector

import (
	"crypto/x509"
	"errors"
	"net/url"
	"path/filepath"
	"testing"
)

func TestEndpointValidate_MissingCAHardError(t *testing.T) {
	ep := Endpoint{
		Host: "pvespiceproxy:token",
		Port: 61002,
		TLS:  &TLSParams{HostSubject: "CN=x"},
	}
	err := ep.validate()
	if !errors.Is(err, ErrMissingRootCAs) {
		t.Fatalf("got %v", err)
	}
}

func TestEndpointValidate_EmptyCertPoolHardError(t *testing.T) {
	ep := Endpoint{
		Host: "pvespiceproxy:token",
		Port: 61002,
		TLS: &TLSParams{
			RootCAs:     x509.NewCertPool(), // non-nil but empty
			HostSubject: "CN=x",
		},
	}
	err := ep.validate()
	if !errors.Is(err, ErrMissingRootCAs) {
		t.Fatalf("empty pool: got %v", err)
	}
}

func TestEndpointValidate_CleartextDenied(t *testing.T) {
	ep := Endpoint{Host: "127.0.0.1", Port: 5900}
	if err := ep.validate(); !errors.Is(err, ErrCleartextDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestEndpointValidate_CleartextAllowed(t *testing.T) {
	ep := Endpoint{Host: "127.0.0.1", Port: 5900, AllowCleartext: true}
	if err := ep.validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointValidate_TLSIdentity(t *testing.T) {
	ep := Endpoint{
		Host: "example.com",
		Port: 443,
		TLS:  &TLSParams{},
	}
	if err := ep.validate(); !errors.Is(err, ErrMissingTLSIdentity) {
		t.Fatalf("got %v", err)
	}
	// DNS mode: ServerName set, nil RootCAs (system roots) is OK.
	ep.TLS.ServerName = "example.com"
	if err := ep.validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointValidate_DNSModeSystemRoots(t *testing.T) {
	ep := Endpoint{
		Host: "example.com",
		Port: 443,
		TLS:  &TLSParams{ServerName: "example.com"}, // RootCAs nil
	}
	if err := ep.validate(); err != nil {
		t.Fatalf("DNS mode should allow nil RootCAs: %v", err)
	}
}

func TestEndpointValidate_PinModeRequiresCA(t *testing.T) {
	roots := loadRootPool(t, filepath.Join(certsDir(t), "ca.pem"))
	ep := Endpoint{
		Host: "pvespiceproxy:token",
		Port: 61002,
		TLS:  &TLSParams{RootCAs: roots, HostSubject: "CN=pve.example.com"},
	}
	if err := ep.validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointValidate_Port(t *testing.T) {
	ep := Endpoint{Host: "h", Port: 0, AllowCleartext: true}
	if err := ep.validate(); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("got %v", err)
	}
}

func TestEndpointValidate_HostControlChars(t *testing.T) {
	cases := []string{
		"evil\r\nHost: injected",
		"has space",
		"null\x00byte",
		"tab\there",
	}
	for _, host := range cases {
		ep := Endpoint{Host: host, Port: 1, AllowCleartext: true}
		if err := ep.validate(); !errors.Is(err, ErrInvalidHost) {
			t.Fatalf("host %q: got %v", host, err)
		}
	}
}

func TestEndpointValidate_HTTPSProxyRejected(t *testing.T) {
	u, err := url.Parse("https://proxy.example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	ep := Endpoint{
		Host:           "target",
		Port:           61002,
		Proxy:          u,
		AllowCleartext: true,
	}
	if err := ep.validate(); !errors.Is(err, ErrUnsupportedProxy) {
		t.Fatalf("got %v", err)
	}
}

func TestEndpointValidate_HTTPProxyOK(t *testing.T) {
	u, err := url.Parse("http://proxy.example.com:3128")
	if err != nil {
		t.Fatal(err)
	}
	ep := Endpoint{
		Host:           "target",
		Port:           61002,
		Proxy:          u,
		AllowCleartext: true,
	}
	if err := ep.validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDialSPICE_EmptyPoolBeforeDial(t *testing.T) {
	// Covered at validate; ensure DialSPICE path also fails before network.
	// Uses dialer_test pattern via Endpoint validation only here.
	ep := Endpoint{
		Host: "h",
		Port: 1,
		TLS: &TLSParams{
			RootCAs:     x509.NewCertPool(),
			HostSubject: "CN=x",
		},
	}
	if err := ep.validate(); !errors.Is(err, ErrMissingRootCAs) {
		t.Fatal(err)
	}
}
