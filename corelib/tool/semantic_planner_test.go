package tool

import (
	"testing"
	"time"
)

func semanticRegistry(t *testing.T) *CapabilityRegistry {
	t.Helper()
	registry := NewCapabilityRegistry("v1")
	if err := registry.Register(CapabilityDescriptor{
		ID:      "visual.capture.desktop",
		Version: "v1",
		Qualifiers: map[string]QualifierConstraint{
			"display": {Values: []string{"primary", "all"}, Required: true},
		},
		Effects: []EffectClass{EffectReadOnly},
	}); err != nil {
		t.Fatalf("register capture capability: %v", err)
	}
	if err := registry.Register(CapabilityDescriptor{
		ID:      "artifact.deliver.current_channel",
		Version: "v1",
		Qualifiers: map[string]QualifierConstraint{
			"format": {Values: []string{"image", "file"}, Required: true},
		},
		Effects: []EffectClass{EffectExternalEffect},
	}); err != nil {
		t.Fatalf("register delivery capability: %v", err)
	}
	return registry
}

func semanticSnapshot(t *testing.T, registry *CapabilityRegistry, providers []ProviderSpec) ToolCatalogSnapshot {
	t.Helper()
	catalog := NewToolCatalog(registry)
	snapshot, err := catalog.Publish(providers, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("publish catalog: %v", err)
	}
	return snapshot
}

func semanticProvider(adapter string, capability CapabilityID, qualifiers map[string]string, effects ...EffectClass) ProviderSpec {
	return semanticProviderWithSchema(adapter, capability, qualifiers, semanticClosedEmptyParameterSchema(), effects...)
}

func semanticClosedEmptyParameterSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
}

func semanticProviderWithSchema(adapter string, capability CapabilityID, qualifiers map[string]string, schema map[string]interface{}, effects ...EffectClass) ProviderSpec {
	authorization, err := NewParameterAuthorization(schema)
	if err != nil {
		panic(err)
	}
	return ProviderSpec{
		AdapterName: adapter,
		Binding: ProviderBinding{
			Kind:             "builtin",
			ProviderID:       "core",
			ImplementationID: adapter,
			SchemaDigest:     SchemaDigest([]byte(adapter)),
		},
		ParameterAuthorization: authorization,
		Provides:               []CapabilityProvision{{Capability: capability, Qualifiers: qualifiers, Quality: 1}},
		Effects:                effects,
		Ready:                  true,
	}
}

func TestToolPlannerSelectsCapabilityProvidersWithoutToolNameRules(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
		semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect),
	})
	planner := NewToolPlanner(registry)
	plan, err := planner.Plan(RouteRequest{
		RootTaskID:   "task-1",
		TurnID:       "turn-1",
		ChannelScope: "lansenger",
		Snapshot:     snapshot,
		Needs: []CapabilityNeed{
			{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Polarity: NeedRequire, Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Polarity: NeedRequire, Required: true},
		},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Unmet) != 0 || len(plan.Selections) != 2 {
		t.Fatalf("plan = %+v, want two selected capabilities", plan)
	}
	if plan.Selections[0].AdapterName != "capture_adapter" || plan.Selections[1].AdapterName != "delivery_adapter" {
		t.Fatalf("selected adapters = %#v", plan.Selections)
	}
	for _, selection := range plan.Selections {
		if selection.FitProof.Digest == "" || selection.FitProof.SnapshotGeneration != snapshot.Generation {
			t.Fatalf("selection proof is not bound to snapshot: %+v", selection.FitProof)
		}
	}
}

func TestIsLightPromptSafeSelectionUsesEffectsAndConfirmationNotAdapterName(t *testing.T) {
	readOnly := PlannedSelection{AdapterName: "dangerous_sounding_name", Effects: []EffectClass{EffectReadOnly}}
	if !IsLightPromptSafeSelection(readOnly) {
		t.Fatal("read-only selection should be light-safe regardless of adapter name")
	}
	external := PlannedSelection{AdapterName: "harmless_name", Effects: []EffectClass{EffectExternalEffect}}
	if IsLightPromptSafeSelection(external) {
		t.Fatal("external-effect selection must not become light-safe through adapter name")
	}
	confirmed := PlannedSelection{Effects: []EffectClass{EffectReadOnly}, Requires: []string{"confirmation:need"}}
	if IsLightPromptSafeSelection(confirmed) {
		t.Fatal("confirmation-bound selection must require the full policy path")
	}
	if IsLightPromptSafeSelection(PlannedSelection{}) {
		t.Fatal("selection without a declared effect must fail closed")
	}
}

