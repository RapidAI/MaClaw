package main

import (
	"context"
	"log"
	"time"
)

func (h *IMMessageHandler) maybeStartAsyncCapabilityGapSearch(ctx *LoopContext, iteration int, visibleContent, msgContent string, truncatedToolCount int, userText, userID string) {
	skipCapabilityGap := iteration >= 3 || len(visibleContent) > 500 || truncatedToolCount > 0
	if skipCapabilityGap || h.capabilityGapDetector == nil || !h.capabilityGapDetector.Detect(msgContent) {
		return
	}
	capturedUserText := userText
	capturedUserID := userID
	capturedPlatform := runtimePlatformFromLoopContext(ctx)
	if capturedPlatform == "" && ctx == nil {
		capturedPlatform = h.currentRuntimePlatform()
	}
	capturedPolicyOwnerID := h.workflowPolicyOwnerID(userID, ctx)
	go h.runAsyncCapabilityGapSearch(capturedUserText, capturedPlatform, capturedUserID, capturedPolicyOwnerID)
}

func (h *IMMessageHandler) runAsyncCapabilityGapSearch(userText, platform, userID, policyOwnerID string) {
	// Use a generous timeout for the search + download + install pipeline.
	// The confirmation wait inside confirmRiskSkillInstall has its own independent
	// timeout (confirmTimeout = 120s) managed by a cleanup goroutine. We pass
	// a separate installCtx so that the confirmation wait is NOT bounded by the
	// search timeout — the user can take the full confirmTimeout to respond.
	searchCtx, searchCancel := context.WithTimeout(context.Background(), 60*time.Second)
	searcher := NewSkillSearcher(NewSkillMarketClient(h.app))
	best, searchErr := searcher.SearchAndInstall(searchCtx, userText)
	searchCancel() // release timer immediately; search phase is done
	if searchErr != nil || best == nil {
		return
	}
	log.Printf("[skill-auto-async] found skill: %s (%s)", best.Name, best.Status)

	// installAndExecuteSkill may block on user confirmation (up to confirmTimeout).
	// Use a separate context that won't expire before the confirmation window closes.
	// confirmTimeout (120s) + buffer for post-confirm install steps (30s) = 150s.
	installCtx, installCancel := context.WithTimeout(context.Background(), confirmTimeout+30*time.Second)
	defer installCancel()

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
