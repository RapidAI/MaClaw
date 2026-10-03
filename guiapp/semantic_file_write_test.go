package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
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

func TestMarkdownFileWritePlanningTextMapsBareContinue(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "生成markdown"},
		{Role: "assistant", Content: "回复继续即可"},
	}
	for _, msg := range []string{"继续", "继续。", "请继续", "重试", "continue", "继续…"} {
		if got := markdownFileWritePlanningText(msg, history); got != "生成markdown" {
			t.Fatalf("msg=%q planning=%q, want 生成markdown", msg, got)
		}
	}
	if got := markdownFileWritePlanningText("今天天气怎么样", history); got != "今天天气怎么样" {
		t.Fatalf("non-continue must stay itself, got %q", got)
	}
	weatherHistory := []agent.ConversationEntry{{Role: "user", Content: "今天天气怎么样"}}
	if got := markdownFileWritePlanningText("继续", weatherHistory); got != "继续" {
		t.Fatalf("continue after weather must stay 继续, got %q", got)
	}
	if got := markdownFileWritePlanningText(acpProgrammingUserText(`F:\个人介绍`, "继续"), weatherHistory); got != "继续" {
		t.Fatalf("ACP-wrapped continue after weather must stay 继续, got %q", got)
	}
	skippedContinues := []agent.ConversationEntry{
		{Role: "user", Content: "生成markdown"},
		{Role: "assistant", Content: "回复继续即可"},
		{Role: "user", Content: "继续"},
	}
	if got := markdownFileWritePlanningText("请继续", skippedContinues); got != "生成markdown" {
		t.Fatalf("continue after a cancelled continue must still find 生成markdown, got %q", got)
	}
	parts := []agent.ConversationEntry{
		{Role: "user", Content: []interface{}{map[string]interface{}{"type": "text", "text": "生成markdown"}}},
	}
	if got := markdownFileWritePlanningText("继续", parts); got != "生成markdown" {
		t.Fatalf("multipart user content must still map, got %q", got)
	}
	if got := markdownFileWritePlanningText("继续", []agent.ConversationEntry{
		{Role: "user", Content: map[string]interface{}{"markdown": true, "path": "notes.md"}},
	}); got != "继续" {
		t.Fatalf("struct-shaped content must not look like a markdown write, got %q", got)
	}
	wrappedContinue := acpProgrammingUserText(`F:\个人介绍`, "继续")
	if got := markdownFileWritePlanningText(wrappedContinue, history); got != "生成markdown" {
		t.Fatalf("ACP-wrapped 继续 must map onto 生成markdown, got %q", got)
	}
	wrappedMarkdown := acpProgrammingUserText(`F:\个人介绍`, "生成markdown")
	wrappedHistory := []agent.ConversationEntry{
		{Role: "user", Content: wrappedMarkdown},
		{Role: "assistant", Content: "回复继续即可"},
	}
	if got := markdownFileWritePlanningText(wrappedContinue, wrappedHistory); got != "生成markdown" {
		t.Fatalf("ACP-wrapped 继续 after ACP-wrapped 生成markdown must plan the inner write, got %q", got)
	}
	if got := markdownFileWritePlanningText(wrappedMarkdown, nil); got != "生成markdown" {
		t.Fatalf("ACP-wrapped 生成markdown leftover text must be the User request, got %q", got)
	}
	explain := acpProgrammingUserText(`F:\个人介绍`, "解释这段 markdown")
	if got := markdownFileWritePlanningText(explain, nil); got != "解释这段 markdown" {
		t.Fatalf("ACP wrapper must not mint a markdown-file write from ASCII write, got %q", got)
	}
	noPDF := []agent.ConversationEntry{
		{Role: "user", Content: "生成markdown，不要PDF"},
		{Role: "assistant", Content: "回复继续即可"},
	}
	if got := markdownFileWritePlanningText("继续", noPDF); got != "生成markdown，不要PDF" {
		t.Fatalf("继续 after 不要PDF markdown write must still map, got %q", got)
	}
}

