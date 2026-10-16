package agentservice

import (
	"context"
	"fmt"
	"strings"
	"testing"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// sealedTestRegistry builds a minimal sealed vocabulary that mirrors the IM
// registry's open-lookup permission: information.lookup may carry read_only and
// external_effect, so the open family and any vendor-declared x: capability
// validate against it.
func sealedTestRegistry(t *testing.T) *coretool.CapabilityRegistry {
	t.Helper()
	reg := coretool.NewCapabilityRegistry("test-v1")
	if err := reg.Register(coretool.CapabilityDescriptor{
		ID:      "information.lookup",
		Version: "v1",
		Owner:   "im",
		Summary: "open lookup",
		Qualifiers: map[string]coretool.QualifierConstraint{
			"scope": {Values: []string{"reference", "current"}, Required: true},
		},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly, coretool.EffectExternalEffect},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	if err := reg.Seal(); err != nil {
		t.Fatalf("seal registry: %v", err)
	}
	return reg
}

func TestInstalledMCPToolSummaryNamesTheTool(t *testing.T) {
	reg := sealedTestRegistry(t)
	id := AutoMCPCapabilityIDForBinding("tengyun-mt0y9cwv", "work_log_query")
	if err := EnsureExternalCapabilityVocabulary(reg, []coretool.CapabilityID{id}); err != nil {
		t.Fatal(err)
	}
	desc, ok := reg.Lookup(id)
	if !ok {
		t.Fatal("exact installed-tool capability was not registered")
	}
	if !strings.Contains(desc.Summary, "work_log_query") || !strings.Contains(desc.Summary, "tengyun-mt0y9cwv") {
		t.Fatalf("summary = %q", desc.Summary)
	}
}

// TestInstalledMCPDescriptorAgreesAcrossRegistrationPaths: publication and
// the per-turn vocabulary seed must write one descriptor. A mismatch is
// rejected, and the publisher then skips the provision, so the tool has no
// exact address.
func TestInstalledMCPDescriptorAgreesAcrossRegistrationPaths(t *testing.T) {
	id := AutoMCPCapabilityIDForBinding("tengyun-mt0y9cwv", "query_worklog")
	tools := []ObservedMCPTool{{
		Name:        "query_worklog",
		InputSchema: map[string]interface{}{"type": "object"},
		Annotations: &MCPToolAnnotations{ReadOnlyHint: boolPtr(true)},
	}}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	publish := func(reg *coretool.CapabilityRegistry) error {
		contracts := NewDynamicCapabilityRegistry()
		pub, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, reg, nil, nil)
		if err != nil {
			return err
		}
		return pub.PublishReviewedMCPObservationForCapabilityWithOpenLookup(
			principal, "tengyun-mt0y9cwv", MCPCapabilityIdentity{}, tools, func(string) bool { return true },
		)
	}
	seededFirst := sealedTestRegistry(t)
	if err := EnsureExternalCapabilityVocabulary(seededFirst, []coretool.CapabilityID{id}); err != nil {
		t.Fatal(err)
	}
	if err := publish(seededFirst); err != nil {
		t.Fatalf("publish after vocabulary seed: %v", err)
	}
	publishedFirst := sealedTestRegistry(t)
	if err := publish(publishedFirst); err != nil {
		t.Fatal(err)
	}
	if err := EnsureExternalCapabilityVocabulary(publishedFirst, []coretool.CapabilityID{id}); err != nil {
		t.Fatalf("vocabulary seed after publish: %v", err)
	}
	for _, reg := range []*coretool.CapabilityRegistry{seededFirst, publishedFirst} {
		desc, ok := reg.Lookup(id)
		if !ok || !strings.Contains(desc.Summary, "query_worklog") || !strings.Contains(desc.Summary, "tengyun-mt0y9cwv") {
			t.Fatalf("summary = %q ok=%v", desc.Summary, ok)
		}
	}
}

// TestPublishRegistersVendorDeclaredCapability closes the first Phase 1
// acceptance point: an installed MCP that declares x:acme:translate enters the
// semantic surface under its own capability name (not merely generic lookup)
// and is bounded by the host default effect ceiling.
func TestPublishRegistersVendorDeclaredCapability(t *testing.T) {
	reg := sealedTestRegistry(t)
	contracts := NewDynamicCapabilityRegistry()
	pub, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, reg, nil, nil)
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	tools := []ObservedMCPTool{
		{
			Name:                 "translate",
			InputSchema:          map[string]interface{}{"type": "object"},
			Annotations:          &MCPToolAnnotations{ReadOnlyHint: boolPtr(true)},
			DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"},
		},
	}
	if err := pub.PublishReviewedMCPObservationForCapabilityWithOpenLookup(principal, "srv1", MCPCapabilityIdentity{}, tools, func(string) bool { return true }); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// The vendor capability must now be part of the governed vocabulary.
	if _, ok := reg.Lookup("x:acme:translate"); !ok {
		t.Fatal("vendor capability x:acme:translate was not registered on the host registry")
	}
	// And it must be a provision on the tool's single dynamic contract.
	contract, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "srv1", "translate")
	if !ok {
		t.Fatal("no dynamic contract published for srv1/translate")
	}
	found := false
	for _, prov := range contract.Provisions {
		if prov.Capability == "x:acme:translate" {
			found = true
		}
	}
	if !found {
		t.Fatalf("published contract has no x:acme:translate provision: %+v", contract.Provisions)
	}
}

