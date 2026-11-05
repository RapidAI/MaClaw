package tool

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
)

func TestHopJITPlanToolProviderExecutesAuthorizedRealCall(t *testing.T) {
	fixture := newHopJITProviderFixture(t)
	calls := 0
	fixture.provider.Invoke = func(_ context.Context, selection PlannedSelection, request CanonicalRequest) SelectionExecutionResult {
		calls++
		if selection.AdapterName != "read" || request.Values["path"] != "input.md" {
			t.Fatalf("selection=%+v request=%+v", selection, request)
		}
		return SelectionExecutionResult{Result: "文件内容", Succeeded: true}
	}
	engine := hopjit.NewExecutionEngine(nil, hopjit.WithToolProvider(fixture.provider))
	_, err := engine.Init(context.Background(), `# Spec: 真实工具桥接
Goal: 通过 PlanExecutor 读取
Outputs:
- content % text
## Steps
1. [act] 读取文件
  + → content % text
  > `+"```hop_python"+`
  > content = read(path: "input.md")
  > `+"```"+`
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != hopjit.NextCompleted || next.Outputs["content"] != "文件内容" || calls != 1 {
		t.Fatalf("next=%+v calls=%d", next, calls)
	}
	record, err := fixture.executions.Execution(fixture.scope, "selection:read")
	if err != nil || record.State != PlanExecutionSucceeded {
		t.Fatalf("execution=%+v err=%v", record, err)
	}
	_, err = fixture.provider.Execute(context.Background(), "read", map[string]any{"path": "input.md"})
	if err == nil || !strings.Contains(err.Error(), "invocation_grant_replayed") {
		t.Fatalf("replay error=%v", err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestHopJITPlanToolProviderRejectsUnauthorizedArguments(t *testing.T) {
	fixture := newHopJITProviderFixture(t)
	called := false
	fixture.provider.Invoke = func(context.Context, PlannedSelection, CanonicalRequest) SelectionExecutionResult {
		called = true
		return SelectionExecutionResult{Succeeded: true}
	}
	_, err := fixture.provider.Execute(context.Background(), "read", map[string]any{"unexpected": "value"})
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	if _, err := fixture.executions.Execution(fixture.scope, "selection:read"); err == nil {
		t.Fatal("rejected argument created an execution record")
	}
}

func TestHopJITPlanToolProviderDoesNotProjectUnsettledResultAsValue(t *testing.T) {
	for _, result := range []SelectionExecutionResult{
		{Paused: true},
		{AwaitingReceipt: true},
		{Unknown: true},
	} {
		fixture := newHopJITProviderFixture(t)
		fixture.provider.Invoke = func(context.Context, PlannedSelection, CanonicalRequest) SelectionExecutionResult {
			return result
		}
		value, err := fixture.provider.Execute(context.Background(), "read", map[string]any{"path": "input.md"})
		if err == nil || value != nil {
			t.Fatalf("result=%+v value=%v err=%v", result, value, err)
		}
	}
}

type hopJITResultStoreStub struct {
	values map[string]string
}

func (s *hopJITResultStoreStub) key(scope InvocationScope, selectionID, requestDigest string) string {
	return scope.RootTaskID + "\x00" + scope.PlanID + "\x00" + scope.SessionID + "\x00" + scope.TurnID + "\x00" + scope.PrincipalID + "\x00" + selectionID + "\x00" + requestDigest
}

func (s *hopJITResultStoreStub) LoadHopJITResult(scope InvocationScope, selectionID, requestDigest string) (string, bool, error) {
	value, ok := s.values[s.key(scope, selectionID, requestDigest)]
	return value, ok, nil
}

func (s *hopJITResultStoreStub) SaveHopJITResult(scope InvocationScope, selectionID, requestDigest, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[s.key(scope, selectionID, requestDigest)] = value
	return nil
}

func TestHopJITPlanToolProviderRecoversPersistedResultWithoutReplay(t *testing.T) {
	fixture := newHopJITProviderFixture(t)
	results := &hopJITResultStoreStub{}
	fixture.provider.ResultStore = results
	calls := 0
	fixture.provider.Invoke = func(context.Context, PlannedSelection, CanonicalRequest) SelectionExecutionResult {
		calls++
		return SelectionExecutionResult{Result: "已持久化", Succeeded: true}
	}
	if _, err := fixture.provider.Execute(context.Background(), "read", map[string]any{"path": "input.md"}); err != nil {
		t.Fatal(err)
	}
	recovered := *fixture.provider
	recovered.AllowResultRecovery = true
	recovered.Invoke = func(context.Context, PlannedSelection, CanonicalRequest) SelectionExecutionResult {
		t.Fatal("recovery invoked external tool")
		return SelectionExecutionResult{}
	}
	value, err := recovered.Execute(context.Background(), "read", map[string]any{"path": "input.md"})
	if err != nil || value != "已持久化" || calls != 1 {
		t.Fatalf("value=%v calls=%d err=%v", value, calls, err)
	}
	if _, err := recovered.Execute(context.Background(), "read", map[string]any{"path": "other.md"}); err == nil || !strings.Contains(err.Error(), "result_missing") {
		t.Fatalf("different request recovered stale result: %v", err)
	}
	for key := range results.values {
		results.values[key] = "篡改"
	}
	if _, err := recovered.Execute(context.Background(), "read", map[string]any{"path": "input.md"}); err == nil || !strings.Contains(err.Error(), "digest_mismatch") {
		t.Fatalf("digest mismatch err=%v", err)
	}
}

type hopJITProviderFixture struct {
	provider   *HopJITPlanToolProvider
	scope      InvocationScope
	executions PlanExecutionStore
}

func newHopJITProviderFixture(t *testing.T) *hopJITProviderFixture {
	t.Helper()
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string"},
		},
		"required": []string{"path"}, "additionalProperties": false,
	}
	registry := semanticRegistry(t)
	qualifiers := map[string]string{"display": "primary"}
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProviderWithSchema("read", CapabilityID("visual.capture.desktop"), qualifiers, schema, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "hop-task", TurnID: "hop-turn", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "read", Capability: CapabilityID("visual.capture.desktop"), Qualifiers: qualifiers, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("h", 32)), NewMemoryInvocationGrantStore())
	if err != nil {
		t.Fatal(err)
	}
	executions := NewMemoryPlanExecutionStore()
	executor, err := NewPlanExecutor(issuer, executions)
	if err != nil {
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: "hop-task", PlanID: plan.ID, SessionID: "hop-session", TurnID: "hop-turn", PrincipalID: "hop-user"}
	grants, err := issuer.IssueReady(plan, scope, time.Minute, nil)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%+v err=%v", grants, err)
	}
	provider := &HopJITPlanToolProvider{
		Executor: executor, Scope: scope, Plan: plan,
		Grants:  map[string]InvocationGrant{"read": grants[0]},
		Schemas: map[string]map[string]interface{}{"read": schema},
	}
	return &hopJITProviderFixture{provider: provider, scope: scope, executions: executions}
}
