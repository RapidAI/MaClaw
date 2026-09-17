package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

// P0-4 (0b-ii) write-tool execution-layer credential gate.
//
// The pre-execution confirmation gate (im_confirmation_gate.go) intercepts a
// task BEFORE the agent loop starts. The credential gate is different: it is
// a mid-loop tool-call fence. When a knowledge write tool is dispatched with
// a payload that matches the authoritative secret scanner
// (corelib/security), the tool call is suspended, a one-shot credential card
// is pushed to the user, and the loop blocks on a result channel until the
// user confirms (button/internal command only), cancels, the 2-minute
// timeout fires, or the loop itself dies. One hook in the tool dispatcher
// covers every path that can reach a write tool (L3 main path, Layer=4 local
// authorization, P0-1 degraded continuation).

// credentialCardTaskType marks a pendingConfirmation as a P0-4 credential
// card. The convergence semantics (no free-text approval, free text voids the
// card) apply ONLY to cards with this TaskType; plan confirmations keep their
// existing behavior.
const credentialCardTaskType = "credential"

// credentialFenceTimeout bounds how long a tool call waits for the user to
// confirm a credential card. Unconfirmed = rejected. A var (not a const) so
// tests can shrink it.
var credentialFenceTimeout = 2 * time.Minute

// credentialGateMaxPreviewRunes caps the redacted payload preview shown on
// the card so a huge paste cannot blow up the chat UI.
const credentialGateMaxPreviewRunes = 800

// credentialGatedKnowledgeWriteTool reports whether a tool call must pass the
// credential fence before touching the knowledge store.
func credentialGatedKnowledgeWriteTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "knowledge_save_text",
		"knowledge_save_url",
		"knowledge_save_urls",
		"knowledge_import_directory",
		"knowledge_import_files",
		"knowledge_import_snapshot",
		"semantic_ingest_trusted_knowledge":
		return true
	default:
		return false
	}
}

func isCredentialConfirmationCard(pending *pendingConfirmation) bool {
	return pending != nil && strings.TrimSpace(pending.TaskType) == credentialCardTaskType
}

// credentialFenceWaiter is one suspended tool call. resultCh has capacity 1
// so resolution never blocks the resolver (preflight runs before the session
// lock; a blocking resolver would deadlock against the fenced loop).
type credentialFenceWaiter struct {
	userID   string
	toolName string
	resultCh chan bool // true = approved; buffered 1
}

// credentialFenceManager tracks the suspended tool calls, keyed by
// confirmation card ID. The manager is process-local: a dead loop's waiter is
// unregistered by the fence itself (ctx cancel / timeout), and a card whose
// waiter is gone is answered with "expired".
type credentialFenceManager struct {
	mu      sync.Mutex
	waiters map[string]*credentialFenceWaiter
}

func newCredentialFenceManager() *credentialFenceManager {
	return &credentialFenceManager{waiters: make(map[string]*credentialFenceWaiter)}
}

func (m *credentialFenceManager) register(id string, w *credentialFenceWaiter) {
	if m == nil || strings.TrimSpace(id) == "" || w == nil {
		return
	}
	m.mu.Lock()
	m.waiters[id] = w
	m.mu.Unlock()
}

func (m *credentialFenceManager) unregister(id string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	delete(m.waiters, id)
	m.mu.Unlock()
}

// resolve delivers an answer to a suspended tool call. It reports false when
// no waiter exists (loop died / timed out) so the caller answers "expired".
func (m *credentialFenceManager) resolve(id string, approved bool) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	w, ok := m.waiters[id]
	if ok {
		delete(m.waiters, id)
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case w.resultCh <- approved:
	default:
	}
	return true
}

// rejectForUser voids every waiter of a user (used when free text arrives
// while a credential card is pending: the card is voided as rejected).
func (m *credentialFenceManager) rejectForUser(userID string) int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	var waiters []*credentialFenceWaiter
	for id, w := range m.waiters {
		if w.userID == userID {
			waiters = append(waiters, w)
			delete(m.waiters, id)
		}
	}
	m.mu.Unlock()
	for _, w := range waiters {
		select {
		case w.resultCh <- false:
		default:
		}
	}
	return len(waiters)
}

// credentialFence returns the handler's fence manager, lazily initialized so
// zero-value IMMessageHandler structs (tests) stay race-free.
func (h *IMMessageHandler) credentialFence() *credentialFenceManager {
	if h == nil {
		return nil
	}
	h.credentialFenceInit.Do(func() {
		h.credentialFenceMgr = newCredentialFenceManager()
	})
	return h.credentialFenceMgr
}

