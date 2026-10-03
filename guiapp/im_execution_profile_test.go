package guiapp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
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

func TestClassifyIMExecutionProfileLiveVisualIsBounded(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelLiveDataVisual,
		Confidence: 0.93,
		Layer:      3,
		Reason:     "tree-after-embedding+synthesized composite: live_data(0.930)+live_data_visual(0.723)",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "崇州天气"}, false, false, semantic)
	if !profile.IsLight() || profile.Reason != "semantic capability-managed live visual" || profile.IterationBudget != 4 || profile.ToolBudget != 0 {
		t.Fatalf("weather card profile = %+v, want a bounded live-visual turn", profile)
	}
	composite := &intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelLiveDataVisual},
		Confidence: 0.92,
	}
	compositeProfile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "北京天气"}, false, false, composite)
	if !compositeProfile.IsLight() || compositeProfile.IterationBudget != 4 {
		t.Fatalf("lookup+visual profile = %+v, want the same bound", compositeProfile)
	}
}

func TestAskUserContinuationKeepsConfidentVisualBudget(t *testing.T) {
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelLiveDataVisual},
		Confidence: 0.89,
		Layer:      2,
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "画出近一月股价趋势图"}, false, true, semantic)
	if !profile.IsLight() || profile.IterationBudget != 4 || !strings.Contains(profile.Reason, "ask_user continuation") {
		t.Fatalf("profile=%+v, want the visual budget kept on an ask_user continuation", profile)
	}
	weak := &intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}
	full := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "好的"}, false, true, weak)
	if full.IsLight() || full.Reason != "ask_user continuation" {
		t.Fatalf("weak answer profile=%+v, want the unbounded continuation", full)
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

func TestClassifyIMExecutionProfileTreeSynthesizedLookupPDFCompositeStaysManaged(t *testing.T) {
	// 2026-08-25 production shape for "全网搜索张惠妹歌曲列表，生成详细pdf版本清单":
	// the tree answered web_fetch 0.95 and the classifier synthesized the
	// declared lookup+generate composite.  The verdict must keep the tree's
	// score so the turn stays capability-managed; when the composite carried
	// the weaker half's 0.68 it fell under both floors and shipped without
	// generate_pdf ("当前工具列表中没有PDF生成权限").
	semantic := &intent.ClassificationResult{
		Primary:    intent.LabelWebFetch,
		Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: 0.95,
		Layer:      3,
		Reason:     "tree-after-embedding+synthesized composite: web_fetch(0.950)+document_generate(0.683)",
	}
	profile := classifyIMExecutionProfileWithSemantic(
		IMUserMessage{Text: "全网搜索张惠妹歌曲列表，生成详细pdf版本清单"}, false, false, semantic,
	)
	if profile.IsLight() || profile.Reason != "semantic capability-managed mutating intent" {
		t.Fatalf("profile = %+v, want full governed lookup+document-generation chain", profile)
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

func TestClassifyIMExecutionProfileSuppliedSemanticIgnoresClockWording(t *testing.T) {
	semantic := &intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.4, Reason: "unmanaged"}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(IMUserMessage{Text: "现在几点？"}, false, false, semantic, explicitInferredExecutionContractForTest)
	if profile.IsDirect() || profile.Reason == "local deterministic current time intent" {
		t.Fatalf("profile = %+v, clock wording must not override a supplied classification", profile)
	}
}

func TestClassifyIMExecutionProfileLocalCurrentTimeFallbackUsesDirectTool(t *testing.T) {
	profile := classifyIMExecutionProfileWithSemanticAndContracts(IMUserMessage{Text: "\u73b0\u5728\u51e0\u70b9\uff1f"}, false, false, nil, explicitInferredExecutionContractForTest)
	if profile.IsDirect() || profile.DirectToolName == "current_datetime" || profile.Reason == "local deterministic current time intent" {
		t.Fatalf("profile = %+v, clock wording must not select a tool without a classification", profile)
	}
}

func TestClassifyIMExecutionProfileLocalCurrentTimeAllowsLongPoliteQuery(t *testing.T) {
	msg := IMUserMessage{Text: "\u9ebb\u70e6\u4f60\u770b\u4e00\u4e0b\u6211\u8fd9\u8fb9\u7684\u5f53\u524d\u65f6\u95f4\uff0c\u73b0\u5728\u51e0\u70b9\u4e86\uff1f\u987a\u4fbf\u544a\u8bc9\u6211\u4eca\u5929\u5468\u51e0\uff0c\u8c22\u8c22"}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(msg, false, false, nil, explicitInferredExecutionContractForTest)
	if profile.IsDirect() || profile.DirectToolName == "current_datetime" {
		t.Fatalf("profile = %+v, a long clock sentence must not select a tool without a classification", profile)
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
	prompt := buildLightIMSystemPrompt(IMUserMessage{Text: "\u5927\u8fde\u5929\u6c14"}, profile, nil, "")
	// Light bundle includes the shared Chinese output-format fence (~1.5KB), a
	// short GUI capability fence, and the ~0.5KB governed-tool grant fence
	// (semanticGrantPromptFence). Keep a hard cap so full-agent sections
	// cannot creep in; measured size is ~2.9KB.
	if len(prompt) > 3200 {
		t.Fatalf("light prompt len = %d, want <= 3200", len(prompt))
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
	if !containsText(prompt, "the live tool list is the ground truth") || !containsText(prompt, "web_search") {
		t.Fatalf("light prompt missing governed-tools fence: %s", prompt)
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
	}, profile, nil, "")
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

func TestHydrateParentExecutionWaitsForTheScan(t *testing.T) {
	const userID = "desktop-user:restored-task"
	mem := agent.NewConversationMemory()
	mem.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "分析以下仓库"},
		{Role: "assistant", ToolCalls: []map[string]interface{}{
			{"function": map[string]interface{}{"name": "bash"}},
		}},
		{Role: "tool", ToolName: "bash", Content: "ok"},
	})
	h := &IMMessageHandler{memory: mem}
	var wg sync.WaitGroup
	misses := make(chan struct{}, 32)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !h.parentExecutionIsFull(userID) {
				misses <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(misses)
	if _, ok := <-misses; ok {
		t.Fatal("a concurrent hydrate observed an empty carry before the history scan finished")
	}
}

func TestHydrateParentExecutionReadsTypedToolCalls(t *testing.T) {
	const userID = "desktop-user:typed-calls"
	mem := agent.NewConversationMemory()
	mem.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "分析以下仓库"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "c1",
			Function: llm.ToolCallFunction{
				Name:      "bash",
				Arguments: strings.Repeat("echo ", 2000),
			},
		}}},
		{Role: "tool", ToolCallID: "c1", Content: "ok"},
	})
	h := &IMMessageHandler{memory: mem}
	light := ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light", TaskType: "general"}
	got := h.continuationKeepsParentExecution(light, userID, "嵌入模型用在哪些场景", nil)
	if got.IsLight() || got.Reason != shortContinuationReason {
		t.Fatalf("typed tool calls must restore the parent surface, got %+v", got)
	}
	if tools := h.parentExecutionTools(userID); len(tools) != 1 || tools[0] != "bash" {
		t.Fatalf("hydrated carry = %v, want [bash]", tools)
	}
}