func TestSemanticMarkdownFileWriteWeakTreePlansWriteFile(t *testing.T) {
	// Production 2026-09-21: tree file_write 0.55 after L2 document_generate
	// 0.84 missed the 0.70 floor; leftover ranked generate_pdf #1.
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileWrite)}
	h.semanticTrustedFileWrite = func(string, string, string, string) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "生成markdown", "desktop", "root-md-weak", "turn-md-weak",
		&intent.ClassificationResult{
			Primary: intent.LabelFileWrite, Confidence: 0.55, Layer: 3,
			Reason: "tree-after-embedding: file_write (0.550)",
		},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("markdown-file query must plan at tree 0.55: handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(surface.plan, tool.CapabilityFSWriteLocal) {
		t.Fatalf("selections=%#v, want fs.write.local", surface.plan.Selections)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter) != "write_file" {
		t.Fatalf("model name=%q, want write_file", semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter))
	}
	for _, def := range defs {
		switch extractToolName(def) {
		case "generate_pdf":
			t.Fatal("generate_pdf must not appear for 生成markdown")
		}
	}
}

func TestSemanticMarkdownQueryRedirectsDocumentGenerateToFileWrite(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	h.semanticTrustedFileWrite = func(string, string, string, string) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "生成markdown", "desktop", "root-md-l2", "turn-md-l2",
		&intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.84, Layer: 2},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("L2 document_generate + 生成markdown must plan file_write: handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(surface.plan, tool.CapabilityFSWriteLocal) {
		t.Fatalf("selections=%#v, want fs.write.local", surface.plan.Selections)
	}
	if planHasCapabilities(surface.plan, "document.generate.file") {
		t.Fatalf("markdown-file query must not mint generate_pdf: %#v", surface.plan.Selections)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter) != "write_file" {
		t.Fatalf("model name=%q, want write_file", semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter))
	}
	for _, def := range defs {
		if extractToolName(def) == "generate_pdf" {
			t.Fatal("generate_pdf must not be listed for 生成markdown")
		}
	}
}

func TestSemanticMarkdownQueryDoesNotHostRejectGenerateDeliveryConflict(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	h.semanticTrustedFileWrite = func(string, string, string, string) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "生成markdown", "desktop", "root-md-conflict", "turn-md-conflict",
		&intent.ClassificationResult{
			Primary:    intent.LabelDocumentGenerate,
			Secondary:  []intent.IntentLabel{intent.LabelAttachmentDelivery},
			Confidence: 0.9,
			Layer:      2,
		},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("markdown-file query must not inherit generate+delivery HostReject: handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(surface.plan, tool.CapabilityFSWriteLocal) {
		t.Fatalf("selections=%#v, want fs.write.local", surface.plan.Selections)
	}
}

func TestSemanticContinueAfterMarkdownPlansWriteFile(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedFileWrite = func(string, string, string, string) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	ctx := &LoopContext{
		History: []agent.ConversationEntry{
			{Role: "user", Content: "生成markdown"},
			{Role: "assistant", Content: "回复继续即可"},
		},
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{
				Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3,
			},
		},
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndAttachments(ctx, "user-1", "继续。", "desktop", nil)
	if err != nil || !handled || surface == nil {
		t.Fatalf("继续。 after 生成markdown must plan file_write: handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(surface.plan, tool.CapabilityFSWriteLocal) {
		t.Fatalf("selections=%#v, want fs.write.local", surface.plan.Selections)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter) != "write_file" {
		t.Fatalf("model name=%q, want write_file", semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter))
	}
	for _, def := range defs {
		if extractToolName(def) == "generate_pdf" {
			t.Fatal("generate_pdf must not appear when continuing a markdown-file write")
		}
	}
	wrappedContinue := acpProgrammingUserText(`F:\个人介绍`, "请继续")
	wrappedDefs, wrappedSurface, wrappedHandled, wrappedErr := h.semanticCallSurfaceForSharedTurnWithContextAndAttachments(ctx, "user-1", wrappedContinue, "desktop", nil)
	if wrappedErr != nil || !wrappedHandled || wrappedSurface == nil {
		t.Fatalf("ACP-wrapped 请继续 after 生成markdown must plan file_write: handled=%v err=%v", wrappedHandled, wrappedErr)
	}
	if !planHasCapabilities(wrappedSurface.plan, tool.CapabilityFSWriteLocal) {
		t.Fatalf("ACP-wrapped continue selections=%#v, want fs.write.local", wrappedSurface.plan.Selections)
	}
	if semanticGrantNameForAdapter(wrappedSurface, semanticTrustedFileWriteAdapter) != "write_file" {
		t.Fatalf("ACP-wrapped continue model name=%q, want write_file", semanticGrantNameForAdapter(wrappedSurface, semanticTrustedFileWriteAdapter))
	}
	for _, def := range wrappedDefs {
		if extractToolName(def) == "generate_pdf" {
			t.Fatal("generate_pdf must not appear when ACP-wrapped continue follows a markdown-file write")
		}
	}
}

