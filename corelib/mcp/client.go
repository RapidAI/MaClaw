package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// MCPServerConfig holds the configuration for connecting to an MCP server.
type MCPServerConfig struct {
	EndpointURL string            `json:"endpoint_url"`
	Transport   string            `json:"transport"` // "sse" | "streamable-http"
	Headers     map[string]string `json:"headers,omitempty"`
	APIKey      string            `json:"api_key,omitempty"`
	// PublicNetworkOnly (default false) additionally routes MCP remote requests
	// through the SSRF-safe HTTP client (no proxy, IP-level allowlist, DNS
	// re-resolve), which rejects loopback, RFC1918, CGNAT and every other
	// private range. Set it to true to harden a server that is known to live on
	// the public internet.
	//
	// It defaults to false on purpose: MCP-over-HTTP on localhost is a
	// legitimate, tested deployment, so making public-only the default would
	// break it. Independently of this flag, cloud-metadata and link-local
	// destinations are always refused — see isAlwaysBlockedMCPHost.
	// P0-6 of the 2026-09-08 review.
	PublicNetworkOnly *bool `json:"public_network_only,omitempty"`
}

// jsonRPCRequest represents a JSON-RPC 2.0 request message.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse represents a JSON-RPC 2.0 response message.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError represents a JSON-RPC 2.0 error object.
type jsonRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// requestIDCounter is used to generate unique JSON-RPC request IDs.
var requestIDCounter atomic.Int64

// sendMCPRequest sends a JSON-RPC request to an MCP server via HTTP transport.
// Only "sse" and "streamable-http" transports are supported (HTTP-based).
// Returns the result field from the JSON-RPC response, or an error for
// non-200 status, JSON-RPC errors, or transport errors.
//
// Security (P0-6, 2026-09-08 review):
//   - The endpoint URL is validated against the SSRF guard (loopback, link-local,
//     RFC1918, CGNAT, cloud-metadata addresses are rejected) when
//     config.PublicNetworkOnly is true or unset.
//   - Requests are dispatched through an http.Client without a proxy so the
//     desktop HTTP_PROXY/HTTPS_PROXY environment cannot be abused to reach
//     internal services the public-network guard would otherwise have denied.
func sendMCPRequest(ctx context.Context, config MCPServerConfig, method string, params any) (json.RawMessage, error) {
	// Validate transport type.
	switch config.Transport {
	case "sse", "streamable-http", "":
		// Supported HTTP-based transports.
	default:
		return nil, fmt.Errorf("unsupported MCP transport: %q (only \"sse\" and \"streamable-http\" are supported for server-side validation)", config.Transport)
	}

	// Default is false, i.e. preserve the pre-hardening behaviour of allowing
	// loopback and private-network endpoints. MCP servers served over HTTP from
	// localhost are a legitimate and widely used deployment (and are covered by
	// this package's own tests), so flipping the default to public-only would
	// break supported setups rather than fix a vulnerability.
	//
	// The catastrophic SSRF class — cloud instance metadata — is blocked
	// unconditionally further down and cannot be re-enabled here.
	publicOnly := config.PublicNetworkOnly != nil && *config.PublicNetworkOnly

	// Validate endpoint URL.
	if config.EndpointURL == "" {
		return nil, fmt.Errorf("MCP server endpoint URL is empty")
	}
	if _, err := url.ParseRequestURI(config.EndpointURL); err != nil {
		return nil, fmt.Errorf("invalid MCP server endpoint URL: %w", err)
	}
	if publicOnly {
		if _, err := websearch.ValidatePublicHTTPURL(config.EndpointURL); err != nil {
			return nil, fmt.Errorf("MCP endpoint URL rejected by SSRF guard: %w", err)
		}
	}
	// Always-on guard, independent of PublicNetworkOnly: cloud-metadata and
	// link-local destinations are refused even for operators who deliberately
	// opted into private-network MCP servers. A single JSON-RPC round trip to
	// 169.254.169.254 hands back cloud STS credentials, so this class is
	// blocked unconditionally rather than left to configuration.
	if host, err := mcpEndpointHost(config.EndpointURL); err == nil && isAlwaysBlockedMCPHost(host) {
		return nil, fmt.Errorf("MCP endpoint host %q is always blocked (cloud metadata or link-local)", host)
	}

	// Build JSON-RPC 2.0 request.
	reqID := requestIDCounter.Add(1)
	rpcReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON-RPC request: %w", err)
	}

	// Create HTTP request.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, config.EndpointURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set standard headers.
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	// Apply configured headers.
	for key, value := range config.Headers {
		httpReq.Header.Set(key, value)
	}

	// Apply API key as Bearer token if configured.
	if config.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+config.APIKey)
	}

	// Send HTTP request.
	client := pickMCPClient(publicOnly)
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("MCP HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body (limit to 10MB to prevent OOM from malicious servers).
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read MCP response body: %w", err)
	}

	// Check HTTP status.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP server returned HTTP %d: body_len=%d", resp.StatusCode, len(respBody))
	}

	// Parse JSON-RPC response.
	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to parse MCP JSON-RPC response: %w", err)
	}

	// Check for JSON-RPC error.
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("MCP JSON-RPC error (code %d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// mcpAlwaysBlockedHosts are hosts that must never be used as an MCP endpoint,
// regardless of PublicNetworkOnly. They are either cloud instance-metadata
// services or link-local addresses whose only purpose is infrastructure
// discovery.
var mcpAlwaysBlockedHosts = map[string]struct{}{
	"metadata":                 {},
	"metadata.google.internal": {},
	"metadata.goog":            {},
	"169.254.169.254":          {}, // AWS / Azure / OpenStack / DigitalOcean
	"100.100.100.200":          {}, // Alibaba Cloud
	"fd00:ec2::254":            {}, // AWS over IPv6
	"fd00:ec2::253":            {},
}

func mcpEndpointHost(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	return u.Hostname(), nil
}

// isAlwaysBlockedMCPHost reports whether host is a cloud-metadata or link-local
// destination. Applied to every MCP remote request as a floor that
// PublicNetworkOnly=false cannot lower.
func isAlwaysBlockedMCPHost(host string) bool {
	h := strings.ToLower(strings.Trim(strings.Trim(host, "[]"), "."))
	if h == "" {
		return true
	}
	if _, ok := mcpAlwaysBlockedHosts[h]; ok {
		return true
	}
	if strings.HasPrefix(h, "metadata.") && strings.HasSuffix(h, ".internal") {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		// A DNS name is not blocked on its own; resolveMCPDialAddrs rejects it
		// if it resolves to a metadata address.
		return false
	}
	return isAlwaysBlockedMCPIP(ip)
}

// isAlwaysBlockedMCPIP reports whether a resolved address is a cloud-metadata
// or link-local destination. Loopback is deliberately allowed so that
// localhost MCP sidecars keep working.
func isAlwaysBlockedMCPIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 100.64.0.0/10 — CGNAT, which is where Alibaba Cloud metadata lives
		// (100.100.100.200) alongside 169.254.169.254 (link-local above).
		return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
	}
	// IPv6 unique-local (fc00::/7) — fd00:ec2::254 style endpoints.
	return ip.IsPrivate()
}

