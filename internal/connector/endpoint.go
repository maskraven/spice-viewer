package connector

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
)

// Endpoint describes how to reach a SPICE peer (direct or via HTTP CONNECT).
//
// Host is treated as opaque for CONNECT: never pass it to net.SplitHostPort.
type Endpoint struct {
	// Host is the CONNECT tunnel target hostname/token (opaque). Never SplitHostPort.
	Host string
	// Port is tls-port (or port for cleartext debug).
	Port int
	// Proxy is the real TCP peer for CONNECT mode; nil = direct dial Host:Port.
	Proxy *url.URL
	// TLS holds certificate verification parameters. nil = cleartext (lab only).
	TLS *TLSParams
	// AllowCleartext must be true when TLS is nil; otherwise DialSPICE fails before dial.
	AllowCleartext bool
}

// TLSParams configures TLS for a SPICE dial.
//
// RootCAs is required whenever TLS is non-nil (hard error before dial if missing).
// When HostSubject is non-empty, Proxmox subject-pin mode is used: chain verify
// against RootCAs plus structured DN match. ServerName is only used in direct
// DNS mode (HostSubject empty) and must not be set to a pvespiceproxy token.
type TLSParams struct {
	// RootCAs is required if TLSParams is non-nil.
	RootCAs *x509.CertPool
	// HostSubject, if non-empty, enables Proxmox subject pin mode (OpenSSL-style DN).
	HostSubject string
	// ServerName is used only for direct DNS hosts when HostSubject is empty.
	ServerName string
}

// Dialer establishes a SPICE transport connection for an Endpoint.
type Dialer interface {
	DialSPICE(ctx context.Context, ep Endpoint) (net.Conn, error)
}

// Validation errors returned before any network I/O.
var (
	ErrEmptyHost          = errors.New("connector: empty host")
	ErrInvalidPort        = errors.New("connector: invalid port")
	ErrMissingRootCAs     = errors.New("connector: TLS requires RootCAs")
	ErrMissingTLSIdentity = errors.New("connector: TLS requires HostSubject or ServerName")
	ErrCleartextDenied    = errors.New("connector: cleartext not allowed (set AllowCleartext or provide TLS)")
)

// validate checks Endpoint constraints before dialing.
func (ep Endpoint) validate() error {
	if ep.Host == "" {
		return ErrEmptyHost
	}
	if ep.Port <= 0 || ep.Port > 65535 {
		return ErrInvalidPort
	}
	if ep.TLS != nil {
		if ep.TLS.RootCAs == nil {
			return ErrMissingRootCAs
		}
		if ep.TLS.HostSubject == "" && ep.TLS.ServerName == "" {
			return ErrMissingTLSIdentity
		}
	} else if !ep.AllowCleartext {
		return ErrCleartextDenied
	}
	return nil
}

// String returns a redacted summary suitable for logs (no secrets).
func (ep Endpoint) String() string {
	mode := "direct"
	if ep.Proxy != nil {
		mode = "connect"
	}
	tlsMode := "cleartext"
	if ep.TLS != nil {
		if ep.TLS.HostSubject != "" {
			tlsMode = "tls-pin"
		} else {
			tlsMode = "tls-dns"
		}
	}
	return fmt.Sprintf("Endpoint{mode=%s tls=%s host=%q port=%d}", mode, tlsMode, ep.Host, ep.Port)
}