func TestSemanticContinueAfterWeatherDoesNotPlanFileWrite(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	ctx := &LoopContext{
		History: []agent.ConversationEntry{
			{Role: "user", Content: "今天天气怎么样"},
			{Role: "assistant", Content: "晴"},
		},
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{
				Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3,
			},
		},
	}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndAttachments(ctx, "user-1", "继续", "desktop", nil)
	if err != nil || handled || surface != nil {
		t.Fatalf("继续 after weather must not mint write_file: handled=%v err=%v surface=%#v", handled, err, surface)
	}
}

func TestSemanticLocalFileDeletePlansDeleteFile(t *testing.T) {
	// Production 2026-09-26 18:30: 「删除刚才的markdown文件」was tree file_write
	// 0.60. Leftover ranked write_file; the model called it three times, then
	// discover_tool, then petitioned "shell". Deletion is its own outcome.
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "删除刚才的markdown文件", "desktop", "root-delete-md", "turn-delete-md",
		&intent.ClassificationResult{
			Primary: intent.LabelFileWrite, Confidence: 0.60, Layer: 3,
			Reason: "tree-after-embedding: file_write (0.600)",
		},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("local file delete must plan delete_file at tree file_write 0.60: handled=%v err=%v", handled, err)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedFileDeleteAdapter) != "delete_file" {
		t.Fatalf("model name=%q, want delete_file", semanticGrantNameForAdapter(surface, semanticTrustedFileDeleteAdapter))
	}
	if !planHasCapabilities(surface.plan, tool.CapabilityFSDeleteLocal) {
		t.Fatalf("selections=%#v, want fs.delete.local", surface.plan.Selections)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter) != "" {
		t.Fatalf("write_file must stay off a delete plan, got %q", semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter))
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedShellAdapter) != "" {
		t.Fatalf("bash must stay off a delete plan, got %q", semanticGrantNameForAdapter(surface, semanticTrustedShellAdapter))
	}
	seenDelete := false
	for _, def := range defs {
		switch name := extractToolName(def); name {
		case "delete_file":
			seenDelete = true
			function, _ := def["function"].(map[string]interface{})
			desc, _ := function["description"].(string)
			if !strings.Contains(desc, "Do not rewrite it with write_file") {
				t.Fatalf("delete_file description=%q", desc)
			}
		case "write_file", "bash", "discover_tool", "generate_pdf":
			t.Fatalf("%s must not be listed for a local file delete", name)
		}
	}
	if !seenDelete {
		t.Fatal("delete_file was not rendered")
	}
}

func TestSemanticWeakTreeFileWriteWithoutMarkdownStillMisses(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "保存 notes.txt", "desktop", "root-weak-write", "turn-weak-write",
		&intent.ClassificationResult{
			Primary: intent.LabelFileWrite, Confidence: 0.55, Layer: 3,
			Reason: "tree-after-embedding: file_write (0.550)",
		},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("tree file_write 0.55 without markdown evidence must miss: handled=%v err=%v surface=%#v", handled, err, surface)
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
	_, escapeErr := h.writeTrustedFile(principal, `..\escape.txt`, "nope", "")
	if escapeErr == nil || !strings.Contains(escapeErr.Error(), "trusted_file_write_path_rejected") {
		t.Fatalf("escape path err=%v", escapeErr)
	}
	base, absErr := filepath.Abs(workspace)
	if absErr != nil {
		t.Fatal(absErr)
	}
	base = normalizeProjectSessionPath(base)
	if !strings.Contains(escapeErr.Error(), base) {
		t.Fatalf("rejection must name the writable dir %q: %v", base, escapeErr)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(workspace), "escape.txt")); err == nil {
		t.Fatal("escaped write must not create a sibling file")
	}
}

