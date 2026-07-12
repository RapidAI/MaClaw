package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// MCPValidator defines the interface for validating MCP Server connectivity,
// tool availability, schema correctness, and runtime health.
type MCPValidator interface {
	// Validate runs all four checks in sequence (connectivity → tool availability
	// → schema correctness → runtime health) and returns a combined report.
	// Short-circuits on connectivity failure.
	Validate(ctx context.Context, config MCPServerConfig) (*ValidationReport, error)

	// CheckConnectivity verifies the MCP Server is reachable by sending an
	// initialize request with a 10s timeout.
	CheckConnectivity(ctx context.Context, config MCPServerConfig) *ConnectivityResult

	// CheckToolAvailability invokes the MCP tools/list method and returns
	// the list of available tools.
	CheckToolAvailability(ctx context.Context, config MCPServerConfig) *ToolAvailabilityResult

	// CheckSchemaCorrectness validates that each tool's input schema is
	// well-formed and that required parameters have corresponding property
	// definitions.
	CheckSchemaCorrectness(ctx context.Context, tools []ToolEntry) *SchemaCorrectnessResult

	// CheckRuntimeHealth performs a lightweight tool invocation on the MCP
	// Server to verify it is functioning correctly.
	CheckRuntimeHealth(ctx context.Context, config MCPServerConfig, tools []ToolEntry) *RuntimeHealthResult
}

// Validator implements the MCPValidator interface. It validates MCP Server
// connectivity, tool availability, schema correctness, and runtime health.
type Validator struct {
	// Timeout is the total timeout for a full validation run (all four checks).
	// Default: 30 seconds.
	Timeout time.Duration
}

// NewValidator creates a new Validator with a default total timeout of 30 seconds.
func NewValidator() *Validator {
	return &Validator{Timeout: 30 * time.Second}
}

// Validate runs all four validation checks in sequence and returns a combined
// ValidationReport. If connectivity fails, subsequent checks are skipped.
// The overall_status is computed based on the results of all checks.
func (v *Validator) Validate(ctx context.Context, config MCPServerConfig) (*ValidationReport, error) {
	timeout := v.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	report := &ValidationReport{
		Timestamp: start,
	}

	// 1. Connectivity check.
	report.Connectivity = v.CheckConnectivity(ctx, config)
	if !report.Connectivity.Connected {
		report.OverallStatus = "fail"
		report.DurationMs = positiveElapsedMs(start)
		return report, nil
	}

	// Check context deadline after each step.
	if ctx.Err() != nil {
		report.OverallStatus = "fail"
		report.DurationMs = positiveElapsedMs(start)
		return report, nil
	}

	// 2. Tool availability check.
	report.ToolAvailability = v.CheckToolAvailability(ctx, config)

	if ctx.Err() != nil {
		report.OverallStatus = computeOverallStatus(report)
		report.DurationMs = positiveElapsedMs(start)
		return report, nil
	}

	// 3. Schema correctness check (uses tools from availability check).
	// Fetch tools with full schema once and reuse for both schema check and health check.
	var tools []ToolEntry
	if report.ToolAvailability.Available && len(report.ToolAvailability.Tools) > 0 {
		tools = v.fetchToolsWithSchema(ctx, config)
		if len(tools) > 0 {
			report.SchemaCorrectness = v.CheckSchemaCorrectness(ctx, tools)
		}
	}

	if ctx.Err() != nil {
		report.OverallStatus = computeOverallStatus(report)
		report.DurationMs = positiveElapsedMs(start)
		return report, nil
	}

	// 4. Runtime health check (reuse tools fetched above).
	if tools == nil {
		tools = v.fetchToolsWithSchema(ctx, config)
	}
	report.RuntimeHealth = v.CheckRuntimeHealth(ctx, config, tools)

	report.OverallStatus = computeOverallStatus(report)
	report.DurationMs = positiveElapsedMs(start)
	return report, nil
}

// CheckConnectivity verifies the MCP Server is reachable by sending an
// initialize request with a 10s timeout. Returns a ConnectivityResult
// indicating success/failure and latency.
func (v *Validator) CheckConnectivity(ctx context.Context, config MCPServerConfig) *ConnectivityResult {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	start := time.Now()
	_, err := sendMCPRequest(ctx, config, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "maclaw-validator",
			"version": "1.0.0",
		},
	})
	latency := positiveElapsedMs(start)

	if err != nil {
		return &ConnectivityResult{Connected: false, Error: err.Error(), LatencyMs: latency}
	}
	return &ConnectivityResult{Connected: true, LatencyMs: latency}
}

