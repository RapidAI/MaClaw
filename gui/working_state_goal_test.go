package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
)

func TestSharedActiveWorkingStateGoal_OrdinaryChatIgnoresLeftovers(t *testing.T) {
	store := goal.NewStore("")
	if _, err := store.Set("u1", "leftover objective"); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{goalStore: store}
	h.horizonSessions.Store("u1", &horizonSession{
		state: &longhorizon.TaskState{UserGoal: "old horizon"},
	})
	cb := &sharedAgentLoopCallbacks{handler: h, userID: "u1", platform: "desktop"}
	if got := cb.ActiveWorkingStateGoal(); got != "" {
		t.Fatalf("ordinary chat projected leftover: %q", got)
	}
}

func TestSharedActiveWorkingStateGoal_HorizonRole(t *testing.T) {
	h := &IMMessageHandler{}
	h.horizonSessions.Store("u1", &horizonSession{
		state: &longhorizon.TaskState{UserGoal: "horizon goal text"},
	})
	cb := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "u1",
		loopCtx: &LoopContext{HorizonRole: longhorizon.RoleCLIExecutor},
	}
	if got := cb.ActiveWorkingStateGoal(); got != "horizon goal text" {
		t.Fatalf("got %q", got)
	}
}

func TestSharedActiveWorkingStateGoal_CancelledHorizonEmpty(t *testing.T) {
	h := &IMMessageHandler{}
	h.horizonSessions.Store("u1", &horizonSession{
		state:     &longhorizon.TaskState{UserGoal: "horizon goal text"},
		cancelled: true,
	})
	cb := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "u1",
		loopCtx: &LoopContext{HorizonRole: longhorizon.RoleCLIExecutor},
	}
	if got := cb.ActiveWorkingStateGoal(); got != "" {
		t.Fatalf("cancelled horizon projected: %q", got)
	}
}

func TestSharedActiveWorkingStateGoal_Continuation(t *testing.T) {
	store := goal.NewStore("")
	if _, err := store.Set("u1", "continue this"); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{goalStore: store}
	cb := &sharedAgentLoopCallbacks{handler: h, userID: "u1", platform: "goal-continuation"}
	if got := cb.ActiveWorkingStateGoal(); got != "continue this" {
		t.Fatalf("got %q", got)
	}
}

func TestSharedActiveWorkingStateGoal_TerminalContinuationEmpty(t *testing.T) {
	store := goal.NewStore("")
	g, err := store.Set("u1", "done already")
	if err != nil {
		t.Fatal(err)
	}
	if !store.UpdateStatus("u1", g.GoalID, goal.StatusComplete, "ok") {
		t.Fatal("status update failed")
	}
	h := &IMMessageHandler{goalStore: store}
	cb := &sharedAgentLoopCallbacks{handler: h, userID: "u1", platform: "goal-continuation"}
	if got := cb.ActiveWorkingStateGoal(); got != "" {
		t.Fatalf("terminal goal projected: %q", got)
	}
}

func TestSharedLoadWorkingStateUsesResumeOnly(t *testing.T) {
	h := &IMMessageHandler{}
	h.pendingAskUser.Store("u1", &pendingAskUserState{
		WorkingState: agent.NewWorkingState("leftover pending"),
	})
	cb := &sharedAgentLoopCallbacks{handler: h, userID: "u1", platform: "desktop"}
	if got := cb.LoadWorkingState(); got != nil {
		t.Fatalf("leftover pending leaked into load: %#v", got)
	}
	cb.loopCtx = &LoopContext{ResumeWorkingState: agent.NewWorkingState("resume goal")}
	got := cb.LoadWorkingState()
	if got == nil || got.Goal != "resume goal" {
		t.Fatalf("resume carrier missing: %#v", got)
	}
}

func TestConsumePendingAskUserAnswerReturnsWorkingState(t *testing.T) {
	h := &IMMessageHandler{}
	entries := []agent.ConversationEntry{{Role: "user", Content: "publish"}}
	paused := agent.NewWorkingState("keep goal")
	paused.LastAction = agent.ActionSeekUser
	paused.Next = "询问用户后继续"
	h.pendingAskUser.Store("user-1", &pendingAskUserState{
		Question:     "choose",
		History:      entries,
		Timestamp:    time.Now(),
		WorkingState: paused,
	})
	_, ws, ok := h.consumePendingAskUserAnswer("user-1", "A", entries)
	if !ok || ws == nil || ws.Goal != "keep goal" {
		t.Fatalf("ok=%v ws=%#v", ok, ws)
	}
	if ws.LastAction != "" {
		t.Fatalf("answered ask_user left LastAction=%q", ws.LastAction)
	}
	if !strings.Contains(ws.Next, "keep goal") {
		t.Fatalf("answered ask_user left Next=%q", ws.Next)
	}
}

