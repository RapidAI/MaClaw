package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestNormalizeAgentLoopToolArgumentsJSON(t *testing.T) {
	if got := normalizeAgentLoopToolArgumentsJSON(""); got != "{}" {
		t.Fatalf("empty args normalize to %q, want {}", got)
	}
	if got := normalizeAgentLoopToolArgumentsJSON(" \n\t "); got != "{}" {
		t.Fatalf("blank args normalize to %q, want {}", got)
	}
	if got := normalizeAgentLoopToolArgumentsJSON(`{"path":"README.md"}`); got != `{"path":"README.md"}` {
		t.Fatalf("non-empty args changed to %q", got)
	}
}

func TestExecuteToolDetailedBlankArgumentsDoNotParseFail(t *testing.T) {
	h := &IMMessageHandler{}
	result := h.executeToolDetailedWithRuntimeState("", false, "", "unknown_tool_for_blank_args", " \n\t ", "", nil)
	if result.FailureKind == toolFailureArgumentParse {
		t.Fatalf("blank args should not cause argument parse failure: %+v", result)
	}
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailureUnknownTool {
		t.Fatalf("result = %+v, want unknown tool failure", result)
	}
}

func TestExecuteAgentLoopToolCallRejectsInvalidJSONBeforeRegistry(t *testing.T) {
	h := &IMMessageHandler{}
	result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		ToolCall: llm.ToolCall{ID: "call_bad_json", Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"path":"a.go","content":"` + strings.Repeat("x", 9000),
		}},
	})
	if result.FailureKind != toolFailureArgumentParse {
		t.Fatalf("failure kind = %q text=%q, want argument parse", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "appears truncated") || strings.Contains(result.Text, "argument parse failed:") {
		t.Fatalf("result should guide truncated JSON retry without generic parse text: %q", result.Text)
	}
}

func TestExecuteAgentLoopToolCallRejectsNonObjectJSONBeforeRegistry(t *testing.T) {
	h := &IMMessageHandler{}
	for _, args := range []string{`[]`, `null`, `"text"`} {
		result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
			ToolCall: llm.ToolCall{ID: "call_non_object", Function: llm.ToolCallFunction{
				Name:      "read_file",
				Arguments: args,
			}},
		})
		if result.FailureKind != toolFailureArgumentParse {
			t.Fatalf("args %s failure kind = %q text=%q, want argument parse", args, result.FailureKind, result.Text)
		}
		if !strings.Contains(result.Text, "valid JSON object") || strings.Contains(strings.ToLower(result.Text), "argument parse failed") {
			t.Fatalf("result should guide JSON object retry for %s: %q", args, result.Text)
		}
	}
}

type imToolExecutionContextKey string

func TestExecuteToolDetailedInvokesContextHandler(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "ctx_tool",
		HandlerProg: func(args map[string]interface{}, onProgress coretool.ProgressCallback) string {
			t.Fatal("HandlerProg should not run when HandlerCtx is present")
			return ""
		},
		HandlerCtx: func(ctx context.Context, args map[string]interface{}, onProgress coretool.ProgressCallback) string {
			if got, _ := ctx.Value(imToolExecutionContextKey("marker")).(string); got != "ok" {
				t.Fatalf("context marker = %q, want ok", got)
			}
			return "ctx handler"
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	h := &IMMessageHandler{registry: registry}
	ctx := context.WithValue(context.Background(), imToolExecutionContextKey("marker"), "ok")
	result := h.executeToolDetailedWithRuntimeContext(ctx, "", false, "", "ctx_tool", `{}`, "", nil)
	if result.Text != "ctx handler" || result.Outcome != toolOutcomeSucceeded {
		t.Fatalf("result = %+v, want ctx handler success", result)
	}
}

func TestExecuteToolDetailedDoesNotStartHandlerWhenContextAlreadyCancelled(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "ctx_pre_cancel_tool",
		HandlerProg: func(args map[string]interface{}, onProgress coretool.ProgressCallback) string {
			t.Fatal("handler should not run after context cancellation")
			return ""
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	h := &IMMessageHandler{registry: registry}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := h.executeToolDetailedWithRuntimeContext(ctx, "", false, "", "ctx_pre_cancel_tool", `{}`, "", nil)
	if result.Text != context.Canceled.Error() || result.Outcome != toolOutcomeFailed {
		t.Fatalf("result = %+v, want context canceled failure", result)
	}
}

func TestExecuteAgentLoopToolCallCancelsContextHandlerOnReplan(t *testing.T) {
	started := make(chan struct{})
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "ctx_cancel_tool",
		HandlerCtx: func(ctx context.Context, args map[string]interface{}, onProgress coretool.ProgressCallback) string {
			close(started)
			<-ctx.Done()
			return ctx.Err().Error()
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	h := &IMMessageHandler{registry: registry}
	loopCtx := NewLoopContext("ctx-cancel", 3, nil)
	done := make(chan toolExecutionResult, 1)
	go func() {
		done <- h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
			Context: loopCtx,
			ToolCall: llm.ToolCall{ID: "call_ctx_cancel", Function: llm.ToolCallFunction{
				Name:      "ctx_cancel_tool",
				Arguments: `{}`,
			}},
		})
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("context handler did not start")
	}
	loopCtx.RequestReplan()

	select {
	case result := <-done:
		if result.Text != context.Canceled.Error() || result.Outcome != toolOutcomeFailed {
			t.Fatalf("result = %+v, want context canceled failure", result)
		}
	case <-time.After(time.Second):
		t.Fatal("tool call did not stop after replan")
	}
}

func TestLoopCycleExecuteToolCancelsContextHandler(t *testing.T) {
	started := make(chan struct{})
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "loop_ctx_cancel_tool",
		HandlerCtx: func(ctx context.Context, args map[string]interface{}, onProgress coretool.ProgressCallback) string {
			close(started)
			<-ctx.Done()
			return ctx.Err().Error()
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	parent := &guiLoopCommandCallbacks{
		handler:  &IMMessageHandler{registry: registry},
		cancelCh: make(chan struct{}),
	}
	cb := &loopCycleCallbacks{parent: parent}
	done := make(chan string, 1)
	go func() {
		done <- cb.ExecuteTool("loop_ctx_cancel_tool", `{}`)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("context handler did not start")
	}
	parent.Cancel()

	select {
	case result := <-done:
		if result != context.Canceled.Error() {
			t.Fatalf("result = %q, want context canceled", result)
		}
	case <-time.After(time.Second):
		t.Fatal("tool call did not stop after loop cancel")
	}
}
