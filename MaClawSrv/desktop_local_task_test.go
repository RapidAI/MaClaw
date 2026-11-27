package main

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestLocalTaskCatalogStaysLocal(t *testing.T) {
	prev := localRelayPost
	called := false
	localRelayPost = func(context.Context, map[string]any) (localRelayReply, error) {
		called = true
		return localRelayReply{}, nil
	}
	t.Cleanup(func() { localRelayPost = prev })
	text, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("plan"), localTaskToolName, map[string]any{
		"arguments": map[string]any{"tool": "tools"},
	})
	if err != nil || called || !strings.Contains(text, "create_task") || !strings.Contains(text, "knowledge_search") || strings.Contains(text, "browser_test") {
		t.Fatalf("catalog %q err=%v called=%v", text, err, called)
	}
}

func TestLocalTaskKnowledgeSearchRidesTheHubRelay(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: `{"ok":true,"query":"部署笔记","count":1}`}, nil
	}
	text, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("plan"), localTaskToolName, map[string]any{
		"tool":  "knowledge",
		"query": "部署笔记",
		"limit": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "部署笔记") {
		t.Fatalf("result = %s", text)
	}
	if got["op"] != "mcp_call" || got["server"] != "maclaw" || got["tool"] != "knowledge_search" {
		t.Fatalf("relay body = %+v", got)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if args["query"] != "部署笔记" || args["limit"] != float64(3) {
		t.Fatalf("args = %+v", args)
	}
}

func TestLocalKnowledgeSearchToolRidesTheHubRelay(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: `{"ok":true,"query":"用户本地笔记","count":2}`}, nil
	}
	text, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("plan"), localKnowledgeToolName, map[string]any{
		"query": "用户本地笔记",
		"limit": 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "用户本地笔记") {
		t.Fatalf("result = %s", text)
	}
	if got["op"] != "mcp_call" || got["server"] != "maclaw" || got["tool"] != "knowledge_search" {
		t.Fatalf("relay body = %+v", got)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if args["query"] != "用户本地笔记" || args["limit"] != float64(5) {
		t.Fatalf("args = %+v", args)
	}
	if _, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localKnowledgeToolName, map[string]any{}); err == nil || !strings.Contains(err.Error(), "query") {
		t.Fatalf("missing query err=%v", err)
	}
}

func TestLocalKnowledgeSearchToolAdvertisedForBots(t *testing.T) {
	prev := localRelayTestEnabled
	localRelayTestEnabled = true
	t.Cleanup(func() { localRelayTestEnabled = prev })
	tools, err := (localRelayModule{}).Tools(context.Background(), localTaskTurn("plan"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names[localKnowledgeToolName] || !names[localTaskToolName] {
		t.Fatalf("tools = %#v", names)
	}
}

func TestLocalTaskRejectsAnOversizedBody(t *testing.T) {
	prevLimit := localTaskBodyLimit
	localTaskBodyLimit = 80
	t.Cleanup(func() { localTaskBodyLimit = prevLimit })
	prev := localRelayPost
	called := false
	localRelayPost = func(context.Context, map[string]any) (localRelayReply, error) {
		called = true
		return localRelayReply{OK: true}, nil
	}
	t.Cleanup(func() { localRelayPost = prev })
	_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "create_task", "prompt": "实现登录，路径里有很多引号\"和反斜杠\\", "working_dir": `D:\work\a\b`,
	})
	if err == nil || called || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized err=%v called=%v", err, called)
	}
}

func TestLocalTaskRedactsTheJSONPassword(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	secret := "p&ass<word!!"
	quoted, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	escaped := string(quoted[1 : len(quoted)-1])
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: `{"text":"` + escaped + ` slipped"}`}, nil
	}
	text, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"arguments": map[string]any{"tool": "get", "sshPassword": secret, "project_path": `D:\work`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "p&ass") || strings.Contains(text, escaped) {
		t.Fatalf("result = %s", text)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if _, ok := args["sshPassword"]; ok {
		t.Fatalf("alias forwarded: %v", args)
	}
	if _, ok := args["ssh_password"]; ok {
		t.Fatalf("password forwarded on a read: %v", args)
	}
}

