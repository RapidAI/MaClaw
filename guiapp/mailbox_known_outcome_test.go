package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
)

func TestMailboxRequiredSendResolvedLocally(t *testing.T) {
	notConfigured := "[system rejected] " + mailbox.MsgNotConfigured
	history := []agent.ConversationEntry{
		{Role: "user", Content: "向znsoft@163.com发邮件"},
		{Role: "tool", Content: notConfigured, ToolName: "mailbox_send"},
	}
	result := agent.LoopResult{Error: "required_effect_unsettled", HardExit: true}
	if !mailboxRequiredSendResolvedLocally(result, history) {
		t.Fatal("a host-known mailbox non-send must finish the in-flight marker")
	}

	uncertain := []agent.ConversationEntry{
		history[0],
		{Role: "tool", Content: "[system unknown] " + mailbox.MsgUncertain, ToolName: "mailbox_send"},
	}
	if mailboxRequiredSendResolvedLocally(result, uncertain) {
		t.Fatal("an uncertain mailbox result must keep the marker")
	}
	if mailboxRequiredSendResolvedLocally(result, []agent.ConversationEntry{{Role: "user", Content: "hi"}}) {
		t.Fatal("a turn with no mailbox tool must keep the marker")
	}
	if mailboxRequiredSendResolvedLocally(result, nil) {
		t.Fatal("a missing mailbox result must keep the marker")
	}

	asked := result
	asked.AskUser = &agent.AskUserRequest{Question: "发送这封邮件？"}
	if mailboxRequiredSendResolvedLocally(asked, history) {
		t.Fatal("an ask-user pause must keep the marker")
	}

	confirm := []agent.ConversationEntry{
		history[0],
		{Role: "tool", Content: `__ASK_USER__{"question":"发送这封邮件？"}`, ToolName: "mailbox_send"},
	}
	if mailboxRequiredSendResolvedLocally(result, confirm) {
		t.Fatal("a confirmation card must keep the marker")
	}

	withIM := append(append([]agent.ConversationEntry{}, history...), agent.ConversationEntry{
		Role: "tool", Content: "已发出", ToolName: "im_message",
	})
	if mailboxRequiredSendResolvedLocally(result, withIM) {
		t.Fatal("an im send in the same turn must keep the marker")
	}

	if sharedLoopTurnSettled(result, &IMAgentResponse{Error: "required_effect_unsettled"}, &sharedAgentLoopCallbacks{}) {
		t.Fatal("a known mailbox non-send is not a host-settled delivery")
	}

	laterTurn := []agent.ConversationEntry{{Role: "assistant", Content: "这一轮没有发信"}}
	if mailboxRequiredSendResolvedLocally(result, laterTurn) {
		t.Fatal("a later turn must not be settled by an earlier known mailbox result")
	}
	if !sharedLoopMayCompleteMailboxMarker(result, history, history) {
		t.Fatal("a known mailbox non-send must finish when this history has no uncertain result")
	}
	withOlderUncertain := append(append([]agent.ConversationEntry{}, uncertain...), history[1])
	if sharedLoopMayCompleteMailboxMarker(result, history, withOlderUncertain) {
		t.Fatal("an older uncertain mailbox result must keep the marker")
	}
	olderConfirm := []agent.ConversationEntry{
		{Role: "tool", Content: `__ASK_USER__{"question":"发送这封邮件？"}`, ToolName: "mailbox_send"},
		history[1],
	}
	if !sharedLoopMayCompleteMailboxMarker(result, history, olderConfirm) {
		t.Fatal("an older confirmation card must not keep the marker")
	}
}

