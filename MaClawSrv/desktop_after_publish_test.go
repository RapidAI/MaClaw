package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// After action=publish verified the post, any further look is refused with
// the published link so the run replies instead of hunting.
func TestAfterVerifiedPublishStopsTheHunt(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-ap", InstanceID: "i", SessionID: "s", RunID: "run-ap"}
	st, _ := desktopProgressFor(scope)
	if st == nil {
		t.Skip("no run state")
	}
	desktopPublishRecordUpdate(scope, func(r *desktopPublishRecord) { r.State, r.URL = browser.PublishLive, "https://x.example/p/1" })
	desktopProgressNote(scope, "publish", "verify_result: ok", nil)
	req := "在小红书发布一篇笔记"
	err := desktopDriftCheck(scope, "probe", nil, req)
	if err == nil {
		t.Fatal("a probe after a verified publish must be refused")
	}
	if !strings.Contains(err.Error(), "https://x.example/p/1") {
		t.Fatalf("refusal must repeat the published link: %v", err)
	}
}

func TestPostURLForListedID(t *testing.T) {
	if got := desktopPostURLFor(browser.PublishSpec{VerifyURL: "https://creator.xiaohongshu.com/new/note-manager"}, "6adbf6450000000024036add"); got != "https://www.xiaohongshu.com/explore/6adbf6450000000024036add" {
		t.Fatal(got)
	}
	if desktopPostURLFor(browser.PublishSpec{VerifyURL: "https://example.com/list"}, "1") != "" {
		t.Fatal("unknown host must give no link")
	}
}

func TestReplyIsTheTemplateFromTheRecord(t *testing.T) {
	desktopPublishRecordUpdate(agentruntime.Scope{RunID: "run-l"}, func(r *desktopPublishRecord) {
		r.State, r.URL, r.Title = browser.PublishLive, "https://x.example/p/9?just_published=2", "论文解读"
	})
	got := desktopReplyWithLink("run-l", "桌面被守卫锁死了，我改用 API 核验……文件在 /tmp/a.png")
	if got != "已发布《论文解读》\n链接：https://x.example/p/9" {
		t.Fatal(got)
	}
	desktopPublishRecordUpdate(agentruntime.Scope{RunID: "run-i"}, func(r *desktopPublishRecord) {
		r.State, r.URL, r.Missing = browser.PublishLiveIncomplete, "https://x.example/p/8", "picture 2 missing"
	})
	if got := desktopReplyWithLink("run-i", "全部完成"); !strings.Contains(got, "不完整：picture 2 missing") || !strings.Contains(got, "https://x.example/p/8") {
		t.Fatal(got)
	}
	desktopPublishRecordUpdate(agentruntime.Scope{RunID: "run-n"}, func(r *desktopPublishRecord) { r.State = browser.PublishNoEditor })
	if got := desktopReplyWithLink("run-n", "没找到编辑器，需要登录。"); got != "没找到编辑器，需要登录。" {
		t.Fatal(got)
	}
	if got := desktopReplyWithLink("run-none", "你好"); got != "你好" {
		t.Fatal(got)
	}
}

func TestReplyGetsTheVerifiedPageURL(t *testing.T) {
	desktopRecordVerifiedPage(agentruntime.Scope{RunID: "run-v"}, "https://zhuanlan.zhihu.com/p/2097265154266227495?just_published=2")
	if got := desktopReplyWithLink("run-v", "核验完成。"); !strings.Contains(got, "https://zhuanlan.zhihu.com/p/2097265154266227495") || strings.Contains(got, "just_published") {
		t.Fatal(got)
	}
}

func TestTemplatedReplyDropsTheAsk(t *testing.T) {
	m := map[string]string{"ask_user_question": "要修吗？", "ask_user_options_json": "[]", "model": "x"}
	desktopDropAskAfterPost("已发布《t》\n链接：https://x.example/p/1", m)
	if len(m) != 1 || m["model"] != "x" {
		t.Fatal(m)
	}
	m = map[string]string{"ask_user_question": "登录了吗？"}
	desktopDropAskAfterPost("需要你先登录。", m)
	if len(m) != 1 {
		t.Fatal(m)
	}
}
