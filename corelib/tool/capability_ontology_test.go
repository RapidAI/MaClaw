package tool

import (
	"strings"
	"testing"
	"time"
)

func newOntologyRegistry(t *testing.T) *CapabilityRegistry {
	t.Helper()
	registry := NewCapabilityRegistry(BuiltinCapabilityOntologyVersion)
	if err := RegisterBuiltinCapabilityOntology(registry); err != nil {
		t.Fatalf("register builtin capability ontology: %v", err)
	}
	return registry
}

func TestBuiltinCapabilityOntologyCoversExpectedFamilies(t *testing.T) {
	registry := newOntologyRegistry(t)
	expected := map[CapabilityID]EffectClass{
		CapabilityShellExecuteLocal:         EffectSensitive,
		CapabilityShellExecuteRemoteHost:    EffectExternalEffect,
		CapabilityBuildVerifyLocal:          EffectSensitive,
		CapabilityFSReadLocal:               EffectReadOnly,
		CapabilityFSWriteLocal:              EffectSensitive,
		CapabilitySystemLaunchLocal:         EffectSensitive,
		CapabilityRepoInspectVCS:            EffectReadOnly,
		CapabilityRepoMutateVCS:             EffectExternalEffect,
		CapabilityDocumentWriteOffice:       EffectSensitive,
		CapabilityDocumentRenderPDF:         EffectSensitive,
		CapabilityInformationFetchWeb:       EffectReadOnly,
		CapabilityArtifactAcquireRemote:     EffectSensitive,
		CapabilityComputerControlDesktop:    EffectExternalEffect,
		CapabilityBrowserControlWeb:         EffectExternalEffect,
		CapabilityAudioCaptureMicrophone:    EffectSensitive,
		CapabilityAudioSynthesizeSpeech:     EffectExternalEffect,
		CapabilityAudioSynthesizeLocal:      EffectLocalMutation,
		CapabilityAudioRenderSpeech:         EffectLocalMutation,
		CapabilityAudioTranscribeSpeech:     EffectReadOnly,
		CapabilityMessageSendIM:             EffectExternalEffect,
		CapabilityScheduleManageLocal:       EffectExternalEffect,
		CapabilityScheduleAdministerLocal:   EffectLocalMutation,
		CapabilityScheduleDispatchChannel:   EffectExternalEffect,
		CapabilitySessionManageCoding:       EffectSensitive,
		CapabilityAgentDelegateSubtask:      EffectSensitive,
		CapabilityTaskTrackLocal:            EffectSensitive,
		CapabilityGoalManageLongRunning:     EffectSensitive,
		CapabilityMemoryManageAgent:         EffectSensitive,
		CapabilityMemoryRecallAgent:         EffectReadOnly,
		CapabilityKnowledgeReadLocal:        EffectReadOnly,
		CapabilityKnowledgeIngestLocal:      EffectSensitive,
		CapabilityKnowledgeAdminMaintenance: EffectSensitive,
		CapabilityConfigManageSelf:          EffectSensitive,
		CapabilitySecurityAuditRead:         EffectReadOnly,
		CapabilityTemplateManageSession:     EffectSensitive,
		CapabilityBusinessDataRead:          EffectReadOnly,
		CapabilityBusinessDataMIS:           EffectSensitive,
		CapabilityInteractionAskUser:        EffectReadOnly,
		CapabilityGovernanceInspectExp:      EffectReadOnly,
	}
	for id, effect := range expected {
		descriptor, ok := registry.Lookup(id)
		if !ok {
			t.Fatalf("capability %q is not registered", id)
		}
		if descriptor.Version == "" || descriptor.Owner == "" || descriptor.Summary == "" {
			t.Fatalf("capability %q descriptor is incomplete: %+v", id, descriptor)
		}
		if len(descriptor.Effects) != 1 || descriptor.Effects[0] != effect {
			t.Fatalf("capability %q effects = %v, want [%q]", id, descriptor.Effects, effect)
		}
	}
	if len(BuiltinCapabilityOntology()) != len(expected) {
		t.Fatalf("ontology has %d descriptors, want %d", len(BuiltinCapabilityOntology()), len(expected))
	}
}

