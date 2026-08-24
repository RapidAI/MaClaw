package agentservice

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostRepoInspector struct {
	principal Principal
	result    string
	err       error
}

func (f *fakeHostRepoInspector) InspectReviewedHostRepo(_ context.Context, principal Principal) (string, error) {
	f.principal = principal
	return f.result, f.err
}

func TestReviewedHostRepoInspectExecutesAndRejectsLookupMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	inspector := &fakeHostRepoInspector{result: "git status:\n## main\n"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{RepoInspect: inspector})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "git", Capability: CapabilityRepoInspect, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("repo inspect plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityRepoInspect {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !result.Succeeded || result.Result != inspector.result {
		t.Fatalf("repo inspect result=%#v", result)
	}
	if inspector.principal.TenantID != principal.TenantID || inspector.principal.UserID != principal.UserID {
		t.Fatalf("inspector=%#v", inspector)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"project_path":"/tmp","staged":true}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("project_path/staged must fail closed, result=%#v", rejected)
	}

	lookupPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-lookup", TurnID: "turn-lookup", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "lookup", Capability: CapabilityInformationLookup, Required: true,
			Qualifiers: map[string]string{QualifierInformationScope: InformationScopeReference},
		}},
	})
	if err != nil || len(lookupPlan.Selections) != 0 {
		t.Fatalf("lookup must not be satisfied by host repo inspect, plan=%#v err=%v", lookupPlan, err)
	}
}

func TestReviewedHostRepoInspectIsAbsentWithoutInspector(t *testing.T) {
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
		Needs: []coretool.CapabilityNeed{{ID: "git", Capability: CapabilityRepoInspect, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("repo inspect without inspector must stay unmet, plan=%#v err=%v", plan, err)
	}
	clockPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-clock", TurnID: "turn-clock", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "clock", Capability: CapabilityCurrentTime, Required: true}},
	})
	if err != nil || len(clockPlan.Selections) != 1 {
		t.Fatalf("clock must still plan without a repo inspector, plan=%#v err=%v", clockPlan, err)
	}
}

func TestProjectReviewedHostRepoInspectRejectsPathAndChannelFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostRepoInspectProvider(&fakeHostRepoInspector{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityRepoInspect {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if len(props) != 0 {
		t.Fatalf("repo inspect schema=%#v", props)
	}
	for _, key := range []string{"project_path", "path", "staged", "channel", "destination", "message"} {
		if _, ok := props[key]; ok {
			t.Fatalf("repo inspect schema leaked %s", key)
		}
	}
}

func TestReviewedHostRepoInspectReadsWorkspaceGitWithoutMutating(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for host repo inspect")
	}
	dir := t.TempDir()
	runReviewedHostTestGit(t, dir, "init")
	runReviewedHostTestGit(t, dir, "config", "user.email", "repo-inspect@example.invalid")
	runReviewedHostTestGit(t, dir, "config", "user.name", "Repo Inspect")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runReviewedHostTestGit(t, dir, "add", "tracked.txt")
	runReviewedHostTestGit(t, dir, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: dir}
	out, err := cb.InspectReviewedHostRepo(context.Background(), principal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "git status:") || !strings.Contains(out, "tracked.txt") || !strings.Contains(out, "new.txt") {
		t.Fatalf("inspect=%q", out)
	}
	head, err := runReviewedHostGit(context.Background(), dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	after, err := runReviewedHostGit(context.Background(), dir, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(after) != strings.TrimSpace(head) {
		t.Fatalf("inspect mutated HEAD: before=%q after=%q err=%v", head, after, err)
	}
	plain := t.TempDir()
	notRepo, err := inspectReviewedHostRepo(context.Background(), plain)
	if err != nil || !strings.Contains(notRepo, "not a git repository") {
		t.Fatalf("non-repo=%q err=%v", notRepo, err)
	}
}

func runReviewedHostTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
