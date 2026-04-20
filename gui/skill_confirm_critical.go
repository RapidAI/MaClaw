package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// criticalRiskConfirmResponse is sent on the response channel when the user
// answers the confirmation prompt.
type criticalRiskConfirmResponse struct {
	Confirmed bool
}

// buildCriticalRiskPrompt formats a confirmation prompt for a Critical-risk
// skill installation. The returned string contains a warning header with the
// skill name and source, the risk factors as bullet points, and a
// confirmation question.
func buildCriticalRiskPrompt(skillName, source string, factors []string) string {
	var sb strings.Builder

	// Warning header
	fmt.Fprintf(&sb, "⚠️ 安全警告: Skill「%s」来自 %s 被评估为 Critical 风险。\n", skillName, source)

	// Risk factors
	if len(factors) > 0 {
		sb.WriteString("\n风险因素:\n")
		for _, f := range factors {
			fmt.Fprintf(&sb, "  • %s\n", f)
		}
	}

	// Confirmation question
	sb.WriteString("\n确认安装此 Skill？\n")

	return sb.String()
}

// confirmCriticalRiskSkill presents a blocking confirmation prompt to the user
// when a skill is assessed as RiskCritical. Returns true if the user confirms,
// false on rejection, timeout, or any error condition (fail-closed).
//
// Parameters:
//
//	ctx       - parent context; cancellation returns false
//	skillName - display name of the skill
//	source    - origin (hub URL, GitHub repo URL, "clawhub", etc.)
//	factors   - risk factors from RiskAssessment.Factors
//	platform  - "desktop" or IM platform identifier (feishu/wechat/qq/telegram)
//	userID    - user identity; used together with platform to key IM pending
//	            confirmations so that concurrent users on the same platform
//	            do not overwrite each other's confirmation state.
//
// Behavior:
//   - Desktop: emits a Wails event with confirm/reject buttons, blocks on channel
//   - IM: builds an AskUserRequest with confirm input_type, blocks on channel
//   - Timeout: 120 seconds, defaults to reject
//   - Nil/empty platform: returns false (fail-closed)
func (h *IMMessageHandler) confirmCriticalRiskSkill(
	ctx context.Context,
	skillName, source string,
	factors []string,
	platform string,
	userID string,
) bool {
	// Fail-closed: empty platform means no channel to confirm through.
	if platform == "" {
		log.Printf("[critical-confirm] fail-closed: empty platform for skill %q", skillName)
		return false
	}

	// Generate a unique confirmation ID.
	confirmID := fmt.Sprintf("crit_%d", time.Now().UnixNano())

	// Build the prompt text.
	promptText := buildCriticalRiskPrompt(skillName, source, factors)

	// Create a buffered response channel (buffer 1 so the sender never blocks).
	respCh := make(chan criticalRiskConfirmResponse, 1)

	// Store the channel so ResolveCriticalConfirm can find it.
	h.pendingCriticalConfirm.Store(confirmID, respCh)
	defer h.pendingCriticalConfirm.Delete(confirmID)

	// Dispatch based on platform.
	switch {
	case platform == "desktop":
		// Desktop path: emit a Wails event with confirmation payload.
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
		// IM path: send the confirmation question as a proactive message through
		// the hub so the IM user sees it, and store the confirm ID so we can
		// intercept the user's response in handleIMMessageWithLoop.
		displayText := FormatAskUserForDisplay(&AskUserRequest{
			Question:  promptText,
			Options:   []string{"确认安装", "拒绝安装"},
			InputType: "confirm",
		})
		if h.app != nil {
			// Store the pending IM confirmation so handleIMMessageWithLoop can
			// match the user's reply ("确认安装" / "拒绝安装") to this confirm ID.
			// Key by platform:userID to avoid race conditions when two users on
			// the same IM platform trigger critical-risk installs simultaneously.
			imConfirmKey := platform + ":" + userID
			h.pendingCriticalConfirmIM.Store(imConfirmKey, confirmID)

			// Send the question to the IM user via the hub's proactive message channel.
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

	// Block until response, timeout, or context cancellation.
	select {
	case resp := <-respCh:
		log.Printf("[critical-confirm] received response confirm_id=%s confirmed=%v", confirmID, resp.Confirmed)
		return resp.Confirmed
	case <-time.After(120 * time.Second):
		log.Printf("[critical-confirm] timeout (120s) confirm_id=%s skill=%q — defaulting to reject", confirmID, skillName)
		return false
	case <-ctx.Done():
		log.Printf("[critical-confirm] context cancelled confirm_id=%s skill=%q — defaulting to reject", confirmID, skillName)
		return false
	}
}

// ResolveCriticalConfirm is called by the frontend or IM gateway when the user
// responds to a critical-risk confirmation prompt. It sends the user's answer
// on the response channel associated with the given confirmID.
func (h *IMMessageHandler) ResolveCriticalConfirm(confirmID string, confirmed bool) {
	v, ok := h.pendingCriticalConfirm.Load(confirmID)
	if !ok {
		log.Printf("[critical-confirm] resolve: confirmID=%s not found (expired or already resolved)", confirmID)
		return
	}
	ch, ok := v.(chan criticalRiskConfirmResponse)
	if !ok {
		log.Printf("[critical-confirm] resolve: confirmID=%s has unexpected channel type", confirmID)
		return
	}
	ch <- criticalRiskConfirmResponse{Confirmed: confirmed}
	log.Printf("[critical-confirm] resolve: confirmID=%s confirmed=%v", confirmID, confirmed)
}
