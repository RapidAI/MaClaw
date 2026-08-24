package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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
	if profile.ToolBudget != 1 {
		t.Fatalf("live_data tool budget = %d, want 1 search selection", profile.ToolBudget)
	}
}

func TestClassifyIMExecutionProfileManagedLookupIgnoresLengthGate(t *testing.T) {
	text := strings.Repeat("查实时天气", 12) // 48 runes, above the 40-rune full-profile gate
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: text}, false, false, &intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: .95,
	})
	if !profile.IsLight() || !profile.PromptIsLight() {
		t.Fatalf("managed lookup must stay light despite length, got %+v", profile)
	}
	search := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "全网搜索张慧妹资料"}, false, false, &intent.ClassificationResult{
		Primary: intent.LabelSearch, Confidence: .96,
	})
	if !search.IsLight() || !search.PromptIsLight() {
		t.Fatalf("web search lookup must stay light, got %+v", search)
	}
}

func TestClassifyIMExecutionProfileDoesNotPromoteLookupFromWording(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelKnowledgeRead,
		Confidence: 0.96,
		Reason:     "embedding: top=knowledge_read",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "全网搜索张慧妹资料"}, false, false, semantic)
	if semantic.Primary != intent.LabelKnowledgeRead {
		t.Fatalf("wording changed semantic primary to %s", semantic.Primary)
	}
	if profile.IsLight() || profile.Reason != "semantic capability-managed intent" {
		t.Fatalf("unconfirmed lookup wording must not create a light search route, got %+v", profile)
	}
	weather := &intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true}
	profile = classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "北京天气"}, false, false, weather)
	if weather.Primary != intent.LabelUnknown {
		t.Fatalf("weather wording changed semantic primary to %s", weather.Primary)
	}
	if profile.Reason != "semantic classifier degraded" {
		t.Fatalf("weather wording must not create a governed route, got %+v", profile)
	}
}

func TestClassifyIMExecutionProfileSemanticWeatherPDFUsesFullPlannedChain(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: .91,
	}
	profile := classifyIMExecutionProfileWithSemantic(
		IMUserMessage{Text: "北京天气，输出 格式化pdf报告"}, false, false, semantic,
	)
	if semantic.Primary != intent.LabelLiveData || len(semantic.Secondary) != 1 || semantic.Secondary[0] != intent.LabelDocumentGenerate {
		t.Fatalf("classification = %+v, want live_data + document_generate", semantic)
	}
	if profile.IsLight() || profile.Reason != "semantic capability-managed mutating intent" {
		t.Fatalf("profile = %+v, want full governed document-generation chain", profile)
	}
}

func TestClassifyIMExecutionProfileBorderlineLiveDataUsesLight(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Confidence: 0.785,
		Layer:      2,
		Reason:     "embedding: top=live_data (0.785), gap=0.120",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "天津天气"}, false, false, semantic)
	if !profile.IsLight() {
		t.Fatalf("profile layer = %q, want light; reason=%s", profile.Layer, profile.Reason)
	}
}

func TestClassifyIMExecutionProfileWeakLookupHintUsesLightChat(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Confidence: 0.61,
		Layer:      2,
		Degraded:   true,
		Reason:     "embedding ambiguous; short lookup skipped tree (l2=live_data conf=0.61)",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "北京天所"}, false, false, semantic)
	if !profile.IsLight() || profile.TaskType != "general" || profile.Reason != "semantic lookup hint below floor" {
		t.Fatalf("sub-floor lookup hint must be light chat, got %+v", profile)
	}
	for _, cap := range profile.RequiredCapabilities {
		if cap == "information.search.web" || cap == "web" {
			t.Fatalf("chat projection must not budget a search capability: %+v", profile)
		}
	}
}

func TestClassifyIMExecutionProfileTreeConfirmedShellIsManaged(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelShellCommand,
		Confidence: 0.75,
		Layer:      3,
		Reason:     "tree-after-embedding: shell_command (0.750)",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "清空当前目录"}, false, false, semantic)
	if profile.Reason != "semantic capability-managed mutating intent" {
		t.Fatalf("tree-confirmed shell must not use the L2 0.78 light-threshold miss, got %+v", profile)
	}
	if profile.IsLight() || profile.IsDirect() {
		t.Fatalf("tree-confirmed shell profile = %+v, want full mutating", profile)
	}
}

