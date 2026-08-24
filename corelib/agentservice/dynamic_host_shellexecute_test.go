package agentservice

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostShellExecutor struct {
	command   string
	timeout   time.Duration
	principal Principal
	result    string
	err       error
}

func (f *fakeHostShellExecutor) ExecuteReviewedHostShell(_ context.Context, principal Principal, command string, timeout time.Duration) (string, error) {
	f.principal = principal
	f.command = command
	f.timeout = timeout
	return f.result, f.err
}

func TestReviewedHostShellExecutesWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeHostShellExecutor{result: "hi"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{Shell: executor})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "shell", Capability: CapabilityShellExecute, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("shell plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].AdapterName == "bash" {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host shell must use the local mutation receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"command":"echo hi"}`)
	if !result.Succeeded || result.Result != "hi" || result.Unknown {
		t.Fatalf("shell result=%#v", result)
	}
	if executor.command != "echo hi" || executor.timeout != reviewedHostShellDefaultTimeout || executor.principal.UserID != principal.UserID {
		t.Fatalf("executor=%#v", executor)
	}
	timed := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"command":"echo hi","timeout_seconds":5}`)
	if !timed.Succeeded || executor.timeout != 5*time.Second {
		t.Fatalf("timeout result=%#v executor=%#v", timed, executor)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"command":"echo hi","project_path":"/tmp"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("project_path must fail closed, result=%#v", rejected)
	}

	filePlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-file", TurnID: "turn-file", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "write", Capability: CapabilityFileWrite, Required: true}},
	})
	if err != nil || len(filePlan.Selections) != 0 {
		t.Fatalf("fs.write.local must not be satisfied by shell, plan=%#v err=%v", filePlan, err)
	}
}

func TestReviewedHostShellIsAbsentWithoutExecutor(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "shell", Capability: CapabilityShellExecute, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("shell without executor must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostShellRejectsWorkingDirAndSoupFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostShellProvider(&fakeHostShellExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityShellExecute || provider.AdapterName == "bash" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["command"]; !ok {
		t.Fatalf("shell schema missing command: %#v", props)
	}
	if _, ok := props["timeout_seconds"]; !ok || len(props) != 2 {
		t.Fatalf("shell schema=%#v", props)
	}
	for _, key := range []string{"project_path", "working_dir", "channel", "destination", "group_name", "cwd"} {
		if _, ok := props[key]; ok {
			t.Fatalf("shell schema leaked %s", key)
		}
	}
}

func TestReviewedHostOwnedServicesWiresShellOnlyWhenTrustedLocalBash(t *testing.T) {
	principal := Principal{TenantID: "t", UserID: "u"}
	workspace := t.TempDir()
	denied := (&coreAgentCallbacks{principal: principal, workspace: workspace}).reviewedHostOwnedServices()
	if denied.Shell != nil {
		t.Fatal("shell must stay unpublished without trusted local bash")
	}
	allowed := (&coreAgentCallbacks{
		principal: principal, workspace: workspace,
		allowLocalBash: true, localBashTrustedSingleUser: true,
		localBashTenantID: "t", localBashUserID: "u",
	}).reviewedHostOwnedServices()
	if allowed.Shell == nil || allowed.FileWrite == nil {
		t.Fatalf("trusted local bash must publish shell, services=%#v", allowed)
	}
}

func TestReviewedHostShellRunsInWorkspace(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker.txt")
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal: principal, workspace: dir,
		allowLocalBash: true, localBashTrustedSingleUser: true,
		localBashTenantID: "t", localBashUserID: "u",
	}
	command := "echo hi>marker.txt"
	if runtime.GOOS != "windows" {
		command = "echo hi > marker.txt"
	}
	out, err := cb.ExecuteReviewedHostShell(context.Background(), principal, command, 5*time.Second)
	if err != nil {
		t.Fatalf("shell=%q err=%v", out, err)
	}
	data, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(data), "hi") {
		t.Fatalf("workspace command did not run in cwd, data=%q err=%v out=%q", data, err, out)
	}
	if _, err := cb.ExecuteReviewedHostShell(context.Background(), principal, "ssh host", time.Second); err == nil {
		t.Fatal("raw ssh through shell must fail closed")
	}
	escaped := &coreAgentCallbacks{principal: principal, workspace: dir}
	if _, err := escaped.ExecuteReviewedHostShell(context.Background(), principal, command, time.Second); err == nil {
		t.Fatal("untrusted local bash must not execute")
	}
}

// TestReviewedHostShellAppliesEveryLocalShellGuard keeps this host's managed
// shell on the same guard set as its GUI counterpart. A managed local shell
// grant must not carry remote execution or browser control, both of which have
// their own trusted adapters, so a shell selection that reached them would give
// the model more than the plan selected.
//
// Each case asserts the guard's own rejection text, so a private lookalike
// check in place of the shared guard fails the test.
func TestReviewedHostShellAppliesEveryLocalShellGuard(t *testing.T) {
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal: principal, workspace: t.TempDir(),
		allowLocalBash: true, localBashTrustedSingleUser: true,
		localBashTenantID: "t", localBashUserID: "u",
	}
	for _, tc := range []struct {
		name    string
		command string
		guard   func(string) (string, bool)
	}{
		{"remote host hop", "ssh user@host uptime", coretool.RejectRawSSHCommand},
		{"whole browser process tree", "taskkill /im chrome.exe", coretool.RejectBroadBrowserKillCommand},
		{"authenticated side effect", `curl -X POST https://example.com/publish -H "cookie: a=b"`, coretool.RejectBrowserSideEffectHTTPCommand},
		{"second browser control plane", "npx playwright screenshot https://example.com out.png", coretool.RejectShellBrowserAutomationCommand},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, tripped := tc.guard(tc.command)
			if !tripped {
				t.Fatalf("probe command no longer trips its own guard: %q", tc.command)
			}
			out, err := cb.ExecuteReviewedHostShell(context.Background(), principal, tc.command, time.Second)
			if err == nil || err.Error() != want {
				t.Fatalf("out=%q err=%v, want rejection %q", out, err, want)
			}
		})
	}
}
