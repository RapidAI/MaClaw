package agentservice

import (
	"testing"
)

// BR-20261120-8A708E128485 — a run that stops to wait for the person is 待决策,
// i.e. paused. Before this change every such run was persisted as
// RunStatusSucceeded, so any consumer that reads run status alone counted it as
// finished work: the person saw a 完成 task that was really waiting on them.

func TestRunWaitingForUserIsPausedNotSucceeded(t *testing.T) {
	// The status a waiting run gets must be its own value, and it must not be
	// possible to mistake it for the success value.
	if RunStatusPaused == RunStatusSucceeded {
		t.Fatal("RunStatusPaused must be a distinct run status")
	}
	if RunStatusPaused == "" {
		t.Fatal("RunStatusPaused must have a stable wire value")
	}
	if RunStatusPaused != "paused" {
		t.Fatalf("RunStatusPaused wire value %q, want %q", RunStatusPaused, "paused")
	}
}

// A paused run stays in the transcript as answered history: the person was
// shown a question and the turn is not an abandoned request. The model must not
// be told to answer it again alongside the next, unrelated turn.
func TestPausedRunCountsAsAnsweredHistory(t *testing.T) {
	h := []Message{
		{ID: "u1", Role: MessageRoleUser, Content: "把结果发到 CSDN"},
		{ID: "a1", Role: MessageRoleAssistant, Content: "要发到哪个专栏？"},
		{ID: "u2", Role: MessageRoleUser, Content: "推荐一首歌"},
	}
	runs := []Run{
		// The run asked the person something and stopped; no follow-up yet.
		{ID: "r1", UserMessageID: "u1", Status: RunStatusPaused},
		{ID: "r2", UserMessageID: "u2", Status: RunStatusRunning},
	}
	got := dropAbandonedUserTurns(h, runs, "u2")
	if len(got) != 3 {
		t.Fatalf("a paused turn was dropped from history: %+v", got)
	}
}

// A failed or cancelled run must still count as abandoned, so adding the paused
// case did not flatten the distinction between "waiting on the person" and
// "the run died".
func TestCancelledRunStillDroppedWhilePausedIsKept(t *testing.T) {
	h := []Message{
		{ID: "u1", Role: MessageRoleUser, Content: "查一下天气"},
		{ID: "u2", Role: MessageRoleUser, Content: "把结果发到 CSDN"},
		{ID: "a2", Role: MessageRoleAssistant, Content: "选哪个专栏？"},
		{ID: "u3", Role: MessageRoleUser, Content: "推荐一首歌"},
	}
	runs := []Run{
		{ID: "r1", UserMessageID: "u1", Status: RunStatusCancelled},
		{ID: "r2", UserMessageID: "u2", Status: RunStatusPaused},
		{ID: "r3", UserMessageID: "u3", Status: RunStatusRunning},
	}
	got := dropAbandonedUserTurns(h, runs, "u3")
	ids := make([]string, 0, len(got))
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	for _, id := range ids {
		if id == "u1" {
			t.Fatalf("cancelled turn still reaches the model: %v", ids)
		}
	}
	// The paused turn, its question and the assistant answer stay.
	if len(got) != 3 || got[0].ID != "u2" || got[1].ID != "a2" || got[2].ID != "u3" {
		t.Fatalf("paused turn must stay in history: %v", ids)
	}
}

// The desktop treats a paused run as terminal for events and receipts, but the
// long-job follower only delivers files for a run that actually succeeded.
func TestPausedIsTerminalButNotSuccess(t *testing.T) {
	if RunStatusPaused == RunStatusSucceeded {
		t.Fatal("paused must not equal succeeded")
	}
	if !TaskOutcomeNeedsUser(TaskOutcomeAwaitingUser) {
		t.Fatal("awaiting_user must count as needing the user")
	}
	if TaskOutcomeNeedsUser(TaskOutcomeDone) {
		t.Fatal("done must not count as needing the user")
	}
}
