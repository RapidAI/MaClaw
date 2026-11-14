package remote

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHubHTTPClientRejectsUntrustedCertificate(t *testing.T) {
	t.Setenv(HubCAFileEnv, "")
	t.Setenv(HubTLSInsecureEnv, "")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := NewHubHTTPClient()
	defer client.CloseIdleConnections()

	resp, err := client.Get(server.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("self-signed Hub certificate was accepted without an explicit trust root")
	}
}

func TestHubHTTPClientAcceptsConfiguredCA(t *testing.T) {
	t.Setenv(HubTLSInsecureEnv, "")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	cert, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(t.TempDir(), "hub-ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(HubCAFileEnv, caFile)
	client := NewHubHTTPClient()
	defer client.CloseIdleConnections()

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("configured Hub CA was rejected: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestHubHTTPClientFailsClosedForInvalidCA(t *testing.T) {
	t.Setenv(HubTLSInsecureEnv, "")
	caFile := filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(caFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(HubCAFileEnv, caFile)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := NewHubHTTPClient()
	defer client.CloseIdleConnections()

	resp, err := client.Get(server.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("invalid CA configuration must fail before a successful request")
	}
	if !strings.Contains(err.Error(), "contains no valid PEM certificate") {
		t.Fatalf("request error = %v, want the CA configuration failure", err)
	}
}

func TestHubTLSConfigRequiresExplicitInsecureOptIn(t *testing.T) {
	t.Setenv(HubCAFileEnv, "")
	t.Setenv(HubTLSInsecureEnv, "")
	cfg, err := HubTLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InsecureSkipVerify {
		t.Fatal("TLS verification is disabled by default")
	}

	t.Setenv(HubTLSInsecureEnv, "1")
	cfg, err = HubTLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.InsecureSkipVerify {
		t.Fatal("explicit legacy compatibility opt-in was not honored")
	}
}
