package connector

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultDialer implements Dialer with stdlib TCP, HTTP CONNECT, and TLS.
type DefaultDialer struct {
	// DialContext dials a raw TCP address. If nil, net.Dialer is used.
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
	// Timeout is the default dial timeout when the context has no deadline.
	// Zero means 30s. Applies to TCP dial, CONNECT write/read, and TLS handshake.
	Timeout time.Duration
}

// NewDialer returns a DefaultDialer with standard defaults.
func NewDialer() *DefaultDialer {
	return &DefaultDialer{}
}

// DialSPICE establishes a connection per Endpoint:
//
//   - Validates endpoint (missing CA, cleartext policy, etc.) before any dial
//   - CONNECT mode: TCP dial only the proxy; after HTTP 200, optional TLS on tunnel
//   - Direct mode: TCP dial Host:Port; optional TLS
func (d *DefaultDialer) DialSPICE(ctx context.Context, ep Endpoint) (net.Conn, error) {
	if err := ep.validate(); err != nil {
		return nil, err
	}

	ctx, cancel := d.withTimeout(ctx)
	defer cancel()

	var raw net.Conn
	var err error
	if ep.Proxy != nil {
		raw, err = d.dialViaCONNECT(ctx, ep)
	} else {
		raw, err = d.dialTCP(ctx, directDialAddress(ep.Host, ep.Port))
	}
	if err != nil {
		return nil, err
	}

	if ep.TLS == nil {
		return raw, nil
	}

	tlsCfg, err := buildTLSConfig(ep.TLS)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	tc := tls.Client(raw, tlsCfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = tc.Close()
		return nil, fmt.Errorf("connector: TLS handshake: %w", err)
	}
	return tc, nil
}

func (d *DefaultDialer) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	to := d.Timeout
	if to <= 0 {
		to = 30 * time.Second
	}
	return context.WithTimeout(ctx, to)
}

func (d *DefaultDialer) dialTCP(ctx context.Context, address string) (net.Conn, error) {
	dial := d.DialContext
	if dial == nil {
		nd := &net.Dialer{}
		dial = nd.DialContext
	}
	c, err := dial(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connector: dial %s: %w", address, err)
	}
	return c, nil
}

// dialViaCONNECT TCP-dials only the proxy, then issues:
//
//	CONNECT <opaque-host>:<port> HTTP/1.1
//	Host: <opaque-host>:<port>
//
// On HTTP 200, the connection is the tunnel for subsequent TLS/SPICE.
// CONNECT write/read honor ctx (deadline + cancel) so dial timeout covers the
// full proxy handshake, not only the TCP connect.
func (d *DefaultDialer) dialViaCONNECT(ctx context.Context, ep Endpoint) (net.Conn, error) {
	proxyAddr := proxyDialAddress(ep.Proxy)
	conn, err := d.dialTCP(ctx, proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("connector: proxy dial: %w", err)
	}

	release, err := bindConnToContext(ctx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	authority := connectAuthority(ep.Host, ep.Port)
	// Manual write so request-target is exactly the opaque authority (no scheme tricks).
	if err := writeCONNECT(conn, authority); err != nil {
		release()
		_ = conn.Close()
		return nil, mapConnCtxErr(ctx, fmt.Errorf("connector: write CONNECT: %w", err))
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		release()
		_ = conn.Close()
		return nil, mapConnCtxErr(ctx, fmt.Errorf("connector: read CONNECT response: %w", err))
	}
	// CONNECT 200: the rest of the connection is the tunnel. Do NOT drain
	// resp.Body (that would consume tunnel bytes / block until peer close).
	// On failure, discard a limited error body then close.
	if resp.StatusCode != http.StatusOK {
		if resp.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
		}
		release()
		_ = conn.Close()
		return nil, fmt.Errorf("connector: CONNECT refused: %s", resp.Status)
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}

	// Clear deadlines / cancel watch before returning the long-lived tunnel.
	release()

	// Buffered bytes (if any) must not be lost; wrap if needed.
	if br.Buffered() > 0 {
		return &bufConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

// bindConnToContext applies ctx deadline to conn and interrupts blocking I/O
// when ctx is cancelled. The returned release must be called exactly once:
// it stops the cancel watcher and clears the deadline (so the tunnel is not
// bound to the dial context after CONNECT completes).
//
// Cancel and release coordinate under mu so a late cancel cannot re-arm a past
// deadline after release has cleared it (Issue 6).
func bindConnToContext(ctx context.Context, conn net.Conn) (release func(), err error) {
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("connector: set deadline: %w", err)
		}
	}

	var mu sync.Mutex
	released := false
	done := make(chan struct{})
	var once sync.Once

	go func() {
		select {
		case <-ctx.Done():
			mu.Lock()
			if !released {
				// Unblock in-flight Read/Write only while CONNECT is still active.
				_ = conn.SetDeadline(time.Unix(1, 0))
			}
			mu.Unlock()
		case <-done:
		}
	}()

	release = func() {
		once.Do(func() {
			mu.Lock()
			released = true
			close(done)
			// Clear dial deadline under the same lock so the cancel watcher
			// cannot SetDeadline(past) after we hand the tunnel back.
			_ = conn.SetDeadline(time.Time{})
			mu.Unlock()
		})
	}
	return release, nil
}

func mapConnCtxErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%w", ctx.Err())
	}
	return err
}

// writeCONNECT writes a minimal HTTP CONNECT request.
// The request line is exactly: CONNECT <authority> HTTP/1.1
func writeCONNECT(w io.Writer, authority string) error {
	// authority may contain many ':' — write literally.
	// Control characters are rejected in Endpoint.validate before dial.
	var b strings.Builder
	b.Grow(len(authority)*2 + 64)
	b.WriteString("CONNECT ")
	b.WriteString(authority)
	b.WriteString(" HTTP/1.1\r\n")
	b.WriteString("Host: ")
	b.WriteString(authority)
	b.WriteString("\r\n")
	b.WriteString("User-Agent: virt-viewer\r\n")
	b.WriteString("\r\n")
	_, err := io.WriteString(w, b.String())
	if err != nil {
		return err
	}
	return nil
}

// bufConn re-attaches a bufio.Reader that may hold post-response bytes.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}
