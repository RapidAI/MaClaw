package session

import (
	"testing"
)

func TestSerialize_Empty(t *testing.T) {
	result := Serialize(nil)
	if result != "" {
		t.Errorf("expected empty string for nil entries, got %q", result)
	}
	result = Serialize([]TranscriptEntry{})
	if result != "" {
		t.Errorf("expected empty string for empty entries, got %q", result)
	}
}

func TestSerialize_SimpleConversation(t *testing.T) {
	entries := []TranscriptEntry{
		{Role: "user", Content: "How do I implement a BFS in Go?"},
		{Role: "assistant", Content: "Here's a BFS implementation..."},
	}

	result := Serialize(entries)

	expected := "[user]\nHow do I implement a BFS in Go?\n---\n[assistant]\nHere's a BFS implementation...\n---\n"
	if result != expected {
		t.Errorf("unexpected serialization:\ngot:  %q\nwant: %q", result, expected)
	}
}

func TestSerialize_ToolCall(t *testing.T) {
	entries := []TranscriptEntry{
		{Role: "user", Content: "Write a BFS file"},
		{
			Role: "assistant",
			ToolCalls: []ToolCallMeta{
				{ID: "call_001", Name: "write_file", Args: `{"path": "bfs.go", "content": "package main..."}`},
			},
		},
		{Role: "tool", Content: "File written successfully.", ToolCallID: "call_001"},
		{Role: "assistant", Content: "I've created bfs.go with the implementation."},
	}

	result := Serialize(entries)

	if result == "" {
		t.Fatal("expected non-empty serialization")
	}

	// Verify key markers are present
	if !contains(result, "[tool_call:call_001 name:write_file]") {
		t.Error("missing tool_call marker")
	}
	if !contains(result, "[tool_result:call_001]") {
		t.Error("missing tool_result marker")
	}
	if !contains(result, "[user]") {
		t.Error("missing user marker")
	}
	if !contains(result, "[assistant]") {
		t.Error("missing assistant marker")
	}
}

func TestDeserialize_Empty(t *testing.T) {
	entries, err := Deserialize("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries, got %v", entries)
	}

	entries, err = Deserialize("   \n  \n  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries for whitespace, got %v", entries)
	}
}

func TestRoundTrip_SimpleConversation(t *testing.T) {
	original := []TranscriptEntry{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
		{Role: "user", Content: "How are you?"},
		{Role: "assistant", Content: "I'm doing well, thanks!"},
	}

	serialized := Serialize(original)
	deserialized, err := Deserialize(serialized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deserialized) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(deserialized), len(original))
	}

	for i := range original {
		if deserialized[i].Role != original[i].Role {
			t.Errorf("entry %d: role mismatch: got %q, want %q", i, deserialized[i].Role, original[i].Role)
		}
		if deserialized[i].Content != original[i].Content {
			t.Errorf("entry %d: content mismatch: got %q, want %q", i, deserialized[i].Content, original[i].Content)
		}
	}
}

func TestRoundTrip_WithToolCalls(t *testing.T) {
	original := []TranscriptEntry{
		{Role: "user", Content: "Write a file"},
		{
			Role: "assistant",
			ToolCalls: []ToolCallMeta{
				{ID: "call_001", Name: "write_file", Args: `{"path": "test.go"}`},
			},
		},
		{Role: "tool", Content: "File written.", ToolCallID: "call_001"},
		{Role: "assistant", Content: "Done!"},
	}

	serialized := Serialize(original)
	deserialized, err := Deserialize(serialized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deserialized) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(deserialized), len(original))
	}

	// Check tool call entry
	if len(deserialized[1].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(deserialized[1].ToolCalls))
	}
	tc := deserialized[1].ToolCalls[0]
	if tc.ID != "call_001" {
		t.Errorf("tool call ID: got %q, want %q", tc.ID, "call_001")
	}
	if tc.Name != "write_file" {
		t.Errorf("tool call name: got %q, want %q", tc.Name, "write_file")
	}
	if tc.Args != `{"path": "test.go"}` {
		t.Errorf("tool call args: got %q, want %q", tc.Args, `{"path": "test.go"}`)
	}

	// Check tool result entry
	if deserialized[2].ToolCallID != "call_001" {
		t.Errorf("tool result ID: got %q, want %q", deserialized[2].ToolCallID, "call_001")
	}
	if deserialized[2].Content != "File written." {
		t.Errorf("tool result content: got %q, want %q", deserialized[2].Content, "File written.")
	}
}

func TestRoundTrip_MultipleToolCalls(t *testing.T) {
	original := []TranscriptEntry{
		{Role: "user", Content: "Do two things"},
		{
			Role: "assistant",
			ToolCalls: []ToolCallMeta{
				{ID: "call_001", Name: "write_file", Args: `{"path": "a.go"}`},
				{ID: "call_002", Name: "bash", Args: `{"command": "go build"}`},
			},
		},
		{Role: "tool", Content: "File written.", ToolCallID: "call_001"},
		{Role: "tool", Content: "Build succeeded.", ToolCallID: "call_002"},
	}

	serialized := Serialize(original)
	deserialized, err := Deserialize(serialized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deserialized) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(deserialized), len(original))
	}

	// Check multiple tool calls merged into one entry
	if len(deserialized[1].ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(deserialized[1].ToolCalls))
	}
	if deserialized[1].ToolCalls[0].ID != "call_001" {
		t.Errorf("first tool call ID: got %q, want %q", deserialized[1].ToolCalls[0].ID, "call_001")
	}
	if deserialized[1].ToolCalls[1].ID != "call_002" {
		t.Errorf("second tool call ID: got %q, want %q", deserialized[1].ToolCalls[1].ID, "call_002")
	}
}

func TestRoundTrip_SystemMessage(t *testing.T) {
	original := []TranscriptEntry{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Hello!"},
	}

	serialized := Serialize(original)
	deserialized, err := Deserialize(serialized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deserialized) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(deserialized))
	}
	if deserialized[0].Role != "system" {
		t.Errorf("expected system role, got %q", deserialized[0].Role)
	}
	if deserialized[0].Content != "You are a helpful assistant." {
		t.Errorf("system content mismatch")
	}
}

func TestRoundTrip_MultilineContent(t *testing.T) {
	original := []TranscriptEntry{
		{Role: "user", Content: "Line 1\nLine 2\nLine 3"},
		{Role: "assistant", Content: "Response\nwith\nmultiple\nlines"},
	}

	serialized := Serialize(original)
	deserialized, err := Deserialize(serialized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deserialized) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(deserialized))
	}
	if deserialized[0].Content != "Line 1\nLine 2\nLine 3" {
		t.Errorf("multiline content mismatch: got %q", deserialized[0].Content)
	}
	if deserialized[1].Content != "Response\nwith\nmultiple\nlines" {
		t.Errorf("multiline content mismatch: got %q", deserialized[1].Content)
	}
}

func TestDeserialize_InvalidHeader(t *testing.T) {
	text := "---\ninvalid header\nsome content\n---\n"
	_, err := Deserialize(text)
	if err == nil {
		t.Error("expected error for invalid header, got nil")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
