package connector

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// pveHost is the design-doc multi-colon fixture host (without trailing port).
const pveHost = "pvespiceproxy:687d1ec6:10016:pve::dcc9e35662ef0b1233e12ac02880ea7851f9218e"

func TestDialSPICE_MissingCABeforeDial(t *testing.T) {
	d := NewDialer()
	// Custom dial that fails if called.
	d.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		t.Fatal("dial must not be called when CA missing")
		return nil, errors.New("unreachable")
	}
	_, err := d.DialSPICE(context.Background(), Endpoint{
		Host: pveHost,
		Port: 61002,
		Proxy: &url.URL{
			Scheme: "http",
			Host:   "127.0.0.1:1",
		},
		TLS: &TLSParams{HostSubject: "CN=x"},
	})
	if !errors.Is(err, ErrMissingRootCAs) {
		t.Fatalf("got %v", err)
	}
}

func TestDialSPICE_EmptyCertPoolBeforeDial(t *testing.T) {
	d := NewDialer()
	d.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		t.Fatal("dial must not be called when CA pool empty")
		return nil, errors.New("unreachable")
	}
	_, err := d.DialSPICE(context.Background(), Endpoint{
		Host: pveHost,
		Port: 61002,
		Proxy: &url.URL{
			Scheme: "http",
			Host:   "127.0.0.1:1",
		},
		TLS: &TLSParams{
			RootCAs:     x509.NewCertPool(),
			HostSubject: "CN=x",
		},
	})
	if !errors.Is(err, ErrMissingRootCAs) {
		t.Fatalf("got %v", err)
	}
}

func TestDialSPICE_HTTPSProxyRejectedBeforeDial(t *testing.T) {
	d := NewDialer()
	d.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		t.Fatal("dial must not be called for https proxy")
		return nil, errors.New("unreachable")
	}
	u, err := url.Parse("https://proxy.example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.DialSPICE(context.Background(), Endpoint{
		Host:           pveHost,
		Port:           61002,
		Proxy:          u,
		AllowCleartext: true,
	})
	if !errors.Is(err, ErrUnsupportedProxy) {
		t.Fatalf("got %v", err)
	}
}

func TestDialSPICE_CONNECTHonorsContextTimeout(t *testing.T) {
	// Accept TCP but never complete CONNECT response — dial must return on timeout.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		// Read request then hang (no response).
		br := bufio.NewReader(c)
		_, _ = br.ReadString('\n')
		for {
			h, err := br.ReadString('\n')
			if err != nil || h == "\r\n" || h == "\n" {
				break
			}
		}
		time.Sleep(5 * time.Second)
	}()

	proxyURL, err := url.Parse("http://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	d := NewDialer()
	d.Timeout = 150 * time.Millisecond
	start := time.Now()
	_, err = d.DialSPICE(context.Background(), Endpoint{
		Host:           pveHost,
		Port:           61002,
		Proxy:          proxyURL,
		AllowCleartext: true,
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("CONNECT hang ignored timeout: elapsed %v err %v", elapsed, err)
	}
	// Should surface as context deadline (or wrapped timeout from deadline).
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, os.ErrDeadlineExceeded) {
		// net.Error with Timeout() is also acceptable.
		var ne net.Error
		if !errors.As(err, &ne) || !ne.Timeout() {
			t.Fatalf("want deadline/timeout error, got %v", err)
		}
	}
}

func TestDialSPICE_HostControlCharsBeforeDial(t *testing.T) {
	d := NewDialer()
	d.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		t.Fatal("dial must not be called for injected host")
		return nil, errors.New("unreachable")
	}
	_, err := d.DialSPICE(context.Background(), Endpoint{
		Host:           "pve\r\nX: y",
		Port:           61002,
		AllowCleartext: true,
	})
	if !errors.Is(err, ErrInvalidHost) {
		t.Fatalf("got %v", err)
	}
}

// deadlineRecorder records SetDeadline calls for race assertions (Issue 6).
type deadlineRecorder struct {
	net.Conn
	mu   sync.Mutex
	log  []time.Time // zero means clear
	nset int
}

func (d *deadlineRecorder) SetDeadline(t time.Time) error {
	d.mu.Lock()
	d.log = append(d.log, t)
	d.nset++
	d.mu.Unlock()
	return d.Conn.SetDeadline(t)
}

