package guiapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

type statusMailSender struct {
	calls int
	err   error
}

func (s *statusMailSender) Send(context.Context, mailbox.Account, string, mailbox.OutboundMessage) error {
	s.calls++
	return s.err
}

func installMailboxSender(t *testing.T, sender mailbox.MailSender) {
	t.Helper()
	old := mailboxToolSender
	mailboxToolSender = sender
	t.Cleanup(func() { mailboxToolSender = old })
}

func newMailboxToolFixture(t *testing.T, sender mailbox.MailSender) (*IMMessageHandler, string) {
	t.Helper()
	app := newDatabaseProfileTestApp(t)
	installFakeMailboxKeyring(t)
	installMailboxSender(t, sender)
	if _, err := app.MailboxSignatureSave(map[string]interface{}{
		"name": "公共", "body": "此致 {{邮箱名}}", "default": true,
	}); err != nil {
		t.Fatal(err)
	}
	id, err := app.MailboxAccountSave(map[string]interface{}{
		"name": "Work", "address": "w@ex.com",
		"outbound": map[string]interface{}{"host": "127.0.0.1", "security": "none", "from_address": "w@ex.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.MailboxAccountSetSecret(id, "outbound", "smtp-secret"); err != nil {
		t.Fatal(err)
	}
	userID := "mb-" + strings.ReplaceAll(t.Name(), "/", "-")
	t.Cleanup(func() {
		if draft, ok := guiMailboxDrafts.Pending(userID, userID); ok {
			guiMailboxDrafts.Drop(draft.ID)
		}
	})
	return &IMMessageHandler{app: app}, userID
}

func mailboxAskHistory() []agent.ConversationEntry {
	return []agent.ConversationEntry{{Role: "assistant", Content: "请确认发送这封邮件"}}
}

func freezeMailboxSend(t *testing.T, h *IMMessageHandler, userID string) (string, string) {
	t.Helper()
	h.mailboxBudgets.Store(userID, mailbox.NewTurnBudget())
	payload := h.toolMailboxSend(withMailboxOwner(context.Background(), userID), map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "Hi", "text": "body",
	})
	if !strings.HasPrefix(payload, "__ASK_USER__") || strings.Contains(payload, "smtp-secret") || !strings.Contains(payload, "此致 Work") {
		t.Fatalf("payload=%s", payload)
	}
	raw, ok := h.mailboxDraftNotes.Load(userID)
	if !ok {
		t.Fatal("draft note was not stashed")
	}
	draftID := raw.(pendingMailboxDraftNote).DraftID
	if draftID == "" || strings.Contains(payload, draftID) {
		t.Fatalf("draft id leaked id=%q payload=%s", draftID, payload)
	}
	askReq, ok := ParseAskUserResult(payload)
	if !ok || !strings.HasPrefix(strings.TrimSpace(askReq.Context), mailboxDraftAskContext) {
		t.Fatalf("send card lost its binding context: %+v", askReq)
	}
	h.commitPendingAskUser(userID, askReq, mailboxAskHistory(), nil)
	if _, still := h.mailboxDraftNotes.Load(userID); still {
		t.Fatal("commit left the draft note")
	}
	pending, _ := h.pendingAskUser.Load(userID)
	state, _ := pending.(*pendingAskUserState)
	if state == nil || state.MailboxDraftID != draftID {
		t.Fatalf("pending=%+v", state)
	}
	return draftID, payload
}

func TestMailboxSendConfirmUsesHostAndClearsDraftNote(t *testing.T) {
	sender := &captureMailSender{}
	h, userID := newMailboxToolFixture(t, sender)
	freezeMailboxSend(t, h, userID)
	h.prepareMailboxTurn(userID, "确认")
	raw, _ := h.mailboxBudgets.Load(userID)
	budget := raw.(*mailbox.TurnBudget)
	if !budget.SendUsed || budget.QueryLeft != mailbox.QueryBudget {
		t.Fatalf("budget used=%v left=%d", budget.SendUsed, budget.QueryLeft)
	}
	hint, _, ok, _ := h.consumePendingAskUserAnswer(userID, "确认", mailboxAskHistory())
	if !ok || sender.calls != 1 || hint != "邮件已发送" || strings.Contains(hint, "mailbox_send") || strings.Contains(sender.last.Text, "{{") {
		t.Fatalf("hint=%q calls=%d body=%q ok=%v", hint, sender.calls, sender.last.Text, ok)
	}
}

func TestMailboxPrepareSpendsSendOnlyForConfirmation(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "mb-budget"
	history := mailboxAskHistory()
	h.pendingAskUser.Store(userID, &pendingAskUserState{
		Question: "请确认发送这封邮件", MailboxDraftID: "md-budget", Timestamp: time.Now(), History: history,
	})
	h.mailboxDraftNotes.Store(userID, pendingMailboxDraftNote{DraftID: "leftover", Nonce: "n1"})
	h.prepareMailboxTurn(userID, "发送")
	if _, ok := h.mailboxDraftNotes.Load(userID); ok {
		t.Fatal("prepare left a draft note")
	}
	raw, _ := h.mailboxBudgets.Load(userID)
	budget := raw.(*mailbox.TurnBudget)
	if !budget.SendUsed || budget.QueryLeft != mailbox.QueryBudget {
		t.Fatalf("confirm budget used=%v left=%d", budget.SendUsed, budget.QueryLeft)
	}
	h.prepareMailboxTurn(userID, "改一下主题")
	raw, _ = h.mailboxBudgets.Load(userID)
	budget = raw.(*mailbox.TurnBudget)
	if budget.SendUsed || budget.QueryLeft != mailbox.QueryBudget {
		t.Fatalf("other budget used=%v left=%d", budget.SendUsed, budget.QueryLeft)
	}
}

// The stashed draft id may only ride the send's own confirmation card. An
// unrelated committed ask — a rewritten or deferred question — must leave
// the note in place and bind nothing, so a "确认" can never settle a draft
// the shown question did not freeze.
func TestMailboxDraftNoteBindsOnlyToSendConfirmCard(t *testing.T) {
	h := &IMMessageHandler{}
	h.noteMailboxDraft("user-1", "draft-9", "nonce-1")

	h.commitPendingAskUser("user-1", &AskUserRequest{Question: "q", Context: "anything else"}, nil, nil)
	if _, ok := h.mailboxDraftNotes.Load("user-1"); !ok {
		t.Fatal("unrelated question consumed the note")
	}
	if pending, _ := h.pendingAskUser.Load("user-1"); pending.(*pendingAskUserState).MailboxDraftID != "" {
		t.Fatal("unrelated question bound the draft")
	}

	// A forged card echoes the sentinel but cannot name the note's nonce.
	h.commitPendingAskUser("user-1", &AskUserRequest{Question: "q", Context: mailboxDraftAskContext + "forged; mailbox draft send confirmation"}, nil, nil)
	if _, ok := h.mailboxDraftNotes.Load("user-1"); !ok {
		t.Fatal("forged question consumed the note")
	}
	if pending, _ := h.pendingAskUser.Load("user-1"); pending.(*pendingAskUserState).MailboxDraftID != "" {
		t.Fatal("forged question bound the draft")
	}

	h.commitPendingAskUser("user-1", &AskUserRequest{Question: "q", Context: mailboxDraftAskContext + "nonce-1; mailbox draft send confirmation"}, nil, nil)
	if _, ok := h.mailboxDraftNotes.Load("user-1"); ok {
		t.Fatal("own question left the note behind")
	}
	if pending, _ := h.pendingAskUser.Load("user-1"); pending.(*pendingAskUserState).MailboxDraftID != "draft-9" {
		t.Fatal("own question did not bind the draft")
	}
}

func TestMailboxCancelAndOtherTextDoNotSend(t *testing.T) {
	sender := &captureMailSender{}
	h, userID := newMailboxToolFixture(t, sender)
	freezeMailboxSend(t, h, userID)
	h.prepareMailboxTurn(userID, "取消")
	hint, _, ok, settled := h.consumePendingAskUserAnswer(userID, "取消", mailboxAskHistory())
	if !ok || !settled || sender.calls != 0 || hint != mailbox.MsgCancelled || strings.Contains(hint, "mailbox_send") {
		t.Fatalf("cancel hint=%q calls=%d ok=%v", hint, sender.calls, ok)
	}

	_, payload := freezeMailboxSend(t, h, userID)
	h.prepareMailboxTurn(userID, "改一下主题")
	card, ok := ParseAskUserResult(payload)
	if !ok {
		t.Fatal("send card did not parse")
	}
	// The hint echoes the card's real question; the binding sentinel is
	// plumbing and must not reach the model-facing context.
	want := agent.FormatPendingAskUserAnswerHint(card.Question, "改一下主题", "mailbox draft send confirmation")
	hint, _, ok, settled = h.consumePendingAskUserAnswer(userID, "改一下主题", mailboxAskHistory())
	if !ok || settled || hint != want || sender.calls != 0 || strings.Contains(hint, "用户取消了发送") {
		t.Fatalf("other hint=%q want=%q calls=%d", hint, want, sender.calls)
	}
	if _, pending := guiMailboxDrafts.Pending(userID, userID); pending {
		t.Fatal("other text left the draft pending")
	}
}

func TestMailboxInternalCommandDropsDraftWithoutSending(t *testing.T) {
	sender := &captureMailSender{}
	h, userID := newMailboxToolFixture(t, sender)
	freezeMailboxSend(t, h, userID)
	h.prepareMailboxTurn(userID, "__start_new_task__")
	hint, ws, ok, _ := h.consumePendingAskUserAnswer(userID, "__start_new_task__", mailboxAskHistory())
	if ok || hint != "" || ws != nil || sender.calls != 0 {
		t.Fatalf("internal hint=%q ok=%v calls=%d", hint, ok, sender.calls)
	}
	if _, pending := guiMailboxDrafts.Pending(userID, userID); pending {
		t.Fatal("internal command left the draft")
	}
}

func TestMailboxUncertainApproveDoesNotReplay(t *testing.T) {
	sender := &statusMailSender{err: mailbox.ErrUncertain}
	h, userID := newMailboxToolFixture(t, sender)
	h.memory = agent.NewConversationMemory()
	draftID, _ := freezeMailboxSend(t, h, userID)
	h.prepareMailboxTurn(userID, "确认")
	hint, _, ok, _ := h.consumePendingAskUserAnswer(userID, "确认", mailboxAskHistory())
	if !ok || sender.calls != 1 || strings.Contains(hint, "邮件已发送") || hint != mailbox.MsgUncertain || strings.Contains(hint, "mailbox_send") {
		t.Fatalf("hint=%q calls=%d ok=%v", hint, sender.calls, ok)
	}
	slot := h.memory.GetUnfinishedSlot(userID)
	if slot == nil || slot.SlotID != "mailbox-"+draftID || slot.LastToolName != "mailbox_send" || slot.SideEffectState != "external_uncertain" || slot.RecoveryMode != "requires_review" {
		t.Fatalf("slot=%#v", slot)
	}
	trimmed := "继续上次未完成任务"
	entries := []agent.ConversationEntry(nil)
	msg := &IMUserMessage{UserID: userID, Platform: "desktop", Text: trimmed, Lang: "zh-Hans", ResumeSlotID: slot.SlotID}
	handled, resp, stop := h.applyExplicitTaskSlotAction(msg, &trimmed, explicitTaskSlotDecision{ResumeSlotID: slot.SlotID}, &entries, &slot)
	if !handled || !stop || resp == nil || resp.Error != "semantic_external_delivery_review_required" || !strings.Contains(resp.Text, "上一次外部发送的结果还不能确定") || strings.Contains(resp.Text, "weixin") {
		t.Fatalf("handled=%v stop=%v resp=%#v", handled, stop, resp)
	}
	if active := h.memory.ActiveUnfinishedSlot(userID); active != nil {
		t.Fatalf("slot was bound: %#v", active)
	}
}

func TestMailboxRejectedApproveDoesNotUpsertSlot(t *testing.T) {
	sender := &statusMailSender{err: mailbox.ErrRejected}
	h, userID := newMailboxToolFixture(t, sender)
	h.memory = agent.NewConversationMemory()
	freezeMailboxSend(t, h, userID)
	h.prepareMailboxTurn(userID, "确认")
	hint, _, ok, _ := h.consumePendingAskUserAnswer(userID, "确认", mailboxAskHistory())
	if !ok || sender.calls != 1 || hint != mailbox.MsgRejected || strings.Contains(hint, "mailbox_send") {
		t.Fatalf("hint=%q calls=%d ok=%v", hint, sender.calls, ok)
	}
	if slot := h.memory.GetUnfinishedSlot(userID); slot != nil {
		t.Fatalf("rejected upserted %#v", slot)
	}
}

func TestMailboxApproveWritesAuditWithoutBody(t *testing.T) {
	sender := &captureMailSender{}
	h, userID := newMailboxToolFixture(t, sender)
	log, err := NewAuditLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	h.app.auditLog = log
	freezeMailboxSend(t, h, userID)
	h.prepareMailboxTurn(userID, "confirm")
	if _, _, ok, settled := h.consumePendingAskUserAnswer(userID, "confirm", mailboxAskHistory()); !ok || !settled || sender.calls != 1 {
		t.Fatalf("approve calls=%d", sender.calls)
	}
	entries, err := log.Query(security.AuditFilter{ToolName: "mailbox_send"})
	if err != nil || len(entries) != 1 {
		t.Fatalf("audit %+v err=%v", entries, err)
	}
	entry := entries[0]
	if entry.ToolName != "mailbox_send" || entry.RiskLevel != security.RiskHigh || entry.PolicyAction != security.PolicyAllow {
		t.Fatalf("entry=%+v", entry)
	}
	count, ok := entry.Arguments["recipient_count"].(float64)
	if !ok || count != 1 {
		t.Fatalf("arguments=%#v", entry.Arguments)
	}
	accountID, _ := entry.Arguments["account_id"].(string)
	if accountID == "" {
		t.Fatalf("account=%q", accountID)
	}
	if strings.Contains(entry.Result, "smtp-secret") || strings.Contains(entry.Result, "Hi") || strings.Contains(accountID, "smtp-secret") {
		t.Fatalf("audit leaked message content: %+v", entry)
	}
	if _, leaked := entry.Arguments["text"]; leaked {
		t.Fatalf("audit stored the body: %#v", entry.Arguments)
	}
	if _, leaked := entry.Arguments["subject"]; leaked {
		t.Fatalf("audit stored the subject: %#v", entry.Arguments)
	}
}

func TestMailboxConfirmSkipsExecutionClassifierAndLLMGate(t *testing.T) {
	mem := agent.NewConversationMemory()
	t.Cleanup(mem.Stop)
	userID := "mb-preflight"
	mem.Save(userID, mailboxAskHistory())
	loaded := mem.Load(userID)
	h := &IMMessageHandler{
		memory: mem,
		standaloneConfig: &StandaloneConfig{LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{}
		}},
		confirmationStore: newAIConfirmationStore(""),
	}
	h.confirmationStore.set(&pendingConfirmation{
		ID: "plan-1", UserID: userID, Summary: "run the suite", OriginalText: "run the suite", UpdatedAt: time.Now(),
	})
	h.pendingAskUser.Store(userID, &pendingAskUserState{
		Question:       "请确认发送这封邮件",
		History:        loaded,
		Timestamp:      time.Now(),
		MailboxDraftID: "draft-1",
	})
	trimmed := "confirm"
	msg := IMUserMessage{UserID: userID, Platform: "desktop", Text: trimmed, Lang: "zh-Hans"}
	pre := h.prepareIMMessagePreflight(&msg, &trimmed)
	if pre.Handled || pre.ConfirmedResume || trimmed != "confirm" {
		t.Fatalf("handled=%v resume=%v text=%q resp=%#v", pre.Handled, pre.ConfirmedResume, trimmed, pre.Response)
	}
	if h.confirmationStore.get(userID) == nil {
		t.Fatal("execution confirmation was consumed")
	}
	if _, ok := h.pendingAskUser.Load(userID); !ok {
		t.Fatal("mailbox draft was consumed in preflight")
	}

	other := "hello"
	otherMsg := IMUserMessage{UserID: "mb-ordinary", Platform: "desktop", Text: other}
	ordinary := h.prepareIMMessagePreflight(&otherMsg, &other)
	if !ordinary.Handled || ordinary.Response == nil || ordinary.Response.Error == "" {
		t.Fatalf("ordinary=%#v", ordinary.Response)
	}
}

