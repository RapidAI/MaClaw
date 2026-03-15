package main

import (
	"encoding/json"
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
