package remote

import (
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestHubHTTPClientDoesNotFollowCrossPortCredentialRedirect(t *testing.T) {
	t.Setenv(HubCAFileEnv, "")
	t.Setenv(HubTLSInsecureEnv, "")
	var targetCalled atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalled.Store(true)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	req, err := http.NewRequest(http.MethodGet, source.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	client := NewHubHTTPClient()
	defer client.CloseIdleConnections()

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want the unforwarded redirect", resp.StatusCode)
	}
	if targetCalled.Load() {
		t.Fatal("Hub credential-bearing request followed a redirect to another service")
	}
}

func TestValidateHubURLRequiresEncryptionOffLoopback(t *testing.T) {
	t.Setenv(HubAllowHTTPEnv, "")
	for _, test := range []struct {
		raw     string
		allowed bool
	}{
		{raw: "https://hub.example", allowed: true},
		{raw: "wss://hub.example/ws", allowed: true},
		{raw: "http://127.0.0.1:9399", allowed: true},
		{raw: "ws://localhost:9399/ws", allowed: true},
		{raw: "ws://[::1]:9399/ws", allowed: true},
		{raw: "http://hub.example"},
		{raw: "ws://hub.example/ws"},
		{raw: "ftp://hub.example"},
		{raw: "https://user:secret@hub.example"},
	} {
		err := ValidateHubURL(test.raw)
		if (err == nil) != test.allowed {
			t.Errorf("ValidateHubURL(%q) error = %v, allowed = %t", test.raw, err, test.allowed)
		}
	}

	t.Setenv(HubAllowHTTPEnv, "1")
	if err := ValidateHubURL("http://hub.example"); err != nil {
		t.Fatalf("explicit legacy HTTP opt-in was rejected: %v", err)
	}
}

func TestHubHTTPClientRejectsCleartextPublicHubBeforeDial(t *testing.T) {
	t.Setenv(HubAllowHTTPEnv, "")
	client := NewHubHTTPClient()
	defer client.CloseIdleConnections()
	resp, err := client.Get("http://192.0.2.1/api/private")
	if err == nil {
		resp.Body.Close()
		t.Fatal("cleartext public Hub request was sent")
	}
	if !strings.Contains(err.Error(), HubAllowHTTPEnv) {
		t.Fatalf("error = %v, want explicit cleartext policy failure", err)
	}
}

func TestHubHTTPClientCloseIdleConnections(t *testing.T) {
	t.Setenv(HubAllowHTTPEnv, "")
	states := make(chan http.ConnState, 8)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		states <- state
	}
	server.Start()
	defer server.Close()
	client := NewHubHTTPClient()
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	client.CloseIdleConnections()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case state := <-states:
			if state == http.StateClosed {
				return
			}
		case <-deadline:
			t.Fatal("idle connection was not closed by the client")
		}
	}
}
