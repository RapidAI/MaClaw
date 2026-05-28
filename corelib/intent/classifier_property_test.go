package intent

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// ============================================================================
// Feature: gui-startup-response-optimization, Property 11: UIC timeout degradation
// For any message where the UIC tree channel classification exceeds the configured
// timeout, the UIC SHALL return a result with Degraded=true, allowing downstream
// processing to continue without blocking.
// **Validates: Requirements 4.2**
// ============================================================================

// slowLLMClassifyFunc simulates an LLM classify function that takes longer than
// the configured timeout.
func slowLLMClassifyFunc(delay time.Duration) LLMClassifyFunc {
	return func(systemPrompt, userText string) (string, error) {
		time.Sleep(delay)
		return `{"label":"coding","confidence":0.9,"workflow_type":"coding"}`, nil
	}
}

// TestProperty11_UICTimeoutDegradation verifies that when the UIC tree channel
// classification exceeds the configured timeout, the result is degraded.
func TestProperty11_UICTimeoutDegradation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random messages.
		msg := rapid.OneOf(
			rapid.StringMatching(`[a-zA-Z ]{10,50}`),
			rapid.StringMatching(`[\x{4e00}-\x{9fff}]{5,20}`),
			rapid.Just("帮我开发一个贪吃蛇游戏"),
			rapid.Just("design a REST API for user management"),
		).Draw(t, "message")

		// Generate delay that exceeds the configured tree deadline without making
		// property runs slow.
		delayMs := rapid.IntRange(30, 60).Draw(t, "delayMs")
		delay := time.Duration(delayMs) * time.Millisecond

		// In LLM-only mode, the configured timeout still bounds classification.
		uic := New(Config{
			LLMFunc:    slowLLMClassifyFunc(delay),
			LLMTimeout: 10 * time.Millisecond,
		})

		// Classify in LLM-only mode. The LLM function sleeps longer than the
		// configured deadline, so Classify should return a degraded result instead
		// of waiting for the slow call to finish.
		done := make(chan ClassificationResult, 1)
		go func() {
			done <- uic.Classify(MessageContext{Text: msg})
		}()

		// Property: classification completes within a bounded time.
		select {
		case result := <-done:
			// Property 1: result is degraded because tree reasoning exceeded its
			// configured deadline.
			if !result.Degraded {
				t.Fatalf("expected Degraded=true after tree timeout, got primary=%s conf=%.2f reason=%q",
					result.Primary, result.Confidence, result.Reason)
			}

			// Property 2: result has a valid Primary label (usable for downstream).
			if result.Primary == "" {
				t.Fatalf("result has empty Primary label for message %q", msg)
			}

			// Property 3: confidence is bounded [0, 1].
			if result.Confidence < 0 || result.Confidence > 1.0 {
				t.Fatalf("confidence %.2f out of bounds for message %q", result.Confidence, msg)
			}

		case <-time.After(120 * time.Millisecond):
			t.Fatal("UIC Classify blocked for >120ms; should use configured timeout")
		}
	})
}

// TestProperty11_UICTimeoutDegradation_LLMOnlyPath verifies that when only the
// LLM channel is available and it returns an unparseable response, the UIC
// returns a degraded result with Degraded=true.
func TestProperty11_UICTimeoutDegradation_LLMOnlyPath(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate messages that won't match any L1 keywords.
		msg := rapid.StringMatching(`[a-z]{15,30}`).Draw(t, "randomMsg")

		// LLM that returns quickly but with unparseable output.
		uic := New(Config{
			LLMFunc: func(systemPrompt, userText string) (string, error) {
				time.Sleep(5 * time.Millisecond)
				return "invalid_response_format", nil
			},
			LLMTimeout: 100 * time.Millisecond,
		})

		result := uic.Classify(MessageContext{Text: msg})

		// Property: when LLM returns unparseable response, result is degraded.
		if !result.Degraded {
			t.Fatalf("expected Degraded=true for unparseable LLM response, got primary=%s conf=%.2f",
				result.Primary, result.Confidence)
		}

		// Property: confidence should be low for degraded result.
		if result.Confidence > 0.5 {
			t.Fatalf("expected low confidence for degraded result, got %.2f for message %q",
				result.Confidence, msg)
		}
	})
}

// TestProperty11_UICTimeout_DoesNotBlock verifies that the UIC does not block
// indefinitely when the LLM function hangs for a long time.
func TestProperty11_UICTimeout_DoesNotBlock(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		msg := rapid.StringMatching(`[a-zA-Z ]{5,50}`).Draw(t, "msg")

		// LLM that hangs longer than the configured tree deadline.
		uic := New(Config{
			LLMFunc:    slowLLMClassifyFunc(60 * time.Millisecond),
			LLMTimeout: 10 * time.Millisecond,
		})

		done := make(chan ClassificationResult, 1)
		go func() {
			done <- uic.Classify(MessageContext{Text: msg})
		}()

		// Property: classification completes within a bounded time. The LLM mock
		// sleeps longer than the timeout, so Classify should return before it finishes.
		select {
		case result := <-done:
			// Property: classification completed without waiting for the slow LLM.
			if result.Primary == "" {
				t.Fatalf("result has empty Primary label")
			}
		case <-time.After(120 * time.Millisecond):
			t.Fatal("UIC Classify blocked for >120ms; appears to ignore configured timeout")
		}
	})
}
