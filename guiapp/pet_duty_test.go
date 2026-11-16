package guiapp

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

func TestPetDutyReducerFollowsTheConfirmedTable(t *testing.T) {
	var open map[string]string
	var decision petDutyDecision

	open, decision = reducePetDuty(open, "turn:desktop-user", "running", false)
	if decision.Pose != string(petpack.StateThinking) || decision.Event != "task_started" || decision.Cue != "focus" {
		t.Fatalf("start = %+v", decision)
	}
	open, decision = reducePetDuty(open, "turn:desktop-user", "running", false)
	if !decision.Unchanged {
		t.Fatal("continued work reminded again")
	}

	open, decision = reducePetDuty(open, "turn:desktop-user", "needs_input", false)
	if decision.Pose != string(petpack.StateListening) || decision.Cue != "notice" || decision.Event != "task_needs_you" {
		t.Fatalf("needs you = %+v", decision)
	}
	open, decision = reducePetDuty(open, "turn:desktop-user", "running", false)
	if decision.Pose != string(petpack.StateThinking) || decision.Cue != "" || decision.Event != "" {
		t.Fatalf("answered = %+v", decision)
	}

	open, decision = reducePetDuty(open, "task:other", "running", false)
	open, decision = reducePetDuty(open, "turn:desktop-user", "done", false)
	if decision.Pose != string(petpack.StateThinking) || decision.Cue != "celebrate" || decision.Event != "task_done" {
		t.Fatalf("one done while another runs = %+v", decision)
	}
	open, decision = reducePetDuty(open, "task:other", "failed", false)
	if decision.Pose != string(petpack.StateAlert) || decision.TTLMS != petDutyTerminalTTLMS || decision.Cue != "concern" {
		t.Fatalf("last failure = %+v", decision)
	}

	open, decision = reducePetDuty(nil, "task:ask", "needs_input", false)
	if decision.Event != "task_needs_you" || decision.Cue != "notice" {
		t.Fatalf("direct needs_input = %+v", decision)
	}
	open, decision = reducePetDuty(open, "task:ask", "canceled", false)
	if decision.Pose != string(petpack.StateIdle) || decision.Cue != "" {
		t.Fatalf("cancel = %+v", decision)
	}
	_, decision = reducePetDuty(open, "task:ask", "done", false)
	if !decision.Unchanged {
		t.Fatal("duplicate finish reminded")
	}
}

func TestPetDutyHydrateSetsPoseWithoutAReminder(t *testing.T) {
	open, decision := reducePetDuty(nil, "task:recovered", "running", true)
	if decision.Pose != string(petpack.StateThinking) || decision.Cue != "" || decision.Event != "" {
		t.Fatalf("hydrate = %+v", decision)
	}
	if open["task:recovered"] != "running" {
		t.Fatalf("open = %#v", open)
	}
}

func TestPetDutyLastSuccessThenNewWork(t *testing.T) {
	open, decision := reducePetDuty(nil, "turn:a", "done", false)
	if !decision.Unchanged {
		t.Fatal("done without an open item reminded")
	}
	open, _ = reducePetDuty(nil, "turn:a", "running", false)
	open, decision = reducePetDuty(open, "turn:a", "done", false)
	if decision.Pose != string(petpack.StateDone) || decision.Cue != "celebrate" || decision.TTLMS != petDutyTerminalTTLMS {
		t.Fatalf("last success = %+v", decision)
	}
	_, decision = reducePetDuty(open, "turn:b", "running", false)
	if decision.Pose != string(petpack.StateThinking) || decision.Event != "task_started" {
		t.Fatalf("new work during the done pose = %+v", decision)
	}
}

func TestDutyDecisionStillLiveDropsTheReminder(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	done := petDutyDecision{
		Pose: string(petpack.StateDone), TTLMS: 1600,
		Event: "task_done", Cue: "celebrate", Line: "做好了",
		since: now.Add(-400 * time.Millisecond),
	}
	live := dutyDecisionStillLive(done, now)
	if live.Pose != string(petpack.StateDone) || live.TTLMS != 1200 || live.Event != "" || live.Cue != "" || live.Line != "做好了" {
		t.Fatalf("remaining = %+v", live)
	}
	expired := dutyDecisionStillLive(done, now.Add(2*time.Second))
	if expired.Pose != string(petpack.StateIdle) || expired.TTLMS != 0 || expired.Event != "" || expired.Cue != "" || expired.Line != "" {
		t.Fatalf("expired = %+v", expired)
	}
	thinking := dutyDecisionStillLive(petDutyDecision{Pose: string(petpack.StateThinking), Event: "task_started", Cue: "focus", Line: "正在做"}, now)
	if thinking.Pose != string(petpack.StateThinking) || thinking.TTLMS != 0 || thinking.Event != "" || thinking.Cue != "" || thinking.Line != "正在做" {
		t.Fatalf("open work = %+v", thinking)
	}
	unstamped := dutyDecisionStillLive(petDutyDecision{Pose: string(petpack.StateAlert), TTLMS: 1600, Cue: "concern"}, now)
	if unstamped.Pose != string(petpack.StateIdle) || unstamped.Cue != "" {
		t.Fatalf("unstamped terminal = %+v", unstamped)
	}
}

