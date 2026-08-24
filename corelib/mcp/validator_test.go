package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckConnectivity_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "test-server", "version": "1.0"},
			},
		})
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	result := v.CheckConnectivity(context.Background(), config)

	if !result.Connected {
		t.Fatalf("expected connected=true, got false: %s", result.Error)
	}
	if result.LatencyMs <= 0 {
		t.Fatalf("expected positive latency, got %d", result.LatencyMs)
	}
}

func TestCheckConnectivity_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(12 * time.Second) // exceeds 10s timeout
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	result := v.CheckConnectivity(ctx, config)

	if result.Connected {
		t.Fatal("expected connected=false for timeout")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error for timeout")
	}
}

func TestCheckConnectivity_InvalidURL(t *testing.T) {
	v := NewValidator()
	config := MCPServerConfig{EndpointURL: "not-a-valid-url", Transport: "streamable-http"}
	result := v.CheckConnectivity(context.Background(), config)

	if result.Connected {
		t.Fatal("expected connected=false for invalid URL")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error for invalid URL")
	}
}

func TestCheckToolAvailability_WithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"tools": []map[string]any{
					{"name": "get_weather", "description": "Get weather info"},
					{"name": "search", "description": "Search the web"},
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
	if len(result.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result.Tools))
	}
	if result.Tools[0] != "get_weather" || result.Tools[1] != "search" {
		t.Fatalf("unexpected tool names: %v", result.Tools)
	}
}

func TestCheckToolAvailability_EmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"tools": []any{}},
		})
	}))
	defer server.Close()

	v := NewValidator()
	config := MCPServerConfig{EndpointURL: server.URL, Transport: "streamable-http"}
	result := v.CheckToolAvailability(context.Background(), config)

	if !result.Available {
		t.Fatal("expected available=true for empty list")
	}
	if result.Warning == "" {
		t.Fatal("expected warning for empty tool list")
	}
}

func TestCheckSchemaCorrectness_Valid(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "get_weather",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"city"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if !result.Valid {
		t.Fatalf("expected valid=true, got errors: %v", result.Errors)
	}
}

func TestCheckSchemaCorrectness_MissingProperty(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "broken_tool",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []interface{}{"missing_param"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if result.Valid {
		t.Fatal("expected valid=false for missing property")
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected at least one error")
	}
	if result.Errors[0].ToolName != "broken_tool" {
		t.Fatalf("expected tool_name=broken_tool, got %s", result.Errors[0].ToolName)
	}
}

func TestCheckRuntimeHealth_NoTools(t *testing.T) {
	v := NewValidator()
	result := v.CheckRuntimeHealth(context.Background(), MCPServerConfig{}, nil)

	if result.Healthy != nil {
		t.Fatal("expected healthy=nil when no tools available")
	}
	if result.Note == "" {
		t.Fatal("expected note explaining why health check was skipped")
	}
}

func TestSelectSafeHealthCheckTool_PrefersNoRequired(t *testing.T) {
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
			ToolName:    "simple_tool",
			InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}

	selected := selectSafeHealthCheckTool(tools)
	if selected == nil {
		t.Fatal("expected a tool to be selected")
	}
	if selected.ToolName != "simple_tool" {
		t.Fatalf("expected simple_tool (no required), got %s", selected.ToolName)
	}
}

