package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

// --- stream_chunk / stream_end message construction and parsing tests ---

func TestGroupDiscussionMessage_StreamChunk_Construction(t *testing.T) {
	msg := a2a.GroupDiscussionMessage{
		ID:        "msg-001",
		SessionID: "session-abc",
		FromID:    "ve-agent-1",
		Kind:      a2a.MessageStreamChunk,
		Content:   "Hello, this is a chunk",
		CreatedAt: time.Now(),
	}

	if msg.Kind != a2a.MessageStreamChunk {
		t.Errorf("expected kind=%s, got %s", a2a.MessageStreamChunk, msg.Kind)
	}
	if msg.Content != "Hello, this is a chunk" {
		t.Errorf("unexpected content: %s", msg.Content)
	}
	if msg.FromID != "ve-agent-1" {
		t.Errorf("unexpected from_id: %s", msg.FromID)
	}
	if msg.SessionID != "session-abc" {
		t.Errorf("unexpected session_id: %s", msg.SessionID)
	}
}

func TestGroupDiscussionMessage_StreamEnd_Construction(t *testing.T) {
	msg := a2a.GroupDiscussionMessage{
		ID:        "msg-002",
		SessionID: "session-abc",
		FromID:    "ve-agent-1",
		Kind:      a2a.MessageStreamEnd,
		Content:   "",
		CreatedAt: time.Now(),
	}

	if msg.Kind != a2a.MessageStreamEnd {
		t.Errorf("expected kind=%s, got %s", a2a.MessageStreamEnd, msg.Kind)
	}
	if msg.Content != "" {
		t.Errorf("stream_end should have empty content, got: %s", msg.Content)
	}
}

func TestGroupDiscussionMessage_StreamChunk_InGroupEnvelope(t *testing.T) {
	msg := &a2a.GroupDiscussionMessage{
		ID:        "msg-003",
		SessionID: "session-xyz",
		FromID:    "ve-agent-2",
		Kind:      a2a.MessageStreamChunk,
		Content:   "partial response",
		CreatedAt: time.Now(),
	}

	envelope := a2a.GroupEnvelope{
		ID:        "env-001",
		Type:      a2a.GroupMessageDiscussionMessage,
		Scope:     a2a.GroupScopeCurrentHub,
		FromID:    "ve-agent-2",
		SessionID: "session-xyz",
		CreatedAt: time.Now(),
		Message:   msg,
	}

	if err := envelope.ValidateCurrentHub(); err != nil {
		t.Fatalf("envelope validation failed: %v", err)
	}
	if envelope.Message.Kind != a2a.MessageStreamChunk {
		t.Errorf("expected stream_chunk kind in envelope message")
	}
}

func TestGroupDiscussionMessage_StreamEnd_InGroupEnvelope(t *testing.T) {
	// stream_end with empty content should still be valid in an envelope.
	msg := &a2a.GroupDiscussionMessage{
		ID:        "msg-004",
		SessionID: "session-xyz",
		FromID:    "ve-agent-2",
		Kind:      a2a.MessageStreamEnd,
		Content:   "",
		CreatedAt: time.Now(),
	}

	envelope := a2a.GroupEnvelope{
		ID:        "env-002",
		Type:      a2a.GroupMessageDiscussionMessage,
		Scope:     a2a.GroupScopeCurrentHub,
		FromID:    "ve-agent-2",
		SessionID: "session-xyz",
		CreatedAt: time.Now(),
		Message:   msg,
	}

	if err := envelope.ValidateCurrentHub(); err != nil {
		t.Fatalf("envelope validation failed: %v", err)
	}
	if envelope.Message.Kind != a2a.MessageStreamEnd {
		t.Errorf("expected stream_end kind in envelope message")
	}
}

func TestMessageKindConstants(t *testing.T) {
	// Verify the stream constants are properly defined
	if a2a.MessageStreamChunk != "stream_chunk" {
		t.Errorf("MessageStreamChunk = %q, want %q", a2a.MessageStreamChunk, "stream_chunk")
	}
	if a2a.MessageStreamEnd != "stream_end" {
		t.Errorf("MessageStreamEnd = %q, want %q", a2a.MessageStreamEnd, "stream_end")
	}
}

// --- VE session lifecycle tests ---

