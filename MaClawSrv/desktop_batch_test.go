package main

import (
	"context"
	"strings"
	"testing"

	agentruntime "github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestBatchRejectsBadPlans(t *testing.T) {
	m := desktopRuntimeModule{}
	if _, err := m.invokePlan(context.Background(), agentruntime.TurnRequest{}, "desktop", map[string]any{"action": "batch"}); err == nil {
		t.Fatal("empty plan must be refused")
	}
	_, err := m.invokePlan(context.Background(), agentruntime.TurnRequest{}, "desktop", map[string]any{"action": "batch", "plan": []any{map[string]any{"action": "bash", "command": "ls"}}})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("bash in a plan must be refused, got %v", err)
	}
	_, err = m.invokePlan(context.Background(), agentruntime.TurnRequest{}, "desktop", map[string]any{"action": "batch", "plan": []any{map[string]any{"action": "batch"}}})
	if err == nil {
		t.Fatal("nested batch must be refused")
	}
}
