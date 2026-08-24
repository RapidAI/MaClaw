package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func knowledgeWriteClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelKnowledgeWrite,
		Confidence: .98,
		ToolNames:  []string{"knowledge_save_text", "knowledge_save_url", "knowledge_import_files"},
	}
}

func TestIMSemanticKnowledgeIngestUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelKnowledgeWrite)}
	h.semanticTrustedKnowledgeIngest = func(userID, text, url, path string) (string, error) {
		t.Fatalf("planning must not execute ingest user=%q text=%q url=%q path=%q", userID, text, url, path)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(h.registry, app)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "把这段话存进知识库", "lansenger", "root-kingest", "turn-kingest", knowledgeWriteClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedKnowledgeIngestAdapter || selection.FitProof.MatchedCapability != tool.CapabilityKnowledgeIngestLocal {
		t.Fatalf("selection=%+v", selection)
	}
	if !semanticSelectionRequiresReceipt(selection) || !semanticBuiltinLocalMutationSelection(selection) {
		t.Fatalf("knowledge ingest must use the local mutation receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "knowledge_save_text",
		"knowledge_save_url", "knowledge_save_urls", "knowledge_import_files", "knowledge_import_directory")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["text"]; !ok || len(properties) != 3 {
		t.Fatalf("knowledge ingest schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"action", "title", "labels", "save_scope", "distill_mode", "file_path",
		"query", "save_path", "urls", "channel", "destination", "group_name",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing knowledge ingest schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedKnowledgeIngestAdapter, `{"text":"notes"}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"action":"save","title":"notes","save_scope":"global"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged ingest fields=%q", got)
	}
}

func TestIMSemanticKnowledgeIngestExecutesFieldPresenceWithoutKeywordBranch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelKnowledgeWrite)}
	var seenText, seenURL, seenPath string
	h.semanticTrustedKnowledgeIngest = func(userID, text, url, path string) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seenText, seenURL, seenPath = text, url, path
		return "Text saved to knowledge base. Source ID: s1", nil
	}
	registerKnowledgeTools(h.registry, &App{testHomeDir: t.TempDir()})
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "知识库健康检查后把这段存进去", "lansenger", "root-kingest-exec", "turn-kingest-exec", knowledgeWriteClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"text":"remember this"}`)
	if !strings.Contains(got, "Text saved to knowledge base") || strings.Contains(got, "knowledge_save_text") || strings.Contains(got, "save_scope") {
		t.Fatalf("bound ingest=%q", got)
	}
	if seenText != "remember this" || seenURL != "" || seenPath != "" {
		t.Fatalf("dispatch text=%q url=%q path=%q", seenText, seenURL, seenPath)
	}
	if replay := cb.ExecuteTool(name, `{"text":"remember this"}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticKnowledgeIngestRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelKnowledgeWrite)}
	h.semanticTrustedKnowledgeIngest = func(string, string, string, string) (string, error) {
		return "[file_base64|application/pdf]AAAA", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "把这段话存进知识库", "lansenger", "root-kingest-token", "turn-kingest-token", knowledgeWriteClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"text":"a","url":"https://example.com"}`); !strings.Contains(got, "trusted_knowledge_ingest_text_xor_url_xor_path_required") {
		t.Fatalf("text+url=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "把这段话存进知识库", "lansenger", "root-kingest-token-2", "turn-kingest-token-2", knowledgeWriteClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"text":"notes"}`); !strings.Contains(got, "trusted_knowledge_ingest_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.ingestTrustedKnowledge("", "notes", "", ""); err == nil || !strings.Contains(err.Error(), "trusted_knowledge_ingest_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
	if _, err := h.ingestTrustedKnowledge("user-1", "", "", ""); err == nil || !strings.Contains(err.Error(), "trusted_knowledge_ingest_text_xor_url_xor_path_required") {
		t.Fatalf("empty object err=%v", err)
	}
}

func TestIMSemanticKnowledgeIngestScopesStoreToPrincipal(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := os.MkdirAll(filepath.Dir(app.knowledgeDBPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app}
	saved, err := h.ingestTrustedKnowledge("user-1", "owned notes for isolation", "", "")
	if err != nil || !strings.Contains(saved, "Text saved to knowledge base") || !strings.Contains(saved, "Source ID:") {
		t.Fatalf("save=%q err=%v", saved, err)
	}
	if strings.Contains(saved, "knowledge_save_text") || strings.Contains(saved, "[file_base64") {
		t.Fatalf("projection leaked soup: %q", saved)
	}
	listed, err := h.administerTrustedKnowledge("user-2", "", "", false, false)
	if err != nil || strings.Contains(listed, "owned notes") {
		t.Fatalf("user-2 list=%q err=%v", listed, err)
	}
	ownerListed, err := h.administerTrustedKnowledge("user-1", "", "", false, false)
	if err != nil || !strings.Contains(ownerListed, "owned notes") {
		t.Fatalf("user-1 list=%q err=%v", ownerListed, err)
	}
	if _, err := h.ingestTrustedKnowledge("user-1", "", "", `C:\Windows\System32\drivers\etc\hosts`); err == nil || !strings.Contains(err.Error(), "trusted_knowledge_ingest_path_unavailable") {
		t.Fatalf("empty workspace absolute path err=%v", err)
	}

	workspace := t.TempDir()
	notePath := filepath.Join(workspace, "notes.txt")
	if err := os.WriteFile(notePath, []byte("workspace note"), 0o644); err != nil {
		t.Fatal(err)
	}
	principal := desktopUserID + ":" + workspace
	imported, err := h.ingestTrustedKnowledge(principal, "", "", "notes.txt")
	if err != nil || !strings.Contains(imported, "imported to knowledge base") {
		t.Fatalf("path import=%q err=%v", imported, err)
	}
	if _, err := h.ingestTrustedKnowledge(principal, "", "", `..\escape.txt`); err == nil || !strings.Contains(err.Error(), "trusted_knowledge_ingest_path_rejected") {
		t.Fatalf("escape path err=%v", err)
	}
}
