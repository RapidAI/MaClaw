package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// BR-20261120-06EAE6635C0F — 从机制上修复不能继续的原因.
//
// An uncertain external send leaves a recovery record that stops the same send
// from being replayed. The turn guard treated EVERY turn in that session as a
// continuation of it, so an unrelated follow-up ("模型有多大？") was answered with
// "上一次外部发送的结果还不能确定。这一轮不会再次投递" and the only way out was pressing
// 开始新任务. The record must stop the replay, not the conversation.

// unreviewedSendSlotForTest is the record an uncertain external send leaves:
// recovery mode requires_review, no runtime task, and an external-send tool
// whose result is unknown.
func unreviewedSendSlotForTest(userID string) *agent.UnfinishedTaskSlot {
	return &agent.UnfinishedTaskSlot{
		SlotID:          "slot-uncertain-send",
		UserID:          userID,
		Status:          agent.UnfinishedTaskSlotStatusInterrupted,
		LastTask:        "把报告发到邮箱",
		LastToolName:    "mailbox_send",
		SideEffectState: "external_uncertain",
		RecoveryMode:    "requires_review",
	}
}

func TestUnfinishedSlotNewRequestIsNotAContinuation(t *testing.T) {
	h, userID := newMailboxToolFixture(t, &statusMailSender{})
	h.memory = agent.NewConversationMemory()
	defer h.memory.Stop()

	slot := unreviewedSendSlotForTest(userID)
	h.memory.UpsertUnfinishedSlot(userID, slot)
	h.memory.BindUnfinishedSlot(userID, slot.SlotID)
	active := h.memory.ActiveUnfinishedSlot(userID)
	if active == nil {
		t.Fatal("the uncertain send left no recovery record")
	}
	if !unreviewedExternalDeliverySlot(active) {
		t.Fatalf("record is not an unreviewed send: %#v", active)
	}

	// A brand-new question is not a continuation: it must run. Naming a
	// different thing to send is a new request, not that uncertain send.
	for _, question := range []string{"模型有多大？", "推荐一首歌", "帮我查一下今天的天气", "帮我把这首歌发出去", "发送周报"} {
		if turnContinuesTheUnreviewedSend(question, active) {
			t.Fatalf("%q was treated as a continuation of the uncertain send", question)
		}
	}

	// The person's own continuation of that task still is, and stays refused.
	// Anaphoric send-completion names no new task, so it would deliver again.
	for _, continuation := range []string{"继续", "继续上次未完成任务", "接着做", "continue", "继续发到微信", "把上次没发完的发出去", "把报告发出去", "发出去"} {
		if !turnContinuesTheUnreviewedSend(continuation, active) {
			t.Fatalf("%q must still count as continuing the uncertain send", continuation)
		}
	}
}

// Dropping the record after the person moved on must leave the session usable
// and must not send anything: the uncertain send is never retried.
func TestUnfinishedSlotDropFreesTheSession(t *testing.T) {
	h, userID := newMailboxToolFixture(t, &statusMailSender{})
	h.memory = agent.NewConversationMemory()
	defer h.memory.Stop()

	fresh := unreviewedSendSlotForTest(userID)
	h.memory.UpsertUnfinishedSlot(userID, fresh)
	h.memory.BindUnfinishedSlot(userID, fresh.SlotID)
	active := h.memory.ActiveUnfinishedSlot(userID)
	if active == nil {
		t.Fatal("the uncertain send left no recovery record")
	}

	h.dropUnreviewedSendSlot(userID, active)

	if again := h.memory.ActiveUnfinishedSlot(userID); again != nil {
		t.Fatalf("record still active after the person moved on: %#v", again)
	}
}

// A nil record is not an unreviewed send, and this guard never claims an
// ordinary question as a continuation.
func TestContinuationGuardIgnoresASessionWithoutRecord(t *testing.T) {
	if unreviewedExternalDeliverySlot(nil) {
		t.Fatal("a nil record must not read as an unreviewed send")
	}
	if turnContinuesTheUnreviewedSend("模型有多大？", nil) {
		t.Fatal("an ordinary question counted as a continuation")
	}
}

// Asking to finish that send is the turn that would deliver it again.
// The record stays, and nothing is sent.
func TestUnfinishedSlotAnaphoricSendKeepsTheRecord(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	userID := "user-anaphora"
	slot := unreviewedSendSlotForTest(userID)
	memory.UpsertUnfinishedSlot(userID, slot)
	memory.BindUnfinishedSlot(userID, slot.SlotID)
	h := &IMMessageHandler{memory: memory}
	trimmed := "把上次没发完的发出去"
	msg := &IMUserMessage{UserID: userID, Platform: "desktop", Lang: "zh", Text: trimmed}
	result := h.resolveIMEntryContext(imEntryContextOptions{
		Message:        msg,
		Trimmed:        &trimmed,
		UnfinishedSlot: memory.GetUnfinishedSlot(userID),
	})
	if !result.Handled || result.Response == nil || result.Response.Error != "semantic_external_delivery_review_required" {
		t.Fatalf("handled=%v resp=%#v", result.Handled, result.Response)
	}
	if got := memory.GetUnfinishedSlot(userID); got == nil || got.LastToolName != "mailbox_send" {
		t.Fatalf("anaphoric send completion retired the uncertain send: %#v", got)
	}
}