func TestMailboxUserTurnTextUsesButtonLanguage(t *testing.T) {
	if got := mailboxUserTurnText("confirm", ""); got != "确认" {
		t.Fatalf("empty lang=%q", got)
	}
	if got := mailboxUserTurnText("cancel", "en"); got != "Cancel" {
		t.Fatalf("english=%q", got)
	}
	if got := mailboxUserTurnText("改一下主题", ""); got != "改一下主题" {
		t.Fatalf("other=%q", got)
	}
}

func TestMailboxConfirmSettlesWithoutAnotherModelTurn(t *testing.T) {
	sender := &captureMailSender{}
	h, userID := newMailboxToolFixture(t, sender)
	mem := agent.NewConversationMemory()
	t.Cleanup(mem.Stop)
	h.memory = mem
	freezeMailboxSend(t, h, userID)
	history := mailboxAskHistory()
	trimmed := "confirm"
	msg := IMUserMessage{UserID: userID, Platform: "desktop", Text: trimmed, Lang: "zh-Hans"}
	got := h.resolveIMEntryContext(imEntryContextOptions{
		Message:            &msg,
		Trimmed:            &trimmed,
		EntriesBeforeClear: history,
	})
	if !got.Handled || got.Response == nil || sender.calls != 1 {
		t.Fatalf("handled=%v calls=%d resp=%v", got.Handled, sender.calls, got.Response)
	}
	if got.Response.Text != "邮件已发送" || strings.Contains(got.Response.Text, "mailbox_send") {
		t.Fatalf("text=%q", got.Response.Text)
	}
	saved := h.memory.Load(userID)
	if len(saved) != 3 {
		t.Fatalf("history=%d", len(saved))
	}
	userText, _ := saved[1].Content.(string)
	assistantText, _ := saved[2].Content.(string)
	if userText != "确认" || assistantText != "邮件已发送" {
		t.Fatalf("user=%q assistant=%q", userText, assistantText)
	}
}
