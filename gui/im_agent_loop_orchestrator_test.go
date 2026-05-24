package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestApplyAgentLoopTaskOrchestratorStepUsesNextReadyTask(t *testing.T) {
	registry := NewTaskOrchestratorRegistry()
	orch := registry.GetOrCreate("u1")
	orch.ExternalChecker = &fakeExternalChecker{available: true}
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Blocked task", DependsOn: []int{1}},
		{Index: 1, Title: "Ready task"},
	}, "", "", "/proj", "claude")
	h := &IMMessageHandler{taskOrchestratorRegistry: registry}
	tools := []map[string]interface{}{
		toolDef("create_session", "create session", nil, nil),
		toolDef("bash", "bash", nil, nil),
	}

	result := h.applyAgentLoopTaskOrchestratorStep("u1", nil, tools, nil, false)

	names := map[string]bool{}
	for _, item := range result.Tools {
		names[tool.ExtractToolName(item)] = true
	}
	if !names["create_session"] {
		t.Fatalf("ready external task should keep create_session tool, got %#v", names)
	}
	if len(result.Conversation) != 1 {
		t.Fatalf("expected one task injection, got %#v", result.Conversation)
	}
	injection, _ := result.Conversation[0].(map[string]string)
	content := injection["content"]
	if !strings.Contains(content, `"Ready task"`) || strings.Contains(content, `executing task 1/2`) {
		t.Fatalf("injection should target ready task, got %q", content)
	}
}
