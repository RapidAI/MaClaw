package botmgmt

import (
	"context"
	"encoding/json"
	"testing"
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
	_, next, err := svc.claimDesktopTurnFor("tenant-a", "alice", "bot_p", "k-next", "新的事", "execute")
	if err != nil || next.session != "bot_p" || next.siblings != 0 {
		t.Fatalf("after all ended = %+v, %v", next, err)
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