func TestReleaseSettledMailboxReviewSlotKeepsHistory(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user"
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "向znsoft@163.com发邮件，报告北京天气"},
		{Role: "tool", Content: "[system rejected] " + mailbox.MsgNotConfigured, ToolName: "mailbox_send"},
	}
	memory.Save(userID, entries)
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID:          "interrupted-mail",
		UserID:          userID,
		Status:          agent.UnfinishedTaskSlotStatusInterrupted,
		LastTask:        "向znsoft@163.com发邮件，报告北京天气",
		LastToolName:    "mailbox_send",
		SideEffectState: "external_uncertain",
		RecoveryMode:    "requires_review",
	})
	h := &IMMessageHandler{memory: memory}
	if got := h.releaseSettledMailboxReviewSlot(userID, memory.GetUnfinishedSlot(userID), memory.Load(userID)); got != nil {
		t.Fatalf("settled mailbox slot stayed: %#v", got)
	}
	if slot := memory.GetUnfinishedSlot(userID); slot != nil {
		t.Fatalf("slot remained: %#v", slot)
	}
	loaded := memory.Load(userID)
	if len(loaded) != 2 {
		t.Fatalf("history changed: %#v", loaded)
	}
	if text, ok := loaded[0].Content.(string); !ok || text != "向znsoft@163.com发邮件，报告北京天气" {
		t.Fatalf("history changed: %#v", loaded)
	}
}

func TestReleaseSettledMailboxReviewSlotKeepsUncertainAndIM(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user"
	memory.Save(userID, []agent.ConversationEntry{
		{Role: "tool", Content: "[system unknown] " + mailbox.MsgUncertain, ToolName: "mailbox_send"},
	})
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "uncertain-mail", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		LastToolName: "mailbox_send", SideEffectState: "external_uncertain", RecoveryMode: "requires_review",
	})
	h := &IMMessageHandler{memory: memory}
	if got := h.releaseSettledMailboxReviewSlot(userID, memory.GetUnfinishedSlot(userID), memory.Load(userID)); got == nil || got.SlotID != "uncertain-mail" {
		t.Fatalf("uncertain mailbox slot = %#v", got)
	}

	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "im-send", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		LastToolName: "im_message", SideEffectState: "external_uncertain", RecoveryMode: "requires_review",
	})
	memory.Save(userID, []agent.ConversationEntry{
		{Role: "tool", Content: "[system rejected] " + mailbox.MsgNotConfigured, ToolName: "im_message"},
	})
	if got := h.releaseSettledMailboxReviewSlot(userID, memory.GetUnfinishedSlot(userID), memory.Load(userID)); got == nil || got.LastToolName != "im_message" {
		t.Fatalf("im slot = %#v", got)
	}
}

func TestReleaseSettledMailboxReviewSlotKeepsDroppedUncertain(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user"
	memory.Save(userID, []agent.ConversationEntry{
		{Role: "tool", Content: "[system rejected] " + mailbox.MsgNotConfigured, ToolName: "mailbox_send", ToolCallID: "new-known"},
	})
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "dropped-uncertain", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		LastToolName: "mailbox_send", SideEffectState: "external_uncertain", RecoveryMode: "requires_review",
		MailboxUncertainDropped: true,
	})
	h := &IMMessageHandler{memory: memory}
	if got := h.releaseSettledMailboxReviewSlot(userID, memory.GetUnfinishedSlot(userID), memory.Load(userID)); got == nil || got.SlotID != "dropped-uncertain" {
		t.Fatalf("dropped uncertain slot = %#v", got)
	}
}

