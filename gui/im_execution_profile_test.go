package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type executionProfileSkillProviderForTest struct {
	skills []coretool.SkillSummary
}

func (p *executionProfileSkillProviderForTest) ListActiveSkills() []coretool.SkillSummary {
	return p.skills
}

func TestClassifyIMExecutionProfileSemanticLookupUsesLight(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Confidence: 0.86,
		Layer:      3,
		Reason:     "semantic live data intent",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "\u5170\u5dde\u5929\u6c14"}, false, false, semantic)
	if !profile.IsLight() {
		t.Fatalf("profile layer = %q, want light; reason=%s", profile.Layer, profile.Reason)
	}
	if profile.ToolBudget <= 0 || profile.IterationBudget <= 0 {
		t.Fatalf("light profile should set budgets: %+v", profile)
	}
	if profile.IterationBudget != 3 {
		t.Fatalf("live_data iteration budget = %d, want 3", profile.IterationBudget)
	}
}

func TestClassifyIMExecutionProfileWithoutSemanticStaysFull(t *testing.T) {
	profile := classifyIMExecutionProfile(IMUserMessage{Text: "\u5170\u5dde\u5929\u6c14"}, false, false)
	if profile.IsLight() || profile.IsDirect() {
		t.Fatalf("profile without semantic classifier = %+v, want full", profile)
	}
}

func TestHandlerClassifyIMExecutionProfileSkipsSemanticForStructuralFull(t *testing.T) {
	h := &IMMessageHandler{}
	msg := IMUserMessage{
		Text: "\u8bf7\u57fa\u4e8e\u8fd9\u4e2a\u9879\u76ee\u7684\u65e5\u5fd7\u548c\u4ee3\u7801\u7ed9\u51fa\u5b8c\u6574\u4f18\u5316\u65b9\u6848",
	}
	profile, semantic := h.classifyIMExecutionProfileAndSemantic(msg, false, false)
	if profile.Layer != string(executionLayerFull) {
		t.Fatalf("profile = %+v, want full", profile)
	}
	if semantic != nil {
		t.Fatalf("structural full profile should not run semantic classifier, got %+v", semantic)
	}
}

func TestClassifyIMExecutionProfileGenericSearchStaysFull(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelSearch,
		Confidence: 0.90,
		Layer:      3,
		Reason:     "semantic broad search intent",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "\u641c\u7d22\u6700\u65b0AI\u8bba\u6587"}, false, false, semantic)
	if profile.IsLight() || profile.IsDirect() {
		t.Fatalf("generic search profile = %+v, want full", profile)
	}
}

func TestClassifyIMExecutionProfileGenericNonCodingStaysFull(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelNonCoding,
		Confidence: 0.90,
		Layer:      3,
		Reason:     "semantic broad non-coding intent",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "\u7ffb\u8bd1\u8fd9\u6bb5\u6587\u6863"}, false, false, semantic)
	if profile.IsLight() || profile.IsDirect() {
		t.Fatalf("generic non_coding profile = %+v, want full", profile)
	}
}

func TestClassifyIMExecutionProfileComplexWorkStaysFull(t *testing.T) {
	cases := []string{
		"\u8bfb\u53d6 ~/.maclaw \u65e5\u5fd7\u5e76\u5206\u6790\u4f18\u5316\u65b9\u6848",
		"\u5e2e\u6211\u4fee\u590d\u8fd9\u4e2a\u9879\u76ee\u91cc\u7684\u4ee3\u7801",
		"\u751f\u6210\u4e00\u4e2a\u6280\u672f\u65b9\u6848\u5e76\u5199\u5165\u6587\u4ef6",
		"\u7ee7\u7eed\u63a8\u8fdb",
		"\u5f00\u5de5",
	}
	for _, input := range cases {
		profile := classifyIMExecutionProfile(IMUserMessage{Text: input}, false, false)
		if profile.IsLight() {
			t.Fatalf("profile for %q = light, want full: %+v", input, profile)
		}
	}
}

func TestClassifyIMExecutionProfileShortAmbiguousTextStaysFull(t *testing.T) {
	profile := classifyIMExecutionProfile(IMUserMessage{Text: "\u968f\u4fbf\u770b\u770b"}, false, false)
	if profile.IsLight() {
		t.Fatalf("ambiguous short text should stay full: %+v", profile)
	}
}

func TestClassifyIMExecutionProfileDirectOnlyForSemanticDeterministicTool(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelCurrentTime,
		Confidence: 0.97,
		ToolNames:  []string{"current_datetime"},
		Layer:      3,
		Reason:     "semantic direct clock tool",
	}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(IMUserMessage{Text: "\u73b0\u5728\u51e0\u70b9"}, false, false, semantic, explicitInferredExecutionContractForTest)
	if !profile.IsDirect() || profile.DirectToolName != "current_datetime" {
		t.Fatalf("profile = %+v, want direct current_datetime", profile)
	}
	liveSemantic := &intent.ClassificationResult{
		Primary:    intent.LabelSearch,
		Confidence: 0.97,
		ToolNames:  []string{"web_search"},
		Layer:      3,
		Reason:     "semantic live search tool",
	}
	live := classifyIMExecutionProfileWithSemanticAndContracts(IMUserMessage{Text: "\u5929\u6c14"}, false, false, liveSemantic, explicitInferredExecutionContractForTest)
	if live.IsDirect() {
		t.Fatalf("non-deterministic semantic tool must not use direct profile: %+v", live)
	}
}

