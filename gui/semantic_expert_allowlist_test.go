package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestManagedSemanticIgnoresExpertToolNameAllowList(t *testing.T) {
	if expertToolNameAllowListAppliesToManagedSemantic() {
		t.Fatal("tool-name expert allow-lists must not reshape a managed semantic surface")
	}
	swapExpertStoreForTest(t)
	const expertID = "expert-name-allowlist"
	if err := defaultExpertStore.Save(ExpertDefinition{
		ID: expertID, Name: "Name-only expert", Tools: []string{"bash", "call_mcp_tool", "manage_skill"},
	}); err != nil {
		t.Fatal(err)
	}
	userID := expertSessionUserID(expertID)
	if def := expertDefForUserID(userID); def == nil || def.ID != expertID {
		t.Fatalf("expert session did not resolve: %#v", def)
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		userID, "北京天气", "desktop", "root-expert-allow", "turn-expert-allow",
		&intent.ClassificationResult{
			Primary: intent.LabelLiveData, Confidence: .98,
			ToolNames: []string{"bash", "call_mcp_tool", "manage_skill"},
		},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("managed search must still plan under a name-only expert, handled=%v err=%v defs=%#v", handled, err, defs)
	}
	for _, def := range defs {
		name := extractToolName(def)
		if name == "bash" || isLegacySemanticBypassName(name) {
			t.Fatalf("expert allow-list or ToolNames expanded the surface with %q", name)
		}
		if _, ok := surface.grants[name]; !ok {
			t.Fatalf("rendered name %q is not a CatalogRenderer grant", name)
		}
	}
	stripped := filterToolsForExpert(defs, expertDefForUserID(userID))
	if len(stripped) != 0 {
		t.Fatalf("legacy name filter would keep %#v; managed path must not apply it", stripped)
	}
	if len(defs) == 0 {
		t.Fatal("managed path ignored the planner surface")
	}
	poisoned := append(append([]map[string]interface{}{}, defs...),
		map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "bash"}},
		map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "call_mcp_tool"}},
	)
	closed := closedManagedSemanticDefinitions(poisoned, surface.grants)
	if len(closed) != len(defs) {
		t.Fatalf("closed count=%d want %d", len(closed), len(defs))
	}
	for _, def := range closed {
		name := extractToolName(def)
		if name == "bash" || isLegacySemanticBypassName(name) {
			t.Fatalf("allow-list union survived close: %q", name)
		}
	}
}