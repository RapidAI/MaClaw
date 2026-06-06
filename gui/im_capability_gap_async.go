package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

func (h *IMMessageHandler) maybeStartAsyncCapabilityGapSearch(ctx *LoopContext, iteration int, visibleContent, msgContent string, truncatedToolCount int, userText, userID string, totalToolCallsInLoop int, phase *agentLoopPhase) {
	if !shouldStartAsyncCapabilityGapSearch(iteration, visibleContent, truncatedToolCount, totalToolCallsInLoop, phase) ||
		h.capabilityGapDetector == nil {
		return
	}
	capturedUserText := userText
	capturedGapText := msgContent
	capturedRecoverReason := ""
	if phase != nil {
		capturedRecoverReason = phase.RecoverReason.String()
	}
	capturedUserID := userID
	capturedPlatform := runtimePlatformFromLoopContext(ctx)
	capturedPolicyOwnerID := h.workflowPolicyOwnerID(userID, ctx)
	go h.runAsyncCapabilityGapSearch(capturedUserText, capturedGapText, capturedRecoverReason, capturedPlatform, capturedUserID, capturedPolicyOwnerID)
}

func shouldStartAsyncCapabilityGapSearch(iteration int, visibleContent string, truncatedToolCount int, totalToolCallsInLoop int, phase *agentLoopPhase) bool {
	// Keep this gate purely loop-state based. It runs on the no-tool finalize path,
	// so semantic classifiers or network calls here would create hidden side effects
	// after an otherwise completed response.
	if iteration >= 3 || len(visibleContent) > 500 || truncatedToolCount > 0 {
		return false
	}
	if phase == nil {
		return false
	}
	if phase.SkillFailed || phase.RecoverReason == agentRecoverSkillFailed {
		return true
	}
	if phase.Stage != agentStageRecover {
		return false
	}
	switch phase.RecoverReason {
	case agentRecoverNoToolStall, agentRecoverEmptyFinalResponse, agentRecoverDeliverablePending, agentRecoverTrialFailed:
		return true
	case agentRecoverPendingSkillRunNoTool:
		return totalToolCallsInLoop == 0
	default:
		return false
	}
}

func asyncCapabilityGapSearchQuery(ctx context.Context, detector *CapabilityGapDetector, userText, gapText string) string {
	if contextErr(ctx) != nil {
		return ""
	}
	userText = strings.TrimSpace(userText)
	gapText = strings.TrimSpace(gapText)
	if detector == nil || !detector.isLLMConfigured() || gapText == "" {
		return userText
	}
	queryInput := "User task: " + userText + "\nCapability gap: " + gapText
	query := strings.TrimSpace(detector.extractCapabilityQuery(ctx, queryInput, nil))
	if contextErr(ctx) != nil {
		return ""
	}
	if query == "" {
		return userText
	}
	return query
}