func TestClassifyIMExecutionProfileDirectUsesRegistryContract(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:        "fast_status",
		Description: "fast status",
		Status:      RegToolAvailable,
		ExecutionContract: map[string]interface{}{
			"capabilities":            []interface{}{"status"},
			"deterministic":           true,
			"supports_direct":         true,
			"requires_agent_planning": false,
		},
		Handler: func(args map[string]interface{}) string {
			return "ok"
		},
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{registry: registry}
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelNonCoding,
		Confidence: 0.97,
		ToolNames:  []string{"fast_status"},
		Layer:      3,
		Reason:     "semantic custom deterministic tool",
	}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(IMUserMessage{Text: "status"}, false, false, semantic, h.executionContractForRegisteredToolName)
	if !profile.IsDirect() || profile.DirectToolName != "fast_status" {
		t.Fatalf("profile = %+v, want direct fast_status", profile)
	}
	if len(profile.RequiredCapabilities) != 1 || profile.RequiredCapabilities[0] != "status" {
		t.Fatalf("capabilities = %v, want [status]", profile.RequiredCapabilities)
	}
}

func TestClassifyIMExecutionProfileDirectRequiresExplicitContract(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelCurrentTime,
		Confidence: 0.97,
		ToolNames:  []string{"current_datetime"},
		Layer:      3,
		Reason:     "semantic clock tool without explicit contract",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "\u73b0\u5728\u51e0\u70b9"}, false, false, semantic)
	if profile.IsDirect() {
		t.Fatalf("profile = %+v, want non-direct without explicit tool contract", profile)
	}
}

func TestClassifyIMExecutionProfileLocalCurrentTimeFallbackUsesDirectTool(t *testing.T) {
	profile := classifyIMExecutionProfileWithSemanticAndContracts(IMUserMessage{Text: "\u73b0\u5728\u51e0\u70b9\uff1f"}, false, false, nil, explicitInferredExecutionContractForTest)
	if !profile.IsDirect() || profile.DirectToolName != "current_datetime" {
		t.Fatalf("profile = %+v, want direct current_datetime from local time intent", profile)
	}
	if profile.Reason != "local deterministic current time intent" {
		t.Fatalf("reason = %q, want local deterministic current time intent", profile.Reason)
	}
}

func TestClassifyIMExecutionProfileLocalCurrentTimeAllowsLongPoliteQuery(t *testing.T) {
	msg := IMUserMessage{Text: "\u9ebb\u70e6\u4f60\u770b\u4e00\u4e0b\u6211\u8fd9\u8fb9\u7684\u5f53\u524d\u65f6\u95f4\uff0c\u73b0\u5728\u51e0\u70b9\u4e86\uff1f\u987a\u4fbf\u544a\u8bc9\u6211\u4eca\u5929\u5468\u51e0\uff0c\u8c22\u8c22"}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(msg, false, false, nil, explicitInferredExecutionContractForTest)
	if !profile.IsDirect() || profile.DirectToolName != "current_datetime" {
		t.Fatalf("profile = %+v, want direct current_datetime for long polite current-time query", profile)
	}
}

func TestClassifyIMExecutionProfileLocalCurrentTimeStillSkipsAttachments(t *testing.T) {
	msg := IMUserMessage{
		Text:        "\u73b0\u5728\u51e0\u70b9\uff1f",
		Attachments: []MessageAttachment{{FileName: "note.txt"}},
	}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(msg, false, false, nil, explicitInferredExecutionContractForTest)
	if profile.IsDirect() {
		t.Fatalf("profile = %+v, want non-direct for attachment message", profile)
	}
}

func TestClassifyIMExecutionProfileLocalCurrentTimeAvoidsScheduleQuestions(t *testing.T) {
	for _, text := range []string{
		"\u4f1a\u8bae\u51e0\u70b9\u949f\u5f00\u59cb\uff1f",
		"\u73b0\u5728\u65f6\u95f4\u590d\u6742\u5ea6\u662f\u591a\u5c11\uff1f",
		"what is the current time complexity?",
	} {
		profile := classifyIMExecutionProfileWithSemanticAndContracts(IMUserMessage{Text: text}, false, false, nil, explicitInferredExecutionContractForTest)
		if profile.IsDirect() {
			t.Fatalf("profile = %+v, want non-direct for %q", profile, text)
		}
	}
}

