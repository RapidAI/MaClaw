package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostBuildVerifier struct {
	task      string
	target    string
	principal Principal
	calls     int
	result    string
	err       error
}

func (f *fakeHostBuildVerifier) RunReviewedHostBuildVerify(_ context.Context, principal Principal, task, target string) (string, error) {
	f.principal, f.task, f.target = principal, task, target
	f.calls++
	return f.result, f.err
}

// build.verify.local exists so a plan can be granted verification without
// being granted shell.execute.local. If its schema ever gains a command,
// argument list or shell knob, the narrowed grant has become the wide one and
// there is no longer any reason for the two to be separate capabilities.
func TestReviewedHostBuildVerifySchemaCarriesNoCommandLine(t *testing.T) {
	schema := reviewedHostBuildVerifyInvocationSchema()
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok || len(properties) != 2 {
		t.Fatalf("properties=%#v", schema["properties"])
	}
	for _, forbidden := range []string{
		"command", "args", "argv", "shell", "env", "cwd", "working_dir",
		"project_path", "timeout_seconds", "script", "channel", "destination",
	} {
		if _, present := properties[forbidden]; present {
			t.Fatalf("schema exposes %q, which returns the command line to the model", forbidden)
		}
	}
	task, _ := properties["task"].(map[string]interface{})
	enum, ok := task["enum"].([]string)
	if !ok || len(enum) != 4 {
		t.Fatalf("task enum=%#v", task["enum"])
	}
	// Both hosts must offer the same closed set; a plan cannot mean different
	// things depending on which one served it.
	for i, want := range coretool.BuildVerifyTasks() {
		if enum[i] != want {
			t.Fatalf("enum=%v does not match the shared contract %v", enum, coretool.BuildVerifyTasks())
		}
	}
}

func TestReviewedHostBuildVerifyArgsRejectAnythingOutsideTheClosedSet(t *testing.T) {
	if task, target, err := reviewedHostBuildVerifyArgsAllowed(map[string]interface{}{"task": "lint", "target": "corelib"}); err != nil || task != "lint" || target != "corelib" {
		t.Fatalf("task=%q target=%q err=%v", task, target, err)
	}
	for _, args := range []map[string]interface{}{
		{"task": "test; rm -rf /"},
		{"task": "deploy"},
		{"task": ""},
		{"task": "test", "command": "rm -rf /"},
		{"task": "test", "project_path": "/other"},
		{"command": "go test ./..."},
		{"task": 7},
		{"target": "corelib"},
	} {
		if _, _, err := reviewedHostBuildVerifyArgsAllowed(args); err == nil {
			t.Fatalf("arguments %#v were accepted", args)
		}
	}
}

// The reviewed argv table is the complete set of programs this capability can
// start. A shell entry would reopen the injection surface it exists to close.
func TestBuildVerifyCommandTableNeverRunsThroughAShell(t *testing.T) {
	found := 0
	for _, kind := range coretool.BuildVerifyProjectKinds() {
		for _, task := range coretool.BuildVerifyTasks() {
			argv, ok := coretool.BuildVerifyCommand(kind, task)
			if !ok {
				continue
			}
			found++
			switch argv[0] {
			case "sh", "bash", "cmd", "powershell", "pwsh", "zsh", "env":
				t.Fatalf("%s/%s runs through a shell: %v", kind, task, argv)
			}
			for _, arg := range argv {
				if strings.ContainsAny(arg, "|&;<>$`") {
					t.Fatalf("%s/%s carries shell syntax: %v", kind, task, argv)
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("the command table is empty, this check is vacuous")
	}
	// A caller must not be able to edit the reviewed table through the value
	// it is handed back.
	argv, _ := coretool.BuildVerifyCommand("go", "test")
	argv[0] = "rm"
	if again, _ := coretool.BuildVerifyCommand("go", "test"); again[0] != "go" {
		t.Fatalf("the command table was mutated through a returned slice: %v", again)
	}
}

func TestReviewedHostBuildVerifyPlansAndExecutesWithoutTheShellCapability(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	verifier := &fakeHostBuildVerifier{result: "ok 12 tests"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	// Shell is deliberately absent from the host services. Verification must
	// still plan and run; that separation is the entire point of the slice.
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{BuildVerify: verifier})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "verify", Capability: CapabilityBuildVerify, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("build verify plan=%#v err=%v", plan, err)
	}
	selection := plan.Selections[0]
	if selection.Provider.Kind != reviewedHostProviderKind || selection.FitProof.MatchedCapability != CapabilityBuildVerify {
		t.Fatalf("selection=%#v", selection)
	}
	// Running a build is a local mutation, not an external effect: it writes
	// artifacts inside the workspace and nothing leaves the host.
	if !dynamicHostLocalMutationSelection(selection) || dynamicSelectionRequiresReceipt(selection) {
		t.Fatalf("build verify must not take the external receipt path: %#v", selection)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, selection, `{"task":"test"}`)
	if !result.Succeeded || result.Result != verifier.result || result.Unknown {
		t.Fatalf("result=%#v", result)
	}
	if verifier.task != "test" || verifier.target != "" || verifier.calls != 1 {
		t.Fatalf("verifier got task=%q target=%q calls=%d", verifier.task, verifier.target, verifier.calls)
	}
	// A shell-shaped argument must not reach the host even though the adapter
	// is the one that runs commands.
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, selection, `{"task":"test","command":"rm -rf /"}`)
	if rejected.Succeeded {
		t.Fatalf("a command argument was accepted: %#v", rejected)
	}
	if verifier.calls != 1 {
		t.Fatalf("the rejected call still reached the host, calls=%d", verifier.calls)
	}
}

// A plan that asked only for verification must not be able to obtain the
// shell adapter, which is the failure mode that made this capability
// necessary in the first place.
func TestBuildVerifyGrantDoesNotCarryTheShellAdapter(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	verifier := &fakeHostBuildVerifier{result: "ok"}
	shell := &fakeHostShellExecutor{result: "exit 0"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{BuildVerify: verifier, Shell: shell})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "verify", Capability: CapabilityBuildVerify, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if got := plan.Selections[0].AdapterName; got != reviewedHostBuildVerifyAdapterName {
		t.Fatalf("a verification need selected %q", got)
	}
	if shell.command != "" {
		t.Fatalf("the shell host received %q", shell.command)
	}
}