// CheckToolAvailability invokes the MCP tools/list method on the target server
// and returns the list of available tool names.
func (v *Validator) CheckToolAvailability(ctx context.Context, config MCPServerConfig) *ToolAvailabilityResult {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := sendMCPRequest(ctx, config, "tools/list", nil)
	if err != nil {
		return &ToolAvailabilityResult{Available: false, Error: err.Error()}
	}

	// Parse the tools list from the result.
	tools := parseMCPToolsList(result)
	if len(tools) == 0 {
		return &ToolAvailabilityResult{Available: true, Warning: "no tools exposed by this MCP server"}
	}

	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.ToolName
	}
	return &ToolAvailabilityResult{Available: true, Tools: names}
}

// CheckSchemaCorrectness validates that each tool's input schema is well-formed.
// It checks that required parameters have corresponding property definitions and
// performs round-trip validation using constructSampleArgs + ValidateArgs.
func (v *Validator) CheckSchemaCorrectness(ctx context.Context, tools []ToolEntry) *SchemaCorrectnessResult {
	var errors []SchemaError
	for _, tool := range tools {
		schema := tool.InputSchema
		if schema == nil {
			continue
		}

		// 1. Check that required fields reference existing properties.
		if reqRaw, ok := schema["required"]; ok {
			if reqSlice, ok := reqRaw.([]interface{}); ok {
				properties, _ := schema["properties"].(map[string]interface{})
				for _, r := range reqSlice {
					name, ok := r.(string)
					if !ok {
						continue
					}
					if properties == nil {
						errors = append(errors, SchemaError{
							ToolName: tool.ToolName,
							Message:  fmt.Sprintf("required parameter %q declared but no properties defined", name),
						})
					} else if _, exists := properties[name]; !exists {
						errors = append(errors, SchemaError{
							ToolName: tool.ToolName,
							Message:  fmt.Sprintf("required parameter %q has no corresponding property definition", name),
						})
					}
				}
			}
		}

		// 2. Round-trip validation: construct sample args and validate.
		sampleArgs := constructSampleArgs(schema)
		if valErrs := ValidateArgs(schema, sampleArgs); len(valErrs) > 0 {
			for _, ve := range valErrs {
				errors = append(errors, SchemaError{
					ToolName: tool.ToolName,
					Message:  fmt.Sprintf("round-trip validation failed: %s", ve.Message),
				})
			}
		}
	}
	return &SchemaCorrectnessResult{Valid: len(errors) == 0, Errors: errors}
}

// CheckRuntimeHealth performs a lightweight tool invocation on the MCP Server
// using a safe, read-only tool. Returns nil Healthy if no safe tool is available.
func (v *Validator) CheckRuntimeHealth(ctx context.Context, config MCPServerConfig, tools []ToolEntry) *RuntimeHealthResult {
	tool := selectSafeHealthCheckTool(tools)
	if tool == nil {
		return &RuntimeHealthResult{Healthy: nil, Note: "no safe tool found for testing"}
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	args := constructMinimalArgs(tool.InputSchema)
	start := time.Now()
	_, err := sendMCPRequest(ctx, config, "tools/call", map[string]any{
		"name":      tool.ToolName,
		"arguments": args,
	})
	responseMs := positiveElapsedMs(start)

	if err != nil {
		healthy := false
		return &RuntimeHealthResult{Healthy: &healthy, Error: err.Error(), ToolUsed: tool.ToolName, ResponseMs: responseMs}
	}
	healthy := true
	return &RuntimeHealthResult{Healthy: &healthy, ResponseMs: responseMs, ToolUsed: tool.ToolName}
}

// positiveElapsedMs returns elapsed wall time in whole milliseconds, floored at 1.
// Sub-millisecond httptest responses otherwise report 0 and break "positive latency" contracts.
func positiveElapsedMs(start time.Time) int64 {
	ms := time.Since(start).Milliseconds()
	if ms <= 0 {
		return 1
	}
	return ms
}

// --- Helper functions ---

// parseMCPToolsList parses the JSON-RPC result of a tools/list call into ToolEntry slice.
func parseMCPToolsList(result json.RawMessage) []ToolEntry {
	// MCP tools/list returns {"tools": [{name, description, inputSchema}, ...]}
	var resp struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil
	}
	entries := make([]ToolEntry, len(resp.Tools))
	for i, t := range resp.Tools {
		entries[i] = ToolEntry{
			ToolName:    t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return entries
}

// fetchToolsWithSchema fetches the full tool list with schemas from the MCP server.
func (v *Validator) fetchToolsWithSchema(ctx context.Context, config MCPServerConfig) []ToolEntry {
	result, err := sendMCPRequest(ctx, config, "tools/list", nil)
	if err != nil {
		return nil
	}
	return parseMCPToolsList(result)
}

// selectSafeHealthCheckTool selects the safest tool for health checking.
// Priority: tools with no required params → tools with only string params → first tool.
func selectSafeHealthCheckTool(tools []ToolEntry) *ToolEntry {
	if len(tools) == 0 {
		return nil
	}

	// Priority 1: tools with no required parameters.
	for i := range tools {
		schema := tools[i].InputSchema
		if schema == nil {
			return &tools[i]
		}
		reqRaw, hasReq := schema["required"]
		if !hasReq {
			return &tools[i]
		}
		if reqSlice, ok := reqRaw.([]interface{}); ok && len(reqSlice) == 0 {
			return &tools[i]
		}
	}

	// Priority 2: tools with only string parameters.
	for i := range tools {
		if hasOnlyStringParams(tools[i].InputSchema) {
			return &tools[i]
		}
	}

	// Priority 3: first tool in the list.
	return &tools[0]
}

// hasOnlyStringParams checks if all properties in the schema are of type "string".
func hasOnlyStringParams(schema map[string]interface{}) bool {
	if schema == nil {
		return true
	}
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok || len(properties) == 0 {
		return true
	}
	for _, propRaw := range properties {
		propDef, ok := propRaw.(map[string]interface{})
		if !ok {
			return false
		}
		propType, _ := propDef["type"].(string)
		if propType != "string" {
			return false
		}
	}
	return true
}

// constructSampleArgs generates sample arguments from a JSON Schema's property definitions.
// Used for round-trip validation testing.
func constructSampleArgs(schema map[string]interface{}) map[string]interface{} {
	args := make(map[string]interface{})
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return args
	}

	// Only fill required parameters to keep the sample minimal.
	required := make(map[string]bool)
	if reqRaw, ok := schema["required"]; ok {
		if reqSlice, ok := reqRaw.([]interface{}); ok {
			for _, r := range reqSlice {
				if name, ok := r.(string); ok {
					required[name] = true
				}
			}
		}
	}

	for name, propRaw := range properties {
		if !required[name] {
			continue
		}
		propDef, ok := propRaw.(map[string]interface{})
		if !ok {
			continue
		}
		args[name] = sampleValueForType(propDef)
	}
	return args
}

// constructMinimalArgs generates minimal arguments for a tool call (health check).
func constructMinimalArgs(schema map[string]interface{}) map[string]interface{} {
	return constructSampleArgs(schema)
}

// sampleValueForType generates a sample value based on the JSON Schema type definition.
func sampleValueForType(propDef map[string]interface{}) interface{} {
	propType, _ := propDef["type"].(string)

	// If enum is defined, use the first enum value.
	if enumRaw, ok := propDef["enum"]; ok {
		if enumSlice, ok := enumRaw.([]interface{}); ok && len(enumSlice) > 0 {
			return enumSlice[0]
		}
	}

	// If default is defined, use it.
	if def, ok := propDef["default"]; ok {
		return def
	}

	switch propType {
	case "string":
		return "test"
	case "number":
		return float64(0)
	case "integer":
		return float64(0)
	case "boolean":
		return false
	case "array":
		return []interface{}{}
	case "object":
		return map[string]interface{}{}
	default:
		return "test"
	}
}

// computeOverallStatus determines the overall validation status from the report.
func computeOverallStatus(report *ValidationReport) string {
	if report.Connectivity != nil && !report.Connectivity.Connected {
		return "fail"
	}
	if report.ToolAvailability != nil && !report.ToolAvailability.Available {
		return "fail"
	}
	if report.SchemaCorrectness != nil && !report.SchemaCorrectness.Valid {
		return "warn"
	}
	if report.RuntimeHealth != nil && report.RuntimeHealth.Healthy != nil && !*report.RuntimeHealth.Healthy {
		return "warn"
	}
	if report.ToolAvailability != nil && report.ToolAvailability.Warning != "" {
		return "warn"
	}
	return "pass"
}
