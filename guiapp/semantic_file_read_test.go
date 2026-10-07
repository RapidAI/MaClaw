package guiapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func fileReadClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelFileRead,
		Confidence: .98,
		ToolNames:  []string{"read_file", "list_directory", "search_files"},
	}
}

func TestIMSemanticFileReadUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileRead)}
	h.semanticTrustedFileRead = func(userID, path, query, filePattern string, startLine int) (string, error) {
		t.Fatalf("planning must not execute read user=%q path=%q", userID, path)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看 notes.txt", "lansenger", "root-fread", "turn-fread", fileReadClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedFileReadAdapter || selection.FitProof.MatchedCapability != tool.CapabilityFSReadLocal {
		t.Fatalf("selection=%+v", selection)
	}
	if semanticSelectionRequiresReceipt(selection) {
		t.Fatalf("read-only file inspect must not require a receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "read_file", "list_directory", "search_files", "read_tool_result")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["path"]; !ok || len(properties) != 4 {
		t.Fatalf("file read schema=%#v", properties)
	}
	// file_pattern locates files by name. start_line is the host-fixed page
	// cursor. Both name outcomes path and query cannot reach. The forbidden
	// list is the legacy knob set, not these two fields.
	if _, ok := properties["file_pattern"]; !ok {
		t.Fatalf("managed file read cannot locate files by name: %#v", properties)
	}
	startLineSpec, _ := properties["start_line"].(map[string]interface{})
	if startLineSpec["description"] != "1-based start of one host-sized text page. The page length is fixed. Do not combine with query or file_pattern." {
		t.Fatalf("model-facing start_line description=%#v", startLineSpec)
	}
	// The renderer publishes the capability summary, not the adapter's
	// function description. The page contract has to live on the property.
	if fnDesc, _ := definition["description"].(string); !strings.HasPrefix(fnDesc, "Read or search local filesystem content") || strings.Contains(fnDesc, "host-sized") {
		t.Fatalf("rendered description=%q", fnDesc)
	}
	for _, forbidden := range []string{
		"lines", "offset", "end_line", "file_path", "content", "save_path",
		"channel", "destination", "group_name", "max_results", "include_hidden",
		"include_dirs", "type", "exclude", "project_path",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing file read schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedFileReadAdapter, `{"path":"notes.txt"}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"path":"notes.txt","lines":20,"file_path":"C:/src"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged read fields=%q", got)
	}
}

func TestIMSemanticFileReadExecutesPathQueryWithoutKeywordBranch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileRead)}
	var seenPath, seenQuery, seenPattern string
	var seenStart int
	h.semanticTrustedFileRead = func(userID, path, query, filePattern string, startLine int) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seenPath, seenQuery, seenPattern, seenStart = path, query, filePattern, startLine
		return "hello workspace", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "列出目录再搜一下", "lansenger", "root-fread-exec", "turn-fread-exec", fileReadClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"path":"notes.txt"}`)
	if !strings.Contains(got, "hello workspace") || strings.Contains(got, "read_file") || strings.Contains(got, "list_directory") {
		t.Fatalf("bound read=%q", got)
	}
	if seenPath != "notes.txt" || seenQuery != "" || seenPattern != "" || seenStart != 0 {
		t.Fatalf("dispatch path=%q query=%q file_pattern=%q start_line=%d", seenPath, seenQuery, seenPattern, seenStart)
	}
	if replay := cb.ExecuteTool(name, `{"path":"notes.txt"}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticFileReadRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileRead)}
	h.semanticTrustedFileRead = func(string, string, string, string, int) (string, error) {
		return "[file_base64|text/plain]AAAA", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看 notes.txt", "lansenger", "root-fread-token", "turn-fread-token", fileReadClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"path":"notes.txt","query":"x","channel":"lansenger"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") && !strings.Contains(got, "trusted_file_read_arguments_rejected") {
		t.Fatalf("extra field=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看 notes.txt", "lansenger", "root-fread-token-2", "turn-fread-token-2", fileReadClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"path":"notes.txt"}`); !strings.Contains(got, "trusted_file_read_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.readTrustedFile("", "notes.txt", "", "", 0); err == nil || !strings.Contains(err.Error(), "trusted_file_read_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

func TestIMSemanticFileReadStaysInsideBoundWorkspace(t *testing.T) {
	h := &IMMessageHandler{}
	if _, err := h.readTrustedFile("user-1", `C:\Windows\System32\drivers\etc\hosts`, "", "", 0); err == nil || !strings.Contains(err.Error(), "trusted_file_read_path_unavailable") {
		t.Fatalf("empty workspace absolute path err=%v", err)
	}

	workspace := t.TempDir()
	principal := desktopUserID + ":" + workspace
	if _, err := h.writeTrustedFile(principal, "notes.txt", "hello workspace", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "alpha.go"), []byte("package alpha\nfunc UniqueNeedle123() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "table.csv"), []byte("name,value\nhello-doc,workspace\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	read, err := h.readTrustedFile(principal, "notes.txt", "", "", 0)
	if err != nil || !strings.Contains(read, "hello workspace") || strings.Contains(read, workspace) || strings.Contains(read, "read_file") {
		t.Fatalf("read=%q err=%v", read, err)
	}
	listed, err := h.readTrustedFile(principal, "", "", "", 0)
	if err != nil || !strings.Contains(listed, "notes.txt") || strings.Contains(listed, workspace) || strings.Contains(listed, "list_directory") {
		t.Fatalf("list=%q err=%v", listed, err)
	}
	searched, err := h.readTrustedFile(principal, "", "UniqueNeedle123", "", 0)
	if err != nil || !strings.Contains(searched, "UniqueNeedle123") || !strings.Contains(searched, "alpha.go") {
		t.Fatalf("search=%q err=%v", searched, err)
	}
	if strings.Contains(searched, workspace) {
		t.Fatalf("search leaked workspace path: %q", searched)
	}
	csv, err := h.readTrustedFile(principal, "table.csv", "", "", 0)
	if err != nil || !strings.Contains(csv, "hello-doc") || strings.Contains(csv, "\x00") {
		t.Fatalf("csv=%q err=%v", csv, err)
	}
	if _, err := h.readTrustedFile(principal, `..\escape.txt`, "", "", 0); err == nil || !strings.Contains(err.Error(), "trusted_file_read_path_rejected") {
		t.Fatalf("escape path err=%v", err)
	}
}

// TestIMSemanticFileReadLocatesFilesByName covers the outcome that path and
// query cannot reach. Without it a managed plan holding only fs.read.local can
// read a file it already knows about and grep for a string, but has no way to
// discover which files exist, which is what the legacy coding surface used Glob
// for.
func TestIMSemanticFileReadLocatesFilesByName(t *testing.T) {
	h := &IMMessageHandler{}
	workspace := t.TempDir()
	principal := desktopUserID + ":" + workspace
	if err := os.MkdirAll(filepath.Join(workspace, "pkg", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(workspace, "alpha.go"):                 "package alpha\n// UniqueNeedle123\n",
		filepath.Join(workspace, "pkg", "beta.go"):           "package pkg\n",
		filepath.Join(workspace, "pkg", "inner", "gamma.go"): "package inner\n// UniqueNeedle123\n",
		filepath.Join(workspace, "pkg", "notes.txt"):         "not a go file\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	located, err := h.readTrustedFile(principal, "", "", "**/*.go", 0)
	if err != nil {
		t.Fatalf("locate err=%v", err)
	}
	for _, want := range []string{"alpha.go", "beta.go", "gamma.go"} {
		if !strings.Contains(located, want) {
			t.Fatalf("locate did not reach %q: %q", want, located)
		}
	}
	if strings.Contains(located, "notes.txt") {
		t.Fatalf("locate ignored the name shape: %q", located)
	}
	if strings.Contains(located, workspace) {
		t.Fatalf("locate leaked the absolute workspace path: %q", located)
	}
	for _, leaked := range []string{"Glob", "search_files", "ripgrep"} {
		if strings.Contains(located, leaked) {
			t.Fatalf("locate leaked legacy tool name %q: %q", leaked, located)
		}
	}

	// A name shape alongside a query narrows the content search rather than
	// running a second, separate one.
	scoped, err := h.readTrustedFile(principal, "", "UniqueNeedle123", "*.go", 0)
	if err != nil || !strings.Contains(scoped, "UniqueNeedle123") {
		t.Fatalf("scoped search=%q err=%v", scoped, err)
	}
	if strings.Contains(scoped, "notes.txt") || strings.Contains(scoped, workspace) {
		t.Fatalf("scoped search escaped its file shape or leaked the workspace: %q", scoped)
	}

	// The name shape is matched under the resolved path, so it cannot be used
	// to walk out of the bound workspace.
	if _, err := h.readTrustedFile(principal, `..`, "", "*.go", 0); err == nil || !strings.Contains(err.Error(), "trusted_file_read_path_rejected") {
		t.Fatalf("locate escaped the workspace err=%v", err)
	}
}

func TestIMSemanticFileReadRejectsArgumentsOutsideTheClosedSet(t *testing.T) {
	for _, args := range []map[string]interface{}{
		{"path": "a", "query": "b", "file_pattern": "c", "lines": "4"},
		{"pattern": "*.go"},
		{"file_pattern": 7},
		{"start_line": "4"},
		{"start_line": 0},
		{"start_line": 1.5},
		{"path": "a", "query": "b", "start_line": float64(3)},
		{"path": "a", "file_pattern": "*.go", "start_line": 3},
	} {
		if _, _, _, _, err := semanticTrustedFileReadArgsAllowed(args); err == nil {
			t.Fatalf("arguments %v were accepted by the closed set", args)
		}
	}
	path, query, filePattern, startLine, err := semanticTrustedFileReadArgsAllowed(map[string]interface{}{
		"path": " pkg ", "query": " needle ", "file_pattern": " *.go ",
	})
	if err != nil || path != "pkg" || query != "needle" || filePattern != "*.go" || startLine != 0 {
		t.Fatalf("path=%q query=%q file_pattern=%q start_line=%d err=%v", path, query, filePattern, startLine, err)
	}
	_, _, _, startLine, err = semanticTrustedFileReadArgsAllowed(map[string]interface{}{
		"path": "notes.txt", "start_line": float64(40),
	})
	if err != nil || startLine != 40 {
		t.Fatalf("float64 start_line=%d err=%v", startLine, err)
	}
	_, _, _, startLine, err = semanticTrustedFileReadArgsAllowed(map[string]interface{}{
		"start_line": json.Number("12"),
	})
	if err != nil || startLine != 12 {
		t.Fatalf("json.Number start_line=%d err=%v", startLine, err)
	}
}

func TestIMSemanticFileReadForwardsStartLine(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileRead)}
	var seen int
	h.semanticTrustedFileRead = func(_, _, _, _ string, startLine int) (string, error) {
		seen = startLine
		return "page", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看 notes.txt", "lansenger", "root-fread-page", "turn-fread-page", fileReadClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"path":"notes.txt","start_line":40}`)
	if !strings.Contains(got, "page") || seen != 40 {
		t.Fatalf("forwarded page=%q start_line=%d", got, seen)
	}
}

func TestIMSemanticFileReadPagesFromStartLine(t *testing.T) {
	h := &IMMessageHandler{}
	workspace := t.TempDir()
	principal := desktopUserID + ":" + workspace
	total := readFileMaxLines + 30
	var body strings.Builder
	for i := 1; i <= total; i++ {
		fmt.Fprintf(&body, "LINE-%d\n", i)
	}
	if err := os.WriteFile(filepath.Join(workspace, "long.txt"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var logBody strings.Builder
	logBody.WriteString("HEAD-MARKER\n")
	for i := 0; i < readFileMaxLines+20; i++ {
		logBody.WriteString("line\n")
	}
	logBody.WriteString("TAIL-MARKER\n")
	if err := os.WriteFile(filepath.Join(workspace, "long.log"), []byte(logBody.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "short.txt"), []byte("only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "table.csv"), []byte("name,value\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	short, err := h.readTrustedFile(principal, "short.txt", "", "", 0)
	if err != nil || short != "only\n" {
		t.Fatalf("omitted start on a short file=%q err=%v", short, err)
	}
	shortFromOne, err := h.readTrustedFile(principal, "short.txt", "", "", 1)
	if err != nil || shortFromOne != "only\n" {
		t.Fatalf("start_line=1 on a short file=%q err=%v", shortFromOne, err)
	}
	// A trailing newline is not an extra line. A file that fills the page
	// exactly must come back whole, and the next cursor must not name a line
	// past the end.
	var exact strings.Builder
	for i := 1; i <= readFileMaxLines; i++ {
		fmt.Fprintf(&exact, "EDGE-%d\n", i)
	}
	if err := os.WriteFile(filepath.Join(workspace, "exact.txt"), []byte(exact.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	exactPage, err := h.readTrustedFile(principal, "exact.txt", "", "", 0)
	if err != nil || exactPage != exact.String() || strings.Contains(exactPage, "Next:") {
		t.Fatalf("exact page=%q err=%v", exactPage, err)
	}
	lastLine, err := h.readTrustedFile(principal, "exact.txt", "", "", readFileMaxLines)
	if err != nil || !strings.Contains(lastLine, fmt.Sprintf("EDGE-%d\n", readFileMaxLines)) || strings.Contains(lastLine, "Next:") || strings.Contains(lastLine, "EDGE-1\n") {
		t.Fatalf("last line=%q err=%v", lastLine, err)
	}
	if _, err := h.readTrustedFile(principal, "exact.txt", "", "", readFileMaxLines+1); err == nil || !strings.Contains(err.Error(), "trusted_file_read_start_past_end") {
		t.Fatalf("one past the last line err=%v", err)
	}

	head, err := h.readTrustedFile(principal, "long.txt", "", "", 0)
	if err != nil || !strings.Contains(head, "LINE-1\n") || strings.Contains(head, fmt.Sprintf("LINE-%d\n", total)) || !strings.Contains(head, fmt.Sprintf("Next: start_line=%d", readFileMaxLines+1)) {
		t.Fatalf("head page=%q err=%v", head, err)
	}
	page, err := h.readTrustedFile(principal, "long.txt", "", "", readFileMaxLines+1)
	if err != nil || strings.Contains(page, "LINE-1\n") || !strings.Contains(page, fmt.Sprintf("LINE-%d\n", readFileMaxLines+1)) || !strings.HasPrefix(page, fmt.Sprintf("(lines %d-", readFileMaxLines+1)) {
		t.Fatalf("later page=%q err=%v", page, err)
	}
	if _, err := h.readTrustedFile(principal, "long.txt", "", "", 1000000); err == nil || !strings.Contains(err.Error(), "trusted_file_read_start_past_end") {
		t.Fatalf("past end err=%v", err)
	}
	logTail, err := h.readTrustedFile(principal, "long.log", "", "", 0)
	if err != nil || !strings.Contains(logTail, "TAIL-MARKER") || strings.Contains(logTail, "HEAD-MARKER") {
		t.Fatalf("log tail=%q err=%v", logTail, err)
	}
	logHead, err := h.readTrustedFile(principal, "long.log", "", "", 1)
	if err != nil || !strings.Contains(logHead, "HEAD-MARKER") || strings.Contains(logHead, "TAIL-MARKER") || !strings.Contains(logHead, "Next: start_line=") {
		t.Fatalf("log head=%q err=%v", logHead, err)
	}
	if _, err := h.readTrustedFile(principal, "", "", "", 1); err == nil || !strings.Contains(err.Error(), "trusted_file_read_start_line_unsupported") {
		t.Fatalf("directory start_line err=%v", err)
	}
	if _, err := h.readTrustedFile(principal, "table.csv", "", "", 1); err == nil || !strings.Contains(err.Error(), "trusted_file_read_start_line_unsupported") {
		t.Fatalf("document start_line err=%v", err)
	}
	called := false
	h.semanticTrustedFileRead = func(_, _, _, _ string, _ int) (string, error) {
		called = true
		return "hook", nil
	}
	if _, err := h.readTrustedFile(principal, "long.txt", "needle", "", 2); err == nil || !strings.Contains(err.Error(), "trusted_file_read_conflicting_fields") || called {
		t.Fatalf("conflict err=%v called=%v", err, called)
	}
}

func TestIMSemanticFileReadProjectionKeepsPagedText(t *testing.T) {
	// Line 1 is blank. The model-facing text has to keep it, or the footer
	// names a different line than the body starts on.
	var body strings.Builder
	body.WriteString("\n")
	for i := 2; i <= readFileMaxLines+1; i++ {
		fmt.Fprintf(&body, "L-%d\n", i)
	}
	window, err := trustedFileReadWindow(body.String(), 0, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := semanticTrustedFileReadResultProjection(window)
	if err != nil || got != window || !strings.HasPrefix(got, "\n") || !strings.Contains(got, fmt.Sprintf("Next: start_line=%d", readFileMaxLines+1)) {
		t.Fatalf("head projection=%q err=%v", got, err)
	}
	later, err := trustedFileReadWindow("a\n\nb\n", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err = semanticTrustedFileReadResultProjection(later)
	if err != nil || got != "(lines 2-3 of 3)\n\nb\n" {
		t.Fatalf("later projection=%q err=%v", got, err)
	}
	blank, err := semanticTrustedFileReadResultProjection("\n")
	if err != nil || blank != "\n" {
		t.Fatalf("blank line projection=%q err=%v", blank, err)
	}
	if _, err := semanticTrustedFileReadResultProjection(""); err == nil || !strings.Contains(err.Error(), "trusted_file_read_empty") {
		t.Fatalf("empty read err=%v", err)
	}
	if _, err := semanticTrustedFileReadResultProjection("see [file_base64]"); err == nil || !strings.Contains(err.Error(), "trusted_file_read_delivery_token") {
		t.Fatalf("delivery token err=%v", err)
	}
}
