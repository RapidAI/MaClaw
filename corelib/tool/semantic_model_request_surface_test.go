package tool

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestModelRequestSurfaceRetryReusesGrantButRetiresOldAlias(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	plan, scope, issuer, grant := modelRequestSurfaceFixture(t, coordinator, "request-retry")
	if _, err := issuer.Validate(grant, scope, plan); err != nil {
		t.Fatalf("fixture grant invalid: %v", err)
	}
	first, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
		Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-a",
		Aliases: map[string]InvocationGrant{"skill_a": grant}, Now: time.Now().UTC(),
	})
	if err != nil || first.State != modelRequestSurfacePrepared {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-a", "provider/v1", "connection-1", "response-a", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	second, err := coordinator.ReplaceModelRequestSurface(ModelRequestSurfaceReplace{PreviousEpoch: "epoch-a", Successor: ModelRequestSurfacePublish{
		Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-b",
		Aliases: map[string]InvocationGrant{"skill_b": grant}, Now: time.Now().UTC(),
	}})
	if err != nil || second.ID == first.ID {
		t.Fatalf("second=%+v first=%+v err=%v", second, first, err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-b", "provider/v1", "connection-1", "response-b", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.ResolveModelRequestAlias("epoch-a", "provider/v1", "connection-1", "response-a", "skill_a"); err == nil || err.Error() != "stale_surface" {
		t.Fatalf("retired request resolved: %v", err)
	}
	resolved, resolvedScope, err := coordinator.ResolveModelRequestAlias("epoch-b", "provider/v1", "connection-1", "response-b", "skill_b")
	if err != nil || resolvedScope != scope || InvocationGrantFingerprint(resolved) != InvocationGrantFingerprint(grant) {
		t.Fatalf("resolved=%+v scope=%+v err=%v", resolved, resolvedScope, err)
	}
	if _, err := issuer.Validate(resolved, scope, plan); err != nil {
		t.Fatalf("retry must reuse the unconsumed grant: %v", err)
	}
}