func TestRestartHydratesParentExecutionFromHistory(t *testing.T) {
	const userID = "desktop-user:restored-task"
	mem := agent.NewConversationMemory()
	mem.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "分析以下仓库"},
		{Role: "assistant", Content: "reading", ToolCalls: []map[string]interface{}{
			{"id": "c1", "function": map[string]interface{}{"name": "bash"}},
		}},
		{Role: "tool", ToolName: "bash", Content: "ok"},
		{Role: "assistant", Content: "对比结论"},
		{Role: "user", Content: "再说一下结论"},
		{Role: "assistant", Content: "结论还是那个"},
	})
	h := &IMMessageHandler{memory: mem}
	light := ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light", TaskType: "general"}
	got := h.continuationKeepsParentExecution(light, userID, "嵌入模型用在哪些场景", nil)
	if got.IsLight() || got.Reason != shortContinuationReason {
		t.Fatalf("a short continuation after restart must keep the restored full surface, got %+v", got)
	}
	if tools := h.parentExecutionTools(userID); len(tools) != 1 || tools[0] != "bash" {
		t.Fatalf("hydrated carry = %v, want [bash]", tools)
	}

	h.noteParentExecution(userID, false, nil)
	if again := h.continuationKeepsParentExecution(light, userID, "嵌入模型用在哪些场景", nil); !again.IsLight() {
		t.Fatalf("an in-process light clear must not rehydrate, got %+v", again)
	}

	lightOnly := agent.NewConversationMemory()
	lightOnly.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "查一下"},
		{Role: "tool", ToolName: "web_search", Content: "results"},
	})
	h2 := &IMMessageHandler{memory: lightOnly}
	if got := h2.continuationKeepsParentExecution(light, userID, "接着说", nil); !got.IsLight() {
		t.Fatalf("a restored light-only turn must stay light, got %+v", got)
	}
}

