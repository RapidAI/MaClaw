package desktopbot

import (
	"errors"
	"strings"
	"testing"
)

func TestNeedsPlanLeavesOrdinaryPublishAlone(t *testing.T) {
	ordinary := "使用最新的dailypaper, 挑一篇最热门的，做图文综述，发到知乎、csdn以及小红书"
	if NeedsPlan(ordinary) {
		t.Fatal("fetch, write, and publish are not a plan")
	}
	for _, order := range []string{"把日志删除掉", "安装一个pdf阅读器", "rm -rf /tmp/x", "pay the invoice"} {
		if !NeedsPlan(order) {
			t.Fatalf("%q should stay in plan", order)
		}
	}
}

func TestDecidePhaseTreatsPlanHintAsAHint(t *testing.T) {
	task := "使用最新的dailypaper, 挑一篇最热门的，做图文综述，发到知乎、csdn以及小红书"
	if got := DecidePhase("plan", task, false); got != "execute" {
		t.Fatalf("ordinary plan hint = %q", got)
	}
	if got := DecidePhase("plan", "把日志删除掉", false); got != "plan" {
		t.Fatalf("destructive plan hint = %q", got)
	}
	if got := DecidePhase("plan", "", false); got != "plan" {
		t.Fatalf("empty plan hint = %q", got)
	}
	if got := DecidePhase("execute", "把日志删除掉", false); got != "execute" {
		t.Fatalf("explicit execute = %q", got)
	}
	if got := DecidePhase("plan", "把日志删除掉", true); got != "execute" {
		t.Fatalf("ask reply = %q", got)
	}
	if got := DecidePhase("", task, false); got != "" {
		t.Fatalf("empty hint is left to continuation, got %q", got)
	}
}

func TestResolvePhasePromotesAnAskAnswerAndRunsOrdinaryWork(t *testing.T) {
	ResetPhaseMemory()
	t.Cleanup(ResetPhaseMemory)
	task := "使用最新的dailypaper, 挑一篇最热门的，做图文综述，发到知乎、csdn以及小红书"
	if got := ResolvePhase("t", "paper", "bot", "plan", task); got != "execute" {
		t.Fatalf("dailypaper phase %q", got)
	}
	if got := ResolvePhase("t", "paper", "bot", "plan", task); got != "execute" {
		t.Fatalf("same turn re-read %q", got)
	}
	if got := ResolvePhase("t", "del", "bot", "plan", "把日志删除掉"); got != "plan" {
		t.Fatalf("delete phase %q", got)
	}
	NotePlanAsk("t", "del", "bot")
	if got := ResolvePhase("t", "del", "bot", "plan", "删除"); got != "execute" {
		t.Fatalf("ask answer stayed %q", got)
	}
	if got := ResolvePhase("t", "look", "bot", "plan", "看看"); got != "execute" {
		t.Fatalf("look hint %q", got)
	}
	if got := ResolvePhase("t", "look", "bot", "", "嗯"); got != "execute" {
		t.Fatalf("follow-up of an ordinary turn %q", got)
	}
	if got := ResolvePhase("t", "hold", "bot", "plan", "删除日志"); got != "plan" {
		t.Fatalf("held delete %q", got)
	}
	if got := ResolvePhase("t", "hold", "bot", "", "嗯"); got != "plan" {
		t.Fatalf("short reply inside a destructive plan %q", got)
	}
	hb := "我已经在桌面上完成了需要我做的操作（登录/验证），桌面交还给你，请继续刚才的任务。"
	if got := ResolvePhase("t", "hold", "bot", "", hb); got != "execute" {
		t.Fatalf("hand-back %q", got)
	}
}

func TestWakeFaultIsNotAQuestion(t *testing.T) {
	if strings.Contains(PlanPhaseNextStep, "等这个人确认") || strings.Contains(PlanPhaseNextStep, "ask_user") {
		t.Fatalf("plan refusal asks the person: %s", PlanPhaseNextStep)
	}
	err := AsWakeFault(errors.New("container maclaw-desktop is not running: 容器未运行"))
	text := err.Error()
	for _, banned := range []string{"等这个人", "要我现在启动", "要不要开机", "ask_user"} {
		if strings.Contains(text, banned) {
			t.Fatalf("fault contains %q: %s", banned, text)
		}
	}
	if !strings.Contains(text, "系统故障") || !strings.Contains(text, "电脑没能唤醒") {
		t.Fatalf("fault = %s", text)
	}
	if AsWakeFault(errors.New("dns timeout")).Error() != "dns timeout" {
		t.Fatal("unrelated errors must pass through")
	}
	if AsWakeFault(nil) != nil {
		t.Fatal("nil")
	}
}
