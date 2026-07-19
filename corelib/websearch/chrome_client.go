package websearch

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
	"golang.org/x/net/proxy"

	"github.com/RapidAI/CodeClaw/corelib/proxyutil"
)

// chromeTLSClient builds an *http.Client whose TLS ClientHello mimics Chrome
// (uTLS HelloChrome_Auto), defeating JA3/JA4-based bot detection that the
// default Go crypto/tls fingerprint fails. The HTTP layer (h2 vs http/1.1)
// follows whatever the server negotiates, like a real browser.
//
// The client has no Timeout: the caller's context deadline governs. Each
// RoundTrip dials a fresh connection (no pooling) — fine for downloads and
// retries, which are rare and sequential.
func chromeTLSClient(jar http.CookieJar, checkRedirect func(req *http.Request, via []*http.Request) error) *http.Client {
	return &http.Client{
		Transport:     &chromeRoundTripper{},
		Jar:           jar,
		CheckRedirect: checkRedirect,
	}
}

// chromeRoundTripper dials with a Chrome-like ClientHello and routes the
// request to an HTTP/2 or HTTP/1.1 transport based on ALPN.
type chromeRoundTripper struct{}

// connClosingReadCloser closes the underlying connection when the body is
// closed. The per-request transports are single-use, so without this an
// HTTP/2 connection would linger inside the discarded transport until TCP
// timeout.
type connClosingReadCloser struct {
	io.ReadCloser
	conn net.Conn
	once sync.Once
}

func (b *connClosingReadCloser) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() { _ = b.conn.Close() })
	return err
}

func (rt *chromeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		// Plain HTTP has no TLS fingerprint; use a proxy-aware plain transport.
		return plainHTTPTransport().RoundTrip(req)
	}
	conn, h2, err := dialChromeTLS(req.Context(), req.URL)
	if err != nil {
		return nil, err
	}
	var t http.RoundTripper
	if h2 {
		t = &http2.Transport{
			DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
				return conn, nil
			},
			DisableCompression: true,
		}
	} else {
		t = &http.Transport{
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return conn, nil
			},
			DisableCompression: true,
			DisableKeepAlives:  true,
		}
	}
	resp, err := t.RoundTrip(req)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	resp.Body = &connClosingReadCloser{ReadCloser: resp.Body, conn: conn}
	return resp, nil
}

var (
	plainHTTPTMu  sync.Mutex
	plainHTTPT    *http.Transport
	plainHTTPTCfg proxyutil.Config
)

func plainHTTPTransport() *http.Transport {
	cfg := currentProxyConfig()
	plainHTTPTMu.Lock()
	defer plainHTTPTMu.Unlock()
	if plainHTTPT != nil && plainHTTPTCfg == cfg {
		return plainHTTPT
	}
	t := &http.Transport{DisableCompression: true}
	proxyutil.WrapTransport(t, cfg)
	plainHTTPT = t
	plainHTTPTCfg = cfg
	return plainHTTPT
}

// chromeTLSConfigHook, when non-nil, adjusts the uTLS config right before
// the handshake. Only used by tests (self-signed certs); nil in production.
var chromeTLSConfigHook func(cfg *utls.Config)

// dialChromeTLS opens a TCP connection (through the configured proxy when
// enabled) and completes a uTLS handshake with a Chrome ClientHello. It
// returns the established connection and whether HTTP/2 was negotiated.
func dialChromeTLS(ctx context.Context, target *url.URL) (net.Conn, bool, error) {
	host := target.Hostname()
	port := target.Port()
	if port == "" {
		port = "443"
	}
	addr := net.JoinHostPort(host, port)

	raw, err := dialTCPViaProxyIfNeeded(ctx, addr, host)
	if err != nil {
		return nil, false, err
	}
	// Bound the handshake phase (a stalled peer must not hang until the outer
	// ctx deadline, which can be 600s); cleared once established so the
	// transports manage the connection themselves.
	_ = raw.SetDeadline(time.Now().Add(15 * time.Second))
	cfg := &utls.Config{
		ServerName: host,
		NextProtos: []string{"h2", "http/1.1"},
		MinVersion: utls.VersionTLS12,
	}
	if chromeTLSConfigHook != nil {
		chromeTLSConfigHook(cfg) // test hook (e.g. self-signed certs)
	}
	uconn := utls.UClient(raw, cfg, utls.HelloChrome_Auto)
	if err := uconn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, false, fmt.Errorf("utls handshake: %w", err)
	}
	_ = uconn.SetDeadline(time.Time{})
	return uconn, uconn.ConnectionState().NegotiatedProtocol == "h2", nil
}

