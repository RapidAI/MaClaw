package agent

import (
	"fmt"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// ============================================================================
// Feature: gui-startup-response-optimization, Property 13: Task-Context skip for short history
// For any conversation with fewer than 5 history entries, the Entry_Context resolver
// SHALL skip the Task_Context_LLM call and default to TaskNew action without any
// LLM invocation.
// **Validates: Requirements 5.1**
// ============================================================================

func TestProperty13_TaskContextSkipForShortHistory(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random number of history entries (0 to 4)
		numEntries := rapid.IntRange(0, 4).Draw(t, "numEntries")

		// Generate random conversation entries
		history := make([]ConversationEntry, numEntries)
		for i := range history {
			role := rapid.SampledFrom([]string{"user", "assistant"}).Draw(t, "role")
			content := rapid.StringMatching(`[\x{4e00}-\x{9fff}a-zA-Z ]{1,50}`).Draw(t, "content")
			history[i] = ConversationEntry{Role: role, Content: content}
		}

		// Generate a random user message
		userMsg := rapid.OneOf(
			rapid.StringMatching(`[a-zA-Z ]{5,50}`),
			rapid.StringMatching(`[\x{4e00}-\x{9fff}]{3,20}`),
			rapid.Just("帮我搜索论文"),
			rapid.Just("开发一个游戏"),
			rapid.Just("继续"),
		).Draw(t, "userMsg")

		// Create a mock LLM classifier that tracks calls
		llm := &mockLLMClassifier{response: "continue"}
		mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)

		// Resolve with short history
		d := mgr.Resolve(ResolveInput{
			UserMessage: userMsg,
			History:     history,
			LastAccess:  time.Now().Add(-5 * time.Minute),
		})

		// Property 1: LLM classifier is NOT called for short history
		if llm.calls != 0 {
			t.Fatalf("LLM classifier called %d times for history with %d entries (< 5) — should be skipped",
				llm.calls, numEntries)
		}

		// Property 2: default action is TaskNew
		if d.Action != TaskNew {
			t.Fatalf("expected TaskNew for short history (%d entries), got %s (source=%s)",
				numEntries, d.Action, d.Source)
		}

		// Property 3: source indicates structural decision (not LLM)
		if d.Source == "llm" {
			t.Fatalf("source should not be 'llm' for short history skip, got %s", d.Source)
		}
	})
}

// TestProperty13_EmptyHistory verifies the edge case of completely empty history.
func TestProperty13_EmptyHistory(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userMsg := rapid.StringMatching(`[a-zA-Z\x{4e00}-\x{9fff} ]{1,30}`).Draw(t, "msg")

		llm := &mockLLMClassifier{response: "new"}
		mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)

		d := mgr.Resolve(ResolveInput{
			UserMessage: userMsg,
			History:     nil, // empty
		})

		// Property: empty history → TaskNew, no LLM call
		if llm.calls != 0 {
			t.Fatalf("LLM called for nil history")
		}
		if d.Action != TaskNew {
			t.Fatalf("expected TaskNew for nil history, got %s", d.Action)
		}
	})
}

// TestProperty13_ExactlyFiveEntries_UsesLLM verifies the boundary: exactly 5 entries
// DOES use the LLM (threshold is "fewer than 5", not "fewer than or equal to 5").
func TestProperty13_ExactlyFiveEntries_UsesLLM(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate exactly 5 history entries
		history := make([]ConversationEntry, 5)
		for i := range history {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			content := rapid.StringMatching(`[a-zA-Z ]{5,30}`).Draw(t, "content")
			history[i] = ConversationEntry{Role: role, Content: content}
		}

		userMsg := rapid.StringMatching(`[a-zA-Z ]{10,40}`).Draw(t, "msg")

		llm := &mockLLMClassifier{response: "continue"}
		mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)

		d := mgr.Resolve(ResolveInput{
			UserMessage: userMsg,
			History:     history,
			LastAccess:  time.Now().Add(-10 * time.Minute),
		})

		// Property: with exactly 5 entries, LLM IS called
		if llm.calls == 0 {
			t.Fatal("LLM not called for exactly 5 history entries — should be called")
		}

		// Property: result reflects LLM decision
		if d.Action != TaskContinue {
			t.Fatalf("expected TaskContinue from LLM, got %s", d.Action)
		}
	})
}

