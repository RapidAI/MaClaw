package desktopd

import (
	"bufio"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// forwardproxy.go implements a small authenticated outbound forward proxy
// inside desktopd (env: DESKTOPD_PROXY=1, DESKTOPD_PROXY_ADDR, see main.go)
// so other MaClaw hosts — typically in mainland China — can egress through
// this desktopd host: docker pull, image builds and ad-hoc tool HTTP. It
// speaks HTTP CONNECT (tunnels; what dockerd, docker build and curl use for
// HTTPS targets) and plain HTTP proxying for absolute-form http:// targets.
// Absolute-form https:// requests are refused: an egress proxy must not
// terminate someone else's TLS, and mainstream clients CONNECT instead.
//
// Every request must present the proxy key
//   Authorization: Bearer <key>        (Go clients, this project's tooling)
//   Proxy-Authorization: Basic user:<key>  (dockerd/curl from proxy URLs)
// A token wrong by even one byte gets 407. The proxy refuses to tunnel to
// loopback, link-local, RFC1918, CGNAT or cloud-metadata destinations: those
// belong to the consumers' own network and must be reached directly, not
// through an overseas relay. Hostnames are resolved here and refused when any
// resolved address is private, so a rebinding or LAN-aliasing name cannot be
// smuggled through.

// ProxyConfig is the proxy server configuration main.go assembles from env.
type ProxyConfig struct {
	Addr  string // listen address, e.g. ":18082"
	Token string // required key for every proxied connection
	// Anonymous skips the key check. Only for listeners bound to a private
	// interface (the docker bridge gateway): every container on this Docker
	// host can then use the proxy without holding a key. main.go validates
	// the bind address before starting such a listener.
	Anonymous bool
	// Upstream chains the listener to another forward proxy, e.g. a desktopd
	// abroad (DESKTOPD_UPSTREAM_PROXY). Mainland hosts use this so their
	// containers egress through the overseas proxy without holding its key.
	// Empty means this listener is the terminal egress and dials targets
	// directly.
	Upstream string
	// TLSCert and TLSKey wrap the listener in TLS when set. Mainland consumers
	// then reach the proxy with an https:// proxy URL: the CONNECT request
	// and its key cross the internet inside TLS, which also stops
	// connection resets that plaintext CONNECT lines attract on the
	// mainland side.
	TLSCert string
	TLSKey  string
	// Keys resolves the key per request when set (the admin panel's
	// file-backed key source): replacing the key in the panel takes effect on
	// the next connection without a restart. nil keeps the static Token.
	Keys *ProxyKeySource
	// Upstreams resolves the chained upstream per connection when set (the
	// panel's egress config): pointing the chain at a new proxy takes effect
	// on the next connection. nil keeps the static Upstream.
	Upstreams *EgressSource
}

// DefaultForwardProxyAddr is the default proxy port. 18081 is the admin API;
// 18082 is the next port so an operator can firewall the two independently.
const DefaultForwardProxyAddr = ":18082"

// ForwardProxy is one desktopd's outbound proxy server.
type ForwardProxy struct {
	cfg       ProxyConfig
	transport *http.Transport
	server    *http.Server
	listener  net.Listener // set by Start; lets tests and ops learn the port
	// dialHook lets tests inject a permissive dialer: loopback targets hit
	// the public-only guard, which a test tunnel needs to bypass.
	dialHook func(ctx context.Context, address string) (net.Conn, error)

	mu     sync.Mutex
	tunnel map[net.Conn]struct{}
}

// NewForwardProxy builds a proxy server. It does not listen yet; call Start.
func NewForwardProxy(cfg ProxyConfig) *ForwardProxy {
	p := &ForwardProxy{cfg: cfg, tunnel: map[net.Conn]struct{}{}}
	p.transport = &http.Transport{
		// Proxy resolves per request: nil unless chained upstream, and the
		// panel can repoint it between requests. dialVerified resolves each
		// target here and only dials public addresses.
		DialContext:           p.dialVerified,
		Proxy:                 func(*http.Request) (*url.URL, error) { return p.currentUpstreamURL() },
		ResponseHeaderTimeout: 2 * time.Minute,
		IdleConnTimeout:       90 * time.Second,
		DisableKeepAlives:     false,
	}
	return p
}

// Start binds the listener and serves until Stop or ctx cancellation.
func (p *ForwardProxy) Start(ctx context.Context) error {
	if _, ok := p.currentKey(); !ok {
		return fmt.Errorf("forward proxy requires a key")
	}
	// The static upstream must parse before anything depends on this
	// listener. A broken panel file is NOT fatal: the panel itself heals it
	// (the egress source denies loudly until then), so a restart is never
	// required to recover.
	if p.cfg.Upstream != "" {
		if _, err := parseUpstreamURL(p.cfg.Upstream); err != nil {
			return fmt.Errorf("DESKTOPD_UPSTREAM_PROXY: %w", err)
		}
	}
	ln, err := net.Listen("tcp", p.cfg.Addr)
	if err != nil {
		return err
	}
	if p.cfg.TLSCert != "" || p.cfg.TLSKey != "" {
		if p.cfg.TLSCert == "" || p.cfg.TLSKey == "" {
			_ = ln.Close()
			return fmt.Errorf("forward proxy TLS needs both certificate and key")
		}
		cert, err := tls.LoadX509KeyPair(p.cfg.TLSCert, p.cfg.TLSKey)
		if err != nil {
			_ = ln.Close()
			return fmt.Errorf("forward proxy TLS certificate: %w", err)
		}
		ln = tls.NewListener(ln, &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		})
	}
	p.listener = ln
	server := &http.Server{
		Handler: p,
		// The CONNECT header carries target and auth; a cold-region client
		// still gets a bounded wait, after which the connection is closed.
		ReadHeaderTimeout: 30 * time.Second,
		// Registry layer uploads can take minutes; do not cut idle tunnels.
		IdleTimeout: 30 * time.Minute,
	}
	p.server = server
	go func() {
		<-ctx.Done()
		_ = p.Stop()
	}()
	go func() {
		// Serve from the local capture: Stop nils p.server and a late read
		// of the field would dereference nil.
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[desktopd-proxy] serve error: %v", err)
		}
	}()
	log.Printf("[desktopd-proxy] forward proxy listening on %s", p.cfg.Addr)
	return nil
}

