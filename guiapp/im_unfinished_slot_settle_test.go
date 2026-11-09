package guiapp

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestSuccessfulTurnCompletesResumedUnfinishedSlot(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user:D:/tasks/query"
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "resume-1", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		LastTask: "列出 employees 表中的数据", LastToolName: "database_query",
	})
	if !memory.BindUnfinishedSlot(userID, "resume-1") {
		t.Fatal("resume bind failed")
	}
	h := &IMMessageHandler{memory: memory}

	h.settleResumedUnfinishedSlot(userID, nil, &IMAgentResponse{Text: "共 9 条记录"})

	if got := memory.GetUnfinishedSlot(userID); got != nil {
		t.Fatalf("resumed slot survived a successful turn: %#v", got)
	}
}

func TestUnfinishedTurnReopensResumedSlot(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user:D:/tasks/query"
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "resume-1", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		LastToolName: "database_query",
	})
	if !memory.BindUnfinishedSlot(userID, "resume-1") {
		t.Fatal("resume bind failed")
	}
	h := &IMMessageHandler{memory: memory}
	cancelled := &LoopContext{}
	cancelled.MarkUserCancel()

	h.settleResumedUnfinishedSlot(userID, cancelled, &IMAgentResponse{Text: "Task cancelled"})
	got := memory.GetUnfinishedSlot(userID)
	if got == nil || got.Status != agent.UnfinishedTaskSlotStatusInterrupted || got.SlotID != "resume-1" {
		t.Fatalf("cancelled continuation slot = %#v, want the same slot interrupted", got)
	}
	if active := memory.ActiveUnfinishedSlot(userID); active != nil {
		t.Fatalf("cancelled continuation stayed bound: %#v", active)
	}

	if !memory.BindUnfinishedSlot(userID, "resume-1") {
		t.Fatal("second resume bind failed")
	}
	h.settleResumedUnfinishedSlot(userID, nil, &IMAgentResponse{HardExit: true})
	got = memory.GetUnfinishedSlot(userID)
	if got == nil || got.Status != agent.UnfinishedTaskSlotStatusInterrupted {
		t.Fatalf("hard-exit continuation slot = %#v, want interrupted", got)
	}
}

func TestPreLoopCancelReopensResumedSlot(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user:D:/tasks/query"
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "resume-1", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		LastToolName: "database_query",
	})
	if !memory.BindUnfinishedSlot(userID, "resume-1") {
		t.Fatal("resume bind failed")
	}
	h := &IMMessageHandler{memory: memory}
	// A pause before the loop starts closes the context without MarkUserCancel.
	cancelled := &LoopContext{}
	cancelled.Cancel()

	h.settleResumedUnfinishedSlot(userID, cancelled, &IMAgentResponse{Text: "Task cancelled"})
	got := memory.GetUnfinishedSlot(userID)
	if got == nil || got.Status != agent.UnfinishedTaskSlotStatusInterrupted || got.SlotID != "resume-1" {
		t.Fatalf("pre-loop cancel slot = %#v, want interrupted", got)
	}
}

func TestAbandonedMaxRoundsBindRestoresInterruptedSlot(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user:D:/tasks/query"
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "max-1", UserID: userID, Status: agent.UnfinishedTaskSlotStatusMaxRoundsReached,
		LastTask: "列出 employees 表中的数据",
	})
	h := &IMMessageHandler{memory: memory}
	if !h.bindPendingMaxRoundsSlot(userID) {
		t.Fatal("max-rounds slot was not bound")
	}
	// The wave was superseded after the bind and will not run.
	h.settleResumedUnfinishedSlot(userID, nil, &IMAgentResponse{Error: "session round superseded"})
	got := memory.GetUnfinishedSlot(userID)
	if got == nil || got.Status != agent.UnfinishedTaskSlotStatusInterrupted || got.SlotID != "max-1" {
		t.Fatalf("abandoned max-rounds resume = %#v, want interrupted", got)
	}
}

func TestLaterTurnDoesNotReopenAnOlderResume(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user:D:/tasks/query"
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "resume-old", UserID: userID, Status: agent.UnfinishedTaskSlotStatusResumed,
		LastToolName: "database_query", BoundAt: time.Now().Add(-time.Hour),
	})
	h := &IMMessageHandler{memory: memory}
	later := &LoopContext{StartedAt: time.Now()}

	h.settleResumedUnfinishedSlot(userID, later, &IMAgentResponse{Error: "llm down"})
	if got := memory.GetUnfinishedSlot(userID); got == nil || got.Status != agent.UnfinishedTaskSlotStatusResumed || got.SlotID != "resume-old" {
		t.Fatalf("later failure reopened an older resume: %#v", got)
	}

	h.settleResumedUnfinishedSlot(userID, later, &IMAgentResponse{Text: "done"})
	if got := memory.GetUnfinishedSlot(userID); got != nil {
		t.Fatalf("later success left the older resume in place: %#v", got)
	}
}