func TestToolPlannerSelectsHighestDeclaredQualityWithStableTieBreak(t *testing.T) {
	registry := semanticRegistry(t)
	lower := semanticProvider("lower", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	lower.Provides[0].Quality = 1
	higher := semanticProvider("higher", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	higher.Provides[0].Quality = 2
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{higher, lower})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "task", TurnID: "turn", Snapshot: snapshot, Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 || plan.Selections[0].AdapterName != "higher" {
		t.Fatalf("quality selection plan=%+v err=%v", plan, err)
	}
	higher.Provides[0].Quality = 1
	snapshot = semanticSnapshot(t, registry, []ProviderSpec{higher, lower})
	plan, err = NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "task", TurnID: "turn", Snapshot: snapshot, Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 || plan.Selections[0].AdapterName != "higher" {
		t.Fatalf("stable tie selection plan=%+v err=%v", plan, err)
	}
}

func TestToolCatalogRejectsInvalidCapabilityQuality(t *testing.T) {
	registry := semanticRegistry(t)
	provider := semanticProvider("capture", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	provider.Provides[0].Quality = -1
	if _, err := NewToolCatalog(registry).Publish([]ProviderSpec{provider}, time.Time{}); err == nil {
		t.Fatal("catalog accepted negative capability quality")
	}
}

func TestCapabilityRegistryAndCatalogRejectInvalidEffectClasses(t *testing.T) {
	registry := NewCapabilityRegistry("v1")
	if err := registry.Register(CapabilityDescriptor{ID: "invalid.effect", Version: "v1", Effects: []EffectClass{"unknown"}}); err == nil {
		t.Fatal("capability registry accepted an unknown effect")
	}
	registry = semanticRegistry(t)
	provider := semanticProvider("capture", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	provider.Effects = []EffectClass{EffectReadOnly, EffectReadOnly}
	if _, err := NewToolCatalog(registry).Publish([]ProviderSpec{provider}, time.Time{}); err == nil {
		t.Fatal("catalog accepted duplicate effects")
	}
}

func TestToolPlannerAvoidNeedDoesNotExposeProvider(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Polarity: NeedAvoid, Required: true}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Selections) != 0 || len(plan.Unmet) != 0 {
		t.Fatalf("avoid need must not select a tool: %+v", plan)
	}
}

func TestToolPlannerRejectsCapabilityDenyAndMarksConfirmation(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
		semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect),
	})
	planner := NewToolPlanner(registry)
	plan, err := planner.Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{
			{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true},
		},
		Constraints: []RoutingConstraint{
			{Capability: "visual.capture.desktop", Effect: "deny", Authority: AuthorityPolicy},
			{Capability: "artifact.deliver.current_channel", Effect: "require_confirmation", Authority: AuthorityPolicy},
		},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Unmet) != 1 || plan.Unmet[0].NeedID != "capture" || plan.Unmet[0].ReasonCode != "policy_denied" {
		t.Fatalf("unmet = %#v", plan.Unmet)
	}
	if len(plan.Selections) != 1 || !plan.Selections[0].RequiresConfirm {
		t.Fatalf("delivery confirmation selection = %#v", plan.Selections)
	}
}