// resolveMCPDialAddrs resolves host into dialable addresses, refusing any
// cloud-metadata or link-local destination. Literal addresses are returned
// as-is. IPs are returned so callers can dial exactly what was validated,
// closing the DNS-rebinding window between check and connect.
func resolveMCPDialAddrs(ctx context.Context, host string) ([]net.IP, error) {
	h := strings.ToLower(strings.Trim(strings.Trim(host, "[]"), "."))
	if h == "" {
		return nil, fmt.Errorf("MCP endpoint host is empty")
	}
	if ip := net.ParseIP(h); ip != nil {
		if isAlwaysBlockedMCPIP(ip) {
			return nil, fmt.Errorf("MCP host %s is always blocked (cloud metadata or link-local)", h)
		}
		return []net.IP{ip}, nil
	}
	if isAlwaysBlockedMCPHost(h) {
		return nil, fmt.Errorf("MCP host %q is always blocked (cloud metadata)", h)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", h, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", h)
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		if isAlwaysBlockedMCPIP(addr.IP) {
			return nil, fmt.Errorf("MCP host %q resolves to blocked address %s", h, addr.IP)
		}
		ips = append(ips, addr.IP)
	}
	return ips, nil
}

// assertMCPDialAllowed is the guard used by the private-network MCP transport
// and by its redirect policy.
func assertMCPDialAllowed(ctx context.Context, host string) error {
	_, err := resolveMCPDialAddrs(ctx, host)
	return err
}

// pickMCPClient returns the SSRF-safe client when the caller asks for public
// network only, and an isolated private-network client otherwise.
//
// The private-network mode still allows loopback / RFC1918 destinations
// (localhost MCP sidecars are a supported deployment) but it is NOT an
// unguarded http.Client:
//
//   - the environment proxy is ignored, so HTTP(S)_PROXY cannot be used to
//     reach an internal service the guard would otherwise have denied;
//   - every redirect target is re-validated;
//   - the dialer resolves the host itself and refuses cloud-metadata /
//     link-local / CGNAT addresses, then dials exactly the validated IP.
//
// Without the last two, the "always blocked" check on the initial URL alone
// was bypassable with a single 307 to 169.254.169.254 (found on 2026-09-09
// while re-reviewing the P0-6 fix).
func pickMCPClient(publicOnly bool) *http.Client {
	if publicOnly {
		return websearch.NewPublicHTTPClient(30 * time.Second)
	}
	return newMCPPrivateClient(30 * time.Second)
}

// NewPrivateHTTPClient is the exported form of newMCPPrivateClient (2026-09-09
// re-review). corelib/agentservice maintains a second MCP-over-HTTP stack
// (health probes and tools/call) that does not go through sendMCPRequest, so it
// must share this transport instead of building its own http.Client — otherwise
// the metadata guard only covers one of the two remote MCP code paths.
func NewPrivateHTTPClient(timeout time.Duration) *http.Client {
	return newMCPPrivateClient(timeout)
}

// IsAlwaysBlockedMCPHost is the exported form of isAlwaysBlockedMCPHost. It is
// a purely syntactic check: it never resolves DNS, so it is safe to call on
// every admission and every request. Name-to-address binding is enforced
// separately at dial time by newMCPPrivateClient.
func IsAlwaysBlockedMCPHost(host string) bool {
	return isAlwaysBlockedMCPHost(host)
}

// AssertDialAllowed is the exported form of assertMCPDialAllowed, so callers
// can pre-flight a user-supplied endpoint before issuing a request. It
// performs a DNS lookup; do not put it on a hot path.
func AssertDialAllowed(ctx context.Context, host string) error {
	return assertMCPDialAllowed(ctx, host)
}

func newMCPPrivateClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := resolveMCPDialAddrs(ctx, host)
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
			return nil, fmt.Errorf("no dialable address for MCP host %s", host)
		},
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		DisableCompression:    true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			host := req.URL.Hostname()
			if host == "" {
				return fmt.Errorf("MCP redirect target has no host")
			}
			if err := assertMCPDialAllowed(req.Context(), host); err != nil {
				return fmt.Errorf("refusing MCP redirect: %w", err)
			}
			return nil
		},
	}
}
