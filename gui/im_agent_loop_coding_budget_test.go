package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestCodingIterBudgetHardLimitUsesLoopMaxIterations(t *testing.T) {
	ctx := NewLoopContext("chat", 300, nil)
	if got := codingIterBudgetHardLimit(ctx); got != 300 {
		t.Fatalf("codingIterBudgetHardLimit() = %d, want 300", got)
	}
}

func TestCodingIterBudgetHardLimitRespectsLoopOverride(t *testing.T) {
	ctx := NewLoopContext("chat", 200, nil)
	if got := codingIterBudgetHardLimit(ctx); got != 200 {
		t.Fatalf("codingIterBudgetHardLimit() = %d, want 200", got)
	}
}

func TestCodingIterBudgetHardLimitDoesNotExceedSystemCap(t *testing.T) {
	ctx := NewLoopContext("chat", config.MaxAgentIterationsCap*2, nil)
	if got := codingIterBudgetHardLimit(ctx); got != config.MaxAgentIterationsCap {
		t.Fatalf("codingIterBudgetHardLimit() = %d, want %d", got, config.MaxAgentIterationsCap)
	}
}

func TestCodingIterBudgetHardLimitUsesEffectiveMinimum(t *testing.T) {
	ctx := NewLoopContext("chat", config.MinAgentIterations-1, nil)
	if got := codingIterBudgetHardLimit(ctx); got != config.MinAgentIterations {
		t.Fatalf("codingIterBudgetHardLimit() = %d, want %d", got, config.MinAgentIterations)
	}
}

func TestCodingIterBudgetHardLimitFallsBackToConfigCap(t *testing.T) {
	if got := codingIterBudgetHardLimit(nil); got != config.MaxAgentIterationsCap {
		t.Fatalf("codingIterBudgetHardLimit(nil) = %d, want %d", got, config.MaxAgentIterationsCap)
	}
}

func TestEnforceAgentLoopCodingBudgetDoesNotStopAtLegacySixtyFive(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("chat", 300, nil)
	toolCalls := []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "write_file"}}}

	result := h.enforceAgentLoopCodingBudget(
		ctx,
		"desktop-user",
		65,
		64,
		toolCalls,
		nil,
		nil,
		nil,
		"",
		"",
		"",
		func(int, []interface{}) {},
		func(*IMAgentResponse) {},
		func(*IMAgentResponse) {},
	)

	if result.Count != 65 {
		t.Fatalf("Count = %d, want 65", result.Count)
	}
	if result.Response != nil {
		t.Fatalf("Response = %#v, want nil before configured max", result.Response)
	}
}

func TestEnforceAgentLoopCodingBudgetStopsAtConfiguredMax(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	ctx := NewLoopContext("chat", config.MaxAgentIterationsCap, nil)
	toolCalls := []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "write_file"}}}
	telemetryAttached := false
	artifactsAttached := false

	result := h.enforceAgentLoopCodingBudget(
		ctx,
		"desktop-user",
		config.MaxAgentIterationsCap,
		config.MaxAgentIterationsCap-1,
		toolCalls,
		nil,
		nil,
		nil,
		"",
		"",
		"",
		func(int, []interface{}) {},
		func(*IMAgentResponse) { telemetryAttached = true },
		func(*IMAgentResponse) { artifactsAttached = true },
	)

	if result.Count != config.MaxAgentIterationsCap {
		t.Fatalf("Count = %d, want %d", result.Count, config.MaxAgentIterationsCap)
	}
	if result.Response == nil {
		t.Fatal("Response = nil, want configured-max stop response")
	}
	if !strings.Contains(result.Response.Text, "300-iteration limit") {
		t.Fatalf("Response.Text = %q, want configured max in message", result.Response.Text)
	}
	if !telemetryAttached || !artifactsAttached {
		t.Fatalf("callbacks telemetry=%v artifacts=%v, want both true", telemetryAttached, artifactsAttached)
	}
}