func TestToolPlannerQualifierScopedDenyDoesNotDenyOtherFormat(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("file_deliver", "artifact.deliver.current_channel", map[string]string{"format": "file"}, EffectExternalEffect),
		semanticProvider("image_deliver", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{
			{ID: "file", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true},
			{ID: "image", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true},
		},
		Constraints: []RoutingConstraint{{
			ID: "deny-file-deliver", Capability: "artifact.deliver.current_channel", Effect: "deny",
			Authority: AuthorityChannel, Attributes: map[string]string{"format": "file"},
		}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Unmet) != 1 || plan.Unmet[0].NeedID != "file" || plan.Unmet[0].ReasonCode != "policy_denied" {
		t.Fatalf("file deliver must be policy_denied, unmet=%#v", plan.Unmet)
	}
	if len(plan.Selections) != 1 || plan.Selections[0].NeedID != "image" {
		t.Fatalf("image deliver must remain selectable, selections=%#v", plan.Selections)
	}
}

func TestToolPlannerDistinguishesIncompleteCatalogFromNoFeasibleProvider(t *testing.T) {
	registry := semanticRegistry(t)
	catalog := NewToolCatalog(registry)
	incomplete, err := catalog.PublishWithCoverage(nil, CatalogCoverage{State: CatalogCoverageIncomplete, ReasonCode: "provider_not_ready"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	request := RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: incomplete,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}}
	plan, err := NewToolPlanner(registry).Plan(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Unmet) != 1 || plan.Unmet[0].NeedID != "capture" || plan.Unmet[0].ReasonCode != "provider_not_ready" {
		t.Fatalf("incomplete coverage was treated as no feasible provider: %#v", plan.Unmet)
	}
	// Lifecycle timestamps are diagnostic only. Identical snapshot content and
	// coverage status must preserve a stable plan identity across refreshes.
	second := incomplete
	second.Coverage.ObservedAt = second.Coverage.ObservedAt.Add(time.Minute)
	request.Snapshot = second
	again, err := NewToolPlanner(registry).Plan(request)
	if err != nil || again.ID != plan.ID || again.SnapshotDigest != plan.SnapshotDigest {
		t.Fatalf("coverage observation time destabilized plan: first=%#v second=%#v err=%v", plan, again, err)
	}

	complete, err := catalog.Publish(nil, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	request.Snapshot = complete
	plan, err = NewToolPlanner(registry).Plan(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != "no_feasible_provider" {
		t.Fatalf("complete empty catalog did not report infeasibility: %#v", plan.Unmet)
	}
}

func TestToolPlannerCoverageRejectsUnboundedReasons(t *testing.T) {
	registry := semanticRegistry(t)
	catalog := NewToolCatalog(registry)
	if _, err := catalog.PublishWithCoverage(nil, CatalogCoverage{State: CatalogCoverageIncomplete, ReasonCode: "dial tcp internal-host: secret"}, time.Now().UTC()); err == nil {
		t.Fatal("catalog accepted unbounded lifecycle diagnostic")
	}
	if _, err := catalog.PublishWithCoverage(nil, CatalogCoverage{State: CatalogCoverageStale, ReasonCode: CatalogCoverageReasonStale}, time.Now().UTC()); err == nil {
		t.Fatal("stale catalog without a bounded stale window was accepted")
	}
}

func TestToolPlannerUsesFamilyCoverageWithoutHidingReadySibling(t *testing.T) {
	registry := semanticRegistry(t)
	skill := semanticProvider("skill_capture", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	skill.Binding.Kind = "skill"
	mcp := semanticProvider("mcp_capture", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	mcp.Binding.Kind = "mcp"
	catalog := NewToolCatalog(registry)
	snapshot, err := catalog.PublishWithCoverage([]ProviderSpec{skill, mcp}, CatalogCoverage{
		State: CatalogCoverageIncomplete, ReasonCode: CatalogCoverageReasonNotReady,
		Families: []CatalogCoverageFamily{
			{Kind: "skill", State: CatalogCoverageComplete},
			{Kind: "mcp", State: CatalogCoverageIncomplete, ReasonCode: CatalogCoverageReasonNotReady},
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root", TurnID: "turn", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 || plan.Selections[0].Provider.Kind != "skill" || len(plan.Unmet) != 0 {
		t.Fatalf("family coverage did not retain complete sibling candidate: plan=%+v err=%v", plan, err)
	}
}

func TestToolPlannerAllowsOnlyReadOnlyCandidateInBoundedStaleWindow(t *testing.T) {
	registry := semanticRegistry(t)
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	staleUntil := now.Add(time.Minute)
	readOnly := semanticProvider("stale_read", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	readOnly.Binding.Kind = "mcp"
	readOnly.ReadyUntil = now.Add(-time.Minute)
	catalog := NewToolCatalog(registry)
	snapshot, err := catalog.PublishWithCoverage([]ProviderSpec{readOnly}, CatalogCoverage{
		State: CatalogCoverageStale, ReasonCode: CatalogCoverageReasonStale, StaleUntil: staleUntil,
		Families: []CatalogCoverageFamily{{Kind: "mcp", State: CatalogCoverageStale, ReasonCode: CatalogCoverageReasonStale, StaleUntil: staleUntil}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	request := RouteRequest{RootTaskID: "root", TurnID: "turn", Snapshot: snapshot, Now: now,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}}
	plan, err := NewToolPlanner(registry).Plan(request)
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("bounded stale read-only provider was not selectable: plan=%+v err=%v", plan, err)
	}
	request.Now = staleUntil.Add(time.Nanosecond)
	plan, err = NewToolPlanner(registry).Plan(request)
	if err != nil || len(plan.Selections) != 0 || len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != CatalogCoverageReasonStale {
		t.Fatalf("expired stale window remained executable: plan=%+v err=%v", plan, err)
	}

	external := semanticProvider("stale_external", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect)
	external.Binding.Kind = "mcp"
	external.ReadyUntil = now.Add(-time.Minute)
	snapshot, err = catalog.PublishWithCoverage([]ProviderSpec{external}, CatalogCoverage{
		State: CatalogCoverageStale, ReasonCode: CatalogCoverageReasonStale, StaleUntil: staleUntil,
		Families: []CatalogCoverageFamily{{Kind: "mcp", State: CatalogCoverageStale, ReasonCode: CatalogCoverageReasonStale, StaleUntil: staleUntil}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root", TurnID: "turn", Snapshot: snapshot, Now: now,
		Needs: []CapabilityNeed{{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true}}})
	if err != nil || len(plan.Selections) != 0 || len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != CatalogCoverageReasonStale {
		t.Fatalf("stale external provider was executable: plan=%+v err=%v", plan, err)
	}
}

func TestToolCatalogRejectsUndeclaredQualifiers(t *testing.T) {
	registry := semanticRegistry(t)
	catalog := NewToolCatalog(registry)
	_, err := catalog.Publish([]ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"format": "image"}, EffectReadOnly),
	}, time.Time{})
	if err == nil {
		t.Fatal("publish succeeded for an undeclared qualifier")
	}
}

func TestToolPlannerRejectsInvalidNeedQualifiers(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"format": "image"}, Required: true}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Selections) != 0 || len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != "invalid_capability_need" {
		t.Fatalf("plan = %+v, want invalid need", plan)
	}
}

func TestToolPlannerRequiresTrustedConstraints(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
		Constraints: []RoutingConstraint{{
			ID: "untrusted-deny", Capability: "visual.capture.desktop", Effect: "deny", Authority: AuthorityUser,
		}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("untrusted constraint must not control planning: %+v", plan)
	}
}

func TestSemanticPlanIDChangesWithPlanningInputs(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	base := RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	}
	first, err := NewToolPlanner(registry).Plan(base)
	if err != nil {
		t.Fatalf("first plan: %v", err)
	}
	changed := base
	changed.Constraints = []RoutingConstraint{{ID: "confirm", Capability: "visual.capture.desktop", Effect: "require_confirmation", Authority: AuthorityPolicy}}
	second, err := NewToolPlanner(registry).Plan(changed)
	if err != nil {
		t.Fatalf("second plan: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("plan ID must bind planning inputs: first=%s second=%s", first.ID, second.ID)
	}
}

func TestSemanticPlanIDChangesWithParameterAuthorization(t *testing.T) {
	registry := semanticRegistry(t)
	provider := semanticProviderWithSchema("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "additionalProperties": false}, EffectReadOnly)
	first := semanticSnapshot(t, registry, []ProviderSpec{provider})
	provider.ParameterAuthorization, _ = NewParameterAuthorization(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}, "limit": map[string]interface{}{"type": "integer"}}, "additionalProperties": false})
	second := semanticSnapshot(t, registry, []ProviderSpec{provider})
	request := func(snapshot ToolCatalogSnapshot) RouteRequest {
		return RouteRequest{RootTaskID: "task", TurnID: "turn", Snapshot: snapshot, Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}}
	}
	firstPlan, err := NewToolPlanner(registry).Plan(request(first))
	if err != nil {
		t.Fatal(err)
	}
	secondPlan, err := NewToolPlanner(registry).Plan(request(second))
	if err != nil {
		t.Fatal(err)
	}
	if firstPlan.ID == secondPlan.ID || firstPlan.SnapshotDigest == secondPlan.SnapshotDigest {
		t.Fatalf("parameter authorization drift did not change plan identity: first=%+v second=%+v", firstPlan, secondPlan)
	}
}

func TestSemanticPlanIDBindsCatalogIdentityAndScope(t *testing.T) {
	registry := semanticRegistry(t)
	provider := semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{provider})
	base := RouteRequest{
		RootTaskID: "task-1", SessionID: "session-a", TurnID: "turn-1", ChannelScope: "desktop", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	}
	first, err := NewToolPlanner(registry).Plan(base)
	if err != nil {
		t.Fatalf("first plan: %v", err)
	}
	changedScope := base
	changedScope.ChannelScope = "lansenger"
	second, err := NewToolPlanner(registry).Plan(changedScope)
	if err != nil {
		t.Fatalf("scope plan: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("plan ID must bind channel scope: %s", first.ID)
	}
	changedProvider := provider
	changedProvider.Binding.SchemaDigest = SchemaDigest([]byte("changed-schema"))
	changedSnapshot := semanticSnapshot(t, registry, []ProviderSpec{changedProvider})
	changedCatalog := base
	changedCatalog.Snapshot = changedSnapshot
	third, err := NewToolPlanner(registry).Plan(changedCatalog)
	if err != nil {
		t.Fatalf("catalog plan: %v", err)
	}
	if first.ID == third.ID {
		t.Fatalf("plan ID must bind provider identity: %s", first.ID)
	}
}

func TestToolPlannerBuildsArtifactDependencyAndExposesOnlyReadyPhase(t *testing.T) {
	registry := semanticRegistry(t)
	capture := semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	capture.Produces = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	delivery := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect)
	delivery.Consumes = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{capture, delivery})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{
			{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true},
		},
	})
	if err != nil || len(plan.Selections) != 2 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	var captureSelection, deliverySelection PlannedSelection
	for _, selection := range plan.Selections {
		switch selection.AdapterName {
		case "capture_adapter":
			captureSelection = selection
		case "delivery_adapter":
			deliverySelection = selection
		}
	}
	if captureSelection.Phase != PlanPhaseExecution || deliverySelection.Phase != PlanPhaseDelivery {
		t.Fatalf("phases capture=%q delivery=%q", captureSelection.Phase, deliverySelection.Phase)
	}
	if len(deliverySelection.Requires) != 1 || deliverySelection.Requires[0] != captureSelection.ID {
		t.Fatalf("delivery requirements=%#v, capture=%q", deliverySelection.Requires, captureSelection.ID)
	}
	if len(deliverySelection.ArtifactDependencies) != 1 || deliverySelection.ArtifactDependencies[0].ProducerSelection != captureSelection.ID || !producesArtifact(captureSelection.Produces, deliverySelection.ArtifactDependencies[0].Contract) {
		t.Fatalf("delivery artifact dependencies=%#v, capture=%+v", deliverySelection.ArtifactDependencies, captureSelection)
	}
	ready := plan.ReadySelections(nil)
	if len(ready) != 1 || ready[0].ID != captureSelection.ID {
		t.Fatalf("ready before capture=%#v", ready)
	}
	ready = plan.ReadySelections(map[string]bool{captureSelection.ID: true})
	if len(ready) != 2 {
		t.Fatalf("ready after capture=%#v", ready)
	}
}

