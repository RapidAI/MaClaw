package tool

import (
	"testing"
	"time"
)

func digestTestProvider(adapter, implementation string, scopes, fields, targets, artifacts []string) ProviderSpec {
	return ProviderSpec{
		AdapterName: adapter,
		Binding: ProviderBinding{
			Kind:              "mcp",
			ProviderID:        "server",
			ImplementationID:  implementation,
			SchemaDigest:      "schema:" + implementation,
			CatalogGeneration: 7,
		},
		Classification: ProviderClassProvision,
		ParameterAuthorization: ParameterAuthorization{
			Digest:             "auth:" + implementation,
			CanonicalizerVer:   semanticCanonicalizerVersion,
			AllowedFields:      append([]string(nil), fields...),
			AllowedTargets:     append([]string(nil), targets...),
			AllowedArtifactIDs: append([]string(nil), artifacts...),
		},
		Provides: []CapabilityProvision{{
			Capability: "information.search.web",
			Qualifiers: map[string]string{"freshness": "current", "region": implementation},
			Quality:    0.9,
		}},
		Consumes:      []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Produces:      []ArtifactContract{{Kind: "text", MIMEType: "text/plain", Required: false}},
		Effects:       []EffectClass{EffectReadOnly},
		Ready:         true,
		ReadyUntil:    time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
		ChannelScopes: append([]string(nil), scopes...),
	}
}

func digestTestCoverageFamilies() []CatalogCoverageFamily {
	return []CatalogCoverageFamily{
		{Kind: "mcp", State: CatalogCoverageComplete},
		{Kind: "skill", State: CatalogCoverageStale, ReasonCode: CatalogCoverageReasonStale, StaleUntil: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)},
	}
}

func TestCatalogSnapshotDigestIsPermutationInvariantAndRetainsDuplicateBindings(t *testing.T) {
	providerA := digestTestProvider("adapter-a", "impl-a", []string{" Mobile ", "desktop"}, []string{"z", "a"}, []string{"target:2", "target:1"}, []string{"artifact:b", "artifact:a"})
	// Deliberately use the same binding identity as providerA while changing
	// other fields. Published catalogs reject this malformed state, but the
	// digest must still be deterministic if a hand-built snapshot reaches a
	// recovery check.
	providerDuplicate := providerA
	providerDuplicate.AdapterName = "adapter-z"
	providerDuplicate.Ready = false
	providerDuplicate.ParameterAuthorization.AllowedFields = []string{"q", "p"}

	base := ToolCatalogSnapshot{
		Generation:      7,
		RegistryVersion: "registry-v1",
		Coverage: CatalogCoverage{
			State:      CatalogCoverageComplete,
			Families:   digestTestCoverageFamilies(),
			ObservedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		},
		Providers: []ProviderSpec{providerA, providerDuplicate},
	}
	permuted := base
	permuted.Coverage.Families = []CatalogCoverageFamily{base.Coverage.Families[1], base.Coverage.Families[0]}
	permuted.Providers = []ProviderSpec{providerDuplicate, providerA}
	permuted.Providers[1].ChannelScopes = []string{"desktop", " Mobile "}
	permuted.Providers[1].ParameterAuthorization.AllowedTargets = []string{"target:1", "target:2"}
	permuted.Providers[1].ParameterAuthorization.AllowedArtifactIDs = []string{"artifact:a", "artifact:b"}
	permuted.Providers[1].ParameterAuthorization.AllowedFields = []string{"a", "z"}

	if got, want := CatalogSnapshotDigest(permuted), CatalogSnapshotDigest(base); got != want {
		t.Fatalf("catalog digest changed under provider/family/list permutation: got %s want %s", got, want)
	}
	metadataOnly := base
	metadataOnly.CreatedAt = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	metadataOnly.Coverage.ObservedAt = time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	if got, want := CatalogSnapshotDigest(metadataOnly), CatalogSnapshotDigest(base); got != want {
		t.Fatalf("catalog digest changed for diagnostic timestamps: got %s want %s", got, want)
	}

	changed := base
	changed.Providers = append([]ProviderSpec(nil), base.Providers...)
	changed.Providers[0].ParameterAuthorization.AllowedTargets = []string{"target:changed"}
	if got, want := CatalogSnapshotDigest(changed), CatalogSnapshotDigest(base); got == want {
		t.Fatal("catalog digest ignored an authorization target change")
	}
	changed = base
	changed.Providers = append([]ProviderSpec(nil), base.Providers...)
	changed.Providers[0].ParameterAuthorization.AllowedArtifactIDs = []string{"artifact:changed"}
	if got, want := CatalogSnapshotDigest(changed), CatalogSnapshotDigest(base); got == want {
		t.Fatal("catalog digest ignored an authorization artifact change")
	}
}

