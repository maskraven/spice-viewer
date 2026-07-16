package connector

import (
	"net"
	"net/url"
	"strconv"
)

// connectAuthority builds the CONNECT request-target and Host header value.
//
// host may contain ':', including "::" substrings — treat as opaque.
// Never call net.SplitHostPort or net.JoinHostPort on Proxmox host tokens.
func connectAuthority(host string, port int) string {
	return host + ":" + strconv.Itoa(port)
}

// directDialAddress builds the TCP address for direct (non-proxy) dials.
//
// IPv6 literals use net.JoinHostPort so brackets are correct. All other hosts
// (DNS names or opaque tokens) use literal host:port concatenation so multi-colon
// tokens are never mangled by SplitHostPort/JoinHostPort.
func directDialAddress(host string, port int) string {
	if ip := net.ParseIP(host); ip != nil {
		return net.JoinHostPort(host, strconv.Itoa(port))
	}
	return host + ":" + strconv.Itoa(port)
}

// proxyDialAddress returns the TCP address for dialing an HTTP proxy.
// Defaults ports when the URL omits them (80 for http, 443 for https).
func proxyDialAddress(u *url.URL) string {
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	// Hostname() strips brackets from IPv6; JoinHostPort re-adds them.
	return net.JoinHostPort(host, port)
}
