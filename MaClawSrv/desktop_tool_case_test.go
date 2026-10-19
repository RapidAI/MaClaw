package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// The offline cases for PR-3 that call the desktop tool. Tools and InvokeTool
// are package main, so this runner lives next to them.

func TestDesktopBotToolCases(t *testing.T) {
	for _, id := range []string{"plan_mutate_stays_listed", "password_field_next_step", "file_too_large_next_step"} {
		t.Run(id, func(t *testing.T) {
			raw := readDesktopToolCase(t, id)
			switch id {
			case "plan_mutate_stays_listed":
				runPlanMutateStaysListed(t, raw)
			case "password_field_next_step":
				runPasswordFieldNextStep(t, raw)
			case "file_too_large_next_step":
				runFileTooLargeNextStep(t, raw)
			default:
				t.Fatalf("no runner for %s", id)
			}
		})
	}
}

type desktopToolCase struct {
	ID       string   `json:"id"`
	Mutating []string `json:"mutating"`
	Expect   string   `json:"expect"`
	Secret   string   `json:"secret"`
	Path     string   `json:"path"`
	Marker   string   `json:"marker"`
}

func readDesktopToolCase(t *testing.T, id string) desktopToolCase {
	t.Helper()
	path := filepath.Join("..", "testdata", "desktopbot", "cases", id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c desktopToolCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id {
		t.Fatalf("case id = %s, file %s", c.ID, id)
	}
	return c
}

func desktopCaseTools(t *testing.T, phase string) agentruntime.ToolDefinition {
	t.Helper()
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9")
	tools, err := (desktopRuntimeModule{}).Tools(context.Background(), agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "tenant-case", UserID: "alice", InstanceID: "case-bot"},
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": phase}}},
	})
	if err != nil || len(tools) != 1 || tools[0].Name != desktopToolName {
		t.Fatalf("phase %s tools=%v err=%v", phase, tools, err)
	}
	return tools[0]
}

func desktopActionEnum(t *testing.T, def agentruntime.ToolDefinition) []string {
	t.Helper()
	props, _ := def.Parameters["properties"].(map[string]any)
	action, _ := props["action"].(map[string]any)
	enum, _ := action["enum"].([]string)
	if len(enum) == 0 {
		t.Fatalf("action enum missing: %#v", action)
	}
	return enum
}

func runPlanMutateStaysListed(t *testing.T, c desktopToolCase) {
	t.Helper()
	plan := desktopCaseTools(t, "plan")
	execute := desktopCaseTools(t, "execute")
	if plan.Name != execute.Name || plan.Description != execute.Description {
		t.Fatal("plan and execute published different desktop tools")
	}
	planEnum := strings.Join(desktopActionEnum(t, plan), ",")
	executeEnum := strings.Join(desktopActionEnum(t, execute), ",")
	if planEnum != executeEnum {
		t.Fatalf("plan enum %s execute enum %s", planEnum, executeEnum)
	}
	for _, action := range c.Mutating {
		if !strings.Contains(planEnum, action) {
			t.Fatalf("plan schema dropped %s", action)
		}
	}
	turn := agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "tenant-case", UserID: "alice", InstanceID: "case-bot"},
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "plan"}}},
	}
	for _, action := range c.Mutating {
		_, err := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
			"action":  action,
			"path":    "/home/desktop/a.txt",
			"content": "hello",
			"command": "echo hi",
			"url":     "https://example.com",
			"name":    "note.txt",
		})
		if err == nil || err.Error() != c.Expect || strings.Contains(err.Error(), "plan phase blocks") {
			t.Fatalf("%s err=%v", action, err)
		}
	}
}

func runPasswordFieldNextStep(t *testing.T, c desktopToolCase) {
	t.Helper()
	previous := desktopFocusedInputType
	previousSession := desktopRemoteSession
	desktopFocusedInputType = func(agentruntime.Scope) string { return "password" }
	reached := false
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		reached = true
		return desktopEndpoint{}, context.Canceled
	}
	t.Cleanup(func() {
		desktopFocusedInputType = previous
		desktopRemoteSession = previousSession
	})
	turn := agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "tenant-case", UserID: "alice", InstanceID: "case-bot"},
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	_, err := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "type",
		"text":   c.Secret,
	})
	if err == nil || err.Error() != c.Expect || strings.Contains(err.Error(), c.Secret) || strings.Contains(err.Error(), "not_password_field") || reached {
		t.Fatalf("err=%v reached=%v", err, reached)
	}
}

func runFileTooLargeNextStep(t *testing.T, c desktopToolCase) {
	t.Helper()
	content := c.Marker + strings.Repeat("a", desktop.FileContentMax)
	turn := agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "tenant-case", UserID: "alice", InstanceID: "case-bot"},
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	_, err := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action":  "file_write",
		"path":    c.Path,
		"content": content,
	})
	if err == nil || err.Error() != c.Expect || strings.Contains(err.Error(), c.Marker) || strings.Contains(err.Error(), "sk-") {
		t.Fatalf("err=%v", err)
	}
}