func TestHandlerClassifyIMExecutionProfileUsesUnifiedIntentToolAffinity(t *testing.T) {
	uic := intent.New(intent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"current_time","score":0.98}]} `, nil
	}})
	h := &IMMessageHandler{
		registry:          NewToolRegistry(),
		unifiedClassifier: uic,
	}
	if err := h.registry.Register(RegisteredTool{
		Name:        "current_datetime",
		Description: "clock",
		Status:      RegToolAvailable,
		ExecutionContract: map[string]interface{}{
			"capabilities":            []interface{}{"time"},
			"deterministic":           true,
			"supports_direct":         true,
			"requires_agent_planning": false,
		},
		Handler: func(args map[string]interface{}) string {
			return "clock"
		},
	}); err != nil {
		t.Fatal(err)
	}
	profile := h.classifyIMExecutionProfile(IMUserMessage{Text: "\u73b0\u5728\u51e0\u70b9"}, false, false)
	if !profile.IsDirect() || profile.DirectToolName != "current_datetime" {
		t.Fatalf("profile = %+v, want direct current_datetime from UIC tool affinity", profile)
	}
}

func TestHandlerClassifyIMExecutionProfileLocalCurrentTimeSkipsUIC(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:        "current_datetime",
		Description: "clock",
		Status:      RegToolAvailable,
		ExecutionContract: map[string]interface{}{
			"capabilities":            []interface{}{"time"},
			"deterministic":           true,
			"supports_direct":         true,
			"requires_agent_planning": false,
		},
		Handler: func(args map[string]interface{}) string {
			return "clock"
		},
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{
		registry: registry,
		unifiedClassifier: intent.New(intent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
			t.Fatal("current local time query should not call UIC")
			return "", nil
		}}),
	}
	profile := h.classifyIMExecutionProfile(IMUserMessage{Text: "\u73b0\u5728\u51e0\u70b9\uff1f"}, false, false)
	if !profile.IsDirect() || profile.DirectToolName != "current_datetime" {
		t.Fatalf("profile = %+v, want direct current_datetime without UIC", profile)
	}
}

func TestExecutionProfileSemanticResultReusedByCodingGate(t *testing.T) {
	calls := 0
	uic := intent.New(intent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		calls++
		return `{"top":[{"skill":"live_data","score":0.95}]} `, nil
	}})
	h := &IMMessageHandler{unifiedClassifier: uic}
	msg := IMUserMessage{Text: "\u5929\u6c14", UserID: "user-1"}
	profile, semantic := h.classifyIMExecutionProfileAndSemantic(msg, false, false)
	if semantic == nil {
		t.Fatalf("expected semantic result")
	}
	ctx := NewLoopContext("chat", 300, nil)
	ctx.Runtime.Execution = profile
	ctx.Runtime.SemanticIntent = semantic
	// Execution-profile routing uses ClassifyEmbeddingOnly for latency; the L3
	// tree/LLM channel must not run on this hot path.
	if calls != 0 {
		t.Fatalf("UIC LLM calls = %d, want 0 (embedding-only path)", calls)
	}
}

func TestFilterToolsForExecutionProfileLightKeepsOnlyLowCostTools(t *testing.T) {
	tools := []map[string]interface{}{
		toolDef("manage_skill", "manage skills", nil, nil),
		toolDef("web_fetch", "fetch web", nil, nil),
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("group_discussion", "discuss", nil, nil),
		toolDef("async_wait", "wait", nil, nil),
	}
	for _, def := range tools {
		if contract := defaultExplicitExecutionContractMetadata(extractToolName(def)); len(contract) > 0 {
			def["x_execution_contract"] = contract
		}
	}
	profile := ExecutionProfile{Layer: string(executionLayerLight), RequiredCapabilities: []string{"skill", "web", "async_status"}, ToolBudget: 8}
	filtered := filterToolsForExecutionProfile(tools, profile)
	names := map[string]bool{}
	for _, def := range filtered {
		names[extractToolName(def)] = true
	}
	for _, want := range []string{"manage_skill", "web_fetch", "async_wait"} {
		if !names[want] {
			t.Fatalf("filtered tools missing %s: %v", want, executionProfileToolNames(filtered))
		}
	}
	for _, blocked := range []string{"bash", "read_file", "group_discussion"} {
		if names[blocked] {
			t.Fatalf("filtered tools should not include %s: %v", blocked, executionProfileToolNames(filtered))
		}
	}
}

func TestFilterToolsForExecutionProfileLightRequiresCapabilityMatch(t *testing.T) {
	tools := []map[string]interface{}{
		toolDef("manage_skill", "manage skills", nil, nil),
		toolDef("web_search", "search web", nil, nil),
		toolDef("current_datetime", "clock", nil, nil),
		toolDef("async_wait", "wait", nil, nil),
	}
	for _, def := range tools {
		if contract := defaultExplicitExecutionContractMetadata(extractToolName(def)); len(contract) > 0 {
			def["x_execution_contract"] = contract
		}
	}
	profile := ExecutionProfile{Layer: string(executionLayerLight), RequiredCapabilities: []string{"current_data", "time"}, ToolBudget: 8}
	filtered := filterToolsForExecutionProfile(tools, profile)
	names := map[string]bool{}
	for _, def := range filtered {
		names[extractToolName(def)] = true
	}
	for _, want := range []string{"web_search", "current_datetime"} {
		if !names[want] {
			t.Fatalf("filtered tools missing %s: %v", want, executionProfileToolNames(filtered))
		}
	}
	for _, blocked := range []string{"manage_skill", "web_fetch", "async_wait"} {
		if names[blocked] {
			t.Fatalf("filtered tools should not include %s: %v", blocked, executionProfileToolNames(filtered))
		}
	}
}

func TestFilterToolsForExecutionProfileLightKeepsMatchedSkillCapabilities(t *testing.T) {
	tools := []map[string]interface{}{
		toolDef("manage_skill", "manage skills", nil, nil),
		toolDef("web_search", "search web", nil, nil),
		toolDef("current_datetime", "clock", nil, nil),
		toolDef("async_wait", "wait", nil, nil),
	}
	for _, def := range tools {
		if contract := defaultExplicitExecutionContractMetadata(extractToolName(def)); len(contract) > 0 {
			def["x_execution_contract"] = contract
		}
	}
	tools[0]["x_execution_contract"] = map[string]interface{}{
		"capabilities":            []interface{}{"Skill", "CURRENT-DATA"},
		"requires_agent_planning": false,
	}
	profile := ExecutionProfile{
		Layer:                string(executionLayerLight),
		TaskType:             string(intent.LabelLiveData),
		RequiredCapabilities: []string{"current_data", "time"},
		ToolBudget:           8,
	}
	filtered := filterToolsForExecutionProfile(tools, profile)
	names := map[string]bool{}
	for _, def := range filtered {
		names[extractToolName(def)] = true
	}
	for _, want := range []string{"manage_skill", "web_search", "current_datetime"} {
		if !names[want] {
			t.Fatalf("filtered tools missing %s: %v", want, executionProfileToolNames(filtered))
		}
	}
	if names["async_wait"] {
		t.Fatalf("filtered tools should not include async_wait: %v", executionProfileToolNames(filtered))
	}
}

func TestFilterToolsForExecutionProfileLightFallsOpenWhenNoContractMatches(t *testing.T) {
	tools := []map[string]interface{}{
		toolDef("manage_skill", "manage skills", nil, nil),
		toolDef("web_search", "search web", nil, nil),
	}
	for _, def := range tools {
		if contract := defaultExplicitExecutionContractMetadata(extractToolName(def)); len(contract) > 0 {
			def["x_execution_contract"] = contract
		}
	}
	profile := ExecutionProfile{
		Layer:                string(executionLayerLight),
		RequiredCapabilities: []string{"capability_not_declared"},
		ToolBudget:           8,
	}

	filtered := filterToolsForExecutionProfile(tools, profile)
	if len(filtered) != len(tools) {
		t.Fatalf("light filter should fall open when no contract matches; got %v", executionProfileToolNames(filtered))
	}
}

func TestPrepareAgentLoopToolsLightKeepsMatchedSkillFirst(t *testing.T) {
	registry := NewToolRegistry()
	h := &IMMessageHandler{
		app:      &App{},
		registry: registry,
	}
	registerBuiltinTools(registry, h)
	registerNonCodeTools(registry, h.app)
	for i := 0; i < 40; i++ {
		if err := registry.Register(RegisteredTool{
			Name:        fmt.Sprintf("filler_tool_%02d", i),
			Description: "generic filler tool",
			Category:    ToolCategoryNonCode,
			Status:      RegToolAvailable,
		}); err != nil {
			t.Fatal(err)
		}
	}
	h.toolBuilder = NewDynamicToolBuilder(registry)
	router := NewToolRouter(nil)
	router.SetSkillProvider(&executionProfileSkillProviderForTest{skills: []coretool.SkillSummary{{
		Name:         "Live Lookup",
		Triggers:     []string{"live lookup"},
		Description:  "live current data lookup",
		Capabilities: []string{"current_data"},
	}}})
	h.unifiedClassifier = intent.New(intent.Config{
		LLMFunc: func(_, _ string) (string, error) {
			return fmt.Sprintf(`{"top":[{"skill":%q,"score":0.95,"reason":"test live data"}]}`, intent.LabelLiveData), nil
		},
	})
	h.SetToolRouter(router)

	ctx := &LoopContext{
		SkipNeedsConfirmGate: true,
		Runtime: RuntimeContext{
			RequestID: "req-light-skill",
			Execution: ExecutionProfile{
				Layer:                string(executionLayerLight),
				TaskType:             string(intent.LabelLiveData),
				PromptProfile:        "light",
				Confidence:           0.91,
				Reason:               "test live data profile",
				RequiredCapabilities: []string{"current_data", "time"},
				ToolBudget:           8,
				IterationBudget:      2,
			},
		},
	}
	toolSet := h.prepareAgentLoopTools("test-user", "live lookup", ctx, agentLoopPhase{})
	if len(toolSet.Tools) == 0 {
		t.Fatal("expected light tool set")
	}
	if got := extractToolName(toolSet.Tools[0]); got != "manage_skill" {
		t.Fatalf("first tool = %q; tools=%s, want manage_skill first", got, executionProfileToolNames(toolSet.Tools))
	}
	names := map[string]bool{}
	for _, def := range toolSet.Tools {
		if _, ok := def["x_execution_contract"]; ok {
			t.Fatalf("LLM tool leaked execution contract: %#v", def)
		}
		names[extractToolName(def)] = true
	}
	if !names["web_search"] && !names["current_datetime"] {
		t.Fatalf("light live data tools should retain generic current-data/time path too: %s", executionProfileToolNames(toolSet.Tools))
	}
}

func TestFilterToolsForExecutionProfileLightWithoutExplicitContractsFallsBack(t *testing.T) {
	tools := []map[string]interface{}{
		toolDef("manage_skill", "manage skills", nil, nil),
		toolDef("bash", "run shell", nil, nil),
	}
	profile := ExecutionProfile{Layer: string(executionLayerLight), ToolBudget: 8}
	filtered := filterToolsForExecutionProfile(tools, profile)
	if len(filtered) != len(tools) {
		t.Fatalf("filtered len = %d, want fallback len %d", len(filtered), len(tools))
	}
}

func TestFilterToolsForExecutionProfileLightFallsBackWhenExplicitContractsMismatch(t *testing.T) {
	tools := []map[string]interface{}{
		toolDef("manage_skill", "manage skills", nil, nil),
		toolDef("async_wait", "wait", nil, nil),
		toolDef("bash", "run shell", nil, nil),
	}
	for _, def := range tools {
		if contract := defaultExplicitExecutionContractMetadata(extractToolName(def)); len(contract) > 0 {
			def["x_execution_contract"] = contract
		}
	}
	profile := ExecutionProfile{Layer: string(executionLayerLight), RequiredCapabilities: []string{"current_data"}, ToolBudget: 8}
	filtered := filterToolsForExecutionProfile(tools, profile)
	if len(filtered) != len(tools) {
		t.Fatalf("filtered tools = %v, want fallback when explicit contracts mismatch", executionProfileToolNames(filtered))
	}
}

func TestExecutionContractMetadataControlsLightTools(t *testing.T) {
	fastStatus := toolDef("fast_status", "status", nil, nil)
	fastStatus["x_execution_contract"] = map[string]interface{}{
		"capabilities":            []interface{}{"time"},
		"deterministic":           true,
		"supports_direct":         true,
		"requires_agent_planning": false,
		"avg_latency_ms":          float64(10),
	}
	planner := toolDef("planner", "planner", nil, nil)
	planner["x_execution_contract"] = map[string]interface{}{
		"capabilities":            []interface{}{"web"},
		"requires_agent_planning": true,
	}
	profile := ExecutionProfile{Layer: string(executionLayerLight), ToolBudget: 8}
	filtered := filterToolsForExecutionProfile([]map[string]interface{}{fastStatus, planner}, profile)
	if len(filtered) != 1 || extractToolName(filtered[0]) != "fast_status" {
		t.Fatalf("filtered tools = %v, want only fast_status", executionProfileToolNames(filtered))
	}
}

func TestStripExecutionContractMetadataForLLMRemovesInternalField(t *testing.T) {
	tool := toolDef("fast_status", "status", nil, nil)
	tool["x_execution_contract"] = map[string]interface{}{
		"capabilities": []interface{}{"time"},
	}
	stripped := stripExecutionContractMetadataForLLM([]map[string]interface{}{tool})
	if _, ok := stripped[0]["x_execution_contract"]; ok {
		t.Fatalf("stripped tool still has execution contract: %#v", stripped[0])
	}
	if _, ok := tool["x_execution_contract"]; !ok {
		t.Fatalf("strip should not mutate source tool")
	}
	if extractToolName(stripped[0]) != "fast_status" {
		t.Fatalf("stripped tool name = %q", extractToolName(stripped[0]))
	}
}

func TestComputeAgentLoopIterationLimitsUsesLightBudget(t *testing.T) {
	ctx := NewLoopContext("chat", 300, nil)
	ctx.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), IterationBudget: 3}
	limits := computeAgentLoopIterationLimits(ctx, 300, 0)
	if limits.EffectiveMax != 3 || limits.ChatFinalizeGrace != 1 {
		t.Fatalf("limits = %+v, want effectiveMax=3 grace=1", limits)
	}
}

func TestLightFinalizeRoundRunsWithoutTools(t *testing.T) {
	ctx := NewLoopContext("chat", 300, nil)
	ctx.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), IterationBudget: 2}
	if shouldForceLightFinalizeWithoutTools(ctx, 1, 2, 1) {
		t.Fatalf("iteration before light budget should keep tools")
	}
	if !shouldForceLightFinalizeWithoutTools(ctx, 2, 2, 1) {
		t.Fatalf("light finalize grace round should remove tools")
	}
	ctx.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerFull)}
	if shouldForceLightFinalizeWithoutTools(ctx, 2, 2, 1) {
		t.Fatalf("full profile should keep normal finalize behavior")
	}
}

func TestBuildLightIMSystemPromptStaysSmall(t *testing.T) {
	profile := ExecutionProfile{
		Layer:         string(executionLayerLight),
		TaskType:      "simple_lookup",
		PromptProfile: "light",
		Confidence:    0.78,
		Reason:        "test",
	}
	prompt := buildLightIMSystemPrompt(IMUserMessage{Text: "\u5927\u8fde\u5929\u6c14"}, profile)
	// Light bundle includes the shared Chinese output-format fence (~1.5KB) plus a
	// short GUI capability fence. Keep a hard cap so full-agent sections cannot creep in.
	if len(prompt) > 2500 {
		t.Fatalf("light prompt len = %d, want <= 2500", len(prompt))
	}
	for _, blocked := range []string{"Group Discussion", "CodingSubAgent", "compress_context"} {
		if containsText(prompt, blocked) {
			t.Fatalf("light prompt should not contain full-agent section %q: %s", blocked, prompt)
		}
	}
}

func TestBuildLightIMSystemPromptIncludesBotBindingContext(t *testing.T) {
	profile := ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}
	prompt := buildLightIMSystemPrompt(IMUserMessage{
		Text: "查询客服手册",
		AssistantBinding: &agent.AssistantBinding{
			BotProfileID: "support", WorkingDirectory: "D:/support/source",
			DocumentDirectories: []string{"D:/support/manuals"}, InitialPrompt: "仅处理客服问题",
		},
	}, profile)
	for _, want := range []string{"bot_profile_id: support", "D:/support/source", "D:/support/manuals", "仅处理客服问题"} {
		if !containsText(prompt, want) {
			t.Fatalf("light bot prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildIMEntrySystemPromptWorkflowLoopOverridesLightProfile(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-loop-light-profile-prompt-override"
	handler.stashedPhasePrompt.Store(userID, "## Coding Implementation Handoff Contract\nCodingSubAgent delegate_task(agent=\"coding_workflow\")")
	ctx := &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{
		Layer:         string(executionLayerLight),
		TaskType:      string(intent.LabelLiveData),
		PromptProfile: "light",
		Reason:        "stale light profile",
	}}}

	prompt := handler.buildIMEntrySystemPrompt(IMUserMessage{
		UserID:   userID,
		Text:     "\u7ee7\u7eed\u63a8\u8fdb",
		Platform: "desktop",
	}, nil, ctx, true, "", "", "", "")

	for _, bad := range []string{"low-complexity lookup task", "Do not inspect local files"} {
		if containsText(prompt, bad) {
			t.Fatalf("workflow agent loop must not use light prompt fragment %q:\n%s", bad, prompt)
		}
	}
	for _, want := range []string{"Coding Implementation Handoff Contract", "CodingSubAgent", "delegate_task(agent=\"coding_workflow\""} {
		if !containsText(prompt, want) {
			t.Fatalf("workflow prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestTryDirectExecutionProfileRunsToolAndSavesHistory(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:        "current_datetime",
		Description: "test clock",
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{},
		ExecutionContract: map[string]interface{}{
			"capabilities":            []interface{}{"time"},
			"deterministic":           true,
			"supports_direct":         true,
			"requires_agent_planning": false,
		},
		Handler: func(args map[string]interface{}) string {
			return "2026-06-05 12:34:56"
		},
	}); err != nil {
		t.Fatal(err)
	}
	userID := "direct-user"
	h := &IMMessageHandler{
		app:      &App{testHomeDir: t.TempDir()},
		memory:   agent.NewConversationMemory(),
		registry: registry,
	}
	msg := IMUserMessage{UserID: userID, Text: "\u73b0\u5728\u51e0\u70b9", RequestID: "req-direct"}
	loopCtx := NewLoopContext("chat", 300, nil)
	loopCtx.Runtime = runtimeContextFromIMMessage(msg)
	loopCtx.Runtime.Execution = ExecutionProfile{
		Layer:          string(executionLayerDirect),
		TaskType:       "direct_tool",
		PromptProfile:  "none",
		Confidence:     0.97,
		Reason:         "test semantic direct tool",
		DirectToolName: "current_datetime",
		ToolBudget:     1,
	}
	resp, handled := h.tryDirectExecutionProfile(msg, loopCtx, nil)
	if !handled || resp == nil {
		t.Fatalf("direct execution handled=%v resp=%v", handled, resp)
	}
	if resp.ResponseSource != "direct_execution" || resp.RequestID != "req-direct" {
		t.Fatalf("resp = %+v, want direct source and request id", resp)
	}
	if !containsText(resp.Text, "2026-06-05 12:34:56") {
		t.Fatalf("resp text = %q, want tool output", resp.Text)
	}
	history := h.memory.Load(userID)
	if len(history) != 2 || history[0].Role != "user" || history[1].Role != "assistant" {
		t.Fatalf("history = %+v, want user+assistant entries", history)
	}
}

func TestTryImmediateCurrentTimeDirectSkipsProvidedLoop(t *testing.T) {
	h := &IMMessageHandler{}
	loopCtx := NewLoopContext("existing", 1, nil)
	resp, handled := h.tryImmediateCurrentTimeDirect(IMUserMessage{Text: "\u73b0\u5728\u51e0\u70b9\uff1f"}, loopCtx)
	if handled || resp != nil {
		t.Fatalf("tryImmediateCurrentTimeDirect handled provided loop response=%+v handled=%v, want skip", resp, handled)
	}
}

func TestTryImmediateScheduleListDirectUsesManageSchedule(t *testing.T) {
	baseDir := t.TempDir()
	manager, err := scheduler.NewManager(filepath.Join(baseDir, "scheduled_tasks.json"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(manager.Stop)
	id, err := manager.Add(scheduler.ScheduledTask{
		Name:       "蓝信日报",
		Action:     "发送日报",
		Hour:       9,
		Minute:     0,
		DayOfWeek:  -1,
		DayOfMonth: -1,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	app := &App{testHomeDir: baseDir, scheduledTaskManager: manager}
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	msg := IMUserMessage{
		UserID:    "lansenger-user",
		Platform:  "lansenger_local",
		RequestID: "schedule-list-direct",
		Text:      "查看下定时任务",
	}
	resp, handled := h.tryImmediateScheduleListDirect(msg, nil)
	if !handled || resp == nil {
		t.Fatalf("tryImmediateScheduleListDirect handled=%v resp=%+v, want direct response", handled, resp)
	}
	if resp.ResponseSource != "direct_execution" || resp.RequestID != msg.RequestID {
		t.Fatalf("response = %+v, want direct execution with request ID", resp)
	}
	if !containsText(resp.Text, "蓝信日报") || !containsText(resp.Text, id) {
		t.Fatalf("response text = %q, want scheduled task name and ID", resp.Text)
	}
}

func TestTryImmediateScheduleListDirectOnlyHandlesReadOnlyQueries(t *testing.T) {
	for _, text := range []string{"执行定时任务 abc", "帮我创建定时任务", "暂停定时任务 abc"} {
		if isExplicitScheduledTaskListQuery(text) {
			t.Fatalf("isExplicitScheduledTaskListQuery(%q) = true, want false", text)
		}
	}
	if !isExplicitScheduledTaskListQuery("查看下定时任务") {
		t.Fatal("isExplicitScheduledTaskListQuery should recognize an explicit schedule list query")
	}
	if !isExplicitScheduledTaskListQuery("list scheduled tasks") {
		t.Fatal("isExplicitScheduledTaskListQuery should recognize an English schedule list query")
	}

	h := &IMMessageHandler{}
	loopCtx := NewLoopContext("existing", 1, nil)
	if resp, handled := h.tryImmediateScheduleListDirect(IMUserMessage{Text: "查看定时任务"}, loopCtx); handled || resp != nil {
		t.Fatalf("tryImmediateScheduleListDirect with existing loop = (%+v, %v), want skip", resp, handled)
	}
}

func TestTryImmediateScheduleRunDirectUsesManageSchedule(t *testing.T) {
	baseDir := t.TempDir()
	manager, err := scheduler.NewManager(filepath.Join(baseDir, "scheduled_tasks.json"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(manager.Stop)
	executed := make(chan *scheduler.ScheduledTask, 1)
	manager.SetExecutor(func(_ context.Context, task *scheduler.ScheduledTask) (string, error) {
		executed <- task
		return "done", nil
	})
	id, err := manager.Add(scheduler.ScheduledTask{
		Name:       "蓝信日报",
		Action:     "发送日报",
		Hour:       9,
		Minute:     0,
		DayOfWeek:  -1,
		DayOfMonth: -1,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	app := &App{testHomeDir: baseDir, scheduledTaskManager: manager}
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	msg := IMUserMessage{
		UserID:    "lansenger-user",
		Platform:  "lansenger_local",
		RequestID: "schedule-run-direct",
		Text:      "立即执行定时任务 " + id,
	}
	resp, handled := h.tryImmediateScheduleRunDirect(msg, nil)
	if !handled || resp == nil {
		t.Fatalf("tryImmediateScheduleRunDirect handled=%v resp=%+v, want direct response", handled, resp)
	}
	if resp.ResponseSource != "direct_execution" || !containsText(resp.Text, id) {
		t.Fatalf("response = %+v, want direct run confirmation with task ID", resp)
	}
	select {
	case task := <-executed:
		if task.ID != id {
			t.Fatalf("executed task ID = %q, want %q", task.ID, id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("direct schedule run did not execute the task")
	}
}

func TestExplicitScheduledTaskRunIDRequiresRunVerbAndTaskID(t *testing.T) {
	id := "1710000000000000000-abcd"
	if got, ok := explicitScheduledTaskRunID("执行定时任务 " + id); !ok || got != id {
		t.Fatalf("explicitScheduledTaskRunID() = (%q, %v), want (%q, true)", got, ok, id)
	}
	for _, text := range []string{
		"查看定时任务 " + id,
		"执行任务 " + id,
		"执行定时任务 123",
		"执行定时任务 2026-07-22",
	} {
		if got, ok := explicitScheduledTaskRunID(text); ok || got != "" {
			t.Fatalf("explicitScheduledTaskRunID(%q) = (%q, %v), want no match", text, got, ok)
		}
	}
}

func TestTryDirectExecutionProfileRequiresSemanticToolName(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	msg := IMUserMessage{UserID: "direct-user", Text: "\u73b0\u5728\u51e0\u70b9", RequestID: "req-direct"}
	loopCtx := NewLoopContext("chat", 300, nil)
	loopCtx.Runtime = runtimeContextFromIMMessage(msg)
	loopCtx.Runtime.Execution = ExecutionProfile{
		Layer:         string(executionLayerDirect),
		TaskType:      "time_query",
		PromptProfile: "none",
		Confidence:    0.97,
		Reason:        "legacy task type must not imply tool",
	}
	if resp, handled := h.tryDirectExecutionProfile(msg, loopCtx, nil); handled || resp != nil {
		t.Fatalf("direct execution handled=%v resp=%v, want fallback without DirectToolName", handled, resp)
	}
}

func TestTryDirectExecutionProfileRequiresExplicitContract(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:        "current_datetime",
		Description: "test clock",
		Status:      RegToolAvailable,
		Handler: func(args map[string]interface{}) string {
			return "2026-06-05 12:34:56"
		},
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{registry: registry}
	msg := IMUserMessage{UserID: "direct-user", Text: "\u73b0\u5728\u51e0\u70b9", RequestID: "req-direct"}
	loopCtx := NewLoopContext("chat", 300, nil)
	loopCtx.Runtime = runtimeContextFromIMMessage(msg)
	loopCtx.Runtime.Execution = ExecutionProfile{
		Layer:          string(executionLayerDirect),
		TaskType:       "direct_tool",
		PromptProfile:  "none",
		Confidence:     0.97,
		Reason:         "direct profile without explicit contract",
		DirectToolName: "current_datetime",
		ToolBudget:     1,
	}
	if resp, handled := h.tryDirectExecutionProfile(msg, loopCtx, nil); handled || resp != nil {
		t.Fatalf("direct execution handled=%v resp=%v, want fallback without explicit contract", handled, resp)
	}
}

func TestTryDirectExecutionProfileUsesRegistryContract(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:        "fast_status",
		Description: "fast status",
		Status:      RegToolAvailable,
		ExecutionContract: map[string]interface{}{
			"capabilities":            []interface{}{"status"},
			"deterministic":           true,
			"supports_direct":         true,
			"requires_agent_planning": false,
		},
		Handler: func(args map[string]interface{}) string {
			return "custom status ok"
		},
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{registry: registry}
	msg := IMUserMessage{UserID: "direct-user", Text: "status", RequestID: "req-direct"}
	loopCtx := NewLoopContext("chat", 300, nil)
	loopCtx.Runtime = runtimeContextFromIMMessage(msg)
	loopCtx.Runtime.Execution = ExecutionProfile{
		Layer:          string(executionLayerDirect),
		TaskType:       "direct_tool",
		PromptProfile:  "none",
		Confidence:     0.97,
		Reason:         "test semantic direct tool",
		DirectToolName: "fast_status",
		ToolBudget:     1,
	}
	resp, handled := h.tryDirectExecutionProfile(msg, loopCtx, nil)
	if !handled || resp == nil || resp.Text != "custom status ok" {
		t.Fatalf("direct execution handled=%v resp=%+v, want custom status ok", handled, resp)
	}
}

func TestActivePostConversationRequestIDUsesSessionLoop(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("chat", 300, nil)
	ctx.Runtime.RequestID = "req-post"
	h.setSessionLoopCtx("desktop-user", ctx)
	if got := h.activePostConversationRequestID("desktop-user"); got != "req-post" {
		t.Fatalf("activePostConversationRequestID() = %q, want req-post", got)
	}
}

func explicitInferredExecutionContractForTest(name string) ToolExecutionContract {
	contract := inferredExecutionContract(name)
	contract.Explicit = true
	return contract
}

func TestPrepareAgentLoopToolsLightKeepsToolResultReader(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	if err := h.registry.Register(RegisteredTool{Name: "web_search", Description: "search", Status: RegToolAvailable}); err != nil {
		t.Fatal(err)
	}
	if err := h.registry.Register(RegisteredTool{Name: "read_tool_result", Description: "reader", Status: RegToolAvailable}); err != nil {
		t.Fatal(err)
	}
	ctx := NewLoopContext("chat", 3, nil)
	ctx.Runtime.Execution = ExecutionProfile{
		Layer:                string(executionLayerLight),
		PromptProfile:        "light",
		RequiredCapabilities: []string{"web"},
		ToolBudget:           1,
	}
	tools := h.prepareAgentLoopTools("reader-user", "search weather", ctx, agentLoopPhase{}).Tools
	names := make(map[string]bool, len(tools))
	for _, def := range tools {
		names[extractToolName(def)] = true
	}
	if !names["read_tool_result"] {
		t.Fatalf("light tools must retain the handle reader: %#v", names)
	}
}