// scanCredentialPayload runs the authoritative secret scanner over the tool
// arguments plus the turn's user text (the "referenced context" — a
// knowledge_save_text payload often references a password stated earlier in
// the turn). Non-secret payloads return nil and pay only regex cost.
func scanCredentialPayload(detector *security.SensitiveDetector, argsJSON, userText string) []security.SensitiveMatch {
	if detector == nil {
		return nil
	}
	var matches []security.SensitiveMatch
	seen := map[string]bool{}
	merge := func(found []security.SensitiveMatch) {
		for _, m := range found {
			key := m.Category + ":" + m.Pattern
			if !seen[key] {
				seen[key] = true
				matches = append(matches, m)
			}
		}
	}
	if strings.TrimSpace(argsJSON) != "" {
		merge(detector.Detect(argsJSON))
	}
	if strings.TrimSpace(userText) != "" {
		merge(detector.Detect(userText))
	}
	return matches
}

// credentialGateBlock is the tool-execution-layer fence. It returns
// blocked=false when the payload is clean (or the fence is unavailable, in
// which case it fails closed with an error result) and blocked=true with an
// error result when the write was rejected/cancelled/timed out.
func (h *IMMessageHandler) credentialGateBlock(execCtx context.Context, policyUserID, toolName, argsJSON, userText string) (proceed bool, result toolExecutionResult) {
	okResult := toolExecutionResult{ToolName: toolName, ToolKind: classifyAgentToolKind(toolName)}
	if h == nil {
		return false, okResult
	}
	detector := security.NewSensitiveDetector()
	matches := scanCredentialPayload(detector, argsJSON, userText)
	if len(matches) == 0 {
		return true, okResult // clean payload: original path, zero added latency
	}
	userID := trustedAuditPrincipalFromContext(execCtx, policyUserID)
	if strings.TrimSpace(userID) == "" {
		userID = "desktop-user"
	}
	lang := h.getWorkflowLang()
	if h.confirmationStore == nil || h.app == nil {
		// No confirmation channel: fail closed rather than write unreviewed
		// credentials into the store.
		log.Printf("[credential-gate] blocked %s for user=%q: no confirmation channel", toolName, userID)
		okResult.Text = "[system rejected] " + i18n.T(i18n.MsgCredentialBlocked, lang)
		okResult.Outcome = toolOutcomeFailed
		okResult.FailureKind = toolFailurePolicyRejected
		return false, okResult
	}
	if occupied := h.confirmationStore.get(userID); occupied != nil {
		// The per-userID single-slot store already holds a pending confirmation
		// (a plan card, or another channel's card). Overwriting it would
		// permanently destroy that card's ResumeText/OriginalText — approving
		// the old card afterwards would answer "expired" and the original
		// task could never resume. Background messages and the
		// providedLoopCtx bypass can reach here without serialization, so the
		// occupied check is the only guard. Fail closed: reject this write and
		// keep the existing card intact.
		log.Printf("[credential-gate] blocked %s for user=%q: confirmation store occupied by card=%s type=%q", toolName, userID, occupied.ID, occupied.TaskType)
		okResult.Text = "[system rejected] " + i18n.T(i18n.MsgCredentialStoreOccupied, lang)
		okResult.Outcome = toolOutcomeFailed
		okResult.FailureKind = toolFailurePolicyRejected
		return false, okResult
	}

	// Build the one-shot card. Only the REDACTED preview is persisted — the
	// confirmation store is written to disk and must never carry raw secrets.
	// ResumeText/OriginalText stay empty so no code path can ConfirmedResume
	// a dead mid-loop tool call.
	now := time.Now()
	redacted := detector.Redact(argsJSON)
	if runeLen(redacted) > credentialGateMaxPreviewRunes {
		redacted = truncateRunes(redacted, credentialGateMaxPreviewRunes) + "…"
	}
	item := &pendingConfirmation{
		ID:           fmt.Sprintf("credential-%d", now.UnixNano()),
		UserID:       userID,
		OriginalText: "",
		ResumeText:   "",
		Summary:      i18n.Tf(i18n.MsgCredentialConfirmSummary, lang, toolName, redacted),
		TaskType:     credentialCardTaskType,
		RiskFlags:    credentialMatchCategories(matches),
		Status:       confirmationStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	h.confirmationStore.set(item)

	fence := h.credentialFence()
	waiter := &credentialFenceWaiter{
		userID:   userID,
		toolName: toolName,
		resultCh: make(chan bool, 1),
	}
	fence.register(item.ID, waiter)

	payload, _ := json.Marshal(AIContinuationEvent{
		RequestID:    fmt.Sprintf("credential-card-%s", item.ID),
		SessionKey:   userID,
		Kind:         "confirmation",
		Text:         i18n.T(i18n.MsgCredentialConfirmTitle, lang),
		Confirmation: buildCredentialCardPayload(item, lang),
		Actions: []IMResponseAction{
			{Label: i18n.T(i18n.MsgExecConfirmBtnConfirm, lang), Command: buildConfirmationActionCommand(confirmationActionConfirm, item.ID), Style: "primary"},
			{Label: i18n.T(i18n.MsgExecConfirmBtnCancel, lang), Command: buildConfirmationActionCommand(confirmationActionCancel, item.ID), Style: "secondary"},
		},
	})
	h.app.emitEvent("ai-continuation", string(payload))
	log.Printf("[credential-gate] suspended %s card=%s user=%q matches=%v", toolName, item.ID, userID, credentialMatchCategories(matches))

	finish := func(approved bool, outcome toolOutcome, failure toolFailureKind, text string) (bool, toolExecutionResult) {
		fence.unregister(item.ID)
		h.confirmationStore.clear(userID)
		res := okResult
		res.Text = text
		res.Outcome = outcome
		res.FailureKind = failure
		return false, res
	}

	timer := time.NewTimer(credentialFenceTimeout)
	defer timer.Stop()
	select {
	case approved := <-waiter.resultCh:
		if approved {
			// User approved via button/internal command: run the original path.
			h.confirmationStore.clear(userID)
			fence.unregister(item.ID)
			log.Printf("[credential-gate] approved %s card=%s user=%q; continuing execution", toolName, item.ID, userID)
			return true, okResult
		}
		return finish(false, toolOutcomeFailed, toolFailurePolicyRejected,
			"[system rejected] "+i18n.T(i18n.MsgCredentialCancelled, lang))
	case <-execCtx.Done():
		// Cancel pierces the fence: the loop dies normally and the pending
		// card is voided (never resumable — the mid-loop call is dead). This
		// is a user/system cancellation, NOT a timeout — use the cancel copy.
		h.app.emitAIContinuationEvent(AIContinuationEvent{
			RequestID:  fmt.Sprintf("credential-card-%s", item.ID),
			SessionKey: userID,
			Kind:       "notice",
			Text:       i18n.T(i18n.MsgCredentialCancelledNotice, lang),
		})
		return finish(false, toolOutcomeFailed, toolFailureHandlerReported,
			"[system rejected] "+i18n.T(i18n.MsgCredentialCancelled, lang))
	case <-timer.C:
		h.app.emitAIContinuationEvent(AIContinuationEvent{
			RequestID:  fmt.Sprintf("credential-card-%s", item.ID),
			SessionKey: userID,
			Kind:       "notice",
			Text:       i18n.T(i18n.MsgCredentialTimeout, lang),
		})
		log.Printf("[credential-gate] timed out %s card=%s user=%q", toolName, item.ID, userID)
		return finish(false, toolOutcomeFailed, toolFailureHandlerReported,
			"[system rejected] "+i18n.T(i18n.MsgCredentialTimeout, lang))
	}
}

func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func credentialMatchCategories(matches []security.SensitiveMatch) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		cat := strings.TrimSpace(m.Category)
		if cat == "" || seen[cat] {
			continue
		}
		seen[cat] = true
		out = append(out, cat)
	}
	return out
}

