package main

import (
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
	h.semanticTrustedFileRead = func(userID, path, query, filePattern string) (string, error) {
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
	if _, ok := properties["path"]; !ok || len(properties) != 3 {
		t.Fatalf("file read schema=%#v", properties)
	}
	// file_pattern is the reviewed third field: it names an outcome path and
	// query cannot reach. It stays out of the forbidden list below, which is
	// about the legacy tools' other knobs rather than this one shared name.
	if _, ok := properties["file_pattern"]; !ok {
		t.Fatalf("managed file read cannot locate files by name: %#v", properties)
	}
	for _, forbidden := range []string{
		"lines", "start_line", "offset", "file_path", "content", "save_path",
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
	h.semanticTrustedFileRead = func(userID, path, query, filePattern string) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seenPath, seenQuery, seenPattern = path, query, filePattern
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
	if seenPath != "notes.txt" || seenQuery != "" || seenPattern != "" {
		t.Fatalf("dispatch path=%q query=%q file_pattern=%q", seenPath, seenQuery, seenPattern)
	}
	if replay := cb.ExecuteTool(name, `{"path":"notes.txt"}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticFileReadRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileRead)}
	h.semanticTrustedFileRead = func(string, string, string, string) (string, error) {
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
	if _, err := h.readTrustedFile("", "notes.txt", "", ""); err == nil || !strings.Contains(err.Error(), "trusted_file_read_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

func TestIMSemanticFileReadStaysInsideBoundWorkspace(t *testing.T) {
	h := &IMMessageHandler{}
	if _, err := h.readTrustedFile("user-1", `C:\Windows\System32\drivers\etc\hosts`, "", ""); err == nil || !strings.Contains(err.Error(), "trusted_file_read_path_unavailable") {
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

	read, err := h.readTrustedFile(principal, "notes.txt", "", "")
	if err != nil || !strings.Contains(read, "hello workspace") || strings.Contains(read, workspace) || strings.Contains(read, "read_file") {
		t.Fatalf("read=%q err=%v", read, err)
	}
	listed, err := h.readTrustedFile(principal, "", "", "")
	if err != nil || !strings.Contains(listed, "notes.txt") || strings.Contains(listed, workspace) || strings.Contains(listed, "list_directory") {
		t.Fatalf("list=%q err=%v", listed, err)
	}
	searched, err := h.readTrustedFile(principal, "", "UniqueNeedle123", "")
	if err != nil || !strings.Contains(searched, "UniqueNeedle123") || !strings.Contains(searched, "alpha.go") {
		t.Fatalf("search=%q err=%v", searched, err)
	}
	if strings.Contains(searched, workspace) {
		t.Fatalf("search leaked workspace path: %q", searched)
	}
	csv, err := h.readTrustedFile(principal, "table.csv", "", "")
	if err != nil || !strings.Contains(csv, "hello-doc") || strings.Contains(csv, "\x00") {
		t.Fatalf("csv=%q err=%v", csv, err)
	}
	if _, err := h.readTrustedFile(principal, `..\escape.txt`, "", ""); err == nil || !strings.Contains(err.Error(), "trusted_file_read_path_rejected") {
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

	located, err := h.readTrustedFile(principal, "", "", "**/*.go")
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
	scoped, err := h.readTrustedFile(principal, "", "UniqueNeedle123", "*.go")
	if err != nil || !strings.Contains(scoped, "UniqueNeedle123") {
		t.Fatalf("scoped search=%q err=%v", scoped, err)
	}
	if strings.Contains(scoped, "notes.txt") || strings.Contains(scoped, workspace) {
		t.Fatalf("scoped search escaped its file shape or leaked the workspace: %q", scoped)
	}

	// The name shape is matched under the resolved path, so it cannot be used
	// to walk out of the bound workspace.
	if _, err := h.readTrustedFile(principal, `..`, "", "*.go"); err == nil || !strings.Contains(err.Error(), "trusted_file_read_path_rejected") {
		t.Fatalf("locate escaped the workspace err=%v", err)
	}
}

func TestIMSemanticFileReadRejectsArgumentsOutsideTheClosedSet(t *testing.T) {
	for _, args := range []map[string]interface{}{
		{"path": "a", "query": "b", "file_pattern": "c", "lines": "4"},
		{"pattern": "*.go"},
		{"file_pattern": 7},
	} {
		if _, _, _, err := semanticTrustedFileReadArgsAllowed(args); err == nil {
			t.Fatalf("arguments %v were accepted by the closed set", args)
		}
	}
	path, query, filePattern, err := semanticTrustedFileReadArgsAllowed(map[string]interface{}{
		"path": " pkg ", "query": " needle ", "file_pattern": " *.go ",
	})
	if err != nil || path != "pkg" || query != "needle" || filePattern != "*.go" {
		t.Fatalf("path=%q query=%q file_pattern=%q err=%v", path, query, filePattern, err)
	}
}
