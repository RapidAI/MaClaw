package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// The close is checked against the shared list rather than the two names its
// author happened to remember, so a gateway added later is covered here on the
// day it is named. The freeze test below poisons with a hand-written pair and
// would already miss discover_tool.
//
// Each gateway is also granted before the close runs. That is the point: the
// ban has to hold on its own, not as a side effect of the grant filter. A
// planner that ever selected a gateway as though it were a capability would
// otherwise hand back exactly the open-ended selector the managed surface
// exists to remove.
func TestEveryNamedGatewayIsClosedOutOfTheManagedSurface(t *testing.T) {
	gateways := tool.LegacyDynamicGatewayNames()
	if len(gateways) == 0 {
		t.Fatal("no gateway is named, so nothing here is being enforced")
	}
	grants := map[string]tool.InvocationGrant{"granted_capability": {}}
	defs := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "granted_capability"}},
	}
	for _, name := range gateways {
		grants[name] = tool.InvocationGrant{}
		defs = append(defs, map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": name}})
	}
	closed := closedManagedSemanticDefinitions(defs, grants)
	if len(closed) != 1 || extractToolName(closed[0]) != "granted_capability" {
		names := make([]string, 0, len(closed))
		for _, def := range closed {
			names = append(names, extractToolName(def))
		}
		t.Fatalf("closed surface = %v, want only the granted capability", names)
	}
}

func TestManagedSemanticSurfaceRejectsLegacyBypassUnion(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "北京天气", "desktop", "root-freeze", "turn-freeze",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98, ToolNames: []string{"call_mcp_tool", "manage_skill", "bash"}},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	for _, def := range defs {
		name := extractToolName(def)
		if isLegacySemanticBypassName(name) {
			t.Fatalf("renderer leaked legacy gateway %q", name)
		}
		if _, ok := surface.grants[name]; !ok {
			t.Fatalf("rendered name %q is not a CatalogRenderer grant", name)
		}
	}
	poisoned := append(append([]map[string]interface{}{}, defs...),
		map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "call_mcp_tool"}},
		map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "manage_skill"}},
		map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "session_pin_web_search"}},
	)
	closed := closedManagedSemanticDefinitions(poisoned, surface.grants)
	if len(closed) != len(defs) {
		t.Fatalf("closed surface changed authorized count: got %d want %d", len(closed), len(defs))
	}
	for _, def := range closed {
		name := extractToolName(def)
		if isLegacySemanticBypassName(name) || name == "session_pin_web_search" {
			t.Fatalf("legacy bypass survived close: %q", name)
		}
		if _, ok := surface.grants[name]; !ok {
			t.Fatalf("closed surface admitted ungranted name %q", name)
		}
	}
}

func TestClosedManagedSemanticDefinitionsEmptyGrantsAdmitNothing(t *testing.T) {
	poisoned := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "write_file"}},
		{"type": "function", "function": map[string]interface{}{"name": "web_fetch"}},
		{"type": "function", "function": map[string]interface{}{"name": "bash"}},
	}
	if closed := closedManagedSemanticDefinitions(poisoned, nil); len(closed) != 0 {
		t.Fatalf("empty grants must not fail-open: %#v", closed)
	}
	if closed := closedManagedSemanticDefinitions(poisoned, map[string]tool.InvocationGrant{}); len(closed) != 0 {
		t.Fatalf("zero grants must not fail-open: %#v", closed)
	}
}

