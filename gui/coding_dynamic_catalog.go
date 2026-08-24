package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingDynamicCatalogSnapshot is the G2 bridge from a verified Coding
// identity to the common dynamic semantic catalog. It deliberately has no
// BM25 match, function alias, or direct dispatcher: task matching may later
// supply planner evidence, but only a contract-backed inventory can supply a
// provider binding.
type codingDynamicCatalogSnapshot struct {
	Catalog  agentservice.DynamicSemanticCatalog
	Coverage tool.CatalogCoverage
}

// codingDynamicPlanPreparation is the non-materialized G2 result. It is
// deliberately not a request surface: no grant, alias, definition list or
// execution callback is created here. Its only purpose is to prove that a
// contract-backed Coding inventory can be consumed by the common ToolCatalog
// and ToolPlanner without consulting matchedSkills/matchedMCPTools.
//
// G3 owns publication/materialization. Keeping this object below that
// boundary prevents a partial migration from accidentally making a planner
// result executable through the old Coding callback map.
type codingDynamicPlanPreparation struct {
	Catalog tool.ToolCatalogSnapshot
	Plan    tool.ToolPlan
}

// codingDynamicCatalogForIdentity reads a principal/tenant-scoped inventory
// through the same contract registry used by the managed GUI semantic surface.
// The identity must already have been recovered from the durable runtime
// anchor. A missing contract, incomplete lifecycle observation, or an empty
// catalog is represented as catalog_incomplete; callers must not fall back to
// matchedSkills/matchedMCPTools or a generic gateway.
func (h *IMMessageHandler) codingDynamicCatalogForIdentity(ctx context.Context, identity *trustedCodingInvocationIdentity) (codingDynamicCatalogSnapshot, error) {
	if h == nil || identity == nil || !identity.complete() {
		return codingDynamicCatalogSnapshot{Coverage: semanticDynamicCoverage("catalog_incomplete", "catalog_incomplete")}, nil
	}
	principal := agentservice.Principal{TenantID: strings.TrimSpace(identity.TenantID), UserID: strings.TrimSpace(identity.PrincipalID)}
	inventory, err := h.semanticDynamicInventoryForPrincipal(ctx, principal)
	if err != nil {
		return codingDynamicCatalogSnapshot{Coverage: semanticDynamicCoverage("catalog_incomplete", "catalog_incomplete")}, fmt.Errorf("load coding dynamic inventory: %w", err)
	}
	if inventory.coverage.State != tool.CatalogCoverageComplete {
		return codingDynamicCatalogSnapshot{Coverage: inventory.coverage}, nil
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog(inventory.mcpEntries, inventory.skillEntries)
	if err != nil {
		return codingDynamicCatalogSnapshot{Coverage: semanticDynamicCoverage("catalog_incomplete", "catalog_incomplete")}, fmt.Errorf("build coding dynamic catalog: %w", err)
	}
	if len(catalog.Providers) == 0 {
		return codingDynamicCatalogSnapshot{Coverage: semanticDynamicCoverage("catalog_incomplete", "catalog_incomplete")}, nil
	}
	return codingDynamicCatalogSnapshot{Catalog: catalog, Coverage: inventory.coverage}, nil
}

func (s codingDynamicCatalogSnapshot) complete() bool {
	return s.Coverage.State == tool.CatalogCoverageComplete && len(s.Catalog.Providers) > 0
}

const codingDynamicCapabilityNeedEvidence = "host:coding-capability-policy:v1"

// codingDynamicCapabilityNeeds is the R2 host-owned demand side of Coding
// dynamic planning. It intentionally derives needs only from the reviewed
// Coding outcome policy, not from task text, BM25 scores, matched Skill/MCP
// entries, prompt metadata, or provider names. A dynamic provider may satisfy
// one of these needs only after its independently published contract declares
// that same capability.
//
// The sibling expansion mirrors IntentLabelCapabilityNeedResolver: an
// iterative coding budget is represented by individually planned selections,
// never by a runtime counter or a provider-selected repeat policy. Keeping the
// identity construction here deterministic makes plan/grant/journal lineage
// independent of transient discovery state.
func codingDynamicCapabilityNeeds() []tool.CapabilityNeed {
	return codingDynamicCapabilityNeedsFromTemplates(semanticCodingCapabilityRule)
}

// codingDynamicCapabilityNeedsFromTemplates exists for focused policy tests.
// Production callers must use codingDynamicCapabilityNeeds, whose only input
// is the reviewed static Coding policy above.
func codingDynamicCapabilityNeedsFromTemplates(templates []agentservice.IntentCapabilityNeedTemplate) []tool.CapabilityNeed {
	needs := make([]tool.CapabilityNeed, 0, len(templates))
	seen := make(map[string]bool, len(templates))
	for _, template := range templates {
		capability := tool.CapabilityID(strings.TrimSpace(string(template.Capability)))
		if capability == "" {
			// A malformed host policy must reach the common planner as an
			// explicit unmet need, never be silently omitted as though a
			// dynamic provider did not happen to match.
			capability = "invalid.coding.capability"
		}
		polarity := template.Polarity
		if polarity == "" {
			polarity = tool.NeedRequire
		}
		qualifiers := cloneCodingDynamicNeedQualifiers(template.Qualifiers)
		key := string(capability) + "\x00" + string(polarity) + "\x00" + codingDynamicNeedQualifierKey(qualifiers)
		if seen[key] {
			continue
		}
		seen[key] = true
		baseID := "need:coding:" + string(capability) + ":" + tool.SchemaDigest([]byte(key))[:12]
		for index := 0; index < tool.RepeatSiblingBudget(template.MaxInvocations); index++ {
			needs = append(needs, tool.CapabilityNeed{
				ID:          tool.RepeatSiblingNeedID(baseID, index),
				Capability:  capability,
				Qualifiers:  cloneCodingDynamicNeedQualifiers(qualifiers),
				Polarity:    polarity,
				Required:    template.Required,
				Confidence:  1,
				EvidenceIDs: []string{codingDynamicCapabilityNeedEvidence},
			})
		}
	}
	return needs
}

func codingDynamicNeedQualifierKey(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, strings.TrimSpace(key)+"="+strings.TrimSpace(values[key]))
	}
	return strings.Join(parts, "\x1f")
}