// buildCredentialCardPayload renders the credential card. It deliberately
// does not use buildConfirmationLabels: a credential card is a one-shot
// approve/deny decision, so revision hints and planned actions stay empty.
func buildCredentialCardPayload(item *pendingConfirmation, lang string) *IMResponseConfirmation {
	if item == nil {
		return nil
	}
	return &IMResponseConfirmation{
		ID:        item.ID,
		Summary:   item.Summary,
		TaskType:  item.TaskType,
		RiskFlags: append([]string(nil), item.RiskFlags...),
		Status:    item.Status.String(),
		Labels:    buildCredentialCardLabels(lang),
	}
}

func buildCredentialCardLabels(lang string) *IMResponseConfirmLabels {
	return &IMResponseConfirmLabels{
		Title:          i18n.T(i18n.MsgCredentialConfirmTitle, lang),
		Status:         i18n.T(i18n.MsgConfirmLabelStatus, lang),
		TargetPaths:    i18n.T(i18n.MsgConfirmLabelTargetPaths, lang),
		PlannedActions: i18n.T(i18n.MsgConfirmLabelPlannedActions, lang),
		RiskFlags:      i18n.T(i18n.MsgConfirmLabelRiskFlags, lang),
		RevisionHints:  i18n.T(i18n.MsgConfirmLabelRevisionHints, lang),
	}
}

