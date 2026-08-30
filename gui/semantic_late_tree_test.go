package main

import (
	"context"
	"strings"
	"testing"

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
	if blockedNames["web_search"] || blockedNames["web_fetch"] {
		t.Fatalf("group policy must still deny leftover web lookup, got %#v", blockedNames)
	}
}
