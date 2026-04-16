package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ErrorCode constants for the StandardError error_code field.
const (
	ErrValidation = "validation_error" // local schema validation failure
	ErrConnection = "connection_error" // network/timeout
	ErrAuth       = "auth_error"       // 401/403
	ErrRateLimit  = "rate_limit"       // 429
	ErrServer     = "server_error"     // 5xx
	ErrRPC        = "rpc_error"        // JSON-RPC error response
	ErrTool       = "tool_error"       // MCP result.isError
	ErrUnknown    = "unknown_error"    // unclassifiable
)

// ClassifyError categorizes an error into one of the standard error codes.
// It inspects the HTTP status code, error message string, and raw response body
// for known patterns. Classification rules are evaluated in order:
//
//  1. httpStatusCode 401/403 → auth_error
//  2. httpStatusCode 429 → rate_limit
//  3. httpStatusCode >= 500 → server_error
//  4. error contains "timeout" or "deadline exceeded" → connection_error
//  5. error contains "connection refused" or "no such host" → connection_error
//  6. JSON-RPC error object detected in rawBody → rpc_error
//  7. MCP result.isError detected in rawBody → tool_error
//  8. otherwise → unknown_error (raw body truncated to 500 characters)
func ClassifyError(err error, httpStatusCode int, rawBody string) (errorCode string, errorMessage string) {
	// Rule 1: HTTP 401/403 → auth_error
	if httpStatusCode == 401 || httpStatusCode == 403 {
		return ErrAuth, fmt.Sprintf("Authentication failed (HTTP %d)", httpStatusCode)
	}

	// Rule 2: HTTP 429 → rate_limit
	if httpStatusCode == 429 {
		return ErrRateLimit, "Rate limited — retry after a delay (HTTP 429)"
	}

	// Rule 3: HTTP 5xx → server_error
	if httpStatusCode >= 500 {
		return ErrServer, fmt.Sprintf("Server error (HTTP %d)", httpStatusCode)
	}

	// Rules 4 & 5: error message pattern matching for connection errors.
	if err != nil {
		errMsg := err.Error()
		errLower := strings.ToLower(errMsg)

		// Rule 4: timeout / deadline exceeded → connection_error
		if strings.Contains(errLower, "timeout") || strings.Contains(errLower, "deadline exceeded") {
			return ErrConnection, fmt.Sprintf("Connection timeout: %s", errMsg)
		}

		// Rule 5: connection refused / no such host → connection_error
		if strings.Contains(errLower, "connection refused") || strings.Contains(errLower, "no such host") {
			return ErrConnection, fmt.Sprintf("Connection failed: %s", errMsg)
		}

		// Rules 1-3 fallback: extract HTTP status code from error message
		// when httpStatusCode was not provided (e.g., "MCP HTTP 429: ...").
		if httpStatusCode == 0 {
			if code := extractHTTPStatusFromError(errLower); code > 0 {
				if code == 401 || code == 403 {
					return ErrAuth, fmt.Sprintf("Authentication failed (HTTP %d): %s", code, errMsg)
				}
				if code == 429 {
					return ErrRateLimit, fmt.Sprintf("Rate limited — retry after a delay (HTTP 429): %s", errMsg)
				}
				if code >= 500 {
					return ErrServer, fmt.Sprintf("Server error (HTTP %d): %s", code, errMsg)
				}
			}
		}
	}

	// Rules 6 & 7: inspect rawBody for JSON-RPC error or MCP result.isError.
	if rawBody != "" {
		// Rule 6: JSON-RPC error object detected.
		if code, msg, ok := extractJSONRPCError(rawBody); ok {
			return ErrRPC, fmt.Sprintf("RPC error %d: %s", code, msg)
		}

		// Rule 7: MCP result.isError detected.
		if content, ok := extractMCPToolError(rawBody); ok {
			return ErrTool, fmt.Sprintf("Tool error: %s", content)
		}
	}

	// Rule 8: unknown_error — include truncated body or error message.
	detail := truncateToMaxLen(rawBody, 500)
	if detail == "" && err != nil {
		detail = truncateToMaxLen(err.Error(), 500)
	}
	return ErrUnknown, fmt.Sprintf("Unexpected error: %s", detail)
}

// extractJSONRPCError attempts to parse rawBody as a JSON-RPC response containing
// an "error" object with "code" (number) and "message" (string) fields.
// Returns the code, message, and true if found; otherwise returns zero values and false.
func extractJSONRPCError(rawBody string) (code int, message string, ok bool) {
	var envelope struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(rawBody), &envelope); err != nil {
		return 0, "", false
	}
	if envelope.Error == nil {
		return 0, "", false
	}
	return envelope.Error.Code, envelope.Error.Message, true
}

// extractMCPToolError attempts to parse rawBody as a JSON-RPC response where
// result.isError is true. It extracts the text content from result.content.
// Returns the content string and true if found; otherwise returns empty and false.
func extractMCPToolError(rawBody string) (content string, ok bool) {
	var envelope struct {
		Result *struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(rawBody), &envelope); err != nil {
		return "", false
	}
	if envelope.Result == nil || !envelope.Result.IsError {
		return "", false
	}
	// Collect text content entries.
	var parts []string
	for _, c := range envelope.Result.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	if len(parts) == 0 {
		return "unknown tool error", true
	}
	return strings.Join(parts, "; "), true
}

// truncateToMaxLen truncates s to maxLen characters. If truncated, appends "…".
func truncateToMaxLen(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// extractHTTPStatusFromError extracts an HTTP status code from an error message
// string like "MCP HTTP 429: ..." or "http 503". Returns 0 if not found.
func extractHTTPStatusFromError(errLower string) int {
	// Look for patterns like "http 4xx", "http 5xx", "mcp http 4xx"
	for _, prefix := range []string{"http ", "http: "} {
		idx := strings.Index(errLower, prefix)
		if idx < 0 {
			continue
		}
		numStart := idx + len(prefix)
		if numStart+3 > len(errLower) {
			continue
		}
		codeStr := errLower[numStart : numStart+3]
		code := 0
		for _, ch := range codeStr {
			if ch < '0' || ch > '9' {
				code = 0
				break
			}
			code = code*10 + int(ch-'0')
		}
		if code >= 100 && code <= 599 {
			return code
		}
	}
	return 0
}