func TestModelRequestSurfaceRequiresTrustedResponseCorrelation(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	_, scope, _, grant := modelRequestSurfaceFixture(t, coordinator, "request-correlation")
	if _, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
		Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-c",
		Aliases: map[string]InvocationGrant{"mcp_c": grant}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.ResolveModelRequestAlias("epoch-c", "provider/v1", "connection-1", "model-supplied", "mcp_c"); err == nil || err.Error() != "stale_surface" {
		t.Fatalf("uncorrelated response resolved: %v", err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-c", "provider/v1", "connection-1", "response-c", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-c", "provider/v1", "connection-1", "response-c", time.Now().UTC()); err != nil {
		t.Fatalf("identical response binding was not idempotent: %v", err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-c", "provider/v1", "connection-1", "response-other", time.Now().UTC()); err == nil || err.Error() != "model_request_surface_not_active" {
		t.Fatalf("different response replaced trusted binding: %v", err)
	}
	if _, _, err := coordinator.ResolveModelRequestAlias("epoch-c", "provider/v1", "connection-1", "response-c", "mcp_c"); err != nil {
		t.Fatalf("trusted response did not resolve: %v", err)
	}
}

func TestRecoverBoundModelRequestSurfaceSurvivesCoordinatorRestartWithoutRevivingTerminalSurfaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "semantic-execution.db")
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(path, WithCoordinatorContinuityTenant("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	plan, scope, _, grant := modelRequestSurfaceFixture(t, coordinator, "request-recovery")
	if _, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
		Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-recovery",
		Aliases: map[string]InvocationGrant{"opaque_alias": grant}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-recovery", "provider/v1", "connection-1", "response-recovery", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewSQLiteSemanticExecutionCoordinator(path, WithCoordinatorContinuityTenant("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	recovered, err := reopened.RecoverBoundModelRequestSurface(ModelRequestSurfaceRecovery{
		TenantID: "tenant-a", Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-recovery",
	})
	if err != nil {
		t.Fatalf("recover active surface: %v", err)
	}
	if recovered.Scope != scope || recovered.ResponseID != "response-recovery" || recovered.State != modelRequestSurfaceActive {
		t.Fatalf("recovered=%+v", recovered)
	}
	if len(recovered.Aliases) != 1 || InvocationGrantFingerprint(recovered.Aliases["opaque_alias"]) != InvocationGrantFingerprint(grant) {
		t.Fatalf("recovered aliases=%+v", recovered.Aliases)
	}
	if _, _, err := reopened.ResolveModelRequestAlias(recovered.Epoch, recovered.Protocol, recovered.ConnectionID, recovered.ResponseID, "opaque_alias"); err != nil {
		t.Fatalf("recovered surface did not resolve durable alias: %v", err)
	}
	if _, err := reopened.RecoverBoundModelRequestSurface(ModelRequestSurfaceRecovery{
		TenantID: "tenant-b", Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-recovery",
	}); err == nil || err.Error() != "stale_surface" {
		t.Fatalf("wrong tenant recovered surface: %v", err)
	}
	if err := reopened.FinishModelRequestSurface("epoch-recovery", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.RecoverBoundModelRequestSurface(ModelRequestSurfaceRecovery{
		TenantID: "tenant-a", Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-recovery",
	}); err == nil || err.Error() != "stale_surface" {
		t.Fatalf("terminal surface recovered: %v", err)
	}
	_ = plan // fixture also proves recovery never needs a process-local plan copy.
}

func TestRecoverBoundModelRequestSurfacePreservesToolSnapshotIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantic-execution-snapshot.db")
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(path, WithCoordinatorContinuityTenant("tenant-snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	registry := semanticRegistry(t)
	provider := semanticProvider("snapshot_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "request-snapshot", SessionID: "session", TurnID: "turn",
		Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}),
		Needs:    []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil {
		coordinator.Close()
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal", ToolSnapshotID: "toolsnap:surface-v1"}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("s", 32)), coordinator.Grants)
	if err != nil {
		coordinator.Close()
		t.Fatal(err)
	}
	_, grants, err := coordinator.PublishSurface(SurfacePublishRequest{
		Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest},
		TenantID: "tenant-snapshot", Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC(),
	})
	if err != nil || len(grants) != 1 {
		coordinator.Close()
		t.Fatalf("publish grants=%+v err=%v", grants, err)
	}
	if _, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
		Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-snapshot", Epoch: "epoch-snapshot",
		Aliases: map[string]InvocationGrant{"opaque": grants[0]}, Now: time.Now().UTC(),
	}); err != nil {
		coordinator.Close()
		t.Fatal(err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-snapshot", "provider/v1", "connection-snapshot", "response-snapshot", time.Now().UTC()); err != nil {
		coordinator.Close()
		t.Fatal(err)
	}
	coordinator.Close()
	reopened, err := NewSQLiteSemanticExecutionCoordinator(path, WithCoordinatorContinuityTenant("tenant-snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := reopened.RecoverBoundModelRequestSurface(ModelRequestSurfaceRecovery{TenantID: "tenant-snapshot", Protocol: "provider/v1", ConnectionID: "connection-snapshot", Epoch: "epoch-snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Scope.ToolSnapshotID != scope.ToolSnapshotID {
		t.Fatalf("recovered snapshot=%q, want %q", recovered.Scope.ToolSnapshotID, scope.ToolSnapshotID)
	}
	var persisted string
	if err := reopened.db.QueryRow(`SELECT tool_snapshot_id FROM semantic_model_request_surfaces WHERE epoch=?`, "epoch-snapshot").Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != scope.ToolSnapshotID {
		t.Fatalf("persisted surface snapshot=%q, want %q", persisted, scope.ToolSnapshotID)
	}
	if _, err := reopened.db.Exec(`UPDATE semantic_model_request_surfaces SET tool_snapshot_id=? WHERE epoch=?`, "toolsnap:surface-other", "epoch-snapshot"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.ResolveModelRequestAlias("epoch-snapshot", "provider/v1", "connection-snapshot", "response-snapshot", "opaque"); err == nil || err.Error() != "stale_surface" {
		t.Fatalf("mismatched concrete surface snapshot was accepted: %v", err)
	}
	// Simulate a pre-migration request-surface row. The signed grant JSON still
	// carries the concrete scope, so recovery must hydrate the legacy blank
	// column rather than silently dropping the snapshot identity.
	if _, err := reopened.db.Exec(`UPDATE semantic_model_request_surfaces SET tool_snapshot_id='' WHERE epoch=?`, "epoch-snapshot"); err != nil {
		t.Fatal(err)
	}
	legacyRecovered, err := reopened.RecoverBoundModelRequestSurface(ModelRequestSurfaceRecovery{TenantID: "tenant-snapshot", Protocol: "provider/v1", ConnectionID: "connection-snapshot", Epoch: "epoch-snapshot"})
	if err != nil || legacyRecovered.Scope.ToolSnapshotID != scope.ToolSnapshotID {
		t.Fatalf("legacy recovery snapshot=%q err=%v", legacyRecovered.Scope.ToolSnapshotID, err)
	}
	if _, resolvedScope, err := reopened.ResolveModelRequestAlias("epoch-snapshot", "provider/v1", "connection-snapshot", "response-snapshot", "opaque"); err != nil || resolvedScope.ToolSnapshotID != scope.ToolSnapshotID {
		t.Fatalf("resolved scope=%+v err=%v", resolvedScope, err)
	}
}

// A pre-snapshot surface may contain a legacy grant whose signed scope has no
// ToolSnapshotID, while the durable route/surface row has since been
// canonicalized to a concrete snapshot. Recovery must retain that old grant
// and let the explicit canonical validation path verify it; arbitrary direct
// Validate calls remain strict.
func TestModelRequestSurfaceRecoversLegacyGrantAgainstCanonicalScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-surface.db")
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(path, WithCoordinatorContinuityTenant("tenant-legacy-surface"))
	if err != nil {
		t.Fatal(err)
	}
	registry := semanticRegistry(t)
	provider := semanticProvider("legacy_surface_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "legacy-surface", SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		coordinator.Close()
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal", ToolSnapshotID: plan.SnapshotDigest}
	key := []byte(strings.Repeat("u", 32))
	issuer, err := NewInvocationIssuerWithStore(key, coordinator.Grants)
	if err != nil {
		coordinator.Close()
		t.Fatal(err)
	}
	_, grants, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, TenantID: "tenant-legacy-surface", Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err != nil || len(grants) != 1 {
		coordinator.Close()
		t.Fatalf("publish grants=%+v err=%v", grants, err)
	}
	legacy := grants[0]
	legacy.CatalogDigest = ""
	legacy.Scope.ToolSnapshotID = ""
	legacy.Token = invocationTokenForVersion(legacy, invocationGrantPayloadLegacy)
	legacy.Signature = invocationGrantSignatureForVersion(key, legacy, invocationGrantPayloadLegacy)
	legacyJSON, err := json.Marshal(legacy)
	if err != nil {
		coordinator.Close()
		t.Fatal(err)
	}
	if _, err := coordinator.db.Exec(`UPDATE invocation_grants SET fingerprint=? WHERE nonce=?`, invocationGrantFingerprintForVersion(legacy, invocationGrantPayloadLegacy), legacy.Nonce); err != nil {
		coordinator.Close()
		t.Fatal(err)
	}
	if _, err := coordinator.db.Exec(`UPDATE semantic_route_materializations SET function_name=?, grant_json=? WHERE route_key=? AND function_name=?`, legacy.Token, legacyJSON, routeStateKey(scope), grants[0].Token); err != nil {
		coordinator.Close()
		t.Fatal(err)
	}
	if _, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{Scope: scope, Protocol: "provider/v1", ConnectionID: "legacy-connection", Epoch: "legacy-epoch", Aliases: map[string]InvocationGrant{"opaque": legacy}, Now: time.Now().UTC()}); err != nil {
		coordinator.Close()
		t.Fatalf("publish legacy surface: %v", err)
	}
	if err := coordinator.BindModelRequestResponse("legacy-epoch", "provider/v1", "legacy-connection", "legacy-response", time.Now().UTC()); err != nil {
		coordinator.Close()
		t.Fatal(err)
	}
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteSemanticExecutionCoordinator(path, WithCoordinatorContinuityTenant("tenant-legacy-surface"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	restarted, err := NewInvocationIssuerWithStore(key, reopened.Grants)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.RecoverBoundModelRequestSurface(ModelRequestSurfaceRecovery{TenantID: "tenant-legacy-surface", Protocol: "provider/v1", ConnectionID: "legacy-connection", Epoch: "legacy-epoch"})
	if err != nil {
		t.Fatalf("recover legacy surface: %v", err)
	}
	recoveredGrant := recovered.Aliases["opaque"]
	if recovered.Scope.ToolSnapshotID != scope.ToolSnapshotID || recoveredGrant.Scope.ToolSnapshotID != "" {
		t.Fatalf("recovered scope=%+v grant scope=%+v", recovered.Scope, recoveredGrant.Scope)
	}
	if _, err := restarted.Validate(recoveredGrant, recovered.Scope, plan); err == nil || err.Error() != "invocation_grant_scope_mismatch" {
		t.Fatalf("strict validation unexpectedly accepted legacy snapshot: %v", err)
	}
	if _, err := restarted.ValidateWithCanonicalScope(recoveredGrant, recovered.Scope, plan); err != nil {
		t.Fatalf("canonical legacy validation failed: %v", err)
	}
	resolved, resolvedScope, err := reopened.ResolveModelRequestAlias("legacy-epoch", "provider/v1", "legacy-connection", "legacy-response", "opaque")
	if err != nil || resolved.Scope.ToolSnapshotID != "" || resolvedScope.ToolSnapshotID != scope.ToolSnapshotID {
		t.Fatalf("resolved grant=%+v scope=%+v err=%v", resolved, resolvedScope, err)
	}
}

func TestRetireModelRequestSurfaceCannotWriteFinishedState(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	_, scope, _, grant := modelRequestSurfaceFixture(t, coordinator, "retire-finished")
	if _, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
		Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-retire-finished",
		Aliases: map[string]InvocationGrant{"opaque_alias": grant}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.RetireModelRequestSurface("epoch-retire-finished", modelRequestSurfaceFinished, time.Now().UTC()); err == nil || err.Error() != "model_request_surface_retire_state_invalid" {
		t.Fatalf("generic retire accepted finished state: %v", err)
	}
	// A failed attempted settlement must leave a prepared surface prepared, not
	// silently mutate its durable state.
	if err := coordinator.BindModelRequestResponse("epoch-retire-finished", "provider/v1", "connection-1", "response-retire-finished", time.Now().UTC()); err != nil {
		t.Fatalf("rejected generic finish changed prepared state: %v", err)
	}
}

func TestRecoverBoundModelRequestSurfaceRejectsPreparedSupersededAndCancelled(t *testing.T) {
	for _, terminal := range []ModelRequestSurfaceState{modelRequestSurfacePrepared, modelRequestSurfaceSuperseded, modelRequestSurfaceCancelled} {
		t.Run(string(terminal), func(t *testing.T) {
			coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = coordinator.Close() })
			_, scope, _, grant := modelRequestSurfaceFixture(t, coordinator, "request-recovery-"+string(terminal))
			epoch := "epoch-" + string(terminal)
			if _, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
				Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: epoch,
				Aliases: map[string]InvocationGrant{"opaque_alias": grant}, Now: time.Now().UTC(),
			}); err != nil {
				t.Fatal(err)
			}
			if terminal != modelRequestSurfacePrepared {
				if err := coordinator.RetireModelRequestSurface(epoch, terminal, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := coordinator.RecoverBoundModelRequestSurface(ModelRequestSurfaceRecovery{
				TenantID: "tenant", Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: epoch,
			}); err == nil || err.Error() != "stale_surface" {
				t.Fatalf("recovered %s surface: %v", terminal, err)
			}
		})
	}
}

func TestPreparedModelRequestSurfaceCannotResolveBeforeResponseBinding(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	_, scope, _, grant := modelRequestSurfaceFixture(t, coordinator, "request-prepared")
	surface, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
		Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-prepared",
		Aliases: map[string]InvocationGrant{"skill_prepared": grant}, Now: time.Now().UTC(),
	})
	if err != nil || surface.State != modelRequestSurfacePrepared {
		t.Fatalf("prepared surface=%+v err=%v", surface, err)
	}
	if _, _, err := coordinator.ResolveModelRequestAlias("epoch-prepared", "provider/v1", "connection-1", "response-prepared", "skill_prepared"); err == nil || err.Error() != "stale_surface" {
		t.Fatalf("prepared alias resolved: %v", err)
	}
	if err := coordinator.RetireModelRequestSurface("epoch-prepared", modelRequestSurfaceCancelled, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-prepared", "provider/v1", "connection-1", "response-prepared", time.Now().UTC()); err == nil || err.Error() != "model_request_surface_not_active" {
		t.Fatalf("retired prepared surface bound response: %v", err)
	}
}

func TestFinishModelRequestSurfaceRetiresOnlyOnePresentation(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	plan, scope, issuer, grant := modelRequestSurfaceFixture(t, coordinator, "request-finished")
	if _, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
		Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-finished",
		Aliases: map[string]InvocationGrant{"skill_finished": grant}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-finished", "provider/v1", "connection-1", "response-finished", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.FinishModelRequestSurface("epoch-finished", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.ResolveModelRequestAlias("epoch-finished", "provider/v1", "connection-1", "response-finished", "skill_finished"); err == nil || err.Error() != "stale_surface" {
		t.Fatalf("finished presentation resolved: %v", err)
	}
	if err := coordinator.Routes.IsCurrent(scope); err != nil {
		t.Fatalf("finishing one presentation cancelled current route: %v", err)
	}
	if _, err := issuer.Validate(grant, scope, plan); err != nil {
		t.Fatalf("finishing one presentation revoked reusable authority: %v", err)
	}
	if err := coordinator.FinishModelRequestSurface("epoch-finished", time.Now().UTC()); err != nil {
		t.Fatalf("finished surface was not idempotently retired: %v", err)
	}
}

func TestFinishModelRequestSurfaceRejectsAnyNonActiveTerminalState(t *testing.T) {
	for _, terminal := range []ModelRequestSurfaceState{modelRequestSurfacePrepared, modelRequestSurfaceSuperseded, modelRequestSurfaceCancelled} {
		t.Run(string(terminal), func(t *testing.T) {
			coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = coordinator.Close() })
			_, scope, _, grant := modelRequestSurfaceFixture(t, coordinator, "finish-non-active-"+string(terminal))
			epoch := "epoch-finish-" + string(terminal)
			if _, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
				Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: epoch,
				Aliases: map[string]InvocationGrant{"opaque_alias": grant}, Now: time.Now().UTC(),
			}); err != nil {
				t.Fatal(err)
			}
			if terminal != modelRequestSurfacePrepared {
				if err := coordinator.RetireModelRequestSurface(epoch, terminal, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
			}
			if err := coordinator.FinishModelRequestSurface(epoch, time.Now().UTC()); err == nil || err.Error() != "model_request_surface_not_active" {
				t.Fatalf("finished %s surface: %v", terminal, err)
			}
		})
	}
}