func TestLocalTaskNormalizesATopLevelPasswordAlias(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: "running"}, nil
	}
	_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "create_task", "prompt": "登录用 s3cret-not-in-result", "sshPassword": "s3cret-not-in-result",
	})
	if err != nil {
		t.Fatal(err)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if args["ssh_password"] != "s3cret-not-in-result" {
		t.Fatalf("args = %+v", args)
	}
	if _, ok := args["sshPassword"]; ok {
		t.Fatalf("alias left in place: %v", args)
	}
	prompt, _ := args["prompt"].(string)
	if strings.Contains(prompt, "s3cret") || !strings.Contains(prompt, "登录用") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestLocalTaskArgumentsWinOverTopLevelAliases(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: "running"}, nil
	}
	_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool":              "create_task",
		"ssh_password":      "top-level-secret-xx",
		"client_request_id": "from-top",
		"working_dir":       `D:\from-top`,
		"prompt":            "登录用 nested-secret-value 以及 top-level-secret-xx",
		"arguments": map[string]any{
			"sshPassword":     "nested-secret-value",
			"clientRequestId": "from-arguments",
			"workingDir":      `D:\from-args`,
			"instruction":     "以参数为准 nested-secret-value 以及 top-level-secret-xx",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if args["ssh_password"] != "nested-secret-value" || args["client_request_id"] != "from-arguments" || args["working_dir"] != `D:\from-args` {
		t.Fatalf("args = %+v", args)
	}
	for _, key := range []string{"sshPassword", "password", "clientRequestId", "workingDir", "work_dir", "instruction"} {
		if _, ok := args[key]; ok {
			t.Fatalf("alias %s left in place: %+v", key, args)
		}
	}
	prompt, _ := args["prompt"].(string)
	if strings.Contains(prompt, "nested-secret") || strings.Contains(prompt, "top-level-secret") || !strings.Contains(prompt, "以参数为准") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestLocalTaskScrubsThePasswordOutOfTheTaskTitle(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: "running"}, nil
	}
	_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool":         "create_task",
		"ssh_password": "s3cret-not-in-result",
		"prompt":       "登录用 s3cret-not-in-result",
		"name":         "任务 s3cret-not-in-result",
		"command":      "echo s3cret-not-in-result",
		"text":         "s3cret-not-in-result",
		"message":      "说明 s3cret-not-in-result",
		"working_dir":  `D:\work\s3cret-not-in-result`,
		"ssh_host":     "s3cret-not-in-result.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if args["ssh_password"] != "s3cret-not-in-result" {
		t.Fatalf("connection lost the password: %+v", args)
	}
	if args["working_dir"] != `D:\work\s3cret-not-in-result` || args["ssh_host"] != "s3cret-not-in-result.example" {
		t.Fatalf("host or directory was rewritten: %+v", args)
	}
	for _, key := range []string{"prompt", "name", "command", "text", "message"} {
		text, _ := args[key].(string)
		if strings.Contains(text, "s3cret") {
			t.Fatalf("%s = %q", key, text)
		}
	}
	if name, _ := args["name"].(string); !strings.Contains(name, "任务") {
		t.Fatalf("name = %q", name)
	}
}