// ============================================================================
// Feature: gui-startup-response-optimization, Property 14: Task-Context failure fallback
// For any Task_Context_LLM invocation that times out (exceeds 2 seconds) or returns
// an error, the Entry_Context resolver SHALL default to TaskContinue action as the
// conservative assumption.
// **Validates: Requirements 5.4**
// ============================================================================

func TestProperty14_TaskContextFailureFallback(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random error messages
		errMsg := rapid.OneOf(
			rapid.Just("timeout"),
			rapid.Just("context deadline exceeded"),
			rapid.Just("connection refused"),
			rapid.Just("LLM service unavailable"),
			rapid.Just("HTTP 429: rate limit exceeded"),
			rapid.Just("HTTP 502: bad gateway"),
			rapid.Just("unexpected EOF"),
			rapid.StringMatching(`[a-z ]{5,30}`),
		).Draw(t, "errorMsg")

		// Generate history with >= 5 entries (to trigger LLM path)
		numEntries := rapid.IntRange(5, 20).Draw(t, "numEntries")
		history := make([]ConversationEntry, numEntries)
		for i := range history {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			history[i] = ConversationEntry{
				Role:    role,
				Content: rapid.StringMatching(`[a-zA-Z ]{5,30}`).Draw(t, "content"),
			}
		}

		userMsg := rapid.StringMatching(`[a-zA-Z\x{4e00}-\x{9fff} ]{5,40}`).Draw(t, "msg")

		// Create LLM classifier that returns an error
		llm := &mockLLMClassifier{err: fmt.Errorf("%s", errMsg)}
		mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)

		d := mgr.Resolve(ResolveInput{
			UserMessage: userMsg,
			History:     history,
			LastAccess:  time.Now().Add(-10 * time.Minute),
		})

		// Property 1: LLM was called (history >= 5)
		if llm.calls == 0 {
			t.Fatalf("LLM not called for history with %d entries", numEntries)
		}

		// Property 2: on error, default to TaskContinue (conservative)
		if d.Action != TaskContinue {
			t.Fatalf("expected TaskContinue on LLM error %q, got %s (source=%s)",
				errMsg, d.Action, d.Source)
		}

		// Property 3: source indicates fallback
		if d.Source != "fallback" {
			t.Fatalf("expected source=fallback on LLM error, got %s", d.Source)
		}
	})
}

// TestProperty14_TaskContextTimeout verifies the timeout-specific fallback behavior.
func TestProperty14_TaskContextTimeout(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate timeout-related error messages
		errMsg := rapid.OneOf(
			rapid.Just("context deadline exceeded"),
			rapid.Just("timeout"),
			rapid.Just("i/o timeout"),
			rapid.Just("net/http: request canceled"),
		).Draw(t, "timeoutErr")

		history := make([]ConversationEntry, 6)
		for i := range history {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			history[i] = ConversationEntry{Role: role, Content: "msg " + fmt.Sprint(i)}
		}

		llm := &mockLLMClassifier{err: fmt.Errorf("%s", errMsg)}
		mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)

		d := mgr.Resolve(ResolveInput{
			UserMessage: "继续处理",
			History:     history,
			LastAccess:  time.Now().Add(-5 * time.Minute),
		})

		// Property: timeout errors default to TaskContinue
		if d.Action != TaskContinue {
			t.Fatalf("expected TaskContinue on timeout error %q, got %s", errMsg, d.Action)
		}
	})
}

// TestProperty14_VariousErrorTypes verifies that ALL error types (not just timeout)
// result in TaskContinue fallback.
func TestProperty14_VariousErrorTypes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate completely random error messages
		errMsg := rapid.StringMatching(`[a-zA-Z0-9: ]{5,60}`).Draw(t, "randomErr")

		history := make([]ConversationEntry, 7)
		for i := range history {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			history[i] = ConversationEntry{Role: role, Content: "entry " + fmt.Sprint(i)}
		}

		llm := &mockLLMClassifier{err: fmt.Errorf("%s", errMsg)}
		mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)

		d := mgr.Resolve(ResolveInput{
			UserMessage: "做点什么",
			History:     history,
			LastAccess:  time.Now().Add(-10 * time.Minute),
		})

		// Property: ANY error type defaults to TaskContinue
		if d.Action != TaskContinue {
			t.Fatalf("expected TaskContinue for error %q, got %s (source=%s)",
				errMsg, d.Action, d.Source)
		}
	})
}