func TestBuiltinCapabilityOntologyDescriptorsAreComplete(t *testing.T) {
	seen := make(map[CapabilityID]bool)
	for _, descriptor := range BuiltinCapabilityOntology() {
		if seen[descriptor.ID] {
			t.Fatalf("ontology registers %q twice", descriptor.ID)
		}
		seen[descriptor.ID] = true
		if descriptor.ID == "" || descriptor.Version == "" || descriptor.Owner == "" || descriptor.Summary == "" {
			t.Fatalf("ontology descriptor is incomplete: %+v", descriptor)
		}
		if err := validateEffectClasses(descriptor.Effects); err != nil || len(descriptor.Effects) == 0 {
			t.Fatalf("capability %q effects are invalid: %v", descriptor.ID, err)
		}
		for name := range descriptor.Qualifiers {
			if strings.TrimSpace(name) == "" {
				t.Fatalf("capability %q has an empty qualifier name", descriptor.ID)
			}
		}
	}
}

func TestRegisterBuiltinCapabilityOntologySkipsExistingIDs(t *testing.T) {
	registry := NewCapabilityRegistry(BuiltinCapabilityOntologyVersion)
	if err := registry.Register(CapabilityDescriptor{
		ID: CapabilityFSReadLocal, Version: "v9", Owner: "product",
		Summary: "Host-reviewed override registered before the builtin ontology.",
		Effects: []EffectClass{EffectReadOnly},
	}); err != nil {
		t.Fatalf("pre-register descriptor: %v", err)
	}
	if err := RegisterBuiltinCapabilityOntology(registry); err != nil {
		t.Fatalf("register builtin capability ontology: %v", err)
	}
	descriptor, ok := registry.Lookup(CapabilityFSReadLocal)
	if !ok {
		t.Fatalf("capability %q is not registered", CapabilityFSReadLocal)
	}
	if descriptor.Version != "v9" || descriptor.Owner != "product" {
		t.Fatalf("pre-registered descriptor was overwritten: %+v", descriptor)
	}
	if _, ok := registry.Lookup(CapabilityFSWriteLocal); !ok {
		t.Fatalf("remaining ontology families were not registered")
	}
	// A second pass over the same registry is idempotent.
	if err := RegisterBuiltinCapabilityOntology(registry); err != nil {
		t.Fatalf("re-register builtin capability ontology: %v", err)
	}
}

func TestRegisterBuiltinCapabilityOntologyRejectsSealedRegistry(t *testing.T) {
	registry := newOntologyRegistry(t)
	if err := registry.Seal(); err != nil {
		t.Fatalf("seal registry: %v", err)
	}
	if err := RegisterBuiltinCapabilityOntology(registry); err == nil {
		t.Fatalf("registering into a sealed registry must fail")
	}
}

func sealedOntologyCatalog(t *testing.T) *ToolCatalog {
	t.Helper()
	registry := newOntologyRegistry(t)
	if err := registry.Seal(); err != nil {
		t.Fatalf("seal registry: %v", err)
	}
	return NewToolCatalog(registry)
}

func validOntologyProvider(t *testing.T, adapter string) ProviderSpec {
	t.Helper()
	authorization, err := NewParameterAuthorization(semanticClosedEmptyParameterSchema())
	if err != nil {
		t.Fatalf("authorize schema: %v", err)
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
		Provides:               []CapabilityProvision{{Capability: CapabilityFSReadLocal, Quality: 1}},
		Effects:                []EffectClass{EffectReadOnly},
		Ready:                  true,
	}
}