func TestOpenRecordAudioCasualChatKeepsWorkingState(t *testing.T) {
	h := &IMMessageHandler{}
	entries := []agent.ConversationEntry{{Role: "user", Content: "record this"}}
	paused := agent.NewWorkingState("keep recording goal")
	paused.LastAction = agent.ActionSeekUser
	paused.Next = "询问用户后继续"
	h.pendingRecordAudio.Store("user-1", &pendingRecordAudioState{
		Title:        "meeting",
		History:      entries,
		Timestamp:    time.Now(),
		WorkingState: paused,
	})
	msg := IMUserMessage{UserID: "user-1", Text: "顺便问下天气", Platform: "desktop"}
	trimmed := "顺便问下天气"
	got := h.resolveIMEntryContext(imEntryContextOptions{
		Message:            &msg,
		Trimmed:            &trimmed,
		EntriesBeforeClear: entries,
		ConfirmedResume:    true,
	})
	if !got.HasPendingAskUser {
		t.Fatal("open recording should stay pending")
	}
	if got.ResumeWorkingState == nil || got.ResumeWorkingState.Goal != "keep recording goal" {
		t.Fatalf("open-mic chat dropped workspace: %#v", got.ResumeWorkingState)
	}
	if got.ResumeWorkingState.LastAction != agent.ActionSeekUser || got.ResumeWorkingState.Next != "询问用户后继续" {
		t.Fatalf("open-mic must keep seek pause: %#v", got.ResumeWorkingState)
	}
}

func TestPostRecordingSoftChatKeepsSeekPause(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	prior := []agent.ConversationEntry{{Role: "user", Content: "record this"}}
	paused := agent.NewWorkingState("keep recording goal")
	paused.LastAction = agent.ActionSeekUser
	paused.Next = "询问用户后继续"
	if resp := h.offerPostRecordingChoice("u1", "meeting", "", "[Recording completed]\npath: C:\\tmp\\a.wav\n", "zh", prior, paused); resp == nil {
		t.Fatal("expected post-recording offer")
	}
	entries := h.memory.Load("u1")
	msg := IMUserMessage{UserID: "u1", Text: "顺便问下时间", Platform: "desktop"}
	trimmed := "顺便问下时间"
	got := h.resolveIMEntryContext(imEntryContextOptions{
		Message:            &msg,
		Trimmed:            &trimmed,
		EntriesBeforeClear: entries,
		ConfirmedResume:    true,
	})
	if got.ResumeWorkingState == nil || got.ResumeWorkingState.LastAction != agent.ActionSeekUser {
		t.Fatalf("soft chat during buttons must keep seek: %#v", got.ResumeWorkingState)
	}

	choice := "__record_post__ minutes"
	msg.Text = choice
	picked := h.resolveIMEntryContext(imEntryContextOptions{
		Message:            &msg,
		Trimmed:            &choice,
		EntriesBeforeClear: entries,
		ConfirmedResume:    true,
	})
	if picked.ResumeWorkingState == nil || picked.ResumeWorkingState.LastAction != "" {
		t.Fatalf("chosen action must advance seek: %#v", picked.ResumeWorkingState)
	}
	if !strings.Contains(picked.ResumeWorkingState.Next, "keep recording goal") {
		t.Fatalf("chosen action Next=%q", picked.ResumeWorkingState.Next)
	}
}

func TestBindLoopResumeWorkingStateClearsLeftover(t *testing.T) {
	ctx := NewLoopContext("reuse", 3, nil)
	ctx.ResumeWorkingState = agent.NewWorkingState("old task")
	bindLoopResumeWorkingState(ctx, nil, "")
	if ctx.ResumeWorkingState != nil {
		t.Fatalf("leftover resume survived ordinary turn: %#v", ctx.ResumeWorkingState)
	}

	ctx.ResumeWorkingState = agent.NewWorkingState("old task")
	bindLoopResumeWorkingState(ctx, agent.NewWorkingState("ask answer"), "")
	if ctx.ResumeWorkingState != nil {
		t.Fatalf("resume without ask context must not bind: %#v", ctx.ResumeWorkingState)
	}

	fresh := agent.NewWorkingState("keep goal")
	bindLoopResumeWorkingState(ctx, fresh, "[Context hint] answering")
	if ctx.ResumeWorkingState == nil || ctx.ResumeWorkingState.Goal != "keep goal" {
		t.Fatalf("ask-user resume missing: %#v", ctx.ResumeWorkingState)
	}
	if ctx.ResumeWorkingState == fresh {
		t.Fatal("resume must be cloned")
	}
}

func TestSharedCallbacksSeededRevisionIsNotMidTaskSteer(t *testing.T) {
	ctx := NewLoopContext("seed-replan", 3, nil)
	ctx.RequestReplan()
	cb := &sharedAgentLoopCallbacks{loopCtx: ctx}
	cb.llmReplanRevision.Store(ctx.ReplanRevision())
	if cb.LLMReplanRequested() {
		t.Fatal("leftover pre-loop revision must not clear Live/Open")
	}
}

func TestSharedLoopUserFacingTextStripsMarker(t *testing.T) {
	leaked := "answer\n" + agent.WorkingStateMarker + "\n目标: leaked"
	got := sharedLoopUserFacingText(leaked)
	if strings.Contains(got, agent.WorkingStateMarker) {
		t.Fatalf("bubble leaked marker: %q", got)
	}
	if !strings.Contains(got, "answer") {
		t.Fatalf("stripped too much: %q", got)
	}
	inline := "see " + agent.WorkingStateMarker + " inline"
	if sharedLoopUserFacingText(inline) != inline {
		t.Fatalf("inline mention changed: %q", sharedLoopUserFacingText(inline))
	}
}