func TestClassifyIMExecutionProfileWeakDocumentReadUsesLightChat(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelDocumentRead,
		Confidence: 0.55,
		Layer:      3,
		Degraded:   true,
		Reason:     "tree-after-embedding: document_read (0.550)",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "图上有什么？"}, false, false, semantic)
	if !profile.IsLight() || profile.TaskType != "general" || profile.Reason != "semantic understand hint below floor" {
		t.Fatalf("weak document_read must be light chat, got %+v", profile)
	}
	hot := &intent.ClassificationResult{
		Primary:    intent.LabelFileRead,
		Confidence: 0.85,
		Layer:      2,
		Degraded:   true,
		Reason:     "embedding-only fallback",
	}
	hotProfile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "看看 notes.txt"}, false, false, hot)
	if hotProfile.Reason != "semantic capability-managed intent" {
		t.Fatalf("confident degraded file_read must keep a managed profile, got %+v", hotProfile)
	}
}

func TestClassifyIMExecutionProfileDegradedLiveDataLookupUsesLight(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Confidence: 0.73,
		Layer:      2,
		Degraded:   true,
		Reason:     "embedding-only fallback",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "成都天气"}, false, false, semantic)
	if !profile.IsLight() {
		t.Fatalf("degraded live_data lookup profile = %+v, want light", profile)
	}
}

func TestClassifyIMExecutionProfileWithoutSemanticStaysFull(t *testing.T) {
	profile := classifyIMExecutionProfile(IMUserMessage{Text: "\u5170\u5dde\u5929\u6c14"}, false, false)
	if profile.IsLight() || profile.IsDirect() {
		t.Fatalf("profile without semantic classifier = %+v, want full", profile)
	}
}

func TestHandlerClassifyIMExecutionProfileSkipsSemanticForStructuralFullWithoutClassifier(t *testing.T) {
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

func TestHandlerClassifyIMExecutionProfileRetainsSemanticIntentForStructuralFull(t *testing.T) {
	h := &IMMessageHandler{unifiedClassifier: semanticTestClassifier(t)}
	msg := IMUserMessage{
		Text:        "capture the primary screen",
		UserID:      "user-1",
		Attachments: []MessageAttachment{{Type: "image", FileName: "context.png", MimeType: "image/png", Data: "trusted"}},
	}
	profile, semantic := h.classifyIMExecutionProfileAndSemantic(msg, false, false)
	if profile.Layer != string(executionLayerFull) {
		t.Fatalf("profile=%+v, want structural full profile", profile)
	}
	if semantic == nil || semantic.Primary != intent.LabelScreenshot || !imSemanticIntentIsManaged(*semantic) {
		t.Fatalf("semantic=%+v, want retained managed screenshot intent", semantic)
	}
}

func TestClassifyIMExecutionProfileGenericSearchUsesManagedLightSurface(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelSearch,
		Confidence: 0.90,
		Layer:      3,
		Reason:     "semantic broad search intent",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "\u641c\u7d22\u6700\u65b0AI\u8bba\u6587"}, false, false, semantic)
	if !profile.IsLight() || profile.IsDirect() || profile.Reason != "semantic capability-managed lookup" {
		t.Fatalf("generic search profile = %+v, want managed light surface", profile)
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

func TestClassifyIMExecutionProfileDirectOnlyForUnmanagedSemanticDeterministicTool(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelNonCoding,
		Confidence: 0.97,
		ToolNames:  []string{"fast_status"},
		Layer:      3,
		Reason:     "semantic direct status tool",
	}
	contractForTool := func(name string) ToolExecutionContract {
		if name == "fast_status" {
			return ToolExecutionContract{Explicit: true, Deterministic: true, SupportsDirect: true}
		}
		return ToolExecutionContract{}
	}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(IMUserMessage{Text: "status"}, false, false, semantic, contractForTool)
	if !profile.IsDirect() || profile.DirectToolName != "fast_status" {
		t.Fatalf("profile = %+v, want direct fast_status", profile)
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

func TestCapabilityManagedSemanticIntentDoesNotCreateDirectNameExecution(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary: intent.LabelCurrentTime, Confidence: 0.97,
		// Legacy affinity data must not turn this governed family into a direct
		// current_datetime call.
		ToolNames: []string{"current_datetime"}, Layer: 3,
	}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(IMUserMessage{Text: "现在几点"}, false, false, semantic, explicitInferredExecutionContractForTest)
	if profile.IsDirect() || profile.Reason != "semantic capability-managed intent" {
		t.Fatalf("profile=%+v, want non-direct capability-managed route", profile)
	}
}

func TestCapabilityManagedWebIntentDoesNotCreateDirectNameExecution(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary: intent.LabelSearch, Confidence: 0.97,
		ToolNames: []string{"web_search"}, Layer: 3,
	}
	contractForTool := func(name string) ToolExecutionContract {
		if name == "web_search" {
			return ToolExecutionContract{Explicit: true, Deterministic: true, SupportsDirect: true}
		}
		return ToolExecutionContract{}
	}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(IMUserMessage{Text: "search Go docs"}, false, false, semantic, contractForTool)
	if profile.IsDirect() || !profile.IsLight() || profile.Reason != "semantic capability-managed lookup" {
		t.Fatalf("profile=%+v, want light non-direct capability-managed route", profile)
	}
}