func TestLocalTaskForwardsRetryAndLogFields(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: "ok"}, nil
	}
	_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool":                    "retry",
		"project_path":            `D:\work`,
		"step_ids":                `["T5","6"]`,
		"expected_status_version": 3,
		"dry_run":                 true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["tool"] != "retry_steps" {
		t.Fatalf("tool = %v", got["tool"])
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	ids, _ := args["step_ids"].([]any)
	if len(ids) != 2 || ids[0] != "T5" || ids[1] != "6" || args["expected_status_version"] != float64(3) || args["dry_run"] != true {
		t.Fatalf("args = %+v", args)
	}
	if _, ok := args["wait"]; ok {
		t.Fatalf("retry forwarded wait: %v", args)
	}
	_, err = (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "read_log", "project_path": `D:\work`, "cursor": 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if got["tool"] != "read_log" || args["cursor"] != float64(40) {
		t.Fatalf("log body=%v args=%v", got["tool"], args)
	}
}

func TestLocalTaskExpandsAFencedOrQuotedStepList(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: "ok"}, nil
	}
	cases := []struct {
		name string
		raw  string
		want []any
	}{
		{name: "fence", raw: "```json\n[\"T5\",\"6\"]\n```", want: []any{"T5", "6"}},
		{name: "one-line", raw: "```[\"T5\",\"6\"]```", want: []any{"T5", "6"}},
		{name: "tag-line", raw: "```json [\"T5\"]```", want: []any{"T5"}},
		{name: "quoted", raw: `"[\"T5\"]"`, want: []any{"T5"}},
		{name: "bom", raw: "\uFEFF[\"T5\"]", want: []any{"T5"}},
		{name: "trailing-comma", raw: "[\"T5\", \"6\",]", want: []any{"T5", "6"}},
	}
	for _, tc := range cases {
		_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
			"tool": "retry_steps", "project_path": `D:\work`, "step_ids": tc.raw,
		})
		if err != nil {
			t.Fatal(tc.name, err)
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
			t.Fatal(tc.name, err)
		}
		ids, _ := args["step_ids"].([]any)
		if len(ids) != len(tc.want) {
			t.Fatalf("%s ids = %#v", tc.name, args["step_ids"])
		}
		for i := range tc.want {
			if ids[i] != tc.want[i] {
				t.Fatalf("%s ids = %#v", tc.name, ids)
			}
		}
	}
	_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "retry_steps", "project_path": `D:\work`, "step_ids": "T5, 6",
	})
	if err != nil {
		t.Fatal(err)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if args["step_ids"] != "T5, 6" {
		t.Fatalf("comma list = %#v", args["step_ids"])
	}
	_, err = (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "retry_steps", "project_path": `D:\work`, "step_ids": "```json\nT5, 6\n```",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if args["step_ids"] != "T5, 6" {
		t.Fatalf("fenced comma list = %#v", args["step_ids"])
	}
}

func TestLocalTaskArgumentsSupplyTheCommandVersionAndTool(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: "ok"}, nil
	}
	_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool":                    "send",
		"command":                 "外层命令",
		"expected_status_version": 1,
		"dry_run":                 false,
		"arguments": map[string]any{
			"text":                  "以参数为准",
			"expectedStatusVersion": 4,
			"dryRun":                true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["tool"] != "send_command" {
		t.Fatalf("tool = %v", got["tool"])
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if args["command"] != "以参数为准" || args["expected_status_version"] != float64(4) || args["dry_run"] != true {
		t.Fatalf("args = %+v", args)
	}
	for _, key := range []string{"text", "prompt", "message", "expectedStatusVersion", "dryRun"} {
		if _, ok := args[key]; ok {
			t.Fatalf("alias %s left in place: %+v", key, args)
		}
	}
	_, err = (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool":                    "retry_steps",
		"project_path":            `D:\work`,
		"expected_status_version": 3,
		"arguments":               map[string]any{"expectedStatusVersion": 0, "step_ids": "T5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got["args"].(string)), &args); err != nil {
		t.Fatal(err)
	}
	if args["expected_status_version"] != float64(0) || args["step_ids"] != "T5" {
		t.Fatalf("zero version args = %+v", args)
	}
	_, err = (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "create_task",
		"arguments": map[string]any{
			"tool":         "get_task",
			"project_path": `D:\work`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["tool"] != "get_task" {
		t.Fatalf("tool = %v body=%s", got["tool"], got["args"])
	}
}

func TestLocalTaskMapsResultFieldsOntoTheNextCall(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: "ok"}, nil
	}
	_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "command": "继续", "status_version": 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	parse := func() map[string]any {
		t.Helper()
		var parsed map[string]any
		if err := json.Unmarshal([]byte(got["args"].(string)), &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	args := parse()
	if args["expected_status_version"] != float64(4) {
		t.Fatalf("version args = %+v", args)
	}
	if _, ok := args["status_version"]; ok {
		t.Fatalf("status_version still forwarded: %+v", args)
	}
	_, err = (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "command": "继续",
		"expected_status_version": 2, "status_version": 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	args = parse()
	if args["expected_status_version"] != float64(2) {
		t.Fatalf("explicit version lost: %+v", args)
	}
	_, err = (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "create_task", "prompt": "实现登录", "status_version": 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	args = parse()
	if _, ok := args["expected_status_version"]; ok || args["status_version"] != float64(4) {
		t.Fatalf("create version args = %+v", args)
	}
	_, err = (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "read_log", "project_path": `D:\work`, "next_cursor": 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	args = parse()
	if got["tool"] != "read_log" || args["cursor"] != float64(40) {
		t.Fatalf("log args = %v %+v", got["tool"], args)
	}
	if _, ok := args["next_cursor"]; ok {
		t.Fatalf("next_cursor still forwarded: %+v", args)
	}
	_, err = (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "read_log", "project_path": `D:\work`, "cursor": 10, "next_cursor": 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	args = parse()
	if args["cursor"] != float64(10) {
		t.Fatalf("explicit cursor lost: %+v", args)
	}
}

func TestLocalTaskMapsTaskIdentityAndStepObjects(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: "ok"}, nil
	}
	call := func(args map[string]any) map[string]any {
		t.Helper()
		_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, args)
		if err != nil {
			t.Fatal(err)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(got["args"].(string)), &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	args := call(map[string]any{"tool": "get_task", "task_id": `D:\work`})
	if got["tool"] != "get_task" || args["project_path"] != `D:\work` {
		t.Fatalf("task_id args = %v %+v", got["tool"], args)
	}
	if _, ok := args["task_id"]; ok {
		t.Fatalf("task_id still forwarded: %+v", args)
	}
	args = call(map[string]any{"tool": "get_task", "project_path": `D:\keep`, "taskId": `D:\other`})
	if args["project_path"] != `D:\keep` {
		t.Fatalf("explicit project_path lost: %+v", args)
	}
	if _, ok := args["task_id"]; ok {
		t.Fatalf("taskId still forwarded: %+v", args)
	}
	args = call(map[string]any{"tool": "read_log", "project_path": `D:\work`, "current_run_id": "run-9"})
	if got["tool"] != "read_log" || args["run_id"] != "run-9" {
		t.Fatalf("current_run_id args = %v %+v", got["tool"], args)
	}
	if _, ok := args["current_run_id"]; ok {
		t.Fatalf("current_run_id still forwarded: %+v", args)
	}
	args = call(map[string]any{"tool": "read_log", "project_path": `D:\work`, "run_id": "run-keep", "currentRunId": "run-other"})
	if args["run_id"] != "run-keep" {
		t.Fatalf("explicit run_id lost: %+v", args)
	}
	args = call(map[string]any{
		"tool": "retry_steps", "project_path": `D:\work`,
		"steps": []any{
			map[string]any{"id": "T5", "status": "failed"},
			map[string]any{"id": "T6", "status": "passed", "title": "收尾"},
		},
	})
	ids, _ := args["step_ids"].([]any)
	if got["tool"] != "retry_steps" || len(ids) != 1 || ids[0] != "T5" {
		t.Fatalf("step objects = %v %+v", got["tool"], args)
	}
	if strings.Contains(got["args"].(string), "收尾") {
		t.Fatalf("passed step forwarded: %s", got["args"])
	}
	if _, ok := args["steps"]; ok {
		t.Fatalf("steps still forwarded: %+v", args)
	}
	args = call(map[string]any{
		"tool": "retry_steps", "project_path": `D:\work`,
		"step_ids": "[{\"id\":\"T5\",\"status\":\"failed\"},{\"step_id\":\"T7\"},]",
	})
	ids, _ = args["step_ids"].([]any)
	if len(ids) != 2 || ids[0] != "T5" || ids[1] != "T7" {
		t.Fatalf("step object text = %+v", args)
	}
	args = call(map[string]any{
		"tool": "retry_steps", "project_path": `D:\work`,
		"steps": []any{map[string]any{"id": float64(5), "status": "failed"}},
	})
	ids, _ = args["step_ids"].([]any)
	if len(ids) != 1 || ids[0] != "5" {
		t.Fatalf("numeric step id = %+v", args)
	}
	args = call(map[string]any{"tool": "create_task", "prompt": "实现登录", "client_request_id": 42})
	if args["client_request_id"] != "42" || args["wait"] != false {
		t.Fatalf("int request id = %+v", args)
	}
	args = call(map[string]any{
		"tool":      "create_task",
		"arguments": `{"prompt":"实现登录","client_request_id":42}`,
	})
	if args["client_request_id"] != "42" {
		t.Fatalf("json request id = %+v", args)
	}
	args = call(map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "command": "继续", "confirmation_id": 7,
	})
	if args["confirmation_id"] != "7" {
		t.Fatalf("confirmation id = %+v", args)
	}
	args = call(map[string]any{"tool": "create_task", "prompt": "实现登录", "request_id": "mcp-1"})
	if _, ok := args["client_request_id"]; ok || args["request_id"] != "mcp-1" {
		t.Fatalf("request_id args = %+v", args)
	}
	args = call(map[string]any{"tool": "get_task", "parent_task_id": `D:\parent`})
	if _, ok := args["project_path"]; ok || args["parent_task_id"] != `D:\parent` {
		t.Fatalf("parent task args = %+v", args)
	}
}

func TestLocalTaskUsesOneListedCommandAndLiftsTheResultEnvelope(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	calls := 0
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		calls++
		got = body
		return localRelayReply{OK: true, Text: "ok"}, nil
	}
	call := func(args map[string]any) map[string]any {
		t.Helper()
		before := calls
		_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, args)
		if err != nil {
			t.Fatal(err)
		}
		if calls != before+1 {
			t.Fatalf("posted %d", calls-before)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(got["args"].(string)), &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	reject := func(args map[string]any) {
		t.Helper()
		before := calls
		_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, args)
		if err == nil || !strings.Contains(err.Error(), "one") || calls != before {
			t.Fatalf("err=%v posted=%d", err, calls-before)
		}
	}
	args := call(map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "confirmation_id": "mcf_1",
		"commands": []any{"/plan approve"},
	})
	if got["tool"] != "send_command" || args["command"] != "/plan approve" || args["wait"] != false {
		t.Fatalf("one command = %v %+v", got["tool"], args)
	}
	if _, ok := args["commands"]; ok {
		t.Fatalf("commands still forwarded: %+v", args)
	}
	args = call(map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "commands": `["是"]`,
	})
	if args["command"] != "是" {
		t.Fatalf("commands text = %+v", args)
	}
	args = call(map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "text": "previous answer",
		"commands": []any{"/plan approve"},
	})
	if args["command"] != "/plan approve" {
		t.Fatalf("confirmation command = %+v", args)
	}
	if _, ok := args["text"]; ok || strings.Contains(got["args"].(string), "previous answer") {
		t.Fatalf("answer sent as the command: %s", got["args"])
	}
	args = call(map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "text": "继续",
	})
	if args["command"] != "继续" {
		t.Fatalf("text command = %+v", args)
	}
	args = call(map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "prompt": "继续处理",
		"text": "previous answer", "commands": []any{"/plan approve"},
	})
	if args["command"] != "继续处理" {
		t.Fatalf("prompt command = %+v", args)
	}
	if strings.Contains(got["args"].(string), "previous answer") {
		t.Fatalf("prompt call forwarded the answer: %s", got["args"])
	}
	args = call(map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "command": "继续",
		"commands": []any{"是", "否"},
	})
	if args["command"] != "继续" {
		t.Fatalf("explicit command = %+v", args)
	}
	args = call(map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "command": []any{"/plan approve"},
	})
	if args["command"] != "/plan approve" {
		t.Fatalf("command array = %+v", args)
	}
	reject(map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "commands": []any{"是", "否"},
	})
	reject(map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "text": "previous answer",
		"commands": []any{"是", "否"},
	})
	reject(map[string]any{
		"tool": "send_command", "project_path": `D:\work`, "command": []any{"是", "否"},
	})
	args = call(map[string]any{
		"tool": "send_command",
		"task": map[string]any{
			"project_path":    `D:\work`,
			"confirmation_id": "mcf_1",
			"commands":        []any{"/plan approve"},
			"text":            "previous answer",
			"status_version":  4,
			"request_id":      "mcp-1",
			"current_run_id":  "run-9",
		},
	})
	if args["command"] != "/plan approve" || args["project_path"] != `D:\work` || args["confirmation_id"] != "mcf_1" || args["run_id"] != "run-9" || args["expected_status_version"] != float64(4) {
		t.Fatalf("task envelope = %+v", args)
	}
	if _, ok := args["client_request_id"]; ok || args["request_id"] != nil || args["text"] != nil || args["task"] != nil || args["status_version"] != nil {
		t.Fatalf("envelope extras = %+v", args)
	}
	if strings.Contains(got["args"].(string), "previous answer") {
		t.Fatalf("answer text forwarded: %s", got["args"])
	}
	args = call(map[string]any{
		"tool": "retry_steps",
		"task": map[string]any{
			"task_id":         `D:\work`,
			"confirmation_id": "mcf_2",
			"steps":           []any{map[string]any{"id": "T5", "status": "failed"}},
		},
	})
	ids, _ := args["step_ids"].([]any)
	if got["tool"] != "retry_steps" || args["project_path"] != `D:\work` || args["confirmation_id"] != "mcf_2" || len(ids) != 1 || ids[0] != "T5" {
		t.Fatalf("retry envelope = %v %+v", got["tool"], args)
	}
	if _, ok := args["task"]; ok {
		t.Fatalf("task still forwarded: %+v", args)
	}
	args = call(map[string]any{
		"tool": "stop_task",
		"task": map[string]any{"project_path": `D:\work`, "status_version": 3},
	})
	if args["project_path"] != `D:\work` || args["expected_status_version"] != float64(3) {
		t.Fatalf("stop envelope = %+v", args)
	}
	if _, ok := args["status_version"]; ok {
		t.Fatalf("stop status_version still forwarded: %+v", args)
	}
	args = call(map[string]any{
		"tool":  "get_task",
		"scope": map[string]any{"task_id": `D:\work`, "action": "create"},
	})
	if args["project_path"] != `D:\work` {
		t.Fatalf("scope args = %+v", args)
	}
	if _, ok := args["scope"]; ok {
		t.Fatalf("scope still forwarded: %+v", args)
	}
	args = call(map[string]any{
		"tool": "get_task", "project_path": `D:\keep`,
		"scope": map[string]any{"task_id": `D:\other`},
	})
	if args["project_path"] != `D:\keep` {
		t.Fatalf("explicit path lost: %+v", args)
	}
	args = call(map[string]any{
		"tool":         "get_task",
		"project_path": `D:\top`,
		"arguments":    map[string]any{"scope": map[string]any{"task_id": `D:\from-args`}},
	})
	if args["project_path"] != `D:\from-args` {
		t.Fatalf("arguments scope = %+v", args)
	}
	args = call(map[string]any{
		"tool":  "get_task",
		"scope": `{"task_id":"D:\\work"}`,
	})
	if args["project_path"] != `D:\work` {
		t.Fatalf("scope text = %+v", args)
	}
	args = call(map[string]any{
		"tool":   "create_task",
		"prompt": "实现登录",
		"task":   map[string]any{"status_version": 4, "text": "previous answer"},
	})
	if args["prompt"] != "实现登录" {
		t.Fatalf("create prompt = %+v", args)
	}
	if _, ok := args["expected_status_version"]; ok || args["status_version"] != nil || args["task"] != nil {
		t.Fatalf("create envelope = %+v", args)
	}
	if strings.Contains(got["args"].(string), "previous answer") {
		t.Fatalf("create forwarded the nested answer: %s", got["args"])
	}
}

