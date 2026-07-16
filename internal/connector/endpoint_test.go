package connector

import (
	"crypto/x509"
	"errors"
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
	pool := x509.NewCertPool()
	ep := Endpoint{
		Host: "example.com",
		Port: 443,
		TLS:  &TLSParams{RootCAs: pool},
	}
	if err := ep.validate(); !errors.Is(err, ErrMissingTLSIdentity) {
		t.Fatalf("got %v", err)
	}
	ep.TLS.ServerName = "example.com"
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
