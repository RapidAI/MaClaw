package main

import (
	"encoding/json"
	"fmt"
)

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

	// For stream_event messages (type=stream_event) — partial streaming
	// Contains raw Claude API streaming events (content_block_delta, etc.)
	Event map[string]interface{} `json:"event,omitempty"`
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

	// Image fields (type="image")
	Source *SDKImageSource `json:"source,omitempty"`
}

// SDKImageSource represents the source data for an image content block.
type SDKImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", "image/jpeg", etc.
	Data      string `json:"data"`       // base64-encoded image data
}

// SDKUserContentPart represents a single part in a multi-part user message content.
type SDKUserContentPart struct {
	Type   string          `json:"type"`             // "text" or "image"
	Text   string          `json:"text,omitempty"`   // type="text"
	Source *SDKImageSource `json:"source,omitempty"` // type="image"
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
	Subtype   string                  `json:"subtype"` // "success" or "error"
	RequestID string                  `json:"request_id"`
	Error     string                  `json:"error,omitempty"`
	Response  *SDKPermissionResult    `json:"response,omitempty"`
}

// SDKPermissionResult is the nested permission result inside a control response.
type SDKPermissionResult struct {
	Behavior     string                 `json:"behavior"`                // "allow", "deny"
	UpdatedInput map[string]interface{} `json:"updatedInput,omitempty"`
	Message      string                 `json:"message,omitempty"`       // for deny
}

// SDKControlCancelRequest is sent FROM Claude Code to cancel a pending request.
type SDKControlCancelRequest struct {
	Type      string `json:"type"` // "control_cancel_request"
	RequestID string `json:"request_id"`
}

// SDKUserInput is sent TO Claude Code via stdin to provide user messages.
// Matches the format used by the official Claude Code SDK:
// {"type":"user","message":{"role":"user","content":"..."}}
// Note: session_id and parent_tool_use_id are NOT included — the official
// SDK (hapi) does not send them, and Claude Code handles session routing
// internally when using --input-format stream-json.
type SDKUserInput struct {
	Type    string         `json:"type"` // "user"
	Message SDKUserMessage `json:"message"`
}

type SDKUserMessage struct {
	Role    string      `json:"role"`    // "user"
	Content interface{} `json:"content"` // string (text) or []SDKUserContentPart (multi-part)
}

// MarshalJSON implements custom JSON serialization for SDKUserMessage.
// When Content is a string, the "content" field is serialized as a JSON string.
// When Content is []SDKUserContentPart, it is serialized as a JSON array.
func (m SDKUserMessage) MarshalJSON() ([]byte, error) {
	type alias struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}

	a := alias{Role: m.Role}

	switch v := m.Content.(type) {
	case string:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal string content: %w", err)
		}
		a.Content = raw
	case []SDKUserContentPart:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal multi-part content: %w", err)
		}
		a.Content = raw
	default:
		// Fallback: marshal whatever Content is directly
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal content: %w", err)
		}
		a.Content = raw
	}

	return json.Marshal(a)
}

// UnmarshalJSON implements custom JSON deserialization for SDKUserMessage.
// It detects the JSON token type of the "content" field:
// - JSON string → Content is set as a Go string
// - JSON array  → Content is parsed as []SDKUserContentPart
func (m *SDKUserMessage) UnmarshalJSON(data []byte) error {
	type alias struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}

	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}

	m.Role = a.Role

	if len(a.Content) == 0 {
		m.Content = ""
		return nil
	}

	// Detect token type by first non-whitespace byte
	switch a.Content[0] {
	case '"':
		var s string
		if err := json.Unmarshal(a.Content, &s); err != nil {
			return fmt.Errorf("unmarshal string content: %w", err)
		}
		m.Content = s
	case '[':
		var parts []SDKUserContentPart
		if err := json.Unmarshal(a.Content, &parts); err != nil {
			return fmt.Errorf("unmarshal multi-part content: %w", err)
		}
		m.Content = parts
	default:
		// Fallback: store as raw string
		m.Content = string(a.Content)
	}

	return nil
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