func TestClosedManagedSemanticDefinitionsForTurnDropsLightUnsafeGrants(t *testing.T) {
	surface := &semanticCallSurface{
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "read", Effects: []tool.EffectClass{tool.EffectReadOnly}},
			{ID: "write", Effects: []tool.EffectClass{tool.EffectLocalMutation}},
		}},
		grants: map[string]tool.InvocationGrant{
			"invoke_read":  {SelectionID: "read"},
			"invoke_write": {SelectionID: "write"},
		},
	}
	defs := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "invoke_read"}},
		{"type": "function", "function": map[string]interface{}{"name": "invoke_write"}},
		{"type": "function", "function": map[string]interface{}{"name": "write_file"}},
	}
	full := closedManagedSemanticDefinitionsForTurn(defs, surface, false)
	if len(full) != 2 {
		t.Fatalf("full turn closed=%#v, want granted read+write", full)
	}
	light := closedManagedSemanticDefinitionsForTurn(defs, surface, true)
	if len(light) != 1 || extractToolName(light[0]) != "invoke_read" {
		t.Fatalf("light turn closed=%#v, want only invoke_read", light)
	}
}

func TestManagedSemanticLightUpgradeDoesNotRestoreLegacyTools(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "北京天气", "desktop", "root-light-freeze", "turn-light-freeze",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: nil, semanticSurface: surface, tools: defs,
		loopCtx: &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{Layer: string(executionLayerLight)}}},
	}
	if cb.UpgradeLightPromptToFull("test") {
		t.Fatal("governed semantic light turn must not fake a full upgrade")
	}
	if cb.semanticSurface != surface {
		t.Fatal("upgrade replaced the governed surface")
	}
	if !cb.loopCtx.Runtime.Execution.IsLight() {
		t.Fatal("governed semantic upgrade mutated the light execution profile")
	}
	if len(cb.tools) == 0 {
		t.Fatal("lookup surface had no granted tools")
	}
	lightClosed := closedManagedSemanticDefinitionsForTurn(cb.tools, surface, true)
	if len(lightClosed) == 0 {
		t.Fatal("light start close stripped the granted lookup")
	}
	for _, def := range lightClosed {
		name := extractToolName(def)
		if isLegacySemanticBypassName(name) {
			t.Fatalf("upgrade restored legacy gateway %q", name)
		}
		if !cb.IsToolAllowed(name) {
			t.Fatalf("light authorizer rejected granted lookup %q", name)
		}
	}
}

func TestManagedSemanticTurnInjectionCannotAddSoupTools(t *testing.T) {
	handler := managedSemanticAugmentHandler()
	current := []map[string]interface{}{toolDef("invoke_lookup", "lookup", nil, nil)}
	base := managedSemanticSoupCatalog()
	ctx := managedLiveDataLoopContext()
	got, _ := handler.augmentToolsFromInjection(ctx, "user-1", "[用户补充] 直接用ssh连上服务器并打开浏览器", current, base, false)
	assertClosedGrantSurface(t, got, "invoke_lookup")
}

func TestManagedSemanticTurnSessionPinCannotAddSoupTools(t *testing.T) {
	handler := managedSemanticAugmentHandler()
	handler.toolRouter.ActivateSessionToolForSession("user-1", "ssh")
	handler.toolRouter.ActivateSessionToolForSession("user-1", "browser")
	current := []map[string]interface{}{toolDef("invoke_lookup", "lookup", nil, nil)}
	got, _ := handler.augmentToolsFromSessionPins(managedLiveDataLoopContext(), "user-1", current, estimateToolsTokens(current))
	assertClosedGrantSurface(t, got, "invoke_lookup")
}

func TestUnmanagedSessionPinCannotAddConditionalTool(t *testing.T) {
	handler := managedSemanticAugmentHandler()
	handler.toolRouter.ActivateSessionToolForSession("user-1", "ssh")
	current := []map[string]interface{}{toolDef("invoke_lookup", "lookup", nil, nil)}
	got, _ := handler.augmentToolsFromSessionPins(&LoopContext{}, "user-1", current, estimateToolsTokens(current))
	names := toolNamesFromDefs(got)
	if !names["invoke_lookup"] || names["ssh"] {
		t.Fatalf("session pin must not augment any surface, got %#v", names)
	}
}

