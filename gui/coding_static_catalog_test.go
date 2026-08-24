package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func testCodingStaticEnvelope(workspace string, posture codingRequestKind) codingStaticExecutionEnvelope {
	return codingStaticExecutionEnvelope{
		Identity: &trustedCodingInvocationIdentity{
			TenantID: "tenant", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn",
		},
		Workspace: codingStaticWorkspaceBinding{WorkspaceHandle: workspace, HostKind: "local"},
		Posture:   posture,
		Role:      codingRoleWorker,
	}
}

func TestCodingStaticShadowPlanUsesDistinctHostWorkspaceBindings(t *testing.T) {
	now := time.Date(2026, time.August, 22, 0, 0, 0, 0, time.UTC)
	left, err := prepareCodingStaticShadowPlan(testCodingStaticEnvelope("workspace-a", codingRequestInquiry), nil, tool.PlanningBudget{}, now)
	if err != nil {
		t.Fatal(err)
	}
	right, err := prepareCodingStaticShadowPlan(testCodingStaticEnvelope("workspace-b", codingRequestInquiry), nil, tool.PlanningBudget{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(left.Plan.Unmet) != 0 || len(right.Plan.Unmet) != 0 {
		t.Fatalf("complete local workspaces must plan, left=%#v right=%#v", left.Plan.Unmet, right.Plan.Unmet)
	}
	if len(left.Plan.Selections) != 2 || len(right.Plan.Selections) != 2 {
		t.Fatalf("read-only shadow selections left=%#v right=%#v", left.Plan.Selections, right.Plan.Selections)
	}
	for i := range left.Plan.Selections {
		if left.Plan.Selections[i].Provider.ProviderID == right.Plan.Selections[i].Provider.ProviderID {
			t.Fatalf("workspace binding reused across A/B: %q", left.Plan.Selections[i].Provider.ProviderID)
		}
		if !strings.Contains(left.Plan.Selections[i].Provider.ProviderID, "workspace-a") || !strings.Contains(right.Plan.Selections[i].Provider.ProviderID, "workspace-b") {
			t.Fatalf("unexpected bindings left=%q right=%q", left.Plan.Selections[i].Provider.ProviderID, right.Plan.Selections[i].Provider.ProviderID)
		}
	}
}

func TestCodingStaticShadowPlanFailsClosedWhenWorkspaceBindingIsMissing(t *testing.T) {
	envelope := testCodingStaticEnvelope("", codingRequestInquiry)
	prepared, err := prepareCodingStaticShadowPlan(envelope, nil, tool.PlanningBudget{}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Catalog.Coverage.State != tool.CatalogCoverageIncomplete || prepared.Catalog.Coverage.ReasonCode != tool.CatalogCoverageReasonIncomplete {
		t.Fatalf("missing workspace coverage=%#v, want catalog_incomplete", prepared.Catalog.Coverage)
	}
	if len(prepared.Plan.Selections) != 0 || len(prepared.Plan.Unmet) != 2 {
		t.Fatalf("missing workspace plan selections=%#v unmet=%#v", prepared.Plan.Selections, prepared.Plan.Unmet)
	}
	for _, unmet := range prepared.Plan.Unmet {
		if unmet.ReasonCode != tool.CatalogCoverageReasonIncomplete {
			t.Fatalf("missing workspace unmet=%#v, want catalog_incomplete", unmet)
		}
	}
}

func TestCodingStaticSubagentRuntimeBridgeRecordsCatalogIncompleteWithoutWorkspace(t *testing.T) {
	subagent := &CodingSubAgent{dynamicInvocationIdentity: testCodingStaticEnvelope("irrelevant", codingRequestInquiry).Identity}
	prepared, err := prepareCodingStaticShadowPlanForSubagent(subagent, codingRequestInquiry, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if prepared == nil || prepared.Catalog.Coverage.State != tool.CatalogCoverageIncomplete {
		t.Fatalf("missing workspace bridge preparation=%#v", prepared)
	}
	if len(prepared.Plan.Selections) != 0 || len(prepared.Plan.Unmet) != 2 {
		t.Fatalf("missing workspace runtime bridge plan=%#v", prepared.Plan)
	}
	for _, unmet := range prepared.Plan.Unmet {
		if unmet.ReasonCode != tool.CatalogCoverageReasonIncomplete {
			t.Fatalf("runtime bridge unmet=%#v", unmet)
		}
	}
	if subagent.staticWorkspaceBinding.complete() {
		t.Fatalf("runtime bridge fabricated a workspace binding: %#v", subagent.staticWorkspaceBinding)
	}
}

func TestCodingStaticSubagentRuntimeBridgeRequiresVerifiedIdentity(t *testing.T) {
	prepared, err := prepareCodingStaticShadowPlanForSubagent(&CodingSubAgent{}, codingRequestInquiry, time.Now().UTC())
	if err == nil || prepared != nil {
		t.Fatalf("unverified runtime bridge preparation=%#v err=%v", prepared, err)
	}
}

func TestCodingStaticShadowPlanPostureNeverAddsWriteBuildOrShell(t *testing.T) {
	for _, posture := range []codingRequestKind{codingRequestInquiry, codingRequestOperational, codingRequestImplementation} {
		t.Run(string(posture), func(t *testing.T) {
			prepared, err := prepareCodingStaticShadowPlan(testCodingStaticEnvelope("workspace", posture), nil, tool.PlanningBudget{}, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			for _, selection := range prepared.Plan.Selections {
				switch selection.FitProof.MatchedCapability {
				case tool.CapabilityFSWriteLocal, tool.CapabilityBuildVerifyLocal, tool.CapabilityShellExecuteLocal:
					t.Fatalf("%s posture selected out-of-scope capability %s", posture, selection.FitProof.MatchedCapability)
				}
			}
		})
	}
}

func TestCodingStaticShadowPlanDoesNotCreateModelSurfaceOrGrant(t *testing.T) {
	prepared, err := prepareCodingStaticShadowPlan(testCodingStaticEnvelope("workspace", codingRequestImplementation), nil, tool.PlanningBudget{}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Plan.ID == "" || prepared.Catalog.Generation == 0 {
		t.Fatalf("shadow plan was not created: %#v", prepared)
	}
	for _, provider := range prepared.Catalog.Providers {
		if strings.HasPrefix(provider.AdapterName, "skill_") || strings.HasPrefix(provider.AdapterName, "mcp_") {
			t.Fatalf("dynamic provider leaked into static shadow catalog: %#v", provider)
		}
	}
}

func TestCodingStaticWorkspaceExecutorUsesOnlyTheBoundWorkspace(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeCodingTaskRelationService)
	owner := projectSessionOwnerID("C:/workspace/a")
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceA, "only-a.txt"), []byte("workspace A"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceB, "only-b.txt"), []byte("workspace B"), 0o600); err != nil {
		t.Fatal(err)
	}
	token := app.beginDesktopCodingTaskIngressWithWorkspace(owner, workspaceA)
	_, _, _, binding, ok := app.nextDesktopCodingTaskRelationWithWorkspace(token, owner)
	if !ok || !binding.complete() {
		t.Fatalf("workspace binding=%#v ok=%v", binding, ok)
	}
	prepared, err := prepareCodingStaticShadowPlan(codingStaticExecutionEnvelope{
		Identity:  testCodingStaticEnvelope("irrelevant", codingRequestInquiry).Identity,
		Workspace: binding,
		Posture:   codingRequestInquiry,
		Role:      codingRoleWorker,
	}, nil, tool.PlanningBudget{}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	var read tool.PlannedSelection
	for _, selection := range prepared.Plan.Selections {
		if selection.AdapterName == codingStaticReadAdapter {
			read = selection
			break
		}
	}
	if read.ID == "" {
		t.Fatalf("read selection missing: %#v", prepared.Plan.Selections)
	}
	executor := newCodingStaticWorkspaceExecutor(app, owner, binding)
	got, err := executor.ExecuteReadOnlySelection(read, `{"path":"only-a.txt"}`)
	if err != nil || !strings.Contains(got, "workspace A") {
		t.Fatalf("bound read output=%q err=%v", got, err)
	}
	if _, err := executor.ExecuteReadOnlySelection(read, `{"path":"../`+filepath.Base(workspaceB)+`/only-b.txt"}`); err == nil {
		t.Fatal("bound executor escaped its workspace")
	}
	if _, err := executor.ExecuteReadOnlySelection(read, `{"path":"only-a.txt","provider":"forged"}`); err == nil {
		t.Fatal("bound executor accepted reserved provider selector")
	}
}

func TestCodingStaticWorkspaceExecutorRejectsBindingOrWorkspaceDrift(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeCodingTaskRelationService)
	ownerA := projectSessionOwnerID("C:/workspace/a")
	ownerB := projectSessionOwnerID("C:/workspace/b")
	workspace := t.TempDir()
	token := app.beginDesktopCodingTaskIngressWithWorkspace(ownerA, workspace)
	_, _, _, binding, ok := app.nextDesktopCodingTaskRelationWithWorkspace(token, ownerA)
	if !ok {
		t.Fatal("workspace binding was not issued")
	}
	prepared, err := prepareCodingStaticShadowPlan(codingStaticExecutionEnvelope{Identity: testCodingStaticEnvelope("irrelevant", codingRequestInquiry).Identity, Workspace: binding, Posture: codingRequestInquiry, Role: codingRoleWorker}, nil, tool.PlanningBudget{}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	read := prepared.Plan.Selections[0]
	if _, err := newCodingStaticWorkspaceExecutor(app, ownerB, binding).ExecuteReadOnlySelection(read, `{}`); err == nil {
		t.Fatal("workspace binding crossed owners at execution")
	}
	if err := os.RemoveAll(workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := newCodingStaticWorkspaceExecutor(app, ownerA, binding).ExecuteReadOnlySelection(read, `{}`); err == nil {
		t.Fatal("removed workspace remained executable")
	}
}

func TestDesktopCodingWorkspaceBindingIsRevokedWithNewTaskFence(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeCodingTaskRelationService)
	owner := projectSessionOwnerID("C:/workspace/a")
	workspace := t.TempDir()
	token := app.beginDesktopCodingTaskIngressWithWorkspace(owner, workspace)
	_, _, _, binding, ok := app.nextDesktopCodingTaskRelationWithWorkspace(token, owner)
	if !ok || !binding.complete() {
		t.Fatalf("workspace binding=%#v ok=%v", binding, ok)
	}
	app.fenceDesktopCodingTaskRelation(owner, true)
	if _, ok := app.resolveDesktopCodingStaticWorkspace(owner, binding); ok {
		t.Fatal("new-task/cancel fence left old workspace binding resolvable")
	}
}
