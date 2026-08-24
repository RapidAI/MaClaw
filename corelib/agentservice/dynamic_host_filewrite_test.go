package agentservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostFileWriter struct {
	path      string
	content   string
	mode      string
	oldString string
	newString string
	edited    bool
	principal Principal
	result    string
	err       error
}

func (f *fakeHostFileWriter) WriteReviewedHostFile(_ context.Context, principal Principal, path, content, mode string) (string, error) {
	f.principal = principal
	f.path = path
	f.content = content
	f.mode = mode
	f.edited = false
	return f.result, f.err
}

func (f *fakeHostFileWriter) EditReviewedHostFile(_ context.Context, principal Principal, path, oldString, newString string) (string, error) {
	f.principal = principal
	f.path = path
	f.oldString = oldString
	f.newString = newString
	f.edited = true
	return f.result, f.err
}

func TestReviewedHostFileWriteExecutesWithoutCoordinatorAndRejectsLookupMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	writer := &fakeHostFileWriter{result: "Written to notes.txt (5 bytes)"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{FileWrite: writer})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "write", Capability: CapabilityFileWrite, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("file write plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityFileWrite {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host write must use the local mutation receipt, not the external coordinator, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], "{\"path\":\"notes.txt\",\"content\":\"hello\"}")
	if !result.Succeeded || result.Result != writer.result || result.Unknown {
		t.Fatalf("file write result=%#v", result)
	}
	if writer.path != "notes.txt" || writer.content != "hello" || writer.mode != "" || writer.principal.TenantID != principal.TenantID || writer.principal.UserID != principal.UserID {
		t.Fatalf("writer=%#v", writer)
	}
	appended := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], "{\"path\":\"notes.txt\",\"content\":\" more\",\"mode\":\"append\"}")
	if !appended.Succeeded || writer.mode != "append" {
		t.Fatalf("append result=%#v writer=%#v", appended, writer)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], "{\"path\":\"notes.txt\",\"content\":\"hello\",\"channel\":\"lansenger\"}")
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("channel args must fail closed, result=%#v", rejected)
	}
	// Which pair is present routes the outcome; there is no action field.
	edited := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], "{\"path\":\"notes.txt\",\"old_string\":\"hello\",\"new_string\":\"goodbye\"}")
	if !edited.Succeeded || !writer.edited || writer.oldString != "hello" || writer.newString != "goodbye" {
		t.Fatalf("edit result=%#v writer=%#v", edited, writer)
	}
	for _, args := range []string{
		"{\"path\":\"notes.txt\",\"old_string\":\"hello\"}",
		"{\"path\":\"notes.txt\",\"new_string\":\"goodbye\"}",
		"{\"path\":\"notes.txt\",\"old_string\":\"hello\",\"new_string\":\"bye\",\"content\":\"whole\"}",
		"{\"path\":\"notes.txt\",\"old_string\":\"hello\",\"new_string\":\"bye\",\"mode\":\"append\"}",
		"{\"path\":\"notes.txt\",\"old_string\":\"hello\",\"new_string\":\"bye\",\"replace_all\":\"true\"}",
	} {
		if out := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], args); out.Succeeded || out.Unknown {
			t.Fatalf("args %s must fail closed, result=%#v", args, out)
		}
	}

	readPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-read", TurnID: "turn-read", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "read", Capability: CapabilityFileRead, Required: true}},
	})
	if err != nil || len(readPlan.Selections) != 0 {
		t.Fatalf("file_read must not be satisfied by host file write, plan=%#v err=%v", readPlan, err)
	}
}

func TestReviewedHostFileWriteIsAbsentWithoutWriter(t *testing.T) {
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
		Needs: []coretool.CapabilityNeed{{ID: "write", Capability: CapabilityFileWrite, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("file write without writer must stay unmet, plan=%#v err=%v", plan, err)
	}
	clockPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-clock", TurnID: "turn-clock", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "clock", Capability: CapabilityCurrentTime, Required: true}},
	})
	if err != nil || len(clockPlan.Selections) != 1 {
		t.Fatalf("clock must still plan without a file writer, plan=%#v err=%v", clockPlan, err)
	}
}

