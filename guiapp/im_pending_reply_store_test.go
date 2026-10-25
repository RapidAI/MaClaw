package guiapp

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestPendingAskUserSurvivesHandlerRestart(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	history := []agent.ConversationEntry{
		{Role: "user", Content: "make a deck"},
		{Role: "assistant", Content: "which color?"},
	}
	memory.Save(desktopUserID, history)
	h := &IMMessageHandler{app: app, memory: memory}
	h.storePendingAskUser(desktopUserID, &pendingAskUserState{
		Question: "which color?", Options: []string{"blue", "red"}, InputType: "choice",
		History: history, Timestamp: time.Now(), WorkingState: agent.NewWorkingState("make a deck"),
	})

	restarted := &IMMessageHandler{app: app, memory: memory}
	if !restarted.freshPendingAskUser(desktopUserID) {
		t.Fatal("pending ask_user was not restored")
	}
	context, working, ok, _ := restarted.consumePendingAskUserAnswer(desktopUserID, "1", history)
	if !ok || !strings.Contains(context, "blue") {
		t.Fatalf("restored answer context = %q, ok=%v", context, ok)
	}
	if working == nil || !strings.Contains(working.Goal, "make a deck") {
		t.Fatalf("working state was not restored: %#v", working)
	}

	restartedAgain := &IMMessageHandler{app: app, memory: memory}
	if restartedAgain.freshPendingAskUser(desktopUserID) {
		t.Fatal("consumed question was resurrected")
	}
}

func TestPendingUserReplySurvivesHandlerRestart(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	h := &IMMessageHandler{app: app}
	history := []agent.ConversationEntry{{Role: "assistant", Content: "which model?"}}
	h.storePendingUserReply(desktopUserID, &pendingUserReplyState{Question: "which model?", History: history, Timestamp: time.Now()})

	restarted := &IMMessageHandler{app: app}
	restarted.ensurePendingReplyStore()
	raw, ok := restarted.takePendingUserReply(desktopUserID)
	if !ok {
		t.Fatal("pending prose reply was not restored")
	}
	pending, _ := raw.(*pendingUserReplyState)
	if pending == nil || pending.Question != "which model?" || len(pending.History) != 1 {
		t.Fatalf("restored pending reply = %#v", pending)
	}
	restartedAgain := &IMMessageHandler{app: app}
	restartedAgain.ensurePendingReplyStore()
	if _, ok := restartedAgain.pendingUserReply.Load(desktopUserID); ok {
		t.Fatal("consumed prose question was resurrected")
	}
}

func TestPendingAskUserDeleteWinsOverStaleHandler(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	first := &IMMessageHandler{app: app}
	second := &IMMessageHandler{app: app}
	first.storePendingAskUser(desktopUserID, &pendingAskUserState{
		Question: "which color?", Timestamp: time.Now(),
	})

	second.ensurePendingReplyStore()
	if _, ok := second.takePendingAskUser(desktopUserID); !ok {
		t.Fatal("second handler did not restore ask state")
	}

	first.storePendingUserReply(desktopUserID, &pendingUserReplyState{
		Question: "which model?", Timestamp: time.Now(),
	})
	third := &IMMessageHandler{app: app}
	third.ensurePendingReplyStore()
	if _, ok := third.pendingAskUser.Load(desktopUserID); ok {
		t.Fatal("stale handler resurrected a consumed ask")
	}
	if _, ok := third.pendingUserReply.Load(desktopUserID); !ok {
		t.Fatal("concurrent newer user reply was lost")
	}
}

func TestPendingAskUserDeleteClearsAbsentSnapshotState(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	first := &IMMessageHandler{app: app}
	second := &IMMessageHandler{app: app}
	first.storePendingAskUser(desktopUserID, &pendingAskUserState{
		Question: "which color?", Timestamp: time.Now(),
	})

	second.ensurePendingReplyStore()
	if _, ok := second.takePendingAskUser(desktopUserID); !ok {
		t.Fatal("second handler did not restore ask state")
	}
	if _, ok := first.takePendingAskUser(desktopUserID); ok {
		t.Fatal("stale handler consumed a deleted ask after snapshot removal")
	}
}

func TestPendingUserReplyDeleteClearsAbsentSnapshotState(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	first := &IMMessageHandler{app: app}
	second := &IMMessageHandler{app: app}
	first.storePendingUserReply(desktopUserID, &pendingUserReplyState{
		Question: "which model?", Timestamp: time.Now(),
	})

	second.ensurePendingReplyStore()
	if _, ok := second.takePendingUserReply(desktopUserID); !ok {
		t.Fatal("second handler did not restore prose reply")
	}
	if _, ok := first.takePendingUserReply(desktopUserID); ok {
		t.Fatal("stale handler consumed a deleted prose reply after snapshot removal")
	}
}
