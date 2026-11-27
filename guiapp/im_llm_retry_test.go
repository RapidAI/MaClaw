package guiapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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

func TestRetryAgentLoopLLMRequestAfterErrorResendsReasoningOnlyResponsesOutage(t *testing.T) {
	h := &IMMessageHandler{}
	partial := &llm.Response{Choices: []llm.Choice{{
		Message: llm.Message{Role: "assistant", ReasoningContent: "partial thought"},
	}}}
	streamErr := errors.New("Responses API stream failed: The server had an error while processing your request. (code=server_error)")
	loop := NewLoopContext("responses-retry", 1, nil)
	go func() {
		time.Sleep(20 * time.Millisecond)
		loop.Cancel()
	}()

	done := make(chan agentLoopLLMRetryResult, 1)
	go func() {
		done <- h.retryAgentLoopLLMRequestAfterError(
			loop,
			context.Background(),
			corelib.MaclawLLMConfig{URL: "https://example.test", Model: "grok-4.7", Protocol: "openai", WireAPI: "responses"},
			nil,
			nil,
			http.DefaultClient,
			func(string) { t.Fatal("retry must not stream before the wait is cancelled") },
			nil,
			nil,
			nil,
			&llmFirstRequestMetrics{},
			false,
			partial,
			streamErr,
			time.Time{},
		)
	}()
	select {
	case got := <-done:
		if got.RetryCount != 1 {
			t.Fatalf("retry count = %d, want 1", got.RetryCount)
		}
		if !got.Cancelled {
			t.Fatal("cancellation during the retry wait was ignored")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reasoning-only stream failure did not enter the retry wait")
	}
}

func TestRetryAgentLoopLLMRequestAfterErrorKeepsAnswerText(t *testing.T) {
	h := &IMMessageHandler{}
	partial := &llm.Response{Choices: []llm.Choice{{
		Message: llm.Message{Role: "assistant", Content: "partial answer", ReasoningContent: "partial thought"},
	}}}
	streamErr := errors.New("Responses API stream failed: The server had an error while processing your request. (code=server_error)")
	loop := NewLoopContext("responses-keep-answer", 1, nil)
	go func() {
		time.Sleep(20 * time.Millisecond)
		loop.Cancel()
	}()

	done := make(chan agentLoopLLMRetryResult, 1)
	go func() {
		done <- h.retryAgentLoopLLMRequestAfterError(
			loop,
			context.Background(),
			corelib.MaclawLLMConfig{URL: "https://example.test", Model: "grok-4.7", Protocol: "openai", WireAPI: "responses"},
			nil,
			nil,
			http.DefaultClient,
			func(string) { t.Fatal("answer text must not be resent") },
			nil,
			nil,
			nil,
			&llmFirstRequestMetrics{},
			false,
			partial,
			streamErr,
			time.Time{},
		)
	}()
	select {
	case got := <-done:
		if got.RetryCount != 0 {
			t.Fatalf("retry count = %d, want 0", got.RetryCount)
		}
		if got.Response != partial {
			t.Fatal("answer partial was replaced")
		}
		if !errors.Is(got.Err, streamErr) {
			t.Fatalf("error = %v, want %v", got.Err, streamErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("answer text blocked the retry longer than the gate allows")
	}
}

func TestLLMResponseBlocksStreamRetry(t *testing.T) {
	reasoning := &llm.Response{Choices: []llm.Choice{{Message: llm.Message{ReasoningContent: "think"}}}}
	streamErr := errors.New("Responses API stream failed: server_error (code=server_error)")
	if llmResponseBlocksStreamRetry(reasoning, streamErr) {
		t.Fatal("reasoning-only transient stream failure should be resent")
	}
	answered := &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Content: "answer", ReasoningContent: "think"}}}}
	if !llmResponseBlocksStreamRetry(answered, streamErr) {
		t.Fatal("assistant text should not be resent")
	}
	opened := &llm.Response{Choices: []llm.Choice{{Message: llm.Message{ReasoningContent: "think", ToolCalls: []llm.ToolCall{{ID: "call_1"}}}}}}
	if llmResponseBlocksStreamRetry(opened, streamErr) {
		t.Fatal("an opened tool call with no arguments should be resent")
	}
	finished := &llm.Response{Choices: []llm.Choice{{Message: llm.Message{ToolCalls: []llm.ToolCall{{
		ID:       "call_1",
		Function: llm.ToolCallFunction{Name: "bash", Arguments: `{"command":"pwd"}`},
	}}}}}}
	if !llmResponseBlocksStreamRetry(finished, streamErr) {
		t.Fatal("a tool call that already has arguments should not be resent")
	}
	if !llmResponseBlocksStreamRetry(reasoning, errors.New("connection reset")) {
		t.Fatal("reasoning still blocks an ordinary transport error")
	}
	cut := fmt.Errorf("SSE stream read error: %w", io.ErrUnexpectedEOF)
	if llmResponseBlocksStreamRetry(reasoning, cut) {
		t.Fatal("a reasoning-only truncated body should be resent")
	}
	if !llmResponseBlocksStreamRetry(answered, cut) {
		t.Fatal("assistant text should not be resent after a truncated body")
	}
	idle := errors.New("WebSocket idle timeout (4m0s): no data received from wss://example")
	if llmResponseBlocksStreamRetry(reasoning, idle) {
		t.Fatal("a reasoning-only idle stall should be resent")
	}
	blocked := errors.New("Responses API stream failed: blocked by content filter (code=content_filter)")
	if !llmResponseBlocksStreamRetry(reasoning, blocked) {
		t.Fatal("a permanent stream error should not be resent over visible reasoning")
	}
	serverClose := errors.New("WebSocket unexpected close (code=1011): websocket: close 1011 (internal server error)")
	if llmResponseBlocksStreamRetry(reasoning, serverClose) {
		t.Fatal("a reasoning-only server close should be resent")
	}
	if !llmResponseBlocksStreamRetry(answered, serverClose) {
		t.Fatal("assistant text should not be resent after a server close")
	}
	if !llmResponseBlocksStreamRetry(finished, serverClose) {
		t.Fatal("a finished tool call should not be resent after a server close")
	}
	policyClose := errors.New("WebSocket unexpected close (code=1008): websocket: close 1008 (policy violation)")
	if !llmResponseBlocksStreamRetry(reasoning, policyClose) {
		t.Fatal("a policy close should not be resent over visible reasoning")
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

func TestDropUndeliveredAssistantContentAllowsStreamRetry(t *testing.T) {
	buf := newTransactionalTokenBuffer(func(string) {})
	buf.Write("\x01thinking")
	buf.Write("hi")
	if buf.ContentDelivered() {
		t.Fatal("a short fragment was treated as delivered")
	}
	resp := &llm.Response{Choices: []llm.Choice{{Message: llm.Message{
		Content: "hi", ReasoningContent: "thinking",
	}}}}
	dropUndeliveredAssistantContent(resp, buf.ContentDelivered())
	if resp.Choices[0].Message.Content != "" || resp.Choices[0].Message.ReasoningContent != "thinking" {
		t.Fatalf("partial = %#v, want the unseen text cleared and the thinking kept", resp.Choices[0].Message)
	}
	streamErr := errors.New("Responses API stream failed: server_error (code=server_error)")
	if llmResponseBlocksStreamRetry(resp, streamErr) {
		t.Fatal("unseen assistant text blocked a transient retry")
	}

	seen := newTransactionalTokenBuffer(func(string) {})
	seen.Write(strings.Repeat("a", tokenBufferFlushThreshold))
	kept := &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Content: strings.Repeat("a", tokenBufferFlushThreshold)}}}}
	dropUndeliveredAssistantContent(kept, seen.ContentDelivered())
	if kept.Choices[0].Message.Content == "" {
		t.Fatal("text the host already rendered was cleared")
	}

	withCall := &llm.Response{Choices: []llm.Choice{{Message: llm.Message{
		Content: "hi",
		ToolCalls: []llm.ToolCall{{
			ID:       "call_1",
			Function: llm.ToolCallFunction{Name: "bash", Arguments: "{}"},
		}},
	}}}}
	dropUndeliveredAssistantContent(withCall, false)
	if withCall.Choices[0].Message.Content != "" {
		t.Fatal("unseen text stayed beside a finished call")
	}
	if !llm.ResponseHasCommittedAnswer(withCall) {
		t.Fatal("a finished call was cleared with the unseen text")
	}
}
