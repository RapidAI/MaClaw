package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestPromoteMCPNestedArgsFromCallEnvelopeCopiesTopLevelQuery(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"query"},
		"properties": map[string]interface{}{
			"query":       map[string]interface{}{"type": "string"},
			"max_results": map[string]interface{}{"type": "integer"},
		},
	}
	got := promoteMCPNestedArgsFromCallEnvelope(map[string]interface{}{
		"server_id":                      "ews",
		"tool_name":                      "find_person",
		"query":                          "王展毅",
		"max_results":                    float64(99),
		registeredToolPolicyOwnerIDField: "owner",
	}, map[string]interface{}{"max_results": float64(50)}, schema)
	if got["query"] != "王展毅" {
		t.Fatalf("query = %#v, want 王展毅", got["query"])
	}
	if got["max_results"] != float64(50) {
		t.Fatalf("nested max_results should win, got %#v", got["max_results"])
	}
	if _, ok := got[registeredToolPolicyOwnerIDField]; ok {
		t.Fatal("internal envelope fields must not be promoted")
	}
}

func TestPromoteMCPNestedArgsFromCallEnvelopeTopLevelWinsOverBlankNested(t *testing.T) {
	schema := map[string]interface{}{
		"type":       "object",
		"required":   []interface{}{"query"},
		"properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}},
	}
	got := promoteMCPNestedArgsFromCallEnvelope(map[string]interface{}{
		"server_id": "ews",
		"tool_name": "find_person",
		"query":     "王展毅",
	}, map[string]interface{}{"query": "  "}, schema)
	if got["query"] != "王展毅" {
		t.Fatalf("top-level query should replace blank nested query, got %#v", got["query"])
	}
}

func TestMCPToolArgumentsFromAnyDoesNotAliasInputMap(t *testing.T) {
	src := map[string]interface{}{"tool_name": "find_person", "query": "王展毅"}
	got, err := mcpToolArgumentsFromAny(src)
	if err != nil {
		t.Fatalf("mcpToolArgumentsFromAny: %v", err)
	}
	got["tool_name"] = "other"
	if src["tool_name"] != "find_person" {
		t.Fatal("parsing arguments must not mutate the caller map")
	}
}

func TestPromoteMCPNestedArgsFromCallEnvelopeSkipsBlankQuery(t *testing.T) {
	schema := map[string]interface{}{
		"type":       "object",
		"required":   []interface{}{"query"},
		"properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}},
	}
	got := promoteMCPNestedArgsFromCallEnvelope(map[string]interface{}{
		"server_id": "ews",
		"tool_name": "find_person",
		"query":     "   ",
	}, map[string]interface{}{}, schema)
	if _, ok := got["query"]; ok {
		t.Fatalf("blank top-level query must not be promoted, got %#v", got)
	}
}

func TestInferRegisteredToolOutcomeRecognizesMCPError(t *testing.T) {
	if inferRegisteredToolOutcome("[MCP ERROR] server=ews tool=find_person code=validation\nRequired parameter 'query' (string) is missing") != toolOutcomeFailed {
		t.Fatal("MCP validation text must be a failed tool outcome")
	}
	if inferRegisteredToolOutcome("MCP call failed: runtime owner is missing; isolated runtime will not fall back to desktop owner") != toolOutcomeFailed {
		t.Fatal("MCP runtime-owner failure must be a failed tool outcome")
	}
	for _, text := range []string{
		"MCP 调用失败: unknown server。可先用 list_mcp_tools 查看 Name (ID)",
		"MCP 调用被拒绝: \"ssh\" 是 MaClaw 内置工具，不是 MCP Server。",
		"缺少 server_id 或 tool_name 参数；server_id 支持 MCP Server 的 ID 或 Name",
		"arguments JSON 解析失败: unexpected end of JSON",
		"本地 MCP Manager 未初始化",
		"MCP Registry 未初始化",
	} {
		if inferRegisteredToolOutcome(text) != toolOutcomeFailed {
			t.Fatalf("MCP failure %q must be a failed tool outcome", text)
		}
	}
}

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