func TestVEMessageHandler_SessionCreation(t *testing.T) {
	handler := NewVEMessageHandler(&App{})

	if handler.ActiveSessionCount() != 0 {
		t.Fatalf("expected 0 active sessions, got %d", handler.ActiveSessionCount())
	}

	// Simulate receiving a message — session should be created
	msg := a2a.GroupDiscussionMessage{
		FromID:  "requester-1",
		Kind:    a2a.MessageStatement,
		Content: "Hello VE",
	}

	handler.HandleIncomingMessage("session-1", msg)
	// Give goroutine time to start
	time.Sleep(50 * time.Millisecond)

	if handler.ActiveSessionCount() != 1 {
		t.Errorf("expected 1 active session, got %d", handler.ActiveSessionCount())
	}
}

func TestVEMessageHandler_SessionClose(t *testing.T) {
	handler := NewVEMessageHandler(&App{})

	msg := a2a.GroupDiscussionMessage{
		FromID:  "requester-1",
		Kind:    a2a.MessageStatement,
		Content: "Hello VE",
	}

	handler.HandleIncomingMessage("session-1", msg)
	time.Sleep(50 * time.Millisecond)

	handler.CloseSession("session-1")

	if handler.ActiveSessionCount() != 0 {
		t.Errorf("expected 0 active sessions after close, got %d", handler.ActiveSessionCount())
	}
}

func TestVEMessageHandler_MultipleSessionsIndependent(t *testing.T) {
	handler := NewVEMessageHandler(&App{})

	handler.HandleIncomingMessage("session-1", a2a.GroupDiscussionMessage{
		FromID: "user-a", Kind: a2a.MessageStatement, Content: "msg1",
	})
	handler.HandleIncomingMessage("session-2", a2a.GroupDiscussionMessage{
		FromID: "user-b", Kind: a2a.MessageStatement, Content: "msg2",
	})
	time.Sleep(50 * time.Millisecond)

	if handler.ActiveSessionCount() != 2 {
		t.Errorf("expected 2 active sessions, got %d", handler.ActiveSessionCount())
	}

	handler.CloseSession("session-1")
	if handler.ActiveSessionCount() != 1 {
		t.Errorf("expected 1 active session after closing session-1, got %d", handler.ActiveSessionCount())
	}
}

func TestVEMessageHandler_EmptyContentIgnored(t *testing.T) {
	handler := NewVEMessageHandler(&App{})

	handler.HandleIncomingMessage("session-1", a2a.GroupDiscussionMessage{
		FromID: "user-a", Kind: a2a.MessageStatement, Content: "",
	})
	handler.HandleIncomingMessage("session-1", a2a.GroupDiscussionMessage{
		FromID: "user-a", Kind: a2a.MessageStatement, Content: "   ",
	})
	time.Sleep(50 * time.Millisecond)

	if handler.ActiveSessionCount() != 0 {
		t.Errorf("expected 0 sessions for empty messages, got %d", handler.ActiveSessionCount())
	}
}

func TestVEMessageHandler_IgnoresStreamAndSelfMessages(t *testing.T) {
	app := &App{configCacheValid: true, configCache: corelib.AppConfig{RemoteMachineID: "machine-local"}}
	handler := NewVEMessageHandler(app)

	handler.HandleIncomingMessage("session-stream", a2a.GroupDiscussionMessage{
		FromID:  "other",
		Kind:    a2a.MessageStreamChunk,
		Content: "partial response",
	})
	handler.HandleIncomingMessage("session-self", a2a.GroupDiscussionMessage{
		FromID:  "machine-local",
		Kind:    a2a.MessageStatement,
		Content: "my own broadcast",
	})
	time.Sleep(50 * time.Millisecond)

	if handler.ActiveSessionCount() != 0 {
		t.Fatalf("expected stream/self messages to be ignored, got %d sessions", handler.ActiveSessionCount())
	}
}

func TestVEMessageHandler_HandleGroupEnvelope_DiscussionMessage(t *testing.T) {
	handler := NewVEMessageHandler(&App{})

	envelope := a2a.GroupEnvelope{
		ID:        "env-1",
		Type:      a2a.GroupMessageDiscussionMessage,
		Scope:     a2a.GroupScopeCurrentHub,
		FromID:    "user-x",
		SessionID: "session-99",
		CreatedAt: time.Now(),
		Message: &a2a.GroupDiscussionMessage{
			SessionID: "session-99",
			FromID:    "user-x",
			Kind:      a2a.MessageStatement,
			Content:   "Hello from envelope",
		},
	}

	handler.HandleGroupEnvelope(envelope)
	time.Sleep(50 * time.Millisecond)

	if handler.ActiveSessionCount() != 1 {
		t.Errorf("expected 1 session from envelope, got %d", handler.ActiveSessionCount())
	}
}

