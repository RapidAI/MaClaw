package corelib

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ParseMCPResponse reads an HTTP response body and extracts the JSON-RPC
// payload. It handles both plain JSON responses (Content-Type: application/json)
// and SSE/Streamable HTTP responses (Content-Type: text/event-stream).
//
// MCP Streamable HTTP servers (e.g. 智谱 BigModel) return SSE frames like:
//
//	id:1
//	event:message
//	data:{"jsonrpc":"2.0","id":1,"result":{...}}
//
// This function extracts the JSON from the "data:" line of "event:message" frames.
//
// maxBytes limits how much of the body is read (0 = 256KB default).
func ParseMCPResponse(body io.Reader, contentType string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
	raw, err := io.ReadAll(io.LimitReader(body, maxBytes))
	if err != nil {
		return nil, fmt.Errorf("read MCP response body: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty MCP response body")
	}

	ct := strings.ToLower(contentType)

	// Plain JSON response — return as-is.
	if strings.Contains(ct, "application/json") && !strings.Contains(ct, "text/event-stream") {
		return raw, nil
	}

	// SSE / Streamable HTTP response — parse SSE frames.
	if strings.Contains(ct, "text/event-stream") {
		return ParseSSEMessageData(raw)
	}

	// Unknown content type — try JSON first, then SSE fallback.
	if json.Valid(raw) {
		return raw, nil
	}
	if result, sseErr := ParseSSEMessageData(raw); sseErr == nil {
		return result, nil
	}

	return raw, nil
}

// ParseSSEMessageData extracts the JSON-RPC payload from an SSE byte stream.
// It looks for "event:message" lines followed by "data:{...}" lines.
// Falls back to the last "data:" line if no "event:message" is found.
//
// Per the SSE spec, a single event's data can span multiple "data:" lines,
// which are concatenated with newlines. This function handles that case.
func ParseSSEMessageData(raw []byte) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))

	var lastDataParts []string   // accumulates data: lines for the current event
	var messageEventData string  // final chosen data from an "event:message" frame
	var lastData string          // final chosen data from any event (fallback)
	inMessageEvent := false

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line = end of SSE event frame.
		if line == "" {
			if len(lastDataParts) > 0 {
				joined := strings.Join(lastDataParts, "\n")
				lastData = joined
				if inMessageEvent {
					messageEventData = joined
				}
				lastDataParts = lastDataParts[:0]
			}
			inMessageEvent = false
			continue
		}

		// Detect "event:message" or "event: message"
		if strings.HasPrefix(line, "event:") {
			eventName := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			inMessageEvent = (eventName == "message")
			continue
		}

		// Accumulate "data:" payload lines.
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			// SSE spec: strip at most one leading space after "data:".
			if len(data) > 0 && data[0] == ' ' {
				data = data[1:]
			}
			lastDataParts = append(lastDataParts, data)
		}
	}

	// Handle stream that doesn't end with a blank line.
	if len(lastDataParts) > 0 {
		joined := strings.Join(lastDataParts, "\n")
		lastData = joined
		if inMessageEvent {
			messageEventData = joined
		}
	}

	// Prefer data from "event:message" frames.
	chosen := messageEventData
	if chosen == "" {
		chosen = lastData
	}
	if chosen == "" {
		return nil, fmt.Errorf("no data field found in SSE stream")
	}

	if !json.Valid([]byte(chosen)) {
		return nil, fmt.Errorf("SSE data is not valid JSON: %.200s", chosen)
	}

	return []byte(chosen), nil
}