func TestToolPlannerRejectsRequiredArtifactConsumerWithoutProducerOrFact(t *testing.T) {
	registry := semanticRegistry(t)
	delivery := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect)
	delivery.Consumes = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{delivery})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Selections) != 0 || len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != "artifact_dependency_missing" {
		t.Fatalf("plan without artifact=%+v", plan)
	}
	plan, err = NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true}},
		Facts: []RoutingFact{{ID: "image-artifact", Kind: "artifact_available", Authority: AuthorityRuntime, Attributes: map[string]string{"artifact_id": "artifact:trusted-image", "kind": "image", "mime_type": "image/png"}}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 || len(plan.Selections[0].ArtifactDependencies) != 1 || plan.Selections[0].ArtifactDependencies[0].ArtifactID != "artifact:trusted-image" {
		t.Fatalf("plan with artifact=%+v err=%v", plan, err)
	}
}

func TestToolPlannerBindsTrustedArtifactProvenanceIntoPlan(t *testing.T) {
	registry := semanticRegistry(t)
	delivery := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect)
	delivery.Consumes = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{delivery})
	sourceScope := InvocationScope{RootTaskID: "task-1", PlanID: "input:turn-1", SessionID: "session", TurnID: "turn-1", PrincipalID: "principal"}
	payload, err := NewArtifactPayload(sourceScope, "trusted-input:channel-attachment:one", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	binding := ArtifactBindingFromRef(payload.Ref)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true}},
		Facts: []RoutingFact{{ID: "trusted-image", Kind: "artifact_available", Authority: AuthorityChannel, Artifact: &binding, Attributes: map[string]string{"artifact_id": payload.Ref.ID, "kind": "image", "mime_type": "image/png"}}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Selections[0].ArtifactDependencies) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	dependency := plan.Selections[0].ArtifactDependencies[0]
	if dependency.Artifact != binding || dependency.ArtifactID != payload.Ref.ID {
		t.Fatalf("artifact provenance was not bound: %#v", dependency)
	}
	if plan.SnapshotDigest == semanticRouteSnapshotDigest(RouteRequest{RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot, Needs: []CapabilityNeed{{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true}}, Facts: []RoutingFact{{ID: "trusted-image", Kind: "artifact_available", Authority: AuthorityChannel, Attributes: map[string]string{"artifact_id": payload.Ref.ID, "kind": "image", "mime_type": "image/png"}}}}) {
		t.Fatal("artifact provenance must affect immutable route identity")
	}
}

