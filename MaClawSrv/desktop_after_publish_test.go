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
	if got := desktopPostURLFor("https://creator.xiaohongshu.com/new/note-manager", "6adbf6450000000024036add"); got != "https://www.xiaohongshu.com/explore/6adbf6450000000024036add" {
		t.Fatal(got)
	}
	if desktopPostURLFor("https://example.com/list", "1") != "" {
		t.Fatal("unknown host must give no link")
	}
}

func TestReplyGetsThePublishedLink(t *testing.T) {
	desktopPublishRecordUpdate(agentruntime.Scope{RunID: "run-l"}, func(r *desktopPublishRecord) { r.State, r.URL = browser.PublishLive, "https://x.example/p/9" })
	if got := desktopReplyWithLink("run-l", "论文数据已经拿到。"); !strings.Contains(got, "https://x.example/p/9") {
		t.Fatal(got)
	}
	desktopPublishRecordUpdate(agentruntime.Scope{RunID: "run-m"}, func(r *desktopPublishRecord) { r.State, r.URL = browser.PublishLive, "https://x.example/p/8" })
	if got := desktopReplyWithLink("run-m", "见 https://x.example/p/8"); got != "见 https://x.example/p/8" {
		t.Fatal(got)
	}
}

func TestReplyGetsTheVerifiedPageURL(t *testing.T) {
	desktopRecordVerifiedPage(agentruntime.Scope{RunID: "run-v"}, "https://zhuanlan.zhihu.com/p/2097265154266227495?just_published=2")
	if got := desktopReplyWithLink("run-v", "核验完成。"); !strings.Contains(got, "https://zhuanlan.zhihu.com/p/2097265154266227495") || strings.Contains(got, "just_published") {
		t.Fatal(got)
	}
}
