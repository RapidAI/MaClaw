package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func fileWriteClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelFileWrite,
		Confidence: .98,
		ToolNames:  []string{"write_file", "edit_file", "edit_lines"},
	}
}

func TestIMSemanticFileWriteUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileWrite)}
	h.semanticTrustedFileWrite = func(userID, path, content, mode string) (string, error) {
		t.Fatalf("planning must not execute write user=%q path=%q", userID, path)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "把这段写进 notes.txt", "lansenger", "root-fwrite", "turn-fwrite", fileWriteClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, tool.CapabilityFSWriteLocal)
	if !ok || selection.AdapterName != semanticTrustedFileWriteAdapter {
		t.Fatalf("selection=%+v found=%v", selection, ok)
	}
	if !semanticSelectionRequiresReceipt(selection) || !semanticBuiltinLocalMutationSelection(selection) {
		t.Fatalf("file write must use the local mutation receipt: %+v", selection.Effects)
	}
	name := semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter)
	def := semanticDefForGrantName(defs, name)
	if def == nil {
		t.Fatalf("file write def missing for grant %q: defs=%#v", name, defs)
	}
	definition := def["function"].(map[string]interface{})
	assertManagedModelName(t, name, definition, selection, "write_file", "edit_file", "edit_lines")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["path"]; !ok || len(properties) != 5 {
		t.Fatalf("file write schema=%#v", properties)
	}
	// The replacement pair is the reviewed addition: editing one passage is an
	// outcome whole-file content cannot express. It stays out of the forbidden
	// list below, which is about the legacy tools' other knobs.
	for _, required := range []string{"content", "mode", "old_string", "new_string"} {
		if _, ok := properties[required]; !ok {
			t.Fatalf("file write schema missing %q: %#v", required, properties)
		}
	}
	for _, forbidden := range []string{
		"phase_id", "doc_type", "file_path", "query", "save_path",
		"channel", "destination", "group_name",
		"replace_all", "start_line", "end_line", "operation", "occurrence",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing file write schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedFileWriteAdapter, `{"path":"notes.txt","content":"hi"}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"path":"notes.txt","content":"hi","phase_id":"p1","doc_type":"plan"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged write fields=%q", got)
	}
}

func TestIMSemanticFileWriteExecutesWithoutWriteFileSoup(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileWrite)}
	var seenPath, seenContent, seenMode string
	h.semanticTrustedFileWrite = func(userID, path, content, mode string) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seenPath, seenContent, seenMode = path, content, mode
		return "Written to notes.txt (5 bytes)", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "写个工作流文档", "lansenger", "root-fwrite-exec", "turn-fwrite-exec", fileWriteClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter)
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"path":"notes.txt","content":"hello","mode":"append"}`)
	if !strings.Contains(got, "Written to notes.txt") || strings.Contains(got, "write_file") || strings.Contains(got, "phase_id") {
		t.Fatalf("bound write=%q", got)
	}
	if seenPath != "notes.txt" || seenContent != "hello" || seenMode != "append" {
		t.Fatalf("dispatch path=%q content=%q mode=%q", seenPath, seenContent, seenMode)
	}
	if replay := cb.ExecuteTool(name, `{"path":"notes.txt","content":"hello","mode":"append"}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

// TestIMSemanticFileWriteTurnDispatchesEditByFieldPresence checks the routing
// half on a real managed turn: one adapter and one grant serve both outcomes,
// and which one runs is decided by the fields the model sent.
func TestIMSemanticFileWriteTurnDispatchesEditByFieldPresence(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileWrite)}
	var seenOld, seenNew string
	h.semanticTrustedFileWrite = func(userID, path, content, mode string) (string, error) {
		t.Fatalf("an old_string/new_string request must not reach the whole-file writer: path=%q content=%q", path, content)
		return "", nil
	}
	h.semanticTrustedFileEdit = func(userID, path, oldString, newString string) (string, error) {
		if userID != "user-1" || path != "main.go" {
			t.Fatalf("principal=%q path=%q", userID, path)
		}
		seenOld, seenNew = oldString, newString
		return "Edited main.go (64 bytes)", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "写个工作流文档", "lansenger", "root-fedit-exec", "turn-fedit-exec", fileWriteClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter)
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"path":"main.go","old_string":"return 1","new_string":"return 42"}`)
	if !strings.Contains(got, "Edited main.go") || strings.Contains(got, "edit_file") || strings.Contains(got, "edit_lines") {
		t.Fatalf("bound edit=%q", got)
	}
	if seenOld != "return 1" || seenNew != "return 42" {
		t.Fatalf("dispatch old=%q new=%q", seenOld, seenNew)
	}
}

func TestIMSemanticFileWriteRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileWrite)}
	h.semanticTrustedFileWrite = func(string, string, string, string) (string, error) {
		return "[file_base64|text/plain]AAAA", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "把这段写进 notes.txt", "lansenger", "root-fwrite-token", "turn-fwrite-token", fileWriteClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter)
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"path":"notes.txt","content":"hi","mode":"patch"}`); !strings.Contains(got, "trusted_file_write_mode_rejected") {
		t.Fatalf("bad mode=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "把这段写进 notes.txt", "lansenger", "root-fwrite-token-2", "turn-fwrite-token-2", fileWriteClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter)
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"path":"notes.txt","content":"hi"}`); !strings.Contains(got, "trusted_file_write_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.writeTrustedFile("", "notes.txt", "hi", ""); err == nil || !strings.Contains(err.Error(), "trusted_file_write_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
	if _, err := h.writeTrustedFile("user-1", "", "hi", ""); err == nil || !strings.Contains(err.Error(), "trusted_file_write_path_required") {
		t.Fatalf("empty path err=%v", err)
	}
}

func TestIMSemanticFileWriteStaysInsideBoundWorkspace(t *testing.T) {
	h := &IMMessageHandler{}
	if _, err := h.writeTrustedFile("user-1", `C:\Windows\System32\drivers\etc\hosts`, "nope", ""); err == nil || !strings.Contains(err.Error(), "trusted_file_write_path_unavailable") {
		t.Fatalf("empty workspace absolute path err=%v", err)
	}

	workspace := t.TempDir()
	principal := desktopUserID + ":" + workspace
	written, err := h.writeTrustedFile(principal, "notes.txt", "hello write", "")
	if err != nil || !strings.Contains(written, "notes.txt") || strings.Contains(written, workspace) || strings.Contains(written, "write_file") {
		t.Fatalf("write=%q err=%v", written, err)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "notes.txt"))
	if err != nil || string(data) != "hello write" {
		t.Fatalf("workspace file=%q err=%v", data, err)
	}
	appended, err := h.writeTrustedFile(principal, "notes.txt", " more", "append")
	if err != nil || !strings.Contains(appended, "Appended to notes.txt") {
		t.Fatalf("append=%q err=%v", appended, err)
	}
	data, err = os.ReadFile(filepath.Join(workspace, "notes.txt"))
	if err != nil || string(data) != "hello write more" {
		t.Fatalf("appended file=%q err=%v", data, err)
	}
	if _, err := h.writeTrustedFile(principal, `..\escape.txt`, "nope", ""); err == nil || !strings.Contains(err.Error(), "trusted_file_write_path_rejected") {
		t.Fatalf("escape path err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(workspace), "escape.txt")); err == nil {
		t.Fatal("escaped write must not create a sibling file")
	}
}

