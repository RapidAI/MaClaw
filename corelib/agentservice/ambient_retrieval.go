package agentservice

import (
	"sort"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const ambientRetrievalEvidence = "ambient:retrieval"

// ambientRetrievalCapabilities are read-path warehouse tools that stay on
// managed turns that want ambient retrieval, and on unmanaged chat via a
// retrieval Need path. knowledge.ingest and memory writes are not included.
var ambientRetrievalCapabilities = []coretool.CapabilityID{
	coretool.CapabilityKnowledgeReadLocal,
	coretool.CapabilityMemoryRecallAgent,
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
