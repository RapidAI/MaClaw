package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

// criticalRiskConfirmResponse is sent on the response channel when the user
// answers the confirmation prompt.
type criticalRiskConfirmResponse struct {
	Confirmed bool
}

// pendingCriticalConfirmEntry holds the state for a single pending confirmation.
//
// LIFECYCLE: The entry is owned by a dedicated cleanup goroutine that fires
// after confirmTimeout. This decouples the confirmation lifecycle from the
// caller's context/goroutine — the key design invariant.
//
// Three actors interact with the entry:
//   - confirmCriticalRiskSkill (creator): stores entry, blocks on Ch
//   - ResolveCriticalConfirm (user click): sends response on Ch, deletes entry
//   - cleanup goroutine (timeout): closes Ch, deletes entry
//
// The resolved flag (atomic CAS) ensures exactly one actor wins the race
// to operate on the channel. The loser returns early without touching Ch.
type pendingCriticalConfirmEntry struct {
	Ch       chan criticalRiskConfirmResponse
	resolved int32 // 0 = pending, 1 = resolved; atomic CAS guards channel ops
}

// tryResolve atomically transitions the entry from pending to resolved.
// Returns true if this caller won the race (and should operate on Ch).
// Returns false if another actor already resolved the entry.
func (e *pendingCriticalConfirmEntry) tryResolve() bool {
	return atomic.CompareAndSwapInt32(&e.resolved, 0, 1)
}

const confirmTimeout = 120 * time.Second

// buildSkillRiskPrompt formats a confirmation prompt for a risky skill
// installation. It is shared by IM-driven and manual desktop installs.
func buildSkillRiskPrompt(skillName, source string, level security.RiskLevel, factors []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Security warning: Skill %q from %s was assessed as %s risk.\n", skillName, source, level)
	if len(factors) > 0 {
		sb.WriteString("\nRisk factors:\n")
		for _, f := range factors {
			fmt.Fprintf(&sb, "  - %s\n", f)
		}
	}
	sb.WriteString("\nDo you want to allow this skill installation?\n")
	return sb.String()
}

// buildCriticalRiskPrompt formats a confirmation prompt for a Critical-risk
// skill installation.
func buildCriticalRiskPrompt(skillName, source string, factors []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "⚠️ 安全警告: Skill「%s」来自 %s 被评估为 Critical 风险。\n", skillName, source)
	if len(factors) > 0 {
		sb.WriteString("\n风险因素:\n")
		for _, f := range factors {
			fmt.Fprintf(&sb, "  • %s\n", f)
		}
	}
	sb.WriteString("\n确认安装此 Skill？\n")
	return sb.String()
}

// confirmCriticalRiskSkill presents a blocking confirmation prompt to the user
// when a skill is assessed as RiskCritical. Returns true if the user confirms,
// false on rejection, timeout, or any error condition (fail-closed).
//
// LIFECYCLE DESIGN:
// The confirmation channel's lifetime is decoupled from the caller's context.
// A single cleanup goroutine owns the timeout: after confirmTimeout it closes
// the channel and removes the entry. The caller's context cancellation causes
// this function to return false, but does NOT remove the channel — the user
// can still click the button within the confirmTimeout window.
//
// CONCURRENCY: Two actors race to resolve the entry — the cleanup goroutine
// (timeout) and ResolveCriticalConfirm (user click). atomic CAS on
// entry.resolved ensures exactly one wins. The loser never touches the channel.
func (h *IMMessageHandler) confirmCriticalRiskSkill(
	ctx context.Context,
	skillName, source string,
	factors []string,
	platform string,
	userID string,
) bool {
	return h.confirmRiskSkillInstall(ctx, skillName, source, security.RiskCritical, factors, platform, userID)
}

