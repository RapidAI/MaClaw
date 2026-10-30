package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func contTurn(user, content, phase string) agentruntime.TurnRequest {
	meta := map[string]string{"bot_id": "bot_x"}
	if phase != "" {
		meta["bot_phase"] = phase
	}
	var req agentservice.ExecuteRequest
	req.Message.Content = content
	req.Message.Metadata = meta
	return agentruntime.TurnRequest{Scope: agentruntime.Scope{TenantID: "t", UserID: user}, Input: req}
}

func TestDesktopHandBackContinuationExecutes(t *testing.T) {
	hb := "我已经在桌面上完成了需要我做的操作（登录/验证），桌面交还给你，请继续刚才的任务。"
	if got := desktopTurnPhase(contTurn("u_a", hb, "")); got != "execute" {
		t.Fatalf("hand-back phase %q", got)
	}
	desktopTurnPhase(contTurn("u_b", "做摘要发到知乎", "execute"))
	if got := desktopTurnPhase(contTurn("u_b", "继续", "")); got != "execute" {
		t.Fatalf("inherited phase %q", got)
	}
	desktopTurnPhase(contTurn("u_c", "看看", "plan"))
	if got := desktopTurnPhase(contTurn("u_c", "嗯", "")); got != "plan" {
		t.Fatalf("plan must not become execute: %q", got)
	}
}

func TestDesktopPublishIntentApprovesAndSticks(t *testing.T) {
	if !desktopTurnPublishApproved(contTurn("u_d", "做10.16的huggingface dailypapers的摘要，发到知乎上", "execute")) {
		t.Fatal("explicit 发到知乎 should approve")
	}
	if !desktopTurnPublishApproved(contTurn("u_d", "请继续刚才的任务", "")) {
		t.Fatal("continuation should inherit approval")
	}
	if desktopTurnPublishApproved(contTurn("u_e", "帮我写一篇知乎文章草稿", "execute")) {
		t.Fatal("draft request is not approval")
	}
}

func TestDesktopNoPhaseRunsButPublishStillAsks(t *testing.T) {
	turn := contTurn("u_np", "打开 example.com，点 Learn more，告诉我到了哪", "")
	if got := desktopTurnPhase(turn); got != "execute" {
		t.Fatalf("phase-less ordinary task must run, got %q", got)
	}
	if desktopTurnPublishApproved(turn) {
		t.Fatal("running ordinary steps must not approve publishing")
	}
	if got := desktopTurnPhase(contTurn("u_np", "接着来", "")); got != "execute" {
		t.Fatalf("follow-up %q", got)
	}
}
