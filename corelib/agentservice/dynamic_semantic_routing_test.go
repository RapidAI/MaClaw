package agentservice

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

type dynamicNeedResolverFunc func(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error)

func (f dynamicNeedResolverFunc) ResolveDynamicCapabilityNeeds(ctx context.Context, req DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
	return f(ctx, req)
}

type coveredMCPProviderStub struct {
	*boundMCPProviderStub
	lifecycle DynamicCatalogLifecycle
}

func (s *coveredMCPProviderStub) DynamicCatalogLifecycle(context.Context, Principal) DynamicCatalogLifecycle {
	return s.lifecycle
}

type coveredSkillProviderStub struct {
	*boundSkillProviderStub
	lifecycle DynamicCatalogLifecycle
}

func (s *coveredSkillProviderStub) DynamicCatalogLifecycle(context.Context, Principal) DynamicCatalogLifecycle {
	return s.lifecycle
}

// atomicMCPInventoryProviderStub deliberately also has the legacy provider
// methods through its embedded stub. The counters prove that a semantic turn
// never joins a list observation to a later lifecycle observation when the
// provider offers the atomic boundary.
type atomicMCPInventoryProviderStub struct {
	*boundMCPProviderStub
	entries      []MCPToolEntry
	lifecycle    DynamicCatalogLifecycle
	atomicCalls  int
	legacyLists  int
	legacyHealth int
}

func (s *atomicMCPInventoryProviderStub) ListAvailableTools(context.Context, Principal) []MCPToolEntry {
	s.legacyLists++
	return nil
}

func (s *atomicMCPInventoryProviderStub) DynamicCatalogLifecycle(context.Context, Principal) DynamicCatalogLifecycle {
	s.legacyHealth++
	return IncompleteDynamicCatalogLifecycle("must_not_be_used")
}

func (s *atomicMCPInventoryProviderStub) DynamicMCPInventory(context.Context, Principal) ([]MCPToolEntry, DynamicCatalogLifecycle) {
	s.atomicCalls++
	return append([]MCPToolEntry(nil), s.entries...), s.lifecycle
}

type atomicSkillInventoryProviderStub struct {
	*boundSkillProviderStub
	entries      []SkillToolEntry
	lifecycle    DynamicCatalogLifecycle
	atomicCalls  int
	legacyLists  int
	legacyHealth int
}

func (s *atomicSkillInventoryProviderStub) ListSkills(context.Context, Principal) []SkillToolEntry {
	s.legacyLists++
	return nil
}

func (s *atomicSkillInventoryProviderStub) DynamicCatalogLifecycle(context.Context, Principal) DynamicCatalogLifecycle {
	s.legacyHealth++
	return IncompleteDynamicCatalogLifecycle("must_not_be_used")
}

func (s *atomicSkillInventoryProviderStub) DynamicSkillInventory(context.Context, Principal) ([]SkillToolEntry, DynamicCatalogLifecycle) {
	s.atomicCalls++
	return append([]SkillToolEntry(nil), s.entries...), s.lifecycle
}