func TestProjectTaskSearchKeepsParentToolsAfterRestart(t *testing.T) {
	const userID = `desktop-user:C:\tasks\octop`
	store := filepath.Join(t.TempDir(), "conversation.json")
	mem := agent.NewPersistentConversationMemory(store)
	// History compression can drop tool-call names. The carry has to live
	// beside the transcript, not inside it.
	mem.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "分析以下仓库"},
		{Role: "assistant", Content: "对比结论"},
	})
	h := &IMMessageHandler{memory: mem}
	h.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
		{"function": map[string]interface{}{"name": "web_fetch"}},
	})
	mem.Stop()

	reloaded := agent.NewPersistentConversationMemory(store)
	defer reloaded.Stop()
	h2 := &IMMessageHandler{memory: reloaded}
	light := ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}
	search := &intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.96}
	taskSearch := h2.continuationKeepsParentExecution(light, userID, "octop中有sso登录功能吗？", search)
	if !taskSearch.IsLight() || !taskSearch.PromptIsLight() || taskSearch.Reason != lookupContinuationReason || taskSearch.ToolBudget != 0 {
		t.Fatalf("a task search after restart must stay a light lookup with an open tool budget, got %+v", taskSearch)
	}
	if taskSearch.IterationBudget != lookupContinuationIterationBudget {
		t.Fatalf("task search iteration budget = %d, want %d", taskSearch.IterationBudget, lookupContinuationIterationBudget)
	}
	loop := NewLoopContext("chat", 300, nil)
	loop.Runtime.Execution = taskSearch
	limits := computeAgentLoopIterationLimits(loop, 300, 0)
	if limits.EffectiveMax != lookupContinuationIterationBudget {
		t.Fatalf("task search loop cap = %d, want %d", limits.EffectiveMax, lookupContinuationIterationBudget)
	}
	if tools := h2.parentExecutionTools(userID); len(tools) != 1 || tools[0] != "bash" {
		t.Fatalf("durable carry = %v, want [bash]", tools)
	}
	longSearch := "请在 octop 仓库里查一下有没有 SSO 或者单点登录，认证入口在哪个目录，登录流程经过哪些服务"
	longProfile := h2.continuationKeepsParentExecution(light, userID, longSearch, search)
	if longProfile.Reason != lookupContinuationReason || !longProfile.IsLight() || longProfile.ToolBudget != 0 {
		t.Fatalf("a longer task search must stay a lookup continuation, got %+v", longProfile)
	}
	h2.recordSemanticExecutionSurface(userID, longProfile, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "web_search"}},
		{"function": map[string]interface{}{"name": "web_fetch"}},
	})
	if tools := h2.parentExecutionTools(userID); len(tools) != 1 || tools[0] != "bash" {
		t.Fatalf("a longer task search must keep the parent carry, got %v", tools)
	}
	if utf8.RuneCountInString(longSearch) <= 40 {
		t.Fatalf("fixture must be longer than the short-continuation gate, got %d", utf8.RuneCountInString(longSearch))
	}
	fetch := &intent.ClassificationResult{Primary: intent.LabelWebFetch, Confidence: 0.95}
	managedFull := fullExecutionProfile("semantic capability-managed intent")
	fetchProfile := h2.continuationKeepsParentExecution(managedFull, userID, "打开那个仓库", fetch)
	if !fetchProfile.IsLight() || !fetchProfile.PromptIsLight() || fetchProfile.Reason != lookupContinuationReason || fetchProfile.ToolBudget != 0 {
		t.Fatalf("a task page fetch must stay a light lookup, got %+v", fetchProfile)
	}
	h2.recordSemanticExecutionSurface(userID, fetchProfile, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "web_fetch"}},
	})
	if tools := h2.parentExecutionTools(userID); len(tools) != 1 || tools[0] != "bash" {
		t.Fatalf("a page fetch must not clear the parent carry, got %v", tools)
	}
	mutating := fullExecutionProfile("semantic capability-managed mutating intent")
	if got := h2.continuationKeepsParentExecution(mutating, userID, "打开那个仓库", fetch); got.Reason != mutating.Reason || got.IsLight() {
		t.Fatalf("a mutating full profile must not be narrowed, got %+v", got)
	}
	structural := fullExecutionProfile("structural execution signal")
	pathSearch := h2.continuationKeepsParentExecution(structural, userID, "认证在 internal/auth/sso.go 吗", search)
	if pathSearch.Reason != lookupContinuationReason || !pathSearch.IsLight() || pathSearch.ToolBudget != 0 {
		t.Fatalf("a path inside a task search must stay a lookup continuation, got %+v", pathSearch)
	}
	h2.recordSemanticExecutionSurface(userID, pathSearch, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "web_search"}},
		{"function": map[string]interface{}{"name": "web_fetch"}},
	})
	if tools := h2.parentExecutionTools(userID); len(tools) != 1 || tools[0] != "bash" {
		t.Fatalf("a path inside a task search must keep the parent carry, got %v", tools)
	}
	attached := fullExecutionProfile("attachments present")
	if got := h2.continuationKeepsParentExecution(attached, userID, "认证在 internal/auth/sso.go 吗", search); got.Reason != attached.Reason || got.IsLight() {
		t.Fatalf("an attachment turn must stay full, got %+v", got)
	}
	live := &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.98}
	if got := h2.continuationKeepsParentExecution(light, userID, "北京天气", live); !got.IsLight() {
		t.Fatalf("live data in a task must start clean, got %+v", got)
	}
	chat := &IMMessageHandler{}
	chat.noteParentExecution("desktop-user", true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
	})
	if got := chat.continuationKeepsParentExecution(light, "desktop-user", "搜一下", search); !got.IsLight() {
		t.Fatalf("a chat search must start clean, got %+v", got)
	}
	for _, owner := range []string{"desktop-user:acp:session-1", "desktop-user:expert:builtin-latex-paper"} {
		h2.noteParentExecution(owner, true, []map[string]interface{}{
			{"function": map[string]interface{}{"name": "bash"}},
		})
		if got := h2.continuationKeepsParentExecution(light, owner, "octop中有sso登录功能吗？", search); !got.IsLight() {
			t.Fatalf("%s search must start clean, got %+v", owner, got)
		}
	}
}

func TestContinuationSurfaceUnionsCarriedTools(t *testing.T) {
	h := &IMMessageHandler{}
	const userID = "desktop-user"
	h.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "ssh"}},
	})
	continued := fullExecutionProfile(shortContinuationReason)
	h.recordSemanticExecutionSurface(userID, continued, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
		{"function": map[string]interface{}{"name": "web_search"}},
	})
	got := h.parentExecutionTools(userID)
	if len(got) != 2 || got[0] != "ssh" || got[1] != "bash" {
		t.Fatalf("continuation carry = %v, want [ssh bash]", got)
	}
}

func TestSearchContinuationDoesNotGrowBaselineShell(t *testing.T) {
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.96}
	if semanticBaselineWorkspaceApplies(search) {
		t.Fatal("a search continuation stays a lookup; shell is not added just because the parent task had it")
	}
}

