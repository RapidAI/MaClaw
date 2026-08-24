package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestCodingDurableDynamicSurfaceRequiresResponseBindingBeforeAliasResolution(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	identity := &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	contract := agentservice.DynamicCapabilityContract{
		Provisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 2}},
		Effects:    []tool.EffectClass{tool.EffectReadOnly},
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog([]agentservice.MCPToolEntry{{
		ServerID: "trusted-server", ToolName: "search",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []string{"query"}, "additionalProperties": false}, Contract: contract,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dynamic := codingDynamicCatalogSnapshot{Catalog: catalog, Coverage: tool.CatalogCoverage{State: tool.CatalogCoverageComplete}}
	prepared, err := prepareCodingDynamicSemanticPlan(identity, dynamic, []tool.CapabilityNeed{{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true}}, nil, nil, tool.PlanningBudget{}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	surface, err := app.publishCodingDurableDynamicSurface(identity, prepared, dynamic, "test-provider/v1", "connection-a", time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(surface.definitions) != 1 || len(surface.aliases) != 1 {
		t.Fatalf("surface definitions=%#v aliases=%#v", surface.definitions, surface.aliases)
	}
	var alias string
	for name := range surface.aliases {
		alias = name
	}
	if _, _, err := surface.ResolveAlias("response-a", alias); err == nil {
		t.Fatal("prepared alias resolved before response binding")
	}
	if err := surface.BindResponse("response-a", time.Unix(3, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	grant, scope, err := surface.ResolveAlias("response-a", alias)
	if err != nil {
		t.Fatal(err)
	}
	if scope != surface.scope || grant.SelectionID == "" || grant.Scope != surface.scope {
		t.Fatalf("resolved grant=%#v scope=%#v", grant, scope)
	}
	if _, _, err := surface.ResolveAlias("response-other", alias); err == nil {
		t.Fatal("wrong response ID resolved alias")
	}
	retry, err := surface.ReplaceRequest("test-provider/v1", "connection-b", time.Unix(4, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := surface.ResolveAlias("response-a", alias); err == nil {
		t.Fatal("superseded response resolved predecessor alias")
	}
	if err := retry.BindResponse("response-b", time.Unix(5, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := retry.ResolveAlias("response-b", alias); err != nil {
		t.Fatalf("successor did not resolve reused grant: %v", err)
	}
	if err := retry.Cancel(time.Unix(6, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := retry.ResolveAlias("response-b", alias); err == nil {
		t.Fatal("cancelled route resolved alias")
	}
}

func TestCodingDurableDynamicSurfaceRecoversOnlyDurableBoundStateAfterAppRestart(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	identity := &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	contract := agentservice.DynamicCapabilityContract{
		Provisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 2}},
		Effects:    []tool.EffectClass{tool.EffectReadOnly},
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog([]agentservice.MCPToolEntry{{
		ServerID: "trusted-server", ToolName: "search",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []string{"query"}, "additionalProperties": false}, Contract: contract,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dynamic := codingDynamicCatalogSnapshot{Catalog: catalog, Coverage: tool.CatalogCoverage{State: tool.CatalogCoverageComplete}}
	prepared, err := prepareCodingDynamicSemanticPlan(identity, dynamic, []tool.CapabilityNeed{{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true}}, nil, nil, tool.PlanningBudget{}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	surface, err := app.publishCodingDurableDynamicSurface(identity, prepared, dynamic, "test-provider/v1", "connection-a", time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	var alias string
	for name := range surface.aliases {
		alias = name
	}
	if err := surface.BindResponse("response-a", time.Unix(3, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	app.closeSemanticInvocationStore()
	recovered, err := app.recoverCodingDurableDynamicSurface(identity, "test-provider/v1", "connection-a", surface.epoch)
	if err != nil {
		t.Fatalf("recover bound surface: %v", err)
	}
	if len(recovered.aliases) != 0 || len(recovered.definitions) != 0 || recovered.scope != surface.scope || recovered.plan.ID != surface.plan.ID {
		t.Fatalf("recovered helper restored process-local dispatch state: %#v", recovered)
	}
	if _, scope, err := recovered.ResolveAlias("response-a", alias); err != nil || scope != surface.scope {
		t.Fatalf("recovered durable alias did not resolve: scope=%#v err=%v", scope, err)
	}
	if _, err := app.recoverCodingDurableDynamicSurface(&trustedCodingInvocationIdentity{TenantID: "other", PrincipalID: identity.PrincipalID, SessionID: identity.SessionID, RootTaskID: identity.RootTaskID, TurnID: identity.TurnID}, "test-provider/v1", "connection-a", surface.epoch); err == nil {
		t.Fatal("cross-tenant identity recovered durable surface")
	}
}

func TestCodingDurableDynamicSurfaceFixedBridgeRejectsAndJournalsInvalidArguments(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	h := &IMMessageHandler{app: app}
	identity := &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	contract := agentservice.DynamicCapabilityContract{
		Provisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 2}},
		Effects:    []tool.EffectClass{tool.EffectReadOnly},
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog([]agentservice.MCPToolEntry{{
		ServerID: "trusted-server", ToolName: "search",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []string{"query"}, "additionalProperties": false}, Contract: contract,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dynamic := codingDynamicCatalogSnapshot{Catalog: catalog, Coverage: tool.CatalogCoverage{State: tool.CatalogCoverageComplete}}
	prepared, err := prepareCodingDynamicSemanticPlan(identity, dynamic, []tool.CapabilityNeed{{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true}}, nil, nil, tool.PlanningBudget{}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	surface, err := app.publishCodingDurableDynamicSurface(identity, prepared, dynamic, "test-provider/v1", "connection-a", time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	var alias string
	for name := range surface.aliases {
		alias = name
	}
	if got := surface.ExecuteBoundSelection(context.Background(), identity, dynamic, h, "test-provider/v1", "connection-a", "response-a", "missing-call-id", alias, `{}`, time.Unix(3, 0).UTC()); got.ReasonCode != "stale_surface" {
		t.Fatalf("unbound response executed bridge: %#v", got)
	}
	if err := surface.BindResponse("response-a", time.Unix(4, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	first := surface.ExecuteBoundSelection(context.Background(), identity, dynamic, h, "test-provider/v1", "connection-a", "response-a", "call-1", alias, `{"unexpected":true}`, time.Unix(5, 0).UTC())
	if first.ReasonCode != "parameter_schema_invalid" || first.Succeeded || !strings.Contains(first.Result, "parameter_schema_invalid") {
		t.Fatalf("invalid arguments were not durably rejected: %#v", first)
	}
	replay := surface.ExecuteBoundSelection(context.Background(), identity, dynamic, h, "test-provider/v1", "connection-a", "response-a", "call-1", alias, `{"unexpected":true}`, time.Unix(6, 0).UTC())
	if replay.ReasonCode != "parameter_schema_invalid" || replay.Succeeded || replay.Result != first.Result {
		t.Fatalf("identical host call was not replayed from journal: first=%#v replay=%#v", first, replay)
	}
	conflict := surface.ExecuteBoundSelection(context.Background(), identity, dynamic, h, "test-provider/v1", "connection-a", "response-a", "call-1", alias, `{"query":"different"}`, time.Unix(7, 0).UTC())
	if conflict.ReasonCode != "host_call_conflict" || conflict.Succeeded {
		t.Fatalf("different arguments reused host-call identity: %#v", conflict)
	}
	missingID := surface.ExecuteBoundSelection(context.Background(), identity, dynamic, h, "test-provider/v1", "connection-a", "response-a", "", alias, `{}`, time.Unix(8, 0).UTC())
	if missingID.ReasonCode != "stale_surface" || missingID.Succeeded {
		t.Fatalf("missing trusted tool-call ID reached bridge: %#v", missingID)
	}
}

func TestPrepareAndPublishCodingDurableDynamicSurfaceRejectsMissingCorrelation(t *testing.T) {
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}
	t.Cleanup(h.app.closeSemanticInvocationStore)
	identity := &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	_, err := h.prepareAndPublishCodingDurableDynamicSurface(context.Background(), identity, []tool.CapabilityNeed{{ID: "search", Capability: "information.search.web", Required: true}}, nil, nil, tool.PlanningBudget{}, "", "")
	if err == nil {
		t.Fatal("missing request correlation must fail before durable publication")
	}
}

func TestPrepareAndPublishCodingDurableDynamicSurfaceForVerifiedCodingTaskStaysClosedForPartialPolicy(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	h := &IMMessageHandler{app: app}
	identity := &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}

	// An empty contract registry/inventory is intentionally enough for this
	// test: the policy-bound entry must reject before it can create any alias
	// surface. It also demonstrates that it does not accept a matched provider
	// or a caller-supplied "just search" need as a way to shrink the policy.
	if _, err := h.prepareAndPublishCodingDurableDynamicSurfaceForVerifiedCodingTask(context.Background(), identity, nil, nil, tool.PlanningBudget{}, "test-provider/v1", "connection-a"); err == nil {
		t.Fatal("incomplete policy catalog published Coding aliases")
	}
}

func TestCodingDurableDynamicSurfaceRejectsPartialPlan(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	identity := &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	contract := agentservice.DynamicCapabilityContract{
		Provisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 2}},
		Effects:    []tool.EffectClass{tool.EffectReadOnly},
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog([]agentservice.MCPToolEntry{{
		ServerID: "trusted-server", ToolName: "search",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []string{"query"}, "additionalProperties": false}, Contract: contract,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dynamic := codingDynamicCatalogSnapshot{Catalog: catalog, Coverage: tool.CatalogCoverage{State: tool.CatalogCoverageComplete}}
	prepared, err := prepareCodingDynamicSemanticPlan(identity, dynamic, []tool.CapabilityNeed{
		{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true},
		{ID: "missing", Capability: "artifact.deliver.current_channel", Required: true},
	}, nil, nil, tool.PlanningBudget{}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Plan.Unmet) == 0 {
		t.Fatalf("fixture unexpectedly complete: %+v", prepared.Plan)
	}
	if _, err := app.publishCodingDurableDynamicSurface(identity, prepared, dynamic, "test-provider/v1", "connection-a", time.Unix(2, 0).UTC()); err == nil {
		t.Fatal("partial dynamic plan published a selectively executable surface")
	}
}

func TestCodingDurableDynamicSurfaceRetiresRouteWhenRequestSurfacePublicationFails(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	coordinator, err := handler.app.semanticExecutionCoordinatorForApp()
	if err != nil {
		t.Fatal(err)
	}
	// The caller has already reserved this exact tuple. Reusing its epoch makes
	// PublishModelRequestSurface fail after PublishSurface committed plan/grant
	// state, which is the failure boundary that must not leave an orphan route.
	if _, err := handler.app.publishCodingDurableDynamicSurfaceForEpoch(identity, prepared, dynamic, "test-provider/v1", "connection-a", "publication-failure-epoch", time.Unix(2, 0).UTC()); err != nil {
		t.Fatalf("publish predecessor fixture: %v", err)
	}
	if _, err := handler.app.publishCodingDurableDynamicSurfaceForEpoch(identity, prepared, dynamic, "test-provider/v1", "connection-b", "publication-failure-epoch", time.Unix(3, 0).UTC()); err == nil {
		t.Fatal("duplicate epoch unexpectedly published a second request surface")
	}
	scope := tool.InvocationScope{RootTaskID: identity.RootTaskID, PlanID: prepared.Plan.ID, SessionID: identity.SessionID, TurnID: identity.TurnID, PrincipalID: identity.PrincipalID}
	if _, err := coordinator.Routes.PublishedPlan(scope); err == nil || !strings.Contains(err.Error(), "route_revision_cancelled") {
		t.Fatalf("incomplete request publication left route current: %v", err)
	}
	if err := coordinator.BindModelRequestResponse("publication-failure-epoch", "test-provider/v1", "connection-a", "response-late", time.Unix(4, 0).UTC()); err == nil {
		t.Fatal("late predecessor response bound after failed successor publication retired the route")
	}
}