// Stop closes the listener and every still-open tunnel.
func (p *ForwardProxy) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var first error
	if p.server != nil {
		first = p.server.Close()
		p.server = nil
	}
	for conn := range p.tunnel {
		_ = conn.Close()
	}
	p.tunnel = map[net.Conn]struct{}{}
	return first
}

func (p *ForwardProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !p.authOK(r) {
		w.Header().Set("Proxy-Authenticate", `Bearer realm="desktopd forward proxy"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if r.Method == http.MethodConnect {
		p.serveConnect(w, r)
		return
	}
	// Absolute-form request: GET http://example.com/... HTTP/1.1.
	p.serveHTTPProxy(w, r)
}

// currentKey resolves the key for one request: the panel-backed source wins
// when configured, the static cfg.Token otherwise. Anonymous listeners skip
// key checks entirely.
func (p *ForwardProxy) currentKey() (string, bool) {
	if p.cfg.Anonymous {
		return "anonymous", true
	}
	if p.cfg.Keys != nil {
		key, _, err := p.cfg.Keys.Get()
		if err != nil {
			// Unreadable key file denies everything loudly (see proxytoken.go).
			log.Printf("[desktopd-proxy] proxy key unreadable: %v", err)
			return "", false
		}
		return key, key != ""
	}
	return p.cfg.Token, p.cfg.Token != ""
}

func (p *ForwardProxy) authOK(r *http.Request) bool {
	// Anonymous listeners (docker-bridge bound) answer without a key; main.go
	// refuses wildcard binds so they cannot become a public open relay.
	if p.cfg.Anonymous {
		return true
	}
	want, ok := p.currentKey()
	if !ok {
		return false
	}
	if got := strings.TrimSpace(r.Header.Get("Proxy-Authorization")); got != "" {
		scheme, value, hasValue := strings.Cut(got, " ")
		switch {
		case hasValue && strings.EqualFold(scheme, "Bearer"):
			return tokenMatches(want, strings.TrimSpace(value))
		case hasValue && strings.EqualFold(scheme, "Basic"):
			pass, err := basicPassword("Basic " + strings.TrimSpace(value))
			return err == nil && tokenMatches(want, pass)
		}
		return false
	}
	got := strings.TrimSpace(bearerToken(r))
	return got != "" && tokenMatches(want, got)
}

func tokenMatches(want, got string) bool {
	return len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// basicPassword decodes "Basic <base64 user:pass>" into the password part.
func basicPassword(header string) (string, error) {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), "Basic") {
		return "", fmt.Errorf("not basic auth")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[1]))
	if err != nil {
		return "", err
	}
	_, pass, ok := strings.Cut(string(raw), ":")
	if !ok {
		return "", fmt.Errorf("basic credential has no password")
	}
	return pass, nil
}

// serveConnect answers CONNECT by dialing the target and splicing the two
// connections. A hijacked connection can no longer use http.ResponseWriter,
// so the 200 line is written by hand before the splice.
func (p *ForwardProxy) serveConnect(w http.ResponseWriter, r *http.Request) {
	target := r.Host
	if target == "" {
		http.Error(w, "CONNECT target is required", http.StatusBadRequest)
		return
	}
	// The gate refuses non-public targets before any dial. Tests install a
	// dial hook and expect to reach loopback backends, so they bypass it.
	if p.dialHook == nil && !proxyTargetAllowed(target) {
		http.Error(w, "CONNECT target is not allowed", http.StatusForbidden)
		return
	}
	// The dial context must not be tied to the request: the tunnel outlives
	// the CONNECT request, and cancelling the request context would close
	// the just-dialed backend socket mid-transfer.
	dialCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var backend net.Conn
	var err error
	upstream, upstreamErr := p.currentUpstreamURL()
	if upstreamErr != nil {
		// Unreadable egress config denies the chain loudly.
		log.Printf("[desktopd-proxy] upstream config unreadable: %v", upstreamErr)
		http.Error(w, "upstream config unreadable", http.StatusBadGateway)
		return
	}
	if upstream != nil {
		// Chained egress: CONNECT through the upstream proxy (a desktopd
		// abroad) so this host does not need its own route to the target.
		backend, err = p.dialUpstream(dialCtx, target, upstream)
	} else {
		backend, err = p.dial(dialCtx, target)
	}
	if err != nil {
		// Failures are logged for ops (successes never are: this proxy must
		// not accumulate a record of what was browsed). Dialers never embed
		// proxy credentials, so the text carries network diagnostics only.
		log.Printf("[desktopd-proxy] CONNECT %s from %s: %v", target, r.RemoteAddr, err)
		http.Error(w, "upstream dial failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	rw, ok := w.(http.Hijacker)
	if !ok {
		_ = backend.Close()
		http.Error(w, "tunnel setup failed", http.StatusInternalServerError)
		return
	}
	conn, brw, err := rw.Hijack()
	if err != nil {
		_ = backend.Close()
		http.Error(w, "tunnel setup failed", http.StatusInternalServerError)
		return
	}
	// Bytes the client pipelined behind CONNECT (sent before the 200 was
	// seen) reach the backend first, in order. Mainstream clients wait for
	// the 200, so the buffer is normally empty.
	if n := brw.Reader.Buffered(); n > 0 {
		pipelined := make([]byte, n)
		_, _ = io.ReadFull(brw, pipelined)
		if _, err := backend.Write(pipelined); err != nil {
			_ = backend.Close()
			_ = conn.Close()
			return
		}
	}
	_, _ = brw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	_ = brw.Flush()
	p.join(conn, backend)
}

// serveHTTPProxy forwards an absolute-form http:// request. Other targets
// (origin-form requests, https) are rejected: relaying https here would mean
// the TLS session terminates inside this process.
func (p *ForwardProxy) serveHTTPProxy(w http.ResponseWriter, r *http.Request) {
	if r.URL == nil || r.URL.Scheme == "" || r.URL.Host == "" {
		http.Error(w, "proxy requests must use absolute-form", http.StatusBadRequest)
		return
	}
	if r.URL.Scheme != "http" {
		http.Error(w, "issue CONNECT for HTTPS targets", http.StatusBadRequest)
		return
	}
	if p.dialHook == nil && !proxyTargetAllowed(r.URL.Host) {
		http.Error(w, "proxy target is not allowed", http.StatusForbidden)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	// Connection names other hop-by-hop headers; read it before the loop
	// deletes the header itself.
	var connectionTokens []string
	if c := out.Header.Get("Connection"); c != "" {
		for _, name := range strings.Split(c, ",") {
			if name = strings.TrimSpace(name); name != "" {
				connectionTokens = append(connectionTokens, name)
			}
		}
	}
	for _, header := range hopByHopHeaders {
		out.Header.Del(header)
	}
	for _, name := range connectionTokens {
		out.Header.Del(name)
	}
	out.Header.Del("Proxy-Authorization")
	// Authorization normally authenticates to the TARGET site and must
	// travel; it is only consumed when it carries this proxy's own Bearer
	// key. Proxy authentication otherwise rides Proxy-Authorization.
	if auth := out.Header.Get("Authorization"); auth != "" {
		scheme, value, hasValue := strings.Cut(auth, " ")
		if hasValue && strings.EqualFold(scheme, "Bearer") && tokenMatches(p.cfg.Token, strings.TrimSpace(value)) {
			out.Header.Del("Authorization")
		}
	}
	resp, err := p.transport.RoundTrip(out)
	if err != nil {
		http.Error(w, "upstream request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, header := range hopByHopHeaders {
		resp.Header.Del(header)
	}
	header := w.Header()
	for name, values := range resp.Header {
		for _, value := range values {
			header.Add(name, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	// Chunked copy with flush: streaming responses (SSE, progress bodies)
	// reach the client as they arrive instead of waiting for the buffer.
	buf := make([]byte, 32<<10)
	rc := http.NewResponseController(w)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			_ = rc.Flush()
		}
		if readErr != nil {
			return
		}
	}
}

// hopByHopHeaders must not travel across a proxy hop (RFC 9110 §7.6.1).
var hopByHopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

// join pumps both directions until one copy finishes, then tears the tunnel
// down. Verify-first dialing means TLS integrity is the client's business:
// this process only relays bytes.
func (p *ForwardProxy) join(client, backend net.Conn) {
	p.mu.Lock()
	if p.tunnel == nil {
		p.tunnel = map[net.Conn]struct{}{}
	}
	p.tunnel[client] = struct{}{}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.tunnel, client)
		p.mu.Unlock()
	}()
	done := make(chan struct{}, 2)
	pump := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go pump(backend, client)
	go pump(client, backend)
	<-done
	// Both directions are dead the moment one ends. A registry download has
	// fully transferred before the server side closes, so this loses nothing;
	// it also bounds how long a stalled peer can hold the tunnel open.
	_ = client.Close()
	_ = backend.Close()
}

// dialVerified is the Transport DialContext. http.Transport only dials "tcp";
// dial itself works from the address alone.
func (p *ForwardProxy) dialVerified(ctx context.Context, _ string, address string) (net.Conn, error) {
	return p.dial(ctx, address)
}

// parseUpstreamURL is the single validator for chained-upstream URLs: they
// must parse as http(s) proxy URLs (credentials may ride as userinfo).
func parseUpstreamURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Host == "" {
		return nil, fmt.Errorf("upstream %q does not parse as a proxy URL", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("upstream scheme must be http or https, not %q", u.Scheme)
	}
	return u, nil
}

// currentUpstreamURL resolves the chained upstream for one connection: the
// panel's egress config wins when set (its empty value means direct egress),
// the static cfg.Upstream otherwise. nil = this listener dials targets
// directly.
func (p *ForwardProxy) currentUpstreamURL() (*url.URL, error) {
	raw := p.cfg.Upstream
	if p.cfg.Upstreams != nil {
		cfg, fromPanel, err := p.cfg.Upstreams.Get()
		if err != nil {
			return nil, err
		}
		if fromPanel {
			raw = cfg.Upstream
		}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	return parseUpstreamURL(raw)
}

// upstream, send CONNECT with the key it expects, require a 200, then hand the
// raw socket back so the splice covers client → upstream → target. TLS to the
// upstream (https:// scheme) keeps the CONNECT request and its key off the
// wire between this host and the overseas machine.
func (p *ForwardProxy) dialUpstream(ctx context.Context, target string, u *url.URL) (net.Conn, error) {
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, fmt.Errorf("upstream proxy %s: %w", host, err)
	}
	if u.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("upstream proxy %s TLS: %w", host, err)
		}
		conn = tlsConn
	}
	request := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
	if u.User != nil {
		pass, _ := u.User.Password()
		request += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+pass)) + "\r\n"
	}
	request += "\r\n"
	// Bound the upstream handshake, then clear the deadline: the tunnel
	// itself must not carry it.
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := io.WriteString(conn, request); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("upstream proxy %s: %w", host, err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("upstream proxy %s: %w", host, err)
	}
	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("upstream proxy %s answered HTTP %d", host, resp.StatusCode)
	}
	if reader.Buffered() > 0 {
		_ = conn.Close()
		return nil, fmt.Errorf("upstream proxy %s sent data before the tunnel", host)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func (p *ForwardProxy) dial(ctx context.Context, address string) (net.Conn, error) {
	if p.dialHook != nil {
		return p.dialHook(ctx, address)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("proxy address %q is invalid", address)
	}
	host = strings.Trim(host, "[]")
	if !proxyTargetAllowed(host) {
		return nil, fmt.Errorf("proxy target %q is not public", host)
	}
	proxyPort, err := parseProxyPort(port)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if websearch.IsBlockedPublicIP(ip) {
			return nil, fmt.Errorf("proxy target %q is not public", host)
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), proxyPort))
	}
	// A name is resolved here, on the proxy host, and refused when any
	// resolved address is private: a LAN-aliased or rebinding record must
	// not smuggle the tunnel back into somebody's intranet.
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	for _, addr := range ips {
		if websearch.IsBlockedPublicIP(addr.IP) {
			return nil, fmt.Errorf("resolve %s: non-public address %s", host, addr.IP.String())
		}
	}
	var dialer net.Dialer
	var lastErr error
	for _, addr := range ips {
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(addr.IP.String(), proxyPort))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no address dialed for %s", host)
}

// parseProxyPort validates a decimal TCP port, defaulting empty to 80 for
// absolute-form http:// URLs; CONNECT always carries an explicit port.
func parseProxyPort(port string) (string, error) {
	if port == "" {
		return "80", nil
	}
	if len(port) > 5 {
		return "", fmt.Errorf("proxy port is invalid")
	}
	v, err := strconv.Atoi(port)
	if err != nil || v < 1 || v > 65535 {
		return "", fmt.Errorf("proxy port is invalid")
	}
	return port, nil
}

// proxyTargetAllowed reports whether a target authority ("host:port" or a
// bare host) may be tunneled at all: placeholders (localhost), .local/.lan/
// .internal names and literal private addresses are refused. Name resolution
// gating happens in dial.
func proxyTargetAllowed(authority string) bool {
	host := authority
	if split, _, err := net.SplitHostPort(authority); err == nil {
		host = split
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "" {
		return false
	}
	if websearch.IsBlockedPublicHost(host) {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !websearch.IsBlockedPublicIP(ip)
	}
	return true
}

// DesktopProxyBind validates the container egress proxy URL and returns the
// host:port its unauthenticated listener binds plus the canonical URL the
// containers receive (scheme + IP + port; path, query and any userinfo are
// dropped — the bridge listener is unauthenticated and stray credentials
// must not leak into container env or the maclaw.desktop-proxy label).
// The URL host must be an RFC1918 IP literal (a docker bridge gateway, e.g.
// 172.17.0.1) so the unauthenticated proxy is only reachable from this
// host's containers, and loopback is refused because containers cannot reach
// the host's loopback.
func DesktopProxyBind(raw string) (bind, proxyURL string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Host == "" {
		return "", "", fmt.Errorf("desktop proxy URL does not parse")
	}
	if u.Scheme != "http" {
		return "", "", fmt.Errorf("desktop proxy URL scheme must be http")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsPrivate() {
		return "", "", fmt.Errorf("desktop proxy URL host must be a private IP (docker bridge gateway), not %q", u.Hostname())
	}
	if u.Port() == "" {
		return "", "", fmt.Errorf("desktop proxy URL needs an explicit port")
	}
	bind = net.JoinHostPort(ip.String(), u.Port())
	proxyURL = "http://" + net.JoinHostPort(ip.String(), u.Port())
	return bind, proxyURL, nil
}
