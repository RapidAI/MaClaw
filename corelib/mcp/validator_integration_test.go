package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// --- Integration tests for the combined MCP validation flow ---
//
// These tests exercise the full Validate() orchestration:
//   connectivity → tool availability → schema → runtime health
//
// Requirements validated: 13.1, 13.2, 13.3, 13.4, 13.5

// mockMCPServer creates an httptest.Server that simulates an MCP server with
// configurable behavior for each of the four validation checks.
type mockMCPServerConfig struct {
	// Connectivity: if true, initialize returns success.
	ConnectivityOK bool
	// ToolAvailability: tools to return from tools/list. nil = error.
	Tools []mockTool
	// SchemaWarnings: if true, one tool has a schema with a required param
	// that has no corresponding property definition (triggers "warn").
	SchemaWarnings bool
	// RuntimeHealthOK: if true, tools/call returns success. If false, returns error.
	RuntimeHealthOK bool
	// Latency: artificial delay per request.
	Latency time.Duration
}

type mockTool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
}

func newMockMCPServer(cfg mockMCPServerConfig) *httptest.Server {
	var callCount atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg.Latency > 0 {
			time.Sleep(cfg.Latency)
		}

		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		reqID := req["id"]

		w.Header().Set("Content-Type", "application/json")

		switch method {
		case "initialize":
			if !cfg.ConnectivityOK {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`{"error": "server unavailable"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      reqID,
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]interface{}{},
					"serverInfo":      map[string]interface{}{"name": "mock-mcp", "version": "1.0.0"},
				},
			})

		case "tools/list":
			callCount.Add(1)
			if cfg.Tools == nil {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      reqID,
					"error":   map[string]interface{}{"code": -32000, "message": "tools unavailable"},
				})
				return
			}

			tools := make([]map[string]interface{}, 0, len(cfg.Tools))
			for _, t := range cfg.Tools {
				tool := map[string]interface{}{
					"name":        t.Name,
					"description": t.Description,
				}
				if t.InputSchema != nil {
					tool["inputSchema"] = t.InputSchema
				}
				tools = append(tools, tool)
			}

			// If SchemaWarnings is set, inject a tool with a broken schema.
			if cfg.SchemaWarnings {
				tools = append(tools, map[string]interface{}{
					"name":        "broken_schema_tool",
					"description": "Tool with schema warning",
					"inputSchema": map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
						"required":   []interface{}{"nonexistent_param"},
					},
				})
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      reqID,
				"result":  map[string]interface{}{"tools": tools},
			})

		case "tools/call":
			if !cfg.RuntimeHealthOK {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      reqID,
					"error":   map[string]interface{}{"code": -32000, "message": "tool execution failed"},
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      reqID,
				"result": map[string]interface{}{
					"content": []map[string]interface{}{{"type": "text", "text": "health check ok"}},
				},
			})

		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      reqID,
				"result":  map[string]interface{}{},
			})
		}
	}))
}

// defaultTools returns a standard set of tools for the mock server.
func defaultTools() []mockTool {
	return []mockTool{
		{
			Name:        "get_weather",
			Description: "Get current weather for a city",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"city"},
			},
		},
		{
			Name:        "list_files",
			Description: "List files in a directory",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

// TestIntegration_FullValidation_AllPass verifies the happy path where all four
// checks succeed and overall_status is "pass".
func TestIntegration_FullValidation_AllPass(t *testing.T) {
	server := newMockMCPServer(mockMCPServerConfig{
		ConnectivityOK:  true,
		Tools:           defaultTools(),
		SchemaWarnings:  false,
		RuntimeHealthOK: true,
	})
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}

	report, err := v.Validate(context.Background(), config)
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}

	// Overall status should be "pass".
	if report.OverallStatus != "pass" {
		t.Errorf("expected overall_status=\"pass\", got %q", report.OverallStatus)
	}

	// All four check results should be present.
	if report.Connectivity == nil {
		t.Fatal("expected connectivity result to be non-nil")
	}
	if !report.Connectivity.Connected {
		t.Error("expected connectivity.connected=true")
	}
	if report.Connectivity.LatencyMs <= 0 {
		t.Error("expected positive connectivity latency")
	}

	if report.ToolAvailability == nil {
		t.Fatal("expected tool_availability result to be non-nil")
	}
	if !report.ToolAvailability.Available {
		t.Error("expected tool_availability.available=true")
	}
	if len(report.ToolAvailability.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(report.ToolAvailability.Tools))
	}

	if report.SchemaCorrectness == nil {
		t.Fatal("expected schema_correctness result to be non-nil")
	}
	if !report.SchemaCorrectness.Valid {
		t.Errorf("expected schema_correctness.valid=true, got errors: %v", report.SchemaCorrectness.Errors)
	}

	if report.RuntimeHealth == nil {
		t.Fatal("expected runtime_health result to be non-nil")
	}
	if report.RuntimeHealth.Healthy == nil || !*report.RuntimeHealth.Healthy {
		t.Error("expected runtime_health.healthy=true")
	}

	// Duration should be positive.
	if report.DurationMs <= 0 {
		t.Error("expected positive duration_ms")
	}
}

// TestIntegration_ShortCircuit_ConnectivityFailure verifies that when the server
// is unreachable, only the connectivity result is populated and subsequent checks
// are skipped.
func TestIntegration_ShortCircuit_ConnectivityFailure(t *testing.T) {
	// Use a non-routable address to simulate unreachable server.
	v := NewValidator()
	v.Timeout = 3 * time.Second
	config := MCPServerConfig{EndpointURL: "http://192.0.2.1:19999", Transport: "streamable-http"}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	report, err := v.Validate(ctx, config)
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}

	// Overall status should be "fail".
	if report.OverallStatus != "fail" {
		t.Errorf("expected overall_status=\"fail\", got %q", report.OverallStatus)
	}

	// Connectivity result should indicate failure.
	if report.Connectivity == nil {
		t.Fatal("expected connectivity result to be non-nil")
	}
	if report.Connectivity.Connected {
		t.Error("expected connectivity.connected=false")
	}
	if report.Connectivity.Error == "" {
		t.Error("expected non-empty connectivity error")
	}

	// Subsequent checks should be nil (short-circuited).
	if report.ToolAvailability != nil {
		t.Error("expected tool_availability to be nil (short-circuited)")
	}
	if report.SchemaCorrectness != nil {
		t.Error("expected schema_correctness to be nil (short-circuited)")
	}
	if report.RuntimeHealth != nil {
		t.Error("expected runtime_health to be nil (short-circuited)")
	}
}

// TestIntegration_ShortCircuit_ServerReturnsError verifies short-circuit when
// the server is reachable but returns an HTTP error on initialize.
func TestIntegration_ShortCircuit_ServerReturnsError(t *testing.T) {
	server := newMockMCPServer(mockMCPServerConfig{
		ConnectivityOK: false, // initialize returns 503
	})
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}

	report, err := v.Validate(context.Background(), config)
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}

	if report.OverallStatus != "fail" {
		t.Errorf("expected overall_status=\"fail\", got %q", report.OverallStatus)
	}
	if report.Connectivity == nil || report.Connectivity.Connected {
		t.Error("expected connectivity failure")
	}
	if report.ToolAvailability != nil {
		t.Error("expected tool_availability to be nil (short-circuited)")
	}
	if report.SchemaCorrectness != nil {
		t.Error("expected schema_correctness to be nil (short-circuited)")
	}
	if report.RuntimeHealth != nil {
		t.Error("expected runtime_health to be nil (short-circuited)")
	}
}

// TestIntegration_OverallStatus_SchemaWarnings verifies that schema warnings
// result in overall_status="warn" while other checks pass.
func TestIntegration_OverallStatus_SchemaWarnings(t *testing.T) {
	server := newMockMCPServer(mockMCPServerConfig{
		ConnectivityOK:  true,
		Tools:           defaultTools(),
		SchemaWarnings:  true, // injects a tool with broken schema
		RuntimeHealthOK: true,
	})
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}

	report, err := v.Validate(context.Background(), config)
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}

	// Schema has warnings → overall_status should be "warn".
	if report.OverallStatus != "warn" {
		t.Errorf("expected overall_status=\"warn\", got %q", report.OverallStatus)
	}

	// Connectivity and tool availability should pass.
	if report.Connectivity == nil || !report.Connectivity.Connected {
		t.Error("expected connectivity success")
	}
	if report.ToolAvailability == nil || !report.ToolAvailability.Available {
		t.Error("expected tool availability success")
	}

	// Schema correctness should report invalid.
	if report.SchemaCorrectness == nil {
		t.Fatal("expected schema_correctness result to be non-nil")
	}
	if report.SchemaCorrectness.Valid {
		t.Error("expected schema_correctness.valid=false due to warnings")
	}
	if len(report.SchemaCorrectness.Errors) == 0 {
		t.Error("expected at least one schema error")
	}

	// Runtime health should still pass.
	if report.RuntimeHealth == nil || report.RuntimeHealth.Healthy == nil || !*report.RuntimeHealth.Healthy {
		t.Error("expected runtime_health.healthy=true")
	}
}

// TestIntegration_OverallStatus_RuntimeHealthFailure verifies that runtime health
// failure results in overall_status="warn".
func TestIntegration_OverallStatus_RuntimeHealthFailure(t *testing.T) {
	server := newMockMCPServer(mockMCPServerConfig{
		ConnectivityOK:  true,
		Tools:           defaultTools(),
		SchemaWarnings:  false,
		RuntimeHealthOK: false, // tools/call returns error
	})
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}

	report, err := v.Validate(context.Background(), config)
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}

	// Runtime health failure → overall_status should be "warn".
	if report.OverallStatus != "warn" {
		t.Errorf("expected overall_status=\"warn\", got %q", report.OverallStatus)
	}

	if report.RuntimeHealth == nil {
		t.Fatal("expected runtime_health result to be non-nil")
	}
	if report.RuntimeHealth.Healthy == nil || *report.RuntimeHealth.Healthy {
		t.Error("expected runtime_health.healthy=false")
	}
	if report.RuntimeHealth.Error == "" {
		t.Error("expected non-empty runtime health error")
	}
}

// TestIntegration_OverallStatus_ToolAvailabilityFailure verifies that tool
// availability failure results in overall_status="fail".
func TestIntegration_OverallStatus_ToolAvailabilityFailure(t *testing.T) {
	server := newMockMCPServer(mockMCPServerConfig{
		ConnectivityOK:  true,
		Tools:           nil, // tools/list returns error
		RuntimeHealthOK: true,
	})
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}

	report, err := v.Validate(context.Background(), config)
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}

	if report.OverallStatus != "fail" {
		t.Errorf("expected overall_status=\"fail\", got %q", report.OverallStatus)
	}

	if report.ToolAvailability == nil {
		t.Fatal("expected tool_availability result to be non-nil")
	}
	if report.ToolAvailability.Available {
		t.Error("expected tool_availability.available=false")
	}
}

// TestIntegration_ReportIncludesAllFourResults verifies that when all checks
// succeed, the report contains non-nil results for all four checks.
func TestIntegration_ReportIncludesAllFourResults(t *testing.T) {
	server := newMockMCPServer(mockMCPServerConfig{
		ConnectivityOK:  true,
		Tools:           defaultTools(),
		SchemaWarnings:  false,
		RuntimeHealthOK: true,
	})
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}

	report, err := v.Validate(context.Background(), config)
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}

	// Verify all four results are present.
	if report.Connectivity == nil {
		t.Error("connectivity result is nil")
	}
	if report.ToolAvailability == nil {
		t.Error("tool_availability result is nil")
	}
	if report.SchemaCorrectness == nil {
		t.Error("schema_correctness result is nil")
	}
	if report.RuntimeHealth == nil {
		t.Error("runtime_health result is nil")
	}

	// Verify the report has a valid timestamp.
	if report.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}

	// Verify duration is reasonable (should be < 5s for local httptest).
	if report.DurationMs <= 0 || report.DurationMs > 5000 {
		t.Errorf("unexpected duration_ms: %d", report.DurationMs)
	}
}

// TestIntegration_Timeout_EnforcedAt30s verifies that the validator enforces
// the 30s total timeout across all checks.
func TestIntegration_Timeout_EnforcedAt30s(t *testing.T) {
	// Create a server that responds slowly (simulating a slow MCP server).
	server := newMockMCPServer(mockMCPServerConfig{
		ConnectivityOK:  true,
		Tools:           defaultTools(),
		RuntimeHealthOK: true,
		Latency:         5 * time.Second, // each request takes 5s
	})
	defer server.Close()

	// Use a short timeout to verify the mechanism without waiting 30s.
	v := NewValidator()
	v.Timeout = 3 * time.Second // override to 3s for test speed
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}

	start := time.Now()
	report, err := v.Validate(context.Background(), config)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}

	// Should complete within roughly the timeout period.
	if elapsed > 10*time.Second {
		t.Errorf("validation took too long: %v (expected ~3s timeout)", elapsed)
	}

	// The report should still have an overall status (not an error return).
	if report.OverallStatus == "" {
		t.Error("expected non-empty overall_status even on timeout")
	}
}

// TestIntegration_DefaultTimeout_Is30s verifies that NewValidator creates a
// validator with the default 30s timeout.
func TestIntegration_DefaultTimeout_Is30s(t *testing.T) {
	v := NewValidator()
	if v.Timeout != 30*time.Second {
		t.Errorf("expected default timeout=30s, got %v", v.Timeout)
	}
}

// TestIntegration_OverallStatus_AllPassWithToolWarning verifies that a tool
// availability warning (empty tool list) results in overall_status="warn".
func TestIntegration_OverallStatus_AllPassWithToolWarning(t *testing.T) {
	server := newMockMCPServer(mockMCPServerConfig{
		ConnectivityOK:  true,
		Tools:           []mockTool{}, // empty list triggers warning
		RuntimeHealthOK: true,
	})
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}

	report, err := v.Validate(context.Background(), config)
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}

	// Empty tool list → warning → overall_status="warn".
	if report.OverallStatus != "warn" {
		t.Errorf("expected overall_status=\"warn\", got %q", report.OverallStatus)
	}

	if report.ToolAvailability == nil {
		t.Fatal("expected tool_availability to be non-nil")
	}
	if !report.ToolAvailability.Available {
		t.Error("expected tool_availability.available=true (server responded)")
	}
	if report.ToolAvailability.Warning == "" {
		t.Error("expected non-empty warning for empty tool list")
	}
}

// TestIntegration_ConnectivityFail_OverallStatusIsFail verifies the explicit
// mapping: connectivity fails → overall_status="fail".
func TestIntegration_ConnectivityFail_OverallStatusIsFail(t *testing.T) {
	server := newMockMCPServer(mockMCPServerConfig{
		ConnectivityOK: false,
	})
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}

	report, err := v.Validate(context.Background(), config)
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}

	if report.OverallStatus != "fail" {
		t.Errorf("expected overall_status=\"fail\", got %q", report.OverallStatus)
	}
}
