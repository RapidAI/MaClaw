package agentservice

import (
	"fmt"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// autoMCPCapabilityPrefix is the governed external-capability form every
// installed MCP tool receives: x:mcp:<server-slug>:<tool-slug>. The namespace
// segment is "mcp" — shared across installed servers — so the default
// namespace ceiling (read_only + external_effect) bounds every auto-issued
// capability identically, and SetExternalCapabilityCeiling("mcp", ...) is the
// single host knob that widens the whole installed family.
const autoMCPCapabilityPrefix = "x:mcp:"

// autoMCPSlugMaxLen bounds one identity segment. Server ids and vendor tool
// names are unbounded input; a slug long enough to carry a whole name would
// make capability IDs arbitrarily large (they are embedded in needs, plan
// digests, and stable binding keys).
const autoMCPSlugMaxLen = 64

// maxAutoMCPCapabilitiesPerServer bounds how many per-tool capabilities one
// observation may mint. The limit exists so a hostile or broken server that
// reports thousands of tools cannot flood the governed vocabulary; the first
// N tools (sorted by name, so the cut is deterministic) keep theirs.
const maxAutoMCPCapabilitiesPerServer = 256

// autoMCPSlugSegment folds one server/tool identity into a safe capability-ID
// segment. Colons are structural in a capability ID (they delimit the x:
// namespace and the trailing segment), so they — and every other byte outside
// [a-z0-9._-] — fold to '_'. An input that produces nothing (empty, or all
// punctuation/CJK) still yields a deterministic digest-based slug so the tool
// stays addressable instead of silently losing its capability.
func autoMCPSlugSegment(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	slug := strings.Trim(b.String(), "._-")
	if len(slug) > autoMCPSlugMaxLen {
		// Truncated slugs can collide; a short digest of the full input keeps
		// the mapping injective for practical purposes and deterministic.
		slug = strings.TrimRight(slug[:autoMCPSlugMaxLen-9], "._-") + "~" + coretool.SchemaDigest([]byte(raw))[:8]
	}
	if slug == "" {
		slug = "t" + coretool.SchemaDigest([]byte(raw))[:12]
	}
	return slug
}

// AutoMCPCapabilityID derives the exact per-tool capability for one installed
// MCP tool. It is a pure function of (serverID, toolName): the publisher stamps
// it into the contract provisions, and the turn-time need builder derives the
// same ID when the user names that tool, without either side exchanging state.
func AutoMCPCapabilityID(serverID, toolName string) (coretool.CapabilityID, error) {
	server := autoMCPSlugSegment(serverID)
	tool := autoMCPSlugSegment(toolName)
	if server == "" || tool == "" {
		return "", fmt.Errorf("auto MCP capability requires server and tool identity")
	}
	return coretool.CapabilityID(autoMCPCapabilityPrefix + server + ":" + tool), nil
}

// ExternalCapabilityNamespaceOf returns the namespace segment of an external
// capability ID: the token between the x: prefix and the next colon. Exported
// so hosts seeding a planning registry derive the same namespace the publisher
// used at registration time.
func ExternalCapabilityNamespaceOf(id coretool.CapabilityID) string {
	return externalNamespaceOf(id)
}

// AutoMCPCapabilityIDForBinding is AutoMCPCapabilityID without the error:
// binding identity was already validated non-empty upstream, so a missing slug
// can only come from an empty input and yields "" rather than a route.
func AutoMCPCapabilityIDForBinding(serverID, toolName string) coretool.CapabilityID {
	id, err := AutoMCPCapabilityID(serverID, toolName)
	if err != nil {
		return ""
	}
	return id
}

// EnsureExternalCapabilityVocabulary registers the governed descriptor for
// every x: capability ID that is not already present in registry. A planning
// registry is constructed per turn and sealed, while dynamic contracts (and
// the auto per-tool capabilities embedded in them) are written by the
// control-plane publisher at observation time — possibly a previous process.
// Seeding the vocabulary from the provider set each turn is how a need can
// name a capability the publisher registered long ago without sharing
// registry instances between publication and planning.
//
// Descriptors are stamped identically to what the publisher writes (namespace
// ceiling effects, vendor owner, v1), so a capability that exists on both
// paths never produces two conflicting governed definitions.
func EnsureExternalCapabilityVocabulary(registry *coretool.CapabilityRegistry, capabilities []coretool.CapabilityID) error {
	if registry == nil {
		return fmt.Errorf("external capability vocabulary requires a registry")
	}
	for _, capID := range capabilities {
		capID = coretool.CapabilityID(strings.TrimSpace(string(capID)))
		if !strings.HasPrefix(string(capID), string(coretool.ExternalCapabilityPrefix)) {
			continue
		}
		if _, exists := registry.Lookup(capID); exists {
			continue
		}
		ns := externalNamespaceOf(capID)
		summary := fmt.Sprintf("Vendor-declared capability %s", capID)
		if strings.HasPrefix(string(capID), autoMCPCapabilityPrefix) {
			summary = fmt.Sprintf("Installed MCP tool capability %s", capID)
		}
		desc := coretool.CapabilityDescriptor{
			ID:      capID,
			Version: "v1",
			Owner:   "vendor",
			Summary: summary,
			Effects: registry.ExternalEffectCeiling(ns),
		}
		if err := registry.RegisterExternalCapability(ns, desc); err != nil {
			return fmt.Errorf("register external capability %q: %w", capID, err)
		}
	}
	return nil
}
