package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func assignReq(run string) agentruntime.TurnRequest {
	inst := agentservice.Instance{ID: "inst_a", Metadata: map[string]string{"hub_user_id": "alice", "hub_tenant_id": "tenant-a"}}
	return agentruntime.TurnRequest{Scope: agentruntime.Scope{RunID: run}, Input: agentservice.ExecuteRequest{Instance: inst}}
}

func TestDesktopReportStateMapsKindAndEndsTurnOnQuestion(t *testing.T) {
	useTempMultibotState(t)
	var got []map[string]any
	desktopRemoteNotify = func(_ context.Context, body map[string]any) (map[string]any, error) {
		got = append(got, body)
		return map[string]any{"id": "n"}, nil
	}
	defer func() { desktopRemoteNotify = nil }()
	out, err := desktopNotifyMain(context.Background(), assignReq("r1"), map[string]any{"action": "report", "state": "waiting_human", "message": "用哪个标题?", "reply_to": "d1"})
	if err != nil || !strings.Contains(out, "end this turn") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if b := got[0]; b["kind"] != "question" || b["state"] != "waiting_human" || b["expects_reply"] != true {
		t.Fatalf("body = %#v", b)
	}
	if _, err := desktopNotifyMain(context.Background(), assignReq("r1"), map[string]any{"action": "report", "state": "completed", "message": "好了", "answer_to": "q9"}); err != nil {
		t.Fatal(err)
	}
	if b := got[1]; b["kind"] != "done" || b["answer_to"] != "q9" || b["expects_reply"] != nil {
		t.Fatalf("body = %#v", b)
	}
}

func TestDesktopAssignmentActions(t *testing.T) {
	var paths []string
	var bodies []map[string]any
	desktopRemoteAssign = func(_ context.Context, path string, body map[string]any) (map[string]any, error) {
		paths, bodies = append(paths, path), append(bodies, body)
		if strings.HasSuffix(path, "/assignments") {
			return map[string]any{"items": []any{map[string]any{"id": "d1", "target_bot": "bot_b", "state": "completed", "task": "写稿", "reviewer": "bot_c"}}}, nil
		}
		return map[string]any{"id": "d1", "state": "returned"}, nil
	}
	defer func() { desktopRemoteAssign = nil }()
	out, err := desktopAssignmentAction(context.Background(), assignReq("r"), "list_assignments", nil)
	if err != nil || !strings.Contains(out, "ASSIGNMENT id=d1 to=bot_b state=completed reviewer=bot_c") {
		t.Fatalf("list = %q %v", out, err)
	}
	if _, err := desktopAssignmentAction(context.Background(), assignReq("r"), "return_result", map[string]any{}); err == nil {
		t.Fatal("missing id must fail")
	}
	out, err = desktopAssignmentAction(context.Background(), assignReq("r"), "return_result", map[string]any{"assignment_id": "d1", "note": "太长"})
	if err != nil || !strings.Contains(out, "returned") || bodies[1]["verdict"] != "returned" || bodies[1]["note"] != "太长" {
		t.Fatalf("review = %q %v %#v", out, err, bodies)
	}
	t.Setenv("MACLAW_ASSIGNMENTS", "off")
	if _, err := desktopAssignmentAction(context.Background(), assignReq("r"), "list_assignments", nil); err == nil || desktopAssignmentToolHint() != "" {
		t.Fatal("kill switch")
	}
}

func TestDesktopSayPostsAndSkipsRepeat(t *testing.T) {
	calls := 0
	desktopRemoteAssign = func(_ context.Context, path string, body map[string]any) (map[string]any, error) {
		calls++
		if path != "/api/v1/desktop-services/say" || body["text"] == "" {
			t.Fatalf("path=%s body=%#v", path, body)
		}
		if calls == 2 {
			return nil, fmt.Errorf("ALREADY_SAID by bot_main")
		}
		return map[string]any{"id": "s1"}, nil
	}
	defer func() { desktopRemoteAssign = nil }()
	if out, err := desktopSay(context.Background(), assignReq("r"), map[string]any{"message": "好了"}); err != nil || out != "SAID" {
		t.Fatalf("%q %v", out, err)
	}
	if out, err := desktopSay(context.Background(), assignReq("r"), map[string]any{"message": "好了!"}); err != nil || !strings.Contains(out, "already_said") {
		t.Fatalf("%q %v", out, err)
	}
}
