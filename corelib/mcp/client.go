package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
)

// MCPServerConfig holds the configuration for connecting to an MCP server.
type MCPServerConfig struct {
	EndpointURL string            `json:"endpoint_url"`
	Transport   string            `json:"transport"` // "sse" | "streamable-http"
	Headers     map[string]string `json:"headers,omitempty"`
	APIKey      string            `json:"api_key,omitempty"`
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
func sendMCPRequest(ctx context.Context, config MCPServerConfig, method string, params any) (json.RawMessage, error) {
	// Validate transport type.
	switch config.Transport {
	case "sse", "streamable-http", "":
		// Supported HTTP-based transports.
	default:
		return nil, fmt.Errorf("unsupported MCP transport: %q (only \"sse\" and \"streamable-http\" are supported for server-side validation)", config.Transport)
	}

	// Validate endpoint URL.
	if config.EndpointURL == "" {
		return nil, fmt.Errorf("MCP server endpoint URL is empty")
	}
	if _, err := url.ParseRequestURI(config.EndpointURL); err != nil {
		return nil, fmt.Errorf("invalid MCP server endpoint URL: %w", err)
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
	resp, err := http.DefaultClient.Do(httpReq)
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
