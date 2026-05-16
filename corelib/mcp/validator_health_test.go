package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- selectSafeHealthCheckTool priority tests ---

func TestSelectSafeHealthCheckTool_EmptyList(t *testing.T) {
	got := selectSafeHealthCheckTool(nil)
	if got != nil {
		t.Fatal("expected nil for empty tool list")
	}

	got = selectSafeHealthCheckTool([]ToolEntry{})
	if got != nil {
		t.Fatal("expected nil for zero-length tool list")
	}
}

func TestSelectSafeHealthCheckTool_Priority1_NoRequiredParams(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "string_only_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"query"},
			},
		},
		{
			ToolName: "no_required_tool",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"optional": map[string]interface{}{"type": "string"}},
			},
		},
		{
			ToolName: "first_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"data": map[string]interface{}{"type": "object"},
				},
				"required": []interface{}{"data"},
			},
		},
	}

	selected := selectSafeHealthCheckTool(tools)
	if selected == nil {
		t.Fatal("expected a tool to be selected")
	}
	if selected.ToolName != "no_required_tool" {
		t.Fatalf("expected no_required_tool (priority 1), got %s", selected.ToolName)
	}
}

func TestSelectSafeHealthCheckTool_Priority1_NilSchema(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "complex_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"data": map[string]interface{}{"type": "object"},
				},
				"required": []interface{}{"data"},
			},
		},
		{
			ToolName:    "nil_schema_tool",
			InputSchema: nil,
		},
	}

	selected := selectSafeHealthCheckTool(tools)
	if selected == nil {
		t.Fatal("expected a tool to be selected")
	}
	if selected.ToolName != "nil_schema_tool" {
		t.Fatalf("expected nil_schema_tool (nil schema = no required), got %s", selected.ToolName)
	}
}

func TestSelectSafeHealthCheckTool_Priority1_EmptyRequiredArray(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "complex_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"data": map[string]interface{}{"type": "object"},
				},
				"required": []interface{}{"data"},
			},
		},
		{
			ToolName: "empty_required_tool",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"x": map[string]interface{}{"type": "string"}},
				"required":   []interface{}{},
			},
		},
	}

	selected := selectSafeHealthCheckTool(tools)
	if selected == nil {
		t.Fatal("expected a tool to be selected")
	}
	if selected.ToolName != "empty_required_tool" {
		t.Fatalf("expected empty_required_tool (empty required array = priority 1), got %s", selected.ToolName)
	}
}

func TestSelectSafeHealthCheckTool_Priority2_StringOnlyParams(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "object_param_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"config": map[string]interface{}{"type": "object"},
				},
				"required": []interface{}{"config"},
			},
		},
		{
			ToolName: "string_param_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":  map[string]interface{}{"type": "string"},
					"query": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"name"},
			},
		},
		{
			ToolName: "mixed_param_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text":  map[string]interface{}{"type": "string"},
					"count": map[string]interface{}{"type": "integer"},
				},
				"required": []interface{}{"text", "count"},
			},
		},
	}

	selected := selectSafeHealthCheckTool(tools)
	if selected == nil {
		t.Fatal("expected a tool to be selected")
	}
	if selected.ToolName != "string_param_tool" {
		t.Fatalf("expected string_param_tool (priority 2: only string params), got %s", selected.ToolName)
	}
}

func TestSelectSafeHealthCheckTool_Priority3_FallbackToFirst(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "first_complex_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"data": map[string]interface{}{"type": "object"},
				},
				"required": []interface{}{"data"},
			},
		},
		{
			ToolName: "second_complex_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{"type": "array"},
				},
				"required": []interface{}{"items"},
			},
		},
	}

	selected := selectSafeHealthCheckTool(tools)
	if selected == nil {
		t.Fatal("expected a tool to be selected")
	}
	if selected.ToolName != "first_complex_tool" {
		t.Fatalf("expected first_complex_tool (priority 3: fallback to first), got %s", selected.ToolName)
	}
}

// --- CheckRuntimeHealth tests ---

