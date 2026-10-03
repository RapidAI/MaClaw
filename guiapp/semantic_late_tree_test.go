package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestClassifierTimeoutUnknownIgnoresChatProjection(t *testing.T) {
	timeout := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; tree classification unavailable (l2=workflow_task conf=0.73)",
		},
	}}
	if !classifierTimeoutUnknown(timeout) {
		t.Fatal("tree-timeout unknown must keep leftover web lookup")
	}

	projected := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; short lookup skipped tree (l2=live_data conf=0.61); chat projection",
		},
	}}
	if classifierTimeoutUnknown(projected) {
		t.Fatal("gate-7 chat projection must not grow leftover web lookup")
	}

	hint := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelLiveData, Confidence: 0.61, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; short lookup skipped tree (l2=live_data conf=0.61)",
		},
	}}
	if classifierTimeoutUnknown(hint) {
		t.Fatal("sub-floor lookup hint is not a classifier-timeout unknown")
	}

	generic := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true,
			Reason: "no matching capability family",
		},
	}}
	if classifierTimeoutUnknown(generic) {
		t.Fatal("non-timeout unknown must not pin leftover web lookup")
	}

	contradicted := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true,
			Reason: "tree verdict coding(0.950) contradicted by local leader workflow_task(0.730); keeping L2 hint",
		},
	}}
	if !classifierTimeoutUnknown(contradicted) {
		t.Fatal("local-contradicted collapse to unknown must keep leftover web lookup")
	}

	liveData := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelLiveData, Confidence: 0.61, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; short lookup skipped tree (l2=live_data conf=0.61)",
		},
	}}
	applySemanticChatProjection(liveData)
	markClassifierTimeoutLookup(liveData)
	if liveData.Runtime.ClassifierTimeoutLookup {
		t.Fatal("gate-7 chat projection must not set leftover web lookup")
	}
}

func TestPinClassifierTimeoutWebLookupNoopsWithoutFlag(t *testing.T) {
	h := &IMMessageHandler{}
	routed := []map[string]interface{}{toolDef("gui_click", "click", nil, nil)}
	catalog := []map[string]interface{}{
		toolDef("gui_click", "click", nil, nil),
		toolDef("web_search", "search the web", nil, nil),
	}
	got := h.pinClassifierTimeoutWebLookup("u1", &LoopContext{}, routed, catalog)
	if len(got) != 1 || extractToolName(got[0]) != "gui_click" {
		t.Fatalf("unmarked leftover must not change tools, got %#v", got)
	}
}

func TestPinClassifierTimeoutWebLookupAddsSearch(t *testing.T) {
	h := &IMMessageHandler{}
	catalog := []map[string]interface{}{
		toolDef("gui_click", "click", nil, nil),
		toolDef("web_search", "search the web", nil, nil),
		toolDef("web_fetch", "fetch a url", nil, nil),
		toolDef("download_file", "download", nil, nil),
	}
	routed := []map[string]interface{}{toolDef("gui_click", "click", nil, nil)}
	got := h.pinClassifierTimeoutWebLookup("u1", &LoopContext{Runtime: RuntimeContext{ClassifierTimeoutLookup: true}}, routed, catalog)
	names := toolNameSetForWorkflowFilterTest(got)
	if !names["web_search"] || !names["web_fetch"] {
		t.Fatalf("timeout leftover must pin web lookup, got %#v", names)
	}
	if names["download_file"] {
		t.Fatalf("timeout leftover must not pin download_file, got %#v", names)
	}

	already := []map[string]interface{}{
		toolDef("gui_click", "click", nil, nil),
		toolDef("web_search", "search the web", nil, nil),
		toolDef("web_fetch", "fetch a url", nil, nil),
	}
	unchanged := h.pinClassifierTimeoutWebLookup("u1", &LoopContext{Runtime: RuntimeContext{ClassifierTimeoutLookup: true}}, already, catalog)
	if len(unchanged) != 3 {
		t.Fatalf("already-present lookup tools must not be rewritten, got %d", len(unchanged))
	}
}

