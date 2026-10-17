package guiapp

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// toolHeadArtifactBoundCapabilities are reviewed names whose later binder
// rejects the whole turn when the artifact is absent. The head must not mint
// them from a vector: document read, file delivery, and speech-to-text each
// fail closed without an input the intent pass already declared.
var toolHeadArtifactBoundCapabilities = map[tool.CapabilityID]bool{
	"document.read.local":                true,
	"artifact.deliver.current_channel":   true,
	"artifact.deliver.specified_target":  true,
	tool.CapabilityAudioTranscribeSpeech: true,
}

// toolHeadPrivilegedReadCapabilities are read-only in the rule table and
// still open a private store. A theta-level hit must not mint them; they
// use the same probability floor as an effectful grant.
var toolHeadPrivilegedReadCapabilities = map[tool.CapabilityID]bool{
	tool.CapabilityMessageReadMailbox: true,
	tool.CapabilitySecurityAuditRead:  true,
}

// unionToolHeadNeeds is the semantic half of "intent scope ∪ tool-head top-k".
//
// The head does not raise a privilege the intent plan did not already accept
// unless its own probability meets the same write floor as an L2 grant
// (ReviewedIntentMinimumConfidence). Ordinary read-only capabilities are
// candidates at the head's theta. Mailbox and audit reads use the write
// floor. Every added need is optional, so a light lookup budget still
// keeps its required search and drops the extras.
// Names that need a trusted artifact are not minted here.
func (h *IMMessageHandler) unionToolHeadNeeds(registry *tool.CapabilityRegistry, needs []tool.CapabilityNeed, classification intent.ClassificationResult) []tool.CapabilityNeed {
	if h == nil || h.toolRouter == nil || registry == nil || len(classification.QueryVec) == 0 {
		return needs
	}
	if h.toolRouter.ToolHead() == nil {
		return needs
	}
	scores := h.toolRouter.ToolHeadCandidates(classification.QueryVec)
	if len(scores) == 0 {
		log.Printf("[tool-head] semantic no candidate above theta")
		return needs
	}
	var added, held []string
	unmapped := 0
	for _, score := range scores {
		capability, ok := semanticPetitionableCapabilities[score.Tool]
		if !ok {
			unmapped++
			continue
		}
		if capabilityNeedPresent(needs, capability) {
			continue
		}
		if toolHeadArtifactBoundCapabilities[capability] {
			held = append(held, fmt.Sprintf("%s@%.2f needs-input", score.Tool, score.Prob))
			continue
		}
		label, ok := semanticPetitionLabelForCapability(capability)
		if !ok {
			held = append(held, fmt.Sprintf("%s@%.2f no-rule", score.Tool, score.Prob))
			continue
		}
		readOnly := semanticReadOnlyGovernedLabels[label]
		if (!readOnly || toolHeadPrivilegedReadCapabilities[capability]) && score.Prob < agentservice.ReviewedIntentMinimumConfidence {
			held = append(held, fmt.Sprintf("%s@%.2f<%.2f", score.Tool, score.Prob, agentservice.ReviewedIntentMinimumConfidence))
			continue
		}
		template, ok := toolHeadTemplate(label, capability)
		if !ok {
			held = append(held, fmt.Sprintf("%s@%.2f no-template", score.Tool, score.Prob))
			continue
		}
		if _, exists := registry.Lookup(template.Capability); !exists {
			held = append(held, fmt.Sprintf("%s@%.2f unknown-capability", score.Tool, score.Prob))
			continue
		}
		// A pure lookup leaves web_fetch latent: the first face is search, and
		// a lookup continuation adds the multi-page family. A document or
		// office composite already has to finish a file. The archetype lists
		// fetch and then refuses to infer it from the label; a head score at
		// the write floor is that missing declaration. The reviewed template
		// still supplies the five-call family, and ForceOptional keeps the
		// first call from blocking search.
		if template.Capability == tool.CapabilityInformationFetchWeb && !toolHeadPromotesLatentFetch(classification, score.Prob) {
			held = append(held, fmt.Sprintf("%s@%.2f latent", score.Tool, score.Prob))
			continue
		}
		// Keep the reviewed repeat budget. SSH, browser, and search were
		// raised above one call because a single invocation exhausted the
		// quota mid-task. ForceOptional is what keeps a light lookup from
		// treating the head's addition as mandatory.
		siblings := agentservice.ExpandNeedTemplateSiblings(template, agentservice.ExpandNeedTemplateOptions{
			IDPrefix:      "need:tool-head:",
			Confidence:    score.Prob,
			EvidenceIDs:   []string{"tool-head:" + score.Tool},
			ForceOptional: true,
		})
		needs = append(needs, siblings...)
		added = append(added, fmt.Sprintf("%s:%s@%.2f", score.Tool, capability, score.Prob))
	}
	log.Printf("[tool-head] semantic top=%s@%.2f add=%s hold=%s unmapped=%d",
		scores[0].Tool, scores[0].Prob, toolHeadLogList(added), toolHeadLogList(held), unmapped)
	return needs
}

func toolHeadPromotesLatentFetch(classification intent.ClassificationResult, prob float64) bool {
	if prob < agentservice.ReviewedIntentMinimumConfidence {
		return false
	}
	return classification.HasLabel(intent.LabelDocumentGenerate) || classification.HasLabel(intent.LabelOffice)
}

func toolHeadTemplate(label intent.IntentLabel, capability tool.CapabilityID) (agentservice.IntentCapabilityNeedTemplate, bool) {
	for _, template := range imSemanticIntentRuleSet[label] {
		if template.Capability == capability {
			return template, true
		}
	}
	return agentservice.IntentCapabilityNeedTemplate{}, false
}

func toolHeadLogList(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ",")
}
