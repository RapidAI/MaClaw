package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestDesktopScriptWritesRichBody(t *testing.T) {
	bad := []string{
		`const el=document.querySelector('.public-DraftEditor-content');el.innerHTML=t;`,
		`document.querySelector('[contenteditable=true]').dispatchEvent(new ClipboardEvent('paste',{clipboardData:dt}))`,
	}
	for _, s := range bad {
		if !desktopScriptWritesRichBody(s) {
			t.Fatalf("want refused: %s", s)
		}
	}
	ok := []string{`click('[contenteditable=true]')`, `document.querySelector('textarea').value='x'`}
	for _, s := range ok {
		if desktopScriptWritesRichBody(s) {
			t.Fatalf("want allowed: %s", s)
		}
	}
}

func TestDesktopPublishGuard(t *testing.T) {
	if !desktopScriptClicksPublish(`clickText("发布")`) || desktopScriptClicksPublish(`clickText("发布设置")`) {
		t.Fatal("script publish detection")
	}
	ctx := context.Background()
	err := desktopRichPreCheck(ctx, nil, "click", map[string]any{"text": "发布"})
	if err == nil || desktopErrorClass("", err) != "publish_needs_confirmation" {
		t.Fatalf("want refusal, got %v", err)
	}
	if desktopRichPreCheck(ctx, nil, "click", map[string]any{"text": "发布设置"}) != nil {
		t.Fatal("settings button must pass")
	}
	ok := context.WithValue(ctx, desktopPublishApprovedKey{}, true)
	if desktopRichPreCheck(ok, nil, "click", map[string]any{"text": "发布"}) != nil {
		t.Fatal("pre-approved publish must pass")
	}
	req := agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{Message: agentservice.Message{Content: "写好后直接发布到知乎，不用确认"}}}
	if !desktopPublishPreapproved(req) {
		t.Fatal("preapproval phrase")
	}
	req = agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{Message: agentservice.Message{Content: "整理后发到知乎上"}}}
	if desktopPublishPreapproved(req) {
		t.Fatal("plain task is not preapproval")
	}
}

func TestDesktopRichErrorClasses(t *testing.T) {
	if desktopErrorClass("", errors.New("page script failed: wait is not defined")) != "script_error" {
		t.Fatal("script_error")
	}
	if desktopErrorClass("", errors.New(desktopRichWriteRefusal)) != "rich_text_dom_write" {
		t.Fatal("rich_text_dom_write")
	}
	if !desktopStepPastes("app_run", map[string]any{"steps": []any{map[string]any{"key": "ctrl+v"}}}) {
		t.Fatal("paste step")
	}
	if !strings.Contains(strings.Join(desktopSeedNotes["zhihu.com"], " "), "铪") {
		t.Fatal("topic rule seeded")
	}
}

func TestDesktopPublishSideGuards(t *testing.T) {
	ctx := context.Background()
	if desktopPublishSideCheck(ctx, "app_run", map[string]any{"steps": []any{map[string]any{"key": "ctrl+Return"}}}) == nil {
		t.Fatal("ctrl+enter must be refused")
	}
	if desktopPublishSideCheck(ctx, "app_run", map[string]any{"steps": []any{map[string]any{"key": "Return"}}}) != nil {
		t.Fatal("plain enter allowed")
	}
	if desktopPublishSideCheck(ctx, "bash", map[string]any{"command": "python3 cdp.py --port 9222 click 发布"}) == nil {
		t.Fatal("bash automation publish refused")
	}
	if desktopPublishSideCheck(ctx, "bash", map[string]any{"command": "ls ~/Desktop"}) != nil {
		t.Fatal("plain bash allowed")
	}
	if !desktopIsPublishLabel("更新") || desktopIsPublishLabel("发布设置") {
		t.Fatal("labels")
	}
	if desktopRichPreCheck(ctx, nil, "script", map[string]any{"script": `document.querySelector('form').requestSubmit()`}) == nil {
		t.Fatal("script submit refused")
	}
	if !desktopBodyLooksEmpty(browser.RichBody{Count: 30, Body: "zHF Daily Papers 每日速读 2026-10-22：记忆与验证", Title: "HF Daily Papers 每日速读 2026-10-22：记忆与验证z"}) {
		t.Fatal("body ~= title is empty")
	}
	if desktopBodyLooksEmpty(browser.RichBody{Count: 2593, Body: "今天的 Hugging Face Daily Papers 共 12 篇，主线很清晰", Title: "HF Daily Papers"}) {
		t.Fatal("real body")
	}
}

func TestDesktopProgressCoach(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u", RunID: "run_coach"}
	st := desktopRunFor(scope)
	if st == nil {
		t.Skip("no run state in this build")
	}
	note := ""
	for i := 0; i < 12; i++ {
		note = desktopProgressNote(scope, "probe", "probed page x (https://a.example/edit)", nil)
	}
	if !strings.Contains(note, "No progress") {
		t.Fatalf("want no-progress note, got %q", note)
	}
	if desktopProgressNote(scope, "navigate", "navigated to https://b.example/", nil); st.progress.idle != 0 {
		t.Fatal("navigate resets idle")
	}
	st.progress.idle = desktopNoProgressStop()
	if desktopNoProgressCheck(scope, "probe") == nil || desktopNoProgressCheck(scope, "request_takeover") != nil {
		t.Fatal("stop check")
	}
	if n := desktopProgressNote(scope, "probe", "url https://zhuanlan.zhihu.com/p/123 发布成功", nil); !strings.Contains(n, "published") {
		t.Fatalf("published note %q", n)
	}
}
