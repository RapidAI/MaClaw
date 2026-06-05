package main

import (
	"fmt"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/config"
)

type agentLoopIterationLimits struct {
	EffectiveMax      int
	ChatFinalizeGrace int
}

func computeAgentLoopIterationLimits(ctx *LoopContext, maxIter int, minIterations int) agentLoopIterationLimits {
	effectiveMax := config.EffectiveMaxIterations(maxIter)
	chatFinalizeGrace := 0
	if ctx != nil && ctx.Kind == LoopKindChat {
		chatFinalizeGrace = 2
	}
	if ctx != nil && ctx.Runtime.Execution.IsLight() && ctx.Runtime.Execution.IterationBudget > 0 {
		effectiveMax = ctx.Runtime.Execution.IterationBudget
		chatFinalizeGrace = 1
	}
	if minIterations > 0 && effectiveMax < minIterations {
		effectiveMax = minIterations
		if effectiveMax > config.MaxAgentIterationsCap {
			effectiveMax = config.MaxAgentIterationsCap
		}
	}
	return agentLoopIterationLimits{
		EffectiveMax:      effectiveMax,
		ChatFinalizeGrace: chatFinalizeGrace,
	}
}

func (h *IMMessageHandler) refreshAgentLoopEffectiveMax(ctx *LoopContext, iteration int, effectiveMax int, minIterations int, loopDriftDetector *DriftDetector, sendProgress func(string)) int {
	if h.loopMaxOverride > 0 {
		override := h.loopMaxOverride
		if minIterations > 0 && override < minIterations {
			override = minIterations
		}
		ctx.SetMaxIterations(override)
		return override
	}
	if cm := ctx.MaxIterations(); cm > 0 && cm != effectiveMax {
		effectiveMax = cm
	}
	if ctx.Kind != LoopKindChat || effectiveMax <= 0 {
		return effectiveMax
	}
	if ctx.Runtime.Execution.IsLight() {
		return effectiveMax
	}
	remaining := effectiveMax - iteration
	if remaining > 2 {
		return effectiveMax
	}
	driftPreview := loopDriftDetector.PreviewDrift()
	if driftPreview.Drifted && driftPreview.NeedHumanHelp {
		return effectiveMax
	}
	autoExtendCap := config.MaxAgentIterationsCap * 2
	autoExtended := effectiveMax + 30
	if autoExtended > autoExtendCap {
		autoExtended = autoExtendCap
	}
	if autoExtended <= effectiveMax {
		return effectiveMax
	}
	effectiveMax = autoExtended
	ctx.SetMaxIterations(effectiveMax)
	sendProgress(fmt.Sprintf("Current task is long; auto-extended reasoning rounds to %d.", effectiveMax))
	log.Printf("[AgentLoop] auto-extended: iteration=%d new_max=%d cap=%d loop=%s", iteration, effectiveMax, autoExtendCap, ctx.ID)
	if h.traceService != nil && ctx.RunID != "" {
		h.appendTraceEvent(ctx, "loop.extended", "info", "Auto-extended iteration limit", truncateTraceText(fmt.Sprintf("remaining=%d new_max=%d", remaining, effectiveMax), 220), "", "")
	}
	return effectiveMax
}

func handleBackgroundIterationPause(ctx *LoopContext, iteration int, effectiveMax int) (*IMAgentResponse, bool) {
	if ctx.Kind != LoopKindBackground || effectiveMax <= 4 || iteration != effectiveMax-2 {
		return nil, false
	}
	ctx.SetLoopState(LoopStatePaused)
	if ctx.StatusC != nil {
		select {
		case ctx.StatusC <- StatusEvent{
			Type:      StatusEventApproachingLimit,
			LoopID:    ctx.ID,
			SessionID: ctx.SessionID,
			Message:   fmt.Sprintf("Background task %s is approaching the iteration limit (%d/%d)", ctx.ID, iteration, effectiveMax),
			Remaining: effectiveMax - iteration,
		}:
		default:
		}
	}
	select {
	case extra := <-ctx.ContinueC:
		ctx.AddMaxIterations(extra)
		ctx.SetLoopState(LoopStateRunning)
		return nil, false
	case <-ctx.CancelC:
		ctx.SetLoopState(LoopStateStopped)
		return &IMAgentResponse{Text: fmt.Sprintf("Background task %s was stopped.", ctx.ID)}, true
	case <-time.After(5 * time.Minute):
		ctx.SetLoopState(LoopStateTimeout)
		return &IMAgentResponse{Text: fmt.Sprintf("Background task %s timed out waiting for continuation and was ended.", ctx.ID)}, true
	}
}

func shouldStopForAgentLoopIterationLimit(ctx *LoopContext, iteration int, effectiveMax int, chatFinalizeGrace int) bool {
	if iteration < effectiveMax+chatFinalizeGrace {
		return false
	}
	log.Printf("[AgentLoop] iteration limit reached: iteration=%d effectiveMax=%d grace=%d loop=%s", iteration, effectiveMax, chatFinalizeGrace, ctx.ID)
	return true
}
