package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
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