func publishExpectingError(t *testing.T, catalog *ToolCatalog, provider ProviderSpec, want string) {
	t.Helper()
	if _, err := catalog.Publish([]ProviderSpec{provider}, time.Time{}); err == nil {
		t.Fatalf("publish must fail closed, want error containing %q", want)
	} else if !strings.Contains(err.Error(), want) {
		t.Fatalf("publish error = %q, want substring %q", err.Error(), want)
	}
}

func TestCatalogGateRejectsUnknownCapability(t *testing.T) {
	catalog := sealedOntologyCatalog(t)
	provider := validOntologyProvider(t, "unknown_capability_adapter")
	provider.Provides = []CapabilityProvision{{Capability: "fs.read.unknown", Quality: 1}}
	publishExpectingError(t, catalog, provider, "unknown or deprecated capability")
}

func TestCatalogGateRejectsOutOfSchemaQualifier(t *testing.T) {
	catalog := sealedOntologyCatalog(t)

	undeclared := validOntologyProvider(t, "undeclared_qualifier_adapter")
	undeclared.Provides = []CapabilityProvision{{
		Capability: CapabilityFSReadLocal, Qualifiers: map[string]string{"scope": "repo"}, Quality: 1,
	}}
	publishExpectingError(t, catalog, undeclared, "does not declare qualifier")

	disallowed := validOntologyProvider(t, "disallowed_qualifier_adapter")
	disallowed.Provides = []CapabilityProvision{{
		Capability: CapabilityDocumentWriteOffice, Qualifiers: map[string]string{"format": "video"}, Quality: 1,
	}}
	disallowed.Effects = []EffectClass{EffectSensitive}
	publishExpectingError(t, catalog, disallowed, "is not allowed")
}

func TestCatalogGateRejectsMissingSchema(t *testing.T) {
	catalog := sealedOntologyCatalog(t)
	provider := validOntologyProvider(t, "missing_schema_adapter")
	provider.ParameterAuthorization = ParameterAuthorization{}
	publishExpectingError(t, catalog, provider, "parameter authorization is required")
}

func TestCatalogGateRejectsMissingIdentity(t *testing.T) {
	catalog := sealedOntologyCatalog(t)
	provider := validOntologyProvider(t, "missing_identity_adapter")
	provider.Binding.ProviderID = ""
	publishExpectingError(t, catalog, provider, "binding is required")
}

func TestCatalogGateRejectsMissingEffectDeclaration(t *testing.T) {
	catalog := sealedOntologyCatalog(t)
	provider := validOntologyProvider(t, "missing_effect_adapter")
	provider.Effects = nil
	publishExpectingError(t, catalog, provider, "no effect declaration")
}

func TestCatalogGateRejectsUnclassifiedProvisionlessProvider(t *testing.T) {
	catalog := sealedOntologyCatalog(t)
	provider := validOntologyProvider(t, "provisionless_adapter")
	provider.Provides = nil
	provider.Effects = nil
	publishExpectingError(t, catalog, provider, "unclassified")
}

func TestCatalogGateRejectsNonProvisionProviderWithProvisions(t *testing.T) {
	for _, classification := range []ProviderClassification{ProviderClassFixedControlPlane, ProviderClassQuarantined} {
		catalog := sealedOntologyCatalog(t)
		provider := validOntologyProvider(t, "classified_"+string(classification))
		provider.Classification = classification
		publishExpectingError(t, catalog, provider, "must not declare capability provisions")
	}
}

func TestCatalogGateRejectsUnknownClassification(t *testing.T) {
	catalog := sealedOntologyCatalog(t)
	provider := validOntologyProvider(t, "legacy_classification_adapter")
	provider.Classification = "legacy"
	publishExpectingError(t, catalog, provider, "unknown classification")
}