func (d *deadlineRecorder) lastDeadline() (time.Time, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.log) == 0 {
		return time.Time{}, 0
	}
	return d.log[len(d.log)-1], len(d.log)
}

// TestBindConnToContext_ReleaseWinsOverCancel ensures a concurrent cancel cannot
// leave a past deadline on the conn after release (Issue 6).
func TestBindConnToContext_ReleaseWinsOverCancel(t *testing.T) {
	for i := 0; i < 200; i++ {
		c1, c2 := net.Pipe()
		rec := &deadlineRecorder{Conn: c1}
		ctx, cancel := context.WithCancel(context.Background())

		release, err := bindConnToContext(ctx, rec)
		if err != nil {
			c1.Close()
			c2.Close()
			t.Fatal(err)
		}

		// Fire cancel and release concurrently; release must win for the tunnel.
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			cancel()
		}()
		go func() {
			defer wg.Done()
			release()
		}()
		wg.Wait()
		// Allow cancel watcher to finish if it lost the select race.
		time.Sleep(2 * time.Millisecond)

		last, n := rec.lastDeadline()
		if !last.IsZero() && last.Before(time.Now()) {
			c1.Close()
			c2.Close()
			t.Fatalf("iter %d: past deadline left after release (last=%v n=%d log=%v)", i, last, n, rec.log)
		}

		// Concurrent peer read so pipe Write can complete (pipe is synchronous).
		errCh := make(chan error, 1)
		go func() {
			buf := make([]byte, 2)
			_ = c2.SetReadDeadline(time.Now().Add(time.Second))
			_, err := io.ReadFull(c2, buf)
			errCh <- err
		}()
		_ = c1.SetWriteDeadline(time.Now().Add(time.Second))
		if _, err := c1.Write([]byte("ok")); err != nil {
			c1.Close()
			c2.Close()
			t.Fatalf("iter %d: tunnel conn unusable after release+cancel: %v", i, err)
		}
		if err := <-errCh; err != nil {
			c1.Close()
			c2.Close()
			t.Fatalf("iter %d: peer read: %v", i, err)
		}
		c1.Close()
		c2.Close()
	}
}

// TestBindConnToContext_CancelBeforeReleaseStillInterrupts verifies cancel still
// unblocks CONNECT I/O when release has not run yet.
func TestBindConnToContext_CancelBeforeReleaseStillInterrupts(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ctx, cancel := context.WithCancel(context.Background())
	release, err := bindConnToContext(ctx, c1)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, err := c1.Read(buf)
		errCh <- err
	}()

	// Ensure Read is blocked, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected read error after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read did not unblock after cancel")
	}
}

func TestWriteCONNECT_ExactRequestLine(t *testing.T) {
	authority := connectAuthority(pveHost, 61002)
	var buf strings.Builder
	if err := writeCONNECT(&buf, authority); err != nil {
		t.Fatal(err)
	}
	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(buf.String())))
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodConnect {
		t.Fatalf("method %s", req.Method)
	}
	// RequestURI is the opaque authority for CONNECT.
	wantURI := "pvespiceproxy:687d1ec6:10016:pve::dcc9e35662ef0b1233e12ac02880ea7851f9218e:61002"
	if req.RequestURI != wantURI {
		t.Fatalf("RequestURI=%q want %q", req.RequestURI, wantURI)
	}
	if req.Host != wantURI {
		t.Fatalf("Host=%q want %q", req.Host, wantURI)
	}
	// First line exact.
	first := strings.Split(buf.String(), "\r\n")[0]
	wantLine := "CONNECT " + wantURI + " HTTP/1.1"
	if first != wantLine {
		t.Fatalf("first line=%q want %q", first, wantLine)
	}
}

