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

type fakeHostFileReader struct {
	path        string
	query       string
	filePattern string
	principal   Principal
	result      string
	err         error
}

func (f *fakeHostFileReader) ReadReviewedHostFile(_ context.Context, principal Principal, path, query, filePattern string) (string, error) {
	f.principal = principal
	f.path = path
	f.query = query
	f.filePattern = filePattern
	return f.result, f.err
}

func TestReviewedHostFileReadExecutesPathAndRejectsLookupMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	reader := &fakeHostFileReader{result: "hello workspace"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{FileRead: reader})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "file", Capability: CapabilityFileRead, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("file read plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityFileRead {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"README.md"}`)
	if !result.Succeeded || result.Result != reader.result {
		t.Fatalf("file read result=%#v", result)
	}
	if reader.path != "README.md" || reader.query != "" || reader.filePattern != "" || reader.principal.TenantID != principal.TenantID || reader.principal.UserID != principal.UserID {
		t.Fatalf("reader=%#v", reader)
	}
	empty := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !empty.Succeeded {
		t.Fatalf("empty path must list workspace root, result=%#v", empty)
	}
	searched := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"query":"UniqueNeedle123"}`)
	if !searched.Succeeded || reader.query != "UniqueNeedle123" {
		t.Fatalf("query search result=%#v reader=%#v", searched, reader)
	}
	located := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"file_pattern":"**/*.go"}`)
	if !located.Succeeded || reader.filePattern != "**/*.go" || reader.query != "" {
		t.Fatalf("locate by name result=%#v reader=%#v", located, reader)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"README.md","channel":"lansenger"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("channel args must fail closed, result=%#v", rejected)
	}

	lookupPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-lookup", TurnID: "turn-lookup", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "lookup", Capability: CapabilityInformationLookup, Required: true,
			Qualifiers: map[string]string{QualifierInformationScope: InformationScopeReference},
		}},
	})
	if err != nil || len(lookupPlan.Selections) != 0 {
		t.Fatalf("lookup must not be satisfied by host file read, plan=%#v err=%v", lookupPlan, err)
	}
}

func TestReviewedHostFileReadIsAbsentWithoutReader(t *testing.T) {
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
		Needs: []coretool.CapabilityNeed{{ID: "file", Capability: CapabilityFileRead, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("file read without reader must stay unmet, plan=%#v err=%v", plan, err)
	}
	clockPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-clock", TurnID: "turn-clock", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "clock", Capability: CapabilityCurrentTime, Required: true}},
	})
	if err != nil || len(clockPlan.Selections) != 1 {
		t.Fatalf("clock must still plan without a file reader, plan=%#v err=%v", clockPlan, err)
	}
}

