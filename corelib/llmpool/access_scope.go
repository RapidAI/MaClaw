package llmpool

import (
	"strings"
	"unicode"
)

// NormalizeAllowedNodeIDs trims, drops empties, and de-duplicates node IDs
// while preserving first-seen case and order. An empty result means "all nodes".
func NormalizeAllowedNodeIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ParseAllowedNodeList splits a human list such as "hc-1, hc-2,hc-3" into
// node ids. Commas, Chinese commas, semicolons, and whitespace all separate ids.
func ParseAllowedNodeList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == '，' || r == ';' || r == '；'
	})
	return NormalizeAllowedNodeIDs(parts)
}

// ProviderAllowedOnNode reports whether this HubCenter node may call the
// provider's upstream API. An empty allowlist allows every node.
func ProviderAllowedOnNode(provider ProviderConfig, nodeID string) bool {
	return NodeIDAllowed(provider.AllowedNodeIDs, nodeID)
}

// NodeIDAllowed reports whether nodeID is in allowlist. Empty allowlist
// allows every node, including an empty node ID (single-node / HA off).
func NodeIDAllowed(allowlist []string, nodeID string) bool {
	if len(allowlist) == 0 {
		return true
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return false
	}
	for _, id := range allowlist {
		if strings.EqualFold(strings.TrimSpace(id), nodeID) {
			return true
		}
	}
	return false
}