func TestToolPlannerLookupGenerateAfterAndExplainTrace(t *testing.T) {
	registry := NewCapabilityRegistry("v1")
	for _, descriptor := range []CapabilityDescriptor{
		{ID: "information.search.web", Version: "v1", Qualifiers: map[string]QualifierConstraint{"freshness": {Values: []string{"current"}, Required: true}}, Effects: []EffectClass{EffectReadOnly}},
		{ID: "document.generate.file", Version: "v1", Qualifiers: map[string]QualifierConstraint{"format": {Values: []string{"pdf"}, Required: true}}, Effects: []EffectClass{EffectLocalMutation}},
		{ID: "artifact.deliver.current_channel", Version: "v1", Qualifiers: map[string]QualifierConstraint{"format": {Values: []string{"file"}, Required: true}}, Effects: []EffectClass{EffectExternalEffect}},
	} {
		if err := registry.Register(descriptor); err != nil {
			t.Fatal(err)
		}
	}
	search := semanticProvider("search_adapter", "information.search.web", map[string]string{"freshness": "current"}, EffectReadOnly)
	generate := semanticProvider("generate_adapter", "document.generate.file", map[string]string{"format": "pdf"}, EffectLocalMutation)
	generate.Produces = []ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}}
	deliver := semanticProvider("deliver_adapter", "artifact.deliver.current_channel", map[string]string{"format": "file"}, EffectExternalEffect)
	deliver.Consumes = []ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}}
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{search, generate, deliver}),
		Needs: []CapabilityNeed{
			{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true},
			{ID: "generate", Capability: "document.generate.file", Qualifiers: map[string]string{"format": "pdf"}, Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true},
		},
	})
	if err != nil || len(plan.Unmet) != 0 || len(plan.Selections) != 3 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	var generateSel PlannedSelection
	for _, selection := range plan.Selections {
		if selection.NeedID == "generate" {
			generateSel = selection
		}
	}
	foundLookup := false
	for _, requirement := range generateSel.Requires {
		if requirement == "selection:search" {
			foundLookup = true
		}
	}
	if !foundLookup {
		t.Fatalf("generate Requires=%#v, want lookup selection id", generateSel.Requires)
	}
	if len(plan.Decisions) == 0 || len(plan.Trace.Events) == 0 || plan.Trace.PlanID != plan.ID {
		t.Fatalf("explain trace is empty: decisions=%d events=%d", len(plan.Decisions), len(plan.Trace.Events))
	}
	seen := map[string]bool{}
	for _, decision := range plan.Decisions {
		seen[decision.Stage] = true
		if decision.Event == "" || decision.ReasonCode == "" {
			t.Fatalf("empty decision: %+v", decision)
		}
	}
	for _, stage := range []string{TraceStageSemantics, TraceStageFeasibility, TraceStageDependency, TraceStageOptimization, TraceStageMaterialization, TraceStageBinding, TraceStageCatalog, TraceStageRendering} {
		if !seen[stage] {
			t.Fatalf("missing explain stage %s", stage)
		}
	}
}

