package main

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// adoptLateTreeSemanticIntent replaces a degraded in-flight classification
// with a later non-degraded cache hit for the same message. Fusion timeouts
// schedule that verdict in the background; the current turn is often still
// preparing tools when it lands, and ignoring it left lookup questions on a
// leftover surface with no web_search (2026-08-29 长江学者).
func (h *IMMessageHandler) adoptLateTreeSemanticIntent(ctx *LoopContext, userID, userText string, history []agent.ConversationEntry) bool {
	if h == nil || ctx == nil || loopContextIsVisionFallthrough(ctx) {
		return false
	}
	current := ctx.Runtime.SemanticIntent
	if current == nil || !current.Degraded || current.ControlPlaneFailure {
		return false
	}
	// A kept search/office/live_data hint is already routing this turn.
	// Only a collapsed unknown should be replaced by the late verdict.
	if current.Primary != intent.LabelUnknown {
		return false
	}
	uic := h.getUnifiedClassifier()
	if uic == nil {
		return false
	}
	cached, ok := uic.ClassifyCached(classificationCacheMessageForTurn(ctx, userID, userText, history))
	if !ok || cached.Degraded || cached.ControlPlaneFailure {
		return false
	}
	if cached.Primary == intent.LabelUnknown || cached.Primary == intent.LabelAmbiguous {
		return false
	}
	copied := cached
	normalizeSemanticClassificationForTurn(&copied)
	// Same floor as leftover skip cache activation. A below-floor lookup
	// would chat-project and drop the timeout pin; a weak office/search
	// tree must not take over this turn either.
	if copied.Confidence < intent.EmbeddingLookupMinScore || semanticNeedsChatProjection(copied) {
		return false
	}
	// Current-turn adoption is more aggressive than a user resend. Mutating
	// families (coding/browser/ssh/shell) stay cached for the next request;
	// locking this in-flight turn onto them HostRejects a lookup timeout.
	if !lateTreeCurrentTurnAdoptAllowed(copied) {
		return false
	}
	from := current.Primary
	bindLoopSemanticIntent(ctx, &copied)
	if executionProfileCausedByDegradedIntent(ctx.Runtime.Execution) {
		ctx.Runtime.Execution = executionProfileFromSemanticIntent(&copied, h.executionContractForRegisteredToolName)
	}
	log.Printf("[semantic-routing] adopted late tree verdict request_id=%q user=%q from=%s to=%s conf=%.2f reason=%q",
		ctx.Runtime.RequestID, userID, from, copied.Primary, copied.Confidence, copied.Reason)
	return true
}

func lateTreeCurrentTurnAdoptAllowed(result intent.ClassificationResult) bool {
	if semanticDeclaredLookupGenerateComposite(result) || semanticDeclaredLookupVisualComposite(result) {
		return true
	}
	switch result.Primary {
	case intent.LabelSearch, intent.LabelLiveData, intent.LabelWebFetch,
		intent.LabelCurrentTime, intent.LabelKnowledgeRead, intent.LabelOffice:
		return true
	default:
		return false
	}
}

func executionProfileCausedByDegradedIntent(profile ExecutionProfile) bool {
	switch strings.TrimSpace(profile.Reason) {
	case "semantic classifier degraded", "semantic classifier unavailable", "semantic confidence below light threshold":
		return true
	default:
		return false
	}
}

// classifierTimeoutUnknown reports a degraded unknown caused by a classifier
// outage, not a gate-7 chat projection of a sub-floor lookup. Those
// projections must not grow web tools (北京天所).
func classifierTimeoutUnknown(ctx *LoopContext) bool {
	if ctx == nil || ctx.Runtime.SemanticIntent == nil || loopContextIsVisionFallthrough(ctx) {
		return false
	}
	result := *ctx.Runtime.SemanticIntent
	if !result.Degraded || result.ControlPlaneFailure || result.Primary != intent.LabelUnknown {
		return false
	}
	reason := result.Reason
	if strings.Contains(reason, "chat projection") {
		return false
	}
	return strings.Contains(reason, "tree classification unavailable") ||
		strings.Contains(reason, "semantic classifiers unavailable") ||
		strings.Contains(reason, "contradicted by local")
}

func markClassifierTimeoutLookup(ctx *LoopContext) {
	if !classifierTimeoutUnknown(ctx) {
		return
	}
	ctx.Runtime.ClassifierTimeoutLookup = true
}

func loopContextHasClassifierTimeoutLookup(ctx *LoopContext) bool {
	return ctx != nil && ctx.Runtime.ClassifierTimeoutLookup && !loopContextIsVisionFallthrough(ctx)
}

func loopHistory(ctx *LoopContext) []agent.ConversationEntry {
	if ctx == nil {
		return nil
	}
	return ctx.History
}

func classificationCacheMessageForTurn(ctx *LoopContext, userID, userText string, history []agent.ConversationEntry) intent.MessageContext {
	if ctx != nil && strings.TrimSpace(ctx.Runtime.ClassificationMessage.Text) != "" {
		return ctx.Runtime.ClassificationMessage
	}
	return classificationMessage(userID, userText, history)
}

func classificationMessage(userID, userText string, history []agent.ConversationEntry) intent.MessageContext {
	return classificationMessageFromHistory(userID, userText, recentHistoryTexts(history, 6))
}

func classificationMessageFromHistory(userID, userText string, recentHistory []string) intent.MessageContext {
	return intent.MessageContext{
		Text:          semanticUserIntentText(userText),
		UserID:        userID,
		RecentHistory: recentHistory,
	}
}
