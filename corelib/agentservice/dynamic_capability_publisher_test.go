package agentservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func reviewedDynamicCapabilityRegistry(t *testing.T) *coretool.CapabilityRegistry {
	t.Helper()
	registry := coretool.NewCapabilityRegistry("dynamic-contracts-v1")
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      "document.lookup",
		Version: "v1",
		Qualifiers: map[string]coretool.QualifierConstraint{
			"scope": {Values: []string{"current"}, Required: true},
		},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Owner:   "routing-review",
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Seal(); err != nil {
		t.Fatal(err)
	}
	return registry
}

func reviewedDynamicContract() DynamicCapabilityContract {
	return DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{Capability: "document.lookup", Qualifiers: map[string]string{"scope": "current"}, Quality: 1}},
		Effects:    []coretool.EffectClass{coretool.EffectReadOnly},
	}
}

func newDynamicCapabilityPublisherForTest(t *testing.T) (*Service, *DynamicCapabilityContractPublisher) {
	t.Helper()
	svc, err := NewService(Config{DataRoot: t.TempDir()}, nil, EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := NewDynamicCapabilityContractPublisher(svc, reviewedDynamicCapabilityRegistry(t))
	if err != nil {
		_ = svc.Close()
		t.Fatal(err)
	}
	return svc, publisher
}

func TestDynamicCapabilityContractPublisherRejectsUnreviewedRegistry(t *testing.T) {
	svc, err := NewService(Config{DataRoot: t.TempDir()}, nil, EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	registry := coretool.NewCapabilityRegistry("unsealed-v1")
	if _, err := NewDynamicCapabilityContractPublisher(svc, registry); err == nil {
		t.Fatal("publisher accepted an unsealed capability registry")
	}
}

func TestDynamicCapabilityContractPublisherRejectsUnknownCapability(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	contract := reviewedDynamicContract()
	contract.Provisions[0].Capability = "unreviewed.capability"
	if err := publisher.publishObservedMCP(p, "server", "lookup", contract); err == nil {
		t.Fatal("publisher accepted an unknown capability")
	}
}

func TestDynamicCapabilityContractPublisherRejectsInvalidQualifier(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	contract := reviewedDynamicContract()
	contract.Provisions[0].Qualifiers = map[string]string{"scope": "other"}
	if err := publisher.publishObservedMCP(p, "server", "lookup", contract); err == nil {
		t.Fatal("publisher accepted an invalid qualifier value")
	}
}

func TestDynamicCapabilityContractPublisherRejectsEffectEscalation(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	contract := reviewedDynamicContract()
	contract.Effects = []coretool.EffectClass{coretool.EffectExternalEffect}
	if err := publisher.publishObservedSkill(p, "vendor.lookup", contract); err == nil {
		t.Fatal("publisher accepted an effect escalation")
	}
}

func TestDynamicCapabilityContractPublisherRejectsUnreviewedArtifactContract(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	contract := reviewedDynamicContract()
	contract.Produces = []coretool.ArtifactContract{{Kind: "unreviewed", MIMEType: "application/octet-stream"}}
	contract.ObservedBindingDigest = "observed"
	if err := publisher.publishObservedSkill(p, "vendor.lookup", contract); err == nil {
		t.Fatal("publisher accepted an artifact contract absent from the capability descriptor")
	}
}

func TestDynamicCapabilityContractPublisherRejectsUnobservedBinding(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	if err := publisher.publishObservedMCP(p, "server", "lookup", reviewedDynamicContract()); err == nil {
		t.Fatal("publisher accepted a contract without a Service-observed binding digest")
	}
}

func TestDynamicCapabilityContractPublisherRequiresObservedMCPBinding(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	if err := svc.EnsurePrincipal(context.Background(), p, "user@example.test", "User"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateMCPServer(context.Background(), p, MCPServerCreateInput{Kind: "remote", Name: "remote", EndpointURL: "https://mcp.example"}); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishObservedMCP(context.Background(), p, "missing", "lookup", reviewedDynamicContract()); err == nil {
		t.Fatal("publisher accepted an MCP binding absent from the ready inventory")
	}
}

func TestDynamicCapabilityContractPublisherRequiresService(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDynamicCapabilityContractPublisher(nil, registry); err == nil {
		t.Fatal("service publisher accepted a nil service")
	}
}

type stubMCPObserver struct {
	tools    []ObservedMCPTool
	observed bool
}

func (s stubMCPObserver) ObservedMCPTools(context.Context, Principal, string) ([]ObservedMCPTool, bool, error) {
	return s.tools, s.observed, nil
}

func TestReviewedMCPObservationPublishesExactToolNameOnly(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	contracts := NewDynamicCapabilityRegistry()
	publisher, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, registry, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ReviewedMCPCapabilityContracts(); len(got) != 1 || got["work_log_query"].ObservedBindingDigest != "" || got["work_log_query"].Provisions[0].Capability != coretool.CapabilityRecordReadWorklog {
		t.Fatalf("reviewed MCP map=%#v", got)
	}
	if len(ReviewedSkillCapabilityContracts()) != 0 {
		t.Fatalf("reviewed skill map must stay empty until a stable ID is reviewed: %#v", ReviewedSkillCapabilityContracts())
	}
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"day": map[string]interface{}{"type": "string"}}}
	principal := Principal{TenantID: "desktop", UserID: "desktop-user"}
	// work_log_update is absent from the reviewed map even when a description
	// would say it edits a work record. The observation carries no description
	// into the publisher.
	if err := publisher.PublishReviewedMCPObservation(principal, "tengyun", []ObservedMCPTool{
		{Name: "work_log_update", InputSchema: schema},
		{Name: "work_log_query", InputSchema: schema},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "tengyun", "work_log_update"); ok {
		t.Fatal("unlisted mutation tool was published")
	}
	contract, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "tengyun", "work_log_query")
	wantDigest := DynamicMCPObservedBindingDigest("tengyun", "work_log_query", schema)
	if !ok || contract.ObservedBindingDigest != wantDigest || contract.Provisions[0].Capability != coretool.CapabilityRecordReadWorklog || contract.Provisions[0].Quality != 3 || contract.Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("query contract=%#v ok=%v", contract, ok)
	}
	changed := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}}
	if err := publisher.PublishReviewedMCPObservation(principal, "tengyun", []ObservedMCPTool{{Name: "work_log_query", InputSchema: changed}}); err != nil {
		t.Fatal(err)
	}
	contract, ok = contracts.ResolveMCPDynamicContract(context.Background(), principal, "tengyun", "work_log_query")
	if !ok || contract.ObservedBindingDigest != DynamicMCPObservedBindingDigest("tengyun", "work_log_query", changed) {
		t.Fatalf("schema change did not replace the digest: %#v", contract)
	}
	if err := publisher.PublishReviewedMCPObservation(principal, "tengyun", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "tengyun", "work_log_query"); ok {
		t.Fatal("disappeared reviewed tool stayed published")
	}
}

