package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// Bot 3, 2026-11-10: the GUI's brief for "生成一份简历发我" said "不要发布到任何
// 网站". That negated clause made it a publish task and the publish plan's
// research phase refused deliver six times.
func TestNegatedPublishIsNotAPublishRequest(t *testing.T) {
	brief := "目标：在网上搜集黄仁勋的公开资料，整理成一份中文简历文档，其中包含一张他的照片，并交付到聊天中。\n约束：只使用公开可查的信息；不要向用户索要文件；不要发布到任何网站，也不要发送给第三方。"
	if desktopPublishAskRe.MatchString(brief) {
		t.Fatal("a brief that forbids publishing was read as a publish request")
	}
	for _, s := range []string{"别发到知乎，做成文件给我", "do not post it to any site, just send me the file", "无需发布到CSDN"} {
		if desktopPublishAskRe.MatchString(s) {
			t.Fatalf("negated %q read as publish", s)
		}
	}
	for _, s := range []string{"写一篇文章发布到CSDN", "发到知乎", "在小红书上发一篇笔记", "不要用英文，发布到知乎", "post it to zhihu"} {
		if !desktopPublishAskRe.MatchString(s) {
			t.Fatalf("real publish request %q missed", s)
		}
	}
}

// A deliverable is never refused by the publish plan, in any phase.
func TestPublishPlanNeverRefusesDeliver(t *testing.T) {
	for _, phase := range []desktopPublishPhase{phaseResearch, phaseCompose, phaseSubmitted, phaseLive, phaseIncomplete} {
		if err := desktopPublishPlanCheck(phase, "deliver", map[string]any{"path": "/home/desktop/Desktop/a.docx"}); err != nil {
			t.Fatalf("%s refused deliver: %v", phase, err)
		}
	}
	if err := desktopPublishPlanCheck(phaseResearch, "batch", map[string]any{"plan": []any{map[string]any{"action": "deliver", "path": "/tmp/a.docx"}}}); err != nil {
		t.Fatalf("batch deliver refused: %v", err)
	}
	if err := desktopPublishPlanCheck(phaseResearch, "screenshot", nil); err == nil {
		t.Fatal("the plan must still refuse steps outside its phase")
	}
}

// A run the person started in this bot's chat: Hub says direct, so the file
// is attached to this chat reply instead of being handed to the main bot.
func TestNotifyMainFileFromDirectRunGoesToThePerson(t *testing.T) {
	useTempMultibotState(t)
	var got map[string]any
	desktopRemoteNotify = func(_ context.Context, body map[string]any) (map[string]any, error) {
		got = body
		if body["deliver_if_direct"] == true {
			return map[string]any{"direct": true}, nil
		}
		return map[string]any{"id": "note_x"}, nil
	}
	oldRead, oldDeliver := desktopNotifyReadFile, desktopNotifyDeliverPath
	desktopNotifyReadFile = func(_ context.Context, _ agentruntime.Scope, p string) (string, string, []byte, error) {
		return "黄仁勋_简历.docx", "application/octet-stream", []byte("x"), nil
	}
	var delivered []string
	desktopNotifyDeliverPath = func(_ context.Context, _ agentruntime.Scope, p, _ string) (string, error) {
		delivered = append(delivered, p)
		return desktopDelivered("黄仁勋_简历.docx", 1), nil
	}
	defer func() { desktopRemoteNotify = nil; desktopNotifyReadFile = oldRead; desktopNotifyDeliverPath = oldDeliver }()
	inst := agentservice.Instance{ID: "inst_b3", Metadata: map[string]string{"hub_user_id": "alice", "hub_tenant_id": "tenant-a"}}
	req := agentruntime.TurnRequest{Scope: agentruntime.Scope{RunID: "run_direct"}, Input: agentservice.ExecuteRequest{Instance: inst}}
	out, err := desktopNotifyMain(context.Background(), req, map[string]any{"kind": "artifact", "message": "简历好了", "paths": []any{"/home/desktop/Desktop/黄仁勋_简历.docx"}})
	if err != nil {
		t.Fatal(err)
	}
	if got["deliver_if_direct"] != true {
		t.Fatalf("file report to the main bot must ask Hub whether the run is direct: %#v", got)
	}
	if len(delivered) != 1 || !strings.Contains(out, "not to the main bot") || !strings.Contains(out, "attached") {
		t.Fatalf("out=%q delivered=%v", out, delivered)
	}
	// A message to a named peer, or one with no file, is never redirected.
	got = nil
	if _, err := desktopNotifyMain(context.Background(), req, map[string]any{"kind": "artifact", "target": "Bot 2", "message": "给你", "paths": []any{"/tmp/a.docx"}}); err != nil {
		t.Fatal(err)
	}
	if got["deliver_if_direct"] != nil {
		t.Fatalf("named target asked direct: %#v", got)
	}
	got = nil
	if _, err := desktopNotifyMain(context.Background(), req, map[string]any{"kind": "progress", "message": "一半了"}); err != nil {
		t.Fatal(err)
	}
	if got["deliver_if_direct"] != nil {
		t.Fatalf("progress asked direct: %#v", got)
	}
}

// A delegated run (Hub answers without direct) keeps handing files to the
// main bot, as before; so does an older Hub that ignores the flag.
func TestNotifyMainFileFromDelegatedRunStillGoesToMainBot(t *testing.T) {
	useTempMultibotState(t)
	desktopRemoteNotify = func(_ context.Context, body map[string]any) (map[string]any, error) {
		return map[string]any{"id": "note_d"}, nil
	}
	oldRead, oldDeliver := desktopNotifyReadFile, desktopNotifyDeliverPath
	desktopNotifyReadFile = func(_ context.Context, _ agentruntime.Scope, p string) (string, string, []byte, error) {
		return "a.docx", "application/octet-stream", []byte("x"), nil
	}
	desktopNotifyDeliverPath = func(context.Context, agentruntime.Scope, string, string) (string, error) {
		t.Fatal("delegated run must not deliver here")
		return "", nil
	}
	defer func() { desktopRemoteNotify = nil; desktopNotifyReadFile = oldRead; desktopNotifyDeliverPath = oldDeliver }()
	inst := agentservice.Instance{ID: "inst_b3", Metadata: map[string]string{"hub_user_id": "alice"}}
	req := agentruntime.TurnRequest{Scope: agentruntime.Scope{RunID: "run_del"}, Input: agentservice.ExecuteRequest{Instance: inst}}
	out, err := desktopNotifyMain(context.Background(), req, map[string]any{"kind": "artifact", "message": "好了", "paths": []any{"/tmp/a.docx"}})
	if err != nil || !strings.Contains(out, "Sent to the main bot") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
