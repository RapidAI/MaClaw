package botmgmt

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func newParallelService(t *testing.T) *Service {
	t.Helper()
	settings := &memSettings{}
	raw, _ := json.Marshal(record{Bots: []Bot{{ID: "bot_p", OwnerUserID: "alice"}}})
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.BotParallelRuns = 3
	return svc
}

// adoptRun turns a post's reserve into a live run, as followAdmittedDesktopRun
// does once srv names the run (without starting a watcher).
func adoptRun(t *testing.T, svc *Service, cmid, runID string) *desktopAdmitState {
	t.Helper()
	svc.mu.Lock()
	defer svc.mu.Unlock()
	reserve := svc.desktopAdmits[desktopReserveKey("tenant-a", "alice", "bot_p", cmid)]
	if reserve == nil {
		t.Fatalf("no reserve for %s", cmid)
	}
	delete(svc.desktopAdmits, desktopReserveKey("tenant-a", "alice", "bot_p", cmid))
	state := newRunningDesktopTask(admittedDesktopFollow{BotID: "bot_p", UserID: "alice", RunID: runID, ClientMessageID: cmid, Session: reserve.session})
	svc.desktopAdmits[desktopAdmitKey("tenant-a", "alice", "bot_p", runID)] = state
	return state
}

func finishRun(svc *Service, state *desktopAdmitState, reply Reply) {
	state.mu.Lock()
	state.done = true
	session := state.session
	state.mu.Unlock()
	svc.noteDesktopAskOwner("tenant-a", "alice", "bot_p", session, reply)
}

// TestParallelRunsGetTheirOwnConversation: the first run uses the bot's main
// conversation, a run beside it gets its own thread, a question routes the
// answer back to its thread, and a message after everything ended goes to
// the main conversation again.
func TestParallelRunsGetTheirOwnConversation(t *testing.T) {
	svc := newParallelService(t)
	_, a, err := svc.claimDesktopTurnFor("tenant-a", "alice", "bot_p", "k-a", "任务一", "execute")
	if err != nil || a.session != "bot_p" || a.siblings != 0 {
		t.Fatalf("first claim = %+v, %v", a, err)
	}
	runA := adoptRun(t, svc, "k-a", "run-a")
	_, b, err := svc.claimDesktopTurnFor("tenant-a", "alice", "bot_p", "k-b", "任务二", "execute")
	if err != nil || b.session != desktopThreadSession("bot_p", "k-b") || b.siblings != 1 {
		t.Fatalf("second claim = %+v, %v", b, err)
	}
	// A repeat of the same key keeps its conversation.
	if _, again, err := svc.claimDesktopTurnFor("tenant-a", "alice", "bot_p", "k-b", "任务二", "execute"); err != nil || again.session != b.session {
		t.Fatalf("replay claim = %+v, %v", again, err)
	}
	runB := adoptRun(t, svc, "k-b", "run-b")
	// Run B ends with a question; A is still in the main conversation.
	finishRun(svc, runB, Reply{AskUserQuestion: "选哪个版本？"})
	_, answer, err := svc.claimDesktopTurnFor("tenant-a", "alice", "bot_p", "k-ans", "第二个", "execute")
	if err != nil || answer.session != b.session {
		t.Fatalf("answer claim = %+v, %v", answer, err)
	}
	runAns := adoptRun(t, svc, "k-ans", "run-ans")
	finishRun(svc, runAns, Reply{SendToUser: "好了"})
	finishRun(svc, runA, Reply{SendToUser: "做完了"})
	// A clearly new message names the main conversation (the GUI sends the
	// bot id when the person is not following up in a finished thread).
	_, next, err := svc.claimDesktopTurnPref("tenant-a", "alice", "bot_p", "k-next", "新的事", "execute", "bot_p")
	if err != nil || next.session != "bot_p" || next.siblings != 0 {
		t.Fatalf("after all ended = %+v, %v", next, err)
	}
}