func TestReviewedMCPCapabilityBindingJoinsIdentityNotToolName(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	contracts := NewDynamicCapabilityRegistry()
	publisher, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, registry, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ReviewedMCPCapabilityContracts(); len(got) != 1 || got[ReviewedMCPWebSearchPrimeTool].Provisions != nil {
		t.Fatalf("product tool must stay out of the server-agnostic map: %#v", got)
	}
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"search_query": map[string]interface{}{"type": "string"},
		},
		"required": []string{"search_query"},
	}
	principal := Principal{TenantID: "desktop", UserID: "desktop-user"}
	tools := []ObservedMCPTool{
		{Name: ReviewedMCPWebSearchPrimeTool, InputSchema: schema},
		{Name: "extra_tool", InputSchema: schema},
		{Name: "work_log_query", InputSchema: schema},
	}
	instanceOnly := MCPCapabilityIdentity{CapabilityID: "cap_43f40e502b689144"}
	if err := publisher.PublishReviewedMCPObservationForCapability(principal, "web-search-prime", instanceOnly, tools); err != nil {
		t.Fatal(err)
	}
	if _, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "web-search-prime", ReviewedMCPWebSearchPrimeTool); ok {
		t.Fatal("install-instance id published the search tool")
	}
	if _, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "web-search-prime", "work_log_query"); !ok {
		t.Fatal("server-agnostic tool was dropped")
	}
	if err := publisher.PublishReviewedMCPObservationForCapability(principal, "web-search-prime", MCPCapabilityIdentity{GlobalKey: "enterprise_hub:mcp:hubcenter:other"}, tools); err != nil {
		t.Fatal(err)
	}
	if _, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "web-search-prime", ReviewedMCPWebSearchPrimeTool); ok {
		t.Fatal("unreviewed global key published the search tool")
	}
	if err := publisher.PublishReviewedMCPObservationForCapability(principal, "web-search-prime", MCPCapabilityIdentity{
		GlobalKey: ReviewedMCPWebSearchPrimeGlobalKey, CapabilityID: "cap_43f40e502b689144",
	}, tools); err != nil {
		t.Fatal(err)
	}
	contract, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "web-search-prime", ReviewedMCPWebSearchPrimeTool)
	wantDigest := DynamicMCPObservedBindingDigest("web-search-prime", ReviewedMCPWebSearchPrimeTool, schema)
	if !ok || contract.ObservedBindingDigest != wantDigest || len(contract.Provisions) != 2 || contract.Provisions[0].Quality <= 2 || contract.Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("search contract=%#v ok=%v", contract, ok)
	}
	var sawReference, sawCurrent bool
	for _, provision := range contract.Provisions {
		if provision.Capability != CapabilityInformationSearchWeb {
			t.Fatalf("provision=%#v", provision)
		}
		switch provision.Qualifiers[QualifierSearchFreshness] {
		case SearchFreshnessReference:
			sawReference = true
		case SearchFreshnessCurrent:
			sawCurrent = true
		}
	}
	if !sawReference || !sawCurrent {
		t.Fatalf("freshness provisions=%#v", contract.Provisions)
	}
	if _, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "web-search-prime", "extra_tool"); ok {
		t.Fatal("unlisted tool on a matching server was published")
	}
	if err := publisher.PublishReviewedMCPObservation(principal, "web-search-prime", tools); err != nil {
		t.Fatal(err)
	}
	if _, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "web-search-prime", ReviewedMCPWebSearchPrimeTool); ok {
		t.Fatal("cleared capability identity kept the product contract")
	}
	if _, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "web-search-prime", "work_log_query"); !ok {
		t.Fatal("clearing capability identity revoked the server-agnostic tool")
	}
}

