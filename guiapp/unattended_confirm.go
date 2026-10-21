package guiapp

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
)

// unattendedWorkflowPlatform is the continuation turn that advances a custom
// workflow after an unattended confirmation. It is not a person typing.
const unattendedWorkflowPlatform = "unattended-workflow"

const unattendedWorkflowContinuationCap = 12

var (
	unattendedWorkflowContinuations  sync.Map
	unattendedWorkflowContinuationMu sync.Mutex
)

// unattendedMailboxDraftGone is returned when a scheduled send or delete
// cannot find the draft it just froze. It is not an ask card, so the task
// finishes instead of waiting for a click.
const unattendedMailboxDraftGone = "[system rejected] 定时任务没能确认这封邮件，草稿已经不在。"

// unattendedRecordAudioRejected is returned when a scheduled run tries to
// open the desktop recorder. Nobody is present to stop the recording.
const unattendedRecordAudioRejected = "[system rejected] 定时任务无法录音，这一步没有执行。请改用已有的录音文件，或在有人在场时再录。"

// mailboxLoopAutoConfirms reports whether this loop has nobody present to
// click a confirmation card. Scheduler turns and other background loops are
// included. Ordinary chat is not.
func mailboxLoopAutoConfirms(ctx *LoopContext) bool {
	if ctx == nil {
		return false
	}
	if ctx.Kind == LoopKindBackground {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(ctx.Platform)) {
	case "scheduler", unattendedWorkflowPlatform:
		return true
	default:
		return false
	}
}

// unattendedTurn is a scheduled or run-window execution. Interactive chat on
// the same process stays false, so a person still has to confirm.
func (h *IMMessageHandler) unattendedTurn(userID string) bool {
	if h == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	if mailboxLoopAutoConfirms(h.getSessionLoopCtx(userID)) {
		return true
	}
	h.globalLoopMu.RLock()
	current := h.currentLoopCtx
	h.globalLoopMu.RUnlock()
	if current != nil && strings.TrimSpace(current.UserID) == userID && mailboxLoopAutoConfirms(current) {
		return true
	}
	if h.app == nil {
		return false
	}
	h.app.runWindowManagerMu.Lock()
	manager := h.app.runWindowManager
	h.app.runWindowManagerMu.Unlock()
	return manager != nil && manager.tracksRunningSession(userID)
}

// finishMailboxToolCall binds a frozen send or delete. An unattended turn
// approves that draft immediately so the mail is sent or removed and the
// scheduled task can finish. A person in chat still receives the card.
func (h *IMMessageHandler) finishMailboxToolCall(ctx context.Context, userID string, host *mailbox.Host, text string) string {
	bound := h.bindFrozenMailboxDraft(userID, host, text)
	if h == nil || !strings.HasPrefix(bound, "__ASK_USER__") || !h.unattendedTurn(userID) {
		return bound
	}
	return h.autoApproveBoundMailboxDraft(ctx, userID, host)
}

// autoApproveBoundMailboxDraft approves only the draft this call just froze.
// The note is consumed first, so a later "确认" cannot send it again. Pending
// is used only when no note was stored. A note that lost the race, or that
// names no draft, must not approve some other pending message.
func (h *IMMessageHandler) autoApproveBoundMailboxDraft(ctx context.Context, userID string, host *mailbox.Host) string {
	if h == nil {
		return unattendedMailboxDraftGone
	}
	userID = strings.TrimSpace(userID)
	draftID := ""
	if raw, ok := h.mailboxDraftNotes.Load(userID); ok {
		if !h.mailboxDraftNotes.CompareAndDelete(userID, raw) {
			log.Printf("[mailbox] auto-confirm skipped: draft note changed user=%s", userID)
			return unattendedMailboxDraftGone
		}
		note, ok := raw.(pendingMailboxDraftNote)
		if !ok {
			log.Printf("[mailbox] auto-confirm skipped: draft note type user=%s", userID)
			return unattendedMailboxDraftGone
		}
		draftID = strings.TrimSpace(note.DraftID)
		if draftID == "" {
			log.Printf("[mailbox] auto-confirm skipped: empty draft note user=%s", userID)
			return unattendedMailboxDraftGone
		}
	} else if host != nil && host.Drafts != nil {
		if draft, ok := host.Drafts.Pending(host.Principal, host.Session); ok {
			draftID = strings.TrimSpace(draft.ID)
		}
	}
	if draftID == "" {
		log.Printf("[mailbox] auto-confirm skipped: no draft for user=%s", userID)
		return unattendedMailboxDraftGone
	}
	log.Printf("[mailbox] auto-confirm scheduled draft user=%s kind=%s", userID, mailboxDraftKind(host, draftID))
	return h.approveMailboxDraftCtx(ctx, userID, draftID)
}