func TestShortContinuationKeepsParentExecutionTools(t *testing.T) {
	h := &IMMessageHandler{}
	light := ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light", TaskType: "general"}
	const userID = "desktop-user"
	if got := h.continuationKeepsParentExecution(light, userID, "用这个", nil); got.IsLight() != true {
		t.Fatalf("a short reply with no previous full turn must stay light, got %+v", got)
	}
	h.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "memory"}},
		{"function": map[string]interface{}{"name": "knowledge_search"}},
	})
	if h.parentExecutionIsFull(userID) {
		t.Fatal("a full turn that showed only lookup tools must not stick")
	}
	h.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
		{"function": map[string]interface{}{"name": "memory"}},
		{"function": map[string]interface{}{"name": "knowledge_search"}},
	})
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("only tools a light filter would drop are carried, got %v", got)
	}
	follow := h.continuationKeepsParentExecution(light, userID, "用这个", nil)
	if follow.IsLight() || follow.Reason != shortContinuationReason {
		t.Fatalf("a short continuation of a full turn must stay full, got %+v", follow)
	}
	live := &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.98}
	if got := h.continuationKeepsParentExecution(light, userID, "用这个", live); !got.IsLight() {
		t.Fatalf("a confident live-data turn must start clean, got %+v", got)
	}
	unsure := &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.4}
	if got := h.continuationKeepsParentExecution(light, userID, "用这个", unsure); got.Reason != shortContinuationReason {
		t.Fatalf("a low-confidence label must keep the parent surface, got %+v", got)
	}
	knowledge := &intent.ClassificationResult{Primary: intent.LabelKnowledgeRead, Confidence: 0.98}
	if got := h.continuationKeepsParentExecution(light, userID, "用这个", knowledge); got.Reason != shortContinuationReason {
		t.Fatalf("a knowledge read continues the parent task, got %+v", got)
	}
	if got := h.continuationKeepsParentExecution(light, userID, "谢谢", nil); got.IsLight() || got.Reason != shortContinuationReason {
		t.Fatalf("a short utterance with no classification keeps the parent surface, got %+v", got)
	}
	long := strings.Repeat("续", 41)
	if got := h.continuationKeepsParentExecution(light, userID, long, nil); !got.IsLight() {
		t.Fatalf("a long message is not a short continuation, got %+v", got)
	}
	full := fullExecutionProfile("already full")
	if got := h.continuationKeepsParentExecution(full, userID, "用这个", nil); got.Reason != full.Reason {
		t.Fatalf("a full profile must not be rewritten, got %+v", got)
	}
	h.noteParentExecution(userID, true, nil)
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("a failed plan must not clear the carried surface, got %v", got)
	}
	h.noteParentExecution(userID, false, nil)
	if h.parentExecutionIsFull(userID) {
		t.Fatal("a light turn must clear the carried surface")
	}
	if got := h.continuationKeepsParentExecution(light, userID, "用这个", nil); !got.IsLight() {
		t.Fatalf("after a light turn the next short reply starts clean, got %+v", got)
	}
}

func TestPreferRankedNamesKeepsRestoredToolsAhead(t *testing.T) {
	got := preferRankedNames([]string{"knowledge_search", "bash", "web_search"}, []string{"bash", "ssh"})
	want := []string{"bash", "ssh", "knowledge_search", "web_search"}
	if len(got) != len(want) {
		t.Fatalf("ranked names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranked names = %v, want %v", got, want)
		}
	}
}

func TestRankContinuationToolsKeepsRetrievalAheadOfRestore(t *testing.T) {
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "knowledge_search"}},
		{"function": map[string]interface{}{"name": "memory"}},
		{"function": map[string]interface{}{"name": "web_search"}},
	}
	got := rankContinuationTools([]string{"web_search", "bash"}, []string{"bash", "ssh"}, tools)
	want := []string{"memory", "knowledge_search", "bash", "ssh", "web_search"}
	if len(got) != len(want) {
		t.Fatalf("ranked names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranked names = %v, want %v", got, want)
		}
	}
}

func TestAmbientRecallDoesNotReplaceOpenExecutionSurface(t *testing.T) {
	h := &IMMessageHandler{}
	const userID = "desktop-user"
	full := fullExecutionProfile("semantic capability-managed mutating intent")
	h.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "ssh"}},
	})
	h.recordSemanticExecutionSurface(userID, full, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "memory_recall"}},
		{"function": map[string]interface{}{"name": "knowledge_search"}},
	})
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "ssh" {
		t.Fatalf("ambient recall replaced the open execution surface: %v", got)
	}
	onlyRecall := &IMMessageHandler{}
	onlyRecall.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "memory_recall"}},
	})
	if onlyRecall.parentExecutionIsFull(userID) {
		t.Fatal("memory_recall alone became the parent execution surface")
	}
}

func TestRestartRestoresExecutionSurfacePastAmbientCarry(t *testing.T) {
	const userID = "desktop-user:restored-shell"
	store := filepath.Join(t.TempDir(), "conversation.json")
	mem := agent.NewPersistentConversationMemory(store)
	mem.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "更新服务器上的配置"},
		{Role: "assistant", Content: "connecting", ToolCalls: []map[string]interface{}{
			{"id": "c1", "function": map[string]interface{}{"name": "ssh"}},
		}},
		{Role: "tool", ToolName: "ssh", ToolCallID: "c1", Content: "ok"},
		{Role: "user", Content: "已经解封"},
		{Role: "assistant", Content: "recall", ToolCalls: []map[string]interface{}{
			{"id": "c2", "function": map[string]interface{}{"name": "memory_recall"}},
		}},
		{Role: "tool", ToolName: "memory_recall", ToolCallID: "c2", Content: "notes"},
	})
	mem.SetSemanticSessionResidue(userID, agent.SemanticSessionResidue{
		Status:  string(semanticResidueOpen),
		Summary: "更新api2服务器上的omniroute，保存原始配置",
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:knowledge.read.local:20dd5f458c29", Capability: "knowledge.read.local", Required: true},
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: string(coretool.CapabilityShellExecuteRemoteHost), Required: true},
			{ID: "need:~ambient:memory.recall.agent", Capability: string(coretool.CapabilityMemoryRecallAgent), Required: true},
		},
		Remaining: map[string]int{
			"knowledge.read.local":                            1,
			string(coretool.CapabilityShellExecuteRemoteHost): 0,
			string(coretool.CapabilityMemoryRecallAgent):      1,
		},
	})
	mem.SetParentExecutionTools(userID, []string{"memory_recall"})
	mem.Stop()

	reloaded := agent.NewPersistentConversationMemory(store)
	defer reloaded.Stop()
	h := &IMMessageHandler{memory: reloaded}
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "ssh" {
		t.Fatalf("restart restored %v, want [ssh]", got)
	}
	again, known := reloaded.ParentExecutionTools(userID)
	if !known || len(again) != 1 || again[0] != "ssh" {
		t.Fatalf("durable carry = %v known=%v, want [ssh]", again, known)
	}

	cleared := agent.NewConversationMemory()
	defer cleared.Stop()
	cleared.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "更新服务器"},
		{Role: "tool", ToolName: "ssh", Content: "ok"},
		{Role: "user", Content: "查一下"},
		{Role: "tool", ToolName: "memory_recall", Content: "notes"},
	})
	cleared.SetParentExecutionTools(userID, []string{"memory_recall"})
	h2 := &IMMessageHandler{memory: cleared}
	if h2.parentExecutionIsFull(userID) {
		t.Fatal("ambient carry revived ssh after the obligation was gone")
	}
}

