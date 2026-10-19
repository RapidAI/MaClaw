package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

type mailboxOwnerContextKey struct{}

func withMailboxOwner(ctx context.Context, userID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ctx
	}
	return context.WithValue(ctx, mailboxOwnerContextKey{}, userID)
}

func mailboxOwnerFromContext(ctx context.Context) string {
	if ctx != nil {
		if id, ok := ctx.Value(mailboxOwnerContextKey{}).(string); ok {
			if id = strings.TrimSpace(id); id != "" {
				return id
			}
		}
	}
	return trustedAuditPrincipalFromContext(ctx, "")
}

// mailboxIdentity is the loop owner used as both mailbox principal and session.
func (h *IMMessageHandler) mailboxIdentity() string {
	if h == nil {
		return ""
	}
	h.globalLoopMu.RLock()
	ctx := h.currentLoopCtx
	h.globalLoopMu.RUnlock()
	if ctx != nil {
		if id := strings.TrimSpace(ctx.UserID); id != "" {
			return id
		}
	}
	if owner, explicit := h.currentRuntimePolicyOwnerState(); explicit {
		if owner = strings.TrimSpace(owner); owner != "" {
			return owner
		}
	}
	return h.legacyLastUserID()
}

func (h *IMMessageHandler) mailboxBudget(userID string) *mailbox.TurnBudget {
	userID = strings.TrimSpace(userID)
	if h != nil {
		if raw, ok := h.mailboxBudgets.Load(userID); ok {
			if budget, ok := raw.(*mailbox.TurnBudget); ok && budget != nil {
				return budget
			}
		}
	}
	budget := mailbox.NewTurnBudget()
	if h != nil {
		h.mailboxBudgets.Store(userID, budget)
	}
	return budget
}

func (h *IMMessageHandler) mailboxHost(userID string) *mailbox.Host {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = h.mailboxIdentity()
	}
	budget := h.mailboxBudget(userID)
	workingDir := ""
	if h != nil {
		workingDir = h.effectiveWorkingDirForUser(userID)
	}
	var host *mailbox.Host
	if h != nil && h.app != nil {
		host = h.app.MailboxToolHost(userID, userID, workingDir, budget)
	} else {
		host = &mailbox.Host{
			Budget:     budget,
			Drafts:     guiMailboxDrafts,
			Principal:  userID,
			Session:    userID,
			WorkingDir: workingDir,
		}
	}
	if host == nil {
		return nil
	}
	host.Principal = userID
	host.Session = userID
	host.Audit = func(ev mailbox.AuditEvent) {
		h.auditMailboxSend(userID, ev)
	}
	return host
}

func (h *IMMessageHandler) auditMailboxSend(userID string, ev mailbox.AuditEvent) {
	if h == nil || h.app == nil {
		return
	}
	log := h.app.GetAuditLog()
	if log == nil {
		return
	}
	toolName := strings.TrimSpace(ev.Tool)
	if toolName == "" {
		toolName = "mailbox_send"
	}
	arguments := map[string]interface{}{"account_id": ev.AccountID}
	if toolName == "mailbox_delete" {
		arguments["deleted_count"] = ev.DeletedCount
	} else {
		arguments["recipient_count"] = ev.RecipientCount
	}
	_ = log.Log(security.AuditEntry{
		Timestamp:    time.Now(),
		UserID:       userID,
		ToolName:     toolName,
		RiskLevel:    security.RiskHigh,
		PolicyAction: security.PolicyAllow,
		Result:       ev.Result,
		Arguments:    arguments,
	})
}

func (h *IMMessageHandler) noteMailboxUncertain(userID string, draft mailbox.Draft) {
	if h == nil || h.memory == nil {
		return
	}
	toolName := "mailbox_send"
	lastTask := "发送邮件"
	fallback := "mailbox-send-uncertain"
	if draft.Kind == mailbox.DraftKindDelete {
		toolName = "mailbox_delete"
		lastTask = "删除邮件"
		fallback = "mailbox-delete-uncertain"
	}
	slotID := "mailbox-" + strings.TrimSpace(draft.ID)
	if strings.TrimSpace(draft.ID) == "" {
		slotID = fallback
	}
	h.memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID:          slotID,
		UserID:          userID,
		Status:          agent.UnfinishedTaskSlotStatusInterrupted,
		Source:          agent.UnfinishedTaskSlotSourceInFlightLeaseExpired,
		LastToolName:    toolName,
		SideEffectState: "external_uncertain",
		RecoveryMode:    "requires_review",
		LastTask:        lastTask,
	})
}