func TestAdoptLateTreeSemanticIntentUsesCachedSearch(t *testing.T) {
	uic := semanticClassifierForLabel(t, intent.LabelSearch)
	text := "长江学者申请后，一般研究项目执行几年？"
	userID := "desktop-user:cloud"
	warmed := uic.ClassifyContext(context.Background(), intent.MessageContext{Text: text, UserID: userID})
	if warmed.Primary != intent.LabelSearch || warmed.Degraded {
		t.Fatalf("warm cache = %+v, want search", warmed)
	}

	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: uic}
	registerBuiltinTools(h.registry, h)
	ctx := &LoopContext{Runtime: RuntimeContext{
		RequestID: "req-changjiang",
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; tree classification unavailable (l2=workflow_task conf=0.73)",
		},
		Execution: fullExecutionProfile("semantic classifier degraded"),
	}}
	if !h.adoptLateTreeSemanticIntent(ctx, userID, text, nil) {
		t.Fatal("late tree cache must be adopted on the current turn")
	}
	if ctx.Runtime.SemanticIntent == nil || ctx.Runtime.SemanticIntent.Primary != intent.LabelSearch || ctx.Runtime.SemanticIntent.Degraded {
		t.Fatalf("adopted intent = %+v, want search", ctx.Runtime.SemanticIntent)
	}
	if !strings.Contains(ctx.Runtime.Execution.Reason, "semantic capability-managed lookup") {
		t.Fatalf("execution profile = %+v, want managed lookup", ctx.Runtime.Execution)
	}

	structural := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; tree classification unavailable (l2=workflow_task conf=0.73)",
		},
		Execution: fullExecutionProfile("attachments present"),
	}}
	if !h.adoptLateTreeSemanticIntent(structural, userID, text, nil) {
		t.Fatal("late tree cache must still replace the degraded unknown")
	}
	if structural.Runtime.Execution.Reason != "attachments present" {
		t.Fatalf("structurally full profile overwritten: %+v", structural.Runtime.Execution)
	}

	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(ctx, userID, text, "desktop")
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("adopted search must plan a managed surface: defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter) != "web_search" {
		t.Fatalf("managed grant missing web_search: %#v", defs)
	}
}

func TestAdoptLateTreeProjectSearchKeepsLookupContinuation(t *testing.T) {
	uic := semanticClassifierForLabel(t, intent.LabelSearch)
	text := "octop中有sso登录功能吗？"
	userID := "desktop-user:restored-task"
	warmed := uic.ClassifyContext(context.Background(), intent.MessageContext{Text: text, UserID: userID})
	if warmed.Primary != intent.LabelSearch || warmed.Degraded {
		t.Fatalf("warm cache = %+v, want search", warmed)
	}
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: uic}
	h.semanticTrustedWebSearch = func(_, query string) (string, error) { return "found: " + query, nil }
	registerBuiltinTools(h.registry, h)
	h.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
	})
	ctx := &LoopContext{Runtime: RuntimeContext{
		RequestID: "req-late-lookup",
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; tree classification unavailable (l2=workflow_task conf=0.73)",
		},
		Execution: fullExecutionProfile("semantic classifier degraded"),
	}}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(ctx, userID, text, "desktop")
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("late project search must plan: defs=%d handled=%v err=%v", len(defs), handled, err)
	}
	profile := ctx.Runtime.Execution
	if profile.Reason != lookupContinuationReason || !profile.IsLight() || profile.ToolBudget != 0 {
		t.Fatalf("late project search must stay a lookup continuation, got %+v", profile)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedWebFetchAdapter) == "" {
		t.Fatal("late project search must publish web_fetch")
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedShellAdapter) != "bash" {
		t.Fatal("late project search must restore carried bash")
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedFileWriteAdapter) != "" {
		t.Fatal("late project search must not grow write_file")
	}
	h.recordSemanticExecutionSurface(userID, profile, defs)
	if tools := h.parentExecutionTools(userID); len(tools) != 1 || tools[0] != "bash" {
		t.Fatalf("late project search must keep the parent carry, got %v", tools)
	}
}

func TestAdoptLateTreeSemanticIntentSkipsKeptOfficeHint(t *testing.T) {
	uic := semanticClassifierForLabel(t, intent.LabelSearch)
	text := "生成庆祝生日会的PPT"
	userID := "user-office-hint"
	warmed := uic.ClassifyContext(context.Background(), intent.MessageContext{Text: text, UserID: userID})
	if warmed.Primary != intent.LabelSearch || warmed.Degraded {
		t.Fatalf("warm cache = %+v, want search", warmed)
	}
	h := &IMMessageHandler{unifiedClassifier: uic}
	ctx := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelOffice, Confidence: 0.75, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; tree classification unavailable (l2=office conf=0.75)",
		},
	}}
	if h.adoptLateTreeSemanticIntent(ctx, userID, text, nil) {
		t.Fatal("kept office hint must not be replaced by a late search verdict")
	}
	if ctx.Runtime.SemanticIntent.Primary != intent.LabelOffice {
		t.Fatalf("intent mutated: %+v", ctx.Runtime.SemanticIntent)
	}
}

