package main

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDoneMessageEndsTakeoverOncePerRun(t *testing.T) {
	prev := desktopRemoteCtl
	defer func() { desktopRemoteCtl = prev }()
	var ops []string
	desktopRemoteCtl = func(_ context.Context, _, _, _ string, req map[string]any) (map[string]any, error) {
		ops = append(ops, req["op"].(string))
		return map[string]any{"ok": true}, nil
	}
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("u_1", "bot_a"), RunID: "run_hb1"}
	turn := func(text string) agentruntime.TurnRequest {
		return agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{Message: agentservice.Message{Content: text}}}
	}
	// The chat's 登录完成，继续 sends this text (guiapp BOT_RETURN_TASK).
	done := turn("登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。")
	if !desktopHandBackOnDoneTurn(context.Background(), done, scope) {
		t.Fatal("done message did not hand the display back")
	}
	if desktopHandBackOnDoneTurn(context.Background(), done, scope) {
		t.Fatal("second action of the same run ended the takeover again")
	}
	if len(ops) != 1 || ops[0] != desktop.CtlTakeoverEnd {
		t.Fatalf("ops=%v", ops)
	}
	// An ordinary task does not end a takeover.
	scope.RunID = "run_hb2"
	if desktopHandBackOnDoneTurn(context.Background(), turn("去小红书发一篇帖子"), scope) {
		t.Fatal("ordinary message ended the takeover")
	}
	// Hub's hand-back text counts too.
	if !desktopHandBackOnDoneTurn(context.Background(), turn("我已经在桌面上完成了需要我做的操作（登录/验证），桌面交还给你，请继续刚才的任务。"), scope) {
		t.Fatal("hub hand-back text did not hand back")
	}
	// The owner's own (shared) desktop has no takeover to end.
	shared := agentruntime.Scope{TenantID: "t", UserID: "u_1", RunID: "run_hb3"}
	if desktopHandBackOnDoneTurn(context.Background(), done, shared) {
		t.Fatal("shared desktop handed back")
	}
}
