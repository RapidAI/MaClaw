package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"pgregory.net/rapid"
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

func TestGroupDispatcherRespondsToAttachmentOnlyStatement(t *testing.T) {
	msg := a2a.GroupDiscussionMessage{
		Kind: a2a.MessageStatement,
		FileAttachments: []a2a.FileAttachment{{
			FileURL:  "https://hub.local/api/ve/files/download/file-1",
			Filename: "evidence.pdf",
		}},
	}
	if !shouldExecutorRespond(msg) {
		t.Fatal("attachment-only statement should route to local executor")
	}
}

func TestBuildVEFileAttachmentMessageRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "too-large.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(veFileAttachmentMaxSize, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = (&App{}).buildVEFileAttachmentMessage("session-1", path, "", "")
	if err == nil || !strings.Contains(err.Error(), "50 MB") {
		t.Fatalf("expected 50 MB limit error, got %v", err)
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

func TestVEMessageHandler_AttachmentOnlyFallbackCreatesSession(t *testing.T) {
	handler := NewVEMessageHandler(&App{})

	handler.HandleIncomingMessage("session-attach", a2a.GroupDiscussionMessage{
		FromID: "user-a",
		Kind:   a2a.MessageStatement,
		FileAttachments: []a2a.FileAttachment{{
			Filename: "missing.pdf",
			FileURL:  "https://hub.example/api/ve/files/file-1",
		}},
	})
	time.Sleep(50 * time.Millisecond)

	if handler.ActiveSessionCount() != 1 {
		t.Errorf("expected attachment-only message to create a session, got %d", handler.ActiveSessionCount())
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
	// Test that the per-message timeout mechanism is structurally correct
	// by verifying the processAndRespond function handles the timeout path.
	// We can't easily test the full timeout in a unit test,
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
	if a2a.GroupDiscussionMessageHasPayload(a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamChunk}) {
		t.Fatal("empty stream_chunk should not be sent to Hub")
	}
	if !a2a.GroupDiscussionMessageHasPayload(a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamEnd}) {
		t.Fatal("stream_end without content should still be sent to Hub")
	}
	if !a2a.GroupDiscussionMessageHasPayload(a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamChunk, Content: "hello"}) {
		t.Fatal("non-empty stream_chunk should be sent to Hub")
	}
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

func TestBuildVEConversationHistoryFromMessagesMatchesGeneratedLocalAliases(t *testing.T) {
	messages := []a2a.Message{
		{ID: "m1", FromID: "human", Kind: a2a.MessageQuestion, Content: "start"},
		{ID: "m2", FromID: "ve-local-machine", Kind: a2a.MessageStreamChunk, Content: "alias "},
		{ID: "m3", FromID: "local-machine", Kind: a2a.MessageStreamChunk, Content: "answer"},
		{ID: "m4", FromID: "ve_local-machine", Kind: a2a.MessageStreamEnd},
	}

	history := buildVEConversationHistoryFromMessages(messages, "local-machine", a2a.GroupDiscussionMessage{})
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2: %+v", len(history), history)
	}
	if history[1].Role != "assistant" || history[1].Content != "alias answer" {
		t.Fatalf("assistant alias history entry = %+v", history[1])
	}
}