func TestAdoptLateTreeSemanticIntentSkipsDegradedCache(t *testing.T) {
	h := &IMMessageHandler{unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSearch)}
	ctx := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelLiveData, Confidence: 0.61, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; short lookup skipped tree (l2=live_data conf=0.61)",
		},
	}}
	if h.adoptLateTreeSemanticIntent(ctx, "user-1", "北京天所", nil) {
		t.Fatal("sub-floor lookup must not be replaced without a non-degraded cache hit")
	}
	if ctx.Runtime.SemanticIntent.Primary != intent.LabelLiveData {
		t.Fatalf("intent mutated: %+v", ctx.Runtime.SemanticIntent)
	}
}

func TestLateTreeCurrentTurnAdoptAllowed(t *testing.T) {
	if !lateTreeCurrentTurnAdoptAllowed(intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.88}) {
		t.Fatal("search must be adoptable on the current turn")
	}
	if !lateTreeCurrentTurnAdoptAllowed(intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.92}) {
		t.Fatal("office must be adoptable on the current turn")
	}
	if lateTreeCurrentTurnAdoptAllowed(intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: 0.95}) {
		t.Fatal("coding must stay cached for a resend, not lock this turn")
	}
	if lateTreeCurrentTurnAdoptAllowed(intent.ClassificationResult{Primary: intent.LabelBrowser, Confidence: 0.90}) {
		t.Fatal("browser must stay cached for a resend, not lock this turn")
	}
}

func TestAdoptLateTreeSemanticIntentSkipsCodingCache(t *testing.T) {
	uic := semanticClassifierForLabel(t, intent.LabelCoding)
	text := "长江学者申请后，一般研究项目执行几年？"
	userID := "user-coding-cache"
	warmed := uic.ClassifyContext(context.Background(), intent.MessageContext{Text: text, UserID: userID})
	if warmed.Primary != intent.LabelCoding || warmed.Degraded {
		t.Fatalf("warm cache = %+v, want coding", warmed)
	}
	h := &IMMessageHandler{unifiedClassifier: uic}
	ctx := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; tree classification unavailable (l2=workflow_task conf=0.73)",
		},
	}}
	if h.adoptLateTreeSemanticIntent(ctx, userID, text, nil) {
		t.Fatal("mutating late-tree coding must not replace the in-flight timeout unknown")
	}
	if ctx.Runtime.SemanticIntent.Primary != intent.LabelUnknown {
		t.Fatalf("intent mutated: %+v", ctx.Runtime.SemanticIntent)
	}
}

func TestAdoptLateTreeSemanticIntentSkipsBelowFloorLookup(t *testing.T) {
	uic := intent.New(intent.Config{LLMFunc: func(_, _ string) (string, error) {
		return `{"top":[{"skill":"search","score":0.50}]}`, nil
	}})
	text := "随便查一下"
	userID := "user-below-floor"
	warmed := uic.ClassifyContext(context.Background(), intent.MessageContext{Text: text, UserID: userID})
	if warmed.Primary != intent.LabelSearch || warmed.Degraded || warmed.Confidence >= semanticLookupHintFloor {
		t.Fatalf("warm cache = %+v, want below-floor search", warmed)
	}
	h := &IMMessageHandler{unifiedClassifier: uic}
	ctx := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; tree classification unavailable (l2=workflow_task conf=0.73)",
		},
	}}
	if h.adoptLateTreeSemanticIntent(ctx, userID, text, nil) {
		t.Fatal("below-floor cached search must not replace a timeout unknown")
	}
	if ctx.Runtime.SemanticIntent.Primary != intent.LabelUnknown {
		t.Fatalf("intent mutated: %+v", ctx.Runtime.SemanticIntent)
	}
}