func TestModelRequestSurfaceReplacementRetiresPreparedPredecessor(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	_, scope, _, grant := modelRequestSurfaceFixture(t, coordinator, "request-prepared-retry")
	if _, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
		Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-prepared-a",
		Aliases: map[string]InvocationGrant{"skill_a": grant}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ReplaceModelRequestSurface(ModelRequestSurfaceReplace{PreviousEpoch: "epoch-prepared-a", Successor: ModelRequestSurfacePublish{
		Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-prepared-b",
		Aliases: map[string]InvocationGrant{"skill_b": grant}, Now: time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-prepared-a", "provider/v1", "connection-1", "response-a", time.Now().UTC()); err == nil || err.Error() != "model_request_surface_not_active" {
		t.Fatalf("retired predecessor was response-bound: %v", err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-prepared-b", "provider/v1", "connection-1", "response-b", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.ResolveModelRequestAlias("epoch-prepared-b", "provider/v1", "connection-1", "response-b", "skill_b"); err != nil {
		t.Fatalf("successor did not reuse prepared grant: %v", err)
	}
}

func TestModelRequestSurfaceSupersessionRevokesParentGrant(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	plan, scope, issuer, grant := modelRequestSurfaceFixture(t, coordinator, "request-supersede")
	if _, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
		Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-parent",
		Aliases: map[string]InvocationGrant{"skill_parent": grant}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-parent", "provider/v1", "connection-1", "response-parent", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	child := revisedRouteStatePlan(t, plan, scope.RootTaskID, "turn-child")
	childScope := scope
	childScope.PlanID, childScope.TurnID = child.ID, "turn-child"
	current, err := coordinator.Routes.CurrentRevision(scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: routeRevisionPublish(child, childScope, &current), Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.ResolveModelRequestAlias("epoch-parent", "provider/v1", "connection-1", "response-parent", "skill_parent"); err == nil || err.Error() != "stale_surface" {
		t.Fatalf("parent request survived child publish: %v", err)
	}
	if _, err := issuer.ValidateAndConsume(grant, scope, plan); err == nil || !strings.Contains(err.Error(), "invocation_grant_revoked") {
		t.Fatalf("superseded parent grant result=%v", err)
	}
}

func TestCancelRouteSurfaceAtomicallyRetiresAliasAndRevokesGrant(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	plan, scope, issuer, grant := modelRequestSurfaceFixture(t, coordinator, "request-cancel")
	if _, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
		Scope: scope, Protocol: "provider/v1", ConnectionID: "connection-1", Epoch: "epoch-cancel",
		Aliases: map[string]InvocationGrant{"skill_cancel": grant}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.BindModelRequestResponse("epoch-cancel", "provider/v1", "connection-1", "response-cancel", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.CancelRouteSurface(scope, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.ResolveModelRequestAlias("epoch-cancel", "provider/v1", "connection-1", "response-cancel", "skill_cancel"); err == nil || err.Error() != "stale_surface" {
		t.Fatalf("cancelled request resolved: %v", err)
	}
	if _, err := issuer.ValidateAndConsume(grant, scope, plan); err == nil || !strings.Contains(err.Error(), "invocation_grant_revoked") {
		t.Fatalf("cancelled grant result=%v", err)
	}
	if _, _, err := coordinator.MaterializeReadySurface(scope, issuer, time.Minute, nil, map[string]bool{grant.SelectionID: true}, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "route_revision_cancelled") {
		t.Fatalf("cancelled route rematerialized: %v", err)
	}
}

func modelRequestSurfaceFixture(t *testing.T, coordinator *SQLiteSemanticExecutionCoordinator, root string) (ToolPlan, InvocationScope, *InvocationIssuer, InvocationGrant) {
	t.Helper()
	registry := semanticRegistry(t)
	provider := semanticProvider("request_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: root, SessionID: "session", TurnID: "turn",
		Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}),
		Needs:    []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("r", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	_, grants, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err != nil || len(grants) != 1 {
		t.Fatalf("publish grants=%+v err=%v", grants, err)
	}
	return plan, scope, issuer, grants[0]
}
