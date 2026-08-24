package main

import (
	"strings"

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
	if len(needs) == 0 {
		return nil
	}
	out := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		cloned := need
		if len(need.Qualifiers) > 0 {
			cloned.Qualifiers = make(map[string]string, len(need.Qualifiers))
			for key, value := range need.Qualifiers {
				cloned.Qualifiers[key] = value
			}
		}
		out = append(out, cloned)
	}
	return out
}

func grantedNeedsFromPlan(plan tool.ToolPlan) []tool.CapabilityNeed {
	needs := make([]tool.CapabilityNeed, 0, len(plan.Selections))
	for _, selection := range plan.Selections {
		need := tool.CapabilityNeed{
			ID:         strings.TrimSpace(selection.NeedID),
			Capability: selection.FitProof.MatchedCapability,
			Qualifiers: map[string]string{},
			Polarity:   tool.NeedRequire,
			Required:   true,
		}
		for key, value := range selection.FitProof.QualifierBindings {
			need.Qualifiers[key] = value
		}
		if need.ID == "" {
			need.ID = strings.TrimSpace(selection.ID)
		}
		needs = append(needs, need)
	}
	return needs
}

func sessionGovernedNeedHasSideEffect(need tool.CapabilityNeed) bool {
	capability := strings.TrimSpace(string(need.Capability))
	if capability == "" {
		return false
	}
	if strings.HasPrefix(capability, "information.search.") || capability == "information.current_time" {
		return false
	}
	if strings.HasPrefix(capability, "document.read.") ||
		strings.HasPrefix(capability, "fs.read.") ||
		strings.HasPrefix(capability, "repo.inspect.") ||
		strings.HasPrefix(capability, "knowledge.read.") ||
		strings.HasPrefix(capability, "information.fetch.") ||
		strings.HasPrefix(capability, "security.audit.") ||
		strings.HasPrefix(capability, "audio.transcribe.") ||
		strings.HasPrefix(capability, "governance.inspect.") ||
		capability == "visual.capture.desktop" ||
		capability == string(tool.CapabilityInteractionAskUser) {
		return false
	}
	return true
}

func sessionGovernedNeedsHaveSideEffect(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if sessionGovernedNeedHasSideEffect(need) {
			return true
		}
	}
	return false
}

func (task SessionGovernedTask) replayable() bool {
	if task.Status != sessionGovernedPending && task.Status != sessionGovernedFailedExec {
		return false
	}
	return sessionGovernedNeedsHaveSideEffect(task.Needs)
}

func semanticNeedStillCovered(need tool.CapabilityNeed) bool {
	for _, templates := range imSemanticIntentRuleSet {
		for _, tmpl := range templates {
			if tmpl.Capability == need.Capability {
				return true
			}
		}
	}
	return false
}

func grantedNeedsStillCovered(needs []tool.CapabilityNeed) []tool.CapabilityNeed {
	kept := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		if semanticNeedStillCovered(need) {
			kept = append(kept, need)
		}
	}
	return cloneSessionGovernedNeeds(kept)
}

func classificationFromGrantedNeeds(needs []tool.CapabilityNeed) intent.ClassificationResult {
	hasGenerate := false
	hasImageDeliver := false
	hasFileDeliver := false
	seen := make(map[intent.IntentLabel]bool)
	labels := make([]intent.IntentLabel, 0, len(needs))
	add := func(label intent.IntentLabel) {
		if label == "" || seen[label] {
			return
		}
		seen[label] = true
		labels = append(labels, label)
	}
	for _, need := range needs {
		switch need.Capability {
		case "document.generate.file":
			hasGenerate = true
			add(intent.LabelDocumentGenerate)
		case "information.search.web":
			if need.Qualifiers["freshness"] == "current" {
				add(intent.LabelLiveData)
			} else {
				add(intent.LabelSearch)
			}
		case "information.current_time":
			add(intent.LabelCurrentTime)
		case tool.CapabilityAudioRenderSpeech:
			add(intent.LabelAudioDeliver)
		case tool.CapabilitySystemLaunchLocal:
			// app_launch and document_open share this capability. Replay
			// keeps a single label so the planner emits one launch need.
			add(intent.LabelAppLaunch)
		case "visual.capture.desktop":
			add(intent.LabelScreenshot)
		case "artifact.deliver.current_channel":
			if need.Qualifiers["format"] == "image" {
				hasImageDeliver = true
			}
			if need.Qualifiers["format"] == "file" {
				hasFileDeliver = true
			}
			if need.Qualifiers["format"] == "voice" {
				add(intent.LabelAudioDeliver)
			}
		default:
			for label, templates := range imSemanticIntentRuleSet {
				for _, tmpl := range templates {
					if tmpl.Capability == need.Capability {
						add(label)
					}
				}
			}
		}
	}
	if hasImageDeliver {
		add(intent.LabelScreenshot)
	}
	if hasFileDeliver && !hasGenerate {
		add(intent.LabelAttachmentDelivery)
	}
	if len(labels) == 0 {
		return intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.9, Layer: 3}
	}
	return intent.ClassificationResult{Primary: labels[0], Secondary: append([]intent.IntentLabel(nil), labels[1:]...), Confidence: 0.98, Layer: 3}
}

func isGenericContinuationPrimary(result intent.ClassificationResult) bool {
	switch result.Primary {
	case intent.LabelContinuation, intent.LabelUnknown, intent.LabelAmbiguous:
		return true
	default:
		return false
	}
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