func TestRecoverInterruptedMailboxNonSendDoesNotOpenReviewSlot(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user"
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "发邮件"},
		{Role: "tool", Content: "[system rejected] " + mailbox.MsgNotConfigured, ToolName: "mailbox_send"},
	}
	if err := memory.PersistInFlightCheckpoint(userID, entries, "发邮件", "", "run-mail", agent.InFlightCheckpoint{
		Sequence: 1, LastToolName: "mailbox_send", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{memory: memory}
	if slot := h.recoverInterruptedTaskSlot(userID, memory.Load(userID)); slot != nil {
		t.Fatalf("known non-send opened a review slot: %#v", slot)
	}
	if slot := memory.GetUnfinishedSlot(userID); slot != nil {
		t.Fatalf("slot remained: %#v", slot)
	}
}

func TestRecoverKnownMailboxBesideConfirmationDoesNotOpenReviewSlot(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user"
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "发邮件"},
		{Role: "tool", Content: `__ASK_USER__{"question":"发送这封邮件？"}`, ToolName: "mailbox_send", ToolCallID: "old-confirm"},
		{Role: "tool", Content: "[system rejected] " + mailbox.MsgNotConfigured, ToolName: "mailbox_send", ToolCallID: "new-known"},
	}
	if err := memory.PersistInFlightCheckpoint(userID, entries, "发邮件", "", "run-mail", agent.InFlightCheckpoint{
		Sequence: 1, LastToolName: "mailbox_send", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{memory: memory}
	if slot := h.recoverInterruptedTaskSlot(userID, memory.Load(userID)); slot != nil {
		t.Fatalf("known result beside a confirmation opened a review slot: %#v", slot)
	}
	if slot := memory.GetUnfinishedSlot(userID); slot != nil {
		t.Fatalf("slot remained: %#v", slot)
	}
}

func TestReleaseSettledMailboxReviewSlotKeepsTheQueuedDecision(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user"
	notConfigured := "[system rejected] " + mailbox.MsgNotConfigured
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "发邮件"},
		{Role: "tool", Content: `__ASK_USER__{"question":"发送这封邮件？"}`, ToolName: "mailbox_send", ToolCallID: "old-confirm"},
		{Role: "tool", Content: notConfigured, ToolName: "mailbox_send", ToolCallID: "new-known"},
	}
	memory.Save(userID, entries)
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "dropped-uncertain", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		LastToolName: "mailbox_send", SideEffectState: "external_uncertain", RecoveryMode: "requires_review",
		Source: agent.UnfinishedTaskSlotSourceInFlightRecovery, MailboxUncertainDropped: true, LastTask: "可能已发出",
	})
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "settled-behind", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		LastToolName: "mailbox_send", SideEffectState: "external_uncertain", RecoveryMode: "requires_review",
		Source: agent.UnfinishedTaskSlotSourceInFlightRecovery, LastTask: "旧的未配置",
	})
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "settled-current", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		LastToolName: "mailbox_send", SideEffectState: "external_uncertain", RecoveryMode: "requires_review",
		Source: agent.UnfinishedTaskSlotSourceInFlightRecovery, LastTask: "尚未配置",
	})
	h := &IMMessageHandler{memory: memory}
	got := h.releaseSettledMailboxReviewSlot(userID, memory.GetUnfinishedSlot(userID), memory.Load(userID))
	if got == nil || got.SlotID != "dropped-uncertain" || !got.MailboxUncertainDropped {
		t.Fatalf("queued decision = %#v", got)
	}
	if slot := memory.GetUnfinishedSlot(userID); slot == nil || slot.SlotID != "dropped-uncertain" {
		t.Fatalf("current slot = %#v", slot)
	}
	for _, slot := range memory.UnfinishedSlots() {
		if slot != nil && (slot.SlotID == "settled-behind" || slot.SlotID == "settled-current") {
			t.Fatalf("settled mailbox slot survived: %#v", slot)
		}
	}
	if loaded := memory.Load(userID); len(loaded) != len(entries) {
		t.Fatalf("history changed: %#v", loaded)
	}
}