func TestPetDutyQueuedStaysCalmUntilWorkStarts(t *testing.T) {
	open, decision := reducePetDuty(nil, "task:deck", "queued", false)
	if decision.Pose != string(petpack.StateIdle) || decision.Event != "task_started" || decision.Cue != "focus" {
		t.Fatalf("accepted = %+v", decision)
	}
	open, decision = reducePetDuty(open, "task:deck", "running", false)
	if decision.Pose != string(petpack.StateThinking) || decision.Event != "" || decision.Cue != "" {
		t.Fatalf("begun = %+v", decision)
	}
	open, decision = reducePetDuty(open, "task:deck", "done", false)
	open, decision = reducePetDuty(open, "task:next", "queued", false)
	if decision.Pose != string(petpack.StateIdle) || decision.Event != "task_started" {
		t.Fatalf("next queued = %+v", decision)
	}
}

func TestPetPresenceLineUsesTheRealAction(t *testing.T) {
	fold, decision := foldPetDuty(petDutyFold{}, "task:deck", "queued", "周五汇报", false, false)
	if decision.Pose != string(petpack.StateIdle) || decision.Line != "还没开始：周五汇报" || decision.Event != "task_started" {
		t.Fatalf("queued = %+v", decision)
	}
	fold, decision = foldPetDuty(fold, "task:deck", "running", "周五汇报", false, false)
	if decision.Pose != string(petpack.StateThinking) || decision.Line != "正在做：周五汇报" || decision.Event != "" {
		t.Fatalf("title = %+v", decision)
	}
	fold, decision = foldPetDuty(fold, "task:deck", "running", "在改定价页", true, false)
	if !decision.Unchanged || !decision.LineChanged || decision.Event != "" || decision.Line != "在改定价页" {
		t.Fatalf("step = %+v", decision)
	}
	if strings.Contains(decision.Line, "math.ts") || strings.Contains(decision.Line, "Edited") {
		t.Fatalf("invented step = %q", decision.Line)
	}

	fold, decision = foldPetDuty(fold, "task:other", "running", "另一件", false, false)
	if decision.Line != "在改定价页" {
		t.Fatalf("primary action lost = %+v", decision)
	}
	fold, decision = foldPetDuty(fold, "task:ask", "needs_input", "确认发送", false, false)
	if decision.Pose != string(petpack.StateListening) || decision.Line != "要你看一下：确认发送" || decision.Event != "task_needs_you" {
		t.Fatalf("needs you = %+v", decision)
	}
	if strings.Contains(decision.Line, "在改定价页") || strings.Contains(decision.Line, "另一件") {
		t.Fatalf("line listed other work = %q", decision.Line)
	}

	_, second := foldPetDuty(petDutyFold{open: map[string]string{"turn:a": "running"}}, "turn:b", "running", "", false, false)
	if second.Line != "正在做 2 件事" {
		t.Fatalf("count = %+v", second)
	}
	_, decision = foldPetDuty(petDutyFold{open: map[string]string{"turn:a": "running"}, line: "正在做"}, "turn:a", "done", "", false, false)
	if decision.Pose != string(petpack.StateDone) || decision.Line != "做好了" {
		t.Fatalf("settled = %+v", decision)
	}
	_, decision = foldPetDuty(petDutyFold{open: map[string]string{"turn:a": "running"}}, "turn:a", "failed", "", false, false)
	if decision.Pose != string(petpack.StateAlert) || decision.Line != "这件没做成" {
		t.Fatalf("blocked = %+v", decision)
	}

	long := strings.Repeat("字", 40)
	if got := clipPresence(long); utf8.RuneCountInString(got) != petPresenceRunes || !strings.HasSuffix(got, "…") {
		t.Fatalf("clip = %q (%d)", got, utf8.RuneCountInString(got))
	}
	if got := sanitizePresenceFact("第一行\n第二行"); got != "第一行 第二行" {
		t.Fatalf("one line = %q", got)
	}
}

func TestPetPresenceTipStaysByThePet(t *testing.T) {
	x, y, ok := petPresenceTipPoint(100, 80, 340, 320, 120, 24, 0, 0, 1920, 1080)
	if !ok || y >= 80 || x < 0 {
		t.Fatalf("above = %d,%d ok=%v", x, y, ok)
	}
	_, y, ok = petPresenceTipPoint(100, 4, 340, 244, 120, 24, 0, 0, 1920, 1080)
	if !ok || y < 244 {
		t.Fatalf("below = %d ok=%v", y, ok)
	}
	if _, _, ok = petPresenceTipPoint(40000, 10, 40240, 250, 80, 20, 0, 0, 50000, 2000); ok {
		t.Fatal("coordinate past the tooltip range was accepted")
	}
	// A short work area cannot hold the note above or below. Clamping it
	// into that area used to drop the note onto her body, where it steals
	// the click. The note stays fully off her.
	x, y, ok = petPresenceTipPoint(100, 10, 200, 90, 80, 30, 0, 0, 400, 100)
	if !ok || petPresenceCoversPet(x, y, 80, 30, 100, 10, 100, 80) {
		t.Fatalf("note covered the pet at %d,%d ok=%v", x, y, ok)
	}
}