func TestPrepareAgentLoopToolsClassifierTimeoutKeepsWebSearch(t *testing.T) {
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("gui_click", "click", nil, nil),
		toolDef("project_manage", "manage a project", nil, nil),
		toolDef("web_search", "search the public web", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{toolDefGen: NewToolDefinitionGenerator(nil, defs)}
	ctx := &LoopContext{Runtime: RuntimeContext{
		ClassifierTimeoutLookup: true,
		RoutingMissFallback:     true,
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true,
			Reason: "embedding ambiguous; tree classification unavailable (l2=workflow_task conf=0.73); chat projection; routing miss fallback",
		},
	}}
	got := h.prepareAgentLoopTools("u1", "长江学者申请后，一般研究项目执行几年？", ctx, agentLoopPhase{})
	names := toolNameSetForWorkflowFilterTest(got.Tools)
	if !names["web_search"] || !names["web_fetch"] {
		t.Fatalf("classifier-timeout leftover must expose web lookup, got %#v", names)
	}

	denied := *ctx
	denied.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{AllowWebSearch: false}
	blocked := h.prepareAgentLoopTools("u1", "长江学者申请后，一般研究项目执行几年？", &denied, agentLoopPhase{})
	blockedNames := toolNameSetForWorkflowFilterTest(blocked.Tools)
	if blockedNames["web_search"] || blockedNames["web_fetch"] || blockedNames["bash"] || blockedNames["read_file"] {
		t.Fatalf("group policy must still deny leftover web lookup and the execution floor, got %#v", blockedNames)
	}
}

// A classifier timeout skips the name router and used to publish only
// web_search/web_fetch. A full assistant turn, including an expert session,
// must still be able to run a command and change a file.
func TestPrepareAgentLoopToolsFullTimeoutKeepsExecutionFloor(t *testing.T) {
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("edit_lines", "edit line ranges", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("download_file", "download", nil, nil),
		toolDef("web_search", "search the public web", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{toolDefGen: NewToolDefinitionGenerator(nil, defs)}
	ctx := &LoopContext{Runtime: RuntimeContext{
		ClassifierTimeoutLookup: true,
		RoutingMissFallback:     true,
		Execution:               fullExecutionProfile("expert session"),
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true,
			Reason: "embedding ambiguous; tree classification unavailable (l2=template_manage conf=0.68); routing miss fallback",
		},
	}}
	got := h.prepareAgentLoopTools("desktop-user", "把 elsarticle 模板里的标题改成论文题目", ctx, agentLoopPhase{})
	names := toolNameSetForWorkflowFilterTest(got.Tools)
	for _, name := range []string{"bash", "read_file", "write_file", "edit_file", "web_search", "web_fetch"} {
		if !names[name] {
			t.Fatalf("full timeout must keep %s, got %#v", name, names)
		}
	}
	for _, name := range []string{"download_file", "edit_lines"} {
		if names[name] {
			t.Fatalf("full timeout must not restore privilege tool %s, got %#v", name, names)
		}
	}
	base := toolNameSetForWorkflowFilterTest(got.BaseTools)
	if !base["bash"] || !base["read_file"] || !base["write_file"] || !base["edit_file"] {
		t.Fatalf("full timeout base surface must keep the floor for recovery, got %#v", base)
	}
}

func TestPrepareAgentLoopToolsLightTimeoutStaysWebOnly(t *testing.T) {
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_search", "search the public web", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{toolDefGen: NewToolDefinitionGenerator(nil, defs)}
	ctx := &LoopContext{Runtime: RuntimeContext{
		ClassifierTimeoutLookup: true,
		RoutingMissFallback:     true,
		Execution: ExecutionProfile{
			Layer:         string(executionLayerLight),
			PromptProfile: "light",
			Reason:        "short lookup",
		},
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true,
			Reason: "embedding ambiguous; tree classification unavailable (l2=live_data conf=0.61); routing miss fallback",
		},
	}}
	got := h.prepareAgentLoopTools("desktop-user", "现在几点", ctx, agentLoopPhase{})
	names := toolNameSetForWorkflowFilterTest(got.Tools)
	if !names["web_search"] || !names["web_fetch"] {
		t.Fatalf("light timeout must keep web lookup, got %#v", names)
	}
	for _, name := range []string{"bash", "read_file", "write_file", "edit_file"} {
		if names[name] {
			t.Fatalf("light timeout must not grow the execution floor with %s, got %#v", name, names)
		}
	}
}

func TestPrepareAgentLoopToolsSkillSearchTimeoutDoesNotRestoreBash(t *testing.T) {
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_search", "search the public web", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{toolDefGen: NewToolDefinitionGenerator(nil, defs)}
	ctx := &LoopContext{Runtime: RuntimeContext{
		ClassifierTimeoutLookup: true,
		Execution:               fullExecutionProfile("expert session"),
	}}
	phase := agentLoopPhase{ForceSkillPreference: true}
	got := h.prepareAgentLoopTools("desktop-user", "找一个能改 tex 的技能", ctx, phase)
	names := toolNameSetForWorkflowFilterTest(got.Tools)
	if names["bash"] {
		t.Fatalf("skill preference must keep bash off a timeout floor pin, got %#v", names)
	}
	if !names["edit_file"] || !names["web_fetch"] {
		t.Fatalf("skill preference still allows file edit and web fetch, got %#v", names)
	}
}

