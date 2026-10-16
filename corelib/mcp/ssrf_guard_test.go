package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsAlwaysBlockedMCPHost(t *testing.T) {
	blocked := []string{
		"169.254.169.254", // AWS / Azure / OpenStack metadata
		"100.100.100.200", // Alibaba Cloud metadata
		"fd00:ec2::254",   // AWS metadata over IPv6
		"metadata",
		"metadata.google.internal",
		"METADATA.GOOGLE.INTERNAL",
		"metadata.custom.internal",
		"0.0.0.0",
		"::",
		"fe80::1",
		"",
	}
	for _, host := range blocked {
		if !isAlwaysBlockedMCPHost(host) {
			t.Errorf("isAlwaysBlockedMCPHost(%q) = false, want true", host)
		}
	}

	allowed := []string{
		"127.0.0.1", // loopback MCP servers are a supported deployment
		"localhost",
		"10.0.0.5",
		"192.168.1.10",
		"example.com",
		"mcp.example.com",
	}
	for _, host := range allowed {
		if isAlwaysBlockedMCPHost(host) {
			t.Errorf("isAlwaysBlockedMCPHost(%q) = true, want false", host)
		}
	}
}

// The metadata floor must hold even when the operator opts into
// private-network access, otherwise PublicNetworkOnly=false would reopen the
// catastrophic SSRF class.
func TestSendMCPRequestBlocksMetadataWhenPrivateNetworkAllowed(t *testing.T) {
	priv := false
	cfg := MCPServerConfig{
		EndpointURL:       "http://169.254.169.254/latest/meta-data/",
		Transport:         "streamable-http",
		PublicNetworkOnly: &priv,
	}
	_, err := sendMCPRequest(context.Background(), cfg, "tools/list", nil)
	if err == nil {
		t.Fatal("expected cloud metadata endpoint to be rejected even with PublicNetworkOnly=false")
	}
}

func TestSendMCPRequestAllowsLoopbackByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	cfg := MCPServerConfig{EndpointURL: srv.URL, Transport: "streamable-http"}
	if _, err := sendMCPRequest(context.Background(), cfg, "tools/list", nil); err != nil {
		t.Fatalf("loopback MCP server should remain reachable by default, got: %v", err)
	}
}

func TestSendMCPRequestRejectsLoopbackWhenPublicOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	pub := true
	cfg := MCPServerConfig{
		EndpointURL:       srv.URL,
		Transport:         "streamable-http",
		PublicNetworkOnly: &pub,
	}
	_, err := sendMCPRequest(context.Background(), cfg, "tools/list", nil)
	if err == nil {
		t.Fatal("expected loopback endpoint to be rejected when PublicNetworkOnly=true")
	}
}

func TestMCPEndpointHost(t *testing.T) {
	got, err := mcpEndpointHost("http://user:pw@169.254.169.254:80/x?y=1")
	if err != nil {
		t.Fatalf("mcpEndpointHost: %v", err)
	}
	if got != "169.254.169.254" {
		t.Fatalf("mcpEndpointHost = %q, want 169.254.169.254", got)
	}
}

var _ = json.RawMessage{}
