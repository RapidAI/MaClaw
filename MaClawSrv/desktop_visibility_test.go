package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestMessageKindKeepsProcessOutOfChat(t *testing.T) {
	if desktopMessageKind("", false, false) != desktopMsgProcess {
		t.Fatal("a mid-run note is process")
	}
	if desktopMessageKind(desktopMsgOutcome, false, false) != desktopMsgProcess {
		t.Fatal("a claimed outcome during the run stayed outcome")
	}
	if desktopMessageKind(desktopMsgOutcome, true, false) != desktopMsgOutcome {
		t.Fatal("the finish is the outcome")
	}
	if desktopMessageKind(desktopMsgNeedsUser, false, true) != desktopMsgNeedsUser {
		t.Fatal("a structured ask is needs_user")
	}
	if desktopMessageKind(desktopMsgNeedsUser, false, false) != desktopMsgProcess {
		t.Fatal("needs_user without an ask was posted")
	}
	if desktopChatKind(desktopMsgProcess) || !desktopChatKind(desktopMsgOutcome) || !desktopChatKind(desktopMsgNeedsUser) {
		t.Fatal("chat kinds")
	}
}

func TestSayDuringARunIsProcess(t *testing.T) {
	run := "run_vis"
	t.Cleanup(func() {
		desktopProcessMu.Lock()
		delete(desktopProcessLines, run)
		desktopProcessMu.Unlock()
	})
	out, err := desktopSay(t.Context(), agentruntime.TurnRequest{Scope: agentruntime.Scope{RunID: run}}, map[string]any{"message": "正在打开编辑器，第 3 步"})
	if err != nil || !strings.Contains(out, "NOT SENT process") {
		t.Fatalf("say %q %v", out, err)
	}
	lines := desktopProcessOf(run)
	if len(lines) != 1 || !strings.Contains(lines[0], "正在打开编辑器") {
		t.Fatalf("record %v", lines)
	}
	if !strings.Contains(desktopRunStatusLine(run), "正在打开编辑器") {
		t.Fatalf("status %q", desktopRunStatusLine(run))
	}
	asked, err := desktopSay(t.Context(), agentruntime.TurnRequest{Scope: agentruntime.Scope{RunID: run, InstanceID: "missing"}}, map[string]any{"kind": desktopMsgNeedsUser, "message": "需要你登录"})
	if err == nil || strings.Contains(asked, "NOT SENT process") {
		t.Fatalf("needs_user was treated as process: %q %v", asked, err)
	}
}

func TestImprovementProceedsOnlyWhenReady(t *testing.T) {
	if !desktopImprovementReady("resource", true, false) {
		t.Fatal("a resource blocker the bot can fix should continue")
	}
	if desktopImprovementReady("resource", true, true) || desktopImprovementReady("login", false, false) || desktopImprovementReady("permission", false, false) {
		t.Fatal("a tried step or a person-sized blocker continued")
	}
}
