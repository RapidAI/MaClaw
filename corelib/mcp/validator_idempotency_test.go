package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pgregory.net/rapid"
)

// TestProperty_ValidationIdempotency verifies that for the same MCP Server with
// unchanged state, two consecutive calls to Validate() return the same
// overall_status value.
//
// The mock server is deterministic: it always returns the same tools and
// responses for the same requests. The property holds that validation results
// are stable when the server state does not change between calls.
//
// Requirements validated: 13.1
func TestProperty_ValidationIdempotency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Draw a random but deterministic server configuration.
		numTools := rapid.IntRange(0, 5).Draw(t, "numTools")
		connectivityOK := rapid.Bool().Draw(t, "connectivityOK")
		toolsListOK := rapid.Bool().Draw(t, "toolsListOK")
		runtimeHealthOK := rapid.Bool().Draw(t, "runtimeHealthOK")

		// Build deterministic tool definitions.
		tools := make([]map[string]any, numTools)
		for i := 0; i < numTools; i++ {
			toolName := rapid.StringMatching(`[a-z][a-z0-9_]{2,10}`).Draw(t, "toolName")
			tools[i] = map[string]any{
				"name":        toolName,
				"description": "Tool " + toolName,
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"input": map[string]any{"type": "string"}},
				},
			}
		}

		// Create a deterministic mock MCP server.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			method, _ := req["method"].(string)

			w.Header().Set("Content-Type", "application/json")

			switch method {
			case "initialize":
				if !connectivityOK {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0", "id": req["id"],
					"result": map[string]any{
						"protocolVersion": "2024-11-05",
						"capabilities":    map[string]any{},
						"serverInfo":      map[string]any{"name": "test-server", "version": "1.0.0"},
					},
				})
			case "tools/list":
				if !toolsListOK {
					json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0", "id": req["id"],
						"error":  map[string]any{"code": -32603, "message": "internal error"},
					})
					return
				}
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0", "id": req["id"],
					"result": map[string]any{"tools": tools},
				})
			case "tools/call":
				if !runtimeHealthOK {
					json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0", "id": req["id"],
						"error":  map[string]any{"code": -32603, "message": "tool execution failed"},
					})
					return
				}
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0", "id": req["id"],
					"result": map[string]any{
						"content": []map[string]any{{"type": "text", "text": "ok"}},
					},
				})
			default:
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0", "id": req["id"],
					"result": map[string]any{},
				})
			}
		}))
		defer server.Close()

		v := NewValidator()
		config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}

		// First call.
		report1, err1 := v.Validate(context.Background(), config)
		if err1 != nil {
			t.Fatalf("first Validate() returned unexpected error: %v", err1)
		}

		// Second call with identical config and unchanged server state.
		report2, err2 := v.Validate(context.Background(), config)
		if err2 != nil {
			t.Fatalf("second Validate() returned unexpected error: %v", err2)
		}

		// Property: both calls must return the same overall_status.
		if report1.OverallStatus != report2.OverallStatus {
			t.Fatalf("idempotency violated: first call returned overall_status=%q, second call returned overall_status=%q",
				report1.OverallStatus, report2.OverallStatus)
		}
	})
}
