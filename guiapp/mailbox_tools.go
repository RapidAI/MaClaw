package guiapp

import (
	"context"
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
	_ = log.Log(security.AuditEntry{
		Timestamp:    time.Now(),
		UserID:       userID,
		ToolName:     "mailbox_send",
		RiskLevel:    security.RiskHigh,
		PolicyAction: security.PolicyAllow,
		Result:       ev.Result,
		Arguments: map[string]interface{}{
			"account_id":      ev.AccountID,
			"recipient_count": ev.RecipientCount,
		},
	})
}

func (h *IMMessageHandler) noteMailboxUncertain(userID string, draft mailbox.Draft) {
	if h == nil || h.memory == nil {
		return
	}
	slotID := "mailbox-" + strings.TrimSpace(draft.ID)
	if strings.TrimSpace(draft.ID) == "" {
		slotID = "mailbox-send-uncertain"
	}
	h.memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID:          slotID,
		UserID:          userID,
		Status:          agent.UnfinishedTaskSlotStatusInterrupted,
		Source:          agent.UnfinishedTaskSlotSourceInFlightLeaseExpired,
		LastToolName:    "mailbox_send",
		SideEffectState: "external_uncertain",
		RecoveryMode:    "requires_review",
		LastTask:        "发送邮件",
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
	if h == nil || h.app == nil {
		return mailbox.MsgNotConfigured
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return "邮箱没有保存"
	}
	next, saved, secrets, msg, err := mailbox.ApplyConfigure(cfg.MailboxAccounts, args, time.Now())
	if err != nil {
		return configureMailboxToolErr(err, stringArg(args, "password"))
	}
	if err := h.app.PatchConfig(func(cfg *corelib.AppConfig) { cfg.MailboxAccounts = next }); err != nil {
		return "邮箱没有保存"
	}
	for _, sec := range secrets {
		if err := h.app.MailboxAccountSetSecret(sec.ID, sec.Side, sec.Secret); err != nil {
			text := err.Error()
			if strings.Contains(text, sec.Secret) {
				text = mailbox.MsgKeyringLong
			}
			return fmt.Sprintf("邮箱「%s」已保存，但%s", saved.Name, text)
		}
	}
	return msg
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
	if strings.HasPrefix(text, "__ASK_USER__") && host.Drafts != nil {
		if draft, ok := host.Drafts.Pending(host.Principal, host.Session); ok {
			h.noteMailboxDraft(userID, draft.ID)
		}
	}
	return text
}

func (h *IMMessageHandler) noteMailboxDraft(userID, draftID string) {
	if h == nil || strings.TrimSpace(draftID) == "" {
		return
	}
	h.mailboxDraftNotes.Store(strings.TrimSpace(userID), strings.TrimSpace(draftID))
}

// takeMailboxDraftNote copies the stashed draft id and clears it so a later
// unrelated ask_user cannot inherit it. The id never enters the question.
func (h *IMMessageHandler) takeMailboxDraftNote(userID string) string {
	if h == nil {
		return ""
	}
	raw, ok := h.mailboxDraftNotes.LoadAndDelete(strings.TrimSpace(userID))
	if !ok {
		return ""
	}
	id, _ := raw.(string)
	return strings.TrimSpace(id)
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
// A confirmation or cancellation spends the send budget before the model runs
// and still resets the query budget. Any other turn gets a fresh budget.
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
	}
	h.mailboxBudgets.Store(userID, budget)
}

func (h *IMMessageHandler) approveMailboxDraft(userID, draftID string) string {
	host := h.mailboxHost(userID)
	if host == nil {
		return mailbox.MsgExpired + "\n不要再次调用 mailbox_send。"
	}
	result, class := host.Approve(context.Background(), draftID)
	switch class {
	case "approved":
		return result + "\n这封邮件已经发送。不要再次调用 mailbox_send。"
	case "uncertain":
		h.noteMailboxUncertain(userID, mailbox.Draft{ID: draftID})
		return result + "\n发送结果还不能确定。不要再次调用 mailbox_send。"
	case "rejected":
		return result + "\n不要再次调用 mailbox_send。"
	default:
		return result + "\n不要再次调用 mailbox_send。"
	}
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
	return result + "\n没有连接邮件服务器。不要再次调用 mailbox_send。"
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
