package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestCheckToolAvailability_Success verifies that CheckToolAvailability correctly
// parses a successful tools/list response containing multiple tools.
func TestCheckToolAvailability_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request is a valid JSON-RPC call to tools/list.
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		method, _ := req["method"].(string)
		if method != "tools/list" {
			t.Errorf("expected method=tools/list, got %q", method)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]any{
				"tools": []map[string]any{
					{"name": "read_file", "description": "Read a file from disk"},
					{"name": "write_file", "description": "Write content to a file"},
					{"name": "list_dir", "description": "List directory contents"},
				},
			},
		})
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	result := v.CheckToolAvailability(context.Background(), config)

	if !result.Available {
		t.Fatalf("expected available=true, got false: %s", result.Error)
	}
	if result.Error != "" {
		t.Fatalf("expected no error, got: %s", result.Error)
	}
	if result.Warning != "" {
		t.Fatalf("expected no warning, got: %s", result.Warning)
	}
	if len(result.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(result.Tools))
	}

	expectedNames := []string{"read_file", "write_file", "list_dir"}
	for i, name := range expectedNames {
		if result.Tools[i] != name {
			t.Errorf("tool[%d]: expected %q, got %q", i, name, result.Tools[i])
		}
	}
}

// TestCheckToolAvailability_EmptyToolList verifies that an empty tools list
// returns Available=true with a warning message.
func TestCheckToolAvailability_EmptyToolList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result":  map[string]any{"tools": []any{}},
		})
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	result := v.CheckToolAvailability(context.Background(), config)

	if !result.Available {
		t.Fatalf("expected available=true for empty list, got false: %s", result.Error)
	}
	if result.Warning == "" {
		t.Fatal("expected a warning for empty tool list, got empty string")
	}
	if len(result.Tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(result.Tools))
	}
}

// TestCheckToolAvailability_NullToolsField verifies that a response with
// a null/missing tools field is treated as an empty list (warning).
func TestCheckToolAvailability_NullToolsField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result":  map[string]any{},
		})
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	result := v.CheckToolAvailability(context.Background(), config)

	if !result.Available {
		t.Fatalf("expected available=true for null tools, got false: %s", result.Error)
	}
	if result.Warning == "" {
		t.Fatal("expected a warning for null/missing tools field")
	}
}

// TestCheckToolAvailability_JSONRPCError verifies that a JSON-RPC error response
// from tools/list results in Available=false with the error message.
func TestCheckToolAvailability_JSONRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"error": map[string]any{
				"code":    -32601,
				"message": "Method not found",
			},
		})
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	result := v.CheckToolAvailability(context.Background(), config)

	if result.Available {
		t.Fatal("expected available=false for JSON-RPC error")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error message")
	}
}

// TestCheckToolAvailability_HTTPError verifies that an HTTP error (non-200)
// from the server results in Available=false with error details.
func TestCheckToolAvailability_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	result := v.CheckToolAvailability(context.Background(), config)

	if result.Available {
		t.Fatal("expected available=false for HTTP 500")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error for HTTP 500")
	}
}

// TestCheckToolAvailability_Timeout verifies that the 10s timeout is enforced
// when the server is slow to respond.
func TestCheckToolAvailability_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow server that exceeds the 10s timeout.
		time.Sleep(12 * time.Second)
	}))
	defer server.Close()

	// Use a short parent context to avoid waiting the full 12s in tests.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	result := v.CheckToolAvailability(ctx, config)

	if result.Available {
		t.Fatal("expected available=false for timeout")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error for timeout")
	}
}

// TestCheckToolAvailability_ConnectionRefused verifies behavior when the
// server is unreachable (connection refused).
func TestCheckToolAvailability_ConnectionRefused(t *testing.T) {
	v := NewValidator()
	// Use a port that is almost certainly not listening.
	config := MCPServerConfig{EndpointURL: "http://127.0.0.1:1", Transport: "streamable-http"}
	result := v.CheckToolAvailability(context.Background(), config)

	if result.Available {
		t.Fatal("expected available=false for connection refused")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error for connection refused")
	}
}

// TestCheckToolAvailability_SingleTool verifies correct parsing when the
// server exposes exactly one tool.
func TestCheckToolAvailability_SingleTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]any{
				"tools": []map[string]any{
					{
						"name":        "get_weather",
						"description": "Get current weather for a city",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"city": map[string]any{"type": "string"},
							},
							"required": []any{"city"},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	result := v.CheckToolAvailability(context.Background(), config)

	if !result.Available {
		t.Fatalf("expected available=true, got false: %s", result.Error)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0] != "get_weather" {
		t.Fatalf("expected tool name 'get_weather', got %q", result.Tools[0])
	}
}

// TestCheckToolAvailability_MalformedJSON verifies behavior when the server
// returns invalid JSON in the response body.
func TestCheckToolAvailability_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	result := v.CheckToolAvailability(context.Background(), config)

	if result.Available {
		t.Fatal("expected available=false for malformed JSON")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error for malformed JSON")
	}
}

// TestCheckToolAvailability_ContextAlreadyCancelled verifies that a
// pre-cancelled context results in immediate failure.
func TestCheckToolAvailability_ContextAlreadyCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called with cancelled context")
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	result := v.CheckToolAvailability(ctx, config)

	if result.Available {
		t.Fatal("expected available=false for cancelled context")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error for cancelled context")
	}
}