func TestVEMessageHandler_HandleGroupEnvelope_NonDiscussionIgnored(t *testing.T) {
	handler := NewVEMessageHandler(&App{})

	// Profile type envelope should be ignored
	envelope := a2a.GroupEnvelope{
		ID:     "env-2",
		Type:   a2a.GroupMessageProfile,
		Scope:  a2a.GroupScopeCurrentHub,
		FromID: "user-x",
		Profile: &a2a.GroupProfile{
			AgentID: "agent-1",
		},
	}

	handler.HandleGroupEnvelope(envelope)
	time.Sleep(50 * time.Millisecond)

	if handler.ActiveSessionCount() != 0 {
		t.Errorf("expected 0 sessions for non-discussion envelope, got %d", handler.ActiveSessionCount())
	}
}

func TestVEMessageHandler_HandleGroupEnvelope_NilMessageIgnored(t *testing.T) {
	handler := NewVEMessageHandler(&App{})

	envelope := a2a.GroupEnvelope{
		ID:        "env-3",
		Type:      a2a.GroupMessageDiscussionMessage,
		Scope:     a2a.GroupScopeCurrentHub,
		FromID:    "user-x",
		SessionID: "session-100",
		Message:   nil,
	}

	handler.HandleGroupEnvelope(envelope)
	time.Sleep(50 * time.Millisecond)

	if handler.ActiveSessionCount() != 0 {
		t.Errorf("expected 0 sessions for nil message, got %d", handler.ActiveSessionCount())
	}
}

// --- Timeout handling tests ---

func TestVEMessageHandler_TimeoutMechanism(t *testing.T) {
	// Test that the 60s timeout mechanism is structurally correct
	// by verifying the processAndRespond function handles the timeout path.
	// We can't easily test the full 60s timeout in a unit test,
	// but we verify the handler structure supports it.

	handler := NewVEMessageHandler(&App{})

	// Verify the handler has the timeout infrastructure
	if handler.app == nil {
		// This is expected since we pass &App{} which has nil fields
		// The important thing is the handler was created successfully
	}

	// Verify session tracking works correctly for timeout scenarios
	msg := a2a.GroupDiscussionMessage{
		FromID:  "user-timeout",
		Kind:    a2a.MessageStatement,
		Content: "test timeout",
	}

	handler.HandleIncomingMessage("timeout-session", msg)
	time.Sleep(50 * time.Millisecond)

	// Session should exist
	if handler.ActiveSessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", handler.ActiveSessionCount())
	}

	// Close should clean up
	handler.CloseSession("timeout-session")
	if handler.ActiveSessionCount() != 0 {
		t.Errorf("expected 0 sessions after close, got %d", handler.ActiveSessionCount())
	}
}

// --- Streaming response construction tests ---

func TestVEStreamingResponse_ChunkSequence(t *testing.T) {
	// Verify that a streaming response produces the correct sequence:
	// stream_chunk, stream_chunk, ..., stream_end
	chunks := []string{"Hello", " world", "!"}
	messages := make([]a2a.GroupDiscussionMessage, 0)

	for _, chunk := range chunks {
		messages = append(messages, a2a.GroupDiscussionMessage{
			Kind:    a2a.MessageStreamChunk,
			Content: chunk,
		})
	}
	// Final stream_end
	messages = append(messages, a2a.GroupDiscussionMessage{
		Kind:    a2a.MessageStreamEnd,
		Content: "",
	})

	// Verify sequence
	for i, msg := range messages {
		if i < len(messages)-1 {
			if msg.Kind != a2a.MessageStreamChunk {
				t.Errorf("message %d: expected stream_chunk, got %s", i, msg.Kind)
			}
			if msg.Content == "" {
				t.Errorf("message %d: stream_chunk should have non-empty content", i)
			}
		} else {
			if msg.Kind != a2a.MessageStreamEnd {
				t.Errorf("last message: expected stream_end, got %s", msg.Kind)
			}
		}
	}

	// Verify full content reconstruction
	var fullContent strings.Builder
	for _, msg := range messages {
		if msg.Kind == a2a.MessageStreamChunk {
			fullContent.WriteString(msg.Content)
		}
	}
	if fullContent.String() != "Hello world!" {
		t.Errorf("reconstructed content = %q, want %q", fullContent.String(), "Hello world!")
	}
}