func TestLocalTaskUsesOneListedTaskAndDropsResultNoise(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	calls := 0
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		calls++
		got = body
		return localRelayReply{OK: true, Text: "ok"}, nil
	}
	call := func(args map[string]any) map[string]any {
		t.Helper()
		before := calls
		_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, args)
		if err != nil {
			t.Fatal(err)
		}
		if calls != before+1 {
			t.Fatalf("posted %d", calls-before)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(got["args"].(string)), &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	absent := func(args map[string]any, keys ...string) {
		t.Helper()
		for _, key := range keys {
			if _, ok := args[key]; ok {
				t.Fatalf("%s still forwarded: %+v", key, args)
			}
		}
	}
	args := call(map[string]any{
		"tool":  "get_task",
		"tasks": []any{map[string]any{"project_path": `D:\work`, "text": "previous answer"}},
	})
	if args["project_path"] != `D:\work` {
		t.Fatalf("one task = %+v", args)
	}
	absent(args, "tasks", "text")
	if strings.Contains(got["args"].(string), "previous answer") {
		t.Fatalf("task text forwarded: %s", got["args"])
	}
	args = call(map[string]any{"tool": "get_task", "tasks": []any{`D:\work`}})
	if args["project_path"] != `D:\work` {
		t.Fatalf("task path string = %+v", args)
	}
	args = call(map[string]any{"tool": "get_task", "tasks": `[{"project_path":"D:\\work"},]`})
	if args["project_path"] != `D:\work` {
		t.Fatalf("task list text = %+v", args)
	}
	args = call(map[string]any{
		"tool":    "get_task",
		"task_id": `D:\keep`,
		"tasks": []any{
			map[string]any{"project_path": `D:\a`},
			map[string]any{"project_path": `D:\b`},
		},
	})
	if args["project_path"] != `D:\keep` {
		t.Fatalf("chosen path = %+v", args)
	}
	absent(args, "tasks")
	before := calls
	_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "get_task",
		"tasks": []any{
			map[string]any{"project_path": `D:\a`},
			map[string]any{"project_path": `D:\b`},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "project_path") || calls != before {
		t.Fatalf("err=%v posted=%d", err, calls-before)
	}
	args = call(map[string]any{
		"tool":         "get_task",
		"project_path": `D:\top`,
		"arguments":    map[string]any{"tasks": []any{map[string]any{"project_path": `D:\from-args`}}},
	})
	if args["project_path"] != `D:\from-args` {
		t.Fatalf("arguments task list = %+v", args)
	}
	args = call(map[string]any{
		"tool": "send_command",
		"tasks": []any{map[string]any{
			"project_path":   `D:\work`,
			"commands":       []any{"/plan approve"},
			"text":           "previous answer",
			"status_version": 4,
			"request_id":     "mcp-1",
		}},
	})
	if got["tool"] != "send_command" || args["command"] != "/plan approve" || args["project_path"] != `D:\work` || args["expected_status_version"] != float64(4) {
		t.Fatalf("send from list = %v %+v", got["tool"], args)
	}
	absent(args, "tasks", "commands", "client_request_id", "request_id", "status_version")
	if strings.Contains(got["args"].(string), "previous answer") {
		t.Fatalf("list answer forwarded: %s", got["args"])
	}
	args = call(map[string]any{
		"tool": "retry_steps",
		"tasks": []any{map[string]any{
			"task_id":         `D:\work`,
			"confirmation_id": "mcf_9",
			"steps":           []any{map[string]any{"id": "T5", "status": "failed"}},
		}},
	})
	ids, _ := args["step_ids"].([]any)
	if got["tool"] != "retry_steps" || args["project_path"] != `D:\work` || args["confirmation_id"] != "mcf_9" || len(ids) != 1 || ids[0] != "T5" {
		t.Fatalf("retry from list = %v %+v", got["tool"], args)
	}
	args = call(map[string]any{
		"tool":   "create_task",
		"prompt": "实现登录",
		"tasks": []any{
			map[string]any{"project_path": `D:\a`, "text": "previous answer"},
			map[string]any{"project_path": `D:\b`},
		},
	})
	if args["prompt"] != "实现登录" {
		t.Fatalf("create with list = %+v", args)
	}
	absent(args, "tasks")
	if strings.Contains(got["args"].(string), "previous answer") {
		t.Fatalf("create list forwarded the answer: %s", got["args"])
	}
	args = call(map[string]any{
		"tool": "get_task",
		"task": "```json\n{\"task_id\":\"D:\\\\work\",}\n```",
	})
	if args["project_path"] != `D:\work` {
		t.Fatalf("fenced task = %+v", args)
	}
	absent(args, "task")
	args = call(map[string]any{
		"tool":         "read_log",
		"project_path": `D:\work`,
		"next_cursor":  40,
		"events":       []any{map[string]any{"text": "previous log line"}},
		"checks":       []any{map[string]any{"detail": "check marker"}},
		"children":     []any{map[string]any{"project_path": `D:\child`}},
		"conflicts":    []any{map[string]any{"detail": "conflict marker"}},
		"text":         "previous answer",
		"error":        "failure detail",
		"end_detail":   "end detail marker",
	})
	if got["tool"] != "read_log" || args["cursor"] != float64(40) || args["project_path"] != `D:\work` {
		t.Fatalf("log page = %v %+v", got["tool"], args)
	}
	absent(args, "events", "checks", "children", "conflicts", "next_cursor", "text", "error", "end_detail")
	body := got["args"].(string)
	for _, marker := range []string{"previous log line", "check marker", "conflict marker", "previous answer", "failure detail", "end detail marker", `D:\\child`} {
		if strings.Contains(body, marker) {
			t.Fatalf("result noise %q in %s", marker, body)
		}
	}
}

