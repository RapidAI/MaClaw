package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

func TestCompactAgentLoopConversationCreatesLosslessCheckpoint(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })
	oldMode, hadMode := os.LookupEnv("MACLAW_CONTEXT_CHECKPOINT")
	if err := os.Setenv("MACLAW_CONTEXT_CHECKPOINT", "on"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadMode {
			_ = os.Setenv("MACLAW_CONTEXT_CHECKPOINT", oldMode)
		} else {
			_ = os.Unsetenv("MACLAW_CONTEXT_CHECKPOINT")
		}
	})
	conversation := []interface{}{map[string]string{"role": "system", "content": "system"}}
	for i := 0; i < 20; i++ {
		conversation = append(conversation, map[string]string{"role": "user", "content": strings.Repeat("important requirement ", 120)})
	}
	tools := []map[string]interface{}{tooldef.BuildToolDef("read_tool_result", "read", map[string]interface{}{"type": "object"})}
	got := (&IMMessageHandler{}).compactAgentLoopConversation(nil, "owner-a", conversation, tools, 5000, agent.EstimateToolsTokens(tools), false)
	if len(got) >= len(conversation) || len(got) < 3 {
		t.Fatalf("unexpected checkpoint length before=%d after=%d", len(conversation), len(got))
	}
	checkpoint, ok := got[1].(map[string]string)
	if !ok || !strings.Contains(checkpoint["content"], "[tool_result_handle]") || !strings.Contains(checkpoint["content"], "preserved_user_goals_and_constraints") {
		t.Fatalf("missing lossless structured checkpoint: %#v", got[1])
	}
}

func TestContextCheckpointShadowKeepsLegacyConversation(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })
	oldMode, hadMode := os.LookupEnv("MACLAW_CONTEXT_CHECKPOINT")
	_ = os.Setenv("MACLAW_CONTEXT_CHECKPOINT", "shadow")
	t.Cleanup(func() {
		if hadMode {
			_ = os.Setenv("MACLAW_CONTEXT_CHECKPOINT", oldMode)
		} else {
			_ = os.Unsetenv("MACLAW_CONTEXT_CHECKPOINT")
		}
	})
	conversation := []interface{}{map[string]string{"role": "system", "content": "system"}}
	for i := 0; i < 20; i++ {
		conversation = append(conversation, map[string]string{"role": "user", "content": strings.Repeat("payload ", 100)})
	}
	tools := []map[string]interface{}{tooldef.BuildToolDef("read_tool_result", "read", map[string]interface{}{"type": "object"})}
	toolsTokens := agent.EstimateToolsTokens(tools)
	want := trimConversation(conversation, 5000, toolsTokens, nil)
	got := (&IMMessageHandler{}).compactAgentLoopConversation(nil, "owner-a", conversation, tools, 5000, toolsTokens, false)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shadow mode diverged from legacy trimming: got=%d want=%d", len(got), len(want))
	}
	store := maclawpath.ToolResultsDir()
	if entries, err := os.ReadDir(store); err == nil && len(entries) != 0 {
		t.Fatalf("shadow mode persisted tool-result handles: %d entries", len(entries))
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestContextCheckpointDefaultRolloutIsStable(t *testing.T) {
	oldMode, hadMode := os.LookupEnv("MACLAW_CONTEXT_CHECKPOINT")
	_ = os.Unsetenv("MACLAW_CONTEXT_CHECKPOINT")
	t.Cleanup(func() {
		if hadMode {
			_ = os.Setenv("MACLAW_CONTEXT_CHECKPOINT", oldMode)
		}
	})
	if got := contextCheckpointMode(); got != agent.ContextCheckpointOn {
		t.Fatalf("default rollout = %q, want %q (lossless checkpoints on by default)", got, agent.ContextCheckpointOn)
	}
	if got := contextCheckpointStatusMode(); got != "on" {
		t.Fatalf("default status mode = %q, want on", got)
	}
}

func TestContextCheckpointStatusModeReflectsForcedMode(t *testing.T) {
	for _, mode := range []string{"off", "shadow", "on"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("MACLAW_CONTEXT_CHECKPOINT", mode)
			if got := contextCheckpointStatusMode(); got != mode {
				t.Fatalf("status mode = %q, want %q", got, mode)
			}
		})
	}
}