func TestManagedSecondaryIntentDoesNotCreateDirectNameExecution(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary: intent.LabelNonCoding, Secondary: []intent.IntentLabel{intent.LabelSearch}, Confidence: 0.97,
		ToolNames: []string{"fast_status"}, Layer: 3,
	}
	contractForTool := func(name string) ToolExecutionContract {
		if name == "fast_status" {
			return ToolExecutionContract{Explicit: true, Deterministic: true, SupportsDirect: true}
		}
		return ToolExecutionContract{}
	}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(IMUserMessage{Text: "summarize and search Go docs"}, false, false, semantic, contractForTool)
	if profile.IsDirect() || profile.Reason != "semantic capability-managed intent" {
		t.Fatalf("profile=%+v, want non-direct capability-managed route", profile)
	}
}

func TestManagedMixedCapabilityIntentStaysFullUntilCoverageExists(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{semanticUnmigratedFixtureLabel(t)}, Confidence: 0.97,
		ToolNames: []string{"web_search", "send_file"}, Layer: 3,
	}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(IMUserMessage{Text: "search and deliver"}, false, false, semantic, explicitInferredExecutionContractForTest)
	if profile.IsLight() || profile.IsDirect() || profile.Reason != "semantic capability migration coverage incomplete" {
		t.Fatalf("profile=%+v, want full coverage-incomplete route", profile)
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

func TestHandlerClassifyIMExecutionProfileUsesCapabilityManagedCurrentTime(t *testing.T) {
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
	if profile.IsDirect() || profile.Reason != "semantic capability-managed intent" {
		t.Fatalf("profile = %+v, want capability-managed current-time route", profile)
	}
}

func TestHandlerClassifyIMExecutionProfileLocalCurrentTimeUsesUICCapabilityRoute(t *testing.T) {
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
			return `{"top":[{"skill":"current_time","score":0.98}]}`, nil
		}}),
	}
	profile := h.classifyIMExecutionProfile(IMUserMessage{Text: "\u73b0\u5728\u51e0\u70b9\uff1f"}, false, false)
	if profile.IsDirect() || profile.Reason != "semantic capability-managed intent" {
		t.Fatalf("profile = %+v, want capability-managed current-time route", profile)
	}
}

func TestExecutionProfileStoresAuthoritativeSemanticResultForMaterialization(t *testing.T) {
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
	if calls != 1 || semantic.Primary != intent.LabelLiveData || semantic.Layer != 3 {
		t.Fatalf("UIC result calls=%d semantic=%+v, want authoritative tree classification", calls, semantic)
	}
}

func TestSemanticChannelScopeCanonicalizesLocalRuntimePlatforms(t *testing.T) {
	for input, want := range map[string]string{
		"lansenger_local": "lansenger",
		"weixin_local":    "weixin",
		"telegram_local":  "telegram",
		"qqbot_local":     "qqbot",
		"lansenger":       "lansenger",
	} {
		if got := semanticChannelScope(input); got != want {
			t.Fatalf("semanticChannelScope(%q) = %q, want %q", input, got, want)
		}
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

func TestPrepareAgentLoopToolsLightDoesNotExposeLegacyManageSkillGateway(t *testing.T) {
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
	names := map[string]bool{}
	for _, def := range toolSet.Tools {
		if _, ok := def["x_execution_contract"]; ok {
			t.Fatalf("LLM tool leaked execution contract: %#v", def)
		}
		names[extractToolName(def)] = true
	}
	if names["manage_skill"] {
		t.Fatalf("legacy tool surface exposed dynamic manage_skill gateway: %s", executionProfileToolNames(toolSet.Tools))
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
	if len(prompt) > 2800 {
		t.Fatalf("light prompt len = %d, want <= 2800", len(prompt))
	}
	for _, blocked := range []string{"Group Discussion", "CodingSubAgent", "compress_context"} {
		if containsText(prompt, blocked) {
			t.Fatalf("light prompt should not contain full-agent section %q: %s", blocked, prompt)
		}
	}
	if containsText(prompt, "web_search / web_fetch") {
		t.Fatalf("light prompt must not instruct unavailable web_fetch: %s", prompt)
	}
	if !containsText(prompt, "Do not ask the user to re-authorize tools") {
		t.Fatalf("light prompt missing re-authorize fence: %s", prompt)
	}
	if !containsText(prompt, "one-time grants") || !containsText(prompt, "web_search") {
		t.Fatalf("light prompt missing one-time grant fence: %s", prompt)
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