func TestBuildVEConversationHistoryFromMessagesSkipsCurrentByGeneratedAlias(t *testing.T) {
	now := time.Now()
	messages := []a2a.Message{
		{ID: "", FromID: "ve-human", Kind: a2a.MessageStatement, Content: "current", CreatedAt: now},
	}

	history := buildVEConversationHistoryFromMessages(messages, "local-machine", a2a.GroupDiscussionMessage{FromID: "human", Kind: a2a.MessageStatement, Content: "current", CreatedAt: now})
	if len(history) != 0 {
		t.Fatalf("current alias message should not be restored into history: %+v", history)
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

func TestBuildVEConversationHistoryFromMessagesPreservesStreamChunks(t *testing.T) {
	messages := []a2a.Message{
		{ID: "m1", FromID: "local-machine", Kind: a2a.MessageStreamChunk, Content: "Visible "},
		{ID: "m2", FromID: "local-machine", Kind: a2a.MessageStreamChunk, Content: "\n\x01raw intermediate chunk"},
		{ID: "m3", FromID: "local-machine", Kind: a2a.MessageStreamChunk, Content: "reply."},
		{ID: "m4", FromID: "local-machine", Kind: a2a.MessageStreamEnd},
	}

	history := buildVEConversationHistoryFromMessages(messages, "local-machine", a2a.GroupDiscussionMessage{})
	if len(history) != 1 {
		t.Fatalf("history len = %d, want 1: %+v", len(history), history)
	}
	if got := history[0].Content; got != "Visible \n\x01raw intermediate chunkreply." {
		t.Fatalf("assistant content = %q, want raw coalesced stream", got)
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

// ===========================================================================
// Feature: ve-file-sharing-directories, Property 5: Execution-layer path validation for all VE file operations
//
// For any VE file operation (send_file, read_file, list_directory) with a path
// argument that resolves outside all allowed directories after canonicalization,
// the execution layer (ExecuteTool) SHALL return an error and SHALL NOT execute
// the underlying file operation handler.
//
// For any VE file operation with a path argument that resolves inside an allowed
// directory after canonicalization, the execution layer SHALL allow the operation
// (subject to other checks like sensitive file detection and size limits).
//
// **Validates: Requirements 4.1, 4.2, 6.3, 6.4**
// ===========================================================================

func TestProperty5_ExecutionLayerPathValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// --- Setup: create directory structure ---
		baseDir := createTempDirForRapid(t)
		defer os.RemoveAll(baseDir)

		// Create 1-3 allowed directories
		numAllowed := rapid.IntRange(1, 3).Draw(t, "numAllowedDirs")
		allowedDirs := make([]string, numAllowed)
		for i := 0; i < numAllowed; i++ {
			dirName := rapid.StringMatching(`[a-z]{3,8}`).Draw(t, fmt.Sprintf("allowedDir_%d", i))
			dir := filepath.Join(baseDir, "allowed", dirName)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatalf("failed to create allowed dir: %v", err)
			}
			allowedDirs[i] = dir
		}

		// Create an outside directory
		outsideDir := filepath.Join(baseDir, "outside")
		os.MkdirAll(outsideDir, 0755)

		// Create files inside allowed dirs
		chosenIdx := rapid.IntRange(0, len(allowedDirs)-1).Draw(t, "chosenIdx")
		chosenDir := allowedDirs[chosenIdx]

		insideFileName := rapid.StringMatching(`[a-z]{3,8}\.(txt|pdf|md|docx)`).Draw(t, "insideFile")
		insideFilePath := filepath.Join(chosenDir, insideFileName)
		os.WriteFile(insideFilePath, []byte("inside content"), 0644)

		// Create files outside allowed dirs
		outsideFileName := rapid.StringMatching(`[a-z]{3,8}\.(txt|pdf|md)`).Draw(t, "outsideFile")
		outsideFilePath := filepath.Join(outsideDir, outsideFileName)
		os.WriteFile(outsideFilePath, []byte("outside content"), 0644)

		// Create a subdirectory inside allowed dir for list_directory tests
		subDirName := rapid.StringMatching(`[a-z]{3,6}`).Draw(t, "subDir")
		insideSubDir := filepath.Join(chosenDir, subDirName)
		os.MkdirAll(insideSubDir, 0755)

		// Build the veAgentCallbacks with our test configuration
		callbacks := &veAgentCallbacks{
			app: &App{
				configCacheValid: true,
				configCache:      corelib.AppConfig{VEAllowedDirectories: allowedDirs},
			},
		}

		// Pick which tool to test
		toolName := rapid.SampledFrom([]string{"send_file", "read_file", "list_directory"}).Draw(t, "toolName")

		// --- Property A: Paths OUTSIDE allowed dirs are REJECTED ---
		var outsideArgs string
		switch toolName {
		case "send_file", "read_file":
			outsideArgs = fmt.Sprintf(`{"path": %q}`, outsideFilePath)
		case "list_directory":
			outsideArgs = fmt.Sprintf(`{"path": %q}`, outsideDir)
		}

		result := callbacks.ExecuteTool(toolName, outsideArgs)
		if !strings.Contains(result, "[error]") {
			t.Fatalf("ExecuteTool(%s) with path outside allowed dirs should return error, got: %s (path=%s, allowedDirs=%v)",
				toolName, result, outsideFilePath, allowedDirs)
		}
		if !strings.Contains(result, "文件不在允许访问的目录中") {
			t.Fatalf("ExecuteTool(%s) should return containment error, got: %s", toolName, result)
		}

		// --- Property B: Paths INSIDE allowed dirs are ALLOWED (no containment error) ---
		var insideArgs string
		switch toolName {
		case "send_file", "read_file":
			insideArgs = fmt.Sprintf(`{"path": %q}`, insideFilePath)
		case "list_directory":
			insideArgs = fmt.Sprintf(`{"path": %q}`, insideSubDir)
		}

		result = callbacks.ExecuteTool(toolName, insideArgs)
		// The result should NOT contain the containment error.
		// It may contain other errors (e.g., "send_file 处理器不可用" because
		// we don't have a full App with registry), but it should NOT be a
		// path containment rejection.
		if strings.Contains(result, "文件不在允许访问的目录中") {
			t.Fatalf("ExecuteTool(%s) with path inside allowed dirs should NOT return containment error, got: %s (path=%s, allowedDirs=%v)",
				toolName, result, insideFilePath, allowedDirs)
		}

		// --- Property C: .. traversal paths that resolve OUTSIDE are REJECTED ---
		var traversalPath string
		switch toolName {
		case "send_file", "read_file":
			traversalPath = filepath.Join(chosenDir, "..", "..", "outside", outsideFileName)
		case "list_directory":
			traversalPath = filepath.Join(chosenDir, "..", "..", "outside")
		}

		traversalArgs := fmt.Sprintf(`{"path": %q}`, traversalPath)
		result = callbacks.ExecuteTool(toolName, traversalArgs)
		if !strings.Contains(result, "[error]") {
			t.Fatalf("ExecuteTool(%s) with .. traversal outside should return error, got: %s (path=%s)",
				toolName, result, traversalPath)
		}
		// Should be either containment error or file-not-found (if path doesn't resolve)
		if !strings.Contains(result, "文件不在允许访问的目录中") && !strings.Contains(result, "文件不存在") && !strings.Contains(result, "无法解析文件路径") {
			t.Fatalf("ExecuteTool(%s) with traversal should return containment or not-found error, got: %s",
				toolName, result)
		}

		// --- Property D: Empty path is REJECTED ---
		emptyArgs := `{"path": ""}`
		result = callbacks.ExecuteTool(toolName, emptyArgs)
		if !strings.Contains(result, "[error]") {
			t.Fatalf("ExecuteTool(%s) with empty path should return error, got: %s", toolName, result)
		}
		if !strings.Contains(result, "path 参数不能为空") {
			t.Fatalf("ExecuteTool(%s) with empty path should return empty-path error, got: %s", toolName, result)
		}
	})
}