func TestProjectReviewedHostFileReadRejectsWriteAndChannelFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostFileReadProvider(&fakeHostFileReader{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityFileRead {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["path"]; !ok {
		t.Fatalf("file read schema missing path: %#v", props)
	}
	if _, ok := props["query"]; !ok || len(props) != 3 {
		t.Fatalf("file read schema=%#v", props)
	}
	// file_pattern is the reviewed third field: locating files by name is an
	// outcome neither path nor query can reach. The keys below stay out.
	if _, ok := props["file_pattern"]; !ok {
		t.Fatalf("host file read cannot locate files by name: %#v", props)
	}
	for _, key := range []string{
		"channel", "destination", "group_name", "file_path", "content", "save_path",
		"max_results", "include_hidden", "include_dirs", "type", "exclude", "project_path",
	} {
		if _, ok := props[key]; ok {
			t.Fatalf("file read schema leaked %s", key)
		}
	}
}

func TestReviewedHostFileReadStaysInsideWorkspace(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello workspace"), 0o644); err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: dir}
	out, err := cb.ReadReviewedHostFile(context.Background(), principal, "notes.txt", "", "")
	if err != nil || !strings.Contains(out, "hello workspace") {
		t.Fatalf("read file=%q err=%v", out, err)
	}
	listed, err := cb.ReadReviewedHostFile(context.Background(), principal, "", "", "")
	if err != nil || !strings.Contains(listed, "notes.txt") {
		t.Fatalf("list root=%q err=%v", listed, err)
	}
	if _, err := cb.ReadReviewedHostFile(context.Background(), principal, filepath.Join("..", "outside.txt"), "", ""); err == nil {
		t.Fatal("workspace escape must fail closed")
	}
	escaped := &coreAgentCallbacks{principal: principal}
	if _, err := escaped.ReadReviewedHostFile(context.Background(), principal, filepath.Join(dir, "notes.txt"), "", ""); err == nil {
		t.Fatal("empty workspace must not read absolute paths")
	}
}

func TestReviewedHostFileReadUsesNativeDocumentReaderForOfficeFiles(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "table.csv")
	if err := os.WriteFile(csvPath, []byte("name,value\nhello-doc,workspace\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: dir}
	out, err := cb.ReadReviewedHostFile(context.Background(), principal, "table.csv", "", "")
	if err != nil || !strings.Contains(out, "hello-doc") {
		t.Fatalf("document read=%q err=%v", out, err)
	}
	if strings.Contains(out, "\x00") {
		t.Fatalf("document read returned a binary dump: %q", out)
	}

	docxPath := filepath.Join(dir, "notes.docx")
	if err := os.WriteFile(docxPath, []byte("PK\x03\x04not-a-docx\x00binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	rejected, err := cb.ReadReviewedHostFile(context.Background(), principal, "notes.docx", "", "")
	// The reader refused this file, and the refusal is the answer. It used to
	// arrive as a successful read whose body was the refusal notice, which is
	// how a document nobody could parse got recorded as one that was read.
	if err == nil {
		t.Fatalf("a document the reader refused was served as content: %q", rejected)
	}
	// Naming the reader's own class is also what proves the office path was
	// taken at all: a raw byte dump could not produce one.
	if !strings.Contains(err.Error(), "host_document_read_failed_malformed") {
		t.Fatalf("err = %v, want the native reader's own failure class", err)
	}
	if strings.Contains(rejected, "\x00") || strings.Contains(rejected, "PK\x03\x04") {
		t.Fatalf("office file must not dump raw bytes: %q", rejected)
	}
}

func TestReviewedHostFileReadSearchesWorkspaceWithoutLeavingIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.go"), []byte("package alpha\nfunc UniqueNeedle123() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "beta.go"), []byte("package beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: dir}
	out, err := cb.ReadReviewedHostFile(context.Background(), principal, "", "UniqueNeedle123", "")
	if err != nil || !strings.Contains(out, "UniqueNeedle123") || !strings.Contains(out, "alpha.go") {
		t.Fatalf("workspace search=%q err=%v", out, err)
	}
	if strings.Contains(out, "beta.go") && strings.Contains(out, "package beta") && !strings.Contains(out, "UniqueNeedle123") {
		t.Fatalf("search leaked an unmatched file: %q", out)
	}
	if _, err := cb.ReadReviewedHostFile(context.Background(), principal, filepath.Join("..", "outside"), "UniqueNeedle123", ""); err == nil {
		t.Fatal("search path escape must fail closed")
	}
}

// TestReviewedHostFileReadLocatesFilesByName covers the outcome that path and
// query cannot reach, so a plan holding only fs.read.local can discover which
// files exist rather than only reading ones it was already told about.
func TestReviewedHostFileReadLocatesFilesByName(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(dir, "alpha.go"):       "package alpha\n// UniqueNeedle123\n",
		filepath.Join(dir, "pkg", "beta.go"): "package pkg\n",
		filepath.Join(dir, "pkg", "log.txt"): "not a go file\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: dir}

	located, err := cb.ReadReviewedHostFile(context.Background(), principal, "", "", "**/*.go")
	if err != nil {
		t.Fatalf("locate err=%v", err)
	}
	if !strings.Contains(located, "alpha.go") || !strings.Contains(located, "beta.go") {
		t.Fatalf("locate did not reach the matching files: %q", located)
	}
	if strings.Contains(located, "log.txt") {
		t.Fatalf("locate ignored the name shape: %q", located)
	}

	// A name shape alongside a query narrows the content search rather than
	// running a second, separate one.
	scoped, err := cb.ReadReviewedHostFile(context.Background(), principal, "", "UniqueNeedle123", "*.go")
	if err != nil || !strings.Contains(scoped, "UniqueNeedle123") {
		t.Fatalf("scoped search=%q err=%v", scoped, err)
	}
	if strings.Contains(scoped, "log.txt") {
		t.Fatalf("scoped search escaped its file shape: %q", scoped)
	}

	if _, err := cb.ReadReviewedHostFile(context.Background(), principal, filepath.Join("..", "outside"), "", "*.go"); err == nil {
		t.Fatal("locate path escape must fail closed")
	}
}

func TestReviewedHostFileReadTailsLogFilesByType(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	body.WriteString("HEAD-MARKER\n")
	for i := 0; i < srvReadFileMaxLines+20; i++ {
		body.WriteString("line\n")
	}
	body.WriteString("TAIL-MARKER\n")
	if err := os.WriteFile(filepath.Join(dir, "app.log"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: dir}
	logOut, err := cb.ReadReviewedHostFile(context.Background(), principal, "app.log", "", "")
	if err != nil || !strings.Contains(logOut, "TAIL-MARKER") || strings.Contains(logOut, "HEAD-MARKER") {
		t.Fatalf("log tail=%q err=%v", logOut, err)
	}
	txtOut, err := cb.ReadReviewedHostFile(context.Background(), principal, "notes.txt", "", "")
	if err != nil || !strings.Contains(txtOut, "HEAD-MARKER") || strings.Contains(txtOut, "TAIL-MARKER") {
		t.Fatalf("text files must keep head-first paging, out=%q err=%v", txtOut, err)
	}
}