// proxyDialFunc adapts a function to proxy.Dialer.
type proxyDialFunc func(network, addr string) (net.Conn, error)

func (f proxyDialFunc) Dial(network, addr string) (net.Conn, error) { return f(network, addr) }

// dialTCPViaProxyIfNeeded dials addr directly, or tunnels through the
// configured proxy (HTTP CONNECT / HTTPS / SOCKS5) when one is enabled.
// Every proxy path bounds its tunnel-setup phase with a deadline: a proxy
// that accepts the connection but never answers would otherwise hang the
// download forever (the outer ctx cannot interrupt raw socket reads).
func dialTCPViaProxyIfNeeded(ctx context.Context, addr, targetHost string) (net.Conn, error) {
	pcfg := currentProxyConfig()
	d := &net.Dialer{Timeout: 10 * time.Second}
	if !pcfg.Enabled || pcfg.ProxyURL() == "" || pcfg.ShouldBypass(targetHost) {
		return d.DialContext(ctx, "tcp", addr)
	}
	scheme := pcfg.Protocol
	if scheme == "" {
		scheme = "http"
	}
	switch scheme {
	case "http", "https":
		return dialHTTPCONNECT(ctx, d, pcfg, addr)
	case "socks5":
		var auth *proxy.Auth
		if pcfg.Username != "" {
			auth = &proxy.Auth{User: pcfg.Username, Password: pcfg.Password}
		}
		// Pre-dial the proxy ourselves so the SOCKS5 handshake (which ignores
		// ctx inside x/net/proxy) is bounded by the connection deadline.
		raw, err := d.DialContext(ctx, "tcp", net.JoinHostPort(pcfg.Host, pcfg.Port))
		if err != nil {
			return nil, fmt.Errorf("socks5 proxy dial: %w", err)
		}
		_ = raw.SetDeadline(time.Now().Add(15 * time.Second))
		used := false
		dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort(pcfg.Host, pcfg.Port), auth,
			proxyDialFunc(func(network, _ string) (net.Conn, error) {
				if used {
					return nil, fmt.Errorf("socks5 forward connection reused")
				}
				used = true
				return raw, nil
			}))
		if err != nil {
			_ = raw.Close()
			return nil, fmt.Errorf("socks5 proxy: %w", err)
		}
		tun, err := dialer.Dial("tcp", addr)
		if err != nil {
			_ = raw.Close()
			return nil, fmt.Errorf("socks5 handshake: %w", err)
		}
		_ = tun.SetDeadline(time.Time{}) // caller sets its own TLS-handshake deadline
		return tun, nil
	default:
		return nil, fmt.Errorf("unsupported proxy protocol %q for chrome-fingerprint download", scheme)
	}
}

// dialHTTPCONNECT establishes a tunnel through an HTTP(S) proxy.
func dialHTTPCONNECT(ctx context.Context, d *net.Dialer, pcfg proxyutil.Config, addr string) (net.Conn, error) {
	scheme := pcfg.Protocol
	if scheme == "" {
		scheme = "http"
	}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(pcfg.Host, pcfg.Port))
	if err != nil {
		return nil, fmt.Errorf("proxy dial: %w", err)
	}
	// Bound the whole tunnel-setup phase (proxy TLS handshake + CONNECT
	// exchange); cleared on success.
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: pcfg.Host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("proxy tls: %w", err)
		}
		conn = tlsConn
	}
	req := &http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: http.Header{},
	}
	if pcfg.Username != "" {
		cred := base64.StdEncoding.EncodeToString([]byte(pcfg.Username + ":" + pcfg.Password))
		req.Header.Set("Proxy-Authorization", "Basic "+cred)
	}
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy CONNECT write: %w", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy CONNECT response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy CONNECT failed: %s", resp.Status)
	}
	// Any bytes the proxy pipelined past the CONNECT response headers would
	// be lost inside br's buffer; refuse rather than corrupt the TLS stream.
	if br.Buffered() > 0 {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy sent unexpected bytes after CONNECT response")
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}