func TestCheckRuntimeHealth_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "pong"}},
			},
		})
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	tools := []ToolEntry{
		{
			ToolName: "ping",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	result := v.CheckRuntimeHealth(context.Background(), config, tools)

	if result.Healthy == nil {
		t.Fatal("expected healthy to be non-nil")
	}
	if !*result.Healthy {
		t.Fatalf("expected healthy=true, got false: %s", result.Error)
	}
	if result.ResponseMs <= 0 {
		t.Fatalf("expected positive response time, got %d", result.ResponseMs)
	}
	if result.ToolUsed != "ping" {
		t.Fatalf("expected tool_used=ping, got %s", result.ToolUsed)
	}
	if result.Error != "" {
		t.Fatalf("expected no error, got %s", result.Error)
	}
}

func TestCheckRuntimeHealth_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow tool that exceeds the timeout.
		time.Sleep(3 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": 1,
			"result": map[string]any{"content": []map[string]any{{"type": "text", "text": "late"}}},
		})
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	tools := []ToolEntry{
		{
			ToolName: "slow_tool",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	// Use a short parent context timeout to avoid waiting 15s in tests.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	result := v.CheckRuntimeHealth(ctx, config, tools)

	if result.Healthy == nil {
		t.Fatal("expected healthy to be non-nil")
	}
	if *result.Healthy {
		t.Fatal("expected healthy=false for timeout")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error for timeout")
	}
	if result.ToolUsed != "slow_tool" {
		t.Fatalf("expected tool_used=slow_tool, got %s", result.ToolUsed)
	}
}

func TestCheckRuntimeHealth_NoSafeTool(t *testing.T) {
	v := NewValidator()
	config := MCPServerConfig{EndpointURL: "http://localhost:9999", Transport: "streamable-http"}

	// selectSafeHealthCheckTool returns nil only for empty/nil tool list.
	// Test with nil tools to trigger the "no safe tool" path.
	result := v.CheckRuntimeHealth(context.Background(), config, nil)

	if result.Healthy != nil {
		t.Fatalf("expected healthy=nil when no tools, got %v", *result.Healthy)
	}
	if result.Note == "" {
		t.Fatal("expected note explaining why health check was skipped")
	}
	if result.Note != "no safe tool found for testing" {
		t.Fatalf("unexpected note: %s", result.Note)
	}
}

func TestCheckRuntimeHealth_EmptyToolList(t *testing.T) {
	v := NewValidator()
	config := MCPServerConfig{EndpointURL: "http://localhost:9999", Transport: "streamable-http"}

	result := v.CheckRuntimeHealth(context.Background(), config, []ToolEntry{})

	if result.Healthy != nil {
		t.Fatalf("expected healthy=nil for empty tool list, got %v", *result.Healthy)
	}
	if result.Note != "no safe tool found for testing" {
		t.Fatalf("unexpected note: %s", result.Note)
	}
}

func TestCheckRuntimeHealth_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"error": map[string]any{
				"code":    -32603,
				"message": "Internal error: tool execution failed",
			},
		})
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	tools := []ToolEntry{
		{
			ToolName: "failing_tool",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	result := v.CheckRuntimeHealth(context.Background(), config, tools)

	if result.Healthy == nil {
		t.Fatal("expected healthy to be non-nil")
	}
	if *result.Healthy {
		t.Fatal("expected healthy=false for server error")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error")
	}
	if result.ToolUsed != "failing_tool" {
		t.Fatalf("expected tool_used=failing_tool, got %s", result.ToolUsed)
	}
	if result.ResponseMs <= 0 {
		t.Fatal("expected positive response time even on error")
	}
}

func TestCheckRuntimeHealth_SelectsToolByPriority(t *testing.T) {
	var calledTool string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		// Extract the tool name from the tools/call params.
		if params, ok := req["params"].(map[string]any); ok {
			if name, ok := params["name"].(string); ok {
				calledTool = name
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "ok"}},
			},
		})
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}

	// Provide tools in order: complex first, then string-only, then no-required.
	// The validator should pick the no-required tool (priority 1).
	tools := []ToolEntry{
		{
			ToolName: "complex_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"data": map[string]interface{}{"type": "object"},
				},
				"required": []interface{}{"data"},
			},
		},
		{
			ToolName: "string_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"query"},
			},
		},
		{
			ToolName: "safe_tool",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"hint": map[string]interface{}{"type": "string"}},
			},
		},
	}

	result := v.CheckRuntimeHealth(context.Background(), config, tools)

	if result.Healthy == nil || !*result.Healthy {
		t.Fatalf("expected healthy=true, got error: %s", result.Error)
	}
	if result.ToolUsed != "safe_tool" {
		t.Fatalf("expected tool_used=safe_tool (no required params), got %s", result.ToolUsed)
	}
	if calledTool != "safe_tool" {
		t.Fatalf("expected server to receive call for safe_tool, got %s", calledTool)
	}
}

func TestCheckRuntimeHealth_MeasuresResponseTime(t *testing.T) {
	const artificialDelay = 50 * time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(artificialDelay)
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "ok"}},
			},
		})
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	tools := []ToolEntry{
		{
			ToolName:    "timed_tool",
			InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}

	result := v.CheckRuntimeHealth(context.Background(), config, tools)

	if result.Healthy == nil || !*result.Healthy {
		t.Fatalf("expected healthy=true, got error: %s", result.Error)
	}
	// Response time should be at least the artificial delay.
	if result.ResponseMs < artificialDelay.Milliseconds() {
		t.Fatalf("expected response_ms >= %d, got %d", artificialDelay.Milliseconds(), result.ResponseMs)
	}
}

// --- hasOnlyStringParams tests ---

func TestHasOnlyStringParams(t *testing.T) {
	tests := []struct {
		name     string
		schema   map[string]interface{}
		expected bool
	}{
		{
			name:     "nil_schema",
			schema:   nil,
			expected: true,
		},
		{
			name:     "empty_properties",
			schema:   map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			expected: true,
		},
		{
			name: "all_strings",
			schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":  map[string]interface{}{"type": "string"},
					"query": map[string]interface{}{"type": "string"},
				},
			},
			expected: true,
		},
		{
			name: "mixed_types",
			schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":  map[string]interface{}{"type": "string"},
					"count": map[string]interface{}{"type": "integer"},
				},
			},
			expected: false,
		},
		{
			name: "object_type",
			schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"config": map[string]interface{}{"type": "object"},
				},
			},
			expected: false,
		},
		{
			name: "array_type",
			schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{"type": "array"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasOnlyStringParams(tt.schema)
			if got != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}
