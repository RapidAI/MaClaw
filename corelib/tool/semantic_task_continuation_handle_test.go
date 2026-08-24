package tool

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func publishTaskContinuationRoute(t *testing.T, coordinator *SQLiteSemanticExecutionCoordinator, root, session, principal, tenant, turn string, parent *RouteRevisionRef) (InvocationScope, ToolPlan, *InvocationIssuer) {
	t.Helper()
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("continuation_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: root, SessionID: session, TurnID: turn, Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("k", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: root, PlanID: plan.ID, SessionID: session, TurnID: turn, PrincipalID: principal}
	if _, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, ExpectedParent: parent, SnapshotDigest: plan.SnapshotDigest}, TenantID: tenant, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	return scope, plan, issuer
}

func TestTaskContinuationHandleIsScopeBoundSingleUseAndFenced(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	scope, _, _ := publishTaskContinuationRoute(t, coordinator, "root", "session", "tenant:user", "tenant", "turn-1", nil)
	issued, err := coordinator.IssueTaskContinuationHandle("tenant", scope.PrincipalID, scope.SessionID, scope.RootTaskID, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ConsumeTaskContinuationHandle(issued.ID, "other", scope.PrincipalID, scope.SessionID, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "scope_mismatch") {
		t.Fatalf("cross-tenant handle consume err=%v", err)
	}
	if _, err := coordinator.ConsumeTaskContinuationHandle(issued.ID, "tenant", "tenant:other", scope.SessionID, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "scope_mismatch") {
		t.Fatalf("cross-principal handle consume err=%v", err)
	}
	if _, err := coordinator.ConsumeTaskContinuationHandle(issued.ID, "tenant", scope.PrincipalID, "other-session", time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "scope_mismatch") {
		t.Fatalf("cross-session handle consume err=%v", err)
	}
	consumed, err := coordinator.ConsumeTaskContinuationHandle(issued.ID, "tenant", scope.PrincipalID, scope.SessionID, time.Now().UTC())
	if err != nil || consumed.RootTaskID != scope.RootTaskID || consumed.ConsumedAt == nil {
		t.Fatalf("consume handle=%#v err=%v", consumed, err)
	}
	if _, err := coordinator.ConsumeTaskContinuationHandle(issued.ID, "tenant", scope.PrincipalID, scope.SessionID, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "not_active") {
		t.Fatalf("replayed handle err=%v", err)
	}

	stale, err := coordinator.IssueTaskContinuationHandle("tenant", scope.PrincipalID, scope.SessionID, scope.RootTaskID, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	parent, err := coordinator.Routes.CurrentRevision(scope)
	if err != nil {
		t.Fatal(err)
	}
	publishTaskContinuationRoute(t, coordinator, scope.RootTaskID, scope.SessionID, scope.PrincipalID, "tenant", "turn-2", &parent)
	if _, err := coordinator.ConsumeTaskContinuationHandle(stale.ID, "tenant", scope.PrincipalID, scope.SessionID, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "superseded") {
		t.Fatalf("superseded handle err=%v", err)
	}
}

func TestTaskContinuationHandleExpires(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	scope, _, _ := publishTaskContinuationRoute(t, coordinator, "root", "session", "tenant:user", "tenant", "turn-1", nil)
	now := time.Now().UTC()
	handle, err := coordinator.IssueTaskContinuationHandle("tenant", scope.PrincipalID, scope.SessionID, scope.RootTaskID, time.Millisecond, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ConsumeTaskContinuationHandle(handle.ID, "tenant", scope.PrincipalID, scope.SessionID, now.Add(time.Second)); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired handle err=%v", err)
	}
}
