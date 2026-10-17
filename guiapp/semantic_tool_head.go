package guiapp

import (
	"context"
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
// candidates at the head's theta. Latent archetype companions, mailbox
// reads, and audit reads use the write floor: theta only makes a legacy
// name visible, and an unlimited semantic budget would select the need.
// Every added need is optional, so a light lookup budget still
// keeps its required search and drops the extras.
// Names that need a trusted artifact are not minted here.
func (h *IMMessageHandler) unionToolHeadNeeds(ctx context.Context, registry *tool.CapabilityRegistry, needs []tool.CapabilityNeed, classification intent.ClassificationResult) []tool.CapabilityNeed {
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
		// A cold plan already declared this capability, so a second family would
		// only raise a ceiling the intent pass already set. A petition re-plan
		// is different: the added label mints a new need id for a capability
		// the parent already published as a tool-head family. Skipping here
		// drops that family, and the expansion validator rejects the child.
		if capabilityNeedPresent(needs, capability) && !hasPetitionParentFamilies(ctx) {
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
		latent := agentservice.LatentArchetypeCapability(capability, agentservice.ArchetypeBundleKey(classification))
		if (!readOnly || latent || toolHeadPrivilegedReadCapabilities[capability]) && score.Prob < agentservice.ReviewedIntentMinimumConfidence {
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
		if len(siblings) == 0 {
			continue
		}
		if capabilityNeedPresent(needs, capability) {
			if !parentPlanHasNeedFamily(ctx, siblings[0].ID) {
				continue
			}
			// Same contract, new id: keep the parent's family. A different
			// qualifier is a real addition (reference search beside current)
			// and stays for the expansion validator.
			needs = dropSameContractFamilies(needs, siblings[0])
		}
		needs = append(needs, siblings...)
		added = append(added, fmt.Sprintf("%s:%s@%.2f", score.Tool, capability, score.Prob))
	}
	log.Printf("[tool-head] semantic top=%s@%.2f add=%s hold=%s unmapped=%d",
		scores[0].Tool, scores[0].Prob, toolHeadLogList(added), toolHeadLogList(held), unmapped)
	return needs
}

func toolHeadTemplate(label intent.IntentLabel, capability tool.CapabilityID) (agentservice.IntentCapabilityNeedTemplate, bool) {
	for _, template := range imSemanticIntentRuleSet[label] {
		if template.Capability == capability {
			return template, true
		}
	}
	return agentservice.IntentCapabilityNeedTemplate{}, false
}

func hasPetitionParentFamilies(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	families, _ := ctx.Value(semanticPetitionParentFamiliesKey{}).(map[string]struct{})
	return len(families) > 0
}

func parentPlanHasNeedFamily(ctx context.Context, needID string) bool {
	if ctx == nil {
		return false
	}
	families, _ := ctx.Value(semanticPetitionParentFamiliesKey{}).(map[string]struct{})
	if len(families) == 0 {
		return false
	}
	_, ok := families[tool.RepeatFamilyKey(needID)]
	return ok
}

// dropSameContractFamilies removes a petition label's re-mint of the same
// capability and qualifiers. The parent already published that contract
// under the tool-head family id.
func dropSameContractFamilies(needs []tool.CapabilityNeed, keep tool.CapabilityNeed) []tool.CapabilityNeed {
	family := tool.RepeatFamilyKey(keep.ID)
	out := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		if need.Capability == keep.Capability && tool.RepeatFamilyKey(need.ID) != family && sameSemanticQualifiers(need.Qualifiers, keep.Qualifiers) {
			continue
		}
		out = append(out, need)
	}
	return out
}

func toolHeadLogList(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ",")
}
