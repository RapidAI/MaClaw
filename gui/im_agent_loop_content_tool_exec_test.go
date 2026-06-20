package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestAgentLoopExecutesParsedPlainContentToolCall(t *testing.T) {
	content := `Start cleanup task.

TOOL_CALL
{
  "function": "ssh_execute_command",
  "args": {
    "host": "example.com",
    "port": 22,
    "username": "root",
    "password": "secret-value",
    "command": "df -h"
  }
}`

	calls, malformed := llm.ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatal("plain content tool call parsed as malformed")
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls len = %d, want 1", len(calls))
	}
	if calls[0].Function.Name != "ssh" {
		t.Fatalf("tool name = %q, want ssh", calls[0].Function.Name)
	}

	handler := &IMMessageHandler{registry: NewToolRegistry()}
	var gotArgs map[string]interface{}
	if err := handler.registry.Register(RegisteredTool{
		Name:     "ssh",
		Category: ToolCategoryBuiltin,
		Status:   RegToolAvailable,
		Handler: func(args map[string]interface{}) string {
			gotArgs = args
			return "ssh executed"
		},
	}); err != nil {
		t.Fatalf("register ssh: %v", err)
	}

	var recordedCallID, recordedToolName string
	result := handler.executeAgentLoopToolCalls(agentLoopToolCallsOptions{
		UserID:         "desktop-user",
		MessageContent: "",
		ToolCalls:      calls,
		RecordToolCall: func(id, name, args string) {
			recordedCallID = id
			recordedToolName = name
		},
	})

	if recordedCallID == "" {
		t.Fatal("tool call was not recorded; execution path was not entered")
	}
	if recordedToolName != "ssh" {
		t.Fatalf("recorded tool = %q, want ssh", recordedToolName)
	}
	if len(result.History) != 1 || result.History[0].Role != "tool" {
		t.Fatalf("history = %#v, want one tool result", result.History)
	}
	toolContent, _ := result.History[0].Content.(string)
	if strings.Contains(toolContent, "TOOL_CALL") {
		t.Fatalf("raw TOOL_CALL leaked into tool result: %q", result.History[0].Content)
	}
	if gotArgs == nil {
		t.Fatal("fake ssh handler was not called")
	}
	if gotArgs["action"] != "connect" || gotArgs["user"] != "root" || gotArgs["initial_command"] != "df -h" {
		encoded, _ := json.Marshal(gotArgs)
		t.Fatalf("normalized ssh args = %s", encoded)
	}
	if _, ok := gotArgs["username"]; ok {
		t.Fatalf("username alias was not normalized: %#v", gotArgs)
	}
}

func TestAgentLoopNormalizesMissingToolCallIDAndType(t *testing.T) {
	handler := &IMMessageHandler{registry: NewToolRegistry()}
	if err := handler.registry.Register(RegisteredTool{
		Name:     "bash",
		Category: ToolCategoryBuiltin,
		Status:   RegToolAvailable,
		Handler: func(args map[string]interface{}) string {
			return "ok"
		},
	}); err != nil {
		t.Fatalf("register bash: %v", err)
	}

	postTurn := handler.handleAgentLoopPostLLMTurn(agentLoopPostLLMTurnOptions{
		Response: &llm.Response{Choices: []llm.Choice{{
			Message: llm.Message{
				Role: "assistant",
				ToolCalls: []llm.ToolCall{{
					Function: llm.ToolCallFunction{Name: "bash", Arguments: `{"command":"dir"}`},
				}},
			},
			FinishReason: "tool_calls",
		}}},
		Conversation: []interface{}{},
		History:      []agent.ConversationEntry{},
	})
	if len(postTurn.Choice.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(postTurn.Choice.Message.ToolCalls))
	}
	normalizedCall := postTurn.Choice.Message.ToolCalls[0]
	if normalizedCall.ID == "" {
		t.Fatal("normalized tool call id is empty")
	}
	if normalizedCall.Type != "function" {
		t.Fatalf("normalized tool call type = %q, want function", normalizedCall.Type)
	}
	assistantMsg, ok := postTurn.Conversation[0].(map[string]interface{})
	if !ok {
		t.Fatalf("assistant conversation message = %#v", postTurn.Conversation[0])
	}
	assistantCalls, ok := assistantMsg["tool_calls"].([]llm.ToolCall)
	if !ok || len(assistantCalls) != 1 {
		t.Fatalf("assistant tool_calls = %#v", assistantMsg["tool_calls"])
	}
	if assistantCalls[0].ID != normalizedCall.ID || assistantCalls[0].Type != "function" {
		t.Fatalf("assistant tool_call = %#v, want id %q type function", assistantCalls[0], normalizedCall.ID)
	}

	result := handler.executeAgentLoopToolCalls(agentLoopToolCallsOptions{
		ToolCalls:    postTurn.Choice.Message.ToolCalls,
		Conversation: postTurn.Conversation,
		History:      postTurn.History,
	})
	if len(result.Conversation) < 2 {
		t.Fatalf("conversation len = %d, want assistant+tool", len(result.Conversation))
	}
	toolMsg, ok := result.Conversation[1].(map[string]interface{})
	if !ok {
		t.Fatalf("tool conversation message = %#v", result.Conversation[1])
	}
	if got := toolMsg["tool_call_id"]; got != normalizedCall.ID {
		t.Fatalf("tool_call_id = %#v, want %q", got, normalizedCall.ID)
	}
	if len(result.History) != 2 || result.History[1].ToolCallID != normalizedCall.ID {
		t.Fatalf("history = %#v, want matching tool call id", result.History)
	}
}