func TestToolPlannerRenderCurrentVoiceDeliverAfter(t *testing.T) {
	registry := NewCapabilityRegistry("v1")
	for _, descriptor := range []CapabilityDescriptor{
		{ID: "audio.render.speech", Version: "v1", Effects: []EffectClass{EffectLocalMutation}},
		{ID: "artifact.deliver.current_channel", Version: "v1", Qualifiers: map[string]QualifierConstraint{"format": {Values: []string{"file", "image", "voice"}, Required: true}}, Effects: []EffectClass{EffectExternalEffect}},
	} {
		if err := registry.Register(descriptor); err != nil {
			t.Fatal(err)
		}
	}
	render := semanticProvider("render_adapter", "audio.render.speech", nil, EffectLocalMutation)
	render.Produces = []ArtifactContract{{Kind: "audio", MIMEType: "audio/wav", Required: true}}
	voice := semanticProvider("voice_deliver", "artifact.deliver.current_channel", map[string]string{"format": "voice"}, EffectExternalEffect)
	file := semanticProvider("file_deliver", "artifact.deliver.current_channel", map[string]string{"format": "file"}, EffectExternalEffect)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{render, voice, file}),
		Needs: []CapabilityNeed{
			{ID: "render", Capability: "audio.render.speech", Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "voice"}, Required: true},
		},
	})
	if err != nil || len(plan.Unmet) != 0 || len(plan.Selections) != 2 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	var renderID string
	var deliverSel PlannedSelection
	for _, selection := range plan.Selections {
		if selection.NeedID == "render" {
			renderID = selection.ID
		}
		if selection.NeedID == "deliver" {
			deliverSel = selection
		}
	}
	found := false
	for _, requirement := range deliverSel.Requires {
		if requirement == renderID {
			found = true
		}
	}
	if renderID == "" || !found {
		t.Fatalf("voice deliver must wait for render, render=%s requires=%#v", renderID, deliverSel.Requires)
	}
	fileOnly, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{render, voice, file}),
		Needs: []CapabilityNeed{
			{ID: "render", Capability: "audio.render.speech", Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var fileRenderID string
	for _, selection := range fileOnly.Selections {
		if selection.NeedID == "render" {
			fileRenderID = selection.ID
		}
	}
	for _, selection := range fileOnly.Selections {
		if selection.NeedID != "deliver" {
			continue
		}
		for _, requirement := range selection.Requires {
			if requirement == fileRenderID {
				t.Fatalf("file deliver must not wait for speech render, requires=%#v", selection.Requires)
			}
		}
	}
}

func TestToolPlannerCaptureCurrentImageDeliverAfter(t *testing.T) {
	registry := NewCapabilityRegistry("v1")
	for _, descriptor := range []CapabilityDescriptor{
		{ID: "visual.capture.desktop", Version: "v1", Qualifiers: map[string]QualifierConstraint{"display": {Values: []string{"primary"}, Required: true}}, Effects: []EffectClass{EffectLocalMutation}},
		{ID: "artifact.deliver.current_channel", Version: "v1", Qualifiers: map[string]QualifierConstraint{"format": {Values: []string{"file", "image", "voice"}, Required: true}}, Effects: []EffectClass{EffectExternalEffect}},
	} {
		if err := registry.Register(descriptor); err != nil {
			t.Fatal(err)
		}
	}
	capture := semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectLocalMutation)
	capture.Produces = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	image := semanticProvider("image_deliver", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect)
	file := semanticProvider("file_deliver", "artifact.deliver.current_channel", map[string]string{"format": "file"}, EffectExternalEffect)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{capture, image, file}),
		Needs: []CapabilityNeed{
			{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true},
		},
	})
	if err != nil || len(plan.Unmet) != 0 || len(plan.Selections) != 2 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	var captureID string
	var deliverSel PlannedSelection
	for _, selection := range plan.Selections {
		if selection.NeedID == "capture" {
			captureID = selection.ID
		}
		if selection.NeedID == "deliver" {
			deliverSel = selection
		}
	}
	found := false
	for _, requirement := range deliverSel.Requires {
		if requirement == captureID {
			found = true
		}
	}
	if captureID == "" || !found {
		t.Fatalf("image deliver must wait for capture, capture=%s requires=%#v", captureID, deliverSel.Requires)
	}
	fileOnly, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{capture, image, file}),
		Needs: []CapabilityNeed{
			{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var fileCaptureID string
	for _, selection := range fileOnly.Selections {
		if selection.NeedID == "capture" {
			fileCaptureID = selection.ID
		}
	}
	for _, selection := range fileOnly.Selections {
		if selection.NeedID != "deliver" {
			continue
		}
		for _, requirement := range selection.Requires {
			if requirement == fileCaptureID {
				t.Fatalf("file deliver must not wait for desktop capture, requires=%#v", selection.Requires)
			}
		}
	}
}

func TestToolPlannerScheduleDispatchRequiresAdminister(t *testing.T) {
	registry := NewCapabilityRegistry("v1")
	for _, descriptor := range []CapabilityDescriptor{
		{ID: CapabilityScheduleAdministerLocal, Version: "v1", Effects: []EffectClass{EffectLocalMutation}},
		{ID: CapabilityScheduleDispatchChannel, Version: "v1", Effects: []EffectClass{EffectExternalEffect}},
	} {
		if err := registry.Register(descriptor); err != nil {
			t.Fatal(err)
		}
	}
	administer := semanticProvider("schedule_administer", CapabilityScheduleAdministerLocal, nil, EffectLocalMutation)
	dispatch := semanticProvider("semantic_schedule_dispatch", CapabilityScheduleDispatchChannel, nil, EffectExternalEffect)
	dispatch.Binding.Kind = "channel"
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{administer, dispatch}),
		Needs: []CapabilityNeed{
			{ID: "administer", Capability: CapabilityScheduleAdministerLocal, Required: true},
			{ID: "dispatch", Capability: CapabilityScheduleDispatchChannel, Required: true},
		},
	})
	if err != nil || len(plan.Unmet) != 0 || len(plan.Selections) != 2 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	var dispatchSel PlannedSelection
	var administerID string
	for _, selection := range plan.Selections {
		if selection.NeedID == "administer" {
			administerID = selection.ID
		}
		if selection.NeedID == "dispatch" {
			dispatchSel = selection
		}
	}
	found := false
	for _, requirement := range dispatchSel.Requires {
		if requirement == administerID {
			found = true
		}
	}
	if !found {
		t.Fatalf("dispatch Requires=%#v, want %s", dispatchSel.Requires, administerID)
	}
}

