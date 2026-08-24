package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// legacyDefinitionsHaveLiveProvisions identifies a migration-complete static
// GUI surface. Dynamic client/MCP/skill tools deliberately make it false: they
// need a provider-bound semantic selection rather than a name provision.
func legacyDefinitionsHaveLiveProvisions(definitions []map[string]interface{}) bool {
	return len(legacyDefinitionsWithoutLiveProvisions(definitions)) == 0
}

func legacyDefinitionsWithoutLiveProvisions(definitions []map[string]interface{}) []string {
	now := time.Now().UTC()
	missing := make([]string, 0)
	for _, definition := range definitions {
		name := strings.TrimSpace(extractToolName(definition))
		if name == "" {
			missing = append(missing, "(unnamed)")
			continue
		}
		if _, ok := tool.LegacyAdapterProvisionForTool(name, now); !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func legacyDefinitionHasLiveProvision(definition map[string]interface{}) bool {
	name := strings.TrimSpace(extractToolName(definition))
	_, ok := tool.LegacyAdapterProvisionForTool(name, time.Now().UTC())
	return ok
}

// renderReviewedLegacySurface makes the final legacy model surface pass through
// the immutable plan renderer once every definition is covered by a reviewed
// provision. The host-policy pipeline has already performed its current-turn
// filters at this point, so every selected definition is recorded as a
// host-policy selection; no history or registry-wide fallback is consulted.
//
// The bool is false only when the surface includes a dynamic/unmigrated
// definition. Those callers retain snapshot admission until they gain a real
// provider binding; they must not be papered over with a made-up provision.
func renderReviewedLegacySurface(userText string, definitions []map[string]interface{}) ([]map[string]interface{}, bool, error) {
	if !legacyDefinitionsHaveLiveProvisions(definitions) {
		return definitions, false, nil
	}
	now := time.Now().UTC()
	evidence := make([]tool.RoutingEvidence, 0, len(definitions))
	for _, definition := range definitions {
		name := strings.TrimSpace(extractToolName(definition))
		provision, ok := tool.LegacyAdapterProvisionForTool(name, now)
		if !ok {
			return nil, false, fmt.Errorf("legacy definition %q lost its reviewed provision", name)
		}
		reason := "host_policy_required"
		if tool.LegacyBootstrapToolNames[name] {
			reason = "bootstrap"
		}
		evidence = append(evidence, tool.RoutingEvidence{
			ToolName: name, Capability: provision.Capability, AdapterContract: provision.AdapterContract,
			Reason: reason, Score: 1,
		})
	}
	plan, err := tool.BuildLegacyAdapterPlan(tool.LegacyAdapterPlanInput{
		Recommendation: tool.RoutingRecommendation{SearchQuery: strings.TrimSpace(userText), Confidence: 1, Evidence: evidence},
		Definitions:    definitions,
		PolicyDigest:   legacyAdapterSurfacePolicyDigest(userText, definitions),
		Now:            now,
	})
	if err != nil {
		return nil, false, err
	}
	rendered, err := tool.RenderLegacyAdapterPlan(plan, definitions, now)
	if err != nil {
		return nil, false, err
	}
	return rendered, true, nil
}

// renderClosedLegacyReplacementSurface is the final replacement boundary for
// every unmanaged host-tool refresh (initial route, injection and recovery).
// A reviewed LegacyAdapterPlan is required for every host definition.  In
// particular, a renderer error or an unprovisioned definition must not fall
// back to the raw routed slice: doing that would let a recovery-only filter or
// a newly discovered registry name become model authority without a plan.
//
// Client tools are intentionally outside the legacy provision catalog. They
// are separately bound to ClientToolContext for this request, so they are
// added only after the host replacement surface is closed and only if they do
// not collide with an exposed host name.
func (h *IMMessageHandler) renderClosedLegacyReplacementSurface(policyText string, ctx *LoopContext, definitions []map[string]interface{}) ([]map[string]interface{}, []string, bool, error) {
	hostDefinitions := h.legacyHostDefinitionsForReplacement(ctx, definitions)
	rendered, planBacked, err := renderReviewedLegacySurface(policyText, hostDefinitions)
	if err != nil {
		return nil, nil, false, err
	}
	if !planBacked {
		return nil, nil, false, fmt.Errorf("catalog_incomplete: legacy replacement contains unprovisioned host definitions %q", legacyDefinitionsWithoutLiveProvisions(hostDefinitions))
	}
	clientDefinitions := clientToolDefinitionsForAgent(ctx, rendered)
	clientNames := agentLoopToolNamesForLog(clientDefinitions)
	return append(rendered, clientDefinitions...), clientNames, true, nil
}

// legacyHostDefinitionsForReplacement removes only client definitions that
// were carried over from a previous request surface.  The host catalog wins a
// same-name collision; all other client names are re-materialized from the
// current ClientToolContext after the host plan is rendered.  This prevents a
// stale client tool from making the host surface look "unprovisioned" while
// also preventing it from surviving a replacement by name alone.
func (h *IMMessageHandler) legacyHostDefinitionsForReplacement(ctx *LoopContext, definitions []map[string]interface{}) []map[string]interface{} {
	if ctx == nil || ctx.ClientToolContext == nil || len(ctx.ClientTools) == 0 || len(definitions) == 0 {
		return definitions
	}
	clientNames := make(map[string]bool, len(ctx.ClientTools))
	for _, definition := range ctx.ClientTools {
		if name := strings.TrimSpace(definition.Name); name != "" {
			clientNames[name] = true
		}
	}
	hostNames := make(map[string]bool)
	if h != nil {
		for _, definition := range h.getTools() {
			if name := strings.TrimSpace(extractToolName(definition)); name != "" {
				hostNames[name] = true
			}
		}
		if h.registry != nil {
			for _, definition := range h.registry.ListAvailable() {
				if name := strings.TrimSpace(definition.Name); name != "" {
					hostNames[name] = true
				}
			}
		}
	}
	hostDefinitions := make([]map[string]interface{}, 0, len(definitions))
	for _, definition := range definitions {
		name := strings.TrimSpace(extractToolName(definition))
		// A reviewed legacy provision is also sufficient evidence that this is
		// a host definition when a minimal/test host has no registry snapshot.
		if clientNames[name] && !hostNames[name] && !legacyDefinitionHasLiveProvision(definition) {
			continue
		}
		hostDefinitions = append(hostDefinitions, definition)
	}
	return hostDefinitions
}

func legacyAdapterSurfacePolicyDigest(userText string, definitions []map[string]interface{}) string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if name := strings.TrimSpace(extractToolName(definition)); name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	// BuildLegacyAdapterPlan needs a stable host policy identity, rather than a
	// mutable list pointer. The plan's own definition digests bind the schemas.
	sum := sha256.Sum256([]byte("legacy-gui-surface-v1\n" + strings.TrimSpace(userText) + "\n" + strings.Join(names, ",")))
	return "legacy-gui-surface-" + hex.EncodeToString(sum[:16])
}