func TestAgentLoopReplansByCancellingContextAwareToolAfterSupplementaryInput(t *testing.T) {
	handler := &IMMessageHandler{registry: NewToolRegistry()}
	userID := "desktop-user"
	if err := handler.registry.Register(RegisteredTool{
		Name:     "bash",
		Category: ToolCategoryBuiltin,
		Status:   RegToolAvailable,
		HandlerCtx: func(ctx context.Context, args map[string]interface{}, onProgress coretool.ProgressCallback) string {
			if !handler.InjectSupplementary(userID, "use builtin ssh tool") {
				return "inject failed"
			}
			select {
			case <-ctx.Done():
				return ctx.Err().Error()
			case <-time.After(time.Second):
				return "context was not cancelled"
			}
		},
	}); err != nil {
		t.Fatalf("register bash: %v", err)
	}

	ctx := NewLoopContext("chat", 10, nil)
	handler.setSessionLoopCtx(userID, ctx)
	call := llm.ToolCall{
		ID:   "call_stale",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "bash",
			Arguments: `{"command":"ssh root@example.com"}`,
		},
	}

	result := handler.executeAgentLoopToolCalls(agentLoopToolCallsOptions{
		Context:   ctx,
		UserID:    userID,
		ToolCalls: []llm.ToolCall{call},
		Conversation: []interface{}{map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{call},
		}},
		History: []agent.ConversationEntry{{
			Role:      "assistant",
			ToolCalls: []llm.ToolCall{call},
		}},
	})

	if !result.Replanned {
		t.Fatal("tool execution should request replan after supplementary input")
	}
	if len(result.ToolResults) != 0 {
		t.Fatalf("tool results = %#v, want none committed during replan", result.ToolResults)
	}
	if len(result.History) != 1 || result.History[0].ToolCalls != nil {
		t.Fatalf("history = %#v, want assistant tool calls stripped", result.History)
	}
	if len(result.Conversation) != 1 || msgHasToolCalls(result.Conversation[0]) {
		t.Fatalf("conversation = %#v, want assistant tool calls stripped", result.Conversation)
	}
}

func TestWorkflowDocPhaseExtractsStructuredWriteFileToolCallAsContent(t *testing.T) {
	handler := &IMMessageHandler{}
	postTurn := handler.handleAgentLoopPostLLMTurn(agentLoopPostLLMTurnOptions{
		Context: &LoopContext{
			WorkflowAgentLoop: true,
			WorkflowDocPhase:  true,
			WorkflowPhaseID:   "task_breakdown",
		},
		Response: &llm.Response{Choices: []llm.Choice{{
			Message: llm.Message{
				Role: "assistant",
				ToolCalls: []llm.ToolCall{{
					ID:   "call_write",
					Type: "function",
					Function: llm.ToolCallFunction{
						Name:      "write_file",
						Arguments: `{"file_path":"d:\\project\\docs\\tasks.md","content":"# Tasks\n\n- T1"}`,
					},
				}},
			},
			FinishReason: "tool_calls",
		}}},
		Conversation: []interface{}{},
		History:      []agent.ConversationEntry{},
	})

	// After fix: tool calls are preserved for execution (write_file will actually
	// write the file to disk). Document content is captured in post-loop via
	// WorkflowWrittenFiles reading the written file. Content stays as LLM's original.
	if postTurn.MessageContent != "" {
		t.Fatalf("MessageContent should be empty (LLM's original), got %q", postTurn.MessageContent)
	}
	if len(postTurn.Choice.Message.ToolCalls) != 1 {
		t.Fatalf("tool calls should be preserved for execution, got %d", len(postTurn.Choice.Message.ToolCalls))
	}
	if postTurn.Choice.FinishReason != "tool_calls" {
		t.Fatalf("FinishReason = %q, want tool_calls (preserved)", postTurn.Choice.FinishReason)
	}
}