func TestToolPlannerPolicyDeniedWritesDeniedTrace(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
		Constraints: []RoutingConstraint{{
			ID: "deny-capture", Capability: "visual.capture.desktop", Effect: "deny", Authority: AuthorityPolicy,
		}},
	})
	if err != nil || len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != "policy_denied" {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	foundDenied := false
	for _, decision := range plan.Decisions {
		if decision.Event == "denied" && decision.ReasonCode == "policy_denied" {
			foundDenied = true
		}
	}
	if !foundDenied {
		t.Fatalf("decisions=%#v, want denied/policy_denied", plan.Decisions)
	}
}

func TestToolPlannerInquireNeedIsClarificationRequired(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Polarity: NeedInquire, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selections) != 0 || len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != "clarification_required" {
		t.Fatalf("inquire need must ask for clarification: %+v", plan)
	}
}

func TestToolPlannerBudgetKeepsClosedPrefixAndReportsExceeded(t *testing.T) {
	registry := NewCapabilityRegistry("v1")
	for _, descriptor := range []CapabilityDescriptor{
		{ID: "information.search.web", Version: "v1", Qualifiers: map[string]QualifierConstraint{"freshness": {Values: []string{"current"}, Required: true}}, Effects: []EffectClass{EffectReadOnly}},
		{ID: "document.generate.file", Version: "v1", Qualifiers: map[string]QualifierConstraint{"format": {Values: []string{"pdf"}, Required: true}}, Effects: []EffectClass{EffectLocalMutation}},
		{ID: "artifact.deliver.current_channel", Version: "v1", Qualifiers: map[string]QualifierConstraint{"format": {Values: []string{"file"}, Required: true}}, Effects: []EffectClass{EffectExternalEffect}},
	} {
		if err := registry.Register(descriptor); err != nil {
			t.Fatal(err)
		}
	}
	search := semanticProvider("search_adapter", "information.search.web", map[string]string{"freshness": "current"}, EffectReadOnly)
	generate := semanticProvider("generate_adapter", "document.generate.file", map[string]string{"format": "pdf"}, EffectLocalMutation)
	generate.Produces = []ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}}
	deliver := semanticProvider("deliver_adapter", "artifact.deliver.current_channel", map[string]string{"format": "file"}, EffectExternalEffect)
	deliver.Consumes = []ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}}
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{search, generate, deliver})
	needs := []CapabilityNeed{
		{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true},
		{ID: "generate", Capability: "document.generate.file", Qualifiers: map[string]string{"format": "pdf"}, Required: true},
		{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true},
	}
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot, Needs: needs, Budget: PlanningBudget{MaxSelections: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selections) != 1 || plan.Selections[0].NeedID != "search" {
		t.Fatalf("budget=1 must keep the closed lookup prefix, got %#v", plan.Selections)
	}
	unmet := map[string]string{}
	for _, item := range plan.Unmet {
		unmet[item.NeedID] = item.ReasonCode
	}
	if unmet["generate"] != "budget_exceeded" || unmet["deliver"] != "budget_exceeded" {
		t.Fatalf("later waves must be budget_exceeded, unmet=%#v", plan.Unmet)
	}
	unlimited, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot, Needs: needs,
	})
	if err != nil || unlimited.SnapshotDigest == plan.SnapshotDigest {
		t.Fatalf("budget must bind plan identity: err=%v unlimited=%s limited=%s", err, unlimited.SnapshotDigest, plan.SnapshotDigest)
	}
}