func TestLocalTaskSendKeepsTheCurrentModel(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		got = body
		return localRelayReply{OK: true, Text: "ok"}, nil
	}
	call := func(args map[string]any) map[string]any {
		t.Helper()
		_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, args)
		if err != nil {
			t.Fatal(err)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(got["args"].(string)), &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	absent := func(args map[string]any, keys ...string) {
		t.Helper()
		for _, key := range keys {
			if _, ok := args[key]; ok {
				t.Fatalf("%s still forwarded: %+v", key, args)
			}
		}
	}
	args := call(map[string]any{
		"tool":               "send_command",
		"project_path":       `D:\work`,
		"confirmation_id":    "mcf_1",
		"commands":           []any{"__scope_approval__ allow"},
		"provider":           "echo-provider",
		"provider_effective": "echo-provider",
		"model":              "echo-model",
		"model_effective":    "echo-model",
		"provider_requested": "asked-provider",
		"model_requested":    "asked-model",
	})
	if args["command"] != "__scope_approval__ allow" || args["confirmation_id"] != "mcf_1" {
		t.Fatalf("confirmation = %+v", args)
	}
	absent(args, "provider", "model", "provider_effective", "model_effective", "provider_requested", "model_requested")
	body := got["args"].(string)
	for _, marker := range []string{"echo-provider", "echo-model", "asked-provider", "asked-model"} {
		if strings.Contains(body, marker) {
			t.Fatalf("echoed model %q in %s", marker, body)
		}
	}
	args = call(map[string]any{
		"tool":         "send_command",
		"project_path": `D:\work`,
		"command":      "继续",
		"provider":     "Next-provider",
		"model":        "next-model",
	})
	if args["provider"] != "Next-provider" || args["model"] != "next-model" || args["command"] != "继续" {
		t.Fatalf("explicit model = %+v", args)
	}
	args = call(map[string]any{
		"tool":               "send_command",
		"project_path":       `D:\work`,
		"command":            "继续",
		"provider":           "Next-provider",
		"provider_effective": "echo-provider",
		"model":              "echo-model",
		"model_effective":    "echo-model",
	})
	if args["provider"] != "Next-provider" || args["model"] != "echo-model" || args["command"] != "继续" {
		t.Fatalf("provider change = %+v", args)
	}
	absent(args, "provider_effective", "model_effective")
	args = call(map[string]any{
		"tool":               "send_command",
		"project_path":       `D:\work`,
		"command":            "继续",
		"provider":           "echo-provider",
		"provider_effective": "echo-provider",
		"model":              "echo-model",
		"model_effective":    "echo-model",
		"arguments":          map[string]any{"provider": "Next-provider", "model": "next-model"},
	})
	if args["provider"] != "Next-provider" || args["model"] != "next-model" {
		t.Fatalf("arguments model = %+v", args)
	}
	args = call(map[string]any{
		"tool":               "send_command",
		"project_path":       `D:\work`,
		"command":            "继续",
		"provider_effective": "echo-provider",
		"model":              "next-model",
		"model_effective":    "echo-model",
	})
	if args["provider"] != "echo-provider" || args["model"] != "next-model" {
		t.Fatalf("model change keeps provider = %+v", args)
	}
	args = call(map[string]any{
		"tool":               "send_command",
		"project_path":       `D:\work`,
		"command":            "继续",
		"provider":           "Echo-Provider",
		"provider_effective": "echo-provider",
		"model":              "Echo-Model",
		"model_effective":    "echo-model",
	})
	absent(args, "provider", "model", "provider_effective", "model_effective")
	args = call(map[string]any{
		"tool":               "create_task",
		"prompt":             "实现登录",
		"provider":           "echo-provider",
		"provider_effective": "echo-provider",
		"model":              "echo-model",
	})
	if args["prompt"] != "实现登录" || args["provider"] != "echo-provider" || args["model"] != "echo-model" {
		t.Fatalf("create model = %+v", args)
	}
}