// TestIMSemanticFileEditReplacesOnlyAnUnambiguousPassage covers the outcome
// whole-file content cannot express. Without it a managed plan holding only
// fs.write.local must rewrite an entire existing file to change one line, which
// is the exact move the coding surface forbids because the model has to
// reproduce everything it is not changing.
func TestIMSemanticFileEditReplacesOnlyAnUnambiguousPassage(t *testing.T) {
	h := &IMMessageHandler{}
	workspace := t.TempDir()
	principal := desktopUserID + ":" + workspace
	source := "package main\n\nfunc Alpha() int { return 1 }\nfunc Beta() int { return 1 }\n"
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	edited, err := h.editTrustedFile(principal, "main.go", "func Alpha() int { return 1 }", "func Alpha() int { return 42 }")
	if err != nil || !strings.Contains(edited, "main.go") || strings.Contains(edited, workspace) {
		t.Fatalf("edit=%q err=%v", edited, err)
	}
	for _, leaked := range []string{"edit_file", "edit_lines", "write_file"} {
		if strings.Contains(edited, leaked) {
			t.Fatalf("edit leaked legacy tool name %q: %q", leaked, edited)
		}
	}
	data, err := os.ReadFile(filepath.Join(workspace, "main.go"))
	if err != nil || !strings.Contains(string(data), "return 42") {
		t.Fatalf("edited file=%q err=%v", data, err)
	}
	// The rest of the file survives: this is the property a whole-file rewrite
	// cannot promise.
	if !strings.Contains(string(data), "func Beta() int { return 1 }") || !strings.Contains(string(data), "package main") {
		t.Fatalf("edit rewrote more than the matched passage: %q", data)
	}

	if _, err := h.editTrustedFile(principal, "main.go", "int", "int64"); err == nil || !strings.Contains(err.Error(), "trusted_file_edit_ambiguous_match") {
		t.Fatalf("ambiguous passage err=%v", err)
	}
	if _, err := h.editTrustedFile(principal, "main.go", "func Gamma()", "func Delta()"); err == nil || !strings.Contains(err.Error(), "trusted_file_edit_no_match") {
		t.Fatalf("absent passage err=%v", err)
	}
	if _, err := h.editTrustedFile(principal, "missing.go", "a", "b"); err == nil || !strings.Contains(err.Error(), "trusted_file_edit_not_found") {
		t.Fatalf("edit must not create files err=%v", err)
	}
	if _, err := h.editTrustedFile(principal, "main.go", "", "b"); err == nil || !strings.Contains(err.Error(), "trusted_file_edit_old_string_required") {
		t.Fatalf("empty old_string err=%v", err)
	}
	if _, err := h.editTrustedFile("", "main.go", "a", "b"); err == nil || !strings.Contains(err.Error(), "trusted_file_write_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
	if _, err := h.editTrustedFile(principal, `..\escape.txt`, "a", "b"); err == nil || !strings.Contains(err.Error(), "trusted_file_write_path_rejected") {
		t.Fatalf("escape path err=%v", err)
	}

	// Deleting a passage is a replacement with empty text, not a separate mode.
	if _, err := h.editTrustedFile(principal, "main.go", "\nfunc Beta() int { return 1 }", ""); err != nil {
		t.Fatalf("delete via empty new_string err=%v", err)
	}
	data, err = os.ReadFile(filepath.Join(workspace, "main.go"))
	if err != nil || strings.Contains(string(data), "Beta") {
		t.Fatalf("passage was not removed: %q err=%v", data, err)
	}
}

func TestSemanticFileWriteArgsRouteByFieldPresence(t *testing.T) {
	for _, args := range []map[string]interface{}{
		{"path": "a.go", "old_string": "x"},
		{"path": "a.go", "new_string": "y"},
		{"path": "a.go", "old_string": "x", "new_string": "y", "content": "whole"},
		{"path": "a.go", "old_string": "x", "new_string": "y", "mode": "append"},
		{"path": "a.go", "old_string": "", "new_string": "y"},
		{"path": "a.go"},
		{"path": "a.go", "content": "c", "replace_all": "true"},
		{"path": "a.go", "old_string": 7, "new_string": "y"},
	} {
		if _, err := semanticTrustedFileWriteArgsAllowed(args); err == nil {
			t.Fatalf("arguments %v were accepted by the closed set", args)
		}
	}

	edit, err := semanticTrustedFileWriteArgsAllowed(map[string]interface{}{
		"path": " main.go ", "old_string": "  spaced  ", "new_string": "  respaced  ",
	})
	if err != nil || !edit.edit || edit.path != "main.go" {
		t.Fatalf("edit request=%+v err=%v", edit, err)
	}
	// Whitespace is meaningful in the matched and inserted text, so neither
	// side may be trimmed the way path is.
	if edit.oldString != "  spaced  " || edit.newString != "  respaced  " {
		t.Fatalf("edit trimmed significant whitespace: %+v", edit)
	}

	write, err := semanticTrustedFileWriteArgsAllowed(map[string]interface{}{
		"path": "main.go", "content": "body", "mode": "append",
	})
	if err != nil || write.edit || write.content != "body" || write.mode != "append" {
		t.Fatalf("write request=%+v err=%v", write, err)
	}
}