func TestRestartDoesNotSubstituteLocalShellForRemoteObligation(t *testing.T) {
	const userID = "desktop-user:remote-shell"
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	mem.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "更新服务器"},
		{Role: "tool", ToolName: "ssh", Content: "ok"},
		{Role: "user", Content: "看一下本地日志"},
		{Role: "tool", ToolName: "bash", Content: "ok"},
		{Role: "user", Content: "已经解封"},
		{Role: "tool", ToolName: "memory_recall", Content: "notes"},
	})
	mem.SetSemanticSessionResidue(userID, agent.SemanticSessionResidue{
		Status: string(semanticResidueOpen),
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: string(coretool.CapabilityShellExecuteRemoteHost), Required: true},
		},
		Remaining: map[string]int{string(coretool.CapabilityShellExecuteRemoteHost): 0},
	})
	mem.SetParentExecutionTools(userID, []string{"memory_recall"})
	h := &IMMessageHandler{memory: mem}
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "ssh" {
		t.Fatalf("restart restored %v, want [ssh]", got)
	}

	localOnly := agent.NewConversationMemory()
	defer localOnly.Stop()
	localOnly.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "看一下本地日志"},
		{Role: "tool", ToolName: "bash", Content: "ok"},
		{Role: "user", Content: "已经解封"},
		{Role: "tool", ToolName: "memory_recall", Content: "notes"},
	})
	localOnly.SetSemanticSessionResidue(userID, agent.SemanticSessionResidue{
		Status: string(semanticResidueOpen),
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: string(coretool.CapabilityShellExecuteRemoteHost), Required: true},
		},
		Remaining: map[string]int{string(coretool.CapabilityShellExecuteRemoteHost): 0},
	})
	localOnly.SetParentExecutionTools(userID, []string{"memory_recall"})
	h2 := &IMMessageHandler{memory: localOnly}
	if h2.parentExecutionIsFull(userID) {
		t.Fatal("a later bash became the open remote-shell surface")
	}
}

func TestLightSideQuestionKeepsOpenShellCarry(t *testing.T) {
	const userID = "desktop-user:open-shell"
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	mem.SetSemanticSessionResidue(userID, agent.SemanticSessionResidue{
		Status: string(semanticResidueOpen),
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: string(coretool.CapabilityShellExecuteRemoteHost), Required: true},
		},
		Remaining: map[string]int{string(coretool.CapabilityShellExecuteRemoteHost): 0},
	})
	mem.SetParentExecutionTools(userID, []string{"ssh"})
	h := &IMMessageHandler{memory: mem}
	light := ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light", Reason: "semantic capability-managed lookup"}
	h.recordSemanticExecutionSurface(userID, light, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "web_search"}},
	})
	raw, known := mem.ParentExecutionTools(userID)
	if !known || len(raw) != 1 || raw[0] != "ssh" {
		t.Fatalf("light side question cleared the open shell: known=%v tools=%v", known, raw)
	}

	ended := agent.NewConversationMemory()
	defer ended.Stop()
	ended.SetParentExecutionTools(userID, []string{"bash"})
	endedHandler := &IMMessageHandler{memory: ended}
	endedHandler.recordSemanticExecutionSurface(userID, light, nil)
	cleared, known := ended.ParentExecutionTools(userID)
	if !known || len(cleared) != 0 {
		t.Fatalf("a light turn with no open task must clear, known=%v tools=%v", known, cleared)
	}
}

func TestExplicitClearRestoresOpenShellFromHistory(t *testing.T) {
	const userID = "desktop-user:cleared-shell"
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	mem.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "更新服务器"},
		{Role: "tool", ToolName: "ssh", Content: "ok"},
		{Role: "user", Content: "现在几点"},
		{Role: "tool", ToolName: "web_search", Content: "ok"},
	})
	mem.SetSemanticSessionResidue(userID, agent.SemanticSessionResidue{
		Status: string(semanticResidueOpen),
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: string(coretool.CapabilityShellExecuteRemoteHost), Required: true},
		},
		Remaining: map[string]int{string(coretool.CapabilityShellExecuteRemoteHost): 0},
	})
	mem.SetParentExecutionTools(userID, []string{"ssh"})
	mem.ClearParentExecutionTools(userID)
	h := &IMMessageHandler{memory: mem}
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "ssh" {
		t.Fatalf("cleared carry restored %v, want [ssh]", got)
	}

	finished := agent.NewConversationMemory()
	defer finished.Stop()
	finished.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "更新服务器"},
		{Role: "tool", ToolName: "ssh", Content: "ok"},
	})
	finished.SetParentExecutionTools(userID, []string{"ssh"})
	finished.ClearParentExecutionTools(userID)
	h2 := &IMMessageHandler{memory: finished}
	if h2.parentExecutionIsFull(userID) {
		t.Fatal("a cleared carry revived ssh after the task was gone")
	}
}