func (h *IMMessageHandler) toolMailboxQuery(ctx context.Context, args map[string]interface{}) string {
	if ctx == nil {
		ctx = context.Background()
	}
	host := h.mailboxHost(mailboxOwnerFromContext(ctx))
	if host == nil {
		return mailbox.MsgNotConfigured
	}
	return host.Query(ctx, args)
}

func (h *IMMessageHandler) toolMailboxConfigure(ctx context.Context, args map[string]interface{}) string {
	text, err := h.applyMailboxConfigure(ctx, args)
	if err != nil {
		return configureMailboxToolErr(err, stringArg(args, "password"))
	}
	return text
}

// applyMailboxConfigure writes one account. A class error is returned before
// PatchConfig, so a missing server does not become a saved mailbox.
func (h *IMMessageHandler) applyMailboxConfigure(ctx context.Context, args map[string]interface{}) (string, error) {
	_ = ctx
	if h == nil || h.app == nil {
		return "", fmt.Errorf("%s", mailbox.MsgNotConfigured)
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("邮箱没有保存")
	}
	next, saved, secrets, msg, err := mailbox.ApplyConfigure(cfg.MailboxAccounts, args, time.Now())
	if err != nil {
		return "", err
	}
	if err := h.app.PatchConfig(func(cfg *corelib.AppConfig) { cfg.MailboxAccounts = next }); err != nil {
		return "", fmt.Errorf("邮箱没有保存")
	}
	for _, sec := range secrets {
		if err := h.app.MailboxAccountSetSecret(sec.ID, sec.Side, sec.Secret); err != nil {
			text := err.Error()
			if strings.Contains(text, sec.Secret) {
				text = mailbox.MsgKeyringLong
			}
			// The account row is already committed. The warning is the result
			// of that write, not a rejection that would invite another save.
			return fmt.Sprintf("邮箱「%s」已保存，但%s", saved.Name, text), nil
		}
	}
	return msg, nil
}

func configureMailboxToolErr(err error, secret string) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if secret != "" && strings.Contains(text, secret) {
		return mailbox.MsgKeyring
	}
	return text
}

func (h *IMMessageHandler) toolMailboxDelete(ctx context.Context, args map[string]interface{}) string {
	if ctx == nil {
		ctx = context.Background()
	}
	userID := mailboxOwnerFromContext(ctx)
	host := h.mailboxHost(userID)
	if host == nil {
		return mailbox.MsgNotConfigured
	}
	text := host.Delete(ctx, args)
	return h.bindFrozenMailboxDraft(userID, host, text)
}

func (h *IMMessageHandler) toolMailboxSend(ctx context.Context, args map[string]interface{}) string {
	if ctx == nil {
		ctx = context.Background()
	}
	userID := mailboxOwnerFromContext(ctx)
	host := h.mailboxHost(userID)
	if host == nil {
		return mailbox.MsgNotConfigured
	}
	text := host.Send(ctx, args)
	return h.bindFrozenMailboxDraft(userID, host, text)
}

// bindFrozenMailboxDraft stamps the confirmation card so only that question
// can settle the draft. A send and a delete share the same binding.
func (h *IMMessageHandler) bindFrozenMailboxDraft(userID string, host *mailbox.Host, text string) string {
	if h == nil || host == nil || !strings.HasPrefix(text, "__ASK_USER__") || host.Drafts == nil {
		return text
	}
	draft, ok := host.Drafts.Pending(host.Principal, host.Session)
	if !ok {
		return text
	}
	// Stash the note only for a card that carries the binding marker: an
	// unparseable or hand-rolled card stays unbound, so the draft it froze
	// can never be claimed by a different question.
	bound, nonce, ok := bindMailboxDraftAskContext(text)
	if !ok {
		return text
	}
	h.noteMailboxDraft(userID, draft.ID, nonce)
	return bound
}

// mailboxDraftAskContext is the sentinel prefix stamped onto the
// confirmation card a frozen send emits. commitPendingAskUser binds a
// stashed draft note only to a pendingAskUserState carrying this marker, so
// a deferred or rewritten ask — or any unrelated later question — cannot
// inherit the draft id and have the user's "确认" settle a send it never
// described. The draft id itself stays out of the payload.
const mailboxDraftAskContext = "mailbox-send-confirm;"

// pendingMailboxDraftNote stashes the frozen draft id together with the
// nonce stamped into the emitted card's context. The nonce is the binding:
// only the committed question carrying this exact nonce may consume the
// note, and a forged card echoing the sentinel cannot name it.
type pendingMailboxDraftNote struct {
	DraftID string
	Nonce   string
}

