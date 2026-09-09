package guiapp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSDKUserMessage_MarshalJSON_StringContent(t *testing.T) {
	msg := SDKUserMessage{
		Role:    "user",
		Content: "hello world",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Verify the JSON has content as a string, not an array
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	// content should start with " (a JSON string)
	if len(raw["content"]) == 0 || raw["content"][0] != '"' {
		t.Fatalf("expected content to be a JSON string, got: %s", raw["content"])
	}

	var content string
	if err := json.Unmarshal(raw["content"], &content); err != nil {
		t.Fatalf("unmarshal content string: %v", err)
	}
	if content != "hello world" {
		t.Fatalf("expected 'hello world', got %q", content)
	}
}

func TestSDKUserMessage_MarshalJSON_MultiPartContent(t *testing.T) {
	msg := SDKUserMessage{
		Role: "user",
		Content: []SDKUserContentPart{
			{Type: "text", Text: "describe this image"},
			{Type: "image", Source: &SDKImageSource{
				Type:      "base64",
				MediaType: "image/png",
				Data:      "iVBORw0KGgo=",
			}},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	// content should start with [ (a JSON array)
	if len(raw["content"]) == 0 || raw["content"][0] != '[' {
		t.Fatalf("expected content to be a JSON array, got: %s", raw["content"])
	}

	var parts []SDKUserContentPart
	if err := json.Unmarshal(raw["content"], &parts); err != nil {
		t.Fatalf("unmarshal content array: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "describe this image" {
		t.Fatalf("unexpected text part: %+v", parts[0])
	}
	if parts[1].Type != "image" || parts[1].Source == nil || parts[1].Source.MediaType != "image/png" {
		t.Fatalf("unexpected image part: %+v", parts[1])
	}
}

func TestSDKUserMessage_UnmarshalJSON_StringContent(t *testing.T) {
	input := `{"role":"user","content":"hello world"}`

	var msg SDKUserMessage
	if err := json.Unmarshal([]byte(input), &msg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if msg.Role != "user" {
		t.Fatalf("expected role 'user', got %q", msg.Role)
	}

	s, ok := msg.Content.(string)
	if !ok {
		t.Fatalf("expected Content to be string, got %T", msg.Content)
	}
	if s != "hello world" {
		t.Fatalf("expected 'hello world', got %q", s)
	}
}

func TestSDKUserMessage_UnmarshalJSON_ArrayContent(t *testing.T) {
	input := `{"role":"user","content":[{"type":"text","text":"look at this"},{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"abc123"}}]}`

	var msg SDKUserMessage
	if err := json.Unmarshal([]byte(input), &msg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if msg.Role != "user" {
		t.Fatalf("expected role 'user', got %q", msg.Role)
	}

	parts, ok := msg.Content.([]SDKUserContentPart)
	if !ok {
		t.Fatalf("expected Content to be []SDKUserContentPart, got %T", msg.Content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "look at this" {
		t.Fatalf("unexpected text part: %+v", parts[0])
	}
	if parts[1].Type != "image" || parts[1].Source == nil {
		t.Fatalf("unexpected image part: %+v", parts[1])
	}
	if parts[1].Source.MediaType != "image/jpeg" || parts[1].Source.Data != "abc123" {
		t.Fatalf("unexpected image source: %+v", parts[1].Source)
	}
}

func TestSDKUserMessage_RoundTrip_String(t *testing.T) {
	original := SDKUserMessage{
		Role:    "user",
		Content: "round trip test",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SDKUserMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Role != original.Role {
		t.Fatalf("role mismatch: %q vs %q", decoded.Role, original.Role)
	}
	s, ok := decoded.Content.(string)
	if !ok {
		t.Fatalf("expected string content, got %T", decoded.Content)
	}
	if s != "round trip test" {
		t.Fatalf("content mismatch: %q", s)
	}
}

func TestSDKUserMessage_RoundTrip_MultiPart(t *testing.T) {
	original := SDKUserMessage{
		Role: "user",
		Content: []SDKUserContentPart{
			{Type: "image", Source: &SDKImageSource{
				Type:      "base64",
				MediaType: "image/webp",
				Data:      "AAAA",
			}},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SDKUserMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	parts, ok := decoded.Content.([]SDKUserContentPart)
	if !ok {
		t.Fatalf("expected []SDKUserContentPart, got %T", decoded.Content)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Source == nil || parts[0].Source.MediaType != "image/webp" || parts[0].Source.Data != "AAAA" {
		t.Fatalf("round-trip mismatch: %+v", parts[0])
	}
}

func TestSDKUserInput_MarshalJSON_BackwardCompatible(t *testing.T) {
	// This simulates what SDKExecutionHandle.Write() does — Content is a plain string
	input := SDKUserInput{
		Type: "user",
		Message: SDKUserMessage{
			Role:    "user",
			Content: "plain text message",
		},
		SessionID:       "default",
		ParentToolUseID: nil,
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify the JSON structure matches the expected format
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	// Verify session_id and parent_tool_use_id are present
	if _, ok := raw["session_id"]; !ok {
		t.Fatal("expected session_id field in JSON output")
	}
	if _, ok := raw["parent_tool_use_id"]; !ok {
		t.Fatal("expected parent_tool_use_id field in JSON output")
	}

	var msgRaw map[string]json.RawMessage
	if err := json.Unmarshal(raw["message"], &msgRaw); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}

	// content should be a JSON string
	if len(msgRaw["content"]) == 0 || msgRaw["content"][0] != '"' {
		t.Fatalf("expected content to be a JSON string for backward compat, got: %s", msgRaw["content"])
	}

	var content string
	if err := json.Unmarshal(msgRaw["content"], &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if content != "plain text message" {
		t.Fatalf("expected 'plain text message', got %q", content)
	}
}

func TestSDKContentBlock_UnmarshalJSON_ToolResultStringContent(t *testing.T) {
	// tool_result with plain string content — backward compatibility
	input := `{"type":"tool_result","tool_use_id":"tu_1","content":"file contents here","is_error":false}`

	var block SDKContentBlock
	if err := json.Unmarshal([]byte(input), &block); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if block.Type != "tool_result" {
		t.Fatalf("expected type 'tool_result', got %q", block.Type)
	}
	if block.ToolUseID != "tu_1" {
		t.Fatalf("expected tool_use_id 'tu_1', got %q", block.ToolUseID)
	}
	if block.Content != "file contents here" {
		t.Fatalf("expected string content 'file contents here', got %q", block.Content)
	}
	if len(block.NestedContent) != 0 {
		t.Fatalf("expected no nested content, got %d blocks", len(block.NestedContent))
	}
}

func TestSDKContentBlock_UnmarshalJSON_ToolResultArrayWithImage(t *testing.T) {
	// tool_result with array content containing an image block (Read tool returning a PNG)
	input := `{
		"type": "tool_result",
		"tool_use_id": "tu_2",
		"content": [
			{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "iVBORw0KGgo="}}
		]
	}`

	var block SDKContentBlock
	if err := json.Unmarshal([]byte(input), &block); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if block.Type != "tool_result" {
		t.Fatalf("expected type 'tool_result', got %q", block.Type)
	}
	if block.Content != "" {
		t.Fatalf("expected empty string content when array, got %q", block.Content)
	}
	if len(block.NestedContent) != 1 {
		t.Fatalf("expected 1 nested block, got %d", len(block.NestedContent))
	}
	nested := block.NestedContent[0]
	if nested.Type != "image" {
		t.Fatalf("expected nested type 'image', got %q", nested.Type)
	}
	if nested.Source == nil {
		t.Fatal("expected nested source to be non-nil")
	}
	if nested.Source.MediaType != "image/png" {
		t.Fatalf("expected media_type 'image/png', got %q", nested.Source.MediaType)
	}
	if nested.Source.Data != "iVBORw0KGgo=" {
		t.Fatalf("expected data 'iVBORw0KGgo=', got %q", nested.Source.Data)
	}
}

func TestSDKContentBlock_UnmarshalJSON_ToolResultArrayMixed(t *testing.T) {
	// tool_result with array content containing text + image blocks
	input := `{
		"type": "tool_result",
		"tool_use_id": "tu_3",
		"content": [
			{"type": "text", "text": "Screenshot captured"},
			{"type": "image", "source": {"type": "base64", "media_type": "image/jpeg", "data": "/9j/4AAQ"}},
			{"type": "text", "text": "Done"}
		]
	}`

	var block SDKContentBlock
	if err := json.Unmarshal([]byte(input), &block); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if block.Content != "" {
		t.Fatalf("expected empty string content, got %q", block.Content)
	}
	if len(block.NestedContent) != 3 {
		t.Fatalf("expected 3 nested blocks, got %d", len(block.NestedContent))
	}
	if block.NestedContent[0].Type != "text" || block.NestedContent[0].Text != "Screenshot captured" {
		t.Fatalf("unexpected first nested block: %+v", block.NestedContent[0])
	}
	if block.NestedContent[1].Type != "image" || block.NestedContent[1].Source == nil {
		t.Fatalf("unexpected second nested block: %+v", block.NestedContent[1])
	}
	if block.NestedContent[1].Source.MediaType != "image/jpeg" {
		t.Fatalf("expected media_type 'image/jpeg', got %q", block.NestedContent[1].Source.MediaType)
	}
	if block.NestedContent[2].Type != "text" || block.NestedContent[2].Text != "Done" {
		t.Fatalf("unexpected third nested block: %+v", block.NestedContent[2])
	}
}

func TestSDKContentBlock_UnmarshalJSON_NoContentField(t *testing.T) {
	// Block without content field at all (e.g. a text block)
	input := `{"type":"text","text":"hello world"}`

	var block SDKContentBlock
	if err := json.Unmarshal([]byte(input), &block); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if block.Type != "text" {
		t.Fatalf("expected type 'text', got %q", block.Type)
	}
	if block.Text != "hello world" {
		t.Fatalf("expected text 'hello world', got %q", block.Text)
	}
	if block.Content != "" {
		t.Fatalf("expected empty content, got %q", block.Content)
	}
	if len(block.NestedContent) != 0 {
		t.Fatalf("expected no nested content, got %d", len(block.NestedContent))
	}
}

func TestSDKContentBlock_UnmarshalJSON_NestedContentNotSerialized(t *testing.T) {
	// Verify NestedContent (json:"-") is not included in marshaled output
	block := SDKContentBlock{
		Type:      "tool_result",
		ToolUseID: "tu_x",
		Content:   "original",
		NestedContent: []SDKContentBlock{
			{Type: "image", Source: &SDKImageSource{Type: "base64", MediaType: "image/png", Data: "abc"}},
		},
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// The JSON should NOT contain "NestedContent" or "nested_content"
	s := string(data)
	if strings.Contains(s, "NestedContent") || strings.Contains(s, "nested_content") {
		t.Fatalf("NestedContent should not appear in JSON output: %s", s)
	}
	// But should contain the string content
	if !strings.Contains(s, `"content":"original"`) {
		t.Fatalf("expected string content in JSON: %s", s)
	}
}

func TestSDKContentBlock_UnmarshalJSON_EmptyArray(t *testing.T) {
	// tool_result with empty array content
	input := `{"type":"tool_result","tool_use_id":"tu_empty","content":[]}`

	var block SDKContentBlock
	if err := json.Unmarshal([]byte(input), &block); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if block.Content != "" {
		t.Fatalf("expected empty content, got %q", block.Content)
	}
	// Empty array parses to empty slice (not nil)
	if block.NestedContent == nil {
		t.Fatal("expected non-nil NestedContent for empty array")
	}
	if len(block.NestedContent) != 0 {
		t.Fatalf("expected 0 nested blocks, got %d", len(block.NestedContent))
	}
}

func TestSDKMessage_UnmarshalJSON_FullAssistantWithNestedToolResult(t *testing.T) {
	// Full SDKMessage with assistant payload containing a tool_result with nested image
	input := `{
		"type": "user",
		"message": {
			"role": "user",
			"content": [
				{
					"type": "tool_result",
					"tool_use_id": "tu_read",
					"content": [
						{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "AAAA"}}
					]
				}
			]
		}
	}`

	var msg SDKMessage
	if err := json.Unmarshal([]byte(input), &msg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if msg.Type != "user" {
		t.Fatalf("expected type 'user', got %q", msg.Type)
	}
	if msg.Message == nil {
		t.Fatal("expected non-nil message")
	}
	if len(msg.Message.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(msg.Message.Content))
	}
	block := msg.Message.Content[0]
	if block.Type != "tool_result" {
		t.Fatalf("expected type 'tool_result', got %q", block.Type)
	}
	if len(block.NestedContent) != 1 {
		t.Fatalf("expected 1 nested block, got %d", len(block.NestedContent))
	}
	if block.NestedContent[0].Type != "image" {
		t.Fatalf("expected nested type 'image', got %q", block.NestedContent[0].Type)
	}
	if block.NestedContent[0].Source == nil || block.NestedContent[0].Source.Data != "AAAA" {
		t.Fatalf("unexpected nested image source: %+v", block.NestedContent[0].Source)
	}
}