func TestLocalTaskRetryKeepsOpenSteps(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	var got map[string]any
	calls := 0
	localRelayPost = func(_ context.Context, body map[string]any) (localRelayReply, error) {
		calls++
		got = body
		return localRelayReply{OK: true, Text: "ok"}, nil
	}
	call := func(args map[string]any) map[string]any {
		t.Helper()
		before := calls
		_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, args)
		if err != nil {
			t.Fatal(err)
		}
		if calls != before+1 {
			t.Fatalf("posted %d", calls-before)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(got["args"].(string)), &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	idsOf := func(args map[string]any) []any {
		t.Helper()
		ids, _ := args["step_ids"].([]any)
		return ids
	}
	args := call(map[string]any{
		"tool": "retry_steps", "project_path": `D:\work`, "confirmation_id": "mcf_3",
		"steps": []any{
			map[string]any{"id": "T1", "status": "passed", "retryable": false, "title": "已通过"},
			map[string]any{"id": "T2", "status": "failed", "retryable": true, "error": "disk full"},
			map[string]any{"id": "T3", "status": "skipped", "retryable": false, "error": "skipped: prior step failed"},
			map[string]any{"id": "T4", "status": "blocked", "retryable": true},
			map[string]any{"id": "T8", "status": "waiting", "retryable": false, "error": "子任务结果待审阅"},
			map[string]any{"id": "T9", "status": "skipped", "retryable": true},
		},
	})
	ids := idsOf(args)
	if len(ids) != 3 || ids[0] != "T2" || ids[1] != "T4" || ids[2] != "T9" || args["confirmation_id"] != "mcf_3" {
		t.Fatalf("open steps = %+v", args)
	}
	body := got["args"].(string)
	for _, marker := range []string{"已通过", "disk full", "prior step", "子任务结果待审阅", "T1", "T3", "T8"} {
		if strings.Contains(body, marker) {
			t.Fatalf("closed step %q in %s", marker, body)
		}
	}
	args = call(map[string]any{
		"tool": "retry_steps", "project_path": `D:\work`, "step_ids": []any{"T5", "T6"},
	})
	ids = idsOf(args)
	if len(ids) != 2 || ids[0] != "T5" || ids[1] != "T6" {
		t.Fatalf("explicit ids = %+v", args)
	}
	before := calls
	_, err := (localRelayModule{}).InvokeTool(context.Background(), localTaskTurn("execute"), localTaskToolName, map[string]any{
		"tool": "retry_steps", "project_path": `D:\work`,
		"steps": []any{
			map[string]any{"id": "T1", "status": "passed"},
			map[string]any{"id": "T3", "status": "skipped"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "step") || calls != before {
		t.Fatalf("err=%v posted=%d", err, calls-before)
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

func TestLocalTaskTimeoutHintOnlyWhenTheCallWasInFlight(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	scope := agentruntime.Scope{TenantID: "t1", UserID: "user-a"}
	plain := `{"project_path":"D:\\work"}`
	check := func(name, tool, payload string, reply localRelayReply, postErr error, secrets []string, want string, wantCode string) {
		t.Helper()
		localRelayPost = func(context.Context, map[string]any) (localRelayReply, error) {
			return reply, postErr
		}
		_, err := localTaskCall(context.Background(), scope, tool, payload, secrets)
		if err == nil {
			t.Fatalf("%s: nil error", name)
		}
		if want == "" {
			if strings.Contains(err.Error(), "starting another") || strings.Contains(err.Error(), "get_request") {
				t.Fatalf("%s err=%v", name, err)
			}
		} else if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s err=%v", name, err)
		}
		if !strings.Contains(want, "starting another") && strings.Contains(err.Error(), "starting another") {
			t.Fatalf("%s err=%v", name, err)
		}
		if code := localrelay.CodeOf(err); code != wantCode {
			t.Fatalf("%s code=%s err=%v", name, code, err)
		}
		for _, secret := range secrets {
			if secret != "" && strings.Contains(err.Error(), secret) {
				t.Fatalf("%s leaked secret: %v", name, err)
			}
		}
	}
	timedOut := localRelayReply{Code: localrelay.CodeTimeout, Error: "the person's computer did not answer in time"}
	check("create relay timeout", "create_task", plain, timedOut, nil, nil, "use list_tasks before starting another", localrelay.CodeTimeout)
	check("retry relay timeout", "retry_steps", plain, timedOut, nil, nil, "use get_task before starting another", localrelay.CodeTimeout)
	check("send deadline", "send_command", plain, localRelayReply{}, fmt.Errorf("Hub is unreachable: context deadline exceeded"), nil, "use get_task before starting another", localrelay.CodeFailed)
	check("create with id", "create_task", `{"client_request_id":"c-9","prompt":"实现登录"}`, timedOut, nil, nil, "use get_request with client_request_id c-9 before starting another", localrelay.CodeTimeout)
	leaky := localRelayReply{Code: localrelay.CodeTimeout, Error: "the person's computer did not answer in time s3cret-not-in-result"}
	check("create timeout keeps code", "create_task", `{"client_request_id":"c-9"}`, leaky, nil, []string{"s3cret-not-in-result"}, "client_request_id c-9 before starting another", localrelay.CodeTimeout)
	check("get relay timeout", "get_task", plain, timedOut, nil, nil, "", localrelay.CodeTimeout)
	check("read relay timeout", "read_log", plain, timedOut, nil, nil, "", localrelay.CodeTimeout)
	check("create client timeout", "create_task", plain, localRelayReply{}, fmt.Errorf("Hub is unreachable: Client.Timeout exceeded while awaiting headers"), nil, "use list_tasks before starting another", localrelay.CodeFailed)
	check("create dial timeout", "create_task", plain, localRelayReply{}, fmt.Errorf("Hub is unreachable: dial tcp 10.0.0.8:443: i/o timeout"), nil, "", localrelay.CodeFailed)
	check("stop relay timeout", "stop_task", plain, timedOut, nil, nil, "use get_task to see the task", localrelay.CodeTimeout)
	check("stop dial timeout", "stop_task", plain, localRelayReply{}, fmt.Errorf("Hub is unreachable: connect: connection refused"), nil, "", localrelay.CodeFailed)
}

func TestLocalTaskSaysWhenTheAppIsOffline(t *testing.T) {
	prev := localRelayPost
	t.Cleanup(func() { localRelayPost = prev })
	localRelayPost = func(context.Context, map[string]any) (localRelayReply, error) {
		return localRelayReply{Code: localrelay.CodeOffline, Error: localrelay.ErrOffline.Message}, nil
	}
	_, err := localTaskCall(context.Background(), agentruntime.Scope{TenantID: "t1", UserID: "user-a"}, "get_task", `{"project_path":"D:\\work"}`, nil)
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
