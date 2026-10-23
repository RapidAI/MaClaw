package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func useTempMultibotState(t *testing.T) string {
	path := filepath.Join(t.TempDir(), "multibot-state.json")
	old := multibotStatePath
	multibotStatePath = func() string { return path }
	t.Cleanup(func() { multibotStatePath = old })
	return path
}

func TestDesktopNotifyMainPostsReportAndCapsPerRun(t *testing.T) {
	useTempMultibotState(t)
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

func TestDesktopInboxTextReadsAndAcksHubMailbox(t *testing.T) {
	useTempMultibotState(t)
	var calls []map[string]any
	desktopRemoteInbox = func(_ context.Context, body map[string]any) ([]map[string]any, error) {
		calls = append(calls, body)
		if body["ack"] != nil {
			return nil, nil
		}
		return []map[string]any{{"id": "note_9", "from": "bot_main", "kind": "report", "topic": "reply", "text": "用第一个标题"}}, nil
	}
	defer func() { desktopRemoteInbox = nil }()
	inst := agentservice.Instance{ID: "inst_pub", Metadata: map[string]string{"hub_user_id": "alice"}}
	req := agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{Instance: inst}}
	text := desktopInboxText(context.Background(), req)
	if !strings.Contains(text, "id=note_9") || !strings.Contains(text, "用第一个标题") || !strings.Contains(text, "kind=reply") {
		t.Fatalf("text = %q", text)
	}
	if len(calls) != 2 || calls[1]["ack"].([]string)[0] != "note_9" || calls[0]["instance_id"] != "inst_pub" {
		t.Fatalf("calls = %#v", calls)
	}
	// The ack was lost and Hub replays note_9: acked again, not shown twice.
	if again := desktopInboxText(context.Background(), req); again != "" {
		t.Fatalf("replayed message shown twice: %q", again)
	}
	if len(calls) != 4 || calls[3]["ack"].([]string)[0] != "note_9" {
		t.Fatalf("replay not re-acked: %#v", calls)
	}
}

func TestDesktopNotifyCapAndKeySurviveRestart(t *testing.T) {
	path := useTempMultibotState(t)
	var keys []string
	desktopRemoteNotify = func(_ context.Context, body map[string]any) (map[string]any, error) {
		keys = append(keys, body["key"].(string))
		return map[string]any{"id": "n"}, nil
	}
	defer func() { desktopRemoteNotify = nil }()
	inst := agentservice.Instance{ID: "inst_pub", Metadata: map[string]string{"hub_user_id": "alice"}}
	req := agentruntime.TurnRequest{Scope: agentruntime.Scope{RunID: "run_r"}, Input: agentservice.ExecuteRequest{Instance: inst}}
	for i := 0; i < desktopNotifyPerRun; i++ {
		if _, err := desktopNotifyMain(context.Background(), req, map[string]any{"message": "same"}); err != nil {
			t.Fatal(err)
		}
	}
	if keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("same message must carry the same idempotency key: %v", keys[:2])
	}
	// A restart: the in-process state is gone, the file is not.
	multibotStatePath = func() string { return path }
	if _, err := desktopNotifyMain(context.Background(), req, map[string]any{"message": "after restart"}); err == nil {
		t.Fatal("cap reset by restart")
	}
}

func TestDesktopDelegatePostsToHub(t *testing.T) {
	useTempMultibotState(t)
	var got map[string]any
	desktopRemoteDelegate = func(_ context.Context, body map[string]any) (map[string]any, error) {
		got = body
		return map[string]any{"id": "note_d"}, nil
	}
	defer func() { desktopRemoteDelegate = nil }()
	inst := agentservice.Instance{ID: "inst_main", Metadata: map[string]string{"hub_user_id": "alice", "hub_tenant_id": "tenant-a"}}
	req := agentruntime.TurnRequest{Scope: agentruntime.Scope{RunID: "run_m"}, Input: agentservice.ExecuteRequest{Instance: inst}}
	out, err := desktopDelegate(context.Background(), req, map[string]any{"target": "发布员", "message": "导出本月订单"})
	if err != nil || !strings.Contains(out, "note_d") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got["target"] != "发布员" || got["text"] != "导出本月订单" || got["instance_id"] != "inst_main" || got["key"] == "" {
		t.Fatalf("body = %#v", got)
	}
	if _, err := desktopDelegate(context.Background(), req, map[string]any{"message": "no target"}); err == nil {
		t.Fatal("target required")
	}
}