func TestExecuteAgentLoopToolCallRejectsComputerUseForCurrentLocalFileWork(t *testing.T) {
	called := false
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "computer_observe",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "desktop observed"
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := &IMMessageHandler{registry: registry}
	ctx := NewLoopContext("local-file-execution", 1, nil)
	ctx.ComputerUseBlockedForLocalFileWork = true
	recorded := false
	result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		Context:  ctx,
		UserText: "请读取附件",
		ToolCall: llm.ToolCall{ID: "call_computer", Function: llm.ToolCallFunction{
			Name:      "computer_observe",
			Arguments: `{}`,
		}},
		RecordToolCall: func(id, name, args string) {
			recorded = id == "call_computer" && name == "computer_observe" && args == "{}"
		},
	})
	if !recorded {
		t.Fatal("policy-rejected Computer Use call must still be recorded for trajectory pairing")
	}
	if called {
		t.Fatal("Computer Use handler must not run for local-file work")
	}
	if result.FailureKind != toolFailurePolicyRejected || !strings.Contains(result.Text, "local attachment") {
		t.Fatalf("result = %+v, want local attachment policy rejection", result)
	}
}

func TestExecuteAgentLoopToolCallRejectsLegacyGUIForCurrentLocalFileWork(t *testing.T) {
	called := false
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "gui_click",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "desktop clicked"
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := &IMMessageHandler{registry: registry}
	ctx := NewLoopContext("local-file-legacy-gui", 1, nil)
	ctx.ComputerUseBlockedForLocalFileWork = true
	result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		Context:  ctx,
		UserText: "请读取附件",
		ToolCall: llm.ToolCall{ID: "call_gui", Function: llm.ToolCallFunction{
			Name:      "gui_click",
			Arguments: `{}`,
		}},
	})
	if called {
		t.Fatal("legacy GUI handler must not run for local-file work")
	}
	if result.FailureKind != toolFailurePolicyRejected || !strings.Contains(result.Text, "local attachment") {
		t.Fatalf("result = %+v, want local attachment policy rejection", result)
	}
}

func TestExecuteToolDetailedWithRuntimeStateRejectsComputerUseForLocalFileWork(t *testing.T) {
	called := false
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "computer_click",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "desktop clicked"
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	const owner = "desktop-user:local-file-direct"
	h := &IMMessageHandler{registry: registry}
	ctx := NewLoopContext("local-file-direct", 1, nil)
	ctx.ComputerUseBlockedForLocalFileWork = true
	h.setSessionLoopCtx(owner, ctx)

	result := h.executeToolDetailedWithRuntimeState(owner, true, "desktop", "computer_click", `{}`, "", nil)
	if called {
		t.Fatal("direct runtime execution must not invoke Computer Use handler for local-file work")
	}
	if result.FailureKind != toolFailurePolicyRejected || !strings.Contains(result.Text, "local attachment") {
		t.Fatalf("result = %+v, want local attachment policy rejection", result)
	}
}

func TestDirectComputerUseExecutionHonorsAttachmentMarkerAndExplicitOverride(t *testing.T) {
	called := 0
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "computer_click",
		Handler: func(args map[string]interface{}) string {
			called++
			return "desktop clicked"
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := &IMMessageHandler{registry: registry}
	attachmentText := "请处理附件\n[附件: report.docx → 已保存到 C:\\tmp\\report.docx]"

	blocked := h.executeToolDetailedWithUserText("computer_click", `{}`, attachmentText, nil)
	if called != 0 || blocked.FailureKind != toolFailurePolicyRejected || !strings.Contains(blocked.Text, "local attachment") {
		t.Fatalf("attachment direct execution = %+v calls=%d, want policy rejection without handler", blocked, called)
	}

	allowed := h.executeToolDetailedWithUserText("computer_click", `{}`, "@computer "+attachmentText, nil)
	if called != 1 || allowed.Outcome != toolOutcomeSucceeded || allowed.Text != "desktop clicked" {
		t.Fatalf("explicit Computer Use override = %+v calls=%d, want handler success", allowed, called)
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
		// The execution wrapper prefixes the interrupted marker; the handler's
		// own "context canceled" must not be duplicated after it.
		if result.Text != "tool execution interrupted: "+context.Canceled.Error() || result.Outcome != toolOutcomeFailed {
			t.Fatalf("result = %+v, want interrupted context-canceled failure", result)
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
		// Same interrupted prefix contract as the agent-loop path.
		if result != "tool execution interrupted: "+context.Canceled.Error() {
			t.Fatalf("result = %q, want interrupted context canceled", result)
		}
	case <-time.After(time.Second):
		t.Fatal("tool call did not stop after loop cancel")
	}
}
