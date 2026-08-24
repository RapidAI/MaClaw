package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func memoryManageClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelMemoryManage,
		Confidence: .98,
		ToolNames:  []string{"memory"},
	}
}

func TestIMSemanticMemoryUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelMemoryManage)}
	h.semanticTrustedMemory = func(userID, content, query, id string) (string, error) {
		t.Fatalf("planning must not execute the manager user=%q content=%q", userID, content)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "记住我偏好中文", "lansenger", "root-memory", "turn-memory", memoryManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedMemoryAdapter || selection.FitProof.MatchedCapability != tool.CapabilityMemoryManageAgent {
		t.Fatalf("selection=%+v", selection)
	}
	if !semanticSelectionRequiresReceipt(selection) || !semanticBuiltinLocalMutationSelection(selection) {
		t.Fatalf("memory must use the local mutation receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "memory", "manage_skill")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["content"]; !ok || len(properties) != 3 {
		t.Fatalf("memory schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"action", "owner", "owner_id", "surgery", "themes", "apply", "path", "file_path",
		"project_path", "channel", "destination", "group_name", "tenant",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing memory schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedMemoryAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"action":"save","content":"x"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged action field=%q", got)
	}
}

func TestIMSemanticMemoryExecutesFieldPresenceWithoutActionSoup(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelMemoryManage)}
	var seenContent, seenQuery, seenID string
	h.semanticTrustedMemory = func(userID, content, query, id string) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seenContent, seenQuery, seenID = content, query, id
		return "Memory saved: prefer Chinese replies", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "记住我偏好中文", "lansenger", "root-memory-exec", "turn-memory-exec", memoryManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"content":"prefer Chinese replies"}`)
	if !strings.Contains(got, "Memory saved") || strings.Contains(got, "action") {
		t.Fatalf("bound memory=%q", got)
	}
	if seenContent != "prefer Chinese replies" || seenQuery != "" || seenID != "" {
		t.Fatalf("dispatch content=%q query=%q id=%q", seenContent, seenQuery, seenID)
	}
	if replay := cb.ExecuteTool(name, `{"content":"prefer Chinese replies"}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticMemoryRejectsCombinedFieldsAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelMemoryManage)}
	h.semanticTrustedMemory = func(string, string, string, string) (string, error) {
		return "Memory saved: x [file_base64:abc]", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "记住我偏好中文", "lansenger", "root-memory-both", "turn-memory-both", memoryManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"content":"x","query":"y"}`); !strings.Contains(got, "trusted_memory_content_xor_query_xor_id_or_empty_required") {
		t.Fatalf("combined fields=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "记住我偏好中文", "lansenger", "root-memory-token", "turn-memory-token", memoryManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "trusted_memory_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.administerTrustedMemory("", "", "", ""); err == nil || !strings.Contains(err.Error(), "trusted_memory_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

func TestIMSemanticMemoryIsolatesByPrincipalWithoutDesktopFallback(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("memory store: %v", err)
	}
	t.Cleanup(store.Stop)
	h := &IMMessageHandler{memoryStore: store}
	saved, err := h.administerTrustedMemory("user-1", "prefer Chinese replies", "", "")
	if err != nil || !strings.Contains(saved, "Memory saved") {
		t.Fatalf("save=%q err=%v", saved, err)
	}
	listed, err := h.administerTrustedMemory("user-1", "", "", "")
	if err != nil || !strings.Contains(listed, "prefer Chinese replies") {
		t.Fatalf("owner list=%q err=%v", listed, err)
	}
	other, err := h.administerTrustedMemory("user-2", "", "", "")
	if err != nil {
		t.Fatalf("other list err=%v", err)
	}
	if strings.Contains(other, "prefer Chinese replies") {
		t.Fatalf("user-2 saw user-1 memory: %q", other)
	}
	desktop, err := h.administerTrustedMemory("desktop-user", "", "", "")
	if err != nil {
		t.Fatalf("desktop list err=%v", err)
	}
	if strings.Contains(desktop, "prefer Chinese replies") {
		t.Fatalf("desktop-user fallback leaked user-1 memory: %q", desktop)
	}
}