func TestCoreDynamicSemanticSurfaceDoesNotBulkExposeMCP(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := dynamicNeedResolverFunc(func(_ context.Context, request DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
		if request.UserText != "find the approved report" {
			t.Fatalf("resolver received unexpected text %q", request.UserText)
		}
		return DynamicCapabilityNeedResolution{Managed: true, Needs: []coretool.CapabilityNeed{{
			ID: "need:execute", Capability: "test.dynamic.execute", Polarity: coretool.NeedRequire, Required: true,
		}}}, nil
	})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(),
		RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute,
	}
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{
		{ServerID: "approved", ToolName: "report", InputSchema: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}, "required": []string{"q"}, "additionalProperties": false,
		}, Contract: testDynamicCapabilityContract()},
		// This second discovered entry is deliberately valid but lower quality.
		// It must stay invisible; discovery volume cannot expand this request.
		{ServerID: "other", ToolName: "unrelated", InputSchema: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false,
		}, Contract: testDynamicCapabilityContract()},
	}}
	cb := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "find the approved report",
		loopID: "session", dynamicOperationScope: "root", mcpProvider: provider, dynamicSemanticRouting: &routing,
	}
	defs, managed := cb.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("semantic defs=%#v managed=%v", defs, managed)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	if strings.Contains(name, "approved") || strings.Contains(name, "report") || strings.Contains(name, "mcp") {
		t.Fatalf("rendered semantic grant leaked provider identity: %q", name)
	}
	if !cb.IsToolAllowed(name) {
		t.Fatalf("semantic grant was removed by legacy name policy: %q", name)
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"q":"weekly"}`); !allowed || reason != "" {
		t.Fatalf("semantic grant call authorization=%v reason=%q", allowed, reason)
	}
	result := cb.ExecuteToolCall(name, `{"q":"weekly"}`, "call-1")
	if result.Outcome != "ok" || result.Result != "bound" || provider.boundCalls != 1 || provider.legacyCalls != 0 {
		t.Fatalf("semantic execution=%+v bound=%d legacy=%d", result, provider.boundCalls, provider.legacyCalls)
	}
	if replay := cb.ExecuteToolCall(name, `{"q":"weekly"}`, "call-1"); replay.Outcome != "ok" || replay.Result != "bound" || provider.boundCalls != 1 {
		t.Fatalf("host-call replay=%+v bound=%d", replay, provider.boundCalls)
	}
	if defs, managed := cb.dynamicSemanticToolDefinitions(); !managed || len(defs) != 0 {
		t.Fatalf("consumed grant was re-rendered defs=%#v managed=%v", defs, managed)
	}
}

func TestCoreDynamicSemanticSurfaceUsesAtomicMCPInventory(t *testing.T) {
	routing := testDynamicSemanticRouting(t)
	provider := &atomicMCPInventoryProviderStub{
		boundMCPProviderStub: &boundMCPProviderStub{},
		entries: []MCPToolEntry{{
			ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: testDynamicCapabilityContract(),
		}},
		lifecycle: CompleteDynamicCatalogLifecycle(),
	}
	callback := testManagedDynamicCallback(routing, provider, nil)
	if defs, managed := callback.dynamicSemanticToolDefinitions(); !managed || len(defs) != 1 {
		t.Fatalf("atomic MCP inventory definitions=%#v managed=%v", defs, managed)
	}
	if provider.atomicCalls != 1 || provider.legacyLists != 0 || provider.legacyHealth != 0 {
		t.Fatalf("MCP inventory calls atomic=%d list=%d lifecycle=%d", provider.atomicCalls, provider.legacyLists, provider.legacyHealth)
	}
}

func TestCoreDynamicSemanticSurfaceUsesAtomicSkillInventory(t *testing.T) {
	routing := testDynamicSemanticRouting(t)
	provider := &atomicSkillInventoryProviderStub{
		boundSkillProviderStub: &boundSkillProviderStub{},
		entries: []SkillToolEntry{{
			StableID: "skill", Name: "lookup", Version: "v1", ContentDigest: "content-v1", Contract: testDynamicCapabilityContract(),
		}},
		lifecycle: CompleteDynamicCatalogLifecycle(),
	}
	callback := testManagedDynamicCallback(routing, nil, provider)
	if defs, managed := callback.dynamicSemanticToolDefinitions(); !managed || len(defs) != 1 {
		t.Fatalf("atomic Skill inventory definitions=%#v managed=%v", defs, managed)
	}
	if provider.atomicCalls != 1 || provider.legacyLists != 0 || provider.legacyHealth != 0 {
		t.Fatalf("Skill inventory calls atomic=%d list=%d lifecycle=%d", provider.atomicCalls, provider.legacyLists, provider.legacyHealth)
	}
}

func testDynamicSemanticRouting(t *testing.T) DynamicSemanticRouting {
	t.Helper()
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return DynamicSemanticRouting{
		Registry: dynamicSemanticRegistry(t), Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(),
		RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute,
		Resolver: dynamicNeedResolverFunc(func(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
			return DynamicCapabilityNeedResolution{Managed: true, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}}, nil
		}),
	}
}

func testManagedDynamicCallback(routing DynamicSemanticRouting, mcp MCPToolProvider, skill SkillToolProvider) *coreAgentCallbacks {
	return &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "lookup", loopID: "session",
		dynamicOperationScope: "root", mcpProvider: mcp, skillProvider: skill, dynamicSemanticRouting: &routing,
	}
}

type fixedIntentClassificationSource struct{ result intent.ClassificationResult }

func (s fixedIntentClassificationSource) Classify(intent.MessageContext) intent.ClassificationResult {
	return s.result
}

func TestIntentCapabilityNeedResolverIgnoresLegacyToolNames(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelSearch, Confidence: .95,
			// This intentionally malicious legacy field must have no influence.
			ToolNames: []string{"invoke_mcp_everything", "send_secrets"},
		}},
		Registry: registry,
		Rules: map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
			intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
		},
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "search"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%+v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != "test.dynamic.execute" || strings.Contains(resolution.Needs[0].ID, "mcp") {
		t.Fatalf("legacy tool name influenced semantic need: %#v", resolution.Needs[0])
	}
}

func TestIntentCapabilityNeedResolverSkipsGenericNonCoding(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelNonCoding, Confidence: .78,
		}},
		Registry: registry,
		Rules: map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
			intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
		},
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "conceptual question"})
	if err != nil || resolution.Managed || len(resolution.Needs) != 0 {
		t.Fatalf("generic non_coding must not be a coverage gap, resolution=%+v err=%v", resolution, err)
	}
}

func TestIntentCapabilityNeedResolverGenericLabelDoesNotDiscardGovernedNeed(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelNonCoding}, Confidence: .95,
		}},
		Registry: registry,
		Rules: map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
			intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
		},
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "search"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("generic secondary must not discard governed need, resolution=%+v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != "test.dynamic.execute" {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
}

func TestIntentCapabilityNeedResolverGenericPrimaryDoesNotBlockSecondaryNeed(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelNonCoding, Secondary: []intent.IntentLabel{intent.LabelSearch}, Confidence: .95,
		}},
		Registry: registry,
		Rules: map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
			intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
		},
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "search"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 || resolution.Needs[0].Capability != "test.dynamic.execute" {
		t.Fatalf("generic primary must not block governed secondary, resolution=%+v err=%v", resolution, err)
	}
}

func TestIntentCapabilityNeedResolverUnmappedOnlyLabelIsUnmanaged(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelCoding, Confidence: .95,
		}},
		Registry: registry,
		Rules: map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
			intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
		},
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "write a function"})
	if err != nil || resolution.Managed || len(resolution.Needs) != 0 {
		t.Fatalf("unmapped-only coding must not strip tools, resolution=%+v err=%v", resolution, err)
	}
}

func TestIntentCapabilityNeedResolverRefusesCodingPlusSearchAsPartialMigration(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelCoding, Secondary: []intent.IntentLabel{intent.LabelSearch}, Confidence: .95,
		}},
		Registry: registry,
		Rules: map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
			intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
		},
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "code and search"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 0 {
		t.Fatalf("coding+search must fail closed as mixed coverage, resolution=%+v err=%v", resolution, err)
	}
}

func TestIntentCapabilityNeedResolverRefusesPartialMixedIntentMigration(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelDocumentDelivery}, Confidence: .95,
		}},
		Registry: registry,
		Rules: map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
			intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
		},
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "search and deliver"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 0 {
		t.Fatalf("mixed migrated/unmigrated intent must fail closed, resolution=%+v err=%v", resolution, err)
	}
}

func TestDynamicSemanticManagedRequestFailsClosedWithoutCapabilityPolicyAdapter(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	routing := DynamicSemanticRouting{
		Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(),
		RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute,
		Resolver: dynamicNeedResolverFunc(func(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
			return DynamicCapabilityNeedResolution{Managed: true, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}}, nil
		}),
	}
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{{ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object"}, Contract: testDynamicCapabilityContract()}}}
	callback := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "lookup",
		loopID: "session", dynamicOperationScope: "root", mcpProvider: provider, dynamicSemanticRouting: &routing,
		toolPolicy: v2.ToolPolicyDocOnly, mutationScope: v2.MutationScopeArtifact,
	}
	if defs, managed := callback.dynamicSemanticToolDefinitions(); !managed || len(defs) != 0 {
		t.Fatalf("restrictive unmanaged policy exposed semantic definitions=%#v managed=%v", defs, managed)
	}
}

func TestCoreDynamicSemanticSurfaceReportsIncompleteInventoryWithoutLegacyFallback(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	routing := DynamicSemanticRouting{
		Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(),
		RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute,
		Resolver: dynamicNeedResolverFunc(func(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
			return DynamicCapabilityNeedResolution{Managed: true, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}}, nil
		}),
	}
	provider := &coveredMCPProviderStub{boundMCPProviderStub: &boundMCPProviderStub{}, lifecycle: IncompleteDynamicCatalogLifecycle("provider_not_ready")}
	callback := &coreAgentCallbacks{ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "lookup", loopID: "session", dynamicOperationScope: "root", mcpProvider: provider, dynamicSemanticRouting: &routing}
	if defs, managed := callback.dynamicSemanticToolDefinitions(); !managed || len(defs) != 0 {
		t.Fatalf("incomplete dynamic inventory exposed a legacy or semantic adapter defs=%#v managed=%v", defs, managed)
	}
	if callback.dynamicSemanticSurface != nil {
		t.Fatal("incomplete inventory materialized a semantic surface")
	}
}

func TestCoreDynamicSemanticSurfaceAcceptsExplicitCompleteEmptyInventory(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	routing := DynamicSemanticRouting{
		Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(),
		RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute,
		Resolver: dynamicNeedResolverFunc(func(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
			return DynamicCapabilityNeedResolution{Managed: true, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}}, nil
		}),
	}
	provider := &coveredMCPProviderStub{boundMCPProviderStub: &boundMCPProviderStub{}, lifecycle: CompleteDynamicCatalogLifecycle()}
	callback := &coreAgentCallbacks{ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "lookup", loopID: "session", dynamicOperationScope: "root", mcpProvider: provider, dynamicSemanticRouting: &routing}
	if defs, managed := callback.dynamicSemanticToolDefinitions(); !managed || len(defs) != 0 {
		t.Fatalf("complete empty inventory exposed a tool defs=%#v managed=%v", defs, managed)
	}
	if callback.dynamicSemanticSurface != nil {
		t.Fatal("no-feasible-provider inventory materialized a semantic surface")
	}
}

func TestCoreDynamicSemanticSurfaceRetainsReadyFamilyWhenSiblingLoading(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	routing := DynamicSemanticRouting{
		Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(),
		RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute,
		Resolver: dynamicNeedResolverFunc(func(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
			return DynamicCapabilityNeedResolution{Managed: true, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}}, nil
		}),
	}
	skill := &coveredSkillProviderStub{boundSkillProviderStub: &boundSkillProviderStub{entries: []SkillToolEntry{{
		StableID: "skill", Name: "lookup", Version: "v1", ContentDigest: "content-v1",
		Params: []corelib.NLSkillParam{}, Contract: testDynamicCapabilityContract(),
	}}}, lifecycle: CompleteDynamicCatalogLifecycle()}
	mcp := &coveredMCPProviderStub{boundMCPProviderStub: &boundMCPProviderStub{}, lifecycle: IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady)}
	callback := &coreAgentCallbacks{ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "lookup", loopID: "session", dynamicOperationScope: "root", mcpProvider: mcp, skillProvider: skill, dynamicSemanticRouting: &routing}
	if defs, managed := callback.dynamicSemanticToolDefinitions(); !managed || len(defs) != 1 {
		t.Fatalf("ready Skill family was hidden by loading MCP family defs=%#v managed=%v", defs, managed)
	}
}

func TestMergeDynamicCatalogLifecyclesPreservesFamilyWatermarks(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	merged := mergeDynamicCatalogLifecycles([]DynamicCatalogLifecycle{
		dynamicCatalogLifecycleForKind("skill", CompleteDynamicCatalogLifecycle()),
		dynamicCatalogLifecycleForKind("mcp", StaleDynamicCatalogLifecycle(now.Add(time.Minute))),
	})
	if merged.Coverage.State != coretool.CatalogCoverageStale || len(merged.Coverage.Families) != 2 {
		t.Fatalf("family lifecycle was collapsed: %#v", merged)
	}
	if merged.Coverage.Families[0].Kind != "mcp" || merged.Coverage.Families[1].Kind != "skill" {
		t.Fatalf("family lifecycle ordering is unstable: %#v", merged.Coverage.Families)
	}
	if got := merged.Coverage.ForProviderKind("skill"); got.State != coretool.CatalogCoverageComplete {
		t.Fatalf("complete family lost its watermark: %#v", got)
	}
}

func TestStaticCapabilityPolicyAdapterConstrainsSemanticPlan(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	provider, _, _, err := ProjectMCPDynamicProvider(MCPToolEntry{ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: testDynamicCapabilityContract()})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish([]coretool.ProviderSpec{provider}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	request := DynamicCapabilityNeedRequest{WorkflowPolicy: "doc_only", MutationScope: "artifact"}
	adapter := StaticCapabilityPolicyAdapter{Rules: []CapabilityPolicyRule{{
		WorkflowPolicy: "doc_only", MutationScope: "artifact",
		Constraints: []coretool.RoutingConstraint{{ID: "deny-external", Capability: "test.dynamic.execute", Effect: "deny", Authority: coretool.AuthorityPolicy}},
	}}}
	_, constraints, err := adapter.DynamicCapabilityConstraints(request)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}, Constraints: constraints,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selections) != 0 || len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != "policy_denied" {
		t.Fatalf("deny constraint did not fail closed: %#v", plan)
	}

	adapter = StaticCapabilityPolicyAdapter{Rules: []CapabilityPolicyRule{{
		WorkflowPolicy: "doc_only", MutationScope: "artifact",
		Constraints: []coretool.RoutingConstraint{{ID: "confirmation", Capability: "test.dynamic.execute", Effect: "require_confirmation", Authority: coretool.AuthorityPolicy}},
	}}}
	_, constraints, err = adapter.DynamicCapabilityConstraints(request)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}, Constraints: constraints,
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("confirmation policy plan=%#v err=%v", plan, err)
	}
	selection := plan.Selections[0]
	if !selection.RequiresConfirm || len(selection.Requires) != 1 || selection.Requires[0] != coretool.ConfirmationRequirementID("need") {
		t.Fatalf("confirmation did not produce a trusted DAG dependency: %#v", selection)
	}
}

func TestCoreDynamicSemanticSurfaceHonorsTrustedConfirmationFacts(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	entry := MCPToolEntry{ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: testDynamicCapabilityContract()}
	catalog, err := BuildDynamicSemanticCatalog([]MCPToolEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish(catalog.Providers, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: snapshot,
		Needs:       []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}},
		Constraints: []coretool.RoutingConstraint{{ID: "confirmation", Capability: "test.dynamic.execute", Effect: "require_confirmation", Authority: coretool.AuthorityPolicy}},
	})
	if err != nil {
		t.Fatal(err)
	}
	routing := DynamicSemanticRouting{Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute}
	scope := coretool.InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "tenant:user"}
	withoutConfirmation, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	if defs, err := withoutConfirmation.Definitions(); err != nil || len(defs) != 0 {
		t.Fatalf("unconfirmed selection became visible defs=%#v err=%v", defs, err)
	}
	confirmedFact := coretool.RoutingFact{ID: "approval-1", Kind: "confirmation_granted", Authority: coretool.AuthorityPolicy, Attributes: map[string]string{
		"root_task_id": "root", "confirmation_requirement": coretool.ConfirmationRequirementID("need"),
	}}
	withConfirmation, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope, []coretool.RoutingFact{confirmedFact})
	if err != nil {
		t.Fatal(err)
	}
	defs, err := withConfirmation.Definitions()
	if err != nil || len(defs) != 1 {
		t.Fatalf("trusted confirmation did not open selection defs=%#v err=%v", defs, err)
	}
	// Confirmation is a trusted route fact rather than a one-surface hint. A
	// recovered revision can project it as a DAG dependency, but still receives
	// a newly materialized invocation grant rather than reusing an old one.
	recovered, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	recoveredDefs, err := recovered.Definitions()
	if err != nil || len(recoveredDefs) != 1 {
		t.Fatalf("recovered confirmation did not open selection defs=%#v err=%v", recoveredDefs, err)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{entry}}
	result, handled := withConfirmation.Execute(context.Background(), Principal{TenantID: "tenant", UserID: "user"}, provider, nil, name, `{}`, "call-1")
	if !handled || !result.Succeeded || provider.boundCalls != 1 {
		t.Fatalf("confirmed execution=%+v handled=%v calls=%d", result, handled, provider.boundCalls)
	}
}

func TestStaticCapabilityPolicyAdapterRejectsUntrustedOrAmbiguousRules(t *testing.T) {
	request := DynamicCapabilityNeedRequest{WorkflowPolicy: "full", MutationScope: "project"}
	for _, adapter := range []StaticCapabilityPolicyAdapter{
		{Rules: []CapabilityPolicyRule{{Facts: []coretool.RoutingFact{{ID: "user-fact", Kind: "anything", Authority: coretool.AuthorityUser}}}}},
		{Rules: []CapabilityPolicyRule{{Constraints: []coretool.RoutingConstraint{{ID: "unknown-effect", Capability: "test.dynamic.execute", Effect: "allow", Authority: coretool.AuthorityPolicy}}}}},
		{Rules: []CapabilityPolicyRule{{Constraints: []coretool.RoutingConstraint{{ID: "duplicate", Capability: "test.dynamic.execute", Effect: "deny", Authority: coretool.AuthorityPolicy}}}, {Constraints: []coretool.RoutingConstraint{{ID: "duplicate", Capability: "test.dynamic.execute", Effect: "deny", Authority: coretool.AuthorityPolicy}}}}},
	} {
		if _, _, err := adapter.DynamicCapabilityConstraints(request); err == nil {
			t.Fatalf("unsafe policy adapter was accepted: %#v", adapter)
		}
	}
}

func TestCoreDynamicSemanticSurfaceRefreshesAfterUnknownExecution(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	routing := DynamicSemanticRouting{
		Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(),
		RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute,
		Resolver: dynamicNeedResolverFunc(func(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
			return DynamicCapabilityNeedResolution{Managed: true, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}}, nil
		}),
	}
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{{ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: testDynamicCapabilityContract()}}, err: context.DeadlineExceeded}
	callback := &coreAgentCallbacks{ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "lookup", loopID: "session", dynamicOperationScope: "root", mcpProvider: provider, dynamicSemanticRouting: &routing}
	defs, managed := callback.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("definitions=%#v managed=%v", defs, managed)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result := callback.ExecuteToolCall(name, `{}`, "call-1")
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "mcp_execution_unknown") {
		t.Fatalf("unknown execution=%+v", result)
	}
	if !callback.RefreshAfterToolExecution(name) {
		t.Fatal("unknown execution did not request a fresh model surface")
	}
	if callback.RefreshAfterToolExecution(name) {
		t.Fatal("refresh signal was not consumable")
	}
	if defs, managed := callback.dynamicSemanticToolDefinitions(); !managed || len(defs) != 0 {
		t.Fatalf("unknown consumed grant was re-rendered defs=%#v managed=%v", defs, managed)
	}
}

func TestCoreDynamicSemanticSurfaceHidesAwaitingReceiptAfterRecovery(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	entry := MCPToolEntry{ServerID: "server", ToolName: "send", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{Capability: "test.dynamic.execute", Quality: 1}},
		Effects:    []coretool.EffectClass{coretool.EffectExternalEffect},
	}}
	catalog, err := BuildDynamicSemanticCatalog([]MCPToolEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish(catalog.Providers, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: snapshot, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	routing := DynamicSemanticRouting{Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute}
	scope := coretool.InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "tenant:user"}
	first, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := first.Definitions()
	if err != nil || len(defs) != 1 {
		t.Fatalf("definitions=%#v err=%v", defs, err)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	grant := first.grants[name]
	if result, _, err := first.executor.Execute(grant, scope, plan, nil, func(coretool.PlannedSelection) coretool.SelectionExecutionResult {
		return coretool.SelectionExecutionResult{AwaitingReceipt: true, Result: "prepared"}
	}); err != nil || !result.AwaitingReceipt {
		t.Fatalf("prepare awaiting receipt result=%#v err=%v", result, err)
	}
	recovered, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	if defs, err := recovered.Definitions(); err != nil || len(defs) != 0 {
		t.Fatalf("awaiting receipt was rematerialized defs=%#v err=%v", defs, err)
	}
	if !recovered.HasKnownGrant(name) || recovered.HasGrant(name) {
		t.Fatalf("recovered grant state exposed=%v known=%v", recovered.HasGrant(name), recovered.HasKnownGrant(name))
	}
}

func TestCoreDynamicSemanticReceiptSettlementAdvancesOnlyMatchingAwaitingSelection(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	entry := MCPToolEntry{ServerID: "server", ToolName: "send", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{Capability: "test.dynamic.execute", Quality: 1}},
		Effects:    []coretool.EffectClass{coretool.EffectExternalEffect},
	}}
	catalog, err := BuildDynamicSemanticCatalog([]MCPToolEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish(catalog.Providers, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: snapshot, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	ledger := NewMemoryDynamicOperationLedger()
	routing := DynamicSemanticRouting{Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, EffectCoordinator: LedgerDynamicExternalEffectCoordinator{Ledger: ledger, ReceiptStore: NewMemoryDynamicEffectReceiptStore()}}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	scope := coretool.InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: memoryOwnerIDForPrincipal(principal)}
	surface, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := surface.Definitions()
	if err != nil || len(defs) != 1 {
		t.Fatalf("definitions=%#v err=%v", defs, err)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result, handled := surface.Execute(context.Background(), principal, &boundMCPProviderStub{entries: []MCPToolEntry{entry}}, nil, name, `{}`, "call-1")
	if !handled || !result.AwaitingReceipt {
		t.Fatalf("prepared result=%#v handled=%v", result, handled)
	}
	selectionID := plan.Selections[0].ID
	operationID := dynamicOperationKey(principal.TenantID, principal.UserID, scope.RootTaskID, "semantic:mcp:need", plan.Selections[0].Provider.StableID(), dynamicOperationDigest(map[string]interface{}{}))
	if err := routing.SettleDynamicSemanticExternalEffect(scope, principal, selectionID, operationID, DynamicEffectReceiptAccepted, "provider_accepted", "provider-receipt"); err != nil {
		t.Fatalf("settle receipt: %v", err)
	}
	if record, err := routing.ExecutionStore.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded {
		t.Fatalf("settled execution=%#v err=%v", record, err)
	}
	if completed, err := routing.RouteState.CompletedSelections(scope); err != nil || !completed[selectionID] {
		t.Fatalf("settled route completion=%#v err=%v", completed, err)
	}
	if err := routing.SettleDynamicSemanticExternalEffect(scope, Principal{TenantID: "tenant", UserID: "other"}, selectionID, operationID, DynamicEffectReceiptAccepted, "provider_accepted", "provider-receipt"); err == nil {
		t.Fatal("cross-principal settlement succeeded")
	}
}

func TestCoreDynamicSemanticUnifiedReceiptSettlementDoesNotReplayLegacyProjection(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	issuer, err := coretool.NewInvocationIssuerWithStore(make([]byte, 32), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	entry := MCPToolEntry{ServerID: "server", ToolName: "send", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{Capability: "test.dynamic.execute", Quality: 1}},
		Effects:    []coretool.EffectClass{coretool.EffectExternalEffect},
	}}
	catalog, err := BuildDynamicSemanticCatalog([]MCPToolEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish(catalog.Providers, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{RootTaskID: "root-unified", SessionID: "session-unified", TurnID: "turn-unified", Snapshot: snapshot, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	scope := coretool.InvocationScope{RootTaskID: "root-unified", PlanID: plan.ID, SessionID: "session-unified", TurnID: "turn-unified", PrincipalID: memoryOwnerIDForPrincipal(principal)}
	routing := DynamicSemanticRouting{
		Registry: registry, Issuer: issuer, ExecutionStore: coordinator.Executions, RouteState: coordinator.Routes, HostCalls: coordinator.HostCalls,
		Coordinator: coordinator, GrantTTL: time.Minute, EffectCoordinator: LedgerDynamicExternalEffectCoordinator{SemanticCoordinator: coordinator},
	}
	surface, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := surface.Definitions()
	if err != nil || len(defs) != 1 {
		t.Fatalf("definitions=%#v err=%v", defs, err)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{entry}}
	result, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-unified")
	if !handled || !result.AwaitingReceipt || provider.boundCalls != 1 {
		t.Fatalf("prepared result=%#v handled=%v calls=%d", result, handled, provider.boundCalls)
	}
	selectionID := plan.Selections[0].ID
	operationID := dynamicOperationKey(principal.TenantID, principal.UserID, scope.RootTaskID, "semantic:mcp:need", plan.Selections[0].Provider.StableID(), dynamicOperationDigest(map[string]interface{}{}))
	if err := routing.SettleDynamicSemanticExternalEffect(scope, principal, selectionID, operationID, DynamicEffectReceiptAccepted, "provider_accepted", "provider-receipt"); err != nil {
		t.Fatalf("settle unified receipt: %v", err)
	}
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded {
		t.Fatalf("settled execution=%#v err=%v", record, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || !completed[selectionID] {
		t.Fatalf("settled route completion=%#v err=%v", completed, err)
	}
	// The same receipt is an idempotent reconciliation observation. In
	// particular, it must not re-enter the legacy execution/route stores.
	if err := routing.SettleDynamicSemanticExternalEffect(scope, principal, selectionID, operationID, DynamicEffectReceiptAccepted, "provider_accepted", "provider-receipt"); err != nil {
		t.Fatalf("idempotent unified receipt: %v", err)
	}
}

func TestCoreDynamicSemanticLateReceiptSettlesUnknownSelection(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	entry := MCPToolEntry{ServerID: "server", ToolName: "send", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{Capability: "test.dynamic.execute", Quality: 1}},
		Effects:    []coretool.EffectClass{coretool.EffectExternalEffect},
	}}
	catalog, err := BuildDynamicSemanticCatalog([]MCPToolEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish(catalog.Providers, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: snapshot, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	ledger := NewMemoryDynamicOperationLedger()
	principal := Principal{TenantID: "tenant", UserID: "user"}
	routing := DynamicSemanticRouting{Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, EffectCoordinator: LedgerDynamicExternalEffectCoordinator{Ledger: ledger, ReceiptStore: NewMemoryDynamicEffectReceiptStore()}}
	scope := coretool.InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: memoryOwnerIDForPrincipal(principal)}
	surface, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := surface.Definitions()
	if err != nil || len(defs) != 1 {
		t.Fatalf("definitions=%#v err=%v", defs, err)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{entry}, err: context.DeadlineExceeded}
	result, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-1")
	if !handled || !result.Unknown || provider.boundCalls != 1 {
		t.Fatalf("unknown execution=%#v handled=%v calls=%d", result, handled, provider.boundCalls)
	}
	selectionID := plan.Selections[0].ID
	operationID := dynamicOperationKey(principal.TenantID, principal.UserID, scope.RootTaskID, "semantic:mcp:need", plan.Selections[0].Provider.StableID(), dynamicOperationDigest(map[string]interface{}{}))
	if err := routing.SettleDynamicSemanticExternalEffect(scope, principal, selectionID, operationID, DynamicEffectReceiptAccepted, "late_remote_receipt", "receipt-after-timeout"); err != nil {
		t.Fatalf("late settlement: %v", err)
	}
	if record, err := routing.ExecutionStore.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded {
		t.Fatalf("settled unknown execution=%#v err=%v", record, err)
	}
	if completed, err := routing.RouteState.CompletedSelections(scope); err != nil || !completed[selectionID] {
		t.Fatalf("late settled route completion=%#v err=%v", completed, err)
	}
}

type dynamicReceiptSourceStub struct {
	bindingID    string
	observations []DynamicEffectReceiptObservation
	calls        int
}

func (s *dynamicReceiptSourceStub) BindingID() string { return s.bindingID }

func (s *dynamicReceiptSourceStub) ObserveDynamicEffectReceipts(_ context.Context, accept func(DynamicEffectReceiptObservation) error) error {
	s.calls++
	for _, observation := range s.observations {
		if err := accept(observation); err != nil {
			return err
		}
	}
	return nil
}

func TestCoreDynamicSemanticReceiptSourceReconcilesDurableOperationWithoutRedispatch(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	entry := MCPToolEntry{ServerID: "server", ToolName: "send", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{Capability: "test.dynamic.execute", Quality: 1}}, Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
	}}
	catalog, err := BuildDynamicSemanticCatalog([]MCPToolEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish(catalog.Providers, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: snapshot, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	ledger := NewMemoryDynamicOperationLedger()
	routing := DynamicSemanticRouting{Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, EffectCoordinator: LedgerDynamicExternalEffectCoordinator{Ledger: ledger, ReceiptStore: NewMemoryDynamicEffectReceiptStore()}}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	scope := coretool.InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: memoryOwnerIDForPrincipal(principal)}
	surface, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := surface.Definitions()
	if err != nil || len(defs) != 1 {
		t.Fatalf("definitions=%#v err=%v", defs, err)
	}
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{entry}}
	// The stub owns its own counter; use it as proof the source never enters
	// the bound dispatch path after preparation.
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-1")
	if !handled || !result.AwaitingReceipt || provider.boundCalls != 1 {
		t.Fatalf("prepared result=%+v handled=%v calls=%d", result, handled, provider.boundCalls)
	}
	operationID := dynamicOperationKey(principal.TenantID, principal.UserID, scope.RootTaskID, "semantic:mcp:need", plan.Selections[0].Provider.StableID(), dynamicOperationDigest(map[string]interface{}{}))
	source := &dynamicReceiptSourceStub{bindingID: plan.Selections[0].Provider.StableID(), observations: []DynamicEffectReceiptObservation{{OperationID: operationID, State: DynamicEffectReceiptAccepted, ReasonCode: "trusted_provider_accepted", Receipt: "receipt-accepted"}}}
	if err := routing.ReconcileDynamicEffectReceiptSource(context.Background(), source); err != nil {
		t.Fatalf("reconcile source: %v", err)
	}
	if source.calls != 1 || provider.boundCalls != 1 {
		t.Fatalf("source=%d provider calls=%d; reconciliation redispatched", source.calls, provider.boundCalls)
	}
	if record, err := routing.ExecutionStore.Execution(scope, plan.Selections[0].ID); err != nil || record.State != coretool.PlanExecutionSucceeded {
		t.Fatalf("settled execution=%#v err=%v", record, err)
	}
	// Duplicate observations with the same trusted receipt are idempotent;
	// conflicting receipt evidence is rejected by the append-once receipt store.
	if err := routing.ReconcileDynamicEffectReceiptSource(context.Background(), source); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	source.observations[0].Receipt = "conflicting-receipt"
	if err := routing.ReconcileDynamicEffectReceiptSource(context.Background(), source); err == nil {
		t.Fatal("conflicting trusted receipt was accepted")
	}
}

func TestCoreDynamicSemanticReceiptSourceRejectsDifferentBinding(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	entry := MCPToolEntry{ServerID: "server", ToolName: "send", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: DynamicCapabilityContract{Provisions: []coretool.CapabilityProvision{{Capability: "test.dynamic.execute", Quality: 1}}, Effects: []coretool.EffectClass{coretool.EffectExternalEffect}}}
	catalog, err := BuildDynamicSemanticCatalog([]MCPToolEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish(catalog.Providers, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: snapshot, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	ledger := NewMemoryDynamicOperationLedger()
	coordinator := LedgerDynamicExternalEffectCoordinator{Ledger: ledger, ReceiptStore: NewMemoryDynamicEffectReceiptStore()}
	routing := DynamicSemanticRouting{Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, EffectCoordinator: coordinator}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	scope := coretool.InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: memoryOwnerIDForPrincipal(principal)}
	surface, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := surface.Definitions()
	if err != nil || len(defs) != 1 {
		t.Fatalf("definitions=%#v err=%v", defs, err)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	prepared, handled := surface.Execute(context.Background(), principal, &boundMCPProviderStub{entries: []MCPToolEntry{entry}}, nil, name, `{}`, "call-1")
	if !handled || !prepared.AwaitingReceipt {
		t.Fatalf("prepared=%+v handled=%v", prepared, handled)
	}
	operationID := dynamicOperationKey(principal.TenantID, principal.UserID, scope.RootTaskID, "semantic:mcp:need", plan.Selections[0].Provider.StableID(), dynamicOperationDigest(map[string]interface{}{}))
	source := &dynamicReceiptSourceStub{bindingID: "different-binding", observations: []DynamicEffectReceiptObservation{{OperationID: operationID, State: DynamicEffectReceiptAccepted, Receipt: "receipt"}}}
	if err := routing.ReconcileDynamicEffectReceiptSource(context.Background(), source); err == nil {
		t.Fatal("different binding source was accepted")
	}
}

func TestCoreDynamicSemanticSurfaceRecoversMaterializationAndHostCallReplay(t *testing.T) {
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	entry := MCPToolEntry{ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}, "required": []string{"q"}, "additionalProperties": false,
	}, Contract: testDynamicCapabilityContract()}
	catalog, err := BuildDynamicSemanticCatalog([]MCPToolEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish(catalog.Providers, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	routing := DynamicSemanticRouting{
		Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute,
	}
	scope := coretool.InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "tenant:user"}
	first, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := first.Definitions()
	if err != nil || len(defs) != 1 {
		t.Fatalf("first definitions=%#v err=%v", defs, err)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	second, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	defs, err = second.Definitions()
	if err != nil || len(defs) != 1 || defs[0]["function"].(map[string]interface{})["name"] != name {
		t.Fatalf("recovered definitions=%#v err=%v want=%q", defs, err, name)
	}
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{entry}}
	result, handled := second.Execute(context.Background(), Principal{TenantID: "tenant", UserID: "user"}, provider, nil, name, `{"q":"weekly"}`, "call-1")
	if !handled || !result.Succeeded || result.Result != "bound" || provider.boundCalls != 1 {
		t.Fatalf("first execute=%+v handled=%v bound=%d", result, handled, provider.boundCalls)
	}
	third, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	replay, handled := third.Execute(context.Background(), Principal{TenantID: "tenant", UserID: "user"}, provider, nil, name, `{"q":"weekly"}`, "call-1")
	if !handled || !replay.Succeeded || replay.Result != "bound" || provider.boundCalls != 1 {
		t.Fatalf("replayed execute=%+v handled=%v bound=%d", replay, handled, provider.boundCalls)
	}
}

func TestDynamicSemanticRoutingResourcesPersistSigningKey(t *testing.T) {
	root := t.TempDir()
	first, err := OpenDynamicSemanticRoutingResources(root)
	if err != nil {
		t.Fatal(err)
	}
	key := append([]byte(nil), first.key...)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := OpenDynamicSemanticRoutingResources(root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if string(second.key) != string(key) {
		t.Fatal("dynamic semantic signing key changed across restart")
	}
}

func TestDynamicSemanticRoutingResourcesRecoverRouteAndHostCallAcrossRestart(t *testing.T) {
	root := t.TempDir()
	registry := dynamicSemanticRegistry(t)
	entry := MCPToolEntry{ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}, "required": []string{"q"}, "additionalProperties": false,
	}, Contract: testDynamicCapabilityContract()}
	catalog, err := BuildDynamicSemanticCatalog([]MCPToolEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish(catalog.Providers, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstResources, err := OpenDynamicSemanticRoutingResources(root)
	if err != nil {
		t.Fatal(err)
	}
	firstRouting, err := firstResources.Routing(registry, dynamicNeedResolverFunc(func(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
		return DynamicCapabilityNeedResolution{}, nil
	}), nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	scope := coretool.InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "tenant:user"}
	first, err := newCoreDynamicSemanticSurfaceForTenant(firstRouting, catalog, plan, scope, "tenant")
	if err != nil {
		t.Fatal(err)
	}
	defs, err := first.Definitions()
	if err != nil || len(defs) != 1 {
		t.Fatalf("first definitions=%#v err=%v", defs, err)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{entry}}
	if result, handled := first.Execute(context.Background(), Principal{TenantID: "tenant", UserID: "user"}, provider, nil, name, `{"q":"weekly"}`, "call-1"); !handled || !result.Succeeded || result.Result != "bound" || provider.boundCalls != 1 {
		t.Fatalf("first execute=%+v handled=%v bound=%d", result, handled, provider.boundCalls)
	}
	if err := firstResources.Close(); err != nil {
		t.Fatal(err)
	}
	secondResources, err := OpenDynamicSemanticRoutingResources(root)
	if err != nil {
		t.Fatal(err)
	}
	defer secondResources.Close()
	secondRouting, err := secondResources.Routing(registry, dynamicNeedResolverFunc(func(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
		return DynamicCapabilityNeedResolution{}, nil
	}), nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := newCoreDynamicSemanticSurfaceForTenant(secondRouting, catalog, plan, scope, "tenant")
	if err != nil {
		t.Fatal(err)
	}
	result, handled := recovered.Execute(context.Background(), Principal{TenantID: "tenant", UserID: "user"}, provider, nil, name, `{"q":"weekly"}`, "call-1")
	if !handled || !result.Succeeded || result.Result != "bound" || provider.boundCalls != 1 {
		t.Fatalf("recovered replay=%+v handled=%v bound=%d", result, handled, provider.boundCalls)
	}
}

func TestServiceConfigureDynamicSemanticRoutingOwnsDurableResources(t *testing.T) {
	executor := &CoreAgentExecutor{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test"}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatal(err)
	}
	registry := dynamicSemanticRegistry(t)
	err = svc.ConfigureDynamicSemanticRouting(registry, dynamicNeedResolverFunc(func(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
		return DynamicCapabilityNeedResolution{}, nil
	}), nil, time.Minute)
	if err != nil {
		t.Fatalf("ConfigureDynamicSemanticRouting: %v", err)
	}
	if executor.getDynamicSemanticRouting() == nil || svc.dynamicSemantic == nil {
		t.Fatal("service did not install semantic routing resources")
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
}