func TestRestoreTimeoutFloorRespectsDirectMode(t *testing.T) {
	registry := NewTaskOrchestratorRegistry()
	orch := registry.GetOrCreate("u-direct-timeout")
	orch.Activate([]*TaskItem{{Index: 0, Title: "Ready"}}, "", "", "/proj", "claude")
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_search", "search the public web", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{
		taskOrchestratorRegistry: registry,
		toolDefGen:               NewToolDefinitionGenerator(nil, defs),
	}
	ctx := &LoopContext{Runtime: RuntimeContext{
		ClassifierTimeoutLookup: true,
		Execution:               fullExecutionProfile("expert session"),
	}}
	restored, _, directFiltered := h.restoreToolsAfterSkillRecover("u-direct-timeout", ctx, defs, agentLoopPhase{})
	if !directFiltered {
		t.Fatal("direct mode must be recorded on recovery")
	}
	names := toolNameSetForWorkflowFilterTest(restored)
	for _, name := range []string{"bash", "write_file", "edit_file"} {
		if names[name] {
			t.Fatalf("direct mode must not regain %s from the timeout floor, got %#v", name, names)
		}
	}
	if !names["read_file"] || !names["web_fetch"] {
		t.Fatalf("direct mode keeps read and web tools, got %#v", names)
	}
}

func TestInjectionTimeoutFloorRespectsDirectMode(t *testing.T) {
	registry := NewTaskOrchestratorRegistry()
	orch := registry.GetOrCreate("u-direct-inject")
	orch.Activate([]*TaskItem{{Index: 0, Title: "Ready"}}, "", "", "/proj", "claude")
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_search", "search the public web", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{
		taskOrchestratorRegistry: registry,
		toolDefGen:               NewToolDefinitionGenerator(nil, defs),
	}
	ctx := &LoopContext{Runtime: RuntimeContext{
		ClassifierTimeoutLookup: true,
		Execution:               fullExecutionProfile("expert session"),
	}}
	got, _ := h.finalizeInjectionAugmentedTools(ctx, "u-direct-inject", defs, agentLoopPhase{})
	names := toolNameSetForWorkflowFilterTest(got)
	for _, name := range []string{"bash", "write_file", "edit_file"} {
		if names[name] {
			t.Fatalf("direct-mode injection must not regain %s, got %#v", name, names)
		}
	}
	if !names["read_file"] || !names["web_fetch"] {
		t.Fatalf("direct-mode injection keeps read and web tools, got %#v", names)
	}
}

func TestInjectionSkillPreferenceDropsBashWithoutTimeout(t *testing.T) {
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{toolDefGen: NewToolDefinitionGenerator(nil, defs)}
	ctx := &LoopContext{Runtime: RuntimeContext{Execution: fullExecutionProfile("expert session")}}
	phase := agentLoopPhase{
		ForceSkillPreference:   true,
		TruncationBlockedTools: map[string]bool{"edit_file": true},
	}
	got, _ := h.augmentToolsFromInjection(ctx, "desktop-user", "[用户补充] 改一下标题", defs, nil, false, phase)
	names := toolNameSetForWorkflowFilterTest(got)
	if names["bash"] {
		t.Fatalf("skill preference must keep bash off a non-timeout injection, got %#v", names)
	}
	if names["edit_file"] {
		t.Fatalf("truncation block must keep edit_file off a non-timeout injection, got %#v", names)
	}
	if !names["write_file"] || !names["read_file"] || !names["web_fetch"] {
		t.Fatalf("non-timeout injection keeps the tools those policies allow, got %#v", names)
	}
}

func TestInjectionTimeoutFloorRespectsSkillAndTruncation(t *testing.T) {
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_search", "search the public web", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{toolDefGen: NewToolDefinitionGenerator(nil, defs)}
	ctx := &LoopContext{Runtime: RuntimeContext{
		ClassifierTimeoutLookup: true,
		Execution:               fullExecutionProfile("expert session"),
	}}
	phase := agentLoopPhase{
		ForceSkillPreference:   true,
		TruncationBlockedTools: map[string]bool{"edit_file": true},
	}
	got, _ := h.augmentToolsFromInjection(ctx, "desktop-user", "[用户补充] 改一下标题", defs, nil, false, phase)
	names := toolNameSetForWorkflowFilterTest(got)
	if names["bash"] {
		t.Fatalf("skill preference must keep bash off an injection timeout floor, got %#v", names)
	}
	if names["edit_file"] {
		t.Fatalf("truncation block must keep edit_file off an injection timeout floor, got %#v", names)
	}
	if !names["write_file"] || !names["read_file"] || !names["web_fetch"] {
		t.Fatalf("injection timeout floor keeps the tools those policies allow, got %#v", names)
	}
}