func TestUnrecordedParentRestoresOpenShellPastAmbient(t *testing.T) {
	const userID = "desktop-user:unrecorded-shell"
	openShell := agent.SemanticSessionResidue{
		Status: string(semanticResidueOpen),
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: string(coretool.CapabilityShellExecuteRemoteHost), Required: true},
		},
		Remaining: map[string]int{string(coretool.CapabilityShellExecuteRemoteHost): 0},
	}
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	mem.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "更新服务器"},
		{Role: "tool", ToolName: "ssh", Content: "ok"},
		{Role: "user", Content: "已经解封"},
		{Role: "tool", ToolName: "memory_recall", Content: "notes"},
	})
	mem.SetSemanticSessionResidue(userID, openShell)
	h := &IMMessageHandler{memory: mem}
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "ssh" {
		t.Fatalf("unrecorded parent restored %v, want [ssh]", got)
	}
	raw, known := mem.ParentExecutionTools(userID)
	if !known || len(raw) != 1 || raw[0] != "ssh" {
		t.Fatalf("durable carry = %v known=%v, want [ssh]", raw, known)
	}

	finished := agent.NewConversationMemory()
	defer finished.Stop()
	finished.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "更新服务器"},
		{Role: "tool", ToolName: "ssh", Content: "ok"},
		{Role: "user", Content: "已经解封"},
		{Role: "tool", ToolName: "memory_recall", Content: "notes"},
	})
	h2 := &IMMessageHandler{memory: finished}
	if h2.parentExecutionIsFull(userID) {
		t.Fatal("an unrecorded parent revived ssh after the task was gone")
	}

	localOnly := agent.NewConversationMemory()
	defer localOnly.Stop()
	localOnly.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "看一下本地日志"},
		{Role: "tool", ToolName: "bash", Content: "ok"},
		{Role: "user", Content: "已经解封"},
		{Role: "tool", ToolName: "memory_recall", Content: "notes"},
	})
	localOnly.SetSemanticSessionResidue(userID, openShell)
	h3 := &IMMessageHandler{memory: localOnly}
	if h3.parentExecutionIsFull(userID) {
		t.Fatal("an unrecorded parent installed bash for the open remote shell")
	}
}

func TestCompanionRenderDoesNotReplaceOpenShell(t *testing.T) {
	const userID = "desktop-user:companion-render"
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	mem.SetSemanticSessionResidue(userID, agent.SemanticSessionResidue{
		Status: string(semanticResidueOpen),
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: string(coretool.CapabilityShellExecuteRemoteHost), Required: true},
		},
		Remaining: map[string]int{string(coretool.CapabilityShellExecuteRemoteHost): 0},
	})
	mem.SetParentExecutionTools(userID, []string{"ssh"})
	h := &IMMessageHandler{memory: mem}
	full := fullExecutionProfile("semantic capability-managed mutating intent")
	h.recordSemanticExecutionSurface(userID, full, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
		{"function": map[string]interface{}{"name": "write_file"}},
		{"function": map[string]interface{}{"name": "read_file"}},
		{"function": map[string]interface{}{"name": "knowledge_search"}},
	})
	raw, known := mem.ParentExecutionTools(userID)
	if !known || len(raw) != 1 || raw[0] != "ssh" {
		t.Fatalf("baseline companions replaced ssh: known=%v tools=%v", known, raw)
	}
	continued := fullExecutionProfile(shortContinuationReason)
	h.recordSemanticExecutionSurface(userID, continued, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
		{"function": map[string]interface{}{"name": "web_search"}},
	})
	raw, known = mem.ParentExecutionTools(userID)
	if !known || len(raw) != 1 || raw[0] != "ssh" {
		t.Fatalf("continuation unioned baseline bash onto ssh: known=%v tools=%v", known, raw)
	}

	h.recordSemanticExecutionSurfacePlan(userID, full, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "write_file"}},
	}, []coretool.CapabilityNeed{
		{ID: "need:fs.write.local:real", Capability: coretool.CapabilityFSWriteLocal, Required: true},
	})
	raw, known = mem.ParentExecutionTools(userID)
	if !known || len(raw) != 1 || raw[0] != "write_file" {
		t.Fatalf("a file-write plan kept the shell route: known=%v tools=%v", known, raw)
	}

	missed := agent.NewConversationMemory()
	defer missed.Stop()
	missed.SetSemanticSessionResidue(userID, agent.SemanticSessionResidue{
		Status: string(semanticResidueOpen),
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: string(coretool.CapabilityShellExecuteRemoteHost), Required: true},
		},
	})
	missed.SetParentExecutionTools(userID, []string{"ssh"})
	missedHandler := &IMMessageHandler{memory: missed}
	missedHandler.recordSemanticExecutionSurfacePlan(userID, full, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
	}, []coretool.CapabilityNeed{
		{ID: "need:fs.write.local:real", Capability: coretool.CapabilityFSWriteLocal, Required: true},
	})
	raw, known = missed.ParentExecutionTools(userID)
	if !known || len(raw) != 0 {
		t.Fatalf("a switched plan that did not render its tool kept ssh: known=%v tools=%v", known, raw)
	}
}