func TestReviewedSkillObservationUsesFixtureLookupOnly(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	contracts := NewDynamicCapabilityRegistry()
	publisher, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, registry, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "desktop", UserID: "user-1"}
	unlisted := corelib.NLSkillEntry{SkillID: "huashu-art-motion", Name: "motion", Version: "1", Description: "update the morning work log"}
	if err := publisher.PublishReviewedSkillObservation(principal, []corelib.NLSkillEntry{unlisted}); err != nil {
		t.Fatal(err)
	}
	if _, ok := contracts.ResolveSkillDynamicContract(context.Background(), principal, DynamicSkillStableID(unlisted)); ok {
		t.Fatal("unreviewed skill was published from the production map")
	}
	fixture := corelib.NLSkillEntry{SkillID: "fixture.worklog.read", Name: "fixture reader", Version: "1"}
	publisher.UseReviewedBindings(nil, map[string]DynamicCapabilityContract{
		DynamicSkillStableID(fixture): {
			Provisions:            []coretool.CapabilityProvision{{Capability: coretool.CapabilityRecordReadWorklog, Quality: 3}},
			Effects:               []coretool.EffectClass{coretool.EffectReadOnly},
			ObservedBindingDigest: "forged-by-caller",
		},
	})
	if err := publisher.PublishReviewedSkillObservation(principal, []corelib.NLSkillEntry{unlisted, fixture}); err != nil {
		t.Fatal(err)
	}
	if _, ok := contracts.ResolveSkillDynamicContract(context.Background(), principal, DynamicSkillStableID(unlisted)); ok {
		t.Fatal("unlisted skill became routable beside a fixture")
	}
	contract, ok := contracts.ResolveSkillDynamicContract(context.Background(), principal, DynamicSkillStableID(fixture))
	want := DynamicSkillObservedBindingDigest(DynamicSkillStableID(fixture), fixture.Version, DynamicSkillContentDigest(fixture))
	if !ok || contract.ObservedBindingDigest != want || contract.ObservedBindingDigest == "forged-by-caller" {
		t.Fatalf("fixture skill contract=%#v ok=%v wantDigest=%s", contract, ok, want)
	}
}