func TestManagedSemanticRecoverDoesNotRestoreBaseTools(t *testing.T) {
	handler := managedSemanticAugmentHandler()
	phase := &agentLoopPhase{
		Stage:         agentStageRecover,
		RecoverPrompt: "skill failed, continue without it",
		RecoverReason: agentRecoverSkillFailed,
	}
	current := []map[string]interface{}{toolDef("invoke_lookup", "lookup", nil, nil)}
	result := handler.applyAgentLoopRecoverPrompt(
		managedLiveDataLoopContext(), "user-1", phase, nil, current, estimateToolsTokens(current), managedSemanticSoupCatalog(),
	)
	if !result.Applied {
		t.Fatal("recover prompt should still apply")
	}
	assertClosedGrantSurface(t, result.Tools, "invoke_lookup")
}

func TestPrepareAgentLoopToolsManagedTurnDoesNotRunNameRouter(t *testing.T) {
	handler := managedSemanticAugmentHandler()
	var set agentLoopToolSet
	got := captureBrowserDiagLog(t, func() {
		set = handler.prepareAgentLoopTools("user-1", "直接用ssh连上服务器并打开浏览器", managedLiveDataLoopContext(), agentLoopPhase{})
	})
	if len(set.Tools) != 0 || len(set.BaseTools) != 0 {
		t.Fatalf("managed prepare must not open the name router: tools=%#v base=%#v", toolNamesFromDefs(set.Tools), toolNamesFromDefs(set.BaseTools))
	}
	if !strings.Contains(got, "skip name-router prepareAgentLoopTools") {
		t.Fatalf("managed prepare should fail-closed before Route(): %q", got)
	}
}