// TestFollowUpAfterFinishedThreadStaysInThatConversation: a run that ended
// without a question still keeps a follow-up in its thread (sticky window,
// or a continue_session the caller named). A clearly new message goes to
// main. After the window, a message with no hint also goes to main.
func TestFollowUpAfterFinishedThreadStaysInThatConversation(t *testing.T) {
	svc := newParallelService(t)
	now := svc.now()
	svc.Now = func() time.Time { return now }

	_, a, err := svc.claimDesktopTurnFor("tenant-a", "alice", "bot_p", "k-a", "任务一", "execute")
	if err != nil || a.session != "bot_p" {
		t.Fatalf("first claim = %+v, %v", a, err)
	}
	runA := adoptRun(t, svc, "k-a", "run-a")
	_, b, err := svc.claimDesktopTurnFor("tenant-a", "alice", "bot_p", "k-b", "任务二", "execute")
	if err != nil || b.session != desktopThreadSession("bot_p", "k-b") {
		t.Fatalf("second claim = %+v, %v", b, err)
	}
	runB := adoptRun(t, svc, "k-b", "run-b")
	finishRun(svc, runB, Reply{SendToUser: "做完了，报表在桌上"})
	finishRun(svc, runA, Reply{SendToUser: "主线也做完了"})

	_, follow, err := svc.claimDesktopTurnFor("tenant-a", "alice", "bot_p", "k-follow", "把日期也加上", "execute")
	if err != nil || follow.session != b.session {
		t.Fatalf("follow-up after a finished thread = %+v, %v", follow, err)
	}
	runFollow := adoptRun(t, svc, "k-follow", "run-follow")
	finishRun(svc, runFollow, Reply{SendToUser: "日期加上了"})

	// The GUI named the main conversation: a new, independent message.
	_, fresh, err := svc.claimDesktopTurnPref("tenant-a", "alice", "bot_p", "k-new", "另外一件事", "execute", "bot_p")
	if err != nil || fresh.session != "bot_p" {
		t.Fatalf("independent message = %+v, %v", fresh, err)
	}
	runNew := adoptRun(t, svc, "k-new", "run-new")
	finishRun(svc, runNew, Reply{SendToUser: "新的事也做完了"})

	// Another finished thread, then the sticky window runs out.
	_, d, err := svc.claimDesktopTurnFor("tenant-a", "alice", "bot_p", "k-d", "主线再来一件", "execute")
	if err != nil || d.session != "bot_p" {
		t.Fatalf("main again = %+v, %v", d, err)
	}
	runD := adoptRun(t, svc, "k-d", "run-d")
	_, e, err := svc.claimDesktopTurnFor("tenant-a", "alice", "bot_p", "k-e", "旁边再一件", "execute")
	if err != nil || e.session != desktopThreadSession("bot_p", "k-e") {
		t.Fatalf("another thread = %+v, %v", e, err)
	}
	runE := adoptRun(t, svc, "k-e", "run-e")
	finishRun(svc, runE, Reply{SendToUser: "旁边做完了"})
	finishRun(svc, runD, Reply{SendToUser: "主线做完了"})
	now = now.Add(desktopRecentThreadFor + time.Minute)
	_, late, err := svc.claimDesktopTurnFor("tenant-a", "alice", "bot_p", "k-late", "全新的一件", "execute")
	if err != nil || late.session != "bot_p" {
		t.Fatalf("after the sticky window = %+v, %v", late, err)
	}

	// Naming a finished thread still routes there after the window.
	_, named, err := svc.claimDesktopTurnPref("tenant-a", "alice", "bot_p", "k-named", "再改一处", "execute", b.session)
	if err != nil || named.session != b.session {
		t.Fatalf("named thread after the window = %+v, %v", named, err)
	}
}

// TestSiblingSettleLeavesTheLoginHold: only the turn that owns the hold or
// the taken keyboard releases it; a sibling run settling leaves both.
func TestSiblingSettleLeavesTheLoginHold(t *testing.T) {
	svc := newParallelService(t)
	key := desktopViewKey("tenant-a", "alice")
	// Run H handed the desktop to the person for a login.
	svc.noteDesktopHeld("tenant-a", "alice", "bot_p", "k-h")
	// The continuation C (no sibling) takes the keyboard back.
	svc.releaseDesktopKeyboard("tenant-a", "alice", "bot_p", "k-c")
	svc.mu.Lock()
	if svc.desktopAwaiting[key] || svc.desktopKeyboardTaken[key] != "bot_p" || svc.desktopHoldTurn[key] != "k-c" {
		t.Fatalf("after continuation: awaiting=%v taken=%q owner=%q", svc.desktopAwaiting[key], svc.desktopKeyboardTaken[key], svc.desktopHoldTurn[key])
	}
	svc.mu.Unlock()
	// Sibling S fails: the keyboard stays with C's run, not back to the person.
	svc.restoreDesktopKeyboard("tenant-a", "alice", "bot_p", "k-s")
	// Sibling S succeeds: the hold stays.
	svc.clearDesktopHeld("tenant-a", "alice", "bot_p", "k-s")
	svc.mu.Lock()
	_, held := svc.desktopHeld[key]
	taken := svc.desktopKeyboardTaken[key]
	svc.mu.Unlock()
	if !held || taken != "bot_p" {
		t.Fatalf("sibling released: held=%v taken=%q", held, taken)
	}
	// C, the owner, ends: the hold clears.
	svc.clearDesktopHeld("tenant-a", "alice", "bot_p", "k-c")
	svc.mu.Lock()
	_, held = svc.desktopHeld[key]
	svc.mu.Unlock()
	if held {
		t.Fatal("owner's settle left the hold")
	}
}

// TestSiblingFailureDoesNotHandTheKeyboardBack: the continuation that took the
// keyboard fails and gives it back; a sibling's failure never does.
func TestSiblingFailureDoesNotHandTheKeyboardBack(t *testing.T) {
	svc := newParallelService(t)
	key := desktopViewKey("tenant-a", "alice")
	svc.noteDesktopHeld("tenant-a", "alice", "bot_p", "k-h")
	svc.releaseDesktopKeyboard("tenant-a", "alice", "bot_p", "k-c")
	svc.restoreDesktopKeyboard("tenant-a", "alice", "bot_p", "k-s")
	svc.mu.Lock()
	awaiting := svc.desktopAwaiting[key]
	svc.mu.Unlock()
	if awaiting {
		t.Fatal("a sibling's failure handed the keyboard to the person")
	}
	svc.restoreDesktopKeyboard("tenant-a", "alice", "bot_p", "k-c")
	svc.mu.Lock()
	awaiting = svc.desktopAwaiting[key]
	svc.mu.Unlock()
	if !awaiting {
		t.Fatal("the continuation's failure did not hand the keyboard back")
	}
}