func TestSemanticRouteSnapshotDigestIsPermutationInvariantForAllUnorderedInputs(t *testing.T) {
	providerA := digestTestProvider("adapter-a", "impl-a", []string{"b", "a"}, []string{"field-b", "field-a"}, nil, nil)
	providerB := digestTestProvider("adapter-b", "impl-b", []string{"desktop"}, []string{"query"}, nil, nil)
	providerDuplicate := providerB
	providerDuplicate.AdapterName = "adapter-z"
	providerDuplicate.Ready = false
	snapshot := ToolCatalogSnapshot{
		Generation:      3,
		RegistryVersion: "registry-v1",
		Coverage: CatalogCoverage{
			State: CatalogCoverageComplete,
			Families: []CatalogCoverageFamily{
				{Kind: "skill", State: CatalogCoverageComplete},
				{Kind: "mcp", State: CatalogCoverageComplete},
			},
		},
		Providers: []ProviderSpec{providerA, providerB, providerDuplicate},
	}
	base := RouteRequest{
		RootTaskID:   "root",
		SessionID:    "session",
		TurnID:       "turn",
		ChannelScope: "desktop",
		Snapshot:     snapshot,
		Needs: []CapabilityNeed{
			{ID: "same", Capability: "information.search.web", Polarity: NeedRequire, Required: true, Qualifiers: map[string]string{"q": "a", "limit": "1"}},
			{ID: "same", Capability: "information.search.web", Polarity: NeedRequire, Required: false, Qualifiers: map[string]string{"q": "b"}},
		},
		Constraints: []RoutingConstraint{
			{ID: "same", Capability: "information.search.web", Effect: "deny", Authority: AuthorityPolicy, Attributes: map[string]string{"b": "2", "a": "1"}},
			{ID: "same", Capability: "information.search.web", Effect: "allow", Authority: AuthorityRuntime, Attributes: map[string]string{"a": "3"}},
		},
		Facts: []RoutingFact{
			{ID: "same", Kind: "hint", Authority: AuthorityRuntime, Attributes: map[string]string{"b": "2", "a": "1"}},
			{ID: "same", Kind: "other", Authority: AuthorityChannel, Attributes: map[string]string{"a": "3"}},
		},
		Budget: PlanningBudget{MaxSelections: 4, MaxSchemaTokens: 1000},
	}
	permuted := base
	permuted.Snapshot.Coverage.Families = []CatalogCoverageFamily{snapshot.Coverage.Families[1], snapshot.Coverage.Families[0]}
	permuted.Snapshot.Providers = []ProviderSpec{providerDuplicate, providerB, providerA}
	permuted.Snapshot.Providers[2].ChannelScopes = []string{"a", "b"}
	permuted.Snapshot.Providers[2].ParameterAuthorization.AllowedFields = []string{"field-a", "field-b"}
	permuted.Needs = []CapabilityNeed{base.Needs[1], base.Needs[0]}
	permuted.Constraints = []RoutingConstraint{base.Constraints[1], base.Constraints[0]}
	permuted.Facts = []RoutingFact{base.Facts[1], base.Facts[0]}

	if got, want := semanticRouteSnapshotDigest(permuted), semanticRouteSnapshotDigest(base); got != want {
		t.Fatalf("route digest changed under permutation: got %s want %s", got, want)
	}

	changed := base
	changed.Needs = append([]CapabilityNeed(nil), base.Needs...)
	changed.Needs[0].Qualifiers = map[string]string{"q": "changed"}
	if got, want := semanticRouteSnapshotDigest(changed), semanticRouteSnapshotDigest(base); got == want {
		t.Fatal("route digest ignored a need qualifier change")
	}
}

func TestCatalogSnapshotDigestUsesVersionedCanonicalEncoding(t *testing.T) {
	snapshot := ToolCatalogSnapshot{Generation: 1, RegistryVersion: "v1", Coverage: CatalogCoverage{State: CatalogCoverageComplete}}
	digest := CatalogSnapshotDigest(snapshot)
	if len(digest) < len("catalog:sha256:") || digest[:len("catalog:sha256:")] != "catalog:sha256:" {
		t.Fatalf("unexpected catalog digest format: %q", digest)
	}
	// Values containing the historical separators must remain distinct under
	// the length-delimited v2 payload.
	withSeparator := snapshot
	withSeparator.RegistryVersion = "v1\x00\x1f\x1e"
	if CatalogSnapshotDigest(withSeparator) == digest {
		t.Fatal("length-delimited catalog encoding collapsed separator-containing values")
	}
}

func TestEffectiveCatalogSnapshotDigestRejectsTamperedStoredClaim(t *testing.T) {
	snapshot := ToolCatalogSnapshot{Generation: 1, RegistryVersion: "v1", Coverage: CatalogCoverage{State: CatalogCoverageComplete}}
	expected := CatalogSnapshotDigest(snapshot)
	snapshot.Digest = expected
	if got, err := EffectiveCatalogSnapshotDigest(snapshot); err != nil || got != expected {
		t.Fatalf("valid stored digest rejected: got=%q err=%v want=%q", got, err, expected)
	}
	snapshot.Digest = "catalog:sha256:tampered"
	if err := ValidateCatalogSnapshotDigest(snapshot); err == nil {
		t.Fatal("tampered stored catalog digest was accepted")
	}
}
