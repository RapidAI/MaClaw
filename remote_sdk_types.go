package main

// Claude Code SDK message types for stream-json protocol.
// When launched with --output-format stream-json --input-format stream-json,
// Claude Code communicates via structured JSON messages on stdin/stdout.

// SDKMessage represents any message from Claude Code's stdout.
type SDKMessage struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// For system messages (type=system, subtype=init)
	SessionID string `json:"session_id,omitempty"`

	// For assistant messages (type=assistant)
	Message *SDKAssistantPayload `json:"message,omitempty"`

	// For result messages (type=result)
	Result *SDKResultPayload `json:"result,omitempty"`

	// For tool_use within assistant content
	ParentToolUseID string `json:"parent_tool_use_id,omitempty"`
}

type SDKAssistantPayload struct {
	Role    string             `json:"role,omitempty"`
	Content []SDKContentBlock  `json:"content,omitempty"`
}

type SDKContentBlock struct {
	Type  string      `json:"type"`
	Text  string      `json:"text,omitempty"`
	ID    string      `json:"id,omitempty"`
	Name  string      `json:"name,omitempty"`
	Input interface{} `json:"input,omitempty"`

	// For tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type SDKResultPayload struct {
	Duration float64 `json:"duration_ms,omitempty"`
	NumTurns int     `json:"num_turns,omitempty"`
}

// SDKControlRequest is sent FROM Claude Code when it needs permission.
type SDKControlRequest struct {
	Type      string                `json:"type"` // "control_request"
	RequestID string                `json:"request_id"`
	Request   SDKControlRequestBody `json:"request"`
}

type SDKControlRequestBody struct {
	Subtype  string      `json:"subtype"` // "can_use_tool"
	ToolName string      `json:"tool_name,omitempty"`
	Input    interface{} `json:"input,omitempty"`
}

// SDKControlResponse is sent TO Claude Code to approve/deny a tool use.
type SDKControlResponse struct {
	Type     string                 `json:"type"` // "control_response"
	Response SDKControlResponseBody `json:"response"`
}

type SDKControlResponseBody struct {
	Subtype   string `json:"subtype"` // "success" or "error"
	RequestID string `json:"request_id"`
	Error     string `json:"error,omitempty"`

	// Permission result fields
	Behavior  string `json:"behavior,omitempty"`  // "allow", "deny", "ask"
	ToolName  string `json:"tool_name,omitempty"`
}

// SDKControlCancelRequest is sent FROM Claude Code to cancel a pending request.
type SDKControlCancelRequest struct {
	Type      string `json:"type"` // "control_cancel_request"
	RequestID string `json:"request_id"`
}

// SDKUserInput is sent TO Claude Code via stdin to provide user messages.
// The session_id field is required by the stream-json protocol to route
// the message to the correct conversation.
type SDKUserInput struct {
	Type            string         `json:"type"`              // "user"
	Message         SDKUserMessage `json:"message"`
	SessionID       string         `json:"session_id"`
	ParentToolUseID *string        `json:"parent_tool_use_id"`
}

type SDKUserMessage struct {
	Role    string `json:"role"` // "user"
	Content string `json:"content"`
}

// SDKInterruptRequest is sent TO Claude Code to interrupt current processing.
type SDKInterruptRequest struct {
	Type      string                `json:"type"` // "control_request"
	RequestID string                `json:"request_id"`
	Request   SDKInterruptBody      `json:"request"`
}

type SDKInterruptBody struct {
	Subtype string `json:"subtype"` // "interrupt"
}

// ExecutionMode describes how a provider should be launched.
type ExecutionMode string

const (
	// ExecModePTY launches the tool in a pseudo-terminal (interactive TUI).
	ExecModePTY ExecutionMode = "pty"

	// ExecModeSDK launches the tool with structured JSON stdin/stdout.
	ExecModeSDK ExecutionMode = "sdk"
)