func (h *IMMessageHandler) runAsyncCapabilityGapSearch(userText, gapText, recoverReason, platform, userID, policyOwnerID string) {
	// Use a generous timeout for the search + download + install pipeline.
	// The confirmation wait inside confirmRiskSkillInstall has its own independent
	// timeout (confirmTimeout = 120s) managed by a cleanup goroutine. We pass
	// a separate installCtx so that the confirmation wait is NOT bounded by the
	// search timeout — the user can take the full confirmTimeout to respond.
	searchCtx, searchCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer searchCancel()
	if h.capabilityGapDetector == nil || !h.capabilityGapDetector.DetectWithContext(searchCtx, gapText) {
		return
	}
	searchQuery := strings.TrimSpace(asyncCapabilityGapSearchQuery(searchCtx, h.capabilityGapDetector, userText, gapText))
	if searchQuery == "" {
		return
	}
	searcher := NewSkillSearcher(NewSkillMarketClient(h.app))
	log.Printf("[skill-auto-async] searching skill with query: %q recover_reason=%q", searchQuery, recoverReason)
	best, searchErr := searcher.SearchAndInstallForTask(searchCtx, searchQuery, userText)
	if searchErr != nil || best == nil {
		return
	}
	log.Printf("[skill-auto-async] found skill: %s (%s)", best.Name, best.Status)

	// installAndExecuteSkill may block on user confirmation (up to confirmTimeout).
	// Use a separate context that won't expire before the confirmation window closes.
	// confirmTimeout (120s) + buffer for post-confirm install steps (30s) = 150s.
	installCtx, installCancel := context.WithTimeout(context.Background(), confirmTimeout+30*time.Second)
	defer installCancel()
	if !h.confirmAsyncCapabilityGapSkillInstall(installCtx, best, platform, userID, searchQuery, recoverReason) {
		log.Printf("[skill-auto-async] user did not approve background skill install: %s", best.Name)
		return
	}

	installResult := h.installAndExecuteSkill(installCtx, best, userText, platform, userID, policyOwnerID, func(status string) {
		log.Printf("[skill-auto-async] %s", status)
	})
	h.pendingCapabilityGap.Store(userID, &pendingCapabilityGapResult{
		SkillName: best.Name,
		Result:    installResult.Text,
		Success:   installResult.Success,
		Timestamp: time.Now(),
	})
	lang := h.skillConfirmLang()
	if installResult.Success {
		log.Printf("[skill-auto-async] skill %q installed successfully, result pending for next turn", best.Name)
		if h.app != nil {
			h.emitAppEvent("skill-auto-installed", map[string]string{
				"name":   best.Name,
				"result": installResult.Text,
			})
			h.emitAppEvent("skill-install-result", map[string]interface{}{
				"name":    best.Name,
				"success": true,
				"lang":    lang,
				"message": localizedSkillInstallResultMessage(lang, best.Name, true, ""),
			})
		}
		return
	}
	log.Printf("[skill-auto-async] skill install/execute finished without success: %s", installResult.Text)
	// Only emit failure feedback for cases where the user did NOT already see
	// inline feedback from the confirmation buttons. The frontend's executeAction
	// already shows the localized rejection text when the user clicks reject, so we skip
	// the event for user-initiated rejections to avoid duplicate messages.
	if !installResult.SilentFailure && h.app != nil {
		// Truncate long error text for the chat message (full text is in logs).
		errText := installResult.Text
		if len(errText) > 200 {
			errText = errText[:200] + "..."
		}
		h.emitAppEvent("skill-install-result", map[string]interface{}{
			"name":    best.Name,
			"success": false,
			"lang":    lang,
			"message": localizedSkillInstallResultMessage(lang, best.Name, false, errText),
		})
	}
}

func (h *IMMessageHandler) confirmAsyncCapabilityGapSkillInstall(ctx context.Context, best *SkillSearchResult, platform, userID, searchQuery, recoverReason string) bool {
	if h == nil || best == nil {
		log.Printf("[skill-auto-async] background install confirmation failed closed: missing handler or skill")
		return false
	}
	if strings.TrimSpace(platform) == "" {
		log.Printf("[skill-auto-async] background install confirmation failed closed: missing platform for skill %q", best.Name)
		return false
	}
	factors := []string{
		"Background capability repair found a remote skill and needs approval before installing it.",
		"Installation changes the local skill registry and can run skill-defined steps.",
	}
	source := asyncCapabilityGapSkillSourceLabel(best)
	confirmed := h.confirmRiskSkillInstall(ctx, best.Name, source, security.RiskLow, factors, platform, userID)
	h.logAsyncCapabilityGapInstallDecision(best, source, confirmed, searchQuery, recoverReason)
	return confirmed
}

func asyncCapabilityGapSkillSourceLabel(best *SkillSearchResult) string {
	source := "unknown"
	if best != nil {
		source = best.SourceKind().String()
	}
	return "background capability repair via " + source
}

func (h *IMMessageHandler) logAsyncCapabilityGapInstallDecision(best *SkillSearchResult, source string, confirmed bool, searchQuery string, recoverReason string) {
	if h == nil || best == nil || h.getAuditLog() == nil {
		return
	}
	action := security.AuditActionHubSkillReject
	policy := security.PolicyDeny
	result := "user rejected or timed out background capability repair skill install"
	if confirmed {
		action = security.AuditActionHubSkillInstall
		policy = security.PolicyUserOverride
		result = "user approved background capability repair skill install"
	}
	_ = h.getAuditLog().Log(security.AuditEntry{
		Timestamp: time.Now(),
		Action:    action,
		ToolName:  "background_capability_repair_skill_install",
		Arguments: map[string]interface{}{
			"skill":          best.Name,
			"source":         source,
			"search_query":   strings.TrimSpace(searchQuery),
			"recover_reason": strings.TrimSpace(recoverReason),
		},
		RiskLevel:    security.RiskLow,
		PolicyAction: policy,
		Result:       result + fmt.Sprintf(": skill=%s source=%s", best.Name, source),
	})
}
