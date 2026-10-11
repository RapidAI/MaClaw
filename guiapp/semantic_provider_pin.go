package guiapp

import (
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