func TestVisionFallthroughExecutionToolsExcludesSearch(t *testing.T) {
	handler := &IMMessageHandler{
		toolDefGen: NewToolDefinitionGenerator(nil, []map[string]interface{}{
			toolDef("write_file", "write", nil, nil),
			toolDef("edit_file", "edit", nil, nil),
			toolDef("bash", "bash", nil, nil),
			toolDef("read_file", "read", nil, nil),
			toolDef("list_directory", "list", nil, nil),
			toolDef("read_tool_result", "result", nil, nil),
			toolDef("web_search", "search", nil, nil),
			toolDef("web_fetch", "fetch", nil, nil),
			toolDef("browser", "browser", nil, nil),
		}),
	}
	ctx := &LoopContext{Runtime: RuntimeContext{
		VisionFallthrough: true,
		RequestID:         "desktop-ai-vision-poster",
	}}
	if attached := handler.attachVisionFallthroughExecutionTools(ctx, nil, nil, "user-1", "这张图里的天气如何", nil); len(attached) != 0 {
		t.Fatalf("plain image lookup must stay tool-empty: %#v", attached)
	}
	if attached := handler.attachVisionFallthroughExecutionTools(ctx, nil, nil, "user-1", "check the layout of this screenshot", nil); len(attached) != 0 {
		t.Fatalf("generic layout wording must not unlock write tools: %#v", attached)
	}
	if attached := handler.attachVisionFallthroughExecutionTools(ctx, nil, nil, "user-1", "文字堆在一起", nil); len(attached) == 0 {
		t.Fatal("vision critique of a generated artifact must attach a no-search execution surface")
	} else {
		names := toolNamesFromDefs(attached)
		for _, want := range []string{"write_file", "bash", "read_file"} {
			if !names[want] {
				t.Fatalf("missing %q in %#v", want, names)
			}
		}
		for _, forbid := range []string{"web_search", "web_fetch", "browser"} {
			if names[forbid] {
				t.Fatalf("search/browser leaked onto vision fallthrough surface: %#v", names)
			}
		}
	}
	history := []agent.ConversationEntry{{Role: "tool", ToolName: "write_file"}}
	if attached := handler.attachVisionFallthroughExecutionTools(ctx, nil, nil, "user-1", "再看一下这张图", history); len(attached) == 0 {
		t.Fatal("recent file execution history must unlock the vision execution surface")
	}
	mapHistory := []agent.ConversationEntry{{Role: "assistant", ToolCalls: []map[string]string{{"name": "write_file"}}}}
	if attached := handler.attachVisionFallthroughExecutionTools(ctx, nil, nil, "user-1", "再看一下这张图", mapHistory); len(attached) == 0 {
		t.Fatal("string-map tool_calls history must also unlock the vision execution surface")
	}
	nestedHistory := []agent.ConversationEntry{{Role: "assistant", ToolCalls: []interface{}{
		map[string]interface{}{"function": map[string]string{"name": "bash"}},
	}}}
	if attached := handler.attachVisionFallthroughExecutionTools(ctx, nil, nil, "user-1", "再看一下这张图", nestedHistory); len(attached) == 0 {
		t.Fatal("nested string-map function history must also unlock the vision execution surface")
	}
	if attached := handler.attachVisionFallthroughExecutionTools(ctx, nil, nil, "user-1", "这张图里的天气如何", history); len(attached) != 0 {
		t.Fatalf("lookup text must win over leftover file-write history: %#v", attached)
	}
	if attached := handler.attachVisionFallthroughExecutionTools(ctx, nil, nil, "user-1", "图不清晰看不清天气", history); len(attached) != 0 {
		t.Fatalf("weak critique plus lookup must stay tool-empty: %#v", attached)
	}
	if attached := handler.attachVisionFallthroughExecutionTools(ctx, nil, nil, "user-1", "太乱", nil); len(attached) == 0 {
		t.Fatal("standalone weak critique must still attach the no-search execution surface")
	}
	stagedCritique := "太乱\n\n" + filePathPromptPrefix + "\nC:\\tmp\\weather.png\n--- image_ocr: begin ---\n南京天气\n--- image_ocr: end ---"
	if attached := handler.attachVisionFallthroughExecutionTools(ctx, nil, nil, "user-1", stagedCritique, nil); len(attached) == 0 {
		t.Fatal("picker path and OCR weather text must not hide a user critique")
	}
	stagedLookup := "这张图里的天气如何\n\n" + filePathPromptPrefix + "\nC:\\tmp\\poster.png"
	if attached := handler.attachVisionFallthroughExecutionTools(ctx, nil, nil, "user-1", stagedLookup, nil); len(attached) != 0 {
		t.Fatalf("user weather lookup must stay tool-empty after stripping the picker path: %#v", attached)
	}
	groupCtx := &LoopContext{
		Runtime: RuntimeContext{
			VisionFallthrough: true,
			RequestID:         "desktop-ai-vision-group",
		},
		LansengerGroupPermissions: &lansengerGroupPermissionPolicy{},
	}
	if attached := handler.attachVisionFallthroughExecutionTools(groupCtx, nil, nil, "user-1", "文字堆在一起", nil); len(attached) != 0 {
		t.Fatalf("group policy must still strip vision execution tools: %#v", attached)
	}
	if got := handler.attachVisionFallthroughExecutionTools(ctx, []map[string]interface{}{toolDef("invoke_lookup", "lookup", nil, nil)}, nil, "user-1", "文字堆在一起", nil); len(got) != 1 || extractToolName(got[0]) != "invoke_lookup" {
		t.Fatalf("existing tools must not be replaced: %#v", got)
	}
}

func TestPrepareAgentLoopToolsVisionFallthroughDoesNotRunNameRouter(t *testing.T) {
	handler := managedSemanticAugmentHandler()
	ctx := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent:    &intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true},
		VisionFallthrough: true,
	}}
	var set agentLoopToolSet
	got := captureBrowserDiagLog(t, func() {
		set = handler.prepareAgentLoopTools("user-1", "这张图里的天气如何", ctx, agentLoopPhase{})
	})
	if len(set.Tools) != 0 || len(set.BaseTools) != 0 {
		t.Fatalf("vision fallthrough must not open the name router: tools=%#v base=%#v", toolNamesFromDefs(set.Tools), toolNamesFromDefs(set.BaseTools))
	}
	if !strings.Contains(got, "skip name-router prepareAgentLoopTools") {
		t.Fatalf("vision fallthrough should skip Route(): %q", got)
	}
}

