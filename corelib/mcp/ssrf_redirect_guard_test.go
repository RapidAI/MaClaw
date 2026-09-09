package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestMCPPrivateClientFollowsRedirectGuard proves the private-network MCP
// client (the default when PublicNetworkOnly is unset) refuses cloud-metadata
// destinations at redirect time as well as at dial time. Before the fix the
// always-on guard only inspected the initial URL, so a 307 from any endpoint
// redirected straight into 169.254.169.254.
func TestMCPPrivateClientFollowsRedirectGuard(t *testing.T) {
	client := pickMCPClient(false)
	if client.CheckRedirect == nil {
		t.Fatal("private MCP client must install a CheckRedirect guard")
	}
	if err := client.CheckRedirect(&http.Request{URL: mustURL(t, "http://169.254.169.254/latest/meta-data/")}, nil); err == nil {
		t.Fatal("redirect to cloud metadata must be rejected")
	}
	if err := client.CheckRedirect(&http.Request{URL: mustURL(t, "http://100.100.100.200/latest/meta-data/")}, nil); err == nil {
		t.Fatal("redirect to Alibaba metadata must be rejected")
	}
	if err := client.CheckRedirect(&http.Request{URL: mustURL(t, "http://127.0.0.1:8787/mcp")}, nil); err != nil {
		t.Fatalf("redirect to a loopback MCP sidecar must stay allowed: %v", err)
	}
}

func TestMCPDialGuard(t *testing.T) {
	ctx := context.Background()
	blocked := []string{"169.254.169.254", "100.100.100.200", "fd00:ec2::254", "0.0.0.0", "::"}
	for _, host := range blocked {
		if err := assertMCPDialAllowed(ctx, host); err == nil {
			t.Errorf("host %q must be blocked at dial time", host)
		}
	}
	// Localhost / LAN MCP sidecars are a supported deployment and must keep working.
	for _, host := range []string{"127.0.0.1", "::1", "192.168.50.4", "10.0.0.9"} {
		if err := assertMCPDialAllowed(ctx, host); err != nil {
			t.Errorf("host %q must remain dialable: %v", host, err)
		}
	}
}

// TestMCPPrivateClientRejectsMetadataThroughRedirect is an end-to-end check
// that a real 307 to a metadata address never reaches the dialer.
func TestMCPPrivateClientRejectsMetadataThroughRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/iam/security-credentials/", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	client := pickMCPClient(false)
	resp, err := client.Get(server.URL + "/mcp")
	if err == nil {
		resp.Body.Close()
		t.Fatal("redirect to cloud metadata must fail closed")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected a guard error, got: %v", err)
	}
}

func TestMCPPrivateClientIgnoresEnvironmentProxy(t *testing.T) {
	transport, ok := pickMCPClient(false).Transport.(*http.Transport)
	if !ok {
		t.Skip("private MCP client does not use *http.Transport")
	}
	if transport.Proxy == nil {
		return
	}
	// A loopback MCP request must never be tunnelled through an ambient proxy.
	u, err := transport.Proxy(&http.Request{URL: mustURL(t, "http://127.0.0.1:8787/mcp")})
	if err == nil && u != nil {
		t.Fatalf("loopback MCP request must not be proxied, got %s", u)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("bad test URL %q: %v", raw, err)
	}
	return parsed
}
