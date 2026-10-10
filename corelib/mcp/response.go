package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StandardResponse represents a normalized successful MCP tool response.
type StandardResponse struct {
	Status   string `json:"status"` // always "ok"
	ServerID string `json:"server_id"`
	ToolName string `json:"tool_name"`
	Result   string `json:"result"` // original response content
}

// StandardError represents a normalized MCP error response.
type StandardError struct {
	Status       string `json:"status"` // always "error"
	ServerID     string `json:"server_id"`
	ToolName     string `json:"tool_name"`
	ErrorCode    string `json:"error_code"`    // category from ClassifyError
	ErrorMessage string `json:"error_message"` // human-readable description
}

// NewStandardError creates a StandardError with the status field pre-set to "error".
func NewStandardError(serverID, toolName, errorCode, errorMessage string) *StandardError {
	return &StandardError{
		Status:       "error",
		ServerID:     serverID,
		ToolName:     toolName,
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
	}
}

// NormalizeResponse wraps a raw MCP response string into a StandardResponse.
// It detects MCP protocol errors (result.isError=true) in the rawResponse and
// converts them to StandardError with error_code "tool_error".
// Returns (*StandardResponse, nil) on success, or (nil, *StandardError) on error.
func NormalizeResponse(serverID, toolName, rawResponse string) (*StandardResponse, *StandardError) {
	// Try to detect MCP protocol errors (result.isError=true) in the raw response.
	if rawResponse != "" {
		var envelope struct {
			Result *struct {
				IsError bool `json:"isError"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(rawResponse), &envelope); err == nil {
			if envelope.Result != nil && envelope.Result.IsError {
				// Collect text content entries.
				var parts []string
				for _, c := range envelope.Result.Content {
					if c.Text != "" {
						parts = append(parts, c.Text)
					}
				}
				errMsg := "unknown tool error"
				if len(parts) > 0 {
					errMsg = strings.Join(parts, "; ")
				}
				return nil, NewStandardError(serverID, toolName, ErrTool, fmt.Sprintf("Tool error: %s", errMsg))
			}
		}
	}

	// No error detected — wrap as successful response.
	return &StandardResponse{
		Status:   "ok",
		ServerID: serverID,
		ToolName: toolName,
		Result:   rawResponse,
	}, nil
}

// ToolCallError is a completed tools/call. The transport returned a JSON-RPC
// error or result.isError, so the tool did not produce a successful body.
// Callers must not treat the message as lookup evidence.
type ToolCallError struct {
	Message string
}

func (e *ToolCallError) Error() string {
	if e == nil || strings.TrimSpace(e.Message) == "" {
		return "unknown tool error"
	}
	return e.Message
}

// ToolCallContent projects a tools/call payload to the tool body. Remote
// transports return a JSON-RPC envelope; local transports return the result
// object. Neither envelope, server id, nor tool name is part of the body.
// Text content is joined in order. A payload that is not a tool result is
// returned unchanged. isError and a JSON-RPC error are ToolCallError.
func ToolCallContent(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return raw, nil
	}
	if rawErr, ok := envelope["error"]; ok && !jsonRawNull(rawErr) {
		return "", &ToolCallError{Message: jsonRPCErrorMessage(rawErr)}
	}
	resultRaw := json.RawMessage(raw)
	if rawResult, ok := envelope["result"]; ok {
		if jsonRawNull(rawResult) {
			return "", nil
		}
		resultRaw = rawResult
	} else if _, ok := envelope["content"]; !ok {
		if _, ok := envelope["isError"]; !ok {
			return raw, nil
		}
	}
	return projectMCPToolResult(resultRaw)
}

func projectMCPToolResult(raw json.RawMessage) (string, error) {
	if len(raw) > 0 && raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			return text, nil
		}
	}
	var result struct {
		Content json.RawMessage `json:"content"`
		IsError bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return string(raw), nil
	}
	texts := mcpTextContent(result.Content)
	if result.IsError {
		message := strings.Join(texts, "\n")
		if strings.TrimSpace(message) == "" {
			message = "unknown tool error"
		}
		return "", &ToolCallError{Message: message}
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n"), nil
	}
	return string(raw), nil
}

func mcpTextContent(raw json.RawMessage) []string {
	if jsonRawNull(raw) {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []string{text}
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part.Text) == "" {
			continue
		}
		if part.Type != "" && part.Type != "text" {
			continue
		}
		texts = append(texts, part.Text)
	}
	return texts
}

func jsonRPCErrorMessage(raw json.RawMessage) string {
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &body); err == nil {
		if message := strings.TrimSpace(body.Message); message != "" {
			return message
		}
	}
	return "mcp tool call failed"
}

func jsonRawNull(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null"
}

// FormatForLLM formats a StandardResponse or StandardError as a human-readable
// string suitable for returning to the LLM.
//
// For success: "[MCP OK] server={serverID} tool={toolName}\n{result}"
// For error:   "[MCP ERROR] server={serverID} tool={toolName} code={errorCode}\n{errorMessage}"
//
// If both resp and err are nil, returns an empty string.
// If both are provided, the error takes precedence.
func FormatForLLM(resp *StandardResponse, err *StandardError) string {
	if err != nil {
		return fmt.Sprintf("[MCP ERROR] server=%s tool=%s code=%s\n%s",
			err.ServerID, err.ToolName, err.ErrorCode, err.ErrorMessage)
	}
	if resp != nil {
		return fmt.Sprintf("[MCP OK] server=%s tool=%s\n%s",
			resp.ServerID, resp.ToolName, resp.Result)
	}
	return ""
}
