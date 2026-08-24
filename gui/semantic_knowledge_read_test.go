package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func knowledgeReadClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelKnowledgeRead,
		Confidence: .98,
		ToolNames:  []string{"knowledge_search", "knowledge_context_pack", "knowledge_explain"},
	}
}

func TestIMSemanticKnowledgeReadUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelKnowledgeRead)}
	h.semanticTrustedKnowledgeRead = func(userID, query string) (string, error) {
		t.Fatalf("planning must not execute read user=%q query=%q", userID, query)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	registerKnowledgeTools(h.registry, &App{testHomeDir: t.TempDir()})
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "知识库里有没有这段", "lansenger", "root-kread", "turn-kread", knowledgeReadClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedKnowledgeReadAdapter || selection.FitProof.MatchedCapability != tool.CapabilityKnowledgeReadLocal {
		t.Fatalf("selection=%+v", selection)
	}
	if semanticSelectionRequiresReceipt(selection) {
		t.Fatalf("read-only knowledge must not require a receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "knowledge_search",
		"knowledge_image_search", "knowledge_explain", "knowledge_context_pack", "knowledge_search_facets", "knowledge_suggest")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["query"]; !ok || len(properties) != 1 {
		t.Fatalf("knowledge read schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"search_scope", "topic_hint", "source_ids", "ids", "labels", "domain",
		"project_path", "limit", "include_disabled", "channel", "destination",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing knowledge read schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedKnowledgeReadAdapter, `{"query":"notes"}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"query":"notes","search_scope":"all","project_path":"C:/src"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged read fields=%q", got)
	}
}

func TestIMSemanticKnowledgeReadExecutesQueryWithoutKeywordBranch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelKnowledgeRead)}
	var seenQuery string
	h.semanticTrustedKnowledgeRead = func(userID, query string) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seenQuery = query
		return "Found 1 knowledge hits:\n1. owned notes", nil
	}
	registerKnowledgeTools(h.registry, &App{testHomeDir: t.TempDir()})
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "知识库健康检查后搜一下这段", "lansenger", "root-kread-exec", "turn-kread-exec", knowledgeReadClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"query":"owned notes"}`)
	if !strings.Contains(got, "owned notes") || strings.Contains(got, "knowledge_search") || strings.Contains(got, "search_scope") {
		t.Fatalf("bound read=%q", got)
	}
	if seenQuery != "owned notes" {
		t.Fatalf("dispatch query=%q", seenQuery)
	}
	if replay := cb.ExecuteTool(name, `{"query":"owned notes"}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticKnowledgeReadRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelKnowledgeRead)}
	h.semanticTrustedKnowledgeRead = func(string, string) (string, error) {
		return "[file_base64|text/plain]AAAA", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "知识库里有没有这段", "lansenger", "root-kread-token", "turn-kread-token", knowledgeReadClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "parameter_required_field_missing") && !strings.Contains(got, "trusted_knowledge_read_query_required") && !strings.Contains(got, "trusted_knowledge_read_arguments_rejected") {
		t.Fatalf("empty object=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "知识库里有没有这段", "lansenger", "root-kread-token-2", "turn-kread-token-2", knowledgeReadClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"query":"notes"}`); !strings.Contains(got, "trusted_knowledge_read_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.readTrustedKnowledge("", "notes"); err == nil || !strings.Contains(err.Error(), "trusted_knowledge_read_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
	if _, err := h.readTrustedKnowledge("user-1", ""); err == nil || !strings.Contains(err.Error(), "trusted_knowledge_read_query_required") {
		t.Fatalf("empty query err=%v", err)
	}
}

func TestIMSemanticKnowledgeReadScopesStoreToPrincipal(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := os.MkdirAll(filepath.Dir(app.knowledgeDBPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app}
	if _, err := h.ingestTrustedKnowledge("user-1", "owned notes for isolation search", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ingestTrustedKnowledge("user-2", "foreign notes must stay hidden", "", ""); err != nil {
		t.Fatal(err)
	}
	owned, err := h.readTrustedKnowledge("user-1", "owned notes for isolation search")
	if err != nil || !strings.Contains(owned, "owned notes") || strings.Contains(owned, "foreign notes") {
		t.Fatalf("user-1 search=%q err=%v", owned, err)
	}
	if strings.Contains(owned, "knowledge_search") || strings.Contains(owned, "knowledge_context_pack") || strings.Contains(owned, "[file_base64") {
		t.Fatalf("projection leaked soup: %q", owned)
	}
	foreign, err := h.readTrustedKnowledge("user-2", "owned notes for isolation search")
	if err != nil || strings.Contains(foreign, "owned notes") {
		t.Fatalf("user-2 search=%q err=%v", foreign, err)
	}
}