func TestReleaseSettledMailboxResumeContext(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user"
	entries := []agent.ConversationEntry{
		{Role: "tool", Content: `__ASK_USER__{"question":"发送这封邮件？"}`, ToolName: "mailbox_send", ToolCallID: "old-confirm"},
		{Role: "tool", Content: "[system rejected] " + mailbox.MsgNotConfigured, ToolName: "mailbox_send", ToolCallID: "new-known"},
	}
	memory.Save(userID, entries)
	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "resumed-mail", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		LastToolName: "mailbox_send", SideEffectState: "none", RecoveryMode: "resume_context",
	})
	h := &IMMessageHandler{memory: memory}
	if got := h.releaseSettledMailboxReviewSlot(userID, memory.GetUnfinishedSlot(userID), memory.Load(userID)); got != nil {
		t.Fatalf("resume_context mailbox slot stayed: %#v", got)
	}
	if loaded := memory.Load(userID); len(loaded) != len(entries) {
		t.Fatalf("history changed: %#v", loaded)
	}

	memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID: "search", UserID: userID, Status: agent.UnfinishedTaskSlotStatusInterrupted,
		LastToolName: "tools_search", SideEffectState: "none", RecoveryMode: "resume_context",
	})
	if got := h.releaseSettledMailboxReviewSlot(userID, memory.GetUnfinishedSlot(userID), memory.Load(userID)); got == nil || got.SlotID != "search" {
		t.Fatalf("read-only slot = %#v", got)
	}
}

func TestRecoverSecondMailboxSendKeepsTheUnreportedLatch(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user"
	known := []agent.ConversationEntry{
		{Role: "user", Content: "发邮件"},
		{Role: "tool", Content: "[system rejected] " + mailbox.MsgNotConfigured, ToolName: "mailbox_send"},
	}
	if err := memory.PersistInFlightCheckpoint(userID, known, "发邮件", "", "run-first", agent.InFlightCheckpoint{
		Sequence: 1, LastToolName: "mailbox_send", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	if err := memory.CompleteInFlightCheckpointForRun(userID, "run-first"); err != nil {
		t.Fatal(err)
	}
	if err := memory.PersistInFlightCheckpoint(userID, known, "发邮件", "", "run-second", agent.InFlightCheckpoint{
		Sequence: 1, LastToolName: "mailbox_send", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{memory: memory}
	slot := h.recoverInterruptedTaskSlot(userID, memory.Load(userID))
	if slot == nil || slot.LastToolName != "mailbox_send" || slot.SideEffectState != "external_uncertain" || !slot.MailboxResultBaselineSet {
		t.Fatalf("unreported second send = %#v", slot)
	}
	if got := h.releaseSettledMailboxReviewSlot(userID, memory.GetUnfinishedSlot(userID), memory.Load(userID)); got == nil || got.SlotID != slot.SlotID {
		t.Fatalf("historical known result released the new latch: %#v", got)
	}
}

func TestSaveAndCompleteDroppedUncertainSurvivesReviewRelease(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user"
	uncertain := "[system unknown] " + mailbox.MsgUncertain
	notConfigured := "[system rejected] " + mailbox.MsgNotConfigured
	full := []agent.ConversationEntry{
		{Role: "user", Content: "发邮件"},
		{Role: "tool", Content: uncertain, ToolName: "mailbox_send", ToolCallID: "old-uncertain"},
	}
	if err := memory.PersistInFlightCheckpoint(userID, full, "发邮件", "/project", "run-mail", agent.InFlightCheckpoint{
		Sequence: 1, LastToolName: "mailbox_send", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	tail := []agent.ConversationEntry{{Role: "user", Content: "这一轮的摘要"}}
	if err := memory.PersistInFlightCheckpoint(userID, tail, "发邮件", "/project", "run-mail", agent.InFlightCheckpoint{
		Sequence: 2, LastToolName: "mailbox_send", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	pause := append(tail, agent.ConversationEntry{
		Role: "tool", Content: `__ASK_USER__{"question":"发送这封邮件？"}`, ToolName: "mailbox_send", ToolCallID: "confirm-1",
	})
	if err := memory.SaveAndCompleteInFlightCheckpointForRun(userID, "run-mail", pause); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{memory: memory}
	slot := memory.GetUnfinishedSlot(userID)
	knownOnly := []agent.ConversationEntry{
		{Role: "tool", Content: notConfigured, ToolName: "mailbox_send", ToolCallID: "new-known"},
	}
	if got := h.releaseSettledMailboxReviewSlot(userID, slot, knownOnly); got == nil || got.SlotID != slot.SlotID || !got.MailboxUncertainDropped {
		t.Fatalf("release cleared the dropped delivery: %#v", got)
	}
}
