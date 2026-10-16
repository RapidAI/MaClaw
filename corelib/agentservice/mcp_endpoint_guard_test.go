package agentservice

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// TestValidateMCPRemoteEndpointBlocksMetadata guards the agentservice MCP
// stack, which is separate from corelib/mcp.sendMCPRequest and was left
// unguarded by the original P0-6 fix (found on the 2026-09-09 re-review).
func TestValidateMCPRemoteEndpointBlocksMetadata(t *testing.T) {
	blocked := []string{
		"http://169.254.169.254/latest/meta-data/",
		"https://169.254.169.254/",
		"http://100.100.100.200/latest/meta-data/",
		"http://[fd00:ec2::254]/latest/",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://0.0.0.0/",
	}
	for _, endpoint := range blocked {
		if err := validateMCPRemoteEndpoint(endpoint); err == nil {
			t.Errorf("endpoint %q must be rejected", endpoint)
		}
	}
}

func TestValidateMCPRemoteEndpointAllowsLocalAndLAN(t *testing.T) {
	allowed := []string{
		"http://127.0.0.1:8787/mcp",
		"https://localhost:8787/mcp",
		"http://192.168.50.4:8787/mcp",
		"http://10.0.0.9:8787/sse",
		"https://mcp.example.com/mcp",
	}
	for _, endpoint := range allowed {
		if err := validateMCPRemoteEndpoint(endpoint); err != nil {
			t.Errorf("endpoint %q must stay allowed: %v", endpoint, err)
		}
	}
}

func TestValidateMCPRemoteEndpointRejectsBadInput(t *testing.T) {
	for _, endpoint := range []string{"", "   ", "file:///etc/passwd", "ftp://example.com/x", "not-a-url", "http:///mcp"} {
		if err := validateMCPRemoteEndpoint(endpoint); err == nil {
			t.Errorf("endpoint %q must be rejected", endpoint)
		}
	}
}

// TestDoRemoteMCPRoundTripGuardsEndpoint proves the choke point itself refuses
// the request before any transport is touched (client is nil here, so an
// unguarded implementation would panic rather than return an error).
func TestDoRemoteMCPRoundTripGuardsEndpoint(t *testing.T) {
	entry := corelib.MCPServerEntry{
		EndpointURL: "http://169.254.169.254/latest/meta-data/iam/security-credentials/",
	}
	_, _, err := doRemoteMCPRoundTrip(nil, entry, "", map[string]interface{}{"jsonrpc": "2.0"})
	if err == nil {
		t.Fatal("doRemoteMCPRoundTrip must refuse a metadata endpoint")
	}
	if !strings.Contains(err.Error(), "endpoint_url") {
		t.Fatalf("expected an endpoint guard error, got: %v", err)
	}
}