// bindMailboxDraftAskContext prepends the send-confirmation marker plus a
// fresh nonce to the ask_user card's context and returns the bound text and
// the nonce. ok is false when the card is not a parseable __ASK_USER__
// payload; the caller then leaves the draft unbound (fail closed — the
// frozen draft is never sent, it simply stays pending).
func bindMailboxDraftAskContext(text string) (string, string, bool) {
	if !strings.HasPrefix(text, "__ASK_USER__") {
		return "", "", false
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(text, "__ASK_USER__")), &payload); err != nil {
		return "", "", false
	}
	nonce := newConfirmNonce()
	ctx, _ := payload["context"].(string)
	if ctx = strings.TrimSpace(ctx); ctx != "" {
		payload["context"] = mailboxDraftAskContext + nonce + "; " + ctx
	} else {
		payload["context"] = mailboxDraftAskContext + nonce + "; mailbox draft send confirmation"
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", "", false
	}
	return "__ASK_USER__" + string(data), nonce, true
}

func (h *IMMessageHandler) noteMailboxDraft(userID, draftID, nonce string) {
	if h == nil || strings.TrimSpace(draftID) == "" || nonce == "" {
		return
	}
	h.mailboxDraftNotes.Store(strings.TrimSpace(userID), pendingMailboxDraftNote{
		DraftID: strings.TrimSpace(draftID),
		Nonce:   nonce,
	})
}

// takeMailboxDraftNoteFor binds a stashed draft id to the ask_user question
// being committed, but only when that question repeats the nonce the send
// stamped into its own card. Any other committed ask — a rewritten or
// deferred card, an unrelated model question, a tool result echoing the
// sentinel — leaves the note in place (prepareMailboxTurn clears it on the
// next user turn), so a confirmation can never settle a draft the shown
// question did not freeze. The id never enters the question.
func (h *IMMessageHandler) takeMailboxDraftNoteFor(userID string, askReq *AskUserRequest) string {
	if h == nil || askReq == nil {
		return ""
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(askReq.Context), mailboxDraftAskContext)
	if !ok {
		return ""
	}
	questionNonce, _, _ := strings.Cut(rest, ";")
	if questionNonce == "" {
		return ""
	}
	raw, ok := h.mailboxDraftNotes.Load(strings.TrimSpace(userID))
	if !ok {
		return ""
	}
	note, ok := raw.(pendingMailboxDraftNote)
	if !ok || note.Nonce != questionNonce {
		return ""
	}
	// Consume atomically: a racing commit that already swapped or deleted
	// the note fails the compare, and this question binds nothing.
	if !h.mailboxDraftNotes.CompareAndDelete(strings.TrimSpace(userID), raw) {
		return ""
	}
	return strings.TrimSpace(note.DraftID)
}

func (h *IMMessageHandler) dropMailboxDraft(userID, draftID string) {
	if h == nil || strings.TrimSpace(draftID) == "" {
		return
	}
	if guiMailboxDrafts != nil {
		guiMailboxDrafts.Drop(strings.TrimSpace(draftID))
	}
	h.mailboxDraftNotes.Delete(strings.TrimSpace(userID))
}

// freshMailboxDraftID is a frozen send waiting on this history. Only an
// approve or cancel click settles it; other text stays on the normal ask path.
func (h *IMMessageHandler) freshMailboxDraftID(userID, text string, entries []agent.ConversationEntry) string {
	if h == nil || (!mailbox.IsApproveText(text) && !mailbox.IsCancelText(text)) {
		return ""
	}
	raw, ok := h.pendingAskUser.Load(strings.TrimSpace(userID))
	if !ok {
		return ""
	}
	pending, fresh := pendingAskUserForCurrentHistory(raw, entries)
	if !fresh || pending == nil {
		return ""
	}
	return strings.TrimSpace(pending.MailboxDraftID)
}

func (h *IMMessageHandler) peekMailboxDraftID(userID string) string {
	if h == nil {
		return ""
	}
	raw, ok := h.pendingAskUser.Load(strings.TrimSpace(userID))
	if !ok {
		return ""
	}
	pending, ok := raw.(*pendingAskUserState)
	if !ok || pending == nil {
		return ""
	}
	return strings.TrimSpace(pending.MailboxDraftID)
}