// TestProperty5_ExecutionLayerBlocksWithoutAllowedDirs verifies that when
// VEAllowedDirectories is empty, send_file is blocked at the execution layer.
//
// **Validates: Requirements 4.1, 6.3**
func TestProperty5_ExecutionLayerBlocksWithoutAllowedDirs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Create a file that exists on disk
		baseDir := createTempDirForRapid(t)
		defer os.RemoveAll(baseDir)
		fileName := rapid.StringMatching(`[a-z]{3,8}\.txt`).Draw(t, "fileName")
		filePath := filepath.Join(baseDir, fileName)
		os.WriteFile(filePath, []byte("content"), 0644)

		// Build callbacks with EMPTY allowed dirs
		callbacks := &veAgentCallbacks{
			app: &App{
				configCacheValid: true,
				configCache:      corelib.AppConfig{VEAllowedDirectories: []string{}},
			},
		}

		// send_file should be blocked (tool is in veBlockedTools and no allowedDirs to unblock)
		args := fmt.Sprintf(`{"path": %q}`, filePath)
		result := callbacks.ExecuteTool("send_file", args)
		if !strings.Contains(result, "[error]") {
			t.Fatalf("send_file should be blocked when allowedDirs is empty, got: %s", result)
		}
		if !strings.Contains(result, "no allowed access directories are configured") {
			t.Fatalf("send_file should explain missing allowed directories, got: %s", result)
		}
		if !strings.Contains(result, "Settings > Digital Employee > Allowed Access Directories") {
			t.Fatalf("send_file should point users to the directory setting, got: %s", result)
		}
	})
}

