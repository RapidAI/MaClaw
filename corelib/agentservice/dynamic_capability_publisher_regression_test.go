package agentservice

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestMergeVendorDeclaredCapabilityIdempotentAcrossServers locks in the Fix 2
// contract: two independently-installed MCP servers that both self-declare the
// same external (x:) capability must produce an identical governed descriptor
// so registration is idempotent. Before the fix the descriptor Summary baked in
// the server/tool identity, so the second server's descriptor conflicted with
// the first and the capability was silently dropped.
func TestMergeVendorDeclaredCapabilityIdempotentAcrossServers(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	principal := Principal{TenantID: "tenant", UserID: "user"}
	base := DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{
			{Capability: "document.lookup", Qualifiers: map[string]string{"scope": "current"}, Quality: 1},
		},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
	}
	schema := map[string]interface{}{"type": "object"}

	// First server registers x:acme:translate.
	toolA := ObservedMCPTool{
		Name:                 "translate",
		InputSchema:          schema,
		DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"},
	}
	c1, declared1, errs1 := publisher.mergeVendorDeclaredCapabilities(principal, "serverA", toolA, cloneDynamicCapabilityContract(base))
	if len(errs1) != 0 {
		t.Fatalf("serverA registration returned errors: %v", errs1)
	}
	if len(declared1) != 1 || declared1[0] != "x:acme:translate" {
		t.Fatalf("serverA declared=%v, want exactly [x:acme:translate]", declared1)
	}
	if len(c1.Provisions) != 2 {
		t.Fatalf("serverA contract must carry the reviewed + vendor provision, got %d provisions", len(c1.Provisions))
	}

	// Second server declares the SAME capability. This must be idempotent: the
	// second registration must not conflict with the first.
	toolB := ObservedMCPTool{
		Name:                 "translate",
		InputSchema:          schema,
		DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"},
	}
	c2, declared2, errs2 := publisher.mergeVendorDeclaredCapabilities(principal, "serverB", toolB, cloneDynamicCapabilityContract(base))
	if len(errs2) != 0 {
		t.Fatalf("serverB registration must be idempotent, got errors: %v", errs2)
	}
	if len(declared2) != 1 {
		t.Fatalf("serverB declared=%v, want exactly [x:acme:translate]", declared2)
	}
	if len(c2.Provisions) != 2 {
		t.Fatalf("serverB contract must carry the reviewed + vendor provision, got %d provisions", len(c2.Provisions))
	}

	// The shared registry holds exactly one descriptor for the capability, with
	// no server/tool identity baked into the Summary.
	desc, ok := publisher.registry.Lookup("x:acme:translate")
	if !ok {
		t.Fatal("vendor capability not present in the shared registry")
	}
	wantSummary := fmt.Sprintf("Vendor-declared capability %s", coretool.CapabilityID("x:acme:translate"))
	if desc.Summary != wantSummary {
		t.Fatalf("descriptor Summary carried server/tool identity: %q, want %q", desc.Summary, wantSummary)
	}
	// Re-registering the identical descriptor at the registry level is itself
	// idempotent (returns nil, never a conflict).
	if err := publisher.registry.RegisterExternalCapability("acme", desc); err != nil {
		t.Fatalf("idempotent re-registration failed: %v", err)
	}
}