// prepareMailboxTurn runs before the pending ask_user answer is consumed.
// A confirmation or cancellation spends the send and delete budgets before
// the model runs and still resets the query budget. Any other turn gets a fresh budget.
// The handler note is cleared here so a failed commit cannot leak onto a
// later unrelated question.
func (h *IMMessageHandler) prepareMailboxTurn(userID, text string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	h.mailboxDraftNotes.Delete(userID)
	budget := mailbox.NewTurnBudget()
	if id := h.peekMailboxDraftID(userID); id != "" && (mailbox.IsApproveText(text) || mailbox.IsCancelText(text)) {
		budget.SendUsed = true
		budget.DeleteUsed = true
	}
	h.mailboxBudgets.Store(userID, budget)
}

func mailboxDraftKind(host *mailbox.Host, draftID string) string {
	if host == nil || host.Drafts == nil {
		return ""
	}
	draft, ok := host.Drafts.Peek(strings.TrimSpace(draftID))
	if !ok {
		return ""
	}
	return draft.Kind
}

func (h *IMMessageHandler) approveMailboxDraft(userID, draftID string) string {
	host := h.mailboxHost(userID)
	if host == nil {
		return mailbox.MsgExpired
	}
	kind := mailboxDraftKind(host, draftID)
	result, class := host.Approve(context.Background(), draftID)
	if class == "uncertain" {
		h.noteMailboxUncertain(userID, mailbox.Draft{ID: draftID, Kind: kind})
	}
	return result
}

func (h *IMMessageHandler) cancelMailboxDraft(userID, draftID string) string {
	host := h.mailboxHost(userID)
	result := mailbox.MsgCancelled
	if host != nil {
		result, _ = host.Cancel(draftID)
	} else if guiMailboxDrafts != nil {
		guiMailboxDrafts.Drop(draftID)
	}
	h.mailboxDraftNotes.Delete(strings.TrimSpace(userID))
	return result
}

// mailboxUserTurnText is the chat text for a confirm or cancel button.
// The command stays confirm/cancel so approval still matches; the transcript
// uses the same label the button showed.
func mailboxUserTurnText(text, lang string) string {
	// The button labels follow the UI default: an empty language is Chinese.
	if strings.TrimSpace(lang) == "" {
		lang = string(appLanguageZhHans)
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "confirm":
		return unfinishedSlotText(lang, "Confirm", "确认", "確認")
	case "cancel":
		return unfinishedSlotText(lang, "Cancel", "取消", "取消")
	default:
		return text
	}
}

// settleFreshMailboxConfirmation approves or cancels a frozen draft before
// workflow routing and reply classification. The host already dials or drops
// the draft here, so this turn does not start a model.
func (h *IMMessageHandler) settleFreshMailboxConfirmation(msg *IMUserMessage, trimmed string, entries []agent.ConversationEntry) *IMAgentResponse {
	if msg == nil {
		return nil
	}
	draftID := h.freshMailboxDraftID(msg.UserID, trimmed, entries)
	if draftID == "" {
		return nil
	}
	kind := mailboxDraftKind(h.mailboxHost(msg.UserID), draftID)
	h.prepareMailboxTurn(msg.UserID, trimmed)
	hint, _, _, settled := h.consumePendingAskUserAnswer(msg.UserID, trimmed, entries)
	if !settled {
		return nil
	}
	text := strings.TrimSpace(hint)
	history := append(cloneConversationEntries(entries),
		agent.ConversationEntry{Role: "user", Content: mailboxUserTurnText(trimmed, msg.Lang)},
		agent.ConversationEntry{Role: "assistant", Content: text},
	)
	source := "mailbox_send"
	if kind == mailbox.DraftKindDelete {
		source = "mailbox_delete"
	}
	resp := &IMAgentResponse{Text: text, ResponseSource: source}
	h.saveConversationHistoryTimed(msg.UserID, history, resp)
	return resp
}

// finishMailboxAskUser approves, cancels, or drops a frozen draft. Other text
// is dropped without a cancellation message and without SMTP. handled is true
// only when the hint replaces the ordinary ask_user answer.
func (h *IMMessageHandler) finishMailboxAskUser(userID, text, draftID string) (string, bool) {
	draftID = strings.TrimSpace(draftID)
	if draftID == "" {
		return "", false
	}
	if mailbox.IsApproveText(text) {
		return h.approveMailboxDraft(userID, draftID), true
	}
	if mailbox.IsCancelText(text) {
		return h.cancelMailboxDraft(userID, draftID), true
	}
	h.dropMailboxDraft(userID, draftID)
	return "", false
}
