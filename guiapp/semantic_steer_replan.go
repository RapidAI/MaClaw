package guiapp

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// steerPlanLabels returns governed capability labels in verdict that this
// turn has not already declared. A degraded or non-planning verdict adds
// nothing. The classifier is the only authority: wording in the steer is
// not itself a grant.
func steerPlanLabels(current, verdict intent.ClassificationResult) []intent.IntentLabel {
	if verdict.Degraded || verdict.ControlPlaneFailure {
		return nil
	}
	if !semanticClassificationMeetsResolverFloor(verdict) && !semanticClassificationPlansBelowResolverFloor(verdict) {
		return nil
	}
	var out []intent.IntentLabel
	seen := map[intent.IntentLabel]struct{}{}
	for _, label := range verdict.Labels() {
		if label == "" || label.IsNonCapabilityLabel() {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		if classificationHasLabel(current, label) {
			continue
		}
		if len(imSemanticIntentRuleSet[label]) == 0 {
			continue
		}
		out = append(out, label)
	}
	return out
}

// steerLabelAlreadyPlanned reports that every required template leg of label
// is already on the published plan. Optional legs do not block expansion:
// a missing required document or delivery leg is the whole point of the steer.
func steerLabelAlreadyPlanned(plan tool.ToolPlan, label intent.IntentLabel) bool {
	templates := imSemanticIntentRuleSet[label]
	if len(templates) == 0 {
		return true
	}
	sawRequired := false
	for _, tmpl := range templates {
		if !tmpl.Required {
			continue
		}
		sawRequired = true
		if !semanticPlanHasCapability(plan, tmpl.Capability) {
			return false
		}
	}
	return sawRequired
}

// steerEmbeddingGapConfident is the Layer 2 early-exit bar (score and gap).
// A lead that misses it is still ambiguous and has to be read by the tree.
func steerEmbeddingGapConfident(result intent.ClassificationResult) bool {
	if result.Degraded || result.ControlPlaneFailure || result.Confidence < intent.EmbeddingConfidentMinScore {
		return false
	}
	if result.RunnerUp == "" || result.RunnerUpScore <= 0 {
		return true
	}
	return result.Confidence-result.RunnerUpScore >= intent.EmbeddingConfidentMinGap
}

// steerEmbeddingSettled reports a local reading that adds no capability and
// does not need the tree. An unresolved document_generate lead is never
// settled: Layer 2 refuses to grant that artifact until a lookup half exists.
func steerEmbeddingSettled(result intent.ClassificationResult) bool {
	if result.Degraded || result.ControlPlaneFailure || result.Primary == intent.LabelDocumentGenerate {
		return false
	}
	if result.Primary.IsNonCapabilityLabel() {
		return result.Confidence >= intent.EmbeddingLookupMinScore
	}
	return semanticClassificationMeetsResolverFloor(result) || semanticClassificationPlansBelowResolverFloor(result)
}

// acceptSteerEmbedding decides whether the local embedding is enough.
// document_generate is not a local grant on its own — Layer 2 cannot see
// where the facts come from and always escalates. Here the published plan
// is that lookup, so a confident artifact lead composes onto it and the
// tree is not asked again. Any other ambiguous lead still goes to the tree.
func acceptSteerEmbedding(current, embedded intent.ClassificationResult) (intent.ClassificationResult, bool) {
	if embedded.Degraded || embedded.ControlPlaneFailure {
		return intent.ClassificationResult{}, false
	}
	// Layer 2 abstains on a lone document_generate: it can see the artifact
	// but not where the facts come from. The published plan is that lookup,
	// so a lead that already clears the write floor and the gap composes
	// onto it. A secondary attached only as escalation evidence does not:
	// that result is an abstention and still belongs to the tree.
	if !embedded.EmbeddingEarlyExit && steerEmbeddingGapConfident(embedded) {
		if composed, ok := composeSteerDocumentGenerate(current, embedded); ok {
			return composed, true
		}
	}
	if !embedded.EmbeddingEarlyExit {
		return intent.ClassificationResult{}, false
	}
	// Early exit is the classifier's finished local decision. A verified
	// lookup+document composite clears both halves and still has a gap under
	// 0.10; re-applying the single-label gap sends that decision back to the
	// tree, and a tree timeout drops the file.
	labels := steerPlanLabels(current, embedded)
	if len(labels) == 0 {
		if steerEmbeddingSettled(embedded) {
			return embedded, true
		}
		return intent.ClassificationResult{}, false
	}
	return embedded, true
}

// composeSteerDocumentGenerate attaches a write-floor document_generate lead
// to a published lookup. The lead is not a grant by itself; the published
// plan is the fact source Layer 2 could not see. A result the planner would
// drop is not composed.
func composeSteerDocumentGenerate(current, embedded intent.ClassificationResult) (intent.ClassificationResult, bool) {
	if embedded.Degraded || embedded.ControlPlaneFailure || embedded.Primary != intent.LabelDocumentGenerate {
		return intent.ClassificationResult{}, false
	}
	if embedded.Confidence < intent.EmbeddingConfidentMinScore {
		return intent.ClassificationResult{}, false
	}
	if !semanticLookupHalf(current) || classificationHasLabel(current, intent.LabelDocumentGenerate) {
		return intent.ClassificationResult{}, false
	}
	out := current
	out.Degraded = false
	out.ControlPlaneFailure = false
	out.Secondary = append(append([]intent.IntentLabel(nil), current.Secondary...), intent.LabelDocumentGenerate)
	out.Reason = strings.TrimSpace(out.Reason + "; steer document_generate on published lookup")
	if len(steerPlanLabels(current, out)) == 0 {
		return intent.ClassificationResult{}, false
	}
	return out, true
}

// steerDocumentGenerateAfterTreeMiss keeps that lead when the tree was asked
// and did not answer. A close chart score stays unresolved: the margin does
// not choose between a file and a picture. A tree that did answer is left as
// it is, including a verdict that adds nothing.
func steerDocumentGenerateAfterTreeMiss(current, embedded, tree intent.ClassificationResult) (intent.ClassificationResult, bool) {
	if !tree.Degraded && !tree.ControlPlaneFailure {
		return intent.ClassificationResult{}, false
	}
	if embedded.RunnerUp == intent.LabelLiveDataVisual && embedded.RunnerUpScore >= intent.EmbeddingCompositeSecondaryMinScore {
		return intent.ClassificationResult{}, false
	}
	return composeSteerDocumentGenerate(current, embedded)
}

// admitLiveSteerIntoPlan replans a published semantic surface from an
// accepted live steer. Guide launch already promises to re-evaluate the
// plan and tool choice; appending the text while the lookup fence still
// says "do not generate files" makes that promise impossible. The expansion
// is the same child revision a model petition uses, so a steer cannot union
// an unmanaged tool onto the grant.
func (c *sharedAgentLoopCallbacks) admitLiveSteerIntoPlan(steerText string) {
	if c == nil || c.handler == nil || c.semanticSurface == nil || c.semanticSurface.replan == nil {
		return
	}
	if c.sessionTurnDeniesTools() {
		return
	}
	steerText = strings.TrimSpace(steerText)
	if steerText == "" {
		return
	}
	uic := c.handler.getUnifiedClassifier()
	if uic == nil {
		return
	}
	// Classify the steer by itself. The published plan is already the
	// authority for the original utterance. Concatenating them re-embeds a
	// settled lookup ("南京天气" at 0.88, gap 0.14) beside the new half, and
	// a confident lookup early-exits before document_generate is read.
	requestCtx, cancel := semanticRoutingContext(c.loopCtx)
	defer cancel()
	msg := intent.MessageContext{Text: semanticUserIntentText(steerText), UserID: c.userID}
	current := c.semanticSurface.replan.Classification
	embedded := uic.ClassifyEmbeddingOnly(msg)
	normalizeSemanticClassificationForTurn(&embedded)
	verdict, ok := acceptSteerEmbedding(current, embedded)
	if !ok {
		verdict = uic.ClassifyContext(requestCtx, msg)
		normalizeSemanticClassificationForTurn(&verdict)
		// A timed-out tree collapses a document_generate lead to unknown.
		// The published lookup is the missing half, so a write-floor lead
		// that is not a chart contest still composes.
		if len(steerPlanLabels(current, verdict)) == 0 {
			if composed, kept := steerDocumentGenerateAfterTreeMiss(current, embedded, verdict); kept {
				verdict = composed
			}
		}
	}
	labels := steerPlanLabels(current, verdict)
	if len(labels) == 0 {
		log.Printf("[semantic-routing] live steer kept the published plan primary=%s conf=%.2f", verdict.Primary, verdict.Confidence)
		return
	}
	for _, label := range labels {
		if c.semanticSurface == nil || c.semanticSurface.replan == nil {
			return
		}
		if steerLabelAlreadyPlanned(c.semanticSurface.plan, label) {
			continue
		}
		if semanticLabelRequiresFullProfile(label) && c.groupPolicy() != nil {
			log.Printf("[semantic-routing] live steer label %s refused by group policy", label)
			continue
		}
		parent := c.semanticSurface
		child, definitions, err := c.handler.petitionExpandSemanticCallSurface(requestCtx, parent, label)
		if err != nil {
			log.Printf("[semantic-routing] live steer label %s expansion failed: %v", label, err)
			continue
		}
		if parent.removeTurnFence != nil && child.removeTurnFence == nil {
			child.removeTurnFence = parent.removeTurnFence
		}
		c.semanticSurface = child
		c.setVisibleToolDefinitions(definitions)
		// Bind before the prompt rebuild. The full-profile gate reads the
		// turn intent, and the light fence is replaced in the same step.
		if c.loopCtx != nil && child.replan != nil {
			expanded := child.replan.Classification
			bindLoopSemanticIntent(c.loopCtx, &expanded)
		}
		c.leaveLightProfileForUnsafePetition(parent.plan)
		if semanticLabelRequiresFullProfile(label) {
			c.semanticEffectfulPetitionConsumed = true
		} else {
			c.semanticPetitionConsumed = true
		}
		log.Printf("[semantic-routing] live steer admitted label %s into the published plan", label)
	}
}

// replaceConversationSystemMessage writes the rebuilt prompt into the system
// turn the model will actually see. BuildSystemPrompt is not re-read until a
// later tool refresh, so a steer that leaves the light fence has to update
// this request's conversation itself.
func replaceConversationSystemMessage(conversation []interface{}, prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return
	}
	for i, item := range conversation {
		switch msg := item.(type) {
		case map[string]string:
			if msg["role"] != "system" {
				continue
			}
			msg["content"] = prompt
			conversation[i] = msg
			return
		case map[string]interface{}:
			role, _ := msg["role"].(string)
			if role != "system" {
				continue
			}
			msg["content"] = prompt
			conversation[i] = msg
			return
		}
	}
}
