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
// rankedCandidates carries the router-ranked names in fused-score order so the
// plan's count/token guards prune the router's own tail instead of rejecting
// the whole surface when pipeline-mandated additions push past the guard.
//
// The bool is false only when the surface includes a dynamic/unmigrated
// definition. Those callers retain snapshot admission until they gain a real
// provider binding; they must not be papered over with a made-up provision.
//
// legacyRoutingMissFloorToolNames is the parent-invariant-11 basic capability
// floor: the desktop agent must always be able to run a command or write a
// file, even on a degraded leftover turn. Keeping them host_policy_required
// stops the closed plan's count guard from pruning them as optional retrieval
// candidates when flat retrieval scores rank them last (mirrors the documented
// floor in semantic_routing_miss.go).
var legacyRoutingMissFloorToolNames = map[string]bool{
	"bash":       true,
	"write_file": true,
}

// unionMissFloorToolsForSurface appends the invariant-11 floor tool
// definitions (bash/write_file) found in baseTools to the current surface when
// absent. baseTools is the loop's plan-rendered base surface, so the union
// keeps every definition digest-bound; it never invents or revives a raw
// registry definition.
func unionMissFloorToolsForSurface(tools, baseTools []map[string]interface{}) []map[string]interface{} {
	seen := make(map[string]bool, len(tools))
	for _, def := range tools {
		seen[extractToolName(def)] = true
	}
	out := tools
	for _, def := range baseTools {
		name := extractToolName(def)
		if !legacyRoutingMissFloorToolNames[name] || seen[name] {
			continue
		}
		out = append(out, def)
		seen[name] = true
	}
	return out
}

func renderReviewedLegacySurface(userText string, definitions []map[string]interface{}, rankedCandidates []string) ([]map[string]interface{}, bool, error) {
	if !legacyDefinitionsHaveLiveProvisions(definitions) {
		return definitions, false, nil
	}
	now := time.Now().UTC()
	candidateRank := make(map[string]int, len(rankedCandidates))
	for i, name := range rankedCandidates {
		if _, ok := candidateRank[name]; !ok {
			candidateRank[name] = i
		}
	}
	evidence := make([]tool.RoutingEvidence, 0, len(definitions))
	for _, definition := range definitions {
		name := strings.TrimSpace(extractToolName(definition))
		provision, ok := tool.LegacyAdapterProvisionForTool(name, now)
		if !ok {
			return nil, false, fmt.Errorf("legacy definition %q lost its reviewed provision", name)
		}
		reason := "host_policy_required"
		score := 1.0
		if tool.LegacyBootstrapToolNames[name] {
			reason = "bootstrap"
		} else if rank, ranked := candidateRank[name]; ranked && !legacyRoutingMissFloorToolNames[name] {
			reason = "retrieval_candidate"
			score = float64(len(rankedCandidates) - rank)
		}
		evidence = append(evidence, tool.RoutingEvidence{
			ToolName: name, Capability: provision.Capability, AdapterContract: provision.AdapterContract,
			Reason: reason, Score: score,
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
func (h *IMMessageHandler) renderClosedLegacyReplacementSurface(policyText string, ctx *LoopContext, definitions []map[string]interface{}, rankedCandidates []string) ([]map[string]interface{}, []string, bool, error) {
	// The initial route filters policy-rejected tools in prepareAgentLoopTools;
	// injection and recovery enter here directly, so the closed boundary must
	// apply the same name-level policy fence or a mid-loop refresh could
	// reintroduce a tool that can never execute (e.g. bash under a mandated
	// sandbox). prepareAgentLoopTools' earlier pass makes this idempotent.
	hostDefinitions := h.filterPolicyRejectedSurfaceTools(h.legacyHostDefinitionsForReplacement(ctx, definitions))
	rendered, planBacked, err := renderReviewedLegacySurface(policyText, hostDefinitions, rankedCandidates)
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