// TestPublishVendorDeclaredCapabilityListedNotReviewed closes the second Phase
// 1 acceptance point: the provider is surfaced via ListInstalledNotReviewedProviders
// (not silently hidden) while its namespace ceiling is still the host default.
func TestPublishVendorDeclaredCapabilityListedNotReviewed(t *testing.T) {
	reg := sealedTestRegistry(t)
	contracts := NewDynamicCapabilityRegistry()
	pub, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, reg, nil, nil)
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	tools := []ObservedMCPTool{
		{
			Name:                 "translate",
			InputSchema:          map[string]interface{}{"type": "object"},
			Annotations:          &MCPToolAnnotations{ReadOnlyHint: boolPtr(true)},
			DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"},
		},
	}
	if err := pub.PublishReviewedMCPObservationForCapabilityWithOpenLookup(principal, "srv1", MCPCapabilityIdentity{}, tools, func(string) bool { return true }); err != nil {
		t.Fatalf("publish: %v", err)
	}
	listed := pub.ListInstalledNotReviewedProviders()
	if len(listed) != 1 {
		t.Fatalf("expected exactly one installed-not-reviewed provider, got %d", len(listed))
	}
	if listed[0].ServerID != "srv1" {
		t.Fatalf("unexpected server id %q", listed[0].ServerID)
	}
	if listed[0].CeilingReviewed {
		t.Fatal("namespace still at default ceiling should report CeilingReviewed=false")
	}
	// The listing carries both the vendor self-declaration and the
	// host-issued exact capability x:mcp:srv1:translate — the latter is what
	// makes every installed tool addressable, not just vendors that declare.
	var sawVendor, sawAuto bool
	for _, capID := range listed[0].DeclaredCapabilities {
		switch capID {
		case "x:acme:translate":
			sawVendor = true
		case AutoMCPCapabilityIDForBinding("srv1", "translate"):
			sawAuto = true
		}
	}
	if !sawVendor || !sawAuto {
		t.Fatalf("unexpected declared capabilities: %+v", listed[0].DeclaredCapabilities)
	}
}

// TestPublishVendorDeclaredCapabilityEffectCeilingEnforced verifies a vendor
// declaration that exceeds its namespace effect ceiling is rejected and fails
// closed: the write-capable tool is not silently published and the provider is
// not surfaced as installed-not-reviewed.
func TestPublishVendorDeclaredCapabilityEffectCeilingEnforced(t *testing.T) {
	reg := sealedTestRegistry(t)
	// Host narrows the acme namespace to read-only only.
	if err := reg.SetExternalCapabilityCeiling("acme", []coretool.EffectClass{coretool.EffectReadOnly}); err != nil {
		t.Fatalf("set ceiling: %v", err)
	}
	contracts := NewDynamicCapabilityRegistry()
	pub, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, reg, nil, nil)
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	// A write/unannotated tool (external effect) declaring the cap.
	tools := []ObservedMCPTool{
		{
			Name:                 "translate",
			InputSchema:          map[string]interface{}{"type": "object"},
			DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"},
		},
	}
	if err := pub.PublishReviewedMCPObservationForCapabilityWithOpenLookup(principal, "srv1", MCPCapabilityIdentity{}, tools, func(string) bool { return true }); err == nil {
		t.Fatal("expected publish to fail closed for an over-ceiling vendor capability")
	}
	if _, ok := reg.Lookup("x:acme:translate"); ok {
		t.Fatal("over-ceiling vendor capability must not be registered")
	}
	// The over-ceiling vendor DECLARATION is what must not surface. The
	// server itself is still installed and keeps its host-issued exact
	// capability, so the provider appears — carrying no x:acme cap.
	for _, listed := range pub.ListInstalledNotReviewedProviders() {
		for _, capID := range listed.DeclaredCapabilities {
			if capID == "x:acme:translate" {
				t.Fatalf("over-ceiling vendor capability must not be surfaced: %+v", listed)
			}
		}
	}
	// But a read-only-annotated tool under the same narrowed ceiling still works.
	tools[0].Annotations = &MCPToolAnnotations{ReadOnlyHint: boolPtr(true)}
	if err := pub.PublishReviewedMCPObservationForCapabilityWithOpenLookup(principal, "srv1", MCPCapabilityIdentity{}, tools, func(string) bool { return true }); err != nil {
		t.Fatalf("read-only vendor cap under narrowed ceiling should publish: %v", err)
	}
	if _, ok := reg.Lookup("x:acme:translate"); !ok {
		t.Fatal("read-only vendor capability under narrowed ceiling should register")
	}
}

