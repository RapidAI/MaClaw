package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestRetryAgentLoopLLMRequestAfterErrorKeepsPartialThinking(t *testing.T) {
	h := &IMMessageHandler{}
	partial := &llm.Response{Choices: []llm.Choice{{
		Message: llm.Message{Role: "assistant", ReasoningContent: "partial thought"},
	}}}
	readErr := errors.New("connection reset")

	got := h.retryAgentLoopLLMRequestAfterError(
		&LoopContext{},
		context.Background(),
		corelib.MaclawLLMConfig{URL: "https://example.test", Model: "glm-5.3", Protocol: "anthropic"},
		nil,
		nil,
		http.DefaultClient,
		func(string) { t.Fatal("retry must not stream again after visible thinking") },
		nil,
		nil,
		nil,
		&llmFirstRequestMetrics{},
		false,
		partial,
		readErr,
		time.Time{},
	)
	if got.Response != partial {
		t.Fatalf("response = %#v, want original partial", got.Response)
	}
	if !errors.Is(got.Err, readErr) {
		t.Fatalf("error = %v, want %v", got.Err, readErr)
	}
	if got.RetryCount != 0 {
		t.Fatalf("retry count = %d, want 0", got.RetryCount)
	}
}

func TestLLMResponseHasVisibleOutput(t *testing.T) {
	if llmResponseHasVisibleOutput(nil) {
		t.Fatal("nil response should not look visible")
	}
	if llmResponseHasVisibleOutput(&llm.Response{Choices: []llm.Choice{{Message: llm.Message{Role: "assistant"}}}}) {
		t.Fatal("empty assistant message should not look visible")
	}
	if !llmResponseHasVisibleOutput(&llm.Response{Choices: []llm.Choice{{
		Message: llm.Message{ReasoningContent: "think"},
	}}}) {
		t.Fatal("thinking should count as visible output")
	}
}
