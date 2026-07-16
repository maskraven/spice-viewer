package connector

import (
	"net/url"
	"testing"
)

func TestConnectAuthority_MultiColonPVE(t *testing.T) {
	// Real-shaped sanitized Proxmox host token (design doc fixture).
	host := "pvespiceproxy:687d1ec6:10016:pve::dcc9e35662ef0b1233e12ac02880ea7851f9218e"
	port := 61002
	got := connectAuthority(host, port)
	want := "pvespiceproxy:687d1ec6:10016:pve::dcc9e35662ef0b1233e12ac02880ea7851f9218e:61002"
	if got != want {
		t.Fatalf("connectAuthority =\n  %q\nwant %q", got, want)
	}
}

func TestConnectAuthority_CONNECTLineExact(t *testing.T) {
	host := "pvespiceproxy:687d1ec6:10016:pve::dcc9e35662ef0b1233e12ac02880ea7851f9218e"
	authority := connectAuthority(host, 61002)
	line := "CONNECT " + authority + " HTTP/1.1"
	want := "CONNECT pvespiceproxy:687d1ec6:10016:pve::dcc9e35662ef0b1233e12ac02880ea7851f9218e:61002 HTTP/1.1"
	if line != want {
		t.Fatalf("CONNECT line =\n  %q\nwant %q", line, want)
	}
}

func TestConnectAuthority_DoesNotUseSplitHostPortSemantics(t *testing.T) {
	// If someone used JoinHostPort on this host it would wrap or mis-parse.
	host := "pvespiceproxy:aa:bb::cc"
	got := connectAuthority(host, 1)
	if got != "pvespiceproxy:aa:bb::cc:1" {
		t.Fatalf("got %q", got)
	}
}

func TestDirectDialAddress_DNSAndIPv6(t *testing.T) {
	if got := directDialAddress("example.com", 443); got != "example.com:443" {
		t.Fatalf("dns: got %q", got)
	}
	if got := directDialAddress("127.0.0.1", 5900); got != "127.0.0.1:5900" {
		t.Fatalf("ipv4: got %q", got)
	}
	got := directDialAddress("2001:db8::1", 61000)
	if got != "[2001:db8::1]:61000" {
		t.Fatalf("ipv6: got %q", got)
	}
}

func TestProxyDialAddress_Defaults(t *testing.T) {
	u, err := url.Parse("http://proxy.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := proxyDialAddress(u); got != "proxy.example.com:80" {
		t.Fatalf("http default: got %q", got)
	}
	u, _ = url.Parse("https://proxy.example.com")
	if got := proxyDialAddress(u); got != "proxy.example.com:443" {
		t.Fatalf("https default: got %q", got)
	}
	u, _ = url.Parse("http://proxy.example.com:3128")
	if got := proxyDialAddress(u); got != "proxy.example.com:3128" {
		t.Fatalf("explicit: got %q", got)
	}
}
