package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// TestDesktopBotComputerCases is the MaClawSrv half of PR-6. A main_agent
// turn drops click, type, script, and computer. The helper and an unmarked
// execute turn keep the shared desktop schema.
func TestDesktopBotComputerCases(t *testing.T) {
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9")

	main := desktopComputerTools(t, map[string]string{"bot_phase": "execute", "main_agent": "1"})
	for _, action := range []string{"click", "type", "script", "computer"} {
		if desktopComputerHasAction(t, main, action) {
			t.Fatalf("main agent kept %s: %v", action, desktopActionEnum(t, main))
		}
	}
	if !desktopComputerHasAction(t, main, "screenshot") {
		t.Fatal("main agent lost the read-only screenshot")
	}
	mainProps := desktopComputerProps(t, main)
	for _, key := range []string{"x", "y", "region"} {
		if _, ok := mainProps[key]; ok {
			t.Fatalf("main agent screenshot still has %s", key)
		}
	}
	if !strings.Contains(main.Description, "has no coordinates") {
		t.Fatalf("main description = %s", main.Description)
	}

	helper := desktopComputerTools(t, map[string]string{"bot_phase": "execute", "computer_helper": "1"})
	unmarked := desktopComputerTools(t, map[string]string{"bot_phase": "execute"})
	if strings.Join(desktopActionEnum(t, helper), ",") != strings.Join(desktopActionEnum(t, unmarked), ",") {
		t.Fatal("the helper and an unmarked execute turn published different actions")
	}
	for _, action := range []string{"click", "type", "script", "computer"} {
		if !desktopComputerHasAction(t, helper, action) {
			t.Fatalf("helper dropped %s", action)
		}
	}
	helperProps := desktopComputerProps(t, helper)
	for _, key := range []string{"x", "y", "region"} {
		if _, ok := helperProps[key]; !ok {
			t.Fatalf("helper schema lost %s", key)
		}
	}

	turn := agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "tenant-case", UserID: "alice", InstanceID: "case-bot"},
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{
			"bot_phase": "execute", "main_agent": "1",
		}}},
	}
	_, err := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{"action": "click"})
	if err == nil || err.Error() != "这一步由操作电脑的帮手做" {
		t.Fatalf("click err=%v", err)
	}
}

func desktopComputerTools(t *testing.T, meta map[string]string) agentruntime.ToolDefinition {
	t.Helper()
	tools, err := (desktopRuntimeModule{}).Tools(context.Background(), agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "tenant-case", UserID: "alice", InstanceID: "case-bot"},
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: meta}},
	})
	if err != nil || len(tools) != 1 || tools[0].Name != desktopToolName {
		t.Fatalf("tools=%v err=%v", tools, err)
	}
	return tools[0]
}

func desktopComputerProps(t *testing.T, def agentruntime.ToolDefinition) map[string]any {
	t.Helper()
	props, _ := def.Parameters["properties"].(map[string]any)
	if props == nil {
		t.Fatal("properties missing")
	}
	return props
}

func desktopComputerHasAction(t *testing.T, def agentruntime.ToolDefinition, action string) bool {
	t.Helper()
	for _, got := range desktopActionEnum(t, def) {
		if got == action {
			return true
		}
	}
	return false
}