func TestSettleLeavesPendingAndExternalSlots(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user:D:/tasks/query"
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "pending-1", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
	})
	h := &IMMessageHandler{memory: memory}
	h.settleResumedUnfinishedSlot(userID, nil, &IMAgentResponse{Text: "done"})
	if got := memory.GetUnfinishedSlot(userID); got == nil || got.Status != agent.UnfinishedTaskSlotStatusInterrupted {
		t.Fatalf("pending slot changed: %#v", got)
	}

	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "mail-1", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		RecoveryMode: "requires_review", LastToolName: "im_message", SideEffectState: "external_uncertain",
	})
	if !memory.BindUnfinishedSlot(userID, "mail-1") {
		t.Fatal("external resume bind failed")
	}
	h.settleResumedUnfinishedSlot(userID, nil, &IMAgentResponse{Text: "done"})
	if got := memory.GetUnfinishedSlot(userID); got == nil || got.SlotID != "mail-1" || got.Status != agent.UnfinishedTaskSlotStatusResumed {
		t.Fatalf("external review slot changed: %#v", got)
	}
}

func boundQueryResume(t *testing.T) (*agent.ConversationMemory, *IMMessageHandler, string) {
	t.Helper()
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user:D:/tasks/query"
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "resume-1", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		LastTask: "列出 employees 表中的数据", LastToolName: "database_query",
	})
	if !memory.BindUnfinishedSlot(userID, "resume-1") {
		t.Fatal("resume bind failed")
	}
	return memory, &IMMessageHandler{memory: memory}, userID
}

func TestCancelFenceBeforeLoopReopensResumedSlot(t *testing.T) {
	memory, h, userID := boundQueryResume(t)
	h.cancelledTaskBoundary.Store(userID, time.Now())
	resp := h.executePreparedIMEntry(preparedIMEntryExecutionOptions{
		Message: IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续上次未完成任务"},
		Trimmed: "继续上次未完成任务",
	})
	if resp == nil || strings.TrimSpace(resp.Text) == "" {
		t.Fatalf("cancel fence response = %#v", resp)
	}
	got := memory.GetUnfinishedSlot(userID)
	if got == nil || got.Status != agent.UnfinishedTaskSlotStatusInterrupted || got.SlotID != "resume-1" {
		t.Fatalf("cancel fence left slot %#v", got)
	}
}

func TestCancelledLoopBeforeClassifyReopensResumedSlot(t *testing.T) {
	memory, h, userID := boundQueryResume(t)
	loop := NewLoopContext("chat", 1, nil)
	loop.Cancel()
	resp := h.executePreparedIMEntry(preparedIMEntryExecutionOptions{
		Message:             IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续上次未完成任务"},
		Trimmed:             "继续上次未完成任务",
		ProvidedLoopContext: loop,
	})
	if resp == nil {
		t.Fatal("cancelled loop produced no response")
	}
	got := memory.GetUnfinishedSlot(userID)
	if got == nil || got.Status != agent.UnfinishedTaskSlotStatusInterrupted || got.SlotID != "resume-1" {
		t.Fatalf("pre-classify pause left slot %#v, resp=%#v", got, resp)
	}
}

func TestUnarmedRemoteReturnReopensResumedSlot(t *testing.T) {
	memory, h, userID := boundQueryResume(t)
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:          "remote",
		RemoteHost:    "home.example",
		RemoteUser:    "ra",
		RemoteWorkDir: "/home/ra/work",
	})
	resp := h.executePreparedIMEntry(preparedIMEntryExecutionOptions{
		Message: IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续优化"},
		Trimmed: "继续优化",
	})
	if resp == nil || resp.ResponseSource != "remote_coding_reconnect" {
		t.Fatalf("resp=%#v", resp)
	}
	got := memory.GetUnfinishedSlot(userID)
	if got == nil || got.Status != agent.UnfinishedTaskSlotStatusInterrupted || got.SlotID != "resume-1" {
		t.Fatalf("reconnect left slot %#v", got)
	}
}