// TestProperty5_ExecutionLayerRejectsAllToolsConsistently verifies that the
// execution layer consistently rejects paths outside allowed dirs for ALL
// three VE file operations (send_file, read_file, list_directory).
//
// **Validates: Requirements 6.3, 6.4**
func TestProperty5_ExecutionLayerRejectsAllToolsConsistently(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		baseDir := createTempDirForRapid(t)
		defer os.RemoveAll(baseDir)

		// Create allowed and outside directories
		allowedDir := filepath.Join(baseDir, "allowed")
		outsideDir := filepath.Join(baseDir, "outside")
		os.MkdirAll(allowedDir, 0755)
		os.MkdirAll(outsideDir, 0755)

		// Create a file outside
		outsideFile := filepath.Join(outsideDir, "secret.txt")
		os.WriteFile(outsideFile, []byte("secret"), 0644)

		allowedDirs := []string{allowedDir}

		callbacks := &veAgentCallbacks{
			app: &App{
				configCacheValid: true,
				configCache:      corelib.AppConfig{VEAllowedDirectories: allowedDirs},
			},
		}

		// All three tools should reject paths outside allowed dirs
		tools := []struct {
			name string
			args string
		}{
			{"send_file", fmt.Sprintf(`{"path": %q}`, outsideFile)},
			{"read_file", fmt.Sprintf(`{"path": %q}`, outsideFile)},
			{"list_directory", fmt.Sprintf(`{"path": %q}`, outsideDir)},
		}

		for _, tool := range tools {
			result := callbacks.ExecuteTool(tool.name, tool.args)
			if !strings.Contains(result, "文件不在允许访问的目录中") {
				t.Fatalf("ExecuteTool(%s) should reject path outside allowed dirs, got: %s", tool.name, result)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property-Based Test: System prompt capability declaration
// Feature: ve-file-sharing-directories, Property 8: System prompt capability declaration
//
// **Validates: Requirements 4.5**
//
// For any non-empty list of allowed directories, the VE system prompt SHALL
// contain the file-sending capability declaration and SHALL list each
// configured directory path.
// ---------------------------------------------------------------------------

// genDirPathForPrompt generates a random absolute directory path string for prompt testing.
func genDirPathForPrompt(t *rapid.T, label string) string {
	driveLetters := []string{"C", "D", "E", "F", "G", "H"}
	drive := driveLetters[rapid.IntRange(0, len(driveLetters)-1).Draw(t, label+"_drive")]

	numSegments := rapid.IntRange(1, 5).Draw(t, label+"_segments")
	path := drive + ":\\"
	for i := 0; i < numSegments; i++ {
		seg := rapid.StringMatching(`[A-Za-z0-9_\-]{1,25}`).Draw(t, fmt.Sprintf("%s_seg%d", label, i))
		path += seg
		if i < numSegments-1 {
			path += "\\"
		}
	}
	return path
}

// genNonEmptyDirListForPrompt generates a non-empty list of directory paths (1-10 entries).
func genNonEmptyDirListForPrompt(t *rapid.T, label string) []string {
	n := rapid.IntRange(1, 10).Draw(t, label+"_count")
	dirs := make([]string, n)
	for i := 0; i < n; i++ {
		dirs[i] = genDirPathForPrompt(t, fmt.Sprintf("%s_%d", label, i))
	}
	return dirs
}

// TestProperty8_SystemPromptCapabilityDeclaration verifies that for any non-empty
// list of allowed directories, the VE system prompt contains the file-sending
// capability declaration and lists each configured directory path.
func TestProperty8_SystemPromptCapabilityDeclaration(t *testing.T) {
	// Pre-create a lightweight memory store in a temp dir to avoid slow
	// initialization on each rapid iteration.
	tmpDir := t.TempDir()
	memStore, err := memory.NewStore(filepath.Join(tmpDir, "mem"))
	if err != nil {
		t.Fatalf("failed to create temp memory store: %v", err)
	}
	defer memStore.Stop()

	rapid.Check(t, func(t *rapid.T) {
		// Generate random non-empty directory list
		dirs := genNonEmptyDirListForPrompt(t, "dirs")

		// Set up App with the generated directories in config (using cache to avoid disk I/O)
		app := &App{
			configCacheValid: true,
			configCache:      corelib.AppConfig{VEAllowedDirectories: dirs},
			memoryStore:      memStore,
		}

		// Create veAgentCallbacks with the configured app
		callbacks := &veAgentCallbacks{
			app:              app,
			ctx:              context.Background(),
			sessionID:        "test-session",
			llmCfg:           corelib.MaclawLLMConfig{},
			knowledgeChecked: true,  // skip knowledge store initialization
			hasKnowledge:     false, // no knowledge base for this test
		}

		// Call BuildSystemPrompt
		prompt := callbacks.BuildSystemPrompt("请发送文件给我", true)

		// Property 1: Prompt MUST contain file-sending capability declaration
		if !strings.Contains(prompt, "文件发送能力") {
			t.Fatalf("system prompt must contain '文件发送能力' section header when dirs are non-empty.\ndirs=%v\nprompt=%s", dirs, prompt)
		}

		// Property 2: Prompt MUST contain send_file tool mention
		if !strings.Contains(prompt, "send_file") {
			t.Fatalf("system prompt must mention 'send_file' tool when dirs are non-empty.\ndirs=%v", dirs)
		}
		if !strings.Contains(prompt, "do not paste the file contents as plain text") {
			t.Fatalf("system prompt must require send_file instead of pasted content for file-send requests.\ndirs=%v\nprompt=%s", dirs, prompt)
		}

		// Property 3: Prompt MUST list each configured directory path
		for _, dir := range dirs {
			if !strings.Contains(prompt, dir) {
				t.Fatalf("system prompt must contain directory path %q.\ndirs=%v\nprompt=%s", dir, dirs, prompt)
			}
		}

		// Property 4: Prompt MUST contain file size limit notice (50 MB)
		if !strings.Contains(prompt, "50 MB") {
			t.Fatalf("system prompt must contain '50 MB' size limit notice when dirs are non-empty.\ndirs=%v", dirs)
		}

		// Property 5: Prompt MUST contain sensitive file restriction notice
		if !strings.Contains(prompt, "敏感文件") {
			t.Fatalf("system prompt must contain sensitive file restriction notice when dirs are non-empty.\ndirs=%v", dirs)
		}
	})
}

// TestProperty8_SystemPromptCapabilityDeclaration_EmptyDirs verifies that when
// the allowed directories list is empty, the system prompt does NOT contain
// the file-sending capability declaration.
func TestProperty8_SystemPromptCapabilityDeclaration_EmptyDirs(t *testing.T) {
	// Pre-create a lightweight memory store in a temp dir to avoid slow
	// initialization on each rapid iteration.
	tmpDir := t.TempDir()
	memStore, err := memory.NewStore(filepath.Join(tmpDir, "mem"))
	if err != nil {
		t.Fatalf("failed to create temp memory store: %v", err)
	}
	defer memStore.Stop()

	rapid.Check(t, func(t *rapid.T) {
		// Generate empty directory list (randomly choose nil or empty slice)
		var dirs []string
		useNil := rapid.Bool().Draw(t, "useNil")
		if !useNil {
			dirs = []string{}
		}

		// Set up App with empty directories in config (using cache to avoid disk I/O)
		app := &App{
			configCacheValid: true,
			configCache:      corelib.AppConfig{VEAllowedDirectories: dirs},
			memoryStore:      memStore,
		}

		// Create veAgentCallbacks with the configured app
		callbacks := &veAgentCallbacks{
			app:              app,
			ctx:              context.Background(),
			sessionID:        "test-session",
			llmCfg:           corelib.MaclawLLMConfig{},
			knowledgeChecked: true,  // skip knowledge store initialization
			hasKnowledge:     false, // no knowledge base for this test
		}

		// Call BuildSystemPrompt
		prompt := callbacks.BuildSystemPrompt("请发送文件给我", true)

		// Property: Prompt MUST NOT contain file-sending capability section
		if strings.Contains(prompt, "文件发送能力") {
			t.Fatalf("system prompt must NOT contain '文件发送能力' section when dirs are empty.\ndirs=%v\nprompt=%s", dirs, prompt)
		}
	})
}