func (h *IMMessageHandler) confirmRiskSkillInstall(
	ctx context.Context,
	skillName, source string,
	level security.RiskLevel,
	factors []string,
	platform string,
	userID string,
) bool {
	if platform == "" {
		log.Printf("[critical-confirm] fail-closed: empty platform for skill %q", skillName)
		return false
	}

	confirmID := fmt.Sprintf("crit_%d", time.Now().UnixNano())
	promptText := buildSkillRiskPrompt(skillName, source, level, factors)

	entry := &pendingCriticalConfirmEntry{
		Ch: make(chan criticalRiskConfirmResponse, 1),
	}
	h.pendingCriticalConfirm.Store(confirmID, entry)

	// Single cleanup goroutine — the sole timeout owner.
	// After confirmTimeout it tries to win the resolve race. If it wins,
	// it closes the channel (unblocking the select below) and removes the entry.
	// If ResolveCriticalConfirm already won, this is a no-op.
	go func() {
		time.Sleep(confirmTimeout)
		if entry.tryResolve() {
			h.pendingCriticalConfirm.Delete(confirmID)
			close(entry.Ch)
			log.Printf("[critical-confirm] cleanup: confirmID=%s expired after %v", confirmID, confirmTimeout)
		}
	}()

	// Dispatch confirmation UI.
	switch {
	case normalizeIMMessagePlatformKind(platform).IsDesktop():
		if h.app != nil {
			payload := map[string]interface{}{
				"confirm_id": confirmID,
				"summary":    promptText,
				"actions": []map[string]string{
					{"label": "✅ 确认安装", "command": "confirm"},
					{"label": "❌ 拒绝安装", "command": "reject"},
				},
			}
			h.app.emitEvent("critical-risk-confirm", payload)
			log.Printf("[critical-confirm] desktop event emitted confirm_id=%s skill=%q", confirmID, skillName)
		} else {
			log.Printf("[critical-confirm] fail-closed: app is nil for desktop platform, skill %q", skillName)
			return false
		}

	default:
		displayText := FormatAskUserForDisplay(&AskUserRequest{
			Question:  promptText,
			Options:   []string{"确认安装", "拒绝安装"},
			InputType: askUserInputConfirm.String(),
		})
		if h.app != nil {
			imConfirmKey := platform + ":" + userID
			h.pendingCriticalConfirmIM.Store(imConfirmKey, confirmID)
			hubClient := h.app.hubClient()
			if hubClient != nil {
				if err := hubClient.SendIMProactiveMessage(displayText); err != nil {
					log.Printf("[critical-confirm] failed to send proactive message for confirm_id=%s: %v", confirmID, err)
				}
			}
			log.Printf("[critical-confirm] IM proactive message sent confirm_id=%s platform=%s skill=%q", confirmID, platform, skillName)
		} else {
			log.Printf("[critical-confirm] fail-closed: app is nil for IM platform %s, skill %q", platform, skillName)
			return false
		}
	}

	// Block until: user responds, channel closed (timeout), or caller cancelled.
	select {
	case resp, ok := <-entry.Ch:
		if !ok {
			// Channel closed by cleanup goroutine — timeout.
			log.Printf("[critical-confirm] timeout confirm_id=%s skill=%q", confirmID, skillName)
			return false
		}
		log.Printf("[critical-confirm] received response confirm_id=%s confirmed=%v", confirmID, resp.Confirmed)
		return resp.Confirmed

	case <-ctx.Done():
		// Caller's context cancelled — return false but do NOT clean up.
		// The cleanup goroutine owns the entry's lifetime.
		log.Printf("[critical-confirm] context cancelled confirm_id=%s skill=%q — channel stays alive for user", confirmID, skillName)
		return false
	}
}

// ResolveCriticalConfirm is called by the frontend or IM gateway when the user
// responds to a critical-risk confirmation prompt.
// Returns an error if the confirmation has expired or was already resolved.
func (h *IMMessageHandler) ResolveCriticalConfirm(confirmID string, confirmed bool) error {
	v, ok := h.pendingCriticalConfirm.Load(confirmID)
	if !ok {
		log.Printf("[critical-confirm] resolve: confirmID=%s not found (expired or already resolved)", confirmID)
		return fmt.Errorf("确认已过期或已处理，请重新安装")
	}
	entry, ok := v.(*pendingCriticalConfirmEntry)
	if !ok {
		log.Printf("[critical-confirm] resolve: confirmID=%s has unexpected entry type", confirmID)
		return fmt.Errorf("内部错误：确认状态异常")
	}

	// Try to win the resolve race against the cleanup goroutine.
	if !entry.tryResolve() {
		// Cleanup goroutine already won — the channel is closed or being closed.
		log.Printf("[critical-confirm] resolve: confirmID=%s already resolved (timeout race)", confirmID)
		return fmt.Errorf("确认已超时，请重新安装")
	}

	// We won the race — safe to send on the channel (it's not closed).
	h.pendingCriticalConfirm.Delete(confirmID)
	entry.Ch <- criticalRiskConfirmResponse{Confirmed: confirmed}
	log.Printf("[critical-confirm] resolve: confirmID=%s confirmed=%v", confirmID, confirmed)
	return nil
}
