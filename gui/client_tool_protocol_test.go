package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestClientToolCatalogIsPerLoopAndDispatched(t *testing.T) {
	ctx := NewLoopContext("client-tool", 3, nil)
	ctx.ClientTools = []agent.ClientToolDefinition{{
		Name: "alarm_list", Description: "List device alarms",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}}
	ctx.ClientToolContext = &agent.ClientToolContext{ClientID: "pet-a", ConversationID: "default"}
	tools := appendClientToolsForAgent(nil, ctx)
	if len(tools) != 1 || !containsAgentLoopToolNamed(tools, "alarm_list") {
		t.Fatalf("client tool exposure=%#v", tools)
	}

	var gotTarget agent.ClientToolContext
	var gotArgs map[string]any
	h := &IMMessageHandler{clientToolDispatcher: func(_ context.Context, target agent.ClientToolContext, _ agent.ClientToolDefinition, _ string, args map[string]any) error {
		gotTarget, gotArgs = target, args
		return nil
	}}
	result := h.dispatchClientToolCall(ctx, ctx.ClientTools[0], "call-1", `{"scope":"all"}`)
	if result.Outcome != toolOutcomeSucceeded || gotTarget.ClientID != "pet-a" || gotArgs["scope"] != "all" {
		t.Fatalf("result=%#v target=%#v args=%#v", result, gotTarget, gotArgs)
	}
	if _, ok := clientToolForLoop(&LoopContext{}, "alarm_list"); ok {
		t.Fatal("client tool leaked into unrelated loop")
	}
}

func TestBuildClientToolPromptRequiresESPAlarmToolCall(t *testing.T) {
	ctx := NewLoopContext("alarm-device", 3, nil)
	ctx.ClientToolContext = &agent.ClientToolContext{ClientID: "bread-compact"}
	prompt := buildClientToolPrompt([]agent.ClientToolDefinition{
		{Name: "alarm_create"},
		{Name: "alarm_clear"},
	}, ctx)
	for _, want := range []string{
		"Device-local alarm contract (mandatory)",
		"not on the computer or MaClaw GUI",
		"Never claim an alarm was set, cancelled, or listed unless you called that tool",
		"call alarm_create",
		"Do not create a host/GUI scheduled task",
		"bread-compact",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("alarm prompt missing %q:\n%s", want, prompt)
		}
	}
	if got := buildClientToolPrompt([]agent.ClientToolDefinition{{Name: "device_led"}}, ctx); got != "" {
		t.Fatalf("non-alarm client tools unexpectedly produced alarm prompt: %q", got)
	}
}