// TestVendorDescriptorCeilingGrantsNoAuthority is the safety guard for the
// ceiling-based descriptor (§8.5). Stamping the descriptor with the namespace
// ceiling looks like a privilege grant, so this test pins down why it is not:
//
//  1. A namespace the host never widened gets exactly the default ceiling
//     (read_only + external_effect). No installed server can obtain
//     local_mutation or sensitive authority through a vendor declaration,
//     because those effects are absent from the ceiling the descriptor carries.
//  2. A provider that nevertheless DECLARES an out-of-ceiling effect is
//     rejected at catalog publish time. When the descriptor held a narrow
//     per-server value this was enforced implicitly; now that the descriptor is
//     a namespace constant, the catalog-side check is the only thing standing
//     between a lying server and widened authority.
//  3. When a host DOES review and widen the ceiling, the descriptor follows the
//     ceiling — the descriptor tracks host policy, never server self-declaration.
//
// If the assertions here fail, a vendor-declared x: capability can claim
// authority the host never granted.
func TestVendorDescriptorCeilingGrantsNoAuthority(t *testing.T) {
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test"}, NewMemoryStore(), &CoreAgentExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	contracts := NewDynamicCapabilityRegistry()
	publisher, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, registry, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	ready := func(string) bool { return true }
	identity := MCPCapabilityIdentity{}
	schema := map[string]interface{}{"type": "object"}
	readOnlyHint, notReadOnlyHint := true, false

	publish := func(server, capID string) {
		t.Helper()
		tool := ObservedMCPTool{
			Name:                 "op",
			InputSchema:          schema,
			DeclaredCapabilities: []coretool.CapabilityID{coretool.CapabilityID(capID)},
		}
		if err := publisher.PublishReviewedMCPObservationForCapabilityWithOpenLookup(
			principal, server, identity, []ObservedMCPTool{tool}, ready); err != nil {
			t.Fatalf("server %q must publish: %v", server, err)
		}
	}

	// 1. A namespace the host never reviewed keeps the default ceiling.
	publish("server-default-ns", "x:defaultns:op")
	desc, ok := registry.Lookup("x:defaultns:op")
	if !ok {
		t.Fatal("vendor capability was not registered")
	}
	for _, forbidden := range []coretool.EffectClass{coretool.EffectSensitive, coretool.EffectLocalMutation} {
		for _, got := range desc.Effects {
			if got == forbidden {
				t.Fatalf("descriptor for a never-widened namespace grants %q; effects=%v", forbidden, desc.Effects)
			}
		}
	}

	// 2. A provider declaring an effect the descriptor does not permit is
	// rejected at catalog publish time — this is the check that replaces the
	// narrow-descriptor implicit bound.
	declareWith := func(adapter string, effects []coretool.EffectClass) error {
		_, pubErr := coretool.NewToolCatalog(registry).PublishWithCoverage([]coretool.ProviderSpec{{
			AdapterName: adapter,
			Binding: coretool.ProviderBinding{
				Kind: "mcp", ProviderID: "lying-server", ImplementationID: "op", SchemaDigest: "s1",
			},
			ParameterAuthorization: coretool.ParameterAuthorization{
				Digest: "p1", CanonicalizerVer: "semantic-parameters-v1", AllowedFields: []string{"text"},
			},
			Provides:       []coretool.CapabilityProvision{{Capability: "x:defaultns:op", Quality: 1}},
			Effects:        effects,
			Classification: coretool.ProviderClassProvision,
		}}, coretool.CatalogCoverage{State: coretool.CatalogCoverageComplete}, time.Now().UTC())
		return pubErr
	}
	if err := declareWith("lying-sensitive", []coretool.EffectClass{coretool.EffectSensitive}); err == nil {
		t.Fatal("a provider declaring sensitive over a never-widened namespace must be rejected by the catalog")
	}
	// An effect the ceiling does permit must still be accepted, otherwise the
	// guard above would be trivially satisfied by rejecting everything.
	if err := declareWith("within-ceiling", []coretool.EffectClass{coretool.EffectReadOnly, coretool.EffectExternalEffect}); err != nil {
		t.Fatalf("catalog must accept effects within the namespace ceiling: %v", err)
	}

	// 3. Host policy, not server self-declaration, drives the descriptor in
	// BOTH directions.
	//
	//    Widening: a host-reviewed namespace that adds sensitive must produce a
	//    descriptor carrying sensitive, so the ceiling is genuinely followed.
	if err := registry.SetExternalCapabilityCeiling("reviewedns", []coretool.EffectClass{
		coretool.EffectReadOnly, coretool.EffectExternalEffect, coretool.EffectSensitive,
	}); err != nil {
		t.Fatalf("host must be able to widen a reviewed namespace: %v", err)
	}
	readOnlyTool := ObservedMCPTool{
		Name: "op", InputSchema: schema,
		Annotations:          &MCPToolAnnotations{ReadOnlyHint: &readOnlyHint},
		DeclaredCapabilities: []coretool.CapabilityID{"x:reviewedns:op"},
	}
	if err := publisher.PublishReviewedMCPObservationForCapabilityWithOpenLookup(
		principal, "server-reviewed-ns", identity, []ObservedMCPTool{readOnlyTool}, ready); err != nil {
		t.Fatalf("read-only tool must publish inside a host-widened namespace: %v", err)
	}
	reviewedDesc, ok := registry.Lookup("x:reviewedns:op")
	if !ok {
		t.Fatal("vendor capability in the widened namespace was not registered")
	}
	foundSensitive := false
	for _, e := range reviewedDesc.Effects {
		if e == coretool.EffectSensitive {
			foundSensitive = true
		}
	}
	if !foundSensitive {
		t.Fatalf("descriptor must follow the host-widened ceiling; effects=%v", reviewedDesc.Effects)
	}

	//    Narrowing: a host that removes external_effect from a namespace makes
	//    the very same non-read-only server's declaration fail closed. This is
	//    the property that makes the ceiling a real bound rather than a
	//    formality — authority tracks host policy immediately, in both
	//    directions.
	if err := registry.SetExternalCapabilityCeiling("narrowedns", []coretool.EffectClass{
		coretool.EffectReadOnly,
	}); err != nil {
		t.Fatalf("host must be able to narrow a namespace: %v", err)
	}
	writingTool := ObservedMCPTool{
		Name: "op", InputSchema: schema,
		Annotations:          &MCPToolAnnotations{ReadOnlyHint: &notReadOnlyHint},
		DeclaredCapabilities: []coretool.CapabilityID{"x:narrowedns:op"},
	}
	if err := publisher.PublishReviewedMCPObservationForCapabilityWithOpenLookup(
		principal, "server-narrowed-ns", identity, []ObservedMCPTool{writingTool}, ready); err == nil {
		t.Fatal("a host-narrowed namespace must reject a server whose tool declares an effect the host removed")
	}
}

