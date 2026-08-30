package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestCodingDynamicCatalogRequiresVerifiedIdentity(t *testing.T) {
	h := &IMMessageHandler{}
	snapshot, err := h.codingDynamicCatalogForIdentity(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.complete() {
		t.Fatal("missing identity must not produce a Coding dynamic catalog")
	}
}

func TestCodingDynamicCatalogQuarantinesWithoutContractRegistry(t *testing.T) {
	h := &IMMessageHandler{}
	identity := &trustedCodingInvocationIdentity{TenantID: "tenant", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	snapshot, err := h.codingDynamicCatalogForIdentity(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.complete() {
		t.Fatal("unconfigured contract registry must not authorize a Coding dynamic catalog")
	}
}

func TestPrepareCodingDynamicSemanticPlanUsesContractCatalogNotMatches(t *testing.T) {
	identity := &trustedCodingInvocationIdentity{TenantID: "tenant", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	contract := agentservice.DynamicCapabilityContract{
		Provisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 2}},
		Effects:    []tool.EffectClass{tool.EffectReadOnly},
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog([]agentservice.MCPToolEntry{{
		ServerID: "trusted-server", ToolName: "search",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []string{"query"}, "additionalProperties": false},
		Contract:    contract,
	}}, nil)
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	prepared, err := prepareCodingDynamicSemanticPlan(identity, codingDynamicCatalogSnapshot{
		Catalog: catalog, Coverage: tool.CatalogCoverage{State: tool.CatalogCoverageComplete},
	}, []tool.CapabilityNeed{{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true}}, nil, nil, tool.PlanningBudget{}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatalf("prepare plan: %v", err)
	}
	if len(prepared.Plan.Selections) != 1 {
		t.Fatalf("planned selections=%#v", prepared.Plan.Selections)
	}
	selection := prepared.Plan.Selections[0]
	if selection.AdapterName == "" || selection.Provider.ProviderID != "trusted-server" || selection.Provider.ImplementationID != "search" {
		t.Fatalf("selection does not preserve contract binding: %#v", selection)
	}
	if len(prepared.Plan.Unmet) != 0 {
		t.Fatalf("unexpected unmet needs: %#v", prepared.Plan.Unmet)
	}
}

func TestPrepareCodingDynamicSemanticPlanFailsClosedWithoutIdentityOrCatalog(t *testing.T) {
	_, err := prepareCodingDynamicSemanticPlan(nil, codingDynamicCatalogSnapshot{}, []tool.CapabilityNeed{{ID: "need", Capability: "information.search.web", Required: true}}, nil, nil, tool.PlanningBudget{}, time.Now().UTC())
	if err == nil {
		t.Fatal("missing identity must not reach planner")
	}
	identity := &trustedCodingInvocationIdentity{TenantID: "tenant", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	_, err = prepareCodingDynamicSemanticPlan(identity, codingDynamicCatalogSnapshot{Coverage: tool.CatalogCoverage{State: tool.CatalogCoverageComplete}}, []tool.CapabilityNeed{{ID: "need", Capability: "information.search.web", Required: true}}, nil, nil, tool.PlanningBudget{}, time.Now().UTC())
	if err == nil {
		t.Fatal("empty contract catalog must not reach planner")
	}
}

func TestCodingDynamicCapabilityNeedsAreHostPolicyOnlyAndRepeatable(t *testing.T) {
	first := codingDynamicCapabilityNeeds()
	second := codingDynamicCapabilityNeeds()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("coding policy needs must be deterministic: first=%#v second=%#v", first, second)
	}
	if len(first) != 30 { // 12 reads + 8 writes + 4 inspect + 6 verify.
		t.Fatalf("coding policy sibling count=%d, want 30: %#v", len(first), first)
	}
	counts := map[tool.CapabilityID]int{}
	for _, need := range first {
		counts[need.Capability]++
		if need.ID == "" || !strings.HasPrefix(need.ID, "need:coding:") || need.Confidence != 1 || !reflect.DeepEqual(need.EvidenceIDs, []string{codingDynamicCapabilityNeedEvidence}) {
			t.Fatalf("need is not host-owned policy evidence: %#v", need)
		}
	}
	for capability, want := range map[tool.CapabilityID]int{
		tool.CapabilityFSReadLocal: 12, tool.CapabilityFSWriteLocal: 8,
		tool.CapabilityRepoInspectVCS: 4, tool.CapabilityBuildVerifyLocal: 6,
	} {
		if got := counts[capability]; got != want {
			t.Fatalf("capability %q siblings=%d, want %d; all=%#v", capability, got, want, first)
		}
	}
}

func TestCodingDynamicCapabilityNeedsCloneHostPolicyQualifiers(t *testing.T) {
	templates := []agentservice.IntentCapabilityNeedTemplate{{
		Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true, MaxInvocations: 2,
	}}
	needs := codingDynamicCapabilityNeedsFromTemplates(templates)
	if len(needs) != 2 || needs[0].Qualifiers["freshness"] != "current" || needs[1].Qualifiers["freshness"] != "current" {
		t.Fatalf("unexpected projected needs: %#v", needs)
	}
	if !needs[0].Required || needs[1].Required {
		t.Fatalf("repeat budget must require only the first sibling: %#v", needs)
	}
	templates[0].Qualifiers["freshness"] = "reference"
	needs[0].Qualifiers["freshness"] = "changed"
	if needs[1].Qualifiers["freshness"] != "current" {
		t.Fatalf("needs must not share qualifier maps: %#v", needs)
	}
}

func TestPrepareCodingDynamicSemanticPlanForVerifiedCodingTaskRejectsPartialPolicyPlan(t *testing.T) {
	identity := &trustedCodingInvocationIdentity{TenantID: "tenant", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	contract := agentservice.DynamicCapabilityContract{
		Provisions: []tool.CapabilityProvision{{Capability: tool.CapabilityFSReadLocal, Quality: 2}},
		Effects:    []tool.EffectClass{tool.EffectReadOnly},
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog([]agentservice.MCPToolEntry{{
		ServerID: "trusted-server", ToolName: "read_workspace",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "required": []string{}, "additionalProperties": false},
		Contract:    contract,
	}}, nil)
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	prepared, err := prepareCodingDynamicSemanticPlan(identity, codingDynamicCatalogSnapshot{
		Catalog: catalog, Coverage: tool.CatalogCoverage{State: tool.CatalogCoverageComplete},
	}, codingDynamicCapabilityNeeds(), nil, nil, tool.PlanningBudget{}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatalf("prepare policy plan: %v", err)
	}
	if len(prepared.Plan.Selections) != 12 || len(prepared.Plan.Unmet) != 18 {
		t.Fatalf("partial policy plan must remain explicit: selections=%d unmet=%#v", len(prepared.Plan.Selections), prepared.Plan.Unmet)
	}
	for _, selection := range prepared.Plan.Selections {
		if selection.Provider.ProviderID != "trusted-server" || selection.Provider.ImplementationID != "read_workspace" || selection.FitProof.MatchedCapability != tool.CapabilityFSReadLocal {
			t.Fatalf("selection escaped contract binding: %#v", selection)
		}
	}
}

func TestPrepareCodingDynamicSemanticPlanForVerifiedCodingTaskUsesScopedContractInventory(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	identity := &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	h := &IMMessageHandler{app: app}

	// A configured but not observed lifecycle remains incomplete. This is the
	// expected R2 state: provider discovery cannot become an authorization
	// source merely because a host capability policy asks for it.
	prepared, err := h.prepareCodingDynamicSemanticPlanForVerifiedCodingTask(context.Background(), identity, nil, nil, tool.PlanningBudget{})
	if err == nil || len(prepared.Plan.Selections) != 0 {
		t.Fatalf("unobserved scoped inventory must fail closed: prepared=%#v err=%v", prepared, err)
	}
}