func TestTrustedFileWriteResolvePathAcceptsWindowsDriveLetterCase(t *testing.T) {
	workspace := t.TempDir()
	base, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	base = normalizeProjectSessionPath(base)
	got, err := trustedFileWriteResolvePath(base, "notes.txt")
	if err != nil {
		t.Fatalf("relative write err=%v", err)
	}
	if !pathContainedInBase(got, base) {
		t.Fatalf("resolved %q outside %q", got, base)
	}
	if len(base) < 2 || base[1] != ':' {
		return
	}
	mixed := strings.ToLower(base[:1]) + base[1:]
	cased, err := trustedFileWriteResolvePath(base, filepath.Join(mixed, "cased.txt"))
	if err != nil {
		t.Fatalf("drive-letter case write err=%v", err)
	}
	if !pathContainedInBase(cased, base) {
		t.Fatalf("cased path %q outside %q", cased, base)
	}
	if got := trustedFileWriteDisplayPath(mixed, cased, "raw"); got != "cased.txt" {
		t.Fatalf("display path with mixed drive letter = %q, want cased.txt", got)
	}
}

func TestTrustedFileWriteResolvePathRejectsHomeTildeWithWorkspaceHint(t *testing.T) {
	workspace := t.TempDir()
	base, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	base = normalizeProjectSessionPath(base)
	_, err = trustedFileWriteResolvePath(base, "~/escape.svg")
	if err == nil || !strings.Contains(err.Error(), "trusted_file_write_path_rejected") || !strings.Contains(err.Error(), "writes stay inside") {
		t.Fatalf("home-tilde write must name the writable dir, err=%v", err)
	}
	if strings.Contains(err.Error(), base) == false {
		t.Fatalf("rejection must include %q: %v", base, err)
	}
}

func TestTrustedPrincipalBoundWorkspacePrefersBotWorkingDirectory(t *testing.T) {
	botDir := t.TempDir()
	mainDir := t.TempDir()
	app := newProjectSearchTestApp(t)
	if err := app.SetTabWorkingDir("", mainDir); err != nil {
		t.Fatalf("SetTabWorkingDir: %v", err)
	}
	owner := "lansenger-bot-user"
	assistantBindingByUserID.Store(owner, &assistantBindingTurnScope{
		binding: agent.AssistantBinding{WorkingDirectory: botDir},
	})
	t.Cleanup(func() { assistantBindingByUserID.Delete(owner) })
	h := &IMMessageHandler{app: app}
	if got := trustedPrincipalBoundWorkspace(h, owner); got != filepath.Clean(botDir) {
		t.Fatalf("bot trusted workspace = %q, want %q (not main %q)", got, botDir, mainDir)
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

func TestTrustedWriteOfExistingTemplateStillOpensPreview(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "elsarticle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tex := filepath.Join(dir, "elsarticle-template-num.tex")
	if err := os.WriteFile(tex, []byte("\\documentclass{elsarticle}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	var got *CodeFileEvent
	app.codePreviewEventObserver = func(evt CodeFileEvent) {
		copied := evt
		got = &copied
	}
	app.codeEventEmitter = NewCodeEventEmitter(app)
	h := &IMMessageHandler{app: app}
	principal := desktopUserID + ":" + workspace
	written, err := h.writeTrustedFile(principal, "elsarticle/elsarticle-template-num.tex", "\\documentclass{elsarticle}\n\\begin{document}paper\\end{document}\n", "overwrite")
	if err != nil || !strings.Contains(written, "Written to") {
		t.Fatalf("write=%q err=%v", written, err)
	}
	if got == nil {
		t.Fatal("rewriting an exported template emitted no preview")
	}
	if got.OpType != "modify" || !got.ForceOpen {
		t.Fatalf("preview=%+v, want a forced modify of the existing paper", *got)
	}
	if !strings.Contains(got.FilePath, "elsarticle-template-num.tex") {
		t.Fatalf("preview path=%q", got.FilePath)
	}
	if got.Original == "" || !strings.Contains(got.Content, "paper") {
		t.Fatalf("preview lost the before/after text: %+v", *got)
	}
	got = nil
	created, err := h.writeTrustedFile(principal, "elsarticle/chapter.tex", "\\section{New}\n", "")
	if err != nil || !strings.Contains(created, "Written to") {
		t.Fatalf("create=%q err=%v", created, err)
	}
	if got == nil || got.OpType != "create" || !got.ForceOpen || !strings.Contains(got.FilePath, "chapter.tex") {
		t.Fatalf("new tex preview=%v", got)
	}
}