// TestMergeVendorDeclaredCapabilityDivergentEffectsConverge locks the other
// half of the idempotency contract: two servers may declare the SAME x:
// capability with DIFFERENT per-tool effect sets, because the open lookup
// family narrows contract.Effects from each tool's MCP readOnlyHint.
//
// externalDescriptorEqual compares Effects element-wise by index, so submitting
// each server's own value made the second server's descriptor conflict and the
// capability was silently dropped — the same loss the Summary fix addressed,
// one field over. A union of the servers' values does not work either: the
// registry rejects any attempt to change an already-registered descriptor, so a
// union computed from prior state fails as soon as a later server declares a
// wider set. The vocabulary descriptor therefore carries the NAMESPACE EFFECT
// CEILING — a host-reviewed constant, independent of which servers are
// installed and how each annotated its tools.
//
// This test drives the real publish entry (the open family is what produces the
// divergent effects), not the internal merge helper.
func TestMergeVendorDeclaredCapabilityDivergentEffectsConverge(t *testing.T) {
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test"}, NewMemoryStore(), &CoreAgentExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	contracts := NewDynamicCapabilityRegistry()
	publisher, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, registry, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	yes := true
	no := false
	readOnlyTool := ObservedMCPTool{
		Name:                 "translate",
		InputSchema:          map[string]interface{}{"type": "object"},
		Annotations:          &MCPToolAnnotations{ReadOnlyHint: &yes},
		DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"},
	}
	writingTool := ObservedMCPTool{
		Name:                 "translate",
		InputSchema:          map[string]interface{}{"type": "object"},
		Annotations:          &MCPToolAnnotations{ReadOnlyHint: &no},
		DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"},
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	ready := func(string) bool { return true }
	identity := MCPCapabilityIdentity{}

	// Read-only annotated server first, then a server whose tool is not
	// read-only: the divergent order matters, and the reverse order must also
	// converge.
	if err := publisher.PublishReviewedMCPObservationForCapabilityWithOpenLookup(
		principal, "server-readonly", identity, []ObservedMCPTool{readOnlyTool}, ready); err != nil {
		t.Fatalf("read-only server must publish: %v", err)
	}
	if err := publisher.PublishReviewedMCPObservationForCapabilityWithOpenLookup(
		principal, "server-writing", identity, []ObservedMCPTool{writingTool}, ready); err != nil {
		t.Fatalf("divergent-effect server must still publish (descriptor must converge), got: %v", err)
	}

	desc, ok := registry.Lookup("x:acme:translate")
	if !ok {
		t.Fatal("vendor capability must remain registered after a divergent-effect server publishes")
	}
	// The vocabulary descriptor holds the NAMESPACE CEILING, in canonical
	// planner order -- not either server's own value.
	want := []coretool.EffectClass{coretool.EffectReadOnly, coretool.EffectExternalEffect}
	if len(desc.Effects) != len(want) {
		t.Fatalf("descriptor effects = %v, want the namespace ceiling %v", desc.Effects, want)
	}
	for i := range want {
		if desc.Effects[i] != want[i] {
			t.Fatalf("descriptor effects = %v, want the canonical namespace ceiling %v", desc.Effects, want)
		}
	}
	// Re-registering the identical descriptor is idempotent (this is what a
	// second server publishing the same capability now does).
	if err := registry.RegisterExternalCapability("acme", desc); err != nil {
		t.Fatalf("idempotent re-registration failed: %v", err)
	}
	// A descriptor that narrowed the effect set below the ceiling must still be
	// rejected: the vocabulary entry is stable, and silently accepting a
	// narrower one would let a later server change a governed contract.
	narrowed := desc
	narrowed.Effects = []coretool.EffectClass{coretool.EffectReadOnly}
	if err := registry.RegisterExternalCapability("acme", narrowed); err == nil {
		t.Fatal("a descriptor that changes the governed effect set must not silently replace the registered one")
	}
}

// TestMergeVendorDeclaredCapabilityConcurrentConvergence locks that concurrent
// vendor registrations of the SAME capability converge on one descriptor.
//
// MCP observations for different servers can run concurrently (discovery/health
// fans out per server). Because the descriptor effect set is derived purely
// from the namespace ceiling, two goroutines registering the same capability
// compute byte-identical descriptors and the second is idempotent rather than a
// conflict -- so a server's capabilities never depend on which other server
// happened to publish first, nor on whether they raced.
//
// Run with -race to also cover the shared registry access.
func TestMergeVendorDeclaredCapabilityConcurrentConvergence(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test"}, NewMemoryStore(), &CoreAgentExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	publisher, err := NewLifecycleDynamicCapabilityContractPublisher(NewDynamicCapabilityRegistry(), registry, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}

	yes := true
	no := false
	// Two servers, opposite annotations: the case that needs the union.
	specs := []struct {
		server string
		tool   ObservedMCPTool
	}{
		{"server-a", ObservedMCPTool{Name: "translate", InputSchema: map[string]interface{}{"type": "object"}, Annotations: &MCPToolAnnotations{ReadOnlyHint: &yes}, DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"}}},
		{"server-b", ObservedMCPTool{Name: "translate", InputSchema: map[string]interface{}{"type": "object"}, Annotations: &MCPToolAnnotations{ReadOnlyHint: &no}, DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"}}},
		{"server-c", ObservedMCPTool{Name: "translate", InputSchema: map[string]interface{}{"type": "object"}, DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"}}},
		{"server-d", ObservedMCPTool{Name: "translate", InputSchema: map[string]interface{}{"type": "object"}, Annotations: &MCPToolAnnotations{ReadOnlyHint: &yes}, DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"}}},
	}
	var wg sync.WaitGroup
	errsCh := make(chan error, len(specs))
	for _, spec := range specs {
		wg.Add(1)
		go func(server string, tool ObservedMCPTool) {
			defer wg.Done()
			if err := publisher.PublishReviewedMCPObservationForCapabilityWithOpenLookup(
				principal, server, MCPCapabilityIdentity{}, []ObservedMCPTool{tool}, func(string) bool { return true }); err != nil {
				errsCh <- err
			}
		}(spec.server, spec.tool)
	}
	wg.Wait()
	close(errsCh)
	for err := range errsCh {
		t.Fatalf("concurrent vendor registration must converge, got: %v", err)
	}
	desc, ok := registry.Lookup("x:acme:translate")
	if !ok {
		t.Fatal("vendor capability must be registered after concurrent observations")
	}
	want := []coretool.EffectClass{coretool.EffectReadOnly, coretool.EffectExternalEffect}
	if len(desc.Effects) != len(want) {
		t.Fatalf("descriptor effects = %v, want the canonical namespace ceiling %v", desc.Effects, want)
	}
	for i := range want {
		if desc.Effects[i] != want[i] {
			t.Fatalf("descriptor effects = %v, want canonical union %v", desc.Effects, want)
		}
	}
}

// TestPublishVendorDeclaredCapabilityVisibleToCatalogOnSharedRegistry locks in
// the Fix 1 contract: the dynamic capability publisher and the planner catalog
// must share ONE capability-registry instance. The publisher registers a
// vendor-declared external capability into that registry at publish time, and
// the catalog validates provisions against the same instance during
// PublishWithCoverage. A divergent registry (the pre-fix wiring) would make the
// capability "unknown" to the catalog and break routing for every installed MCP
// that declares an x: capability.
func TestPublishVendorDeclaredCapabilityVisibleToCatalogOnSharedRegistry(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	contracts := NewDynamicCapabilityRegistry()
	publisher, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, registry, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The catalog is built from the SAME registry instance the publisher uses.
	catalog := coretool.NewToolCatalog(registry)

	principal := Principal{TenantID: "desktop", UserID: "desktop-user"}
	schema := map[string]interface{}{"type": "object"}
	// An open-lookup observation of a tool that self-declares x:acme:translate.
	// openLookupReady reports the server healthy so the unreviewed tool is
	// admitted and its vendor capability is registered.
	obsErr := publisher.PublishReviewedMCPObservationForCapabilityWithOpenLookup(
		principal, "vendor-server", MCPCapabilityIdentity{}, []ObservedMCPTool{
			{Name: "translate", InputSchema: schema, DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"}},
		}, func(serverID string) bool { return true })
	if obsErr != nil {
		t.Fatalf("vendor observation failed: %v", obsErr)
	}

	desc, ok := registry.Lookup("x:acme:translate")
	if !ok {
		t.Fatal("vendor capability was not registered into the shared registry by the publisher")
	}

	// The catalog, built from the SAME registry, must accept a provider that
	// provisions the capability the publisher just registered.
	snapshot, pubErr := catalog.PublishWithCoverage([]coretool.ProviderSpec{
		{
			AdapterName: "vendor-translate",
			Binding: coretool.ProviderBinding{
				Kind: "mcp", ProviderID: "vendor-server", ImplementationID: "translate", SchemaDigest: "s1",
			},
			ParameterAuthorization: coretool.ParameterAuthorization{
				Digest: "p1", CanonicalizerVer: "semantic-parameters-v1", AllowedFields: []string{"text"},
			},
			Provides:       []coretool.CapabilityProvision{{Capability: "x:acme:translate", Quality: 1}},
			Effects:        append([]coretool.EffectClass(nil), desc.Effects...),
			Classification: coretool.ProviderClassProvision,
		},
	}, coretool.CatalogCoverage{State: coretool.CatalogCoverageComplete}, time.Now().UTC())
	if pubErr != nil {
		t.Fatalf("catalog rejected a capability the shared publisher registered (shared-registry contract broken): %v", pubErr)
	}
	if len(snapshot.Providers) != 1 {
		t.Fatalf("expected 1 published provider, got %d", len(snapshot.Providers))
	}
}

// TestServiceDynamicSemanticRegistryReturnsConfiguredInstance locks in the
// Fix 1 plumbing at the Service boundary: ConfigureDynamicSemanticRouting stores
// the exact capability-registry instance it is handed, and DynamicSemanticRegistry
// returns that same instance. http.go relies on this getter so the publisher and
// the catalog are constructed from one shared vocabulary; reverting the store
// (or constructing a fresh registry in the host wiring) would silently diverge
// them again.
func TestServiceDynamicSemanticRegistryReturnsConfiguredInstance(t *testing.T) {
	executor := &CoreAgentExecutor{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test"}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	registry := dynamicSemanticRegistry(t)
	if svc.DynamicSemanticRegistry() != nil {
		t.Fatal("expected a nil registry before ConfigureDynamicSemanticRouting")
	}
	resolver := dynamicNeedResolverFunc(func(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
		return DynamicCapabilityNeedResolution{}, nil
	})
	if err := svc.ConfigureDynamicSemanticRouting(registry, resolver, nil, time.Minute); err != nil {
		t.Fatalf("ConfigureDynamicSemanticRouting: %v", err)
	}
	if svc.DynamicSemanticRegistry() != registry {
		t.Fatal("Service did not retain the exact configured registry instance for the shared publisher/catalog contract")
	}
}

// TestServiceDynamicSemanticRegistryConcurrentAccess exercises the guard on
// Service.registry: hosts may read the registry from a request goroutine (to
// construct the publisher) while another goroutine configures routing. Run with
// -race, this fails if the field is read or written without dynamicSemanticMu.
// The read side must also never observe a different instance than the one the
// writer installed.
func TestServiceDynamicSemanticRegistryConcurrentAccess(t *testing.T) {
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test"}, NewMemoryStore(), &CoreAgentExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	registry := dynamicSemanticRegistry(t)
	resolver := dynamicNeedResolverFunc(func(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
		return DynamicCapabilityNeedResolution{}, nil
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := svc.ConfigureDynamicSemanticRouting(registry, resolver, nil, time.Minute); err != nil {
			t.Errorf("ConfigureDynamicSemanticRouting: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			// Any non-nil value observed here must be the configured instance;
			// a torn or divergent pointer would be a real defect.
			if got := svc.DynamicSemanticRegistry(); got != nil && got != registry {
				t.Errorf("DynamicSemanticRegistry returned an unexpected instance")
				return
			}
		}
	}()
	wg.Wait()

	if svc.DynamicSemanticRegistry() != registry {
		t.Fatal("registry must remain the configured instance after concurrent access")
	}
}

// TestMergeVendorDeclaredCapabilityRejectsOverCeilingEnvelope guards the
// enforcement that a namespace-ceiling descriptor would otherwise erase.
//
// The registered descriptor carries the NAMESPACE CEILING as its Effects so two
// servers declaring the same x: capability register identically (see
// TestMergeVendorDeclaredCapabilityIdempotentAcrossServers). That makes
// RegisterExternalCapability's own ceiling loop compare the ceiling against
// itself -- it can never fail. The per-provider check now done against
// contract.Effects before registration is therefore the ONLY surviving
// ceiling enforcement at this step, and deleting it silently admits a server
// whose execution envelope exceeds what the host reviewed. This test pins it.
func TestMergeVendorDeclaredCapabilityRejectsOverCeilingEnvelope(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()

	// Host narrows the acme namespace to read-only only.
	if err := publisher.registry.SetExternalCapabilityCeiling("acme", []coretool.EffectClass{coretool.EffectReadOnly}); err != nil {
		t.Fatalf("set ceiling: %v", err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	schema := map[string]interface{}{"type": "object"}
	tool := ObservedMCPTool{
		Name:                 "translate",
		InputSchema:          schema,
		DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"},
	}

	// A provider whose execution envelope exceeds the reviewed ceiling must NOT
	// register the capability, even though the descriptor's own Effects equal
	// the ceiling and would sail through RegisterExternalCapability.
	overCeiling := DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{
			{Capability: "document.lookup", Quality: 1},
		},
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
	}
	c, declared, errs := publisher.mergeVendorDeclaredCapabilities(principal, "serverOver", tool, cloneDynamicCapabilityContract(overCeiling))
	if len(errs) == 0 {
		t.Fatal("over-ceiling envelope must be rejected; the namespace-ceiling descriptor alone does not enforce this")
	}
	if len(declared) != 0 {
		t.Fatalf("over-ceiling capability must not be declared, got %v", declared)
	}
	if len(c.Provisions) != 1 {
		t.Fatalf("over-ceiling capability must not be added as a provision, got %+v", c.Provisions)
	}
	if _, ok := publisher.registry.Lookup("x:acme:translate"); ok {
		t.Fatal("over-ceiling capability must not reach the governed vocabulary")
	}

	// The same namespace still admits a provider within the ceiling, so this
	// guard is refuse-the-escalation, not refuse-everything.
	withinCeiling := DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{
			{Capability: "document.lookup", Quality: 1},
		},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
	}
	c2, declared2, errs2 := publisher.mergeVendorDeclaredCapabilities(principal, "serverOk", tool, cloneDynamicCapabilityContract(withinCeiling))
	if len(errs2) != 0 {
		t.Fatalf("within-ceiling envelope must be accepted, got errors: %v", errs2)
	}
	if len(declared2) != 1 || declared2[0] != "x:acme:translate" {
		t.Fatalf("within-ceiling declared=%v, want [x:acme:translate]", declared2)
	}
	if len(c2.Provisions) != 2 {
		t.Fatalf("within-ceiling contract must carry both provisions, got %+v", c2.Provisions)
	}
}
