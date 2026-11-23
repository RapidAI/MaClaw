package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/localrelay"
)

func localTaskTurn(phase string) agentruntime.TurnRequest {
	return agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "t1", UserID: "user-a", InstanceID: "inst", RunID: "run1"},
		Input: agentservice.ExecuteRequest{
			Instance: agentservice.Instance{ID: "inst", Metadata: map[string]string{
				"hub_bot": "1", "hub_user_id": "user-a", "hub_tenant_id": "t1",
			}},
			Message: agentservice.Message{Metadata: map[string]string{"bot_phase": phase}},
		},
	}
}

func TestLocalTaskCreateRidesTheHubRelay(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: `{"project_path":"D:/work","status":"running","run_id":"r1"}`}, nil
	}
	text, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool":        "create",
		"prompt":      "实现登录",
		"working_dir": `D:\work`,
		"mode":        "coding_dev",
		"wait":        true,
		"arguments": map[string]any{
			"prompt":       "以参数为准",
			"ssh_password": "s3cret-not-in-result",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "s3cret") || !strings.Contains(text, "D:/work") {
		t.Fatalf("result = %s", text)
	}
	if got["op"] != "mcp_call" || got["server"] != "maclaw" || got["tool"] != "create_task" {
		t.Fatalf("relay body = %+v", got)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if args["prompt"] != "以参数为准" || args["working_dir"] != `D:\work` || args["ssh_password"] != "s3cret-not-in-result" || args["wait"] != false {
		t.Fatalf("args = %+v", args)
	}
	if _, ok := got["url"]; ok {
		t.Fatal("the bot dialed an address")
	}
}

func TestLocalTaskDoesNotForwardThePasswordOnARead(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: `{"status":"running","text":"s3cret-not-in-result slipped"}`}, nil
	}
	text, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "get_task", "project_path": `D:\work`, "ssh_password": "s3cret-not-in-result", "wait": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "s3cret") {
		t.Fatalf("result echoed the password: %s", text)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if _, ok := args["ssh_password"]; ok || args["wait"] != nil {
		t.Fatalf("read forwarded %v", args)
	}
}

func TestLocalTaskPlanDoesNotCreate(t *testing.T) {
	prev := localRelayPost
	called := false
	localRelayPost = func(context.Context, map[string]any) (localRelayReply, error) {
		called = true
		return localRelayReply{OK: true, Text: "tasks"}, nil
	}
	t.Cleanup(func() { localRelayPost = prev })
	_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("plan"), localTaskToolName, map[string]any{
		"tool": "create_task", "prompt": "实现登录",
	})
	if err == nil || !strings.Contains(err.Error(), "execute") || called {
		t.Fatalf("plan create err=%v called=%v", err, called)
	}
	text, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("plan"), localTaskToolName, map[string]any{
		"tool": "list_tasks",
	})
	if err != nil || !called {
		t.Fatalf("plan list text=%q err=%v called=%v", text, err, called)
	}
}

func TestLocalTaskSaysWhenTheAppIsOffline(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	localRelayPost = func(context.Context, map[string]any) (localRelayReply, error) {
		return localRelayReply{Code: localrelay.CodeOffline, Error: localrelay.ErrOffline.Message}, nil
	}
	_, err := localTaskCall(context.Background(), agentruntime.Scope{TenantID: "t1", UserID: "user-a"}, "get_task", `{"project_path":"D:\\work"}`, "")
	if err == nil || localrelay.CodeOf(err) != localrelay.CodeOffline {
		t.Fatalf("offline: %v", err)
	}
}

func TestLocalTaskHiddenFromDigitalEmployees(t *testing.T) {
	t.Setenv("MACLAW_LOCAL_RELAY", "")
	prev := localRelayTestEnabled
	localRelayTestEnabled = true
	t.Cleanup(func() { localRelayTestEnabled = prev })
	tools, err := localRelayModule{}.Tools(context.Background(), agentruntime.TurnRequest{})
	if err != nil || len(tools) != 0 {
		t.Fatalf("employee tools=%v err=%v", tools, err)
	}
	prompt, err := localRelayModule{}.ContributePrompt(context.Background(), localTaskTurn(""))
	if err != nil || !strings.Contains(prompt, "intranet") || !strings.Contains(prompt, "outbound") {
		t.Fatalf("prompt=%q err=%v", prompt, err)
	}
}
