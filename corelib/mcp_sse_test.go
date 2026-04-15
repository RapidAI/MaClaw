package corelib

import (
	"strings"
	"testing"
)

func TestParseMCPResponse_PlainJSON(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"web_search","description":"Search the web"}]}}`
	parsed, err := ParseMCPResponse(strings.NewReader(body), "application/json", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(parsed) != body {
		t.Errorf("expected %q, got %q", body, string(parsed))
	}
}

func TestParseMCPResponse_SSEWithEventMessage(t *testing.T) {
	// This is the exact format returned by 智谱 BigModel MCP servers.
	sseBody := "id:1\nevent:message\ndata:{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[{\"name\":\"web_search_prime\",\"description\":\"Search web\"}]}}\n\n"
	parsed, err := ParseMCPResponse(strings.NewReader(sseBody), "text/event-stream;charset=UTF-8", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(parsed), "web_search_prime") {
		t.Errorf("expected parsed result to contain 'web_search_prime', got: %s", string(parsed))
	}
}

func TestParseMCPResponse_SSEWithoutEventLine(t *testing.T) {
	// Some servers may omit the "event:" line.
	sseBody := "data:{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[]}}\n\n"
	parsed, err := ParseMCPResponse(strings.NewReader(sseBody), "text/event-stream", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(parsed), "tools") {
		t.Errorf("expected parsed result to contain 'tools', got: %s", string(parsed))
	}
}

func TestParseMCPResponse_SSEMultipleEvents(t *testing.T) {
	// Multiple SSE events — should pick the "event:message" one.
	sseBody := "event:ping\ndata:{\"type\":\"ping\"}\n\nevent:message\ndata:{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"hello\"}]}}\n\n"
	parsed, err := ParseMCPResponse(strings.NewReader(sseBody), "text/event-stream", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(parsed), "hello") {
		t.Errorf("expected parsed result to contain 'hello', got: %s", string(parsed))
	}
	if strings.Contains(string(parsed), "ping") {
		t.Errorf("should not contain ping event data, got: %s", string(parsed))
	}
}

func TestParseMCPResponse_EmptyBody(t *testing.T) {
	_, err := ParseMCPResponse(strings.NewReader(""), "application/json", 0)
	if err == nil {
		t.Fatal("expected error for empty body")
	}
}

func TestParseMCPResponse_UnknownContentTypeValidJSON(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":{}}`
	parsed, err := ParseMCPResponse(strings.NewReader(body), "text/plain", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(parsed) != body {
		t.Errorf("expected %q, got %q", body, string(parsed))
	}
}

func TestParseMCPResponse_UnknownContentTypeSSE(t *testing.T) {
	// Unknown content type but body is SSE format — should still parse.
	sseBody := "event:message\ndata:{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"
	parsed, err := ParseMCPResponse(strings.NewReader(sseBody), "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(parsed), "result") {
		t.Errorf("expected parsed result to contain 'result', got: %s", string(parsed))
	}
}

func TestParseSSEMessageData_NoDataField(t *testing.T) {
	_, err := ParseSSEMessageData([]byte("event:message\n\n"))
	if err == nil {
		t.Fatal("expected error for SSE with no data field")
	}
}

func TestParseSSEMessageData_DataWithSpaces(t *testing.T) {
	// "data: {...}" with space after colon.
	raw := []byte("event:message\ndata: {\"ok\":true}\n\n")
	parsed, err := ParseSSEMessageData(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(parsed) != `{"ok":true}` {
		t.Errorf("expected {\"ok\":true}, got %s", string(parsed))
	}
}

func TestParseSSEMessageData_MultiLineData(t *testing.T) {
	// SSE spec allows data split across multiple "data:" lines, joined by newlines.
	raw := []byte("event:message\ndata:{\"jsonrpc\":\"2.0\",\ndata:\"id\":1,\ndata:\"result\":{}}\n\n")
	parsed, err := ParseSSEMessageData(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "{\"jsonrpc\":\"2.0\",\n\"id\":1,\n\"result\":{}}"
	if string(parsed) != expected {
		t.Errorf("expected %q, got %q", expected, string(parsed))
	}
}

func TestParseSSEMessageData_NoTrailingBlankLine(t *testing.T) {
	// Stream that doesn't end with a blank line — should still parse.
	raw := []byte("event:message\ndata:{\"ok\":true}")
	parsed, err := ParseSSEMessageData(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(parsed) != `{"ok":true}` {
		t.Errorf("expected {\"ok\":true}, got %s", string(parsed))
	}
}
