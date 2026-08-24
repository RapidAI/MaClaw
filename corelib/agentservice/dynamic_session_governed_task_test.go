package agentservice

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/task"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type scriptedIntentClassificationSource struct {
	byText map[string]intent.ClassificationResult
}

func (s scriptedIntentClassificationSource) Classify(ctx intent.MessageContext) intent.ClassificationResult {
	if result, ok := s.byText[ctx.Text]; ok {
		return result
	}
	return intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.9, Layer: 3}
}

type fixedPrincipalIntentClassificationSource struct {
	result intent.ClassificationResult
}

func (s fixedPrincipalIntentClassificationSource) ClassifyDynamicIntent(context.Context, Principal, string) (intent.ClassificationResult, error) {
	return s.result, nil
}

func testInformationLookupContract(scope string) DynamicCapabilityContract {
	return DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{
			Capability: CapabilityInformationLookup,
			Qualifiers: map[string]string{QualifierInformationScope: scope},
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
	}
}

func testSessionGovernedLookupResolver(t *testing.T, store *SessionGovernedTaskStore, classifier IntentClassificationSource) *IntentLabelCapabilityNeedResolver {
	t.Helper()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	return &IntentLabelCapabilityNeedResolver{
		Classifier: classifier, Registry: registry,
		Rules: ReviewedDynamicIntentCapabilityNeedRules(), SessionGoverned: store,
	}
}

func testSessionGovernedMutationResolver(t *testing.T, store *SessionGovernedTaskStore, classifier IntentClassificationSource) *IntentLabelCapabilityNeedResolver {
	t.Helper()
	return &IntentLabelCapabilityNeedResolver{
		Classifier: classifier, Registry: dynamicSemanticRegistry(t),
		Rules: map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
			intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
		},
		SessionGoverned: store,
	}
}

func TestSessionGovernedLookupIsReadOnlyAndDoesNotReplay(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if sessionGovernedNeedHasSideEffect(registry, coretool.CapabilityNeed{Capability: CapabilityInformationLookup}) {
		t.Fatal("information.lookup must settle as read-only, not as a mutation")
	}
	request := DynamicCapabilityNeedRequest{
		Principal: Principal{TenantID: "tenant", UserID: "user"}, ChannelScope: "core-agent",
	}
	store.PersistGrantedPlan(request, registry, coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
		ID: "sel-lookup", NeedID: "need:lookup",
		FitProof: coretool.FitProof{MatchedCapability: CapabilityInformationLookup, QualifierBindings: map[string]string{QualifierInformationScope: InformationScopeReference}},
	}}})
	task, ok := store.Load(request)
	if !ok || task.Status != sessionGovernedSucceeded || len(task.Needs) != 1 || task.Needs[0].Capability != CapabilityInformationLookup {
		t.Fatalf("lookup task=%#v ok=%v", task, ok)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"continue": {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{
		Principal: request.Principal, UserText: "continue", ChannelScope: "core-agent",
	})
	if err != nil || resolution.Managed || len(resolution.Needs) != 0 {
		t.Fatalf("succeeded lookup must not replay as a mutation, resolution=%#v err=%v", resolution, err)
	}
}

func TestSessionGovernedContinuationDoesNotInventGenerateFromLookup(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	request := DynamicCapabilityNeedRequest{Principal: Principal{TenantID: "tenant", UserID: "user"}, ChannelScope: "core-agent"}
	store.PersistGrantedPlan(request, registry, coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
		NeedID: "need:lookup", FitProof: coretool.FitProof{MatchedCapability: CapabilityInformationLookup},
	}}})
	resolution, ok := store.ReplayContinuation(request, ReviewedDynamicIntentCapabilityNeedRules(), registry, intent.ClassificationResult{
		Primary: intent.LabelContinuation, Confidence: 0.9,
	})
	if ok || resolution.Managed {
		t.Fatalf("continuation after lookup must not invent generate or stay managed, resolution=%#v ok=%v", resolution, ok)
	}
}

func TestSessionGovernedContinuationReplaysGrantedMutationNeeds(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	resolver := testSessionGovernedMutationResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"search reports": {Primary: intent.LabelSearch, Confidence: 0.95, Layer: 3},
		"continue":       {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	request := DynamicCapabilityNeedRequest{
		Principal: Principal{TenantID: "tenant", UserID: "user"}, UserText: "search reports",
		ChannelScope: "core-agent", RootTaskID: "root-1",
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), request)
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 || resolution.Needs[0].Capability != "test.dynamic.execute" {
		t.Fatalf("search resolution=%#v err=%v", resolution, err)
	}
	store.PersistGrantedPlan(request, resolver.Registry, coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
		ID: "sel-1", NeedID: resolution.Needs[0].ID,
		FitProof: coretool.FitProof{MatchedCapability: "test.dynamic.execute"},
	}}})
	continued, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{
		Principal: request.Principal, UserText: "continue", ChannelScope: "core-agent", RootTaskID: "root-2",
	})
	if err != nil || !continued.Managed || len(continued.Needs) != 1 {
		t.Fatalf("pending mutation must replay, resolution=%#v err=%v", continued, err)
	}
	if continued.Needs[0].Capability != "test.dynamic.execute" {
		t.Fatalf("replay invented a capability: %#v", continued.Needs[0])
	}
}