// A path shared mid-run travels as bytes with the message.
func TestDesktopNotifyMainSendsSharedFileBytes(t *testing.T) {
	useTempMultibotState(t)
	var got map[string]any
	desktopRemoteNotify = func(_ context.Context, body map[string]any) (map[string]any, error) {
		got = body
		return map[string]any{"id": "note_f"}, nil
	}
	oldRead := desktopNotifyReadFile
	desktopNotifyReadFile = func(_ context.Context, _ agentruntime.Scope, p string) (string, string, []byte, error) {
		if strings.HasSuffix(p, "missing.csv") {
			return "", "", nil, context.Canceled
		}
		return "orders.csv", "text/csv", []byte("a,b"), nil
	}
	defer func() { desktopRemoteNotify = nil; desktopNotifyReadFile = oldRead }()
	inst := agentservice.Instance{ID: "inst_pub", Metadata: map[string]string{"hub_user_id": "alice", "hub_tenant_id": "tenant-a"}}
	req := agentruntime.TurnRequest{Scope: agentruntime.Scope{RunID: "run_files"}, Input: agentservice.ExecuteRequest{Instance: inst}}
	reply, err := desktopNotifyMain(context.Background(), req, map[string]any{"kind": "artifact", "message": "导出了", "paths": []any{"/home/desktop/orders.csv", "/home/desktop/missing.csv"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "missing.csv") || !strings.Contains(reply, "not attached") {
		t.Fatalf("the bot is not told which file stayed behind: %q", reply)
	}
	files, _ := got["files"].([]map[string]any)
	if len(files) != 1 || files[0]["name"] != "orders.csv" || files[0]["data"] != "YSxi" {
		t.Fatalf("files = %#v", got["files"])
	}
	if arts := got["artifacts"].([]string); len(arts) != 2 {
		t.Fatalf("paths still listed: %#v", arts)
	}
}

func TestNotifyMainReadsSharedFilesFromTheBotsOwnDesktop(t *testing.T) {
	oldRead := desktopNotifyReadFile
	var seen agentruntime.Scope
	desktopNotifyReadFile = func(_ context.Context, scope agentruntime.Scope, p string) (string, string, []byte, error) {
		seen = scope
		return "a.txt", "text/plain", []byte("x"), nil
	}
	desktopRemoteNotify = func(_ context.Context, body map[string]any) (map[string]any, error) { return map[string]any{}, nil }
	defer func() { desktopRemoteNotify = nil; desktopNotifyReadFile = oldRead }()
	inst := agentservice.Instance{ID: "inst_scope", Metadata: map[string]string{"hub_user_id": "alice", "hub_tenant_id": "tenant-a"}}
	req := agentruntime.TurnRequest{Scope: agentruntime.Scope{RunID: "run_scope", UserID: "raw-run-user"}, Input: agentservice.ExecuteRequest{Instance: inst}}
	if _, err := desktopNotifyMain(context.Background(), req, map[string]any{"kind": "artifact", "message": "done", "paths": []any{"/tmp/a.txt"}}); err != nil {
		t.Fatal(err)
	}
	want, _, _ := desktopScopeForTurn(req)
	if seen.UserID != want.UserID || seen.TenantID != "tenant-a" || seen.UserID == "raw-run-user" {
		t.Fatalf("file read with scope %+v, want the bot desktop scope %+v", seen, want)
	}
}
