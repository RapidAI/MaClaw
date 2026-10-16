package agentservice

import (
	"sort"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const ambientRetrievalEvidence = "ambient:retrieval"

// ambientDynamicLookupEvidence marks the optional open-family need, kept
// separate from the warehouse evidence above: a lookup turn must be able to
// drop the warehouse companions without dropping an installed provider's
// only path onto the surface.
const ambientDynamicLookupEvidence = "ambient:dynamic-lookup"

// ambientRetrievalCapabilities are read-path warehouse tools that stay on
// managed turns that want ambient retrieval, and on unmanaged chat via a
// retrieval Need path. knowledge.ingest and memory writes are not included.
var ambientRetrievalCapabilities = []coretool.CapabilityID{
	coretool.CapabilityKnowledgeReadLocal,
	coretool.CapabilityMemoryRecallAgent,
}

// AmbientDynamicLookupNeed is the demand side of the open read-only family.
//
// Publishing a contract is only half of "an installed MCP is usable": the
// planner selects a provider only for a need something actually requested, and
// no intent rule maps any label onto information.lookup — by design, because
// a label→capability rule is an owner-reviewed statement about user outcomes,
// and an arbitrary vendor tool is not one. So the open family is demanded the
// same way the warehouse companions are: an optional need attached to turns
// that want ambient retrieval. It is optional and read-only, so it never makes
// a turn fail, never competes with a reviewed provider for a mutation, and
// never enters the external-effect receipt path.
func AmbientDynamicLookupNeed() coretool.CapabilityNeed {
	return coretool.CapabilityNeed{
		ID:          "need:~ambient:" + string(CapabilityInformationLookup),
		Capability:  CapabilityInformationLookup,
		Qualifiers:  map[string]string{QualifierInformationScope: InformationScopeReference},
		Polarity:    coretool.NeedRequire,
		Required:    false,
		Confidence:  1,
		EvidenceIDs: []string{ambientDynamicLookupEvidence},
	}
}

func applyAmbientRetrieval(registry *coretool.CapabilityRegistry, enabled bool, primary intent.IntentLabel, resolution DynamicCapabilityNeedResolution) DynamicCapabilityNeedResolution {
	if !enabled || !resolution.Managed || len(resolution.Needs) == 0 {
		return resolution
	}
	if !intent.WantsAmbientRetrieval(primary) {
		return resolution
	}
	resolution.Needs = AppendAmbientRetrievalNeeds(registry, resolution.Needs)
	return resolution
}

// AppendAmbientRetrievalNeeds adds optional knowledge-read and memory-recall
// needs when the capability is not already present. An empty needs list is a
// valid starting point (unmanaged chat retrieval). Missing providers omit
// rather than fail the turn.
func AppendAmbientRetrievalNeeds(registry *coretool.CapabilityRegistry, needs []coretool.CapabilityNeed) []coretool.CapabilityNeed {
	if registry == nil {
		return needs
	}
	have := make(map[coretool.CapabilityID]bool, len(needs))
	for _, need := range needs {
		have[need.Capability] = true
	}
	out := append([]coretool.CapabilityNeed(nil), needs...)
	for _, capability := range ambientRetrievalCapabilities {
		if have[capability] {
			continue
		}
		if _, exists := registry.Lookup(capability); !exists {
			continue
		}
		out = append(out, coretool.CapabilityNeed{
			ID:          "need:~ambient:" + string(capability),
			Capability:  capability,
			Polarity:    coretool.NeedRequire,
			Required:    false,
			Confidence:  1,
			EvidenceIDs: []string{ambientRetrievalEvidence},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