func TestSessionGovernedCoordinatorReadsFencedContinuityProjection(t *testing.T) {
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	registry := dynamicSemanticRegistry(t)
	contract := testDynamicCapabilityContract()
	contract.Effects = []coretool.EffectClass{coretool.EffectExternalEffect}
	provider, _, _, err := ProjectMCPDynamicProvider(MCPToolEntry{ServerID: "continuity", ToolName: "write", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: contract})
	if err != nil {
		t.Fatal(err)
	}
	catalog := coretool.NewToolCatalog(registry)
	snapshot, err := catalog.Publish([]coretool.ProviderSpec{provider}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "governed-root", SessionID: "session", TurnID: "turn",
		Snapshot: snapshot,
		Needs:    []coretool.CapabilityNeed{{ID: "write", Capability: "test.dynamic.execute", Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := coretool.InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "tenant:user"}
	issuer, err := coretool.NewInvocationIssuerWithStore([]byte(strings.Repeat("c", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.PublishSurface(coretool.SurfacePublishRequest{Revision: coretool.RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	store := NewSessionGovernedTaskStore()
	store.BindCoordinator(coordinator, "")
	request := DynamicCapabilityNeedRequest{Principal: Principal{TenantID: "tenant", UserID: "user"}, RootTaskID: plan.RootTaskID, SessionID: "session"}
	task, ok := store.Load(request)
	if !ok || task.Status != sessionGovernedPending || len(task.Needs) != 1 || task.Needs[0].Capability != "test.dynamic.execute" {
		t.Fatalf("durable continuity task=%#v ok=%v", task, ok)
	}
}

func TestSessionGovernedCoordinatorRejectsMutationContinuationWithoutVerifiedTaskHandle(t *testing.T) {
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	store := NewSessionGovernedTaskStore()
	store.BindCoordinator(coordinator, "tenant")
	registry := dynamicSemanticRegistry(t)
	contract := testDynamicCapabilityContract()
	provider, _, _, err := ProjectMCPDynamicProvider(MCPToolEntry{ServerID: "continuity", ToolName: "write", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: contract})
	if err != nil {
		t.Fatal(err)
	}
	catalog := coretool.NewToolCatalog(registry)
	snapshot, err := catalog.Publish([]coretool.ProviderSpec{provider}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	request := DynamicCapabilityNeedRequest{
		Principal: Principal{TenantID: "tenant", UserID: "user"}, RootTaskID: "root", SessionID: "session", ChannelScope: "core-agent",
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{RootTaskID: request.RootTaskID, SessionID: request.SessionID, TurnID: "turn", Snapshot: snapshot, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuerWithStore([]byte(strings.Repeat("h", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	scope := coretool.InvocationScope{RootTaskID: request.RootTaskID, PlanID: plan.ID, SessionID: request.SessionID, TurnID: "turn", PrincipalID: memoryOwnerIDForPrincipal(request.Principal)}
	if _, _, err := coordinator.PublishSurface(coretool.SurfacePublishRequest{Revision: coretool.RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, TenantID: request.Principal.TenantID, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.DrainContinuityProjections("tenant", 10, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	continuation := intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.95}
	if replayed, ok := store.ReplayContinuation(request, map[intent.IntentLabel][]IntentCapabilityNeedTemplate{intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}}}, registry, continuation); ok || replayed.Managed {
		t.Fatalf("unverified continuation replayed mutation: %#v", replayed)
	}
	request.TaskRelation = TaskRelationDecision{
		Kind:               TaskRelationContinue,
		RootTaskID:         request.RootTaskID,
		ContinuationHandle: "host-signed-handle",
		TenantID:           request.Principal.TenantID,
		PrincipalID:        memoryOwnerIDForPrincipal(request.Principal),
		SessionID:          request.SessionID,
	}
	if replayed, ok := store.ReplayContinuation(request, map[intent.IntentLabel][]IntentCapabilityNeedTemplate{intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}}}, registry, continuation); ok || replayed.Managed {
		t.Fatalf("raw continuation handle replayed mutation: %#v", replayed)
	}
	request.TaskRelation = verifiedTaskRelationDecision(request.TaskRelation, request.Principal, request.SessionID)
	if replayed, ok := store.ReplayContinuation(request, map[intent.IntentLabel][]IntentCapabilityNeedTemplate{intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}}}, registry, continuation); !ok || !replayed.Managed || len(replayed.Needs) != 1 {
		t.Fatalf("verified continuation did not replay projection facts: %#v ok=%v", replayed, ok)
	}
}

func TestSessionGovernedSucceededMutationDoesNotReplay(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry := dynamicSemanticRegistry(t)
	request := DynamicCapabilityNeedRequest{Principal: Principal{TenantID: "tenant", UserID: "user"}, ChannelScope: "core-agent"}
	store.PersistGrantedPlan(request, registry, coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
		NeedID: "need:exec", FitProof: coretool.FitProof{MatchedCapability: "test.dynamic.execute"},
	}}})
	store.Mark(request, sessionGovernedSucceeded)
	resolution, ok := store.ReplayContinuation(request, map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
		intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
	}, registry, intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9})
	if ok || resolution.Managed {
		t.Fatalf("succeeded mutation must not replay, resolution=%#v ok=%v", resolution, ok)
	}
}

func TestSessionGovernedFailedExecCanReplaySameNeeds(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry := dynamicSemanticRegistry(t)
	request := DynamicCapabilityNeedRequest{Principal: Principal{TenantID: "tenant", UserID: "user"}, ChannelScope: "core-agent"}
	store.PersistGrantedPlan(request, registry, coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
		NeedID: "need:exec", FitProof: coretool.FitProof{MatchedCapability: "test.dynamic.execute"},
	}}})
	store.Mark(request, sessionGovernedFailedExec)
	resolution, ok := store.ReplayContinuation(request, map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
		intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
	}, registry, intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9})
	if !ok || !resolution.Managed || len(resolution.Needs) != 1 || resolution.Needs[0].Capability != "test.dynamic.execute" {
		t.Fatalf("failed_exec mutation must replay, resolution=%#v ok=%v", resolution, ok)
	}
}

func TestSessionGovernedContinuationWithoutPriorTaskIsUnmanaged(t *testing.T) {
	resolver := testSessionGovernedLookupResolver(t, NewSessionGovernedTaskStore(), scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"continue": {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{
		Principal: Principal{TenantID: "tenant", UserID: "user"}, UserText: "continue", ChannelScope: "core-agent",
	})
	if err != nil || resolution.Managed || len(resolution.Needs) != 0 {
		t.Fatalf("continue without a granted task must stay unmanaged, resolution=%#v err=%v", resolution, err)
	}
}

func TestSessionGovernedTaskIsolatesPrincipalsAndDestinations(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry := dynamicSemanticRegistry(t)
	rules := map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
		intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
	}
	owner := DynamicCapabilityNeedRequest{
		Principal: Principal{TenantID: "tenant", UserID: "alice"}, ChannelScope: "core-agent", DestinationID: "group:g1",
	}
	store.PersistGrantedPlan(owner, registry, coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
		NeedID: "need:exec", FitProof: coretool.FitProof{MatchedCapability: "test.dynamic.execute"},
	}}})
	otherUser := owner
	otherUser.Principal.UserID = "bob"
	if _, ok := store.Load(otherUser); ok {
		t.Fatal("granted needs must not leak across users")
	}
	otherDest := owner
	otherDest.DestinationID = "group:g2"
	if _, ok := store.Load(otherDest); ok {
		t.Fatal("granted needs must not leak across destinations")
	}
	continuation := intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}
	if _, ok := store.ReplayContinuation(otherUser, rules, registry, continuation); ok {
		t.Fatal("other user must not replay alice's task")
	}
	if _, ok := store.ReplayContinuation(otherDest, rules, registry, continuation); ok {
		t.Fatal("other destination must not replay group:g1")
	}
	replayed, ok := store.ReplayContinuation(owner, rules, registry, continuation)
	if !ok || !replayed.Managed {
		t.Fatalf("owner destination must replay, resolution=%#v ok=%v", replayed, ok)
	}
}

func TestSessionGovernedUncoveredNeedDoesNotReplay(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry := dynamicSemanticRegistry(t)
	request := DynamicCapabilityNeedRequest{Principal: Principal{TenantID: "tenant", UserID: "user"}, ChannelScope: "core-agent"}
	store.PersistGrantedPlan(request, registry, coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
		NeedID: "need:exec", FitProof: coretool.FitProof{MatchedCapability: "test.dynamic.execute"},
	}}})
	resolution, ok := store.ReplayContinuation(request, map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
		intent.LabelSearch: {{Capability: CapabilityInformationLookup, Required: true}},
	}, registry, intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9})
	if ok || resolution.Managed {
		t.Fatalf("unpublished capability must not replay, resolution=%#v ok=%v", resolution, ok)
	}
}

func TestSessionGovernedCodingTurnDoesNotPersist(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"write a function": {Primary: intent.LabelCoding, Confidence: 0.95, Layer: 3},
	}})
	request := DynamicCapabilityNeedRequest{
		Principal: Principal{TenantID: "tenant", UserID: "user"}, UserText: "write a function", ChannelScope: "core-agent",
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), request)
	if err != nil || resolution.Managed {
		t.Fatalf("coding-only must stay unmanaged, resolution=%#v err=%v", resolution, err)
	}
	if _, ok := store.Load(request); ok {
		t.Fatal("unmanaged coding turn must not write SessionGovernedTask")
	}
}