func TestDeliverableRecoverDoesNotRestoreDirectModeFloor(t *testing.T) {
	registry := NewTaskOrchestratorRegistry()
	orch := registry.GetOrCreate("u-direct-deliver")
	orch.Activate([]*TaskItem{{Index: 0, Title: "Ready"}}, "", "", "/proj", "claude")
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{taskOrchestratorRegistry: registry}
	phase := agentLoopPhase{
		Stage:         agentStageRecover,
		RecoverPrompt: "finish the file",
		RecoverReason: agentRecoverDeliverablePending,
	}
	current := []map[string]interface{}{toolDef("web_fetch", "fetch a web page", nil, nil)}
	result := h.applyAgentLoopRecoverPrompt(nil, "u-direct-deliver", &phase, nil, current, 0, defs)
	if !result.DirectModeToolsFiltered {
		t.Fatal("deliverable recover in direct mode must record the filter")
	}
	names := toolNameSetForWorkflowFilterTest(result.Tools)
	for _, name := range []string{"bash", "write_file", "edit_file"} {
		if names[name] {
			t.Fatalf("deliverable recover must not hand %s back to the main loop, got %#v", name, names)
		}
	}
	if !names["read_file"] || !names["web_fetch"] {
		t.Fatalf("deliverable recover keeps read and web tools, got %#v", names)
	}
}

func TestDeliverableRecoverDropsTruncationBlockedEdit(t *testing.T) {
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{}
	phase := agentLoopPhase{
		Stage:                  agentStageRecover,
		RecoverPrompt:          "finish the file",
		RecoverReason:          agentRecoverDeliverablePending,
		TruncationBlockedTools: map[string]bool{"edit_file": true},
	}
	current := []map[string]interface{}{toolDef("web_fetch", "fetch a web page", nil, nil)}
	result := h.applyAgentLoopRecoverPrompt(nil, "desktop-user", &phase, nil, current, 0, defs)
	names := toolNameSetForWorkflowFilterTest(result.Tools)
	if names["edit_file"] {
		t.Fatalf("deliverable recover must not restore a truncation-blocked edit_file, got %#v", names)
	}
	for _, name := range []string{"bash", "read_file", "write_file", "web_fetch"} {
		if !names[name] {
			t.Fatalf("deliverable recover keeps %s, got %#v", name, names)
		}
	}
}

func TestMissFloorUnlockDropsTruncationAndDirectMode(t *testing.T) {
	registry := NewTaskOrchestratorRegistry()
	registry.GetOrCreate("u-floor-unlock").Activate([]*TaskItem{{Index: 0, Title: "Ready"}}, "", "", "/proj", "claude")
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{taskOrchestratorRegistry: registry}
	ctx := &LoopContext{Runtime: RuntimeContext{Execution: fullExecutionProfile("expert session")}}
	phase := &agentLoopPhase{
		MissFloorToolsUnlock:   true,
		TruncationBlockedTools: map[string]bool{"read_file": true},
	}
	current := []map[string]interface{}{toolDef("web_fetch", "fetch a web page", nil, nil)}
	result := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:                 ctx,
		UserID:                  "u-floor-unlock",
		UserText:                "改一下标题",
		Iteration:               0,
		EffectiveMax:            8,
		Config:                  corelib.MaclawLLMConfig{ContextLength: 100_000},
		Conversation:            []interface{}{map[string]string{"role": "user", "content": "改一下标题"}},
		Tools:                   current,
		BaseTools:               defs,
		DirectModeToolsFiltered: true,
		Phase:                   phase,
	})
	if !result.DirectModeToolsFiltered {
		t.Fatal("floor unlock in direct mode must keep the filter latched")
	}
	names := toolNameSetForWorkflowFilterTest(result.Tools)
	for _, name := range []string{"bash", "write_file", "edit_file", "read_file"} {
		if names[name] {
			t.Fatalf("floor unlock must not restore %s, got %#v", name, names)
		}
	}
	if !names["web_fetch"] {
		t.Fatalf("floor unlock keeps the tools it did not block, got %#v", names)
	}
	if phase.MissFloorToolsUnlock {
		t.Fatal("floor unlock must clear itself after one round")
	}
}

