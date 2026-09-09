package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// SessionGovernedTask is the session-scoped replay record for a managed
// semantic turn. It stores only planner-granted needs, never the original
// UIC label set. RootTaskID is intentionally omitted: a later continuation
// allocates a new loop identity and must not merge the previous RouteState.
type SessionGovernedTask struct {
	Needs  []tool.CapabilityNeed
	Status sessionGovernedTaskStatus
}

type sessionGovernedTaskStatus string

const (
	sessionGovernedPending     sessionGovernedTaskStatus = "pending"
	sessionGovernedSucceeded   sessionGovernedTaskStatus = "succeeded"
	sessionGovernedFailedUnmet sessionGovernedTaskStatus = "failed_unmet"
	sessionGovernedFailedExec  sessionGovernedTaskStatus = "failed_exec"
	sessionGovernedSuperseded  sessionGovernedTaskStatus = "superseded"
)

func sessionGovernedTaskKey(userID, channel, destination string) string {
	return strings.TrimSpace(userID) + "\x1f" + semanticChannelScope(channel) + "\x1f" + strings.TrimSpace(destination)
}

func sessionGovernedDestination(ctx *LoopContext) string {
	if ctx == nil || ctx.DeliveryTarget == nil {
		return ""
	}
	return strings.TrimSpace(ctx.DeliveryTarget.DestinationID)
}

func cloneSessionGovernedNeeds(needs []tool.CapabilityNeed) []tool.CapabilityNeed {
	return tool.CloneCapabilityNeeds(needs)
}

func grantedNeedsFromPlan(plan tool.ToolPlan) []tool.CapabilityNeed {
	return tool.GrantedNeedsFromPlan(plan)
}

func sessionGovernedNeedHasSideEffect(need tool.CapabilityNeed) bool {
	return tool.CapabilityNeedHasSideEffect(nil, need)
}

func sessionGovernedNeedsHaveSideEffect(needs []tool.CapabilityNeed) bool {
	return tool.CapabilityNeedsHaveSideEffect(nil, needs)
}

func (task SessionGovernedTask) replayable() bool {
	if task.Status != sessionGovernedPending && task.Status != sessionGovernedFailedExec {
		return false
	}
	return sessionGovernedNeedsHaveSideEffect(task.Needs)
}

func grantedNeedsStillCovered(needs []tool.CapabilityNeed) []tool.CapabilityNeed {
	return tool.FilterGrantedNeedsStillCovered(needs, agentservice.CoveredCapabilitiesFromNeedTemplates(imSemanticIntentRuleSet), nil)
}

func classificationFromGrantedNeeds(needs []tool.CapabilityNeed) intent.ClassificationResult {
	return agentservice.ClassificationFromGrantedNeeds(needs, imSemanticIntentRuleSet)
}

func isGenericContinuationPrimary(result intent.ClassificationResult) bool {
	return result.IsGenericContinuationPrimary()
}

// allowsLegacySessionGovernedReplay confines the pre-coordinator in-memory
// replay facade to direct unit hosts while its callers migrate.  A real App
// already owns the durable coordinator, and NewIMMessageHandlerStandalone is
// the production TUI/non-GUI constructor: neither can prove a task relation
// from a user/channel/destination key alone.  Both therefore fail closed
// unless a future host wires an explicit, verified handle protocol.
func (h *IMMessageHandler) allowsLegacySessionGovernedReplay() bool {
	return h != nil && h.app == nil && h.standaloneConfig == nil
}

func (h *IMMessageHandler) persistSessionGovernedTask(userID, channel, destination string, workflowAgentLoop bool, plan tool.ToolPlan) {
	if workflowAgentLoop || !h.allowsLegacySessionGovernedReplay() {
		// App hosts own a durable coordinator. Until the ingress has a verified
		// task handle, the old user/channel/destination map cannot safely express
		// a continuation scope, so it must not create a second authority path.
		// The standalone/TUI constructor is a production host too; only direct
		// unit hosts retain this memory facade during compatibility migration.
		return
	}
	needs := grantedNeedsFromPlan(plan)
	if len(needs) == 0 {
		return
	}
	h.sessionGovernedTasks.Store(sessionGovernedTaskKey(userID, channel, destination), SessionGovernedTask{
		Needs:  cloneSessionGovernedNeeds(needs),
		Status: sessionGovernedPending,
	})
}

