package remote

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	HubCAFileEnv      = "MACLAW_HUB_CA_FILE"
	HubTLSInsecureEnv = "MACLAW_HUB_TLS_INSECURE"
)

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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSClientConfig:       tlsConfig,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   32,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
	}
}
