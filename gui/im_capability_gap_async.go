package main

import (
	"context"
	"log"
	"time"
)

func (h *IMMessageHandler) maybeStartAsyncCapabilityGapSearch(iteration int, visibleContent, msgContent string, truncatedToolCount int, userText, userID string) {
	skipCapabilityGap := iteration >= 3 || len(visibleContent) > 500 || truncatedToolCount > 0
	if skipCapabilityGap || h.capabilityGapDetector == nil || !h.capabilityGapDetector.Detect(msgContent) {
		return
	}
	capturedUserText := userText
	capturedUserID := userID
	capturedPlatform := ""
	if h.currentLoopCtx != nil {
		capturedPlatform = h.currentLoopCtx.Platform
	}
	go h.runAsyncCapabilityGapSearch(capturedUserText, capturedPlatform, capturedUserID)
}

func (h *IMMessageHandler) runAsyncCapabilityGapSearch(userText, platform, userID string) {
	goCtx, goCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer goCancel()
	searcher := NewSkillSearcher(NewSkillMarketClient(h.app))
	best, searchErr := searcher.SearchAndInstall(goCtx, userText)
	if searchErr != nil || best == nil {
		return
	}
	log.Printf("[skill-auto-async] found skill: %s (%s)", best.Name, best.Status)
	installResult := h.installAndExecuteSkill(goCtx, best, userText, platform, userID, func(status string) {
		log.Printf("[skill-auto-async] %s", status)
	})
	h.pendingCapabilityGap.Store(userID, &pendingCapabilityGapResult{
		SkillName: best.Name,
		Result:    installResult.Text,
		Success:   installResult.Success,
		Timestamp: time.Now(),
	})
	if installResult.Success {
		log.Printf("[skill-auto-async] skill %q installed successfully, result pending for next turn", best.Name)
		if h.app != nil {
			h.emitAppEvent("skill-auto-installed", map[string]string{
				"name":   best.Name,
				"result": installResult.Text,
			})
		}
		return
	}
	log.Printf("[skill-auto-async] skill install/execute finished without success: %s", installResult.Text)
}