func cloneCodingDynamicNeedQualifiers(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

// prepareCodingDynamicSemanticPlan converts host-governed capability needs
// into a common immutable ToolPlan. The caller supplies only already-trusted
// needs, facts and constraints; textual routing, BM25 scores, matched provider
// names and model arguments have no input path here.
//
// This is intentionally a preparation seam, not G3 materialization. A caller
// may inspect Plan.Unmet to return catalog_incomplete/replan, but it must not
// render a provider definition or dispatch a selection from this result.
func prepareCodingDynamicSemanticPlan(identity *trustedCodingInvocationIdentity, dynamic codingDynamicCatalogSnapshot, needs []tool.CapabilityNeed, facts []tool.RoutingFact, constraints []tool.RoutingConstraint, budget tool.PlanningBudget, now time.Time) (codingDynamicPlanPreparation, error) {
	if identity == nil || !identity.complete() {
		return codingDynamicPlanPreparation{}, fmt.Errorf("coding dynamic identity is incomplete")
	}
	if !dynamic.complete() {
		return codingDynamicPlanPreparation{}, fmt.Errorf("coding dynamic catalog is incomplete")
	}
	if len(needs) == 0 {
		return codingDynamicPlanPreparation{}, fmt.Errorf("coding dynamic capability needs are required")
	}
	registry := newIMSemanticCapabilityRegistry()
	catalog := tool.NewToolCatalog(registry)
	snapshot, err := catalog.PublishWithCoverage(dynamic.Catalog.Providers, dynamic.Coverage, now)
	if err != nil {
		return codingDynamicPlanPreparation{}, fmt.Errorf("publish coding dynamic catalog: %w", err)
	}
	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID:   identity.RootTaskID,
		SessionID:    identity.SessionID,
		TurnID:       identity.TurnID,
		ChannelScope: "coding",
		Snapshot:     snapshot,
		Needs:        append([]tool.CapabilityNeed(nil), needs...),
		Facts:        append([]tool.RoutingFact(nil), facts...),
		Constraints:  append([]tool.RoutingConstraint(nil), constraints...),
		Budget:       budget,
		Now:          now,
	})
	if err != nil {
		return codingDynamicPlanPreparation{}, fmt.Errorf("plan coding dynamic capability: %w", err)
	}
	return codingDynamicPlanPreparation{Catalog: snapshot, Plan: plan}, nil
}

// prepareCodingDynamicSemanticPlanForIdentity is the host adapter used by a
// future verified Coding ingress. It intentionally stops before G3's durable
// coordinator publish path; callers must retain the existing fail-closed
// dynamic surface until that lifecycle has been wired end-to-end.
func (h *IMMessageHandler) prepareCodingDynamicSemanticPlanForIdentity(ctx context.Context, identity *trustedCodingInvocationIdentity, needs []tool.CapabilityNeed, facts []tool.RoutingFact, constraints []tool.RoutingConstraint, budget tool.PlanningBudget) (codingDynamicPlanPreparation, error) {
	dynamic, err := h.codingDynamicCatalogForIdentity(ctx, identity)
	if err != nil {
		return codingDynamicPlanPreparation{}, err
	}
	return prepareCodingDynamicSemanticPlan(identity, dynamic, needs, facts, constraints, budget, time.Now().UTC())
}

// prepareCodingDynamicSemanticPlanForVerifiedCodingTask is the production R2
// adapter. Unlike the lower-level preparation seam, it accepts no provider
// candidate, match list, task text, or caller-supplied needs: the reviewed
// Coding policy is the sole source of desired capabilities. It still stops
// before G3 materialization, so a complete plan here cannot make an alias or
// dispatch path reachable.
func (h *IMMessageHandler) prepareCodingDynamicSemanticPlanForVerifiedCodingTask(ctx context.Context, identity *trustedCodingInvocationIdentity, facts []tool.RoutingFact, constraints []tool.RoutingConstraint, budget tool.PlanningBudget) (codingDynamicPlanPreparation, error) {
	return h.prepareCodingDynamicSemanticPlanForIdentity(ctx, identity, codingDynamicCapabilityNeeds(), facts, constraints, budget)
}
