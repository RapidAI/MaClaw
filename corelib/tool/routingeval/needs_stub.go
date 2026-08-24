package routingeval

import (
	"fmt"

	tool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// labelStubEntry maps one preset intent label onto a capability contract.
// The stub is a deterministic, dictionary-based placeholder for the host UIC:
// the harness deliberately does not classify free text, so samples that
// exercise need recognition supply the label their input text would produce.
type labelStubEntry struct {
	capability tool.CapabilityID
	qualifiers map[string]string
}

// labelStubTable is intentionally small and lives next to the runner so a new
// sample family can extend the vocabulary in one place.
var labelStubTable = map[string]labelStubEntry{
	"read_file":               {capability: tool.CapabilityFSReadLocal},
	"write_file":              {capability: tool.CapabilityFSWriteLocal},
	"capture_screen":          {capability: CapabilityEvalCaptureDesktop, qualifiers: map[string]string{"display": "primary"}},
	"send_message":            {capability: tool.CapabilityMessageSendIM, qualifiers: map[string]string{"format": "text"}},
	"fetch_web":               {capability: tool.CapabilityInformationFetchWeb},
	"run_shell":               {capability: tool.CapabilityShellExecuteLocal},
	"search_knowledge":        {capability: tool.CapabilityKnowledgeReadLocal},
	"deliver_current_channel": {capability: CapabilityEvalDeliverCurrentChannel, qualifiers: map[string]string{"format": "image"}},
	"live_data":               {capability: CapabilityEvalSearchWeb, qualifiers: map[string]string{"freshness": "current"}},
	"document_generate":       {capability: CapabilityEvalGenerateDocument, qualifiers: map[string]string{"format": "pdf"}},
	"deliver_current_file":    {capability: CapabilityEvalDeliverCurrentChannel, qualifiers: map[string]string{"format": "file"}},
}

// deriveNeeds maps sample labels onto typed needs. Label qualifiers override
// the table defaults; polarity defaults to require. Needs are named
// "need-<label>" so expected selection assertions stay readable.
func deriveNeeds(labels []LabelSpec) ([]tool.CapabilityNeed, error) {
	needs := make([]tool.CapabilityNeed, 0, len(labels))
	seen := make(map[string]bool, len(labels))
	for _, spec := range labels {
		entry, ok := labelStubTable[spec.Label]
		if !ok {
			return nil, fmt.Errorf("unknown intent label %q", spec.Label)
		}
		if seen[spec.Label] {
			return nil, fmt.Errorf("duplicate intent label %q", spec.Label)
		}
		seen[spec.Label] = true
		polarity := tool.NeedPolarity(spec.Polarity)
		if polarity == "" {
			polarity = tool.NeedRequire
		}
		qualifiers := make(map[string]string, len(entry.qualifiers)+len(spec.Qualifiers))
		for name, value := range entry.qualifiers {
			qualifiers[name] = value
		}
		for name, value := range spec.Qualifiers {
			qualifiers[name] = value
		}
		needs = append(needs, tool.CapabilityNeed{
			ID:         "need-" + spec.Label,
			Capability: entry.capability,
			Qualifiers: qualifiers,
			Polarity:   polarity,
			Required:   polarity == tool.NeedRequire,
		})
	}
	return needs, nil
}

// needKey canonicalises a need for the set comparison used by the needs-mode
// precision/recall metric.
func needKey(capability tool.CapabilityID, qualifiers map[string]string, polarity tool.NeedPolarity) string {
	return string(capability) + "|" + string(polarity) + "|" + canonicalQualifiers(qualifiers)
}

func canonicalQualifiers(qualifiers map[string]string) string {
	if len(qualifiers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(qualifiers))
	for key := range qualifiers {
		keys = append(keys, key)
	}
	// Insertion sort: qualifier maps in this harness never exceed a handful of
	// entries and avoiding a sort import here keeps the stub dependency-free.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	out := ""
	for i, key := range keys {
		if i > 0 {
			out += ","
		}
		out += key + "=" + qualifiers[key]
	}
	return out
}
