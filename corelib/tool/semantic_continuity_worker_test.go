package tool

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestPendingContinuityTenantsAndWorkerDrainAreBoundedAndTenantScoped(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("worker-read", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	issuer, err := NewInvocationIssuerWithStore([]byte("worker-signing-key-012345678901234567"), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	publish := func(tenant, root string) InvocationScope {
		t.Helper()
		plan, planErr := NewToolPlanner(registry).Plan(RouteRequest{
			RootTaskID: root, SessionID: "session-" + tenant, TurnID: "turn-" + tenant,
			Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}),
			Needs:    []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
		})
		if planErr != nil {
			t.Fatal(planErr)
		}
		scope := InvocationScope{RootTaskID: root, PlanID: plan.ID, SessionID: "session-" + tenant, TurnID: "turn-" + tenant, PrincipalID: "principal-" + tenant}
		if _, _, publishErr := coordinator.PublishSurface(SurfacePublishRequest{
			Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest},
			TenantID: tenant, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC(),
		}); publishErr != nil {
			t.Fatal(publishErr)
		}
		return scope
	}

	scopeA := publish("tenant-a", "worker-root-a")
	scopeB := publish("tenant-b", "worker-root-b")
	tenants, err := coordinator.PendingContinuityTenants(0)
	if err != nil || len(tenants) != 2 || tenants[0] != "tenant-a" || tenants[1] != "tenant-b" {
		t.Fatalf("pending tenants=%v err=%v", tenants, err)
	}

	worker := NewContinuityProjectionWorker(coordinator, time.Hour, 1)
	if applied, err := worker.DrainOnce(context.Background()); err != nil || applied != 2 {
		t.Fatalf("bounded worker drain applied=%d err=%v", applied, err)
	}
	if _, err := coordinator.ContinuityState(ContinuityScope{TenantID: "tenant-a", PrincipalID: scopeA.PrincipalID, ConversationID: scopeA.SessionID, RootTaskID: scopeA.RootTaskID}); err != nil {
		t.Fatalf("tenant-a projection missing: %v", err)
	}
	if _, err := coordinator.ContinuityState(ContinuityScope{TenantID: "tenant-b", PrincipalID: scopeB.PrincipalID, ConversationID: scopeB.SessionID, RootTaskID: scopeB.RootTaskID}); err != nil {
		t.Fatalf("tenant-b projection missing: %v", err)
	}
	if tenants, err := coordinator.PendingContinuityTenants(0); err != nil || len(tenants) != 0 {
		t.Fatalf("pending rows remain tenants=%v err=%v", tenants, err)
	}
}

func TestContinuityProjectionWorkerStopsBeforeCoordinatorClose(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewContinuityProjectionWorker(coordinator, time.Millisecond, 1)
	ctx, cancel := context.WithCancel(context.Background())
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	worker.Stop()
	// Stop is idempotent and leaves no goroutine using the coordinator, so the
	// owner can close the SQLite handle immediately.
	worker.Stop()
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
}