func TestCatalogGateAcceptsExplicitlyClassifiedProviders(t *testing.T) {
	catalog := sealedOntologyCatalog(t)
	provision := validOntologyProvider(t, "provision_adapter")
	controlPlane := validOntologyProvider(t, "control_plane_adapter")
	controlPlane.Classification = ProviderClassFixedControlPlane
	controlPlane.Provides = nil
	controlPlane.Effects = nil
	quarantined := validOntologyProvider(t, "quarantined_adapter")
	quarantined.Classification = ProviderClassQuarantined
	quarantined.Provides = nil
	quarantined.Effects = nil
	quarantined.Ready = false
	snapshot, err := catalog.Publish([]ProviderSpec{provision, controlPlane, quarantined}, time.Time{})
	if err != nil {
		t.Fatalf("publish classified providers: %v", err)
	}
	classes := make(map[string]ProviderClassification, len(snapshot.Providers))
	for _, provider := range snapshot.Providers {
		classes[provider.AdapterName] = provider.Classification
	}
	if classes["provision_adapter"] != ProviderClassProvision {
		t.Fatalf("zero-value classification was not normalised to provision: %q", classes["provision_adapter"])
	}
	if classes["control_plane_adapter"] != ProviderClassFixedControlPlane || classes["quarantined_adapter"] != ProviderClassQuarantined {
		t.Fatalf("explicit classifications were not preserved: %v", classes)
	}
}

func TestToolPlannerNeverSelectsNonPlannableClassifications(t *testing.T) {
	registry := newOntologyRegistry(t)
	if err := registry.Seal(); err != nil {
		t.Fatalf("seal registry: %v", err)
	}
	planner := NewToolPlanner(registry)
	need := CapabilityNeed{ID: "read", Capability: CapabilityFSReadLocal, Polarity: NeedRequire, Required: true}
	request := func(snapshot ToolCatalogSnapshot) RouteRequest {
		return RouteRequest{RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot, Needs: []CapabilityNeed{need}}
	}

	// A quarantined entry is the only catalog record: the need stays unmet
	// instead of falling back to an ungoverned candidate.
	quarantinedOnly := validOntologyProvider(t, "quarantined_adapter")
	quarantinedOnly.Classification = ProviderClassQuarantined
	quarantinedOnly.Provides = nil
	quarantinedOnly.Effects = nil
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{quarantinedOnly})
	plan, err := planner.Plan(request(snapshot))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Selections) != 0 || len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != "no_feasible_provider" {
		t.Fatalf("plan = %+v, want one unmet need with no_feasible_provider", plan)
	}

	// Defense in depth: even a hand-built snapshot that bypasses the Publish
	// gate and carries provisions on a quarantined/control-plane entry must
	// not yield a candidate.
	for _, classification := range []ProviderClassification{ProviderClassFixedControlPlane, ProviderClassQuarantined} {
		rogue := validOntologyProvider(t, "rogue_adapter")
		rogue.Classification = classification
		handBuilt := ToolCatalogSnapshot{
			Generation:      1,
			RegistryVersion: registry.Version(),
			Coverage:        CatalogCoverage{State: CatalogCoverageComplete},
			Providers:       []ProviderSpec{rogue},
		}
		plan, err := planner.Plan(request(handBuilt))
		if err != nil {
			t.Fatalf("plan hand-built %q snapshot: %v", classification, err)
		}
		if len(plan.Selections) != 0 {
			t.Fatalf("planner selected a %q provider: %+v", classification, plan.Selections)
		}
	}

	// A provision-class candidate alongside classified entries is selected.
	provision := validOntologyProvider(t, "provision_adapter")
	controlPlane := validOntologyProvider(t, "control_plane_adapter")
	controlPlane.Classification = ProviderClassFixedControlPlane
	controlPlane.Provides = nil
	controlPlane.Effects = nil
	snapshot = semanticSnapshot(t, registry, []ProviderSpec{provision, controlPlane})
	plan, err = planner.Plan(request(snapshot))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Selections) != 1 || plan.Selections[0].AdapterName != "provision_adapter" {
		t.Fatalf("plan = %+v, want the provision adapter selected", plan)
	}
}