// resolveUnattendedAsk turns a confirmation card into a completed result when
// nobody is present. Secret prompts cannot be invented, so they fail and the
// task continues. Confirm and choice cards take the affirmative option.
func (h *IMMessageHandler) resolveUnattendedAsk(userID, text string) (string, bool) {
	if h == nil || !strings.HasPrefix(text, "__ASK_USER__") || !h.unattendedTurn(userID) {
		return "", false
	}
	req, ok := agent.ParseAskUserResult(text)
	if !ok || req == nil {
		return "ask_user is unavailable in scheduled tasks; choose the next action directly and continue.", true
	}
	// Context is model-controlled. Mailbox send and MCP grants are approved
	// only by the tool that just froze them, never from this card. A browser
	// task id is not an approval; it is how the paused task_run continues.
	switch strings.TrimSpace(req.InputType) {
	case "secret", "secret_fill":
		return "[system rejected] 定时任务无法填写密码或密钥，这一步没有执行。请改用已保存的凭据，或在创建任务时写好。", true
	case "confirm":
		return withUnattendedResume("已确认。这是无人值守的定时任务，按确认继续执行，不要再向用户确认。", req.Context), true
	}
	// A later "确认" must not hide the first real choice.
	if pick := firstContinuingOption(req.Options); pick != "" {
		return withUnattendedResume("已选择「"+pick+"」。这是无人值守的定时任务，按这个选择继续，不要再询问。", req.Context), true
	}
	return withUnattendedResume("ask_user is unavailable in scheduled tasks; choose the next action directly and continue.", req.Context), true
}

// withUnattendedResume keeps a paused browser task_run moving. The id is
// echoed back to the model; it does not approve mail, grants, or secrets.
func withUnattendedResume(text, askContext string) string {
	id := unattendedResumeTaskID(askContext)
	if id == "" {
		return text
	}
	return text + " 继续暂停的浏览器任务，调用 task_run 并带上 resume_task_id=" + id + "。"
}

func unattendedResumeTaskID(askContext string) string {
	const key = "resume_task_id="
	rest := askContext
	for {
		i := strings.Index(rest, key)
		if i < 0 {
			return ""
		}
		token := rest[i+len(key):]
		if cut := strings.IndexAny(token, " \t\r\n;,"); cut >= 0 {
			token = token[:cut]
		}
		token = strings.TrimSpace(token)
		if unattendedResumeToken(token) {
			return token
		}
		rest = rest[i+len(key):]
	}
}

func unattendedResumeToken(token string) bool {
	if token == "" || len(token) > 80 {
		return false
	}
	for _, r := range token {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// resetUnattendedWorkflowContinuations starts the continuation budget over.
// A new workflow must not inherit the previous run's cap.
func resetUnattendedWorkflowContinuations(userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	unattendedWorkflowContinuationMu.Lock()
	unattendedWorkflowContinuations.Delete(userID)
	unattendedWorkflowContinuationMu.Unlock()
}

// unattendedResolvedAsk accepts a completed unattended answer. A value that
// is still an ask marker is rejected so the loop does not pause.
func unattendedResolvedAsk(resolved string, ok bool) (string, bool) {
	if !ok || strings.HasPrefix(resolved, "__ASK_USER__") {
		return "", false
	}
	return resolved, true
}

func firstContinuingOption(options []string) string {
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" || mailbox.IsCancelText(option) {
			continue
		}
		return option
	}
	return ""
}
