package connector

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Endpoint describes how to reach a SPICE peer (direct or via HTTP CONNECT).
//
// Host is treated as opaque for CONNECT: never pass it to net.SplitHostPort.
type Endpoint struct {
	// Host is the CONNECT tunnel target hostname/token (opaque). Never SplitHostPort.
	// Must not contain spaces or ASCII control characters (header-injection defense).
	Host string
	// Port is tls-port (or port for cleartext debug).
	Port int
	// Proxy is the real TCP peer for CONNECT mode; nil = direct dial Host:Port.
	// Phase 1 supports cleartext HTTP proxies only (scheme "http" or empty).
	// https:// proxies are rejected until TLS-to-proxy is implemented.
	Proxy *url.URL
	// TLS holds certificate verification parameters. nil = cleartext (lab only).
	TLS *TLSParams
	// AllowCleartext must be true when TLS is nil; otherwise DialSPICE fails before dial.
	AllowCleartext bool
}

// TLSParams configures TLS for a SPICE dial.
//
// Pin mode (HostSubject non-empty): RootCAs is required and must contain at least
// one certificate (the .vv embedded CA). Chain verify uses only that pool.
//
// Direct DNS mode (HostSubject empty): ServerName is required. RootCAs may be nil
// to use the system trust store, or a custom pool.
// ServerName must not be set to a pvespiceproxy token.
type TLSParams struct {
	// RootCAs: required and non-empty in pin mode (HostSubject set).
	// Optional in DNS mode (nil = system roots).
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
	ErrInvalidHost        = errors.New("connector: invalid host")
	ErrInvalidPort        = errors.New("connector: invalid port")
	ErrMissingRootCAs     = errors.New("connector: TLS pin mode requires non-empty RootCAs")
	ErrMissingTLSIdentity = errors.New("connector: TLS requires HostSubject or ServerName")
	ErrCleartextDenied    = errors.New("connector: cleartext not allowed (set AllowCleartext or provide TLS)")
	ErrUnsupportedProxy   = errors.New("connector: proxy scheme must be http (https proxies not supported in phase 1)")
	ErrInvalidProxy       = errors.New("connector: invalid proxy URL")
)

// validate checks Endpoint constraints before dialing.
func (ep Endpoint) validate() error {
	if ep.Host == "" {
		return ErrEmptyHost
	}
	if err := validateHostToken(ep.Host); err != nil {
		return err
	}
	if ep.Port <= 0 || ep.Port > 65535 {
		return ErrInvalidPort
	}
	if ep.Proxy != nil {
		if err := validateProxy(ep.Proxy); err != nil {
			return err
		}
	}
	if ep.TLS != nil {
		if ep.TLS.HostSubject == "" && ep.TLS.ServerName == "" {
			return ErrMissingTLSIdentity
		}
		// Pin mode requires a non-empty CA pool before dial (design matrix).
		if ep.TLS.HostSubject != "" && certPoolMissing(ep.TLS.RootCAs) {
			return ErrMissingRootCAs
		}
	} else if !ep.AllowCleartext {
		return ErrCleartextDenied
	}
	return nil
}

// validateHostToken rejects spaces and ASCII control bytes that could break or
// inject HTTP CONNECT request lines / Host headers.
func validateHostToken(host string) error {
	for i := 0; i < len(host); i++ {
		c := host[i]
		if c <= 0x1f || c == 0x7f || c == ' ' {
			return fmt.Errorf("%w: contains space or control character", ErrInvalidHost)
		}
	}
	return nil
}

// validateProxy enforces Phase-1 HTTP CONNECT proxy rules.
func validateProxy(u *url.URL) error {
	if u.Host == "" && u.Opaque == "" {
		// url.Parse("http://") yields empty Host.
		if u.Hostname() == "" {
			return fmt.Errorf("%w: missing host", ErrInvalidProxy)
		}
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" {
		return fmt.Errorf("%w: got %q", ErrUnsupportedProxy, u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%w: missing host", ErrInvalidProxy)
	}
	return nil
}

// certPoolMissing reports whether a pool is absent or empty.
//
// Empty non-nil pools must fail before dial in pin mode. CertPool has no
// portable public length API; Subjects() is deprecated but returns DER subjects
// for caller-built pools (e.g. AppendCertsFromPEM from .vv ca). System pools
// are only used via nil RootCAs in DNS mode, not via this check.
func certPoolMissing(p *x509.CertPool) bool {
	if p == nil {
		return true
	}
	return len(p.Subjects()) == 0
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
