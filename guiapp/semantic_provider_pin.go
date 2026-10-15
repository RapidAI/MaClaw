package guiapp

import (
	"log"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticProviderPins derives this turn's implementation pins from the text
// the user actually sent.
//
// A managed surface never shows an MCP vendor name to the model, so "use the
// web_search_prime MCP" has no route through the model: the capability prompt
// name is shared with the builtin search, and quality ranking decides the
// backend silently. Without a pin the user can neither request that backend nor
// observe it, and the failure mode is a model that reports the tool as missing
// while quietly answering through it.
//
// The derivation is deliberately narrow:
//
//   - Only MCP servers are pinned. A Skill carries its own name in the prompt
//     already, so it has no addressing problem to solve here.
//   - A server is pinned only when the text contains its id, its display name,
//     or a compound vendor tool name that server publishes. Ordinary prose
//     cannot produce a pin: bare single-word tool names are ineligible, and a
//     hit inside a longer identifier is not a reference.
//   - A pinned server pins every capability it publishes. The user named the
//     product, not one of its tools, and a partial pin would leave the rest of
//     the server substitutable behind the user's back.
//
// The result is a set of host-authored narrowing constraints. It introduces no
// provider, exposes no vendor name to the model, and relaxes no lifecycle or
// policy gate — see tool.ProviderPin.
func semanticProviderPins(userText string, providers []tool.ProviderSpec, mcpEntries []agentservice.MCPToolEntry) []tool.ProviderPin {
	if strings.TrimSpace(userText) == "" {
		return nil
	}
	names := semanticMCPServerDisplayNames(mcpEntries)
	// One normalization of the turn text, reused for every installed server.
	matcher := tool.NewProviderReferenceMatcher(userText)
	pins := make([]tool.ProviderPin, 0, len(providers))
	seen := map[tool.CapabilityID]bool{}
	// The reference matcher folds '-' into '_', so one mention can hit two
	// sibling impl spellings (foo-bar and foo_bar). Verbatim occurrences —
	// case-folded but separator-preserving — narrow the folded set to the
	// spelling the user actually typed; with none, every folded match stands.
	matchedImplIDs := map[string]bool{}
	for _, provider := range providers {
		implID := strings.TrimSpace(provider.Binding.ImplementationID)
		if implID != "" && matcher.ToolNames(implID) {
			matchedImplIDs[implID] = true
		}
	}
	if verbatim := verbatimToolNameMentions(userText, matchedImplIDs); len(verbatim) > 0 {
		matchedImplIDs = verbatim
	}
	// A tool-name hit pins that exact implementation: one server can publish
	// several tools sharing a capability (every tool on a server offers the
	// open lookup family), so a provider-wide pin would still leave a sibling
	// tool substitutable. Impl-narrowed pins are emitted first; the
	// provider-wide server-name pins below only fill capabilities the tool
	// match did not already claim, so one capability never carries two pins.
	for _, provider := range providers {
		if strings.TrimSpace(provider.Binding.Kind) != "mcp" {
			continue
		}
		serverID := strings.TrimSpace(provider.Binding.ProviderID)
		implID := strings.TrimSpace(provider.Binding.ImplementationID)
		if serverID == "" || implID == "" || !matchedImplIDs[implID] || matcher.ServerNames(serverID, names[serverID]...) {
			continue
		}
		for _, provision := range provider.Provides {
			capability := provision.Capability
			if capability == "" || seen[capability] {
				continue
			}
			seen[capability] = true
			pins = append(pins, tool.ProviderPin{Capability: capability, Kind: "mcp", ProviderID: serverID, ImplementationID: implID})
		}
	}
	for _, provider := range providers {
		kind := strings.TrimSpace(provider.Binding.Kind)
		if kind != "mcp" {
			continue
		}
		serverID := strings.TrimSpace(provider.Binding.ProviderID)
		if serverID == "" {
			continue
		}
		if !matcher.ServerNames(serverID, names[serverID]...) && !matcher.ToolNames(provider.Binding.ImplementationID) {
			continue
		}
		for _, provision := range provider.Provides {
			capability := provision.Capability
			if capability == "" || seen[capability] {
				continue
			}
			seen[capability] = true
			pins = append(pins, tool.ProviderPin{Capability: capability, Kind: kind, ProviderID: serverID})
		}
	}
	if len(pins) == 0 {
		return nil
	}
	// Stable order keeps the pin set — and therefore the plan that embeds it —
	// reproducible for a given catalog, independent of discovery order.
	sort.Slice(pins, func(i, j int) bool {
		if pins[i].Capability != pins[j].Capability {
			return pins[i].Capability < pins[j].Capability
		}
		return pins[i].ProviderID < pins[j].ProviderID
	})
	return pins
}

// verbatimToolNameMentions narrows folded tool-name matches to the spellings
// that appear in the user's text as written. The reference matcher folds
// '-' into '_', so "foo-bar" also names a sibling impl literally spelled
// "foo_bar" — yet each spelling mints a distinct capability id, and folding
// the hit would pin a tool the user never named (or fail the turn when that
// sibling is degraded). The verbatim fold is case-insensitive only: '-' and
// '_' stay distinct, mirroring the slug the capability id derives. An empty
// result means no spelling appeared verbatim and the caller keeps every
// folded match.
func verbatimToolNameMentions(userText string, matchedImplIDs map[string]bool) map[string]bool {
	lowered := strings.ToLower(userText)
	out := make(map[string]bool, len(matchedImplIDs))
	for implID := range matchedImplIDs {
		if strings.Contains(lowered, strings.ToLower(implID)) {
			out[implID] = true
		}
	}
	return out
}

// semanticMCPServerDisplayNames maps each server id to its display name. A
// user who installed a product usually types the name they saw in the catalog,
// not the opaque id the control plane assigned.
func semanticMCPServerDisplayNames(entries []agentservice.MCPToolEntry) map[string][]string {
	if len(entries) == 0 {
		return nil
	}
	names := make(map[string][]string, len(entries))
	for _, entry := range entries {
		serverID := strings.TrimSpace(entry.ServerID)
		serverName := strings.TrimSpace(entry.ServerName)
		if serverID == "" || serverName == "" || serverName == serverID {
			continue
		}
		duplicate := false
		for _, existing := range names[serverID] {
			if existing == serverName {
				duplicate = true
				break
			}
		}
		if !duplicate {
			names[serverID] = append(names[serverID], serverName)
		}
	}
	return names
}

// semanticPinnedProviderLabels lists the display names of the servers this turn
// pinned, so a refusal can name what the user asked for instead of repeating a
// capability the user never mentioned.
func semanticPinnedProviderLabels(pins []tool.ProviderPin, providers []tool.ProviderSpec, mcpEntries []agentservice.MCPToolEntry) []string {
	if len(pins) == 0 {
		return nil
	}
	names := semanticMCPServerDisplayNames(mcpEntries)
	wanted := map[string]bool{}
	for _, pin := range pins {
		wanted[pin.ProviderID] = true
	}
	labels := make([]string, 0, len(wanted))
	for _, provider := range providers {
		serverID := strings.TrimSpace(provider.Binding.ProviderID)
		if !wanted[serverID] {
			continue
		}
		label := strings.TrimSpace(serverID)
		if candidates := names[serverID]; len(candidates) > 0 {
			label = candidates[0]
		}
		if !semanticStringInList(label, labels) {
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)
	return labels
}

func semanticStringInList(value string, list []string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// semanticNamedProviderNeeds mints the demand side of exact tool naming.
// semanticProviderPins narrows selection among providers of capabilities the
// turn already needed; it cannot create a need for a capability no intent
// rule ever declares. When the user names an installed vendor tool by its
// compound spelling (search_docs, create_issue — bare one-word names are
// never eligible), the host mints an exact x:mcp:<server>:<tool> need plus
// the pin that binds it, so "use the X tool" reaches that binding and only
// that binding.
//
// The need is Required: an explicitly named implementation that cannot be
// selected must surface as pinned_provider_unavailable, not be silently
// omitted while the turn answers through a sibling.
//
// The registry is seeded here rather than at publication time because the
// planning registry is rebuilt per turn: the capability vocabulary each plan
// resolves against must be re-derived from this turn's own provider set.
// semanticExternalCapabilityVocabulary collects the x: capability IDs a
// provider set publishes. Shared by the pre-publish registry seed and the
// named-provider pin path: both must see the same vocabulary.
func semanticExternalCapabilityVocabulary(providers []tool.ProviderSpec) []tool.CapabilityID {
	caps := make([]tool.CapabilityID, 0, len(providers))
	for _, provider := range providers {
		for _, provision := range provider.Provides {
			if strings.HasPrefix(string(provision.Capability), string(tool.ExternalCapabilityPrefix)) {
				caps = append(caps, provision.Capability)
			}
		}
	}
	return caps
}

func semanticNamedProviderNeeds(userText string, registry *tool.CapabilityRegistry, providers []tool.ProviderSpec) ([]tool.CapabilityNeed, []tool.ProviderPin) {
	if strings.TrimSpace(userText) == "" || registry == nil {
		return nil, nil
	}
	matcher := tool.NewProviderReferenceMatcher(userText)
	// Seed every external capability this turn's providers publish, named or
	// not: the vocabulary a plan resolves against must contain the caps the
	// contracts granted, and seeding is idempotent.
	allExternal := semanticExternalCapabilityVocabulary(providers)
	type namedBinding struct {
		serverID string
		implID   string
	}
	matched := make([]namedBinding, 0, len(providers))
	matchedImplIDs := map[string]bool{}
	for _, provider := range providers {
		serverID := strings.TrimSpace(provider.Binding.ProviderID)
		implID := strings.TrimSpace(provider.Binding.ImplementationID)
		if strings.TrimSpace(provider.Binding.Kind) != "mcp" || serverID == "" || implID == "" {
			continue
		}
		if matcher.ToolNames(implID) {
			matched = append(matched, namedBinding{serverID: serverID, implID: implID})
			matchedImplIDs[implID] = true
		}
	}
	// Same fold disambiguation as semanticProviderPins: a verbatim spelling
	// occurrence keeps only the impls the user actually typed.
	if verbatim := verbatimToolNameMentions(userText, matchedImplIDs); len(verbatim) > 0 {
		narrowed := matched[:0]
		for _, binding := range matched {
			if verbatim[binding.implID] {
				narrowed = append(narrowed, binding)
			}
		}
		matched = narrowed
	}
	if len(allExternal) > 0 {
		if err := agentservice.EnsureExternalCapabilityVocabulary(registry, allExternal); err != nil {
			log.Printf("[semantic-routing] external capability vocabulary seeding incomplete: %v", err)
		}
	}
	if len(matched) == 0 {
		return nil, nil
	}
	namedSeen := map[tool.CapabilityID]bool{}
	needs := make([]tool.CapabilityNeed, 0, len(matched))
	pins := make([]tool.ProviderPin, 0, len(matched))
	for _, binding := range matched {
		capID := agentservice.AutoMCPCapabilityIDForBinding(binding.serverID, binding.implID)
		if capID == "" || namedSeen[capID] {
			continue
		}
		namedSeen[capID] = true
		needs = append(needs, tool.CapabilityNeed{
			ID:          "need:~named:" + string(capID),
			Capability:  capID,
			Polarity:    tool.NeedRequire,
			Required:    true,
			Confidence:  1,
			EvidenceIDs: []string{"named:mcp_tool"},
		})
		pins = append(pins, tool.ProviderPin{Capability: capID, Kind: "mcp", ProviderID: binding.serverID, ImplementationID: binding.implID})
	}
	sort.Slice(needs, func(i, j int) bool { return needs[i].ID < needs[j].ID })
	sort.Slice(pins, func(i, j int) bool { return pins[i].Capability < pins[j].Capability })
	return needs, pins
}