func TestPrepareAgentLoopToolsWorkflowDocumentGenerateStillRoutes(t *testing.T) {
	handler := managedSemanticAugmentHandler()
	ctx := &LoopContext{
		WorkflowAgentLoop: true,
		Runtime:           RuntimeContext{SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: .98}},
	}
	got := captureBrowserDiagLog(t, func() {
		_ = handler.prepareAgentLoopTools("user-1", "生成一份 PDF", ctx, agentLoopPhase{})
	})
	if strings.Contains(got, "skip name-router prepareAgentLoopTools") {
		t.Fatalf("workflow document_generate must still use the name-router prepare path: %q", got)
	}
}

func TestWorkflowDocumentGenerateStillAllowsNameRouterAugment(t *testing.T) {
	ctx := &LoopContext{
		WorkflowAgentLoop: true,
		Runtime:           RuntimeContext{SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: .98}},
	}
	if loopContextIsSemanticManaged(ctx) {
		t.Fatal("workflow document_generate must stay unmanaged so stage PDFs keep generate_pdf")
	}
	if !loopContextIsSemanticManaged(managedLiveDataLoopContext()) {
		t.Fatal("live_data turn must be treated as managed")
	}
}

func TestPrepareAgentLoopRoundManagedTurnIgnoresInjectionAndPins(t *testing.T) {
	handler := managedSemanticAugmentHandler()
	handler.toolRouter.ActivateSessionToolForSession("user-1", "ssh")
	handler.toolRouter.ActivateSessionToolForSession("user-1", "browser")
	handler.pendingInjection.Store("user-1", "[用户补充] 直接用ssh连上服务器并打开浏览器")
	ctx := NewLoopContext("managed-augment-round", 3, nil)
	ctx.Runtime.SemanticIntent = &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98}
	current := []map[string]interface{}{toolDef("invoke_lookup", "lookup", nil, nil)}
	result := handler.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:          ctx,
		UserID:           "user-1",
		UserText:         "北京天气",
		Iteration:        0,
		EffectiveMax:     3,
		Config:           corelib.MaclawLLMConfig{ContextLength: 100_000},
		Conversation:     []interface{}{map[string]string{"role": "user", "content": "北京天气"}},
		Tools:            current,
		ToolsTokenBudget: estimateToolsTokens(current),
		BaseTools:        managedSemanticSoupCatalog(),
	})
	if result.Stop {
		t.Fatal("managed round prep stopped unexpectedly")
	}
	assertClosedGrantSurface(t, result.Tools, "invoke_lookup")
}

func managedSemanticAugmentHandler() *IMMessageHandler {
	handler := &IMMessageHandler{
		toolDefGen: NewToolDefinitionGenerator(nil, managedSemanticSoupCatalog()),
	}
	handler.toolRouter = NewToolRouter(handler.toolDefGen)
	return handler
}

func managedSemanticSoupCatalog() []map[string]interface{} {
	return []map[string]interface{}{
		toolDef("invoke_lookup", "lookup", nil, nil),
		toolDef("bash", "bash", nil, nil),
		toolDef("ssh", "ssh", nil, nil),
		toolDef("browser", "browser", nil, nil),
	}
}

func managedLiveDataLoopContext() *LoopContext {
	return &LoopContext{Runtime: RuntimeContext{SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98}}}
}

func assertClosedGrantSurface(t *testing.T, defs []map[string]interface{}, grant string) {
	t.Helper()
	names := toolNamesFromDefs(defs)
	if !names[grant] {
		t.Fatalf("lost granted tool %q, got %#v", grant, names)
	}
	for _, soup := range []string{"bash", "ssh", "browser"} {
		if names[soup] {
			t.Fatalf("managed surface admitted soup tool %q: %#v", soup, names)
		}
	}
}

func toolNamesFromDefs(defs []map[string]interface{}) map[string]bool {
	names := make(map[string]bool, len(defs))
	for _, def := range defs {
		if name := extractToolName(def); name != "" {
			names[name] = true
		}
	}
	return names
}
