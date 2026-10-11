package guiapp

import (
	"strings"
	"testing"
)

// TestEvaluateDesktopTaskSupplementKillSwitch pins the §3.1f kill switch: with
// MACLAW_SUPPLEMENT=off the classifier short-circuits to "new task" without
// asking the LLM, so the caller sends the words through the ordinary task path
// and nothing is ever offered to a running turn.
func TestEvaluateDesktopTaskSupplementKillSwitch(t *testing.T) {
	t.Setenv("MACLAW_SUPPLEMENT", "off")
	a := &App{}
	eval, err := a.EvaluateDesktopTaskSupplement("正在打开订单页面", "只要前5条", "zh")
	if err != nil {
		t.Fatal(err)
	}
	if eval.IsSupplement {
		t.Fatalf("the switch did not short-circuit the classifier: %+v", eval)
	}
	if eval.Reason != "supplement disabled" {
		t.Fatalf("reason = %q, want the switch's own reason", eval.Reason)
	}
}

// TestAppendDesktopBotSupplementKillSwitch is the defense-in-depth half: even
// a caller that bypassed the classifier gets a receipt that says the channel
// is closed, and no run id travels out of this path.
func TestAppendDesktopBotSupplementKillSwitch(t *testing.T) {
	resetActiveDesktopRuns(t)
	t.Setenv("MACLAW_SUPPLEMENT", "off")
	a := &App{}
	run := a.registerActiveDesktopRun("bot-1", "req-1", "session-1", "任务", "execute")
	a.setActiveDesktopRunID("bot-1", "req-1", "run-42")

	receipt, err := a.AppendDesktopBotSupplement("bot-1", run.RunID, "只要前5条")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Stage != desktopBotSupplementNoActiveRun {
		t.Fatalf("stage = %q, want the closed-channel stage", receipt.Stage)
	}
	if !strings.Contains(receipt.Message, "补充通道已关闭") {
		t.Fatalf("message = %q, want the closed-channel note", receipt.Message)
	}
	if receipt.Accepted || receipt.SupplementID != "" {
		t.Fatalf("a closed channel must not accept anything: %+v", receipt)
	}
}

// TestAppendDesktopBotSupplementStillRoutesWithoutTheSwitch guards against the
// switch accidentally latching on: without the variable the same call reaches
// the ordinary routing logic, whose no-active-run note is a different sentence
// from the closed-channel one. No hub and no config are touched on this path.
func TestAppendDesktopBotSupplementStillRoutesWithoutTheSwitch(t *testing.T) {
	resetActiveDesktopRuns(t)
	t.Setenv("MACLAW_SUPPLEMENT", "")
	a := &App{}

	receipt, err := a.AppendDesktopBotSupplement("bot-1", "", "只要前5条")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Stage != desktopBotSupplementNoActiveRun {
		t.Fatalf("stage = %q, want the plain no-active-run stage", receipt.Stage)
	}
	if strings.Contains(receipt.Message, "补充通道已关闭") {
		t.Fatalf("the switch leaked into an unset environment: %+v", receipt)
	}
}
