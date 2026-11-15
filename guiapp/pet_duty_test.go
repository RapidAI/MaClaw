package guiapp

import (
	"testing"
	"time"

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
		Event: "task_done", Cue: "celebrate",
		since: now.Add(-400 * time.Millisecond),
	}
	live := dutyDecisionStillLive(done, now)
	if live.Pose != string(petpack.StateDone) || live.TTLMS != 1200 || live.Event != "" || live.Cue != "" {
		t.Fatalf("remaining = %+v", live)
	}
	expired := dutyDecisionStillLive(done, now.Add(2*time.Second))
	if expired.Pose != string(petpack.StateIdle) || expired.TTLMS != 0 || expired.Event != "" || expired.Cue != "" {
		t.Fatalf("expired = %+v", expired)
	}
	thinking := dutyDecisionStillLive(petDutyDecision{Pose: string(petpack.StateThinking), Event: "task_started", Cue: "focus"}, now)
	if thinking.Pose != string(petpack.StateThinking) || thinking.TTLMS != 0 || thinking.Event != "" || thinking.Cue != "" {
		t.Fatalf("open work = %+v", thinking)
	}
	unstamped := dutyDecisionStillLive(petDutyDecision{Pose: string(petpack.StateAlert), TTLMS: 1600, Cue: "concern"}, now)
	if unstamped.Pose != string(petpack.StateIdle) || unstamped.Cue != "" {
		t.Fatalf("unstamped terminal = %+v", unstamped)
	}
}
