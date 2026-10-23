package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestDesktopNotifyMainPostsReportAndCapsPerRun(t *testing.T) {
	var got []map[string]any
	desktopRemoteNotify = func(_ context.Context, body map[string]any) (map[string]any, error) {
		got = append(got, body)
		return map[string]any{"id": "note_1"}, nil
	}
	defer func() { desktopRemoteNotify = nil }()
	inst := agentservice.Instance{ID: "inst_pub", Metadata: map[string]string{"hub_user_id": "alice", "hub_tenant_id": "tenant-a"}}
	req := agentruntime.TurnRequest{Scope: agentruntime.Scope{RunID: "run_notify_1"}, Input: agentservice.ExecuteRequest{Instance: inst}}
	out, err := desktopNotifyMain(context.Background(), req, map[string]any{"action": "notify_main", "kind": "artifact", "message": "截图好了", "paths": []any{"/home/desktop/Desktop/s.png"}})
	if err != nil || !strings.Contains(out, "Sent to the main bot") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	b := got[0]
	if b["instance_id"] != "inst_pub" || b["user_id"] != "alice" || b["kind"] != "artifact" || b["run_id"] != "run_notify_1" {
		t.Fatalf("body = %#v", b)
	}
	if arts := b["artifacts"].([]string); len(arts) != 1 {
		t.Fatalf("artifacts = %#v", arts)
	}
	for i := 1; i < desktopNotifyPerRun; i++ {
		if _, err := desktopNotifyMain(context.Background(), req, map[string]any{"message": "m", "kind": "bogus"}); err != nil {
			t.Fatal(err)
		}
	}
	if got[1]["kind"] != "info" {
		t.Fatalf("unknown kind should be info: %v", got[1]["kind"])
	}
	if _, err := desktopNotifyMain(context.Background(), req, map[string]any{"message": "over"}); err == nil {
		t.Fatal("per-run cap")
	}
	if _, err := desktopNotifyMain(context.Background(), req, map[string]any{}); err == nil {
		t.Fatal("empty message must fail")
	}
}
