package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStampBotRunIdentityKeepsAGivenLine(t *testing.T) {
	run := Run{Metadata: map[string]string{"request_id": "r"}}
	stampBotRunIdentity(&run, map[string]string{"bot_id": "bot_4", "bot_phase": "execute", "bot_task": "北京天气"}, "quiet prompt that should not win")
	if run.Metadata["bot_task"] != "北京天气" || run.Metadata["bot_id"] != "bot_4" || run.Metadata["bot_phase"] != "execute" || run.Metadata["request_id"] != "r" {
		t.Fatalf("metadata = %#v", run.Metadata)
	}
	marked := Run{}
	stampBotRunIdentity(&marked, map[string]string{"bot_id": "bot_none", "bot_desktop": "none"}, "看看新消息")
	if marked.Metadata["bot_desktop"] != "none" || marked.Metadata["bot_id"] != "bot_none" {
		t.Fatalf("desktop mark = %#v", marked.Metadata)
	}
	again := run
	stampBotRunIdentity(&again, map[string]string{"bot_id": "bot_other", "bot_task": "别的任务"}, "别的正文")
	if again.Metadata["bot_id"] != "bot_4" || again.Metadata["bot_task"] != "北京天气" {
		t.Fatalf("overwrite = %#v", again.Metadata)
	}
	long := strings.Repeat("甲", 200)
	fresh := Run{}
	stampBotRunIdentity(&fresh, nil, "  "+long+"  ")
	got := []rune(fresh.Metadata["bot_task"])
	if len(got) != botTaskRunes+1 || !strings.HasSuffix(fresh.Metadata["bot_task"], "…") {
		t.Fatalf("clip = %d %q", len(got), fresh.Metadata["bot_task"])
	}
}

func TestStampSkipsTheRestartSentence(t *testing.T) {
	run := Run{}
	stampBotRunIdentity(&run, nil, restartNoticeContinue)
	if run.Metadata["bot_task"] != "" {
		t.Fatalf("notice stored as the task: %#v", run.Metadata)
	}
	stampBotRunIdentity(&run, nil, "这一轮是例行检查，没有人在等回复。有变化就说。\n每5分钟报告天气")
	if run.Metadata["bot_task"] != "每5分钟报告天气" {
		t.Fatalf("task = %q", run.Metadata["bot_task"])
	}
}

func TestListRunsReplacesTheRestartSentence(t *testing.T) {
	svc, store, principal, inst, sess := setupDeleteInstanceTest(t)
	defer func() { _ = svc.Close() }()
	msg := Message{
		ID: "msg_go", SessionID: sess.ID, TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID,
		Role: MessageRoleUser, Content: "把论文页面抓下来",
		Metadata:  map[string]string{"bot_id": "bot_3"},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.SaveMessage(msg); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRun(Run{
		ID: "run_go", TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID,
		SessionID: sess.ID, UserMessageID: msg.ID, Status: RunStatusRunning, StartedAt: time.Now().UTC(),
		Metadata: map[string]string{"bot_task": restartNoticeContinue},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := svc.ListRuns(context.Background(), principal, inst.ID, ListRunsInput{Status: RunStatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Metadata["bot_task"] != "把论文页面抓下来" || items[0].Metadata["bot_id"] != "bot_3" {
		t.Fatalf("metadata = %#v", items[0].Metadata)
	}
	stored, err := store.GetRun(principal.TenantID, principal.UserID, inst.ID, "run_go")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Metadata["bot_task"] != restartNoticeContinue {
		t.Fatalf("list rewrote the row: %#v", stored.Metadata)
	}
}

func TestListRunsFillsTheTaskFromTheUserMessage(t *testing.T) {
	svc, store, principal, inst, sess := setupDeleteInstanceTest(t)
	defer func() { _ = svc.Close() }()
	msg := Message{
		ID: "msg_weather", SessionID: sess.ID, TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID,
		Role: MessageRoleUser, Content: "每5分钟向我报告下天气情况",
		Metadata:  map[string]string{"bot_id": "bot_4", "bot_origin": "schedule"},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.SaveMessage(msg); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRun(Run{
		ID: "run_weather", TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID,
		SessionID: sess.ID, UserMessageID: msg.ID, Status: RunStatusRunning, StartedAt: time.Now().UTC(),
		Metadata: map[string]string{"request_id": "req-1"},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := svc.ListRuns(context.Background(), principal, inst.ID, ListRunsInput{Status: RunStatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].Metadata["bot_id"] != "bot_4" || items[0].Metadata["bot_origin"] != "schedule" || items[0].Metadata["bot_task"] != "每5分钟向我报告下天气情况" || items[0].Metadata["request_id"] != "req-1" {
		t.Fatalf("metadata = %#v", items[0].Metadata)
	}
	stored, err := store.GetRun(principal.TenantID, principal.UserID, inst.ID, "run_weather")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Metadata["bot_task"] != "" || stored.Metadata["bot_id"] != "" {
		t.Fatalf("list rewrote the row: %#v", stored.Metadata)
	}
}