// TestRevokeClearsInstalledNotReviewed verifies revocation drops the provider
// from the installed-not-reviewed surface.
func TestRevokeClearsInstalledNotReviewed(t *testing.T) {
	reg := sealedTestRegistry(t)
	contracts := NewDynamicCapabilityRegistry()
	pub, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, reg, nil, nil)
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	tools := []ObservedMCPTool{
		{
			Name:                 "translate",
			InputSchema:          map[string]interface{}{"type": "object"},
			Annotations:          &MCPToolAnnotations{ReadOnlyHint: boolPtr(true)},
			DeclaredCapabilities: []coretool.CapabilityID{"x:acme:translate"},
		},
	}
	if err := pub.PublishReviewedMCPObservationForCapabilityWithOpenLookup(principal, "srv1", MCPCapabilityIdentity{}, tools, func(string) bool { return true }); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(pub.ListInstalledNotReviewedProviders()) != 1 {
		t.Fatal("provider should be listed before revoke")
	}
	if err := pub.RevokeObservedMCPServer(principal, "srv1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if len(pub.ListInstalledNotReviewedProviders()) != 0 {
		t.Fatal("provider must be cleared from installed-not-reviewed after revoke")
	}
}

func boolPtr(b bool) *bool { return &b }

// TestRecommendedDiscoveryBudgetScalesWithN closes the core Phase 3 requirement
// (plan §2.4): the remediation/recall budget grows with the number of
// unreviewed providers N and saturates at the cap, so installing many MCPs
// never silently starves discovery.
func TestRecommendedDiscoveryBudgetScalesWithN(t *testing.T) {
	cases := []struct {
		n    int
		want int
	}{
		{0, discoveryBudgetBase},                                    // 8
		{10, discoveryBudgetBase + 10*discoveryBudgetPerUnreviewed}, // 28
		{50, discoveryBudgetBase + 50*discoveryBudgetPerUnreviewed}, // 108
		{200, discoveryBudgetCap},                                   // saturates at 128
	}
	for _, c := range cases {
		got := RecommendedDiscoveryBudget(c.n)
		if got.PetitionSlots != c.want || got.SearchPasses != c.want {
			t.Fatalf("RecommendedDiscoveryBudget(%d) = %+v, want slots/passes=%d", c.n, got, c.want)
		}
	}
	// Monotonic: more unreviewed providers never yields a smaller budget (until cap).
	prev := -1
	for n := 0; n <= 200; n += 7 {
		b := RecommendedDiscoveryBudget(n)
		if b.PetitionSlots < prev {
			t.Fatalf("budget not monotonic at n=%d: %d < %d", n, b.PetitionSlots, prev)
		}
		prev = b.PetitionSlots
	}
}

// TestRecommendedDiscoveryBudgetForInstalledTracksInstalledCount closes the
// Phase 3 acceptance point end-to-end: install 50 unreviewed MCPs, assert the
// discovery surface lists all 50 as not-reviewed and the derived budget scales
// with that count (no longer "install too many → recall starves").
func TestRecommendedDiscoveryBudgetForInstalledTracksInstalledCount(t *testing.T) {
	reg := sealedTestRegistry(t)
	contracts := NewDynamicCapabilityRegistry()
	pub, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, reg, nil, nil)
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	const installed = 50
	for i := 0; i < installed; i++ {
		srv := fmt.Sprintf("srv%d", i)
		capID := coretool.CapabilityID(fmt.Sprintf("x:vendor%d:tool", i))
		tools := []ObservedMCPTool{
			{
				Name:                 "tool",
				InputSchema:          map[string]interface{}{"type": "object"},
				Annotations:          &MCPToolAnnotations{ReadOnlyHint: boolPtr(true)},
				DeclaredCapabilities: []coretool.CapabilityID{capID},
			},
		}
		if err := pub.PublishReviewedMCPObservationForCapabilityWithOpenLookup(principal, srv, MCPCapabilityIdentity{}, tools, func(string) bool { return true }); err != nil {
			t.Fatalf("publish %s: %v", srv, err)
		}
	}
	listed := pub.ListInstalledNotReviewedProviders()
	if len(listed) != installed {
		t.Fatalf("expected %d installed-not-reviewed providers, got %d", installed, len(listed))
	}
	for _, p := range listed {
		if p.CeilingReviewed {
			t.Fatalf("server %s should still be not-reviewed (default ceiling), got reviewed", p.ServerID)
		}
	}
	budget := pub.RecommendedDiscoveryBudgetForInstalled()
	want := discoveryBudgetBase + installed*discoveryBudgetPerUnreviewed // 108
	if budget.PetitionSlots != want || budget.SearchPasses != want {
		t.Fatalf("budget for %d installed = %+v, want %d", installed, budget, want)
	}
}

