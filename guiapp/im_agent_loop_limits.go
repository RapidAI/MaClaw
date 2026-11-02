package guiapp

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
	if ctx != nil && ctx.Runtime.Execution.IterationBudget > 0 &&
		(ctx.Runtime.Execution.IsLight() || operationalExecutionProfile(ctx.Runtime.Execution)) {
		effectiveMax = ctx.Runtime.Execution.IterationBudget
		if ctx.Runtime.Execution.IsLight() {
			chatFinalizeGrace = 1
		}
	}
	if minIterations > 0 && effectiveMax < minIterations {
		effectiveMax = minIterations
		if effectiveMax > config.MaxAgentIterationsCap {
			effectiveMax = config.MaxAgentIterationsCap
		}
	}
	if ctx != nil && ctx.ForegroundChatBudget > 0 && effectiveMax > ctx.ForegroundChatBudget {
		effectiveMax = ctx.ForegroundChatBudget
		chatFinalizeGrace = 0
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
		if ctx != nil {
			ctx.SetMaxIterations(override)
		}
		return override
	}
	if ceiling, raised := ctx.extendedIterationCeiling(); raised {
		// A refill already raised this loop. The foreground budget is the
		// first wave only; clamping back to it spends every later refill
		// and leaves the turn on the original small limit.
		if ceiling >= effectiveMax {
			return ceiling
		}
		return effectiveMax
	}
	if ctx != nil && ctx.ForegroundChatBudget > 0 {
		ctx.SetMaxIterations(ctx.ForegroundChatBudget)
		return ctx.ForegroundChatBudget
	}
	if cm := ctx.MaxIterations(); cm > 0 && cm != effectiveMax {
		effectiveMax = cm
	}
	if ctx.Kind != LoopKindChat || effectiveMax <= 0 {
		return effectiveMax
	}
	if ctx.Runtime.Execution.IsLight() || ctx.Runtime.Execution.IterationBudget > 0 {
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
	// Do not auto-extend if drift was already detected at least once in this
	// loop. Extending gives the drifting LLM 30 more iterations to repeat
	// before the drift detector fires NeedHumanHelp on the second occurrence.
	if loopDriftDetector.ReplanCount() > 0 {
		log.Printf("[AgentLoop] auto-extend suppressed: drift already detected (replanCount=%d) loop=%s", loopDriftDetector.ReplanCount(), ctx.ID)
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
	ctx.raiseIterationCeiling(effectiveMax)
	if sendProgress != nil {
		sendProgress(fmt.Sprintf("Current task is long; auto-extended reasoning rounds to %d.", effectiveMax))
	}
	log.Printf("[AgentLoop] auto-extended: iteration=%d new_max=%d cap=%d loop=%s", iteration, effectiveMax, autoExtendCap, ctx.ID)
	if h.traceService != nil && ctx.RunID != "" {
		h.appendTraceEvent(ctx, "loop.extended", "info", "Auto-extended iteration limit", truncateTraceText(fmt.Sprintf("remaining=%d new_max=%d", remaining, effectiveMax), 220), "", "")
	}
	return effectiveMax
}

func backgroundIterationPauseDue(ctx *LoopContext, iteration int, effectiveMax int) bool {
	return ctx != nil && ctx.Kind == LoopKindBackground && effectiveMax > 4 && iteration == effectiveMax-2
}

func handleBackgroundIterationPause(ctx *LoopContext, iteration int, effectiveMax int) (*IMAgentResponse, bool) {
	if !backgroundIterationPauseDue(ctx, iteration, effectiveMax) {
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

// codingTaskStillOpen reports an unfinished programming task. That task keeps
// receiving iterations in the loop it already has. Ordinary chat keeps the
// session wave cap.
func (h *IMMessageHandler) codingTaskStillOpen(ctx *LoopContext, userID string) bool {
	if ctx != nil && ctx.codingSessionOpen() {
		return true
	}
	// A background job for the same owner is not the coding task. It keeps
	// the pause and the chat wave cap. The interactive workbench turn does not.
	if h == nil || ctx == nil || ctx.Kind == LoopKindBackground {
		return false
	}
	if h.isPureCodingWorkbenchSession(userID) {
		return true
	}
	return h.isPureCodingWorkbenchSession(ctx.UserID)
}

// sameLoopIterationRefill adds another configured budget to the loop that
// is about to stop. The conversation and tool results stay where they are.
// codingStillOpen keeps a programming task on this loop after the chat cap.
func sameLoopIterationRefill(ctx *LoopContext, toolCalls, configuredMax int, codingStillOpen bool) int {
	if !ctx.claimIterationExtensionForOpenTask(toolCalls, codingStillOpen) {
		return 0
	}
	extra := config.EffectiveMaxIterations(configuredMax)
	log.Printf("[agent-loop] same conversation continues with %d more iterations (used tools %d)", extra, toolCalls)
	return extra
}

func shouldStopForAgentLoopIterationLimit(ctx *LoopContext, iteration int, effectiveMax int, chatFinalizeGrace int) bool {
	if iteration < effectiveMax+chatFinalizeGrace {
		return false
	}
	log.Printf("[AgentLoop] iteration limit reached: iteration=%d effectiveMax=%d grace=%d loop=%s", iteration, effectiveMax, chatFinalizeGrace, ctx.ID)
	return true
}
