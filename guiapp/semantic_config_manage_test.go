package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func configManageClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelConfigManage,
		Confidence: .98,
		ToolNames:  []string{"manage_config", "switch_llm_provider", "set_max_iterations"},
	}
}

func TestIMSemanticConfigUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelConfigManage)}
	h.semanticTrustedConfig = func(userID string, maxIterations int, hasMax bool, thinkingMode string, hasThinking bool) (string, error) {
		t.Fatalf("planning must not execute the manager user=%q", userID)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看当前配置", "lansenger", "root-config", "turn-config", configManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedConfigAdapter || selection.FitProof.MatchedCapability != tool.CapabilityConfigManageSelf {
		t.Fatalf("selection=%+v", selection)
	}
	if !semanticSelectionRequiresReceipt(selection) || !semanticBuiltinLocalMutationSelection(selection) {
		t.Fatalf("config must use the local mutation receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "manage_config",
		"switch_llm_provider", "set_max_iterations", "manage_user_model", "get_config", "export_config")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["max_iterations"]; !ok || len(properties) != 2 {
		t.Fatalf("config schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"action", "provider", "url", "key", "model", "llm_vendor", "llm_url", "llm_key", "llm_model",
		"channel", "destination", "export", "import", "section", "json_data", "profile",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing config schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedConfigAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"provider":"zhipu","action":"set"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged provider fields=%q", got)
	}
}

func TestIMSemanticConfigExecutesFieldPresenceWithoutProviderSwitch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelConfigManage)}
	var seenMax int
	var seenHasMax, seenHasThinking bool
	var seenMode string
	h.semanticTrustedConfig = func(userID string, maxIterations int, hasMax bool, thinkingMode string, hasThinking bool) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seenMax, seenHasMax, seenMode, seenHasThinking = maxIterations, hasMax, thinkingMode, hasThinking
		return "配置已更新。\n当前配置:\n- max_iterations: 50\n- thinking_mode: auto\nLLM 服务商由宿主管理，不能在此切换。", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "换智谱", "lansenger", "root-config-exec", "turn-config-exec", configManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"max_iterations":50}`)
	if !strings.Contains(got, "max_iterations: 50") || strings.Contains(got, "zhipu") || strings.Contains(got, "switch_llm_provider") {
		t.Fatalf("bound config=%q", got)
	}
	if !seenHasMax || seenMax != 50 || seenHasThinking || seenMode != "" {
		t.Fatalf("dispatch max=%d hasMax=%v mode=%q hasThinking=%v", seenMax, seenHasMax, seenMode, seenHasThinking)
	}
	if replay := cb.ExecuteTool(name, `{"max_iterations":50}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticConfigRejectsBothFieldsAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelConfigManage)}
	h.semanticTrustedConfig = func(string, int, bool, string, bool) (string, error) {
		return "当前配置:\n- max_iterations: 80\n- thinking_mode: disabled\nhttp://secret.example/v1 sk-secret", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看当前配置", "lansenger", "root-config-both", "turn-config-both", configManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"max_iterations":50,"thinking_mode":"disabled"}`); !strings.Contains(got, "trusted_config_field_presence_rejected") {
		t.Fatalf("both fields=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看当前配置", "lansenger", "root-config-secret", "turn-config-secret", configManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "trusted_config_secret_leaked") {
		t.Fatalf("secret projection=%q", got)
	}
	if _, err := h.administerTrustedConfig("", 0, false, "", false); err == nil || !strings.Contains(err.Error(), "trusted_config_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

func TestIMSemanticConfigPersistsSafeFieldsOnly(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	h := &IMMessageHandler{app: app}
	listed, err := h.administerTrustedConfig("user-1", 0, false, "", false)
	if err != nil || !strings.Contains(listed, "max_iterations") || !strings.Contains(listed, "thinking_mode") {
		t.Fatalf("get=%q err=%v", listed, err)
	}
	if strings.Contains(listed, "http") || strings.Contains(listed, "sk-") || strings.Contains(listed, "zhipu") {
		t.Fatalf("get leaked provider secrets: %q", listed)
	}
	updated, err := h.administerTrustedConfig("user-1", 50, true, "", false)
	if err != nil || !strings.Contains(updated, "50") {
		t.Fatalf("set iterations=%q err=%v", updated, err)
	}
	if got := app.GetMaclawAgentMaxIterations(); got != 50 {
		t.Fatalf("persisted iterations=%d", got)
	}
	if _, err := h.administerTrustedConfig("user-1", 10, true, "", false); err == nil || !strings.Contains(err.Error(), "trusted_config_max_iterations_rejected") {
		t.Fatalf("below min err=%v", err)
	}
	if got := app.GetMaclawAgentMaxIterations(); got != 50 {
		t.Fatalf("rejected write mutated iterations=%d", got)
	}
	if _, err := h.administerTrustedConfig("user-1", config.MaxAgentIterationsCap+1, true, "", false); err == nil {
		t.Fatal("above max must fail closed")
	}
	if _, err := h.administerTrustedConfig("user-1", 0, false, "zhipu", true); err == nil || !strings.Contains(err.Error(), "trusted_config_thinking_mode_rejected") {
		t.Fatalf("provider-as-thinking err=%v", err)
	}
	thinking, err := h.administerTrustedConfig("user-1", 0, false, "disabled", true)
	if err != nil || !strings.Contains(thinking, "disabled") {
		t.Fatalf("set thinking=%q err=%v", thinking, err)
	}
	if got := app.GetMaclawLLMThinkingMode(); got != "disabled" {
		t.Fatalf("persisted thinking=%q", got)
	}
}

// TestTrustedConfigThinkingModeVocabulary pins the drift fix: the GUI surface
// used to keep its own spelling list and refused "true", which the shared
// parser the agent-service surface uses accepts. After the delegation both
// hosts answer "which spellings exist" identically: canonical names advertised
// in the enum, legacy truthy/falsy aliases still accepted-but-unadvertised,
// auto still reading as unset. The refusal must name the canonical set so the
// first failure can redirect the turn instead of ending it.
func TestTrustedConfigThinkingModeVocabulary(t *testing.T) {
	schema := semanticTrustedConfigInvocationSchema()
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("config schema has no properties: %#v", schema)
	}
	thinking, ok := properties["thinking_mode"].(map[string]interface{})
	if !ok {
		t.Fatalf("config schema has no thinking_mode property: %#v", properties)
	}
	enum, ok := thinking["enum"].([]string)
	if !ok || len(enum) != 2 {
		t.Fatalf("thinking_mode must advertise the canonical pair, got %#v", thinking["enum"])
	}
	for _, name := range enum {
		mode, ok := semanticTrustedConfigThinkingMode(name)
		if !ok || mode != name {
			t.Fatalf("schema advertises mode %q, but the parser maps it to (%q, %v)", name, mode, ok)
		}
	}
	// The shared set: legacy spellings accepted on both hosts, not advertised.
	for _, sent := range []string{"true", "1", "false", "none", "on", "disable", "auto", ""} {
		if _, ok := semanticTrustedConfigThinkingMode(sent); !ok {
			t.Fatalf("enumless-but-valid spelling %q must stay accepted by the shared parser", sent)
		}
	}
	for _, invented := range []string{"zhipu", "high", "medium"} {
		_, err := administerConfigThinkingModeProbe(t, invented)
		if err == nil || !strings.Contains(err.Error(), "trusted_config_thinking_mode_rejected") {
			t.Fatalf("mode %q = %v, want trusted_config_thinking_mode_rejected", invented, err)
		}
		for _, valid := range corelib.GlobalThinkingModeOptions() {
			if !strings.Contains(err.Error(), valid) {
				t.Fatalf("refusal for %q = %v does not mention %q", invented, err, valid)
			}
		}
	}
}

// administerConfigThinkingModeProbe drives one invented mode through the real
// administer path, the same way a thinking-only update turn does: the app
// fixture lets the config manager exist so the parse and its refusal are the
// first rejection the turn hits.
func administerConfigThinkingModeProbe(t *testing.T, mode string) (string, error) {
	t.Helper()
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}
	return h.administerTrustedConfig("user-1", 0, false, mode, true)
}