func TestPrincipalResolverReplaysThroughSharedStore(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry := dynamicSemanticRegistry(t)
	resolver := &PrincipalIntentLabelCapabilityNeedResolver{
		Classifier: fixedPrincipalIntentClassificationSource{result: intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3}},
		Registry:   registry,
		Rules: map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
			intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
		},
		SessionGoverned: store,
	}
	request := DynamicCapabilityNeedRequest{Principal: Principal{TenantID: "tenant", UserID: "user"}, ChannelScope: "core-agent"}
	store.PersistGrantedPlan(request, registry, coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
		NeedID: "need:exec", FitProof: coretool.FitProof{MatchedCapability: "test.dynamic.execute"},
	}}})
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), request)
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 || resolution.Needs[0].Capability != "test.dynamic.execute" {
		t.Fatalf("principal resolver must replay granted needs, resolution=%#v err=%v", resolution, err)
	}
}

func TestCoreDynamicSemanticContinuationReplaysOnNewRootTask(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	classifier := scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"search reports": {Primary: intent.LabelSearch, Confidence: 0.95, Layer: 3},
		"continue":       {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}}
	resolver := testSessionGovernedMutationResolver(t, store, classifier)
	routing := DynamicSemanticRouting{
		Registry: resolver.Registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{{
		ServerID: "approved", ToolName: "report",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}, "additionalProperties": false},
		Contract:    testDynamicCapabilityContract(),
	}}}
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "search reports",
		loopID: "session-1", dynamicOperationScope: "root-1", mcpProvider: provider, dynamicSemanticRouting: &routing,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("first turn defs=%#v managed=%v", defs, managed)
	}
	task, ok := store.Load(DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"})
	if !ok || task.Status != sessionGovernedPending || len(task.Needs) != 1 || task.Needs[0].Capability != "test.dynamic.execute" {
		t.Fatalf("first turn must persist granted mutation needs, task=%#v ok=%v", task, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-2", dynamicOperationScope: "root-2", mcpProvider: provider, dynamicSemanticRouting: &routing,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if !contManaged || len(contDefs) != 1 {
		t.Fatalf("continuation must stay managed, defs=%#v managed=%v", contDefs, contManaged)
	}
	if continued.dynamicSemanticSurface == nil || continued.dynamicSemanticSurface.replan == nil || continued.dynamicSemanticSurface.replan.RootTaskID != "root-2" {
		t.Fatalf("continuation must allocate a new RootTaskID, surface=%#v", continued.dynamicSemanticSurface)
	}
}

func TestCoreDynamicSemanticLookupContinuationStaysUnmanaged(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"find reports": {Primary: intent.LabelSearch, Confidence: 0.95, Layer: 3},
		"continue":     {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{{
		ServerID: "approved", ToolName: "lookup",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}, "additionalProperties": false},
		Contract:    testInformationLookupContract(InformationScopeReference),
	}}}
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "find reports",
		loopID: "session-1", dynamicOperationScope: "root-1", mcpProvider: provider, dynamicSemanticRouting: &routing,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("lookup turn defs=%#v managed=%v", defs, managed)
	}
	task, ok := store.Load(DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"})
	if !ok || task.Status != sessionGovernedSucceeded || task.Needs[0].Capability != CapabilityInformationLookup {
		t.Fatalf("lookup must persist as succeeded read-only, task=%#v ok=%v", task, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-2", dynamicOperationScope: "root-2", mcpProvider: provider, dynamicSemanticRouting: &routing,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded lookup must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticCurrentTimeUsesHostClockAndDoesNotReplay(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"what time is it": {Primary: intent.LabelCurrentTime, Confidence: 0.95, Layer: 3},
		"continue":        {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "what time is it",
		loopID: "session-clock", dynamicOperationScope: "root-clock", dynamicSemanticRouting: &routing,
		attachments: []agent.MessageAttachment{{
			FileName: "clip.wav", MimeType: "audio/wav", Data: "not-base64",
		}},
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("current_time turn defs=%#v managed=%v", defs, managed)
	}
	task, ok := store.Load(DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"})
	if !ok || task.Status != sessionGovernedSucceeded || task.Needs[0].Capability != CapabilityCurrentTime {
		t.Fatalf("current_time must persist as succeeded read-only, task=%#v ok=%v", task, ok)
	}
	if task.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("current_time must not persist as information.lookup")
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-clock-2", dynamicOperationScope: "root-clock-2", dynamicSemanticRouting: &routing,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded current_time must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticKnowledgeReadUsesHostStoreAndDoesNotReplay(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"search my knowledge base for this topic": {Primary: intent.LabelKnowledgeRead, Confidence: 0.95, Layer: 3},
		"continue": {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "search my knowledge base for this topic",
		loopID:   "session-kb", dynamicOperationScope: "root-kb", dynamicSemanticRouting: &routing,
		knowledgeStore: noOpKnowledgeStore{},
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("knowledge_read turn defs=%#v managed=%v", defs, managed)
	}
	task, ok := store.Load(DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"})
	if !ok || task.Status != sessionGovernedSucceeded || task.Needs[0].Capability != CapabilityKnowledgeRead {
		t.Fatalf("knowledge_read must persist as succeeded read-only, task=%#v ok=%v", task, ok)
	}
	if task.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("knowledge_read must not persist as information.lookup")
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-kb-2", dynamicOperationScope: "root-kb-2", dynamicSemanticRouting: &routing,
		knowledgeStore: noOpKnowledgeStore{},
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded knowledge_read must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticAuditReadUsesHostReaderAndDoesNotReplay(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"show the recent security audit log": {Primary: intent.LabelAuditRead, Confidence: 0.95, Layer: 3},
		"continue":                           {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	reader := &fakeHostAuditReader{result: "audit events (1):\n- 2026-08-16T13:00:00Z action=message.posted"}
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "show the recent security audit log",
		loopID:   "session-audit", dynamicOperationScope: "root-audit", dynamicSemanticRouting: &routing,
		auditReader: reader,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("audit_read turn defs=%#v managed=%v", defs, managed)
	}
	task, ok := store.Load(DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"})
	if !ok || task.Status != sessionGovernedSucceeded || task.Needs[0].Capability != CapabilityAuditRead {
		t.Fatalf("audit_read must persist as succeeded read-only, task=%#v ok=%v", task, ok)
	}
	if task.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("audit_read must not persist as information.lookup")
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-audit-2", dynamicOperationScope: "root-audit-2", dynamicSemanticRouting: &routing,
		auditReader: reader,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded audit_read must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticWebFetchUsesHostFetcherAndDoesNotReplay(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"fetch the content of this URL": {Primary: intent.LabelWebFetch, Confidence: 0.95, Layer: 3},
		"continue":                      {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "fetch the content of this URL",
		loopID:   "session-fetch", dynamicOperationScope: "root-fetch", dynamicSemanticRouting: &routing,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("web_fetch turn defs=%#v managed=%v", defs, managed)
	}
	task, ok := store.Load(DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"})
	if !ok || task.Status != sessionGovernedSucceeded || task.Needs[0].Capability != CapabilityWebFetch {
		t.Fatalf("web_fetch must persist as succeeded read-only, task=%#v ok=%v", task, ok)
	}
	if task.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("web_fetch must not persist as information.lookup")
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-fetch-2", dynamicOperationScope: "root-fetch-2", dynamicSemanticRouting: &routing,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded web_fetch must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticFileReadUsesHostReaderAndDoesNotReplay(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"show me what is in the README file": {Primary: intent.LabelFileRead, Confidence: 0.95, Layer: 3},
		"continue":                           {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "show me what is in the README file", workspace: t.TempDir(),
		loopID: "session-file", dynamicOperationScope: "root-file", dynamicSemanticRouting: &routing,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("file_read turn defs=%#v managed=%v", defs, managed)
	}
	task, ok := store.Load(DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"})
	if !ok || task.Status != sessionGovernedSucceeded || task.Needs[0].Capability != CapabilityFileRead {
		t.Fatalf("file_read must persist as succeeded read-only, task=%#v ok=%v", task, ok)
	}
	if task.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("file_read must not persist as information.lookup")
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		workspace: t.TempDir(), loopID: "session-file-2", dynamicOperationScope: "root-file-2", dynamicSemanticRouting: &routing,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded file_read must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticGitInspectUsesHostInspectorAndDoesNotReplay(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"show me the current diff": {Primary: intent.LabelGitInspect, Confidence: 0.95, Layer: 3},
		"continue":                 {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "show me the current diff", workspace: t.TempDir(),
		loopID: "session-git", dynamicOperationScope: "root-git", dynamicSemanticRouting: &routing,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("git_inspect turn defs=%#v managed=%v", defs, managed)
	}
	task, ok := store.Load(DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"})
	if !ok || task.Status != sessionGovernedSucceeded || task.Needs[0].Capability != CapabilityRepoInspect {
		t.Fatalf("git_inspect must persist as succeeded read-only, task=%#v ok=%v", task, ok)
	}
	if task.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("git_inspect must not persist as information.lookup")
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		workspace: t.TempDir(), loopID: "session-git-2", dynamicOperationScope: "root-git-2", dynamicSemanticRouting: &routing,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded git_inspect must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticDocumentReadUsesHostReaderAndDoesNotReplay(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"read the attached document": {Primary: intent.LabelDocumentRead, Confidence: 0.95, Layer: 3},
		"continue":                   {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	missing := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "read the attached document", loopID: "session-doc-missing",
		dynamicOperationScope: "root-doc-missing", dynamicSemanticRouting: &routing,
	}
	missingDefs, missingManaged := missing.dynamicSemanticToolDefinitions()
	if !missingManaged || len(missingDefs) != 0 {
		t.Fatalf("document_read without attachment must fail closed, defs=%#v managed=%v", missingDefs, missingManaged)
	}
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "read the attached document", loopID: "session-doc", dynamicOperationScope: "root-doc",
		dynamicSemanticRouting: &routing,
		attachments: []agent.MessageAttachment{{
			FileName: "notes.txt", MimeType: "text/plain",
			Data: base64.StdEncoding.EncodeToString([]byte("hello trusted document")),
		}},
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("document_read turn defs=%#v managed=%v", defs, managed)
	}
	task, ok := store.Load(DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"})
	if !ok || task.Status != sessionGovernedSucceeded || task.Needs[0].Capability != CapabilityDocumentRead {
		t.Fatalf("document_read must persist as succeeded read-only, task=%#v ok=%v", task, ok)
	}
	if task.Needs[0].Capability == CapabilityFileRead {
		t.Fatal("document_read must not persist as fs.read.local")
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-doc-2", dynamicOperationScope: "root-doc-2", dynamicSemanticRouting: &routing,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded document_read must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticAudioTranscribeUsesHostAttachmentAndDoesNotReplay(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"transcribe this recording": {Primary: intent.LabelAudioTranscribe, Confidence: 0.95, Layer: 3},
		"continue":                  {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	engine := &fakeSpeechTranscriber{result: "recognized speech"}
	missing := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "transcribe this recording", loopID: "session-asr-missing",
		dynamicOperationScope: "root-asr-missing", dynamicSemanticRouting: &routing,
		speechTranscriber: engine,
	}
	missingDefs, missingManaged := missing.dynamicSemanticToolDefinitions()
	if !missingManaged || len(missingDefs) != 0 {
		t.Fatalf("audio_transcribe without attachment must fail closed, defs=%#v managed=%v", missingDefs, missingManaged)
	}
	noEngine := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "transcribe this recording", loopID: "session-asr-no-engine",
		dynamicOperationScope: "root-asr-no-engine", dynamicSemanticRouting: &routing,
		attachments: []agent.MessageAttachment{{
			FileName: "clip.wav", MimeType: "audio/wav",
			Data: base64.StdEncoding.EncodeToString([]byte("RIFF trusted-audio")),
		}},
	}
	noEngineDefs, noEngineManaged := noEngine.dynamicSemanticToolDefinitions()
	if !noEngineManaged || len(noEngineDefs) != 0 {
		t.Fatalf("audio_transcribe without a speech engine must fail closed, defs=%#v managed=%v", noEngineDefs, noEngineManaged)
	}
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "transcribe this recording", loopID: "session-asr", dynamicOperationScope: "root-asr",
		dynamicSemanticRouting: &routing, speechTranscriber: engine,
		attachments: []agent.MessageAttachment{{
			FileName: "clip.wav", MimeType: "audio/wav",
			Data: base64.StdEncoding.EncodeToString([]byte("RIFF trusted-audio")),
		}},
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("audio_transcribe turn defs=%#v managed=%v", defs, managed)
	}
	task, ok := store.Load(DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"})
	if !ok || task.Status != sessionGovernedSucceeded || task.Needs[0].Capability != CapabilityAudioTranscribe {
		t.Fatalf("audio_transcribe must persist as succeeded read-only, task=%#v ok=%v", task, ok)
	}
	if task.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("audio_transcribe must not persist as information.lookup")
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-asr-2", dynamicOperationScope: "root-asr-2", dynamicSemanticRouting: &routing,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded audio_transcribe must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestSessionGovernedUnknownMutationDoesNotReplay(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry := dynamicSemanticRegistry(t)
	request := DynamicCapabilityNeedRequest{Principal: Principal{TenantID: "tenant", UserID: "user"}, ChannelScope: "core-agent"}
	store.PersistGrantedPlan(request, registry, coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
		NeedID: "need:exec", FitProof: coretool.FitProof{MatchedCapability: "test.dynamic.execute"},
	}}})
	store.Mark(request, sessionGovernedUnknown)
	resolution, ok := store.ReplayContinuation(request, map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
		intent.LabelSearch: {{Capability: "test.dynamic.execute", Required: true}},
	}, registry, intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9})
	if ok || resolution.Managed {
		t.Fatalf("unknown mutation must not replay, resolution=%#v ok=%v", resolution, ok)
	}
}

func TestCoreDynamicSemanticFileWriteUsesHostWriterAndDoesNotReplayAfterSuccess(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"save this text to a local file": {Primary: intent.LabelFileWrite, Confidence: 0.95, Layer: 3},
		"continue":                       {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "save this text to a local file", workspace: dir,
		loopID: "session-write", dynamicOperationScope: "root-write", dynamicSemanticRouting: &routing,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("file_write turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	task, ok := store.Load(request)
	if !ok || task.Status != sessionGovernedPending || task.Needs[0].Capability != CapabilityFileWrite {
		t.Fatalf("file_write must persist as pending mutation, task=%#v ok=%v", task, ok)
	}
	pending := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		workspace: dir, loopID: "session-write-pending", dynamicOperationScope: "root-write-pending", dynamicSemanticRouting: &routing,
	}
	pendingDefs, pendingManaged := pending.dynamicSemanticToolDefinitions()
	if !pendingManaged || len(pendingDefs) != 1 {
		t.Fatalf("continue before write receipt must replay, defs=%#v managed=%v", pendingDefs, pendingManaged)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result := first.ExecuteToolCall(name, `{"path":"notes.txt","content":"hello write"}`, "call-write")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "notes.txt") || strings.Contains(result.Result, dir) {
		t.Fatalf("file write execution=%+v", result)
	}
	data, err := os.ReadFile(filepath.Join(dir, "notes.txt"))
	if err != nil || string(data) != "hello write" {
		t.Fatalf("workspace file=%q err=%v", data, err)
	}
	task, ok = store.Load(request)
	if !ok || task.Status != sessionGovernedSucceeded {
		t.Fatalf("successful write must mark SessionGoverned succeeded, task=%#v ok=%v", task, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		workspace: dir, loopID: "session-write-2", dynamicOperationScope: "root-write-2", dynamicSemanticRouting: &routing,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded file_write must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticOfficeWriteUsesHostWriterAndDoesNotReplayAfterSuccess(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"写一个表格":    {Primary: intent.LabelOffice, Confidence: 0.95, Layer: 3},
		"continue": {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "写一个表格", workspace: dir,
		loopID: "session-office", dynamicOperationScope: "root-office", dynamicSemanticRouting: &routing,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("office_write turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	task, ok := store.Load(request)
	if !ok || task.Status != sessionGovernedPending || task.Needs[0].Capability != CapabilityOfficeWrite {
		t.Fatalf("office_write must persist as pending mutation, task=%#v ok=%v", task, ok)
	}
	if task.Needs[0].Capability == CapabilityFileWrite {
		t.Fatal("office_write must not persist as fs.write.local")
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	if name == "office" || name == "write_excel" {
		t.Fatalf("leaked soup name %q", name)
	}
	result := first.ExecuteToolCall(name, `{"path":"sheet.xlsx","sheets":[{"name":"S1","rows":[["hello"]]}]}`, "call-office")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "sheet.xlsx") || strings.Contains(result.Result, dir) {
		t.Fatalf("office write execution=%+v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "sheet.xlsx")); err != nil {
		t.Fatalf("workspace spreadsheet missing: %v", err)
	}
	task, ok = store.Load(request)
	if !ok || task.Status != sessionGovernedSucceeded {
		t.Fatalf("successful office write must mark SessionGoverned succeeded, task=%#v ok=%v", task, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		workspace: dir, loopID: "session-office-2", dynamicOperationScope: "root-office-2", dynamicSemanticRouting: &routing,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded office_write must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticShellUsesHostExecutorAndDoesNotReplayAfterSuccess(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"运行 echo hi": {Primary: intent.LabelShellCommand, Confidence: 0.95, Layer: 3},
		"continue":   {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "t", UserID: "u"},
		userText: "运行 echo hi", workspace: dir,
		allowLocalBash: true, localBashTrustedSingleUser: true,
		localBashTenantID: "t", localBashUserID: "u",
		loopID: "session-shell", dynamicOperationScope: "root-shell", dynamicSemanticRouting: &routing,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("shell turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	task, ok := store.Load(request)
	if !ok || task.Status != sessionGovernedPending || task.Needs[0].Capability != CapabilityShellExecute {
		t.Fatalf("shell must persist as pending mutation, task=%#v ok=%v", task, ok)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	if name == "bash" {
		t.Fatalf("leaked soup name %q", name)
	}
	result := first.ExecuteToolCall(name, `{"command":"echo hi"}`, "call-shell")
	if result.Outcome != "ok" || !strings.Contains(strings.ToLower(result.Result), "hi") {
		t.Fatalf("shell execution=%+v", result)
	}
	task, ok = store.Load(request)
	if !ok || task.Status != sessionGovernedSucceeded {
		t.Fatalf("successful shell must mark SessionGoverned succeeded, task=%#v ok=%v", task, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "t", UserID: "u"}, userText: "continue",
		workspace: dir, allowLocalBash: true, localBashTrustedSingleUser: true,
		localBashTenantID: "t", localBashUserID: "u",
		loopID: "session-shell-2", dynamicOperationScope: "root-shell-2", dynamicSemanticRouting: &routing,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded shell must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticDelegateUsesHostRunnerAndDoesNotReplayAfterUnknown(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"交给子代理":    {Primary: intent.LabelDelegateTask, Confidence: 0.95, Layer: 3},
		"continue": {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "t", UserID: "u"},
		userText: "交给子代理", loopID: "session-del", dynamicOperationScope: "root-del",
		dynamicSemanticRouting: &routing,
		delegateSubtask: func(_ context.Context, _ Principal, task string) (string, error) {
			return "child completed: " + task, nil
		},
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("delegate turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	task, ok := store.Load(request)
	if !ok || task.Status != sessionGovernedPending || task.Needs[0].Capability != CapabilityDelegateSubtask {
		t.Fatalf("delegate must persist as pending mutation, task=%#v ok=%v", task, ok)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	if name == "delegate_task" {
		t.Fatalf("leaked soup name %q", name)
	}
	result := first.ExecuteToolCall(name, `{"task":"summarize"}`, "call-del")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "child completed") {
		t.Fatalf("delegate execution=%+v", result)
	}
	task, ok = store.Load(request)
	if !ok || task.Status != sessionGovernedSucceeded {
		t.Fatalf("successful delegate must mark SessionGoverned succeeded, task=%#v ok=%v", task, ok)
	}
	unknownFirst := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "t", UserID: "u2"},
		userText: "交给子代理", loopID: "session-del-u", dynamicOperationScope: "root-del-u",
		dynamicSemanticRouting: &routing,
		delegateSubtask: func(_ context.Context, _ Principal, _ string) (string, error) {
			return "child started", nil
		},
	}
	unknownDefs, unknownManaged := unknownFirst.dynamicSemanticToolDefinitions()
	if !unknownManaged || len(unknownDefs) != 1 {
		t.Fatalf("unknown delegate turn defs=%#v managed=%v", unknownDefs, unknownManaged)
	}
	unknownName := unknownDefs[0]["function"].(map[string]interface{})["name"].(string)
	unknownResult := unknownFirst.ExecuteToolCall(unknownName, `{"task":"summarize"}`, "call-del-u")
	if unknownResult.Outcome == "ok" || !strings.Contains(unknownResult.Result, "host_delegate_started_is_not_complete") {
		t.Fatalf("started must not succeed, result=%+v", unknownResult)
	}
	unknownReq := DynamicCapabilityNeedRequest{Principal: unknownFirst.principal, ChannelScope: "core-agent"}
	unknownTask, ok := store.Load(unknownReq)
	if !ok || unknownTask.Status != sessionGovernedUnknown {
		t.Fatalf("started-not-complete must mark unknown, task=%#v ok=%v", unknownTask, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: unknownFirst.principal, userText: "continue",
		loopID: "session-del-u2", dynamicOperationScope: "root-del-u2", dynamicSemanticRouting: &routing,
		delegateSubtask: unknownFirst.delegateSubtask,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after unknown delegate must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticSSHUsesHostRunnerAndDoesNotReplayAfterUnknown(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"登录服务器查看日志": {Primary: intent.LabelSSH, Confidence: 0.95, Layer: 3},
		"continue":  {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "t", UserID: "u"},
		userText: "登录服务器查看日志", loopID: "session-ssh", dynamicOperationScope: "root-ssh",
		dynamicSemanticRouting: &routing,
		trustedSSH: func(_ context.Context, _ Principal, command string) (string, error) {
			return "remote:" + command, nil
		},
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("ssh turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	task, ok := store.Load(request)
	if !ok || task.Status != sessionGovernedPending || task.Needs[0].Capability != CapabilitySSHExecute {
		t.Fatalf("ssh must persist as pending mutation, task=%#v ok=%v", task, ok)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	if name == "ssh" {
		t.Fatalf("leaked soup name %q", name)
	}
	result := first.ExecuteToolCall(name, `{"command":"uname"}`, "call-ssh")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "remote:uname") {
		t.Fatalf("ssh execution=%+v", result)
	}
	task, ok = store.Load(request)
	if !ok || task.Status != sessionGovernedSucceeded {
		t.Fatalf("successful ssh must mark SessionGoverned succeeded, task=%#v ok=%v", task, ok)
	}
	unknownFirst := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "t", UserID: "u2"},
		userText: "登录服务器查看日志", loopID: "session-ssh-u", dynamicOperationScope: "root-ssh-u",
		dynamicSemanticRouting: &routing,
		trustedSSH: func(_ context.Context, _ Principal, _ string) (string, error) {
			return "", fmt.Errorf("host_ssh_session_disconnected")
		},
	}
	unknownDefs, unknownManaged := unknownFirst.dynamicSemanticToolDefinitions()
	if !unknownManaged || len(unknownDefs) != 1 {
		t.Fatalf("unknown ssh turn defs=%#v managed=%v", unknownDefs, unknownManaged)
	}
	unknownName := unknownDefs[0]["function"].(map[string]interface{})["name"].(string)
	unknownResult := unknownFirst.ExecuteToolCall(unknownName, `{"command":"uname"}`, "call-ssh-u")
	if unknownResult.Outcome == "ok" || !strings.Contains(unknownResult.Result, "host_ssh_session_disconnected") {
		t.Fatalf("disconnect must not succeed, result=%+v", unknownResult)
	}
	unknownReq := DynamicCapabilityNeedRequest{Principal: unknownFirst.principal, ChannelScope: "core-agent"}
	unknownTask, ok := store.Load(unknownReq)
	if !ok || unknownTask.Status != sessionGovernedUnknown {
		t.Fatalf("disconnect must mark unknown, task=%#v ok=%v", unknownTask, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: unknownFirst.principal, userText: "continue",
		loopID: "session-ssh-u2", dynamicOperationScope: "root-ssh-u2", dynamicSemanticRouting: &routing,
		trustedSSH: unknownFirst.trustedSSH,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after unknown ssh must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticKnowledgeWriteUsesHostIngesterAndDoesNotReplayAfterSuccess(t *testing.T) {
	kb := &recordingKnowledgeStore{}
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"save this note into the knowledge base for future retrieval": {Primary: intent.LabelKnowledgeWrite, Confidence: 0.95, Layer: 3},
		"continue": {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "save this note into the knowledge base for future retrieval",
		loopID:   "session-kb-write", dynamicOperationScope: "root-kb-write", dynamicSemanticRouting: &routing,
		knowledgeStore: kb,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("knowledge_write turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	task, ok := store.Load(request)
	if !ok || task.Status != sessionGovernedPending || task.Needs[0].Capability != CapabilityKnowledgeWrite {
		t.Fatalf("knowledge_write must persist as pending mutation, task=%#v ok=%v", task, ok)
	}
	if task.Needs[0].Capability == CapabilityKnowledgeRead || task.Needs[0].Capability == CapabilityFileWrite {
		t.Fatal("knowledge_write must not persist as knowledge.read.local or fs.write.local")
	}
	pending := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-kb-write-pending", dynamicOperationScope: "root-kb-write-pending",
		dynamicSemanticRouting: &routing, knowledgeStore: kb,
	}
	pendingDefs, pendingManaged := pending.dynamicSemanticToolDefinitions()
	if !pendingManaged || len(pendingDefs) != 1 {
		t.Fatalf("continue before ingest receipt must replay, defs=%#v managed=%v", pendingDefs, pendingManaged)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result := first.ExecuteToolCall(name, `{"text":"remember this note"}`, "call-ingest")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "src-text") {
		t.Fatalf("knowledge ingest execution=%+v", result)
	}
	if kb.text.Text != "remember this note" || kb.text.TenantID != first.principal.TenantID || kb.text.OwnerID != first.principal.UserID {
		t.Fatalf("saved text=%#v", kb.text)
	}
	task, ok = store.Load(request)
	if !ok || task.Status != sessionGovernedSucceeded {
		t.Fatalf("successful ingest must mark SessionGoverned succeeded, task=%#v ok=%v", task, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-kb-write-2", dynamicOperationScope: "root-kb-write-2",
		dynamicSemanticRouting: &routing, knowledgeStore: kb,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded knowledge_write must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticMemoryManageUsesHostStoreAndDoesNotReplayAfterSuccess(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	mem, err := memory.NewStoreWithMode(t.TempDir(), memory.StoreModeJSON)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Stop()
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"remember that I prefer Chinese": {Primary: intent.LabelMemoryManage, Confidence: 0.95, Layer: 3},
		"continue":                       {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "remember that I prefer Chinese",
		loopID:   "session-memory", dynamicOperationScope: "root-memory", dynamicSemanticRouting: &routing,
		memory: mem,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("memory_manage turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	task, ok := store.Load(request)
	if !ok || task.Status != sessionGovernedPending || task.Needs[0].Capability != CapabilityMemoryManage {
		t.Fatalf("memory_manage must persist as pending mutation, task=%#v ok=%v", task, ok)
	}
	if task.Needs[0].Capability == CapabilityKnowledgeRead || task.Needs[0].Capability == CapabilityKnowledgeWrite {
		t.Fatal("memory_manage must not persist as knowledge.read.local or knowledge.ingest.local")
	}
	pending := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-memory-pending", dynamicOperationScope: "root-memory-pending",
		dynamicSemanticRouting: &routing, memory: mem,
	}
	pendingDefs, pendingManaged := pending.dynamicSemanticToolDefinitions()
	if !pendingManaged || len(pendingDefs) != 1 {
		t.Fatalf("continue before memory receipt must replay, defs=%#v managed=%v", pendingDefs, pendingManaged)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result := first.ExecuteToolCall(name, `{"content":"I prefer Chinese"}`, "call-memory")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "Memory saved") {
		t.Fatalf("memory manage execution=%+v", result)
	}
	task, ok = store.Load(request)
	if !ok || task.Status != sessionGovernedSucceeded {
		t.Fatalf("successful memory manage must mark SessionGoverned succeeded, task=%#v ok=%v", task, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-memory-2", dynamicOperationScope: "root-memory-2",
		dynamicSemanticRouting: &routing, memory: mem,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded memory_manage must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticTaskTrackUsesHostStoreAndDoesNotReplayAfterSuccess(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	todos := task.NewStore()
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"add this item to the task list": {Primary: intent.LabelTaskTrack, Confidence: 0.95, Layer: 3},
		"continue":                       {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "add this item to the task list",
		loopID:   "session-task", dynamicOperationScope: "root-task", dynamicSemanticRouting: &routing,
		tasks: todos,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("task_track turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	taskState, ok := store.Load(request)
	if !ok || taskState.Status != sessionGovernedPending || taskState.Needs[0].Capability != CapabilityTaskTrack {
		t.Fatalf("task_track must persist as pending mutation, task=%#v ok=%v", taskState, ok)
	}
	if taskState.Needs[0].Capability == coretool.CapabilityGoalManageLongRunning || taskState.Needs[0].Capability == coretool.CapabilityAgentDelegateSubtask {
		t.Fatal("task_track must not persist as goal.manage.long_running or agent.delegate.subtask")
	}
	pending := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-task-pending", dynamicOperationScope: "root-task-pending",
		dynamicSemanticRouting: &routing, tasks: todos,
	}
	pendingDefs, pendingManaged := pending.dynamicSemanticToolDefinitions()
	if !pendingManaged || len(pendingDefs) != 1 {
		t.Fatalf("continue before task receipt must replay, defs=%#v managed=%v", pendingDefs, pendingManaged)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result := first.ExecuteToolCall(name, `{"title":"fix login"}`, "call-task")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "fix login") {
		t.Fatalf("task track execution=%+v", result)
	}
	taskState, ok = store.Load(request)
	if !ok || taskState.Status != sessionGovernedSucceeded {
		t.Fatalf("successful task track must mark SessionGoverned succeeded, task=%#v ok=%v", taskState, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-task-2", dynamicOperationScope: "root-task-2",
		dynamicSemanticRouting: &routing, tasks: todos,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded task_track must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticGoalManageUsesHostStoreAndDoesNotReplayAfterSuccess(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	goals := goal.NewStore("")
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"create a long-running goal to keep this documentation up to date": {Primary: intent.LabelGoalManage, Confidence: 0.95, Layer: 3},
		"continue": {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "create a long-running goal to keep this documentation up to date",
		loopID:   "session-goal", dynamicOperationScope: "root-goal", dynamicSemanticRouting: &routing,
		goals: goals,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("goal_manage turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	taskState, ok := store.Load(request)
	if !ok || taskState.Status != sessionGovernedPending || taskState.Needs[0].Capability != CapabilityGoalManage {
		t.Fatalf("goal_manage must persist as pending mutation, task=%#v ok=%v", taskState, ok)
	}
	if taskState.Needs[0].Capability == CapabilityTaskTrack || taskState.Needs[0].Capability == coretool.CapabilityAgentDelegateSubtask {
		t.Fatal("goal_manage must not persist as task.track.local or agent.delegate.subtask")
	}
	pending := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-goal-pending", dynamicOperationScope: "root-goal-pending",
		dynamicSemanticRouting: &routing, goals: goals,
	}
	pendingDefs, pendingManaged := pending.dynamicSemanticToolDefinitions()
	if !pendingManaged || len(pendingDefs) != 1 {
		t.Fatalf("continue before goal receipt must replay, defs=%#v managed=%v", pendingDefs, pendingManaged)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result := first.ExecuteToolCall(name, `{"objective":"keep this documentation up to date"}`, "call-goal")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "keep this documentation up to date") {
		t.Fatalf("goal manage execution=%+v", result)
	}
	taskState, ok = store.Load(request)
	if !ok || taskState.Status != sessionGovernedSucceeded {
		t.Fatalf("successful goal manage must mark SessionGoverned succeeded, task=%#v ok=%v", taskState, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-goal-2", dynamicOperationScope: "root-goal-2",
		dynamicSemanticRouting: &routing, goals: goals,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded goal_manage must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticTemplateManageUsesHostStoreAndDoesNotReplayAfterSuccess(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	templates, err := remote.NewSessionTemplateManager(filepath.Join(t.TempDir(), "session_templates.json"))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"create a session template that uses codex": {Primary: intent.LabelTemplateManage, Confidence: 0.95, Layer: 3},
		"continue": {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "create a session template that uses codex",
		loopID:   "session-template", dynamicOperationScope: "root-template", dynamicSemanticRouting: &routing,
		templates: templates,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("template_manage turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	taskState, ok := store.Load(request)
	if !ok || taskState.Status != sessionGovernedPending || taskState.Needs[0].Capability != CapabilityTemplateManage {
		t.Fatalf("template_manage must persist as pending mutation, task=%#v ok=%v", taskState, ok)
	}
	if taskState.Needs[0].Capability == coretool.CapabilitySessionManageCoding || taskState.Needs[0].Capability == coretool.CapabilityConfigManageSelf {
		t.Fatal("template_manage must not persist as session.manage.coding or config.manage.self")
	}
	pending := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-template-pending", dynamicOperationScope: "root-template-pending",
		dynamicSemanticRouting: &routing, templates: templates,
	}
	pendingDefs, pendingManaged := pending.dynamicSemanticToolDefinitions()
	if !pendingManaged || len(pendingDefs) != 1 {
		t.Fatalf("continue before template receipt must replay, defs=%#v managed=%v", pendingDefs, pendingManaged)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result := first.ExecuteToolCall(name, `{"name":"codex-default","coding_tool":"codex"}`, "call-template")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "codex-default") {
		t.Fatalf("template manage execution=%+v", result)
	}
	taskState, ok = store.Load(request)
	if !ok || taskState.Status != sessionGovernedSucceeded {
		t.Fatalf("successful template manage must mark SessionGoverned succeeded, task=%#v ok=%v", taskState, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-template-2", dynamicOperationScope: "root-template-2",
		dynamicSemanticRouting: &routing, templates: templates,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded template_manage must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticScheduleAdministerUsesHostStoreAndDoesNotReplayAfterSuccess(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	schedules, err := scheduler.NewManager(filepath.Join(t.TempDir(), "schedules.json"))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"list all scheduled tasks": {Primary: intent.LabelScheduleManage, Confidence: 0.95, Layer: 3},
		"continue":                 {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "list all scheduled tasks",
		loopID:   "session-schedule", dynamicOperationScope: "root-schedule", dynamicSemanticRouting: &routing,
		schedules: schedules,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("schedule_manage turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	taskState, ok := store.Load(request)
	if !ok || taskState.Status != sessionGovernedPending || taskState.Needs[0].Capability != CapabilityScheduleAdminister {
		t.Fatalf("schedule_manage must persist as pending mutation, task=%#v ok=%v", taskState, ok)
	}
	if taskState.Needs[0].Capability == coretool.CapabilityScheduleDispatchChannel || taskState.Needs[0].Capability == CapabilityTaskTrack {
		t.Fatal("schedule_manage must not persist as schedule.dispatch.channel or task.track.local")
	}
	pending := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-schedule-pending", dynamicOperationScope: "root-schedule-pending",
		dynamicSemanticRouting: &routing, schedules: schedules,
	}
	pendingDefs, pendingManaged := pending.dynamicSemanticToolDefinitions()
	if !pendingManaged || len(pendingDefs) != 1 {
		t.Fatalf("continue before schedule receipt must replay, defs=%#v managed=%v", pendingDefs, pendingManaged)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result := first.ExecuteToolCall(name, `{"name":"standup","task_action":"remind standup","hour":9}`, "call-schedule")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "standup") {
		t.Fatalf("schedule administer execution=%+v", result)
	}
	if created := schedules.List(); len(created) != 1 || created[0].Delivery != nil {
		t.Fatalf("managed create must persist a local record without Delivery, got=%#v", created)
	}
	taskState, ok = store.Load(request)
	if !ok || taskState.Status != sessionGovernedSucceeded {
		t.Fatalf("successful schedule administer must mark SessionGoverned succeeded, task=%#v ok=%v", taskState, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-schedule-2", dynamicOperationScope: "root-schedule-2",
		dynamicSemanticRouting: &routing, schedules: schedules,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded schedule_manage must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticKnowledgeAdminUsesHostStoreAndDoesNotReplayAfterSuccess(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	knowledgeStore := &mgmtFakeKnowledgeStore{sources: map[string]knowledge.Source{
		"s1": {ID: "s1", Title: "notes", Status: "active", TenantID: "tenant", OwnerID: "user"},
	}}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"disable this knowledge base source": {Primary: intent.LabelKnowledgeAdmin, Confidence: 0.95, Layer: 3},
		"continue":                           {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "disable this knowledge base source",
		loopID:   "session-knowledge-admin", dynamicOperationScope: "root-knowledge-admin", dynamicSemanticRouting: &routing,
		knowledgeStore: knowledgeStore,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("knowledge_admin turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	taskState, ok := store.Load(request)
	if !ok || taskState.Status != sessionGovernedPending || taskState.Needs[0].Capability != CapabilityKnowledgeAdmin {
		t.Fatalf("knowledge_admin must persist as pending mutation, task=%#v ok=%v", taskState, ok)
	}
	if taskState.Needs[0].Capability == CapabilityKnowledgeRead || taskState.Needs[0].Capability == CapabilityKnowledgeWrite {
		t.Fatal("knowledge_admin must not persist as knowledge.read.local or knowledge.ingest.local")
	}
	pending := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-knowledge-admin-pending", dynamicOperationScope: "root-knowledge-admin-pending",
		dynamicSemanticRouting: &routing, knowledgeStore: knowledgeStore,
	}
	pendingDefs, pendingManaged := pending.dynamicSemanticToolDefinitions()
	if !pendingManaged || len(pendingDefs) != 1 {
		t.Fatalf("continue before knowledge admin receipt must replay, defs=%#v managed=%v", pendingDefs, pendingManaged)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result := first.ExecuteToolCall(name, `{"id":"s1","status":"disabled"}`, "call-knowledge-admin")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "s1") {
		t.Fatalf("knowledge admin execution=%+v", result)
	}
	if len(knowledgeStore.disabledIDs) != 1 || knowledgeStore.disabledIDs[0] != "s1" {
		t.Fatalf("disable must hit the host store, got=%v", knowledgeStore.disabledIDs)
	}
	taskState, ok = store.Load(request)
	if !ok || taskState.Status != sessionGovernedSucceeded {
		t.Fatalf("successful knowledge admin must mark SessionGoverned succeeded, task=%#v ok=%v", taskState, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-knowledge-admin-2", dynamicOperationScope: "root-knowledge-admin-2",
		dynamicSemanticRouting: &routing, knowledgeStore: knowledgeStore,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded knowledge_admin must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticConfigManageUsesHostStoreAndDoesNotReplayAfterSuccess(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	manager := &fakeHostConfigManager{result: "配置已更新。\n当前配置:\n- max_iterations: 50\n- thinking_mode: auto"}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"raise the max iteration limit": {Primary: intent.LabelConfigManage, Confidence: 0.95, Layer: 3},
		"continue":                      {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "raise the max iteration limit",
		loopID:   "session-config-manage", dynamicOperationScope: "root-config-manage", dynamicSemanticRouting: &routing,
		configManager: manager,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("config_manage turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	taskState, ok := store.Load(request)
	if !ok || taskState.Status != sessionGovernedPending || taskState.Needs[0].Capability != CapabilityConfigManage {
		t.Fatalf("config_manage must persist as pending mutation, task=%#v ok=%v", taskState, ok)
	}
	if taskState.Needs[0].Capability == CapabilityInformationLookup || taskState.Needs[0].Capability == coretool.CapabilitySessionManageCoding {
		t.Fatal("config_manage must not persist as information.lookup or session.manage.coding")
	}
	pending := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-config-manage-pending", dynamicOperationScope: "root-config-manage-pending",
		dynamicSemanticRouting: &routing, configManager: manager,
	}
	pendingDefs, pendingManaged := pending.dynamicSemanticToolDefinitions()
	if !pendingManaged || len(pendingDefs) != 1 {
		t.Fatalf("continue before a config receipt must replay, defs=%#v managed=%v", pendingDefs, pendingManaged)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result := first.ExecuteToolCall(name, `{"max_iterations":50}`, "call-config-manage")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "50") {
		t.Fatalf("config execution=%+v", result)
	}
	if !manager.hasMax || manager.maxIterations != 50 {
		t.Fatalf("set must hit the host config manager, got=%#v", manager)
	}
	taskState, ok = store.Load(request)
	if !ok || taskState.Status != sessionGovernedSucceeded {
		t.Fatalf("successful config manage must mark SessionGoverned succeeded, task=%#v ok=%v", taskState, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-config-manage-2", dynamicOperationScope: "root-config-manage-2",
		dynamicSemanticRouting: &routing, configManager: manager,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded config_manage must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestCoreDynamicSemanticSessionManageInspectsWithoutDriveAndDoesNotReplayAfterSuccess(t *testing.T) {
	store := NewSessionGovernedTaskStore()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := testSessionGovernedLookupResolver(t, store, scriptedIntentClassificationSource{byText: map[string]intent.ClassificationResult{
		"list my running coding sessions": {Primary: intent.LabelSessionManage, Confidence: 0.95, Layer: 3},
		"continue":                        {Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	}})
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, SessionGoverned: store,
	}
	bindSessionGovernedStore(&routing)
	first := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "list my running coding sessions",
		loopID:   "session-session-manage", dynamicOperationScope: "root-session-manage", dynamicSemanticRouting: &routing,
	}
	defs, managed := first.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("session_manage turn defs=%#v managed=%v", defs, managed)
	}
	request := DynamicCapabilityNeedRequest{Principal: first.principal, ChannelScope: "core-agent"}
	taskState, ok := store.Load(request)
	if !ok || taskState.Status != sessionGovernedPending || taskState.Needs[0].Capability != CapabilitySessionManage {
		t.Fatalf("session_manage must persist as pending inspect, task=%#v ok=%v", taskState, ok)
	}
	if taskState.Needs[0].Capability == CapabilityTemplateManage || taskState.Needs[0].Capability == coretool.CapabilityAgentDelegateSubtask {
		t.Fatal("session_manage must not persist as template.manage.session or agent.delegate.subtask")
	}
	pending := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-session-manage-pending", dynamicOperationScope: "root-session-manage-pending",
		dynamicSemanticRouting: &routing,
	}
	pendingDefs, pendingManaged := pending.dynamicSemanticToolDefinitions()
	if !pendingManaged || len(pendingDefs) != 1 {
		t.Fatalf("continue before a session inspect receipt must replay, defs=%#v managed=%v", pendingDefs, pendingManaged)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	result := first.ExecuteToolCall(name, `{}`, "call-session-manage")
	if result.Outcome != "ok" || !strings.Contains(result.Result, "没有编码会话") {
		t.Fatalf("session inspect execution=%+v", result)
	}
	taskState, ok = store.Load(request)
	if !ok || taskState.Status != sessionGovernedSucceeded {
		t.Fatalf("successful session inspect must mark SessionGoverned succeeded, task=%#v ok=%v", taskState, ok)
	}
	continued := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"}, userText: "continue",
		loopID: "session-session-manage-2", dynamicOperationScope: "root-session-manage-2",
		dynamicSemanticRouting: &routing,
	}
	contDefs, contManaged := continued.dynamicSemanticToolDefinitions()
	if contManaged || len(contDefs) != 0 {
		t.Fatalf("continue after succeeded session_manage must stay unmanaged, defs=%#v managed=%v", contDefs, contManaged)
	}
}

func TestServiceConfigureDynamicSemanticRoutingBindsSessionGovernedStore(t *testing.T) {
	executor := &CoreAgentExecutor{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test"}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: scriptedIntentClassificationSource{}, Registry: registry,
		Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	if err := svc.ConfigureDynamicSemanticRouting(registry, resolver, ReviewedDynamicCapabilityPolicyAdapter(), time.Minute); err != nil {
		t.Fatal(err)
	}
	routing := executor.getDynamicSemanticRouting()
	if routing == nil || routing.SessionGoverned == nil || resolver.SessionGoverned == nil || routing.SessionGoverned != resolver.SessionGoverned {
		t.Fatal("ConfigureDynamicSemanticRouting must share one SessionGoverned store with the intent resolver")
	}
	if executor.auditReader == nil {
		t.Fatal("ConfigureDynamicSemanticRouting must wire the host-owned audit reader")
	}
	if executor.configManager == nil {
		t.Fatal("ConfigureDynamicSemanticRouting must wire the host-owned config manager")
	}
}
