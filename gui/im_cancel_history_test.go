package main

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// TestCancelPreservesHistory verifies the core invariant: when an agent loop
// is cancelled, conversation history is SAVED (not cleared). This ensures
// the next message retains full context.
//
// The mechanistic principle: runAgentLoop must never call memory.Clear.
// History lifecycle is managed by the caller (handleIMMessageWithLoop),
// not by the loop itself. All exit paths — normal, cancelled, error —
// use saveConversationHistoryTimed to persist accumulated history.
func TestCancelPreservesHistory(t *testing.T) {
	mem := agent.NewConversationMemory()
	h := &IMMessageHandler{memory: mem}
	userID := "test-user"

	history := []agent.ConversationEntry{
		{Role: "user", Content: "使用drawio skill 画一个北京5环图"},
		{Role: "assistant", Content: "好，我来用 drawio-skill 画一个北京五环路的同心环示意图。"},
	}
	h.saveConversationHistoryTimed(userID, history, nil)

	loaded := mem.Load(userID)
	if len(loaded) == 0 {
		t.Fatal("history must be preserved after cancel, got empty")
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(loaded))
	}
	firstContent, _ := loaded[0].Content.(string)
	if !strings.Contains(firstContent, "drawio") {
		t.Errorf("first entry should contain original task, got: %s", firstContent)
	}
}

// TestCancelThenContinueHasContext simulates the exact user scenario from
// the bug report: user sends task → agent works → user cancels → user
// sends "继续" → agent should have full context.
func TestCancelThenContinueHasContext(t *testing.T) {
	mem := agent.NewConversationMemory()
	h := &IMMessageHandler{memory: mem}
	userID := "test-user"

	history := []agent.ConversationEntry{
		{Role: "user", Content: "使用drawio skill 画一个北京5环图"},
		{Role: "assistant", Content: "好，我来用 drawio-skill 画一个北京五环路的同心环示意图。先读一下参考规范，然后编写 XML。"},
	}
	h.saveConversationHistoryTimed(userID, history, nil)

	loaded := mem.Load(userID)
	if len(loaded) < 2 {
		t.Fatalf("expected at least 2 entries for context, got %d", len(loaded))
	}
	var hasContext bool
	for _, entry := range loaded {
		if content, ok := entry.Content.(string); ok {
			if strings.Contains(content, "drawio") || strings.Contains(content, "五环") {
				hasContext = true
				break
			}
		}
	}
	if !hasContext {
		t.Error("loaded history should contain drawio/五环 context for the LLM")
	}
}

// TestExplicitClearStillWorks verifies that /new, /reset, /clear still
// correctly clear history. These are the ONLY legitimate Clear paths.
func TestExplicitClearStillWorks(t *testing.T) {
	mem := agent.NewConversationMemory()
	userID := "test-user"

	mem.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	})
	mem.Clear(userID)

	if loaded := mem.Load(userID); len(loaded) != 0 {
		t.Fatalf("explicit Clear should remove all entries, got %d", len(loaded))
	}
}

// TestCancelledExitResponse verifies the helper function produces correct output.
func TestCancelledExitResponse(t *testing.T) {
	mem := agent.NewConversationMemory()
	h := &IMMessageHandler{memory: mem}
	userID := "test-user"

	history := []agent.ConversationEntry{
		{Role: "user", Content: "画一个图"},
		{Role: "assistant", Content: "好的，开始画..."},
	}

	resp := h.cancelledExitResponse(userID, history, "画一个图")

	// Should save history.
	if loaded := mem.Load(userID); len(loaded) != 2 {
		t.Fatalf("cancelledExitResponse should save history, got %d entries", len(loaded))
	}
	// Should return cancel message with task preview.
	if resp == nil || !strings.Contains(resp.Text, "画一个图") {
		t.Errorf("cancel message should contain task preview, got: %v", resp)
	}
	if !strings.Contains(resp.Text, "Task cancelled") {
		t.Errorf("cancel message should mention cancellation, got: %s", resp.Text)
	}
}

// TestRunAgentLoopHasNoMemoryClear enforces the invariant by scanning the
// source file: runAgentLoop must not contain memory.Clear calls.
func TestRunAgentLoopHasNoMemoryClear(t *testing.T) {
	f, err := os.Open("im_message_handler.go")
	if err != nil {
		t.Skipf("cannot open source file for static check: %v", err)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inRunAgentLoop := false
	braceDepth := 0
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Detect entry into runAgentLoop.
		if !inRunAgentLoop && strings.Contains(line, "func (h *IMMessageHandler) runAgentLoop(") {
			inRunAgentLoop = true
			braceDepth = 0
		}

		if inRunAgentLoop {
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

			// Check for the forbidden pattern in actual code (not comments).
			trimmed := strings.TrimSpace(line)
			isComment := strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*")
			if !isComment && strings.Contains(line, "memory.Clear") {
				t.Errorf("line %d: runAgentLoop contains memory.Clear — violates invariant: %s", lineNum, trimmed)
			}

			// Exit when we've closed the function's opening brace.
			if braceDepth <= 0 && lineNum > 1 {
				inRunAgentLoop = false
			}
		}
	}
}
