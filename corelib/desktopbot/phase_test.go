package desktopbot

import (
	"errors"
	"strings"
	"testing"
)

func TestApplyTurnPhaseUsesIntakeAndIgnoresKeywords(t *testing.T) {
	for _, text := range []string{
		"用支付宝查一下账单",
		"删除重复段落后发布",
		"format as a table",
		"please pay attention",
		"remove the watermark",
		"使用最新的dailypaper, 挑一篇最热门的，做图文综述，发到知乎、csdn以及小红书",
	} {
		if got := ApplyTurnPhase(TurnPhaseInput{Intake: "execute"}); got != "execute" {
			t.Fatalf("%s with intake execute -> %s", text, got)
		}
	}
	if got := ApplyTurnPhase(TurnPhaseInput{Intake: "plan"}); got != "plan" {
		t.Fatalf("intake plan -> %s", got)
	}
	if got := ApplyTurnPhase(TurnPhaseInput{}); got != "execute" {
		t.Fatalf("missing intake -> %s", got)
	}
	if got := ApplyTurnPhase(TurnPhaseInput{Button: true, Intake: "plan"}); got != "execute" {
		t.Fatalf("arrange button -> %s", got)
	}
}

func TestPlanAskIsTiedToItsRun(t *testing.T) {
	ResetPhaseMemory()
	t.Cleanup(ResetPhaseMemory)
	NotePlanAsk("t", "u", "bot", "ask-1", "run-1")
	if got := ApplyTurnPhase(TurnPhaseInput{
		Intake: "plan", Tenant: "t", User: "u", Bot: "bot",
	}); got != "plan" {
		t.Fatalf("a new order after an ask ran as %s", got)
	}
	if got := ApplyTurnPhase(TurnPhaseInput{
		Tenant: "t", User: "u", Bot: "bot", AskRun: "run-1", Intake: "plan",
	}); got != "execute" {
		t.Fatalf("the answer to run-1 -> %s", got)
	}
	if got := ApplyTurnPhase(TurnPhaseInput{
		Tenant: "t", User: "u", Bot: "bot", Intake: "plan",
	}); got != "plan" {
		t.Fatalf("after the answer was consumed, a new order -> %s", got)
	}
	if ConsumePlanAsk("t", "u", "bot", "ask-1", "") {
		t.Fatal("the ask was already consumed by its answer")
	}
}

func TestWakeFaultMatchesCodesNotBroadText(t *testing.T) {
	if !IsDesktopAsleep(AsleepError{}) {
		t.Fatal("coded asleep")
	}
	if !IsDesktopAsleep(errors.New("Error response from daemon: container maclaw-desktop-ab is not running")) {
		t.Fatal("old desktopd container state")
	}
	if IsDesktopAsleep(errors.New("Cannot connect to the Docker daemon. Is the docker daemon running")) {
		t.Fatal("daemon down is not a stopped container")
	}
	if IsDesktopAsleep(errors.New("the server is not running")) {
		t.Fatal("unrelated not-running")
	}
	if !IsWaking(WakingError{}) || IsDesktopAsleep(WakingError{}) {
		t.Fatal("waking is its own error")
	}
	fault := AsWakeFault(WakingError{})
	if !strings.Contains(fault.Error(), "系统故障") || strings.Contains(fault.Error(), "要不要开机") {
		t.Fatalf("fault = %s", fault)
	}
	if AsWakeFault(errors.New("dns timeout")).Error() != "dns timeout" {
		t.Fatal("unrelated errors pass through")
	}
}