func TestDialSPICE_MockCONNECT_ExactAuthority(t *testing.T) {
	var gotRequestLine string
	var mu sync.Mutex

	// CONNECT mock that records the first request line and returns 200.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		mu.Lock()
		gotRequestLine = strings.TrimRight(line, "\r\n")
		mu.Unlock()
		// Drain headers
		for {
			h, err := br.ReadString('\n')
			if err != nil || h == "\r\n" || h == "\n" {
				break
			}
		}
		_, _ = io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
		// Keep open briefly so client can finish.
		time.Sleep(50 * time.Millisecond)
	}()

	proxyURL, err := url.Parse("http://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	d := NewDialer()
	conn, err := d.DialSPICE(context.Background(), Endpoint{
		Host:           pveHost,
		Port:           61002,
		Proxy:          proxyURL,
		AllowCleartext: true, // tunnel only, no TLS for this unit test
	})
	if err != nil {
		t.Fatalf("DialSPICE: %v", err)
	}
	_ = conn.Close()
	<-done

	mu.Lock()
	got := gotRequestLine
	mu.Unlock()
	want := "CONNECT pvespiceproxy:687d1ec6:10016:pve::dcc9e35662ef0b1233e12ac02880ea7851f9218e:61002 HTTP/1.1"
	if got != want {
		t.Fatalf("CONNECT line=\n  %q\nwant %q", got, want)
	}
}

func TestDialSPICE_MockCONNECT_TLSPin(t *testing.T) {
	dir := certsDir(t)
	roots := loadRootPool(t, filepath.Join(dir, "ca.pem"))
	hsBytes, err := os.ReadFile(filepath.Join(dir, "host-subject.txt"))
	if err != nil {
		t.Fatal(err)
	}
	hostSubject := strings.TrimSpace(string(hsBytes))

	certPEM, err := os.ReadFile(filepath.Join(dir, "server-chain.pem"))
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, "server-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	// Single listener: first speak HTTP CONNECT, then TLS on the same conn.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var gotLine string
	var mu sync.Mutex
	errCh := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		line, err := br.ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		mu.Lock()
		gotLine = strings.TrimRight(line, "\r\n")
		mu.Unlock()
		for {
			h, err := br.ReadString('\n')
			if err != nil || h == "\r\n" || h == "\n" {
				break
			}
		}
		if _, err := io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			errCh <- err
			return
		}
		// Remaining stream is TLS. Re-attach buffered reader if needed.
		var base net.Conn = c
		if br.Buffered() > 0 {
			base = &bufConn{Conn: c, r: br}
		}
		tc := tls.Server(base, &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			MinVersion:   tls.VersionTLS12,
		})
		if err := tc.Handshake(); err != nil {
			errCh <- err
			return
		}
		_, _ = io.WriteString(tc, "SPICE")
		_ = tc.Close()
		errCh <- nil
	}()

	proxyURL, err := url.Parse("http://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	d := NewDialer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := d.DialSPICE(ctx, Endpoint{
		Host:  pveHost,
		Port:  61002,
		Proxy: proxyURL,
		TLS: &TLSParams{
			RootCAs:     roots,
			HostSubject: hostSubject,
		},
	})
	if err != nil {
		t.Fatalf("DialSPICE TLS: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "SPICE" {
		t.Fatalf("payload %q", buf)
	}
	_ = conn.Close()

	if err := <-errCh; err != nil {
		t.Fatalf("server: %v", err)
	}

	mu.Lock()
	line := gotLine
	mu.Unlock()
	want := "CONNECT pvespiceproxy:687d1ec6:10016:pve::dcc9e35662ef0b1233e12ac02880ea7851f9218e:61002 HTTP/1.1"
	if line != want {
		t.Fatalf("CONNECT line=%q", line)
	}
}

func TestDialSPICE_TLSPin_WrongSubject(t *testing.T) {
	dir := certsDir(t)
	roots := loadRootPool(t, filepath.Join(dir, "ca.pem"))

	certPEM, err := os.ReadFile(filepath.Join(dir, "server-wrong-subject-chain.pem"))
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, "server-wrong-subject-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.(*tls.Conn).Handshake()
		time.Sleep(20 * time.Millisecond)
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	d := NewDialer()
	_, err = d.DialSPICE(context.Background(), Endpoint{
		Host: host,
		Port: port,
		TLS: &TLSParams{
			RootCAs:     roots,
			HostSubject: "OU=PVE Cluster Node,O=Proxmox Virtual Environment,CN=pve.example.com",
		},
	})
	if err == nil {
		t.Fatal("expected TLS subject failure")
	}
}

func TestDialSPICE_DirectCleartext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.WriteString(c, "ok")
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	d := NewDialer()
	conn, err := d.DialSPICE(context.Background(), Endpoint{
		Host:           host,
		Port:           port,
		AllowCleartext: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ok" {
		t.Fatalf("got %q", buf)
	}
}
