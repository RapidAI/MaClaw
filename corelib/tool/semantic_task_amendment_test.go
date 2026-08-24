package tool

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func amendmentRouteFixture(t *testing.T) (*SQLiteSemanticExecutionCoordinator, *InvocationIssuer, ToolPlan, InvocationScope, string) {
	t.Helper()
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("a", 32)), coordinator.Grants)
	if err != nil {
		_ = coordinator.Close()
		t.Fatal(err)
	}
	registry := semanticRegistry(t)
	provider := semanticProvider("amend_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "amend-root", SessionID: "amend-session", TurnID: "parent",
		Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}),
		Needs:    []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil {
		_ = coordinator.Close()
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "amend-session", TurnID: "parent", PrincipalID: "amend-principal"}
	if _, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, TenantID: "amend-tenant", Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()}); err != nil {
		_ = coordinator.Close()
		t.Fatal(err)
	}
	return coordinator, issuer, plan, scope, "amend-tenant"
}

func amendmentChildPlan(t *testing.T, parent ToolPlan, scope InvocationScope) (ToolPlan, InvocationScope) {
	t.Helper()
	child := parent
	child.ID = parent.ID + ":amended"
	child.SnapshotDigest = parent.SnapshotDigest + ":amended"
	childScope := scope
	childScope.PlanID = child.ID
	childScope.TurnID = "amended"
	return child, childScope
}

func TestTaskAmendmentCommandConsumesOnlyWithChildSurfacePublication(t *testing.T) {
	coordinator, issuer, parentPlan, parentScope, tenantID := amendmentRouteFixture(t)
	defer coordinator.Close()
	parent, err := coordinator.Routes.CurrentRevision(parentScope)
	if err != nil {
		t.Fatal(err)
	}
	command, err := coordinator.IssueTaskAmendmentCommand(tenantID, parentScope.PrincipalID, parentScope.SessionID, parentScope.RootTaskID, "sha256:amendment", time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ValidateTaskAmendmentCommand(command.ID, tenantID, parentScope.PrincipalID, parentScope.SessionID, parentScope.RootTaskID, time.Now().UTC()); err != nil {
		t.Fatalf("validate before publish: %v", err)
	}
	childPlan, childScope := amendmentChildPlan(t, parentPlan, parentScope)
	amendment := &RouteAmendmentRef{CommandID: command.ID, Digest: command.Digest, ParentRevision: command.ParentRevision, ParentFencingToken: command.ParentFencingToken}
	state, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: childScope, Plan: childPlan, ExpectedParent: &parent, SnapshotDigest: childPlan.SnapshotDigest, Amendment: amendment}, TenantID: tenantID, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if !sameOptionalRouteAmendmentRef(state.Amendment, amendment) {
		t.Fatalf("published amendment=%#v want %#v", state.Amendment, amendment)
	}
	if _, err := coordinator.ValidateTaskAmendmentCommand(command.ID, tenantID, parentScope.PrincipalID, parentScope.SessionID, parentScope.RootTaskID, time.Now().UTC()); err == nil || err.Error() != "task_amendment_command_not_active" {
		t.Fatalf("command must be consumed after child publish: %v", err)
	}
	if _, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: childScope, Plan: childPlan, ExpectedParent: &parent, SnapshotDigest: childPlan.SnapshotDigest, Amendment: amendment}, TenantID: tenantID, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()}); err != nil {
		t.Fatalf("same amendment child publication must be idempotent: %v", err)
	}
}