func TestProjectReviewedHostFileWriteRejectsChannelAndReadFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostFileWriteProvider(&fakeHostFileWriter{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityFileWrite || provider.AdapterName == "write_file" || provider.AdapterName == "edit_file" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["path"]; !ok {
		t.Fatalf("file write schema missing path: %#v", props)
	}
	if _, ok := props["content"]; !ok || len(props) != 5 {
		t.Fatalf("file write schema=%#v", props)
	}
	if _, ok := props["mode"]; !ok {
		t.Fatalf("file write schema missing mode: %#v", props)
	}
	// The replacement pair is the reviewed addition: editing one passage is an
	// outcome whole-file content cannot express. The legacy edit tool's other
	// knobs stay out.
	if _, ok := props["old_string"]; !ok {
		t.Fatalf("host file write cannot replace a passage: %#v", props)
	}
	if _, ok := props["new_string"]; !ok {
		t.Fatalf("host file write cannot replace a passage: %#v", props)
	}
	for _, key := range []string{
		"channel", "destination", "group_name", "file_path", "query", "save_path",
		"replace_all", "start_line", "end_line", "operation", "occurrence",
	} {
		if _, ok := props[key]; ok {
			t.Fatalf("file write schema leaked %s", key)
		}
	}
}

func TestReviewedHostFileWriteStaysInsideWorkspace(t *testing.T) {
	dir := t.TempDir()
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: dir}
	out, err := cb.WriteReviewedHostFile(context.Background(), principal, "notes.txt", "hello write", "")
	if err != nil || !strings.Contains(out, "notes.txt") || strings.Contains(out, dir) {
		t.Fatalf("write file=%q err=%v", out, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "notes.txt"))
	if err != nil || string(data) != "hello write" {
		t.Fatalf("workspace file=%q err=%v", data, err)
	}
	appended, err := cb.WriteReviewedHostFile(context.Background(), principal, "notes.txt", " more", "append")
	if err != nil || !strings.Contains(appended, "notes.txt") {
		t.Fatalf("append=%q err=%v", appended, err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "notes.txt"))
	if err != nil || string(data) != "hello write more" {
		t.Fatalf("appended file=%q err=%v", data, err)
	}
	if _, err := cb.WriteReviewedHostFile(context.Background(), principal, filepath.Join("..", "outside.txt"), "nope", ""); err == nil {
		t.Fatal("workspace escape must fail closed")
	}
	escaped := &coreAgentCallbacks{principal: principal}
	if _, err := escaped.WriteReviewedHostFile(context.Background(), principal, filepath.Join(dir, "notes.txt"), "nope", ""); err == nil {
		t.Fatal("empty workspace must not write absolute paths")
	}
}

// TestReviewedHostFileEditReplacesOnlyAnUnambiguousPassage covers the outcome
// whole-file content cannot express, and the ambiguity rule that replaces the
// legacy replace_all switch.
func TestReviewedHostFileEditReplacesOnlyAnUnambiguousPassage(t *testing.T) {
	dir := t.TempDir()
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: dir}
	source := "package main\n\nfunc Alpha() int { return 1 }\nfunc Beta() int { return 1 }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := cb.EditReviewedHostFile(context.Background(), principal, "main.go", "func Alpha() int { return 1 }", "func Alpha() int { return 42 }")
	if err != nil || !strings.Contains(out, "main.go") || strings.Contains(out, dir) {
		t.Fatalf("edit=%q err=%v", out, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil || !strings.Contains(string(data), "return 42") {
		t.Fatalf("edited file=%q err=%v", data, err)
	}
	// The rest of the file survives: this is the property a whole-file rewrite
	// cannot promise.
	if !strings.Contains(string(data), "func Beta() int { return 1 }") || !strings.Contains(string(data), "package main") {
		t.Fatalf("edit rewrote more than the matched passage: %q", data)
	}

	// "return 1" still appears once (in Beta), so a passage that now matches a
	// single site must still be refused if it was never unique to begin with.
	if _, err := cb.EditReviewedHostFile(context.Background(), principal, "main.go", "int", "int64"); err == nil || !strings.Contains(err.Error(), "host_file_edit_ambiguous_match") {
		t.Fatalf("ambiguous passage err=%v", err)
	}
	if _, err := cb.EditReviewedHostFile(context.Background(), principal, "main.go", "func Gamma()", "func Delta()"); err == nil || !strings.Contains(err.Error(), "host_file_edit_no_match") {
		t.Fatalf("absent passage err=%v", err)
	}
	if _, err := cb.EditReviewedHostFile(context.Background(), principal, "missing.go", "a", "b"); err == nil || !strings.Contains(err.Error(), "host_file_edit_not_found") {
		t.Fatalf("edit must not create files err=%v", err)
	}
	if _, err := cb.EditReviewedHostFile(context.Background(), principal, "main.go", "", "b"); err == nil || !strings.Contains(err.Error(), "host_file_edit_old_string_required") {
		t.Fatalf("empty old_string err=%v", err)
	}
	if _, err := cb.EditReviewedHostFile(context.Background(), principal, filepath.Join("..", "outside.txt"), "a", "b"); err == nil {
		t.Fatal("workspace escape must fail closed")
	}

	// Deleting a passage is a replacement with empty text, not a separate mode.
	if _, err := cb.EditReviewedHostFile(context.Background(), principal, "main.go", "\nfunc Beta() int { return 1 }", ""); err != nil {
		t.Fatalf("delete via empty new_string err=%v", err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil || strings.Contains(string(data), "Beta") {
		t.Fatalf("passage was not removed: %q err=%v", data, err)
	}
}