// directModePolicyOwnerSplit registers the live direct-mode task under the
// runtime policy owner, not the loop user. The loop user has no orchestrator.
func directModePolicyOwnerSplit(loopUser, owner string) (*IMMessageHandler, *LoopContext) {
	registry := NewTaskOrchestratorRegistry()
	registry.GetOrCreate(owner).Activate([]*TaskItem{{Index: 0, Title: "Ready"}}, "", "", "/proj", "claude")
	h := &IMMessageHandler{taskOrchestratorRegistry: registry, lastUserID: loopUser}
	ctx := &LoopContext{Runtime: RuntimeContext{
		RequestID:     "req-floor-owner",
		PolicyOwnerID: owner,
		Execution:     fullExecutionProfile("expert session"),
	}}
	return h, ctx
}

func TestAgentGuidedRoundStripsDirectModeOfPolicyOwner(t *testing.T) {
	const loopUser = "desktop-floor-user"
	const owner = "remote:floor-owner"
	h, ctx := directModePolicyOwnerSplit(loopUser, owner)
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	phase := &agentLoopPhase{SkillMode: skillPreferenceAgentGuided}
	result := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:      ctx,
		UserID:       loopUser,
		UserText:     "改一下标题",
		Iteration:    0,
		EffectiveMax: 8,
		Config:       corelib.MaclawLLMConfig{ContextLength: 100_000},
		Conversation: []interface{}{map[string]string{"role": "user", "content": "改一下标题"}},
		Tools:        []map[string]interface{}{toolDef("read_file", "read file", nil, nil), toolDef("web_fetch", "fetch a web page", nil, nil)},
		BaseTools:    defs,
		Phase:        phase,
	})
	if !result.DirectModeToolsFiltered {
		t.Fatal("agent-guided reseal must latch direct mode from the policy owner")
	}
	names := toolNameSetForWorkflowFilterTest(result.Tools)
	for _, name := range []string{"bash", "write_file", "edit_file"} {
		if names[name] {
			t.Fatalf("policy-owner direct mode must keep %s off the agent-guided round, got %#v", name, names)
		}
	}
	if !names["read_file"] || !names["web_fetch"] {
		t.Fatalf("agent-guided round keeps read and web tools, got %#v", names)
	}
}

func TestMissFloorUnlockStripsDirectModeOfPolicyOwner(t *testing.T) {
	const loopUser = "desktop-floor-unlock"
	const owner = "remote:floor-unlock-owner"
	h, ctx := directModePolicyOwnerSplit(loopUser, owner)
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	phase := &agentLoopPhase{MissFloorToolsUnlock: true}
	result := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:                 ctx,
		UserID:                  loopUser,
		UserText:                "改一下标题",
		Iteration:               0,
		EffectiveMax:            8,
		Config:                  corelib.MaclawLLMConfig{ContextLength: 100_000},
		Conversation:            []interface{}{map[string]string{"role": "user", "content": "改一下标题"}},
		Tools:                   []map[string]interface{}{toolDef("web_fetch", "fetch a web page", nil, nil)},
		BaseTools:               defs,
		DirectModeToolsFiltered: true,
		Phase:                   phase,
	})
	if !result.DirectModeToolsFiltered {
		t.Fatal("floor unlock must keep the direct-mode latch")
	}
	names := toolNameSetForWorkflowFilterTest(result.Tools)
	for _, name := range []string{"bash", "write_file", "edit_file"} {
		if names[name] {
			t.Fatalf("policy-owner direct mode must keep %s off a latched floor unlock, got %#v", name, names)
		}
	}
	if !names["read_file"] || !names["web_fetch"] {
		t.Fatalf("floor unlock keeps read and web tools, got %#v", names)
	}
}

func TestPrepareAgentLoopToolsDirectModeFollowsPolicyOwner(t *testing.T) {
	const loopUser = "desktop-shared-direct"
	const owner = "remote:shared-direct-owner"
	h, ctx := directModePolicyOwnerSplit(loopUser, owner)
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h.toolDefGen = NewToolDefinitionGenerator(nil, defs)
	got := h.prepareAgentLoopTools(loopUser, "把标题改一下", ctx, agentLoopPhase{})
	names := toolNameSetForWorkflowFilterTest(got.Tools)
	for _, name := range []string{"bash", "write_file", "edit_file"} {
		if names[name] {
			t.Fatalf("shared-loop prepare must keep %s off a direct-mode policy owner, got %#v", name, names)
		}
	}
	if !names["read_file"] || !names["web_fetch"] {
		t.Fatalf("shared-loop prepare keeps read and web tools, got %#v", names)
	}
	base := toolNameSetForWorkflowFilterTest(got.BaseTools)
	for _, name := range []string{"bash", "write_file", "edit_file"} {
		if base[name] {
			t.Fatalf("direct-mode base surface must not keep %s for a later unlock, got %#v", name, base)
		}
	}
}