func TestDisjointCompanionCarryRestoresOpenShell(t *testing.T) {
	const userID = "desktop-user:disjoint-carry"
	openRemote := agent.SemanticSessionResidue{
		Status: string(semanticResidueOpen),
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: string(coretool.CapabilityShellExecuteRemoteHost), Required: true},
		},
		Remaining: map[string]int{string(coretool.CapabilityShellExecuteRemoteHost): 0},
	}
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	mem.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "更新服务器"},
		{Role: "tool", ToolName: "ssh", Content: "ok"},
		{Role: "user", Content: "已经解封"},
		{Role: "tool", ToolName: "bash", Content: "ok"},
		{Role: "tool", ToolName: "write_file", Content: "ok"},
	})
	mem.SetSemanticSessionResidue(userID, openRemote)
	mem.SetParentExecutionTools(userID, []string{"bash", "write_file"})
	h := &IMMessageHandler{memory: mem}
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "ssh" {
		t.Fatalf("disjoint carry restored %v, want [ssh]", got)
	}

	mixed := agent.NewConversationMemory()
	defer mixed.Stop()
	mixed.SetSemanticSessionResidue(userID, openRemote)
	mixed.SetParentExecutionTools(userID, []string{"ssh", "bash", "write_file"})
	h2 := &IMMessageHandler{memory: mixed}
	if got := h2.parentExecutionTools(userID); len(got) != 1 || got[0] != "ssh" {
		t.Fatalf("companion names stayed on the shell route: %v", got)
	}
	raw, known := mixed.ParentExecutionTools(userID)
	if !known || len(raw) != 1 || raw[0] != "ssh" {
		t.Fatalf("durable carry = %v known=%v, want [ssh]", raw, known)
	}

	local := agent.NewConversationMemory()
	defer local.Stop()
	local.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "更新服务器"},
		{Role: "tool", ToolName: "ssh", Content: "ok"},
		{Role: "user", Content: "看一下本地日志"},
		{Role: "tool", ToolName: "bash", Content: "ok"},
	})
	local.SetSemanticSessionResidue(userID, agent.SemanticSessionResidue{
		Status: string(semanticResidueOpen),
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:shell.execute.local:real", Capability: string(coretool.CapabilityShellExecuteLocal), Required: true},
		},
	})
	local.SetParentExecutionTools(userID, []string{"bash"})
	h3 := &IMMessageHandler{memory: local}
	if got := h3.parentExecutionTools(userID); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("a local-shell obligation restored %v, want [bash]", got)
	}

	miss := agent.NewConversationMemory()
	defer miss.Stop()
	miss.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "看一下本地日志"},
		{Role: "tool", ToolName: "bash", Content: "ok"},
	})
	miss.SetSemanticSessionResidue(userID, openRemote)
	miss.SetParentExecutionTools(userID, []string{"bash"})
	h4 := &IMMessageHandler{memory: miss}
	if h4.parentExecutionIsFull(userID) {
		t.Fatal("a disjoint bash carry became the open remote-shell surface")
	}
	raw, known = miss.ParentExecutionTools(userID)
	if !known || len(raw) != 0 {
		t.Fatalf("disjoint bash stayed recorded: known=%v tools=%v", known, raw)
	}
}

func TestSessionResetDropsParentExecutionCarry(t *testing.T) {
	const userID = "desktop-user:reset-carry"
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	mem.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "更新服务器"},
		{Role: "tool", ToolName: "ssh", Content: "ok"},
	})
	mem.SetSemanticSessionResidue(userID, agent.SemanticSessionResidue{
		Status: string(semanticResidueOpen),
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: string(coretool.CapabilityShellExecuteRemoteHost), Required: true},
		},
	})
	h := &IMMessageHandler{memory: mem}
	h.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "ssh"}},
	})
	h.clearPerUserSessionState(userID)
	if h.parentExecutionIsFull(userID) {
		t.Fatal("session reset kept the in-process ssh carry")
	}
	raw, known := mem.ParentExecutionTools(userID)
	if !known || len(raw) != 0 {
		t.Fatalf("session reset left the durable carry known=%v tools=%v", known, raw)
	}
	restarted := &IMMessageHandler{memory: mem}
	if restarted.parentExecutionIsFull(userID) {
		t.Fatal("restart restored ssh after the task was reset")
	}
}

func TestRecordSemanticExecutionSurface(t *testing.T) {
	h := &IMMessageHandler{}
	const userID = "desktop-user"
	h.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
	})
	light := ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}
	h.recordSemanticExecutionSurface(userID, light, nil)
	if h.parentExecutionIsFull(userID) {
		t.Fatal("a light managed turn must drop the previous execution tools")
	}
	h.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
	})
	full := fullExecutionProfile("semantic capability-managed mutating intent")
	h.recordSemanticExecutionSurface(userID, full, nil)
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("a failed full surface must keep the previous list, got %v", got)
	}
	h.recordSemanticExecutionSurface(userID, full, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "ssh"}},
		{"function": map[string]interface{}{"name": "memory"}},
	})
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "ssh" {
		t.Fatalf("a full managed turn must replace the carry with its own execution tools, got %v", got)
	}
	h.recordSemanticExecutionSurface(userID, full, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "web_fetch"}},
		{"function": map[string]interface{}{"name": "knowledge_search"}},
	})
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "ssh" {
		t.Fatalf("a full turn that only rendered lookups must keep the parent execution tools, got %v", got)
	}
	continued := fullExecutionProfile(shortContinuationReason)
	h.recordSemanticExecutionSurface(userID, continued, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "knowledge_search"}},
		{"function": map[string]interface{}{"name": "memory"}},
	})
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "ssh" {
		t.Fatalf("a lookup continuation must keep the parent execution tools, got %v", got)
	}
	h.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
	})
	lookup := ExecutionProfile{
		Layer:           string(executionLayerLight),
		PromptProfile:   "light",
		Reason:          lookupContinuationReason,
		ToolBudget:      0,
		IterationBudget: lookupContinuationIterationBudget,
	}
	h.recordSemanticExecutionSurface(userID, lookup, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "web_search"}},
		{"function": map[string]interface{}{"name": "web_fetch"}},
	})
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("a light task lookup must keep the parent execution tools, got %v", got)
	}
}