// TestPublishMCPStableIdentitySurvivesSchemaDrift closes the Phase 5 acceptance
// point (§2.5): a vendor parameter description/label tweak (schema drift) must
// keep the grant-stable identity (StableBindingKey + Digest) so in-flight
// grants do not fail-closed; only the observed schema digest rotates.
func TestPublishMCPStableIdentitySurvivesSchemaDrift(t *testing.T) {
	reg := sealedTestRegistry(t)
	contracts := NewDynamicCapabilityRegistry()
	pub, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, reg, nil, nil)
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	schemaV1 := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string", "description": "the query"}}}
	schemaV2 := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string", "description": "search query text"}}}
	toolsV1 := []ObservedMCPTool{{Name: "lookup", InputSchema: schemaV1, Annotations: &MCPToolAnnotations{ReadOnlyHint: boolPtr(true)}}}
	toolsV2 := []ObservedMCPTool{{Name: "lookup", InputSchema: schemaV2, Annotations: &MCPToolAnnotations{ReadOnlyHint: boolPtr(true)}}}
	if err := pub.PublishReviewedMCPObservationForCapabilityWithOpenLookup(principal, "srv1", MCPCapabilityIdentity{}, toolsV1, func(string) bool { return true }); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	c1, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "srv1", "lookup")
	if !ok {
		t.Fatal("no contract v1")
	}
	if err := pub.PublishReviewedMCPObservationForCapabilityWithOpenLookup(principal, "srv1", MCPCapabilityIdentity{}, toolsV2, func(string) bool { return true }); err != nil {
		t.Fatalf("publish v2: %v", err)
	}
	c2, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "srv1", "lookup")
	if !ok {
		t.Fatal("no contract v2")
	}
	if c1.StableBindingKey != c2.StableBindingKey {
		t.Fatalf("StableBindingKey rotated on schema drift: %q -> %q", c1.StableBindingKey, c2.StableBindingKey)
	}
	if c1.Digest() != c2.Digest() {
		t.Fatalf("Digest rotated on schema drift (in-flight grants would fail-closed): %q -> %q", c1.Digest(), c2.Digest())
	}
	if c1.ObservedBindingDigest == c2.ObservedBindingDigest {
		t.Fatalf("ObservedBindingDigest must still rotate on schema drift (drift detection/re-review hint)")
	}
}

// TestPublishMCPDigestRotatesOnEffectChange is the reverse validation: a
// capability/effect change (not mere schema drift) MUST rotate the grant
// identity, so in-flight grants are retired as intended. A vendor that starts
// writing external state is a substantive change, not a cosmetic one.
func TestPublishMCPDigestRotatesOnEffectChange(t *testing.T) {
	reg := sealedTestRegistry(t)
	contracts := NewDynamicCapabilityRegistry()
	pub, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, reg, nil, nil)
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}}
	readOnly := []ObservedMCPTool{{Name: "lookup", InputSchema: schema, Annotations: &MCPToolAnnotations{ReadOnlyHint: boolPtr(true)}}}
	unannotated := []ObservedMCPTool{{Name: "lookup", InputSchema: schema}} // fail-safe: EffectExternalEffect
	if err := pub.PublishReviewedMCPObservationForCapabilityWithOpenLookup(principal, "srv1", MCPCapabilityIdentity{}, readOnly, func(string) bool { return true }); err != nil {
		t.Fatalf("publish read-only: %v", err)
	}
	c1, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "srv1", "lookup")
	if !ok {
		t.Fatal("no read-only contract")
	}
	if err := pub.PublishReviewedMCPObservationForCapabilityWithOpenLookup(principal, "srv1", MCPCapabilityIdentity{}, unannotated, func(string) bool { return true }); err != nil {
		t.Fatalf("publish unannotated: %v", err)
	}
	c2, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "srv1", "lookup")
	if !ok {
		t.Fatal("no unannotated contract")
	}
	if c1.Digest() == c2.Digest() {
		t.Fatalf("Digest must rotate when effect changes read-only -> external_effect")
	}
	if c1.StableBindingKey != c2.StableBindingKey {
		t.Fatalf("StableBindingKey should hold across an effect change (same tool_name/capability): %q -> %q", c1.StableBindingKey, c2.StableBindingKey)
	}
}
