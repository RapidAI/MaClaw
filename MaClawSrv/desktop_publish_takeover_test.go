package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// With publishing approved, a takeover only to click 发布 is refused and the
// bot is steered to action=publish; a login takeover still goes through, and
// without approval the confirmation takeover stays.
func TestDesktopTakeoverForPublishRefusedWhenApproved(t *testing.T) {
	old := desktopRemoteCtl
	desktopRemoteCtl = func(ctx context.Context, tenantID, userID, display string, request map[string]any) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}
	defer func() { desktopRemoteCtl = old }()
	t.Setenv("MACLAW_DESKTOP_PUBLISH_CONFIRM", "1")
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("alice", "bot_1"), InstanceID: "inst-pubtake"}
	approved := context.WithValue(context.Background(), desktopPublishApprovedKey{}, true)
	for _, reason := range []string{"请检查内容并亲自点发布（标题与正文已填好）", "10.18 摘要已填好，请检查内容并亲自点「发布」。", "please click Publish"} {
		_, err := requestDesktopTakeover(approved, scope, reason)
		if err == nil || !strings.Contains(err.Error(), "action=publish") {
			t.Fatalf("%q: %v", reason, err)
		}
	}
	if _, err := requestDesktopTakeover(approved, scope, "请扫码登录小红书后点交还"); err != nil {
		t.Fatalf("login takeover refused: %v", err)
	}
	if _, err := requestDesktopTakeover(context.Background(), scope, "请检查内容并亲自点发布"); err != nil {
		t.Fatalf("unapproved confirmation takeover refused: %v", err)
	}
}

func TestDesktopPublishIntentSiteFirst(t *testing.T) {
	for _, s := range []string{"在小红书创作者中心发布一篇图文笔记", "在知乎专栏发表一篇文章"} {
		if !desktopPublishIntentRe.MatchString(s) {
			t.Fatalf("no intent: %q", s)
		}
	}
	if desktopPublishIntentRe.MatchString("在知乎看看有什么热门") {
		t.Fatal("browse matched")
	}
}

// 10-31 05:55: the bot could not find 发布 (an overlay covered it) and asked
// for a takeover the person read as a login prompt. A missing control is
// refused with overlay guidance; the live 05:55:37 reason is a publish ask.
func TestDesktopTakeoverForMissingElementRefused(t *testing.T) {
	old := desktopRemoteCtl
	desktopRemoteCtl = func(ctx context.Context, tenantID, userID, display string, request map[string]any) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}
	defer func() { desktopRemoteCtl = old }()
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("alice", "bot_1"), InstanceID: "inst-missing"}
	for _, reason := range []string{"页面里我拿不到\"发布\"按钮的元素，请你帮忙点一下", "发布按钮被弹窗挡住了", "cannot find the Publish button"} {
		_, err := requestDesktopTakeover(context.Background(), scope, reason)
		if err == nil || !strings.Contains(err.Error(), "Escape") {
			t.Fatalf("%q: %v", reason, err)
		}
	}
	approved := context.WithValue(context.Background(), desktopPublishApprovedKey{}, true)
	if _, err := requestDesktopTakeover(approved, scope, "标题、正文和两张配图都已填好，发布按钮需要你亲自点：页面右下角「发布」，点完请按交还"); err == nil || !strings.Contains(err.Error(), "action=publish") {
		t.Fatalf("live publish takeover passed: %v", err)
	}
	if _, err := requestDesktopTakeover(context.Background(), scope, "找不到内容，页面显示扫码登录，请登录后交还"); err != nil {
		t.Fatalf("login wall refused: %v", err)
	}
}