func (h *IMMessageHandler) loadSessionGovernedTask(userID, channel, destination string) (SessionGovernedTask, bool) {
	if !h.allowsLegacySessionGovernedReplay() {
		return SessionGovernedTask{}, false
	}
	value, ok := h.sessionGovernedTasks.Load(sessionGovernedTaskKey(userID, channel, destination))
	if !ok {
		return SessionGovernedTask{}, false
	}
	task, ok := value.(SessionGovernedTask)
	if !ok {
		return SessionGovernedTask{}, false
	}
	task.Needs = cloneSessionGovernedNeeds(task.Needs)
	return task, true
}

func (h *IMMessageHandler) markSessionGovernedTaskStatus(userID, channel, destination string, status sessionGovernedTaskStatus) {
	if !h.allowsLegacySessionGovernedReplay() || status == "" {
		return
	}
	key := sessionGovernedTaskKey(userID, channel, destination)
	value, ok := h.sessionGovernedTasks.Load(key)
	if !ok {
		return
	}
	task, ok := value.(SessionGovernedTask)
	if !ok {
		return
	}
	task.Status = status
	h.sessionGovernedTasks.Store(key, task)
}

func (h *IMMessageHandler) settleSessionGovernedTaskAfterLoop(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse) {
	if h == nil || resp == nil {
		return
	}
	channel := msg.Platform
	destination := sessionGovernedDestination(loopCtx)
	if strings.TrimSpace(resp.Error) == "semantic_capability_unmet" {
		return
	}
	if strings.TrimSpace(resp.Error) != "" {
		h.markSessionGovernedTaskStatus(msg.UserID, channel, destination, sessionGovernedFailedExec)
		return
	}
	task, ok := h.loadSessionGovernedTask(msg.UserID, channel, destination)
	if !ok {
		return
	}
	if !sessionGovernedNeedsHaveSideEffect(task.Needs) {
		h.markSessionGovernedTaskStatus(msg.UserID, channel, destination, sessionGovernedSucceeded)
	}
}

func (h *IMMessageHandler) clearSessionGovernedTasksForUser(userID string) {
	if h == nil {
		return
	}
	prefix := strings.TrimSpace(userID) + "\x1f"
	h.sessionGovernedTasks.Range(func(key, _ any) bool {
		if text, ok := key.(string); ok && strings.HasPrefix(text, prefix) {
			h.sessionGovernedTasks.Delete(key)
		}
		return true
	})
}

// applySessionGovernedContinuation rewrites a generic continuation/unknown
// classification to the previously granted needs when those needs are still
// replayable and covered. It never invents document_generate from UIC text.
// A newly staged image is a new request: replaying generate/deliver would
// steal the vision/OCR turn the host already prepared. The pending task is
// retired here even when the current label is not continuation, so a later
// oral "继续" cannot resurrect the previous PDF after the user moved on.
func (h *IMMessageHandler) applySessionGovernedContinuation(userID, channel, destination string, workflowAgentLoop bool, current intent.ClassificationResult, userText string, attachments []MessageAttachment) (intent.ClassificationResult, bool) {
	// A classifier protocol violation is host-owned failure state, not a generic
	// continuation. Replaying a previous granted plan here would turn a broken
	// control-plane response into capability authority before the loop-start
	// host rejection can enforce its boundary.
	if current.ControlPlaneFailure {
		return current, false
	}
	if workflowAgentLoop || !h.allowsLegacySessionGovernedReplay() {
		// Production App and standalone modes require a trusted task relation/handle before
		// reusing any durable fact. Do not let this legacy map turn a generic
		// utterance into a mutation merely because it shares an owner/channel.
		return current, false
	}
	if hostTurnSelectedLocalImage(userText, attachments) {
		h.markSessionGovernedTaskStatus(userID, channel, destination, sessionGovernedSuperseded)
		return current, false
	}
	if !isGenericContinuationPrimary(current) {
		return current, false
	}
	task, ok := h.loadSessionGovernedTask(userID, channel, destination)
	if !ok || !task.replayable() {
		return current, false
	}
	needs := grantedNeedsStillCovered(task.Needs)
	if len(needs) == 0 || !sessionGovernedNeedsHaveSideEffect(needs) {
		return current, false
	}
	replayed := classificationFromGrantedNeeds(needs)
	if !imSemanticIntentIsManaged(replayed) {
		return current, false
	}
	if _, unmapped := imSemanticIntentCoverage(replayed); unmapped != "" {
		return current, false
	}
	return replayed, true
}
