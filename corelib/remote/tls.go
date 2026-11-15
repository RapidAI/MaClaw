package remote

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	HubCAFileEnv      = "MACLAW_HUB_CA_FILE"
	HubTLSInsecureEnv = "MACLAW_HUB_TLS_INSECURE"
	HubAllowHTTPEnv   = "MACLAW_HUB_ALLOW_HTTP"
)

// ValidateHubURL rejects cleartext credentials outside loopback unless an
// operator explicitly opts into a legacy HTTP deployment.
func ValidateHubURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.User != nil || parsed.Hostname() == "" {
		return fmt.Errorf("invalid Hub URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "wss":
		return nil
	case "http", "ws":
		if strings.EqualFold(parsed.Hostname(), "localhost") {
			return nil
		}
		if ip := net.ParseIP(parsed.Hostname()); ip != nil && ip.IsLoopback() {
			return nil
		}
		switch strings.ToLower(strings.TrimSpace(os.Getenv(HubAllowHTTPEnv))) {
		case "1", "true", "yes", "on":
			return nil
		}
	default:
		return fmt.Errorf("Hub URL must use HTTPS/WSS")
	}
	return fmt.Errorf("cleartext Hub traffic requires loopback or explicit %s=1", HubAllowHTTPEnv)
}

// HubTLSConfig verifies Hub certificates by default. A private Hub can add its
// CA to the system roots without disabling hostname or chain verification.
func HubTLSConfig() (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(HubTLSInsecureEnv))) {
	case "1", "true", "yes", "on":
		// Explicit compatibility mode for legacy deployments. Never infer this
		// from a connection failure or a self-signed certificate.
		cfg.InsecureSkipVerify = true
		return cfg, nil
	}

	caFile := strings.TrimSpace(os.Getenv(HubCAFileEnv))
	if caFile == "" {
		return cfg, nil
	}
	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read Hub CA file %q: %w", caFile, err)
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system certificate roots: %w", err)
	}
	if roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("Hub CA file %q contains no valid PEM certificate", caFile)
	}
	cfg.RootCAs = roots
	return cfg, nil
}

// SameOriginRedirect allows redirects only within one scheme/host/port origin.
func SameOriginRedirect(req *http.Request, via []*http.Request) error {
	if req == nil || req.URL == nil || req.URL.User != nil || len(via) == 0 || len(via) >= 3 {
		return http.ErrUseLastResponse
	}
	previous := via[len(via)-1].URL
	current := req.URL
	if previous == nil || previous.Hostname() == "" ||
		!strings.EqualFold(previous.Scheme, current.Scheme) ||
		!strings.EqualFold(previous.Hostname(), current.Hostname()) ||
		effectiveURLPort(previous) != effectiveURLPort(current) {
		return http.ErrUseLastResponse
	}
	return nil
}

func effectiveURLPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(value.Scheme, "http") {
		return "80"
	}
	return ""
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type hubTransport struct {
	base *http.Transport
}

func (t *hubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := ValidateHubURL(req.URL.String()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

func (t *hubTransport) CloseIdleConnections() {
	t.base.CloseIdleConnections()
}

func (t *hubTransport) Base() *http.Transport {
	return t.base
}

// NewHubTransport applies Hub URL policy to a custom HTTP/1.1 or HTTP/2 transport.
func NewHubTransport(base *http.Transport) http.RoundTripper {
	return &hubTransport{base: base}
}

// NewHubHTTPClient creates a pooled Hub client. Invalid TLS configuration
// fails every request explicitly; it never falls back to skipping verification.
func NewHubHTTPClient() *http.Client {
	tlsConfig, err := HubTLSConfig()
	if err != nil {
		return &http.Client{
			Timeout: 30 * time.Second,
			Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return nil, err
			}),
		}
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       tlsConfig,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: SameOriginRedirect,
		Transport:     &hubTransport{base: transport},
	}
}
