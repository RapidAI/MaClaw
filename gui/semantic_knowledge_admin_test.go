package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func knowledgeAdminClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelKnowledgeAdmin,
		Confidence: .98,
		ToolNames:  []string{"knowledge_maintain", "knowledge_doctor", "knowledge_health"},
	}
}

func TestIMSemanticKnowledgeAdminUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelKnowledgeAdmin)}
	h.semanticTrustedKnowledgeAdmin = func(userID, id, status string, refresh, hasRefresh bool) (string, error) {
		t.Fatalf("planning must not execute the administrator user=%q id=%q", userID, id)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(h.registry, app)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "管理知识库", "lansenger", "root-kadmin", "turn-kadmin", knowledgeAdminClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedKnowledgeAdminAdapter || selection.FitProof.MatchedCapability != tool.CapabilityKnowledgeAdminMaintenance {
		t.Fatalf("selection=%+v", selection)
	}
	if !semanticSelectionRequiresReceipt(selection) || !semanticBuiltinLocalMutationSelection(selection) {
		t.Fatalf("knowledge admin must use the local mutation receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "knowledge_maintain",
		"knowledge_doctor", "knowledge_health", "knowledge_stats", "knowledge_share_to_hub")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["id"]; !ok || len(properties) != 3 {
		t.Fatalf("knowledge admin schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"action", "source_id", "query", "text", "url", "path", "labels", "snapshot",
		"channel", "destination", "group_name", "health", "doctor", "quality",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing knowledge admin schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedKnowledgeAdminAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"action":"maintain","source_id":"s1"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged admin fields=%q", got)
	}
}

func TestIMSemanticKnowledgeAdminExecutesFieldPresenceWithoutKeywordBranch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelKnowledgeAdmin)}
	var seenID, seenStatus string
	var seenRefresh, seenHasRefresh bool
	h.semanticTrustedKnowledgeAdmin = func(userID, id, status string, refresh, hasRefresh bool) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seenID, seenStatus, seenRefresh, seenHasRefresh = id, status, refresh, hasRefresh
		return "知识来源已更新: notes [disabled] ID=s1", nil
	}
	registerKnowledgeTools(h.registry, &App{testHomeDir: t.TempDir()})
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "知识库健康检查", "lansenger", "root-kadmin-exec", "turn-kadmin-exec", knowledgeAdminClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"id":"s1","status":"disabled"}`)
	if !strings.Contains(got, "知识来源已更新") || strings.Contains(got, "knowledge_doctor") || strings.Contains(got, "quality") {
		t.Fatalf("bound admin=%q", got)
	}
	if seenID != "s1" || seenStatus != "disabled" || seenRefresh || seenHasRefresh {
		t.Fatalf("dispatch id=%q status=%q refresh=%v hasRefresh=%v", seenID, seenStatus, seenRefresh, seenHasRefresh)
	}
	if replay := cb.ExecuteTool(name, `{"id":"s1","status":"disabled"}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticKnowledgeAdminRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelKnowledgeAdmin)}
	h.semanticTrustedKnowledgeAdmin = func(string, string, string, bool, bool) (string, error) {
		return "[file_base64|application/pdf]AAAA", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "管理知识库", "lansenger", "root-kadmin-token", "turn-kadmin-token", knowledgeAdminClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"id":"s1","refresh":false}`); !strings.Contains(got, "trusted_knowledge_admin_field_presence_rejected") {
		t.Fatalf("refresh=false=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "管理知识库", "lansenger", "root-kadmin-token-2", "turn-kadmin-token-2", knowledgeAdminClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "trusted_knowledge_admin_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.administerTrustedKnowledge("", "", "", false, false); err == nil || !strings.Contains(err.Error(), "trusted_knowledge_admin_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

func TestIMSemanticKnowledgeAdminScopesStoreToPrincipal(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := os.MkdirAll(filepath.Dir(app.knowledgeDBPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := knowledge.NewSQLiteStore(app.knowledgeDBPath())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.SaveSource(ctx, knowledge.Source{
		ID: "s-own", Kind: knowledge.SourceKindText, URI: "memory://own", Title: "notes",
		OwnerID: "user-1", Status: knowledge.StatusParsed,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSource(ctx, knowledge.Source{
		ID: "s-other", Kind: knowledge.SourceKindText, URI: "memory://other", Title: "foreign",
		OwnerID: "user-2", Status: knowledge.StatusParsed,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSource(ctx, knowledge.Source{
		ID: "s-local", Kind: knowledge.SourceKindText, URI: "memory://local", Title: "desktop notes",
		Status: knowledge.StatusParsed,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	h := &IMMessageHandler{app: app}
	listed, err := h.administerTrustedKnowledge("user-1", "", "", false, false)
	if err != nil || !strings.Contains(listed, "s-own") || strings.Contains(listed, "foreign") || strings.Contains(listed, "s-other") || strings.Contains(listed, "s-local") {
		t.Fatalf("im list=%q err=%v", listed, err)
	}
	if strings.Contains(listed, "knowledge_stats") || strings.Contains(listed, "doctor") {
		t.Fatalf("list leaked admin soup: %q", listed)
	}
	desktopListed, err := h.administerTrustedKnowledge(desktopUserID, "", "", false, false)
	if err != nil || !strings.Contains(desktopListed, "s-local") || strings.Contains(desktopListed, "s-own") || strings.Contains(desktopListed, "foreign") {
		t.Fatalf("desktop list=%q err=%v", desktopListed, err)
	}
	got, err := h.administerTrustedKnowledge("user-1", "s-own", "", false, false)
	if err != nil || !strings.Contains(got, "s-own") {
		t.Fatalf("get=%q err=%v", got, err)
	}
	if _, err := h.administerTrustedKnowledge("user-1", "s-other", "disabled", false, false); err == nil || !strings.Contains(err.Error(), "trusted_knowledge_admin_not_found") {
		t.Fatalf("foreign write err=%v", err)
	}
	if _, err := h.administerTrustedKnowledge("user-1", "s-local", "disabled", false, false); err == nil || !strings.Contains(err.Error(), "trusted_knowledge_admin_not_found") {
		t.Fatalf("im must not mutate host-local source err=%v", err)
	}
	updated, err := h.administerTrustedKnowledge("user-1", "s-own", "disabled", false, false)
	if err != nil || !strings.Contains(updated, "知识来源已更新") {
		t.Fatalf("disable=%q err=%v", updated, err)
	}
	localUpdated, err := h.administerTrustedKnowledge(desktopUserID, "s-local", "disabled", false, false)
	if err != nil || !strings.Contains(localUpdated, "知识来源已更新") {
		t.Fatalf("desktop host-local disable=%q err=%v", localUpdated, err)
	}
}
