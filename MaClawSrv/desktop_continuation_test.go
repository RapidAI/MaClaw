package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
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

func contIntake(user, content, intake string) agentruntime.TurnRequest {
	turn := contTurn(user, content, "plan")
	_, meta := desktopTurnMessage(turn)
	meta["bot_intake_phase"] = intake
	return turn
}

func TestDesktopHandBackContinuationExecutes(t *testing.T) {
	desktopbot.ResetPhaseMemory()
	t.Cleanup(desktopbot.ResetPhaseMemory)
	hb := "我已经在桌面上完成了需要我做的操作（登录/验证），桌面交还给你，请继续刚才的任务。"
	if got := desktopTurnPhase(contTurn("u_a", hb, "")); got != "execute" {
		t.Fatalf("hand-back phase %q", got)
	}
	// A client plan label does not stick, and a typed follow-up is not held in plan.
	if got := desktopTurnPhase(contTurn("u_c", "看看", "plan")); got != "execute" {
		t.Fatalf("client plan hint %q", got)
	}
	if got := desktopTurnPhase(contTurn("u_c", "确认，开始吧", "plan")); got != "execute" {
		t.Fatalf("typed follow-up %q", got)
	}
	if got := desktopTurnPhase(contIntake("u_hold", "删掉我的全部文件", "plan")); got != "plan" {
		t.Fatalf("intake plan %q", got)
	}
	if got := desktopTurnPhase(contTurn("u_hold", "确认，开始吧", "")); got != "execute" {
		t.Fatalf("follow-up must not inherit plan: %q", got)
	}
}

func TestAskUserAnswerIsTiedToTheRun(t *testing.T) {
	desktopbot.ResetPhaseMemory()
	t.Cleanup(desktopbot.ResetPhaseMemory)
	desktopbot.NotePlanAsk("t", "u_ask", "bot_x", "ask-9", "run-9")
	fresh := contIntake("u_ask", "删除 /data 下所有文件", "plan")
	if got := desktopTurnPhase(fresh); got != "plan" {
		t.Fatalf("unrelated order %q", got)
	}
	answer := contTurn("u_ask", "确认", "")
	_, meta := desktopTurnMessage(answer)
	meta["bot_ask_run"] = "run-9"
	if got := desktopTurnPhase(answer); got != "execute" {
		t.Fatalf("answer %q", got)
	}
}

func TestOrdinaryPlanHintDoesNotAskToWake(t *testing.T) {
	desktopbot.ResetPhaseMemory()
	t.Cleanup(desktopbot.ResetPhaseMemory)
	task := "使用最新的dailypaper, 挑一篇最热门的，做图文综述，发到知乎、csdn以及小红书"
	turn := contTurn("u_paper", task, "plan")
	if got := desktopTurnPhase(turn); got != "execute" {
		t.Fatalf("phase %q", got)
	}
	content, meta := desktopTurnMessage(turn)
	if content == "" || meta["bot_phase"] != "execute" || meta["bot_phase_decided"] != "1" {
		t.Fatalf("stamp content=%q meta=%v", content, meta)
	}
	if strings.Contains(desktopbot.PlanPhaseNextStep, "等这个人确认") {
		t.Fatal(desktopbot.PlanPhaseNextStep)
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
