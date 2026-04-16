package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StandardResponse represents a normalized successful MCP tool response.
type StandardResponse struct {
	Status   string `json:"status"`    // always "ok"
	ServerID string `json:"server_id"`
	ToolName string `json:"tool_name"`
	Result   string `json:"result"` // original response content
}

// StandardError represents a normalized MCP error response.
type StandardError struct {
	Status       string `json:"status"`        // always "error"
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
