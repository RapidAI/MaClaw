package a2a

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStreamChunkMessageConstruction(t *testing.T) {
	msg := GroupDiscussionMessage{
		ID:        "chunk-001",
		SessionID: "session-abc",
		FromID:    "ve-agent-1",
		Kind:      MessageStreamChunk,
		Content:   "Hello, this is a streaming",
		CreatedAt: time.Now(),
	}

	if msg.Kind != MessageStreamChunk {
		t.Errorf("expected kind=%s, got %s", MessageStreamChunk, msg.Kind)
	}
	if msg.Content == "" {
		t.Error("stream chunk content should not be empty")
	}
}

func TestStreamEndMessageConstruction(t *testing.T) {
	msg := GroupDiscussionMessage{
		ID:        "end-001",
		SessionID: "session-abc",
		FromID:    "ve-agent-1",
		Kind:      MessageStreamEnd,
		Content:   "",
		CreatedAt: time.Now(),
	}

	if msg.Kind != MessageStreamEnd {
		t.Errorf("expected kind=%s, got %s", MessageStreamEnd, msg.Kind)
	}
}

func TestStreamChunkMessageJSONRoundTrip(t *testing.T) {
	original := GroupDiscussionMessage{
		ID:        "chunk-002",
		SessionID: "session-xyz",
		FromID:    "ve-agent-2",
		Kind:      MessageStreamChunk,
		Content:   "这是一个流式响应片段",
		CreatedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded GroupDiscussionMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Kind != MessageStreamChunk {
		t.Errorf("kind mismatch: expected %s, got %s", MessageStreamChunk, decoded.Kind)
	}
	if decoded.Content != original.Content {
		t.Errorf("content mismatch: expected %q, got %q", original.Content, decoded.Content)
	}
	if decoded.FromID != original.FromID {
		t.Errorf("from_id mismatch: expected %q, got %q", original.FromID, decoded.FromID)
	}
	if decoded.SessionID != original.SessionID {
		t.Errorf("session_id mismatch: expected %q, got %q", original.SessionID, decoded.SessionID)
	}
}

func TestStreamEndMessageJSONRoundTrip(t *testing.T) {
	original := GroupDiscussionMessage{
		ID:        "end-002",
		SessionID: "session-xyz",
		FromID:    "ve-agent-2",
		Kind:      MessageStreamEnd,
		Content:   "",
		CreatedAt: time.Date(2026, 5, 1, 12, 0, 1, 0, time.UTC),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded GroupDiscussionMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Kind != MessageStreamEnd {
		t.Errorf("kind mismatch: expected %s, got %s", MessageStreamEnd, decoded.Kind)
	}
	if decoded.Content != "" {
		t.Errorf("stream_end content should be empty, got %q", decoded.Content)
	}
}

func TestStreamMessageInGroupEnvelope(t *testing.T) {
	chunkMsg := &GroupDiscussionMessage{
		ID:        "chunk-003",
		SessionID: "session-123",
		FromID:    "ve-agent-3",
		Kind:      MessageStreamChunk,
		Content:   "partial response",
		CreatedAt: time.Now(),
	}

	envelope := GroupEnvelope{
		ID:        "env-001",
		Type:      GroupMessageDiscussionMessage,
		Scope:     GroupScopeCurrentHub,
		FromID:    "ve-agent-3",
		SessionID: "session-123",
		CreatedAt: time.Now(),
		Message:   chunkMsg,
	}

	// Validate envelope
	if err := envelope.ValidateCurrentHub(); err != nil {
		t.Fatalf("envelope validation failed: %v", err)
	}

	// Verify the message kind is preserved in the envelope
	if envelope.Message.Kind != MessageStreamChunk {
		t.Errorf("expected message kind=%s in envelope, got %s", MessageStreamChunk, envelope.Message.Kind)
	}
}

func TestStreamMessageKindConstants(t *testing.T) {
	// Verify the constants have the expected string values
	if string(MessageStreamChunk) != "stream_chunk" {
		t.Errorf("MessageStreamChunk = %q, want %q", MessageStreamChunk, "stream_chunk")
	}
	if string(MessageStreamEnd) != "stream_end" {
		t.Errorf("MessageStreamEnd = %q, want %q", MessageStreamEnd, "stream_end")
	}
}

func TestStreamChunkWithAttachments(t *testing.T) {
	// Stream chunks can carry attachments (e.g., AI-generated images)
	msg := GroupDiscussionMessage{
		ID:        "chunk-att-001",
		SessionID: "session-att",
		FromID:    "ve-agent-4",
		Kind:      MessageStreamChunk,
		Content:   "Here is the generated image:",
		ImageAttachments: []ImageAttachment{
			{FileURL: "https://hub.local/files/img-001", Filename: "output.png", MimeType: "image/png"},
		},
		CreatedAt: time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded GroupDiscussionMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(decoded.ImageAttachments) != 1 {
		t.Fatalf("expected 1 image attachment, got %d", len(decoded.ImageAttachments))
	}
	if decoded.ImageAttachments[0].FileURL != "https://hub.local/files/img-001" {
		t.Errorf("image URL mismatch")
	}
}

func TestSessionLifecycle_MessageSequence(t *testing.T) {
	// Simulate a complete VE session message sequence:
	// 1. User sends statement
	// 2. VE responds with stream_chunk (multiple)
	// 3. VE sends stream_end

	messages := []GroupDiscussionMessage{
		{ID: "msg-1", SessionID: "sess-lc", FromID: "user-1", Kind: MessageStatement, Content: "What is Go?", CreatedAt: time.Now()},
		{ID: "msg-2", SessionID: "sess-lc", FromID: "ve-1", Kind: MessageStreamChunk, Content: "Go is a ", CreatedAt: time.Now()},
		{ID: "msg-3", SessionID: "sess-lc", FromID: "ve-1", Kind: MessageStreamChunk, Content: "programming language", CreatedAt: time.Now()},
		{ID: "msg-4", SessionID: "sess-lc", FromID: "ve-1", Kind: MessageStreamEnd, Content: "", CreatedAt: time.Now()},
	}

	// Verify all messages serialize/deserialize correctly
	for i, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("message %d: marshal failed: %v", i, err)
		}
		var decoded GroupDiscussionMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("message %d: unmarshal failed: %v", i, err)
		}
		if decoded.Kind != msg.Kind {
			t.Errorf("message %d: kind mismatch: expected %s, got %s", i, msg.Kind, decoded.Kind)
		}
		if decoded.SessionID != "sess-lc" {
			t.Errorf("message %d: session_id mismatch", i)
		}
	}

	// Verify the sequence: statement → chunks → end
	if messages[0].Kind != MessageStatement {
		t.Error("first message should be statement")
	}
	if messages[1].Kind != MessageStreamChunk || messages[2].Kind != MessageStreamChunk {
		t.Error("middle messages should be stream_chunk")
	}
	if messages[3].Kind != MessageStreamEnd {
		t.Error("last message should be stream_end")
	}
}