func TestManagedFullPrepareKeepsParentExecution(t *testing.T) {
	h := &IMMessageHandler{}
	const userID = "desktop-user"
	h.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
	})
	full := NewLoopContext("chat", 3, nil)
	full.Runtime.SemanticIntent = &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.98}
	full.Runtime.Execution = fullExecutionProfile("semantic capability-managed mutating intent")
	h.prepareAgentLoopTools(userID, "继续改", full, agentLoopPhase{})
	if got := h.parentExecutionTools(userID); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("a managed full rebuild must keep the parent execution tools, got %v", got)
	}
	light := NewLoopContext("chat", 3, nil)
	light.Runtime.SemanticIntent = &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.98}
	light.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}
	h.prepareAgentLoopTools(userID, "北京天气", light, agentLoopPhase{})
	if h.parentExecutionIsFull(userID) {
		t.Fatal("a managed light rebuild must drop the previous execution tools")
	}
}

func TestAnswerOnlyTurnKeepsCarriedExecutionTools(t *testing.T) {
	h := &IMMessageHandler{}
	h.noteParentExecution("desktop-user", true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
	})
	ctx := NewLoopContext("chat", 3, nil)
	ctx.semanticTurnAnswerOnly = true
	ctx.Runtime.Execution = fullExecutionProfile("managed")
	h.prepareAgentLoopTools("desktop-user", "用这个", ctx, agentLoopPhase{})
	if got := h.parentExecutionTools("desktop-user"); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("an answer-only turn must keep the parent execution tools, got %v", got)
	}
}

func TestLookupCarryOpensSiblingBudget(t *testing.T) {
	needs, added := ensureLookupContinuationCarriedNeeds(nil, []string{"bash", "web_search"}, 0.9)
	if lookupCarryNeedIDPrefix <= "need:lookup-continuation:" {
		t.Fatal("carried shell ids must sort after the fetch family")
	}
	if len(added) != 1 {
		t.Fatalf("carried capabilities = %v, want one non-light family", added)
	}
	if len(needs) != lookupContinuationIterationBudget {
		t.Fatalf("carry siblings = %d, want the lookup iteration budget %d", len(needs), lookupContinuationIterationBudget)
	}
	for _, need := range needs {
		if need.Required {
			t.Fatalf("carry sibling is required: %+v", need)
		}
		if !strings.HasPrefix(need.ID, lookupCarryNeedIDPrefix) {
			t.Fatalf("carry sibling id = %s", need.ID)
		}
	}
	stripped := withoutLookupCarryNeeds(needs)
	if len(stripped) != 0 {
		t.Fatalf("carry siblings survived stripping: %+v", stripped)
	}
}

func TestLookupContinuationCarriedGrant(t *testing.T) {
	lookup := ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light", Reason: lookupContinuationReason}
	if !lookupContinuationCarriedGrant(lookup, []string{"bash", "ssh"}, "bash") {
		t.Fatal("carried bash must stay authorized on a lookup")
	}
	if lookupContinuationCarriedGrant(lookup, []string{"bash"}, "write_file") {
		t.Fatal("a tool that was not carried must stay unauthorized")
	}
	light := ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light", Reason: "semantic capability-managed lookup"}
	if lookupContinuationCarriedGrant(light, []string{"bash"}, "bash") {
		t.Fatal("a cold lookup must not inherit a carry")
	}
	if !lookupContinuationRepeatAllowed(lookup, []string{"bash"}, "web_search") {
		t.Fatal("a lookup must be able to search again after the first call")
	}
	if !lookupContinuationRepeatAllowed(lookup, []string{"bash"}, "bash") {
		t.Fatal("a lookup must be able to run carried bash again")
	}
	if lookupContinuationRepeatAllowed(lookup, []string{"bash"}, "write_file") {
		t.Fatal("a lookup must not repeat a tool that was not carried")
	}
	if lookupContinuationRepeatAllowed(light, nil, "web_search") {
		t.Fatal("a cold search must not open the next sibling")
	}
	if lookupContinuationRepeatAllowed(lookup, []string{"bash"}, "download_file") {
		t.Fatal("a lookup must not open another download")
	}
	bashPrompt := lookupContinuationToolPrompt([]string{"bash", "web_search"}, intent.LabelSearch)
	if !strings.Contains(bashPrompt, "(bash)") || !strings.Contains(bashPrompt, "call web_fetch") || !strings.Contains(bashPrompt, "not to run shell") {
		t.Fatalf("lookup prompt = %q", bashPrompt)
	}
	if strings.Contains(bashPrompt, "web_search)") || strings.Contains(bashPrompt, "(web_search") {
		t.Fatal("lookup prompt must not authorize a light tool by name")
	}
	if strings.Contains(lookupContinuationToolPrompt(nil, intent.LabelSearch), "bash") {
		t.Fatal("lookup prompt must not name bash when nothing was carried")
	}
	if strings.Contains(lookupContinuationToolPrompt([]string{"bash"}, intent.LabelWebFetch), "After web_search") {
		t.Fatal("a page-fetch lookup must not send the model to web_search first")
	}
}