func TestInjectionStripsDirectModeOfPolicyOwner(t *testing.T) {
	const loopUser = "desktop-floor-inject"
	const owner = "remote:floor-inject-owner"
	h, ctx := directModePolicyOwnerSplit(loopUser, owner)
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h.toolDefGen = NewToolDefinitionGenerator(nil, defs)
	got, _ := h.finalizeInjectionAugmentedTools(ctx, loopUser, defs, agentLoopPhase{})
	names := toolNameSetForWorkflowFilterTest(got)
	for _, name := range []string{"bash", "write_file", "edit_file"} {
		if names[name] {
			t.Fatalf("policy-owner direct mode must keep %s off an injection, got %#v", name, names)
		}
	}
	if !names["read_file"] || !names["web_fetch"] {
		t.Fatalf("injection keeps read and web tools, got %#v", names)
	}
}

func TestDeliverableRecoverRespectsGroupDeny(t *testing.T) {
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{}
	ctx := &LoopContext{LansengerGroupPermissions: &lansengerGroupPermissionPolicy{}}
	phase := agentLoopPhase{
		Stage:         agentStageRecover,
		RecoverPrompt: "finish the file",
		RecoverReason: agentRecoverDeliverablePending,
	}
	current := []map[string]interface{}{toolDef("web_fetch", "fetch a web page", nil, nil)}
	result := h.applyAgentLoopRecoverPrompt(ctx, "lansenger:group:floor", &phase, nil, current, 0, defs)
	names := toolNameSetForWorkflowFilterTest(result.Tools)
	for _, name := range []string{"bash", "read_file", "write_file", "edit_file", "web_fetch"} {
		if names[name] {
			t.Fatalf("group policy must deny %s restored by deliverable recover, got %#v", name, names)
		}
	}
}

func TestMissFloorUnlockRespectsSkillPreference(t *testing.T) {
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	h := &IMMessageHandler{}
	ctx := &LoopContext{Runtime: RuntimeContext{Execution: fullExecutionProfile("expert session")}}
	phase := &agentLoopPhase{
		MissFloorToolsUnlock: true,
		ForceSkillPreference: true,
	}
	current := []map[string]interface{}{toolDef("web_fetch", "fetch a web page", nil, nil)}
	result := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:      ctx,
		UserID:       "desktop-user",
		UserText:     "找一个能改 tex 的技能",
		Iteration:    0,
		EffectiveMax: 8,
		Config:       corelib.MaclawLLMConfig{ContextLength: 100_000},
		Conversation: []interface{}{map[string]string{"role": "user", "content": "找一个能改 tex 的技能"}},
		Tools:        current,
		BaseTools:    defs,
		Phase:        phase,
	})
	names := toolNameSetForWorkflowFilterTest(result.Tools)
	if names["bash"] {
		t.Fatalf("skill preference must keep bash off a floor unlock, got %#v", names)
	}
	for _, name := range []string{"read_file", "write_file", "edit_file", "web_fetch"} {
		if !names[name] {
			t.Fatalf("skill preference still allows %s, got %#v", name, names)
		}
	}
}

func TestExpertEmptyWhitelistKeepsExecutionFloor(t *testing.T) {
	tools := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("write_file", "write a file", nil, nil),
		toolDef("edit_file", "edit a file", nil, nil),
		toolDef("download_file", "download", nil, nil),
		toolDef("web_fetch", "fetch a web page", nil, nil),
	}
	latex := &ExpertDefinition{ID: builtinLatexExpertID, Tools: []string{}}
	kept := filterToolsForExpert(tools, latex)
	names := toolNameSetForWorkflowFilterTest(kept)
	for _, name := range []string{"bash", "write_file", "edit_file", "download_file"} {
		if !names[name] {
			t.Fatalf("empty expert whitelist must keep %s, got %#v", name, names)
		}
	}
	restricted := filterToolsForExpert(tools, &ExpertDefinition{Tools: []string{"web_fetch"}})
	restrictedNames := toolNameSetForWorkflowFilterTest(restricted)
	if restrictedNames["bash"] || restrictedNames["edit_file"] || !restrictedNames["web_fetch"] {
		t.Fatalf("named expert whitelist must win over the floor, got %#v", restrictedNames)
	}
}
