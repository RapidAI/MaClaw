package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestSetMaxIterationsUsesRuntimeOwnerLoop(t *testing.T) {
	h := &IMMessageHandler{}
	desktopCtx := NewLoopContext("desktop", 20, nil)
	remoteCtx := NewLoopContext("remote", 20, nil)
	h.setSessionLoopCtx(desktopUserID, desktopCtx)
	h.setSessionLoopCtx("remote:mobile", remoteCtx)
	h.globalLoopMu.Lock()
	h.currentLoopCtx = desktopCtx
	h.lastUserID = desktopUserID
	h.globalLoopMu.Unlock()

	result := h.toolSetMaxIterations(map[string]interface{}{
		"max_iterations":                 float64(31),
		registeredToolPolicyOwnerIDField: "remote:mobile",
	})
	if result == "" {
		t.Fatal("empty result")
	}
	if remoteCtx.MaxIterations() != 31 {
		t.Fatalf("remote max iterations = %d, want 31", remoteCtx.MaxIterations())
	}
	if desktopCtx.MaxIterations() == 31 {
		t.Fatal("desktop loop should not receive remote set_max_iterations")
	}
	if h.loopMaxOverride == 31 {
		t.Fatal("runtime-scoped set_max_iterations should not set legacy global override")
	}
}

func TestSetMaxIterationsEmptyRuntimeOwnerFailsClosed(t *testing.T) {
	h := &IMMessageHandler{currentLoopCtx: NewLoopContext("desktop", 20, nil)}
	result := h.toolSetMaxIterations(map[string]interface{}{
		"max_iterations":                 float64(31),
		registeredToolPolicyOwnerIDField: "",
	})
	if result == "" || !strings.Contains(result, "runtime owner is missing") {
		t.Fatalf("set_max_iterations should fail closed for empty runtime owner, got %q", result)
	}
}

func TestSetMaxIterationsMissingRuntimeOwnerFailsClosed(t *testing.T) {
	desktopCtx := NewLoopContext("desktop", 20, nil)
	h := &IMMessageHandler{currentLoopCtx: desktopCtx, lastUserID: desktopUserID}
	result := h.toolSetMaxIterations(map[string]interface{}{"max_iterations": float64(31)})
	if result == "" || !strings.Contains(result, "runtime owner is missing") {
		t.Fatalf("set_max_iterations should fail closed without runtime owner, got %q", result)
	}
	if desktopCtx.MaxIterations() == 31 {
		t.Fatal("ownerless set_max_iterations must not update current loop")
	}
	if h.loopMaxOverride == 31 {
		t.Fatal("ownerless set_max_iterations must not set global override")
	}
}

func TestSetMaxIterationsExpiresWithTheTurn(t *testing.T) {
	h := &IMMessageHandler{standaloneConfig: &StandaloneConfig{MaxIterationsFunc: func() int { return 100 }}}
	ctx := NewLoopContext("desktop", 100, nil)
	h.setSessionLoopCtx(desktopUserID, ctx)
	turn := IMUserMessage{RequestID: "turn-1", UserID: desktopUserID, Platform: "desktop", Text: "做一个游戏"}
	h.prepareIMLoopContext(ctx, turn, nil, false, false)

	result := h.toolSetMaxIterations(map[string]interface{}{
		"max_iterations":                 float64(40),
		registeredToolPolicyOwnerIDField: desktopUserID,
	})
	if !strings.Contains(result, "40") || ctx.MaxIterations() != 40 || !ctx.iterationCeilingRaised {
		t.Fatalf("result=%q max=%d raised=%v", result, ctx.MaxIterations(), ctx.iterationCeilingRaised)
	}
	kept := h.prepareIMLoopContext(ctx, turn, nil, false, false)
	if kept.MaxIterations() != 40 || !kept.iterationCeilingRaised {
		t.Fatalf("retry dropped the turn limit max=%d raised=%v", kept.MaxIterations(), kept.iterationCeilingRaised)
	}
	next := h.prepareIMLoopContext(ctx, IMUserMessage{RequestID: "turn-2", UserID: desktopUserID, Platform: "desktop", Text: "下一句"}, nil, false, false)
	if next.MaxIterations() != 100 || next.iterationCeilingRaised {
		t.Fatalf("next message kept the turn limit max=%d raised=%v", next.MaxIterations(), next.iterationCeilingRaised)
	}
}

func TestAgentLoopSetMaxIterationsEmptyRuntimeOwnerFailsClosed(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	desktopCtx := NewLoopContext("desktop", 20, nil)
	loopCtx := NewLoopContext("remote", 20, nil)
	loopCtx.Runtime = RuntimeContext{RequestID: "req-empty-owner"}
	h.globalLoopMu.Lock()
	h.currentLoopCtx = desktopCtx
	h.lastUserID = desktopUserID
	h.globalLoopMu.Unlock()

	result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		Context: loopCtx,
		ToolCall: llm.ToolCall{ID: "call_set_max", Function: llm.ToolCallFunction{
			Name:      "set_max_iterations",
			Arguments: `{"max_iterations":31}`,
		}},
	})
	if result.Text == "" || !strings.Contains(result.Text, "runtime owner is missing") {
		t.Fatalf("agent-loop set_max_iterations should fail closed for empty runtime owner, got %+v", result)
	}
	if desktopCtx.MaxIterations() == 31 {
		t.Fatal("empty-owner runtime set_max_iterations must not update desktop current loop")
	}
	if h.loopMaxOverride == 31 {
		t.Fatal("empty-owner runtime set_max_iterations must not set legacy global override")
	}
}