func TestValidate_ShortCircuitsOnConnectivityFailure(t *testing.T) {
	v := NewValidator()
	v.Timeout = 5 * time.Second
	config := MCPServerConfig{EndpointURL: "http://192.0.2.1:9999", Transport: "streamable-http"} // non-routable

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	report, err := v.Validate(ctx, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.OverallStatus != "fail" {
		t.Fatalf("expected overall_status=fail, got %s", report.OverallStatus)
	}
	if report.Connectivity == nil || report.Connectivity.Connected {
		t.Fatal("expected connectivity failure")
	}
	if report.ToolAvailability != nil {
		t.Fatal("expected tool_availability to be nil (short-circuited)")
	}
}

func TestValidate_FullPass(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)

		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "initialize":
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": req["id"],
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"serverInfo":      map[string]any{"name": "test", "version": "1.0"},
				},
			})
		case "tools/list":
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": req["id"],
				"result": map[string]any{
					"tools": []map[string]any{
						{
							"name":        "echo",
							"description": "Echo input",
							"inputSchema": map[string]any{
								"type":       "object",
								"properties": map[string]any{"text": map[string]any{"type": "string"}},
							},
						},
					},
				},
			})
		case "tools/call":
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

	report, err := v.Validate(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.OverallStatus != "pass" {
		t.Fatalf("expected overall_status=pass, got %s", report.OverallStatus)
	}
	if report.Connectivity == nil || !report.Connectivity.Connected {
		t.Fatal("expected connectivity success")
	}
	if report.ToolAvailability == nil || !report.ToolAvailability.Available {
		t.Fatal("expected tool availability success")
	}
	if report.SchemaCorrectness == nil || !report.SchemaCorrectness.Valid {
		t.Fatal("expected schema correctness valid")
	}
	if report.RuntimeHealth == nil || report.RuntimeHealth.Healthy == nil || !*report.RuntimeHealth.Healthy {
		t.Fatal("expected runtime health pass")
	}
	if report.DurationMs <= 0 {
		t.Fatal("expected positive duration")
	}
}

func TestValidateArgsTreatsBlankRequiredStringAsMissing(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"query"},
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
	}
	for _, args := range []map[string]interface{}{
		{},
		{"query": ""},
		{"query": "   "},
		{"query": nil},
	} {
		errs := ValidateArgs(schema, args)
		if len(errs) != 1 || errs[0].Code != "missing_required" || errs[0].Param != "query" {
			t.Fatalf("args=%#v: expected missing query, got %#v", args, errs)
		}
	}
	if errs := ValidateArgs(schema, map[string]interface{}{"query": "王展毅"}); len(errs) != 0 {
		t.Fatalf("non-empty query should pass, got %#v", errs)
	}
}

func TestConstructSampleArgs_RoundTrip(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":  map[string]interface{}{"type": "string"},
			"count": map[string]interface{}{"type": "integer"},
			"flag":  map[string]interface{}{"type": "boolean"},
		},
		"required": []interface{}{"name", "count"},
	}

	args := constructSampleArgs(schema)
	errs := ValidateArgs(schema, args)
	if len(errs) > 0 {
		t.Fatalf("round-trip validation failed: %v", errs)
	}
}

func TestComputeOverallStatus(t *testing.T) {
	tests := []struct {
		name     string
		report   *ValidationReport
		expected string
	}{
		{
			name:     "connectivity_fail",
			report:   &ValidationReport{Connectivity: &ConnectivityResult{Connected: false}},
			expected: "fail",
		},
		{
			name:     "tools_unavailable",
			report:   &ValidationReport{Connectivity: &ConnectivityResult{Connected: true}, ToolAvailability: &ToolAvailabilityResult{Available: false}},
			expected: "fail",
		},
		{
			name:     "schema_invalid",
			report:   &ValidationReport{Connectivity: &ConnectivityResult{Connected: true}, ToolAvailability: &ToolAvailabilityResult{Available: true}, SchemaCorrectness: &SchemaCorrectnessResult{Valid: false}},
			expected: "warn",
		},
		{
			name: "all_pass",
			report: &ValidationReport{
				Connectivity:      &ConnectivityResult{Connected: true},
				ToolAvailability:  &ToolAvailabilityResult{Available: true},
				SchemaCorrectness: &SchemaCorrectnessResult{Valid: true},
				RuntimeHealth:     &RuntimeHealthResult{Healthy: boolPtr(true)},
			},
			expected: "pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeOverallStatus(tt.report)
			if got != tt.expected {
				t.Fatalf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }
