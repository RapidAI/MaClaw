package mcp

import "time"

// ValidationReport is the combined result of all MCP Server validation checks.
// It contains connectivity, tool availability, schema correctness, and runtime
// health results, along with an overall status summary.
type ValidationReport struct {
	Connectivity      *ConnectivityResult      `json:"connectivity"`
	ToolAvailability  *ToolAvailabilityResult  `json:"tool_availability,omitempty"`
	SchemaCorrectness *SchemaCorrectnessResult `json:"schema_correctness,omitempty"`
	RuntimeHealth     *RuntimeHealthResult     `json:"runtime_health,omitempty"`
	OverallStatus     string                   `json:"overall_status"` // "pass" | "warn" | "fail"
	DurationMs        int64                    `json:"duration_ms"`
	Timestamp         time.Time                `json:"timestamp"`
}

// ConnectivityResult holds the result of an MCP Server connectivity check.
type ConnectivityResult struct {
	Connected bool   `json:"connected"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ToolAvailabilityResult holds the result of an MCP Server tool availability check.
type ToolAvailabilityResult struct {
	Available bool     `json:"available"`
	Tools     []string `json:"tools,omitempty"`
	Warning   string   `json:"warning,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// SchemaCorrectnessResult holds the result of validating all tool schemas
// exposed by an MCP Server.
type SchemaCorrectnessResult struct {
	Valid  bool          `json:"valid"`
	Errors []SchemaError `json:"errors,omitempty"`
}

// SchemaError describes a schema validation error for a specific tool.
type SchemaError struct {
	ToolName string `json:"tool_name"`
	Message  string `json:"message"`
}

// RuntimeHealthResult holds the result of a runtime health check performed
// by invoking a safe tool on the MCP Server. A nil Healthy pointer indicates
// the check was skipped (e.g., no safe tool available).
type RuntimeHealthResult struct {
	Healthy    *bool  `json:"healthy"`              // nil = skipped
	ResponseMs int64  `json:"response_ms,omitempty"`
	ToolUsed   string `json:"tool_used,omitempty"`
	Error      string `json:"error,omitempty"`
	Note       string `json:"note,omitempty"`
}