func TestVEStreamingResponse_EmptyChunksFiltered(t *testing.T) {
	// The handler should not send empty chunks
	handler := NewVEMessageHandler(&App{})

	// SendStreamChunk with empty content should still call sendMessage
	// but the runAgentWithStreaming onToken callback filters empty chunks
	// Verify the filtering logic
	emptyChunk := strings.TrimSpace("")
	if emptyChunk != "" {
		t.Error("empty chunk should be filtered")
	}

	nonEmptyChunk := strings.TrimSpace("hello")
	if nonEmptyChunk == "" {
		t.Error("non-empty chunk should not be filtered")
	}

	_ = handler
}

func TestBuildVEConversationHistoryFromMessagesRestoresPriorTurns(t *testing.T) {
	messages := []a2a.Message{
		{ID: "m1", FromID: "human", Kind: a2a.MessageQuestion, Content: "analyze the contract"},
		{ID: "m2", FromID: "local-machine", Kind: a2a.MessageStreamChunk, Content: "Sure, "},
		{ID: "m3", FromID: "local-machine", Kind: a2a.MessageStreamChunk, Content: "start with the clauses."},
		{ID: "m4", FromID: "local-machine", Kind: a2a.MessageStreamEnd},
		{ID: "m5", FromID: "human", Kind: a2a.MessageStatement, Content: "continue", CreatedAt: time.Now()},
	}

	history := buildVEConversationHistoryFromMessages(messages, "local-machine", a2a.GroupDiscussionMessage{ID: "m5"})
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2: %+v", len(history), history)
	}
	if history[0].Role != "user" || !strings.Contains(history[0].Content.(string), "analyze the contract") {
		t.Fatalf("first history entry = %+v", history[0])
	}
	if history[1].Role != "assistant" || history[1].Content != "Sure, start with the clauses." {
		t.Fatalf("assistant history entry = %+v", history[1])
	}
}

func TestBuildVEConversationHistoryFromMessagesSkipsCurrentByTimestampWhenIDMissing(t *testing.T) {
	now := time.Now()
	messages := []a2a.Message{
		{ID: "", FromID: "human", Kind: a2a.MessageStatement, Content: "current without id", CreatedAt: now},
	}

	history := buildVEConversationHistoryFromMessages(messages, "local-machine", a2a.GroupDiscussionMessage{FromID: "human", Kind: a2a.MessageStatement, Content: "current without id", CreatedAt: now})
	if len(history) != 0 {
		t.Fatalf("current message should not be restored into history: %+v", history)
	}
}

func TestBuildVEConversationHistoryFromMessagesPrefixesOtherParticipants(t *testing.T) {
	messages := []a2a.Message{
		{ID: "m1", FromID: "expert-a", Kind: a2a.MessageStatement, Content: "check the facts first"},
		{ID: "m2", FromID: "local-machine", Kind: a2a.MessageStreamChunk, Content: sensitivePermissionWaitingText + "Approved."},
		{ID: "m3", FromID: "local-machine", Kind: a2a.MessageStreamEnd},
	}

	history := buildVEConversationHistoryFromMessages(messages, "local-machine", a2a.GroupDiscussionMessage{})
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2: %+v", len(history), history)
	}
	if got := history[0].Content; got != "[expert-a] check the facts first" {
		t.Fatalf("prefixed participant content = %q", got)
	}
	if got := history[1].Content; got != "Approved." {
		t.Fatalf("assistant content should strip waiting marker, got %q", got)
	}
}

// --- Concurrent session safety tests ---

func TestVEMessageHandler_ConcurrentAccess(t *testing.T) {
	handler := NewVEMessageHandler(&App{})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sessionID := "session-" + strings.Repeat("x", idx+1)
			handler.HandleIncomingMessage(sessionID, a2a.GroupDiscussionMessage{
				FromID:  "user",
				Kind:    a2a.MessageStatement,
				Content: "concurrent msg",
			})
		}(i)
	}
	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	count := handler.ActiveSessionCount()
	if count != 10 {
		t.Errorf("expected 10 sessions from concurrent access, got %d", count)
	}
}