func TestToolPlannerSchemaTokenBudgetKeepsClosedWave(t *testing.T) {
	registry := NewCapabilityRegistry("v1")
	for _, descriptor := range []CapabilityDescriptor{
		{ID: "information.search.web", Version: "v1", Qualifiers: map[string]QualifierConstraint{"freshness": {Values: []string{"current"}, Required: true}}, Effects: []EffectClass{EffectReadOnly}},
		{ID: "document.generate.file", Version: "v1", Qualifiers: map[string]QualifierConstraint{"format": {Values: []string{"pdf"}, Required: true}}, Effects: []EffectClass{EffectLocalMutation}},
		{ID: "artifact.deliver.current_channel", Version: "v1", Qualifiers: map[string]QualifierConstraint{"format": {Values: []string{"file"}, Required: true}}, Effects: []EffectClass{EffectExternalEffect}},
	} {
		if err := registry.Register(descriptor); err != nil {
			t.Fatal(err)
		}
	}
	search := semanticProvider("search_adapter", "information.search.web", map[string]string{"freshness": "current"}, EffectReadOnly)
	generate := semanticProvider("generate_adapter", "document.generate.file", map[string]string{"format": "pdf"}, EffectLocalMutation)
	generate.Produces = []ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}}
	deliver := semanticProvider("deliver_adapter", "artifact.deliver.current_channel", map[string]string{"format": "file"}, EffectExternalEffect)
	deliver.Consumes = []ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}}
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{search, generate, deliver})
	needs := []CapabilityNeed{
		{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true},
		{ID: "generate", Capability: "document.generate.file", Qualifiers: map[string]string{"format": "pdf"}, Required: true},
		{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true},
	}
	searchCost := selectionSchemaTokenCost([]PlannedSelection{{ParameterAuthorization: search.ParameterAuthorization}})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot, Needs: needs,
		Budget: PlanningBudget{MaxSchemaTokens: searchCost + 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selections) != 1 || plan.Selections[0].NeedID != "search" {
		t.Fatalf("schema budget must keep the lookup prefix, got %#v", plan.Selections)
	}
	unmet := map[string]string{}
	for _, item := range plan.Unmet {
		unmet[item.NeedID] = item.ReasonCode
	}
	if unmet["generate"] != "budget_exceeded" || unmet["deliver"] != "budget_exceeded" {
		t.Fatalf("later waves must be budget_exceeded, unmet=%#v", plan.Unmet)
	}
	tooSmall, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot, Needs: needs,
		Budget: PlanningBudget{MaxSchemaTokens: searchCost - 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tooSmall.Selections) != 0 {
		t.Fatalf("first-wave schema overflow must keep none: %#v", tooSmall.Selections)
	}
	for _, item := range tooSmall.Unmet {
		if item.ReasonCode != "planning_budget_exceeded" {
			t.Fatalf("want planning_budget_exceeded, got %#v", item)
		}
	}
}

func TestToolPlannerPlanningBudgetExceededWhenFirstWaveDoesNotFit(t *testing.T) {
	registry := NewCapabilityRegistry("v1")
	for _, descriptor := range []CapabilityDescriptor{
		{ID: "information.search.web", Version: "v1", Qualifiers: map[string]QualifierConstraint{"freshness": {Values: []string{"current", "reference"}, Required: true}}, Effects: []EffectClass{EffectReadOnly}},
		{ID: "information.current_time", Version: "v1", Effects: []EffectClass{EffectReadOnly}},
	} {
		if err := registry.Register(descriptor); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("search_adapter", "information.search.web", map[string]string{"freshness": "current"}, EffectReadOnly),
		semanticProvider("clock_adapter", "information.current_time", nil, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{
			{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true},
			{ID: "clock", Capability: "information.current_time", Required: true},
		},
		Budget: PlanningBudget{MaxSelections: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selections) != 0 {
		t.Fatalf("first-wave overflow must not keep a random subset: %#v", plan.Selections)
	}
	if len(plan.Unmet) != 2 {
		t.Fatalf("unmet=%#v", plan.Unmet)
	}
	for _, item := range plan.Unmet {
		if item.ReasonCode != "planning_budget_exceeded" {
			t.Fatalf("want planning_budget_exceeded, got %#v", item)
		}
	}
}
