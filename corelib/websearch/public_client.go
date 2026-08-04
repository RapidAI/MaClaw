package websearch

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type publicNetworkContextKey struct{}

// WithPublicNetworkOnly marks an operation as an untrusted public-network
// request. Search and fetch handlers use it to avoid browser, proxy and
// credential-bearing transports.
func WithPublicNetworkOnly(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, publicNetworkContextKey{}, true)
}

func isPublicNetworkOnly(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(publicNetworkContextKey{}).(bool)
	return value
}

// newPublicHTTPClient only connects to public IP addresses. It intentionally
// ignores the desktop proxy and cookie jar: callers use it for untrusted URLs
// where a group member must not be able to reach workstation-local services.
func newPublicHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           publicDialContext(dialer),
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DisableCompression:    true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return validateResolvedPublicURL(req.Context(), req.URL)
		},
	}
}

func validatePublicHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("public fetch only supports HTTP(S) URLs")
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("URL host is required")
	}
	if u.User != nil {
		// net/http turns URL userinfo into an Authorization header. Public group
		// fetches must not carry caller-supplied credentials, including after a
		// redirect where the target URL contains userinfo.
		return nil, fmt.Errorf("public fetch URLs must not contain credentials")
	}
	if isBlockedPublicHost(u.Hostname()) {
		return nil, fmt.Errorf("URL host %q is not public", u.Hostname())
	}
	u.Fragment = ""
	return u, nil
}

func validateResolvedPublicURL(ctx context.Context, u *url.URL) error {
	if u == nil {
		return fmt.Errorf("URL is required")
	}
	if _, err := validatePublicHTTPURL(u.String()); err != nil {
		return err
	}
	_, err := resolvePublicIPs(ctx, u.Hostname())
	return err
}

func publicDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := resolvePublicIPs(ctx, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("no public address found for %s", host)
	}
}

func resolvePublicIPs(ctx context.Context, host string) ([]net.IP, error) {
	host = strings.Trim(host, "[]")
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedPublicIP(ip) {
			return nil, fmt.Errorf("host %s resolves to non-public IP", host)
		}
		return []net.IP{ip}, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	public := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		if isBlockedPublicIP(addr.IP) {
			return nil, fmt.Errorf("host %s resolves to non-public IP %s", host, addr.IP.String())
		}
		public = append(public, addr.IP)
	}
	return public, nil
}

func isBlockedPublicHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return true
	}
	if strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".home") {
		return true
	}
	return isBlockedPublicIP(net.ParseIP(host))
}

func isBlockedPublicIP(ip net.IP) bool {
	if ip == nil {
		return false // A hostname must be resolved before a connection is made.
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 10 ||
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
			(ip4[0] == 192 && ip4[1] == 168) ||
			(ip4[0] == 169 && ip4[1] == 254) ||
			(ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127) ||
			(ip4[0] == 127) || ip4[0] == 0 || ip4[0] >= 224 ||
			(ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19))
	}
	return ip.IsPrivate()
}