func TestTaskAmendmentCommandSurvivesCASConflictWithoutChildPublication(t *testing.T) {
	coordinator, issuer, parentPlan, parentScope, tenantID := amendmentRouteFixture(t)
	defer coordinator.Close()
	parent, err := coordinator.Routes.CurrentRevision(parentScope)
	if err != nil {
		t.Fatal(err)
	}
	command, err := coordinator.IssueTaskAmendmentCommand(tenantID, parentScope.PrincipalID, parentScope.SessionID, parentScope.RootTaskID, "sha256:amendment", time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	competingPlan, competingScope := amendmentChildPlan(t, parentPlan, parentScope)
	competingPlan.ID, competingScope.PlanID, competingScope.TurnID = parentPlan.ID+":competing", parentPlan.ID+":competing", "competing"
	competingPlan.SnapshotDigest = parentPlan.SnapshotDigest + ":competing"
	if _, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: competingScope, Plan: competingPlan, ExpectedParent: &parent, SnapshotDigest: competingPlan.SnapshotDigest}, TenantID: tenantID, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	childPlan, childScope := amendmentChildPlan(t, parentPlan, parentScope)
	amendment := &RouteAmendmentRef{CommandID: command.ID, Digest: command.Digest, ParentRevision: command.ParentRevision, ParentFencingToken: command.ParentFencingToken}
	if _, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: childScope, Plan: childPlan, ExpectedParent: &parent, SnapshotDigest: childPlan.SnapshotDigest, Amendment: amendment}, TenantID: tenantID, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()}); err == nil || err.Error() != "route_revision_conflict" {
		t.Fatalf("CAS conflict err=%v", err)
	}
	var state string
	if err := coordinator.db.QueryRow(`SELECT state FROM semantic_task_amendment_commands WHERE command_id=?`, command.ID).Scan(&state); err != nil || state != "active" {
		t.Fatalf("failed child publish must not consume amendment: state=%q err=%v", state, err)
	}
	var count int
	if err := coordinator.db.QueryRow(`SELECT COUNT(*) FROM semantic_route_amendments WHERE command_id=?`, command.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed child publish wrote amendment record count=%d err=%v", count, err)
	}
}

func TestTaskAmendmentCommandRejectsStaleRoute(t *testing.T) {
	coordinator, issuer, parentPlan, parentScope, tenantID := amendmentRouteFixture(t)
	defer coordinator.Close()
	command, err := coordinator.IssueTaskAmendmentCommand(tenantID, parentScope.PrincipalID, parentScope.SessionID, parentScope.RootTaskID, "sha256:amendment", time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	parent, err := coordinator.Routes.CurrentRevision(parentScope)
	if err != nil {
		t.Fatal(err)
	}
	childPlan, childScope := amendmentChildPlan(t, parentPlan, parentScope)
	if _, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: childScope, Plan: childPlan, ExpectedParent: &parent, SnapshotDigest: childPlan.SnapshotDigest}, TenantID: tenantID, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ValidateTaskAmendmentCommand(command.ID, tenantID, parentScope.PrincipalID, parentScope.SessionID, parentScope.RootTaskID, time.Now().UTC()); err == nil || err.Error() != "task_amendment_command_superseded" {
		t.Fatalf("stale command err=%v", err)
	}
}

func TestPrepareTaskRefinementConsumesHandleAndRetriesOnlySameActiveAmendment(t *testing.T) {
	coordinator, _, _, parentScope, tenantID := amendmentRouteFixture(t)
	defer coordinator.Close()
	handle, err := coordinator.IssueTaskContinuationHandle(tenantID, parentScope.PrincipalID, parentScope.SessionID, parentScope.RootTaskID, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	continued, command, err := coordinator.PrepareTaskRefinement(handle.ID, tenantID, parentScope.PrincipalID, parentScope.SessionID, "sha256:amendment", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if continued.ID != handle.ID || command.SourceContinuationHandle != handle.ID || command.ID == "" {
		t.Fatalf("prepared refinement continuation=%#v command=%#v", continued, command)
	}
	if _, err := coordinator.ConsumeTaskContinuationHandle(handle.ID, tenantID, parentScope.PrincipalID, parentScope.SessionID, time.Now().UTC()); err == nil || err.Error() != "task_continuation_handle_not_active" {
		t.Fatalf("prepare must consume continuation handle: %v", err)
	}
	_, retry, err := coordinator.PrepareTaskRefinement(handle.ID, tenantID, parentScope.PrincipalID, parentScope.SessionID, "sha256:amendment", time.Now().UTC())
	if err != nil || retry.ID != command.ID {
		t.Fatalf("same refinement retry command=%#v err=%v", retry, err)
	}
	if _, _, err := coordinator.PrepareTaskRefinement(handle.ID, tenantID, parentScope.PrincipalID, parentScope.SessionID, "sha256:other", time.Now().UTC()); err == nil || err.Error() != "task_continuation_handle_not_active" {
		t.Fatalf("different amendment must not reuse selector err=%v", err)
	}
}