func TestLifecyclePublisherComputesMCPDigestFromObserver(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	schema := map[string]interface{}{"type": "object"}
	contracts := NewDynamicCapabilityRegistry()
	publisher, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, registry, stubMCPObserver{
		observed: true,
		tools:    []ObservedMCPTool{{Name: "work_log_query", InputSchema: schema}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "desktop", UserID: "desktop-user"}
	forged := reviewedWorklogReadContract()
	forged.ObservedBindingDigest = "forged-by-caller"
	if err := publisher.PublishObservedMCP(context.Background(), principal, "tengyun", "work_log_query", forged); err != nil {
		t.Fatal(err)
	}
	contract, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "tengyun", "work_log_query")
	want := DynamicMCPObservedBindingDigest("tengyun", "work_log_query", schema)
	if !ok || contract.ObservedBindingDigest != want {
		t.Fatalf("observer digest=%q want=%q ok=%v", contract.ObservedBindingDigest, want, ok)
	}
	miss, err := NewLifecycleDynamicCapabilityContractPublisher(contracts, registry, stubMCPObserver{observed: false}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := miss.PublishObservedMCP(context.Background(), principal, "other", "work_log_query", reviewedWorklogReadContract()); err == nil {
		t.Fatal("cache miss published a contract")
	}
	if _, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "other", "work_log_query"); ok {
		t.Fatal("cache miss revoked or published a server that was not observed")
	}
}

func TestObservedBindingDigestsQuarantineSchemaAndContentDrift(t *testing.T) {
	if dynamicMCPContractMatchesEntry(reviewedDynamicContract(), MCPToolEntry{ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object"}}) {
		t.Fatal("MCP contract without an observed binding digest was routable")
	}
	mcpContract := reviewedDynamicContract()
	mcpContract.ObservedBindingDigest = dynamicMCPObservedBindingDigest("server", "lookup", map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}})
	matchingMCP := MCPToolEntry{ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}}, Contract: mcpContract}
	if !dynamicMCPContractMatchesEntry(mcpContract, matchingMCP) {
		t.Fatal("matching MCP binding was rejected")
	}
	matchingMCP.InputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{"page": map[string]interface{}{"type": "integer"}}}
	if dynamicMCPContractMatchesEntry(mcpContract, matchingMCP) {
		t.Fatal("MCP schema drift retained a contract")
	}

	if dynamicSkillContractMatchesEntry(reviewedDynamicContract(), SkillToolEntry{StableID: "vendor.lookup", Name: "lookup", Version: "v1", ContentDigest: "content-v1"}) {
		t.Fatal("Skill contract without an observed binding digest was routable")
	}
	skillContract := reviewedDynamicContract()
	skillContract.ObservedBindingDigest = dynamicSkillObservedBindingDigest("vendor.lookup", "v1", "content-v1")
	skillEntry := SkillToolEntry{StableID: "vendor.lookup", Name: "lookup", Version: "v1", ContentDigest: "content-v1", Contract: skillContract}
	if !dynamicSkillContractMatchesEntry(skillContract, skillEntry) {
		t.Fatal("matching Skill binding was rejected")
	}
	skillEntry.ContentDigest = "content-v2"
	if dynamicSkillContractMatchesEntry(skillContract, skillEntry) {
		t.Fatal("Skill content drift retained a contract")
	}
}