// credentialGateSemanticIngest guards the semantic trusted-knowledge ingest
// adapter, which is dispatched through the semantic adapter path
// (executeTrustedKnowledgeIngest) and bypasses the registry tool executor
// where the main credential fence hangs. Everything funnels into
// credentialGateBlock so behavior (card, timeout, cancel) is identical.
func (h *IMMessageHandler) credentialGateSemanticIngest(loopCtx *LoopContext, principalID, userText, text, url, path string) (proceed bool, rejection string) {
	if h == nil {
		return true, ""
	}
	argsJSON, err := json.Marshal(map[string]string{"text": text, "url": url, "path": path})
	if err != nil {
		return true, ""
	}
	execCtx := context.Background()
	cancel := func() {}
	if loopCtx != nil {
		execCtx, cancel = loopCtx.Context()
	}
	defer cancel()
	proceed, result := h.credentialGateBlock(execCtx, strings.TrimSpace(principalID), semanticTrustedKnowledgeIngestAdapter, string(argsJSON), userText)
	if proceed {
		return true, ""
	}
	return false, result.Text
}

// handleCredentialCardAction resolves a user reply that targets a pending
// credential card. Called from handlePendingExecutionConfirmation BEFORE any
// plan-confirmation / workflow / free-text classification runs.
//
// Convergence semantics (credential cards only):
//   - Button/internal command with matching ID: the answer is delivered to
//     the suspended tool call through the fence result channel. No
//     ConfirmedResume: resuming a dead mid-loop tool call would rerun a write
//     without its original context.
//   - Free text (anything else): the card is voided as rejected and the user
//     is told to resend. "好的" can never approve a credential write.
func (h *IMMessageHandler) handleCredentialCardAction(msg *IMUserMessage, trimmed *string, pending *pendingConfirmation, action confirmationAction, confirmationID string, hasConfirmationAction bool) pendingExecutionConfirmationResult {
	if h == nil || msg == nil || pending == nil {
		return pendingExecutionConfirmationResult{}
	}
	lang := h.getWorkflowLang()
	fence := h.credentialFence()

	if hasConfirmationAction {
		if confirmationID != pending.ID {
			// A different (stale) card: leave the current one alone.
			return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: i18n.T(i18n.MsgExecConfirmExpired, lang)}}
		}
		approved := action == confirmationActionConfirm
		if !fence.resolve(pending.ID, approved) {
			// The fenced loop died (cancel/restart/timeout) and its card is
			// void: nothing can be resumed or re-run.
			h.confirmationStore.clear(msg.UserID)
			return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: i18n.T(i18n.MsgExecConfirmExpired, lang)}}
		}
		h.confirmationStore.clear(msg.UserID)
		if approved {
			return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: i18n.T(i18n.MsgCredentialConfirmed, lang)}}
		}
		return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: i18n.T(i18n.MsgCredentialCancelled, lang)}}
	}

	if msg.IsBackground {
		// Background messages never resolve interactive cards.
		return pendingExecutionConfirmationResult{}
	}
	// Free text while a credential card is pending: void the card (counted as
	// rejection) and tell the user to resend. This applies ONLY to credential
	// cards; plan confirmations keep their LLM-classified reply behavior.
	//
	// KNOWN PRODUCT SEMANTIC (deliberate, not a bug): voiding keys on userID,
	// not on channel. When two channels share one userID (e.g. desktop + IM
	// bridge), casual free text on either channel voids the other channel's
	// pending credential card and that message is consumed as the void reply.
	// Single-channel users are unaffected. Scoped per-user convergence was
	// chosen over per-channel tracking to keep the fail-closed guarantee
	// simple; revisit if multi-channel same-user deployments become common.
	fence.rejectForUser(msg.UserID)
	h.confirmationStore.clear(msg.UserID)
	log.Printf("[credential-gate] card %s voided by free text for user=%q", pending.ID, msg.UserID)
	return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: i18n.T(i18n.MsgCredentialVoided, lang)}}
}
