package agentservice

import (
	"context"
	"fmt"
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
	startLine   int
	principal   Principal
	result      string
	err         error
}

func (f *fakeHostFileReader) ReadReviewedHostFile(_ context.Context, principal Principal, path, query, filePattern string, startLine int) (string, error) {
	f.principal = principal
	f.path = path
	f.query = query
	f.filePattern = filePattern
	f.startLine = startLine
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
	if reader.path != "README.md" || reader.query != "" || reader.filePattern != "" || reader.startLine != 0 || reader.principal.TenantID != principal.TenantID || reader.principal.UserID != principal.UserID {
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
	paged := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"README.md","start_line":40}`)
	if !paged.Succeeded || reader.path != "README.md" || reader.startLine != 40 || reader.query != "" {
		t.Fatalf("start_line result=%#v reader=%#v", paged, reader)
	}
	conflict := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"README.md","start_line":40,"query":"x"}`)
	if conflict.Succeeded || conflict.Unknown || conflict.ReasonCode != "host_file_read_conflicting_fields" || reader.query != "" || reader.startLine != 40 {
		t.Fatalf("start_line plus query must fail closed before the reader, result=%#v reader=%#v", conflict, reader)
	}
	linesRejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"README.md","lines":20}`)
	if linesRejected.Succeeded || linesRejected.Unknown {
		t.Fatalf("lines must stay out of the schema, result=%#v", linesRejected)
	}
	washed := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"README.md","start_line":"40"}`)
	if washed.Succeeded || washed.Unknown {
		t.Fatalf("a numeric string must not be washed into start_line, result=%#v", washed)
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
	if _, ok := props["query"]; !ok || len(props) != 4 {
		t.Fatalf("file read schema=%#v", props)
	}
	// file_pattern locates files by name. start_line is the host-fixed page
	// cursor. The keys below stay out, including the legacy page knobs.
	if _, ok := props["file_pattern"]; !ok {
		t.Fatalf("host file read cannot locate files by name: %#v", props)
	}
	startLineSpec, _ := props["start_line"].(map[string]interface{})
	if startLineSpec["description"] != "1-based start of one host-sized text page. The page length is fixed. Do not combine with query or file_pattern." {
		t.Fatalf("host start_line description=%#v", startLineSpec)
	}
	for _, key := range []string{
		"channel", "destination", "group_name", "file_path", "content", "save_path",
		"lines", "offset", "end_line",
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
	out, err := cb.ReadReviewedHostFile(context.Background(), principal, "notes.txt", "", "", 0)
	if err != nil || !strings.Contains(out, "hello workspace") {
		t.Fatalf("read file=%q err=%v", out, err)
	}
	fromOne, err := cb.ReadReviewedHostFile(context.Background(), principal, "notes.txt", "", "", 1)
	if err != nil || fromOne != "hello workspace" {
		t.Fatalf("start_line=1 on a short file must stay raw text, out=%q err=%v", fromOne, err)
	}
	if _, err := cb.ReadReviewedHostFile(context.Background(), principal, "notes.txt", "", "", 2); err == nil || !strings.Contains(err.Error(), "host_file_read_range_rejected") {
		t.Fatalf("past end err=%v", err)
	}
	if _, err := cb.ReadReviewedHostFile(context.Background(), principal, "", "", "", 1); err == nil || !strings.Contains(err.Error(), "host_file_read_start_line_unsupported") {
		t.Fatalf("directory start_line err=%v", err)
	}
	if _, err := cb.ReadReviewedHostFile(context.Background(), principal, "notes.txt", "needle", "", 2); err == nil || !strings.Contains(err.Error(), "host_file_read_conflicting_fields") {
		t.Fatalf("conflict err=%v", err)
	}
	listed, err := cb.ReadReviewedHostFile(context.Background(), principal, "", "", "", 0)
	if err != nil || !strings.Contains(listed, "notes.txt") {
		t.Fatalf("list root=%q err=%v", listed, err)
	}
	if _, err := cb.ReadReviewedHostFile(context.Background(), principal, filepath.Join("..", "outside.txt"), "", "", 0); err == nil {
		t.Fatal("workspace escape must fail closed")
	}
	escaped := &coreAgentCallbacks{principal: principal}
	if _, err := escaped.ReadReviewedHostFile(context.Background(), principal, filepath.Join(dir, "notes.txt"), "", "", 0); err == nil {
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
	out, err := cb.ReadReviewedHostFile(context.Background(), principal, "table.csv", "", "", 0)
	if err != nil || !strings.Contains(out, "hello-doc") {
		t.Fatalf("document read=%q err=%v", out, err)
	}
	if strings.Contains(out, "\x00") {
		t.Fatalf("document read returned a binary dump: %q", out)
	}
	if _, err := cb.ReadReviewedHostFile(context.Background(), principal, "table.csv", "", "", 1); err == nil || !strings.Contains(err.Error(), "host_file_read_start_line_unsupported") {
		t.Fatalf("document start_line err=%v", err)
	}

	docxPath := filepath.Join(dir, "notes.docx")
	if err := os.WriteFile(docxPath, []byte("PK\x03\x04not-a-docx\x00binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	rejected, err := cb.ReadReviewedHostFile(context.Background(), principal, "notes.docx", "", "", 0)
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
	out, err := cb.ReadReviewedHostFile(context.Background(), principal, "", "UniqueNeedle123", "", 0)
	if err != nil || !strings.Contains(out, "UniqueNeedle123") || !strings.Contains(out, "alpha.go") {
		t.Fatalf("workspace search=%q err=%v", out, err)
	}
	if strings.Contains(out, "beta.go") && strings.Contains(out, "package beta") && !strings.Contains(out, "UniqueNeedle123") {
		t.Fatalf("search leaked an unmatched file: %q", out)
	}
	if _, err := cb.ReadReviewedHostFile(context.Background(), principal, filepath.Join("..", "outside"), "UniqueNeedle123", "", 0); err == nil {
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

	located, err := cb.ReadReviewedHostFile(context.Background(), principal, "", "", "**/*.go", 0)
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
	scoped, err := cb.ReadReviewedHostFile(context.Background(), principal, "", "UniqueNeedle123", "*.go", 0)
	if err != nil || !strings.Contains(scoped, "UniqueNeedle123") {
		t.Fatalf("scoped search=%q err=%v", scoped, err)
	}
	if strings.Contains(scoped, "log.txt") {
		t.Fatalf("scoped search escaped its file shape: %q", scoped)
	}

	if _, err := cb.ReadReviewedHostFile(context.Background(), principal, filepath.Join("..", "outside"), "", "*.go", 0); err == nil {
		t.Fatal("locate path escape must fail closed")
	}
}

func TestReviewedHostFileReadStartLineCountsCompilerLines(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	for i := 1; i <= srvReadFileMaxLines; i++ {
		fmt.Fprintf(&body, "EDGE-%d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "exact.txt"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	// A blank line is a real line. The final newline is not a second one.
	if err := os.WriteFile(filepath.Join(dir, "blank.txt"), []byte("a\n\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: dir}
	out, err := cb.ReadReviewedHostFile(context.Background(), principal, "exact.txt", "", "", 0)
	if err != nil || out != body.String() || strings.Contains(out, "Next:") {
		t.Fatalf("exact page=%q err=%v", out, err)
	}
	last, err := cb.ReadReviewedHostFile(context.Background(), principal, "exact.txt", "", "", srvReadFileMaxLines)
	if err != nil || !strings.Contains(last, fmt.Sprintf("EDGE-%d\n", srvReadFileMaxLines)) || strings.Contains(last, "Next:") || strings.Contains(last, "EDGE-1\n") {
		t.Fatalf("last line=%q err=%v", last, err)
	}
	if _, err := cb.ReadReviewedHostFile(context.Background(), principal, "exact.txt", "", "", srvReadFileMaxLines+1); err == nil || !strings.Contains(err.Error(), "host_file_read_range_rejected") {
		t.Fatalf("one past the last line err=%v", err)
	}
	blank, err := cb.ReadReviewedHostFile(context.Background(), principal, "blank.txt", "", "", 2)
	if err != nil || blank != "(lines 2-3 of 3)\n\nb\n" {
		t.Fatalf("blank line=%q err=%v", blank, err)
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
	logOut, err := cb.ReadReviewedHostFile(context.Background(), principal, "app.log", "", "", 0)
	if err != nil || !strings.Contains(logOut, "TAIL-MARKER") || strings.Contains(logOut, "HEAD-MARKER") {
		t.Fatalf("log tail=%q err=%v", logOut, err)
	}
	txtOut, err := cb.ReadReviewedHostFile(context.Background(), principal, "notes.txt", "", "", 0)
	if err != nil || !strings.Contains(txtOut, "HEAD-MARKER") || strings.Contains(txtOut, "TAIL-MARKER") || !strings.Contains(txtOut, "Next: start_line=") {
		t.Fatalf("text files must keep head-first paging, out=%q err=%v", txtOut, err)
	}
	logHead, err := cb.ReadReviewedHostFile(context.Background(), principal, "app.log", "", "", 1)
	if err != nil || !strings.Contains(logHead, "HEAD-MARKER") || strings.Contains(logHead, "TAIL-MARKER") || !strings.Contains(logHead, "Next: start_line=") {
		t.Fatalf("start_line=1 must read the log head, out=%q err=%v", logHead, err)
	}
	later, err := cb.ReadReviewedHostFile(context.Background(), principal, "notes.txt", "", "", srvReadFileMaxLines+1)
	if err != nil || strings.Contains(later, "HEAD-MARKER") || !strings.HasPrefix(later, fmt.Sprintf("(lines %d-", srvReadFileMaxLines+1)) {
		t.Fatalf("later page=%q err=%v", later, err)
	}
}
