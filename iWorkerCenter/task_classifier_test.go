package main

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Classify tests
// ---------------------------------------------------------------------------

func TestClassify_ExplicitTaskTypeMatch(t *testing.T) {
	rules := DefaultRoutingRules()
	input := ClassifyInput{TaskType: "公文", MessageContent: "hello"}
	result := Classify(input, rules)
	if result.WorkType != WorkTypeDocumentWriting {
		t.Fatalf("WorkType = %q, want %q", result.WorkType, WorkTypeDocumentWriting)
	}
	if result.Method != "task_type_match" {
		t.Fatalf("Method = %q, want task_type_match", result.Method)
	}
	if result.CostTier != CostTierHigh {
		t.Fatalf("CostTier = %q, want high", result.CostTier)
	}
}

func TestClassify_KeywordMatch(t *testing.T) {
	rules := DefaultRoutingRules()
	input := ClassifyInput{
		TaskType:       "",
		MessageContent: "请帮我分析一下这个数据的统计趋势",
	}
	result := Classify(input, rules)
	if result.WorkType != WorkTypeDataAnalysis {
		t.Fatalf("WorkType = %q, want %q", result.WorkType, WorkTypeDataAnalysis)
	}
	if result.Method != "keyword_match" {
		t.Fatalf("Method = %q, want keyword_match", result.Method)
	}
	if result.CostTier != CostTierHigh {
		t.Fatalf("CostTier = %q, want high", result.CostTier)
	}
}

func TestClassify_DefaultFallback(t *testing.T) {
	rules := DefaultRoutingRules()
	input := ClassifyInput{
		TaskType:       "",
		MessageContent: "what is the weather today",
	}
	result := Classify(input, rules)
	if result.WorkType != WorkTypeSimpleQA {
		t.Fatalf("WorkType = %q, want %q", result.WorkType, WorkTypeSimpleQA)
	}
	if result.Method != "default" {
		t.Fatalf("Method = %q, want default", result.Method)
	}
	if result.CostTier != CostTierLow {
		t.Fatalf("CostTier = %q, want low", result.CostTier)
	}
}

func TestClassify_FreeInputFallsToKeywords(t *testing.T) {
	rules := DefaultRoutingRules()
	input := ClassifyInput{
		TaskType:       "自由输入",
		MessageContent: "帮我总结一下这篇文章的要点",
	}
	result := Classify(input, rules)
	if result.WorkType != WorkTypeLongTextSummary {
		t.Fatalf("WorkType = %q, want %q", result.WorkType, WorkTypeLongTextSummary)
	}
	if result.Method != "keyword_match" {
		t.Fatalf("Method = %q, want keyword_match", result.Method)
	}
}

func TestClassify_EmptyInput(t *testing.T) {
	rules := DefaultRoutingRules()
	input := ClassifyInput{}
	result := Classify(input, rules)
	if result.WorkType != WorkTypeSimpleQA {
		t.Fatalf("WorkType = %q, want %q", result.WorkType, WorkTypeSimpleQA)
	}
	if result.Method != "default" {
		t.Fatalf("Method = %q, want default", result.Method)
	}
}

func TestClassify_LatencyIsRecorded(t *testing.T) {
	rules := DefaultRoutingRules()
	input := ClassifyInput{MessageContent: "hello"}
	result := Classify(input, rules)
	if result.Latency < 0 {
		t.Fatalf("Latency = %v, want >= 0", result.Latency)
	}
}

func TestClassify_QualityReportKeywords(t *testing.T) {
	rules := DefaultRoutingRules()
	input := ClassifyInput{
		MessageContent: "这批产品有缺陷，需要质检整改",
	}
	result := Classify(input, rules)
	if result.WorkType != WorkTypeQualityReport {
		t.Fatalf("WorkType = %q, want %q", result.WorkType, WorkTypeQualityReport)
	}
	if result.CostTier != CostTierHigh {
		t.Fatalf("CostTier = %q, want high", result.CostTier)
	}
}

func TestClassify_ProductionReportKeywords(t *testing.T) {
	rules := DefaultRoutingRules()
	input := ClassifyInput{
		MessageContent: "今天产线的产量和良率如何",
	}
	result := Classify(input, rules)
	if result.WorkType != WorkTypeProductionReport {
		t.Fatalf("WorkType = %q, want %q", result.WorkType, WorkTypeProductionReport)
	}
	if result.CostTier != CostTierMedium {
		t.Fatalf("CostTier = %q, want medium", result.CostTier)
	}
}

func TestClassify_TableFormattingKeywords(t *testing.T) {
	rules := DefaultRoutingRules()
	input := ClassifyInput{
		MessageContent: "请把这些数据整理成表格列表",
	}
	result := Classify(input, rules)
	if result.WorkType != WorkTypeTableFormatting {
		t.Fatalf("WorkType = %q, want %q", result.WorkType, WorkTypeTableFormatting)
	}
}

// ---------------------------------------------------------------------------
// classifyByTaskType tests
// ---------------------------------------------------------------------------

func TestClassifyByTaskType_MatchFound(t *testing.T) {
	rules := DefaultRoutingRules()
	wt, ok := classifyByTaskType("纪要", rules.WorkTypeKeywords)
	if !ok {
		t.Fatal("expected match")
	}
	if wt != WorkTypeDocumentWriting {
		t.Fatalf("WorkType = %q, want %q", wt, WorkTypeDocumentWriting)
	}
}

func TestClassifyByTaskType_NoMatch(t *testing.T) {
	rules := DefaultRoutingRules()
	_, ok := classifyByTaskType("random_stuff", rules.WorkTypeKeywords)
	if ok {
		t.Fatal("expected no match")
	}
}

// ---------------------------------------------------------------------------
// classifyByKeywords tests
// ---------------------------------------------------------------------------

func TestClassifyByKeywords_BestMatchWins(t *testing.T) {
	rules := DefaultRoutingRules()
	// "分析数据统计" has 3 hits for data_analysis
	wt := classifyByKeywords("分析数据统计", rules.WorkTypeKeywords)
	if wt != WorkTypeDataAnalysis {
		t.Fatalf("WorkType = %q, want %q", wt, WorkTypeDataAnalysis)
	}
}

func TestClassifyByKeywords_NoMatch(t *testing.T) {
	rules := DefaultRoutingRules()
	wt := classifyByKeywords("hello world", rules.WorkTypeKeywords)
	if wt != "" {
		t.Fatalf("WorkType = %q, want empty", wt)
	}
}

// ---------------------------------------------------------------------------
// FormatTaskRouteLog tests
// ---------------------------------------------------------------------------

func TestFormatTaskRouteLog_ContainsAllFields(t *testing.T) {
	result := ClassificationResult{
		WorkType: WorkTypeDocumentWriting,
		CostTier: CostTierHigh,
		Latency:  2 * time.Millisecond,
		Method:   "keyword_match",
	}
	log := FormatTaskRouteLog(result, "req-123", "office-openai", "请帮我起草通知")

	for _, field := range []string{
		"[TaskRoute]",
		"ts=",
		"req_id=req-123",
		"work_type=document_writing",
		"cost_tier=high",
		"provider=office-openai",
		"latency_ms=",
		"method=keyword_match",
		`summary="请帮我起草通知"`,
	} {
		if !strings.Contains(log, field) {
			t.Errorf("log missing %q: %s", field, log)
		}
	}
}

func TestFormatTaskRouteLog_TruncatesSummary(t *testing.T) {
	longSummary := strings.Repeat("测", 300)
	result := ClassificationResult{
		WorkType: WorkTypeSimpleQA,
		CostTier: CostTierLow,
		Latency:  1 * time.Millisecond,
		Method:   "default",
	}
	log := FormatTaskRouteLog(result, "req-456", "none", longSummary)

	// The summary in the log should be truncated to 200 runes
	idx := strings.Index(log, `summary="`)
	if idx < 0 {
		t.Fatal("summary field not found")
	}
	summaryPart := log[idx+len(`summary="`):]
	endIdx := strings.LastIndex(summaryPart, `"`)
	if endIdx < 0 {
		t.Fatal("closing quote not found")
	}
	summaryContent := summaryPart[:endIdx]
	runes := []rune(summaryContent)
	if len(runes) != 200 {
		t.Fatalf("summary rune count = %d, want 200", len(runes))
	}
}

// ---------------------------------------------------------------------------
// DefaultRoutingRules tests
// ---------------------------------------------------------------------------

func TestDefaultRoutingRules_AllWorkTypesPresent(t *testing.T) {
	rules := DefaultRoutingRules()
	expectedTypes := []string{
		WorkTypeDocumentWriting,
		WorkTypeDataAnalysis,
		WorkTypeQualityReport,
		WorkTypeProductionReport,
		WorkTypeTableFormatting,
		WorkTypeLongTextSummary,
		WorkTypeSimpleQA,
	}
	for _, wt := range expectedTypes {
		if _, ok := rules.WorkTypeKeywords[wt]; !ok {
			t.Errorf("WorkTypeKeywords missing %q", wt)
		}
		if _, ok := rules.WorkTypeTier[wt]; !ok {
			t.Errorf("WorkTypeTier missing %q", wt)
		}
	}
	if len(rules.WorkTypeKeywords) != 7 {
		t.Fatalf("WorkTypeKeywords count = %d, want 7", len(rules.WorkTypeKeywords))
	}
}

func TestDefaultRoutingRules_TierMappings(t *testing.T) {
	rules := DefaultRoutingRules()
	cases := map[string]string{
		WorkTypeDocumentWriting:  CostTierHigh,
		WorkTypeDataAnalysis:     CostTierHigh,
		WorkTypeQualityReport:    CostTierHigh,
		WorkTypeProductionReport: CostTierMedium,
		WorkTypeTableFormatting:  CostTierMedium,
		WorkTypeLongTextSummary:  CostTierMedium,
		WorkTypeSimpleQA:         CostTierLow,
	}
	for wt, expectedTier := range cases {
		if got := rules.WorkTypeTier[wt]; got != expectedTier {
			t.Errorf("WorkTypeTier[%q] = %q, want %q", wt, got, expectedTier)
		}
	}
}

// ---------------------------------------------------------------------------
// MergeWithDefaults tests
// ---------------------------------------------------------------------------

func TestMergeWithDefaults_FillsEmptyRules(t *testing.T) {
	empty := RoutingRules{}
	merged := empty.MergeWithDefaults()
	if len(merged.WorkTypeKeywords) != 7 {
		t.Fatalf("WorkTypeKeywords count = %d, want 7", len(merged.WorkTypeKeywords))
	}
	if len(merged.WorkTypeTier) != 7 {
		t.Fatalf("WorkTypeTier count = %d, want 7", len(merged.WorkTypeTier))
	}
	if merged.DefaultWorkType != WorkTypeSimpleQA {
		t.Fatalf("DefaultWorkType = %q, want %q", merged.DefaultWorkType, WorkTypeSimpleQA)
	}
	if merged.DefaultCostTier != CostTierMedium {
		t.Fatalf("DefaultCostTier = %q, want %q", merged.DefaultCostTier, CostTierMedium)
	}
}

func TestMergeWithDefaults_PreservesCustomRules(t *testing.T) {
	custom := RoutingRules{
		WorkTypeKeywords: map[string][]string{
			"custom_type": {"自定义"},
		},
		WorkTypeTier: map[string]string{
			"custom_type": "low",
		},
		DefaultWorkType: "custom_type",
	}
	merged := custom.MergeWithDefaults()
	// Custom values should be preserved
	if _, ok := merged.WorkTypeKeywords["custom_type"]; !ok {
		t.Fatal("custom WorkTypeKeywords lost after merge")
	}
	if merged.DefaultWorkType != "custom_type" {
		t.Fatalf("DefaultWorkType = %q, want custom_type", merged.DefaultWorkType)
	}
	// DefaultCostTier should be filled from defaults
	if merged.DefaultCostTier != CostTierMedium {
		t.Fatalf("DefaultCostTier = %q, want medium", merged.DefaultCostTier)
	}
}

// ---------------------------------------------------------------------------
// LookupTier tests
// ---------------------------------------------------------------------------

func TestLookupTier_KnownType(t *testing.T) {
	rules := DefaultRoutingRules()
	if tier := rules.LookupTier(WorkTypeDocumentWriting); tier != CostTierHigh {
		t.Fatalf("LookupTier(%q) = %q, want high", WorkTypeDocumentWriting, tier)
	}
}

func TestLookupTier_UnknownTypeFallsToMedium(t *testing.T) {
	rules := DefaultRoutingRules()
	if tier := rules.LookupTier("nonexistent_type"); tier != CostTierMedium {
		t.Fatalf("LookupTier(nonexistent) = %q, want medium", tier)
	}
}

// ---------------------------------------------------------------------------
// rankProvidersWithTier tests
// ---------------------------------------------------------------------------

func TestRankProvidersWithTier_FiltersByTier(t *testing.T) {
	setCenterTestHome(t)
	server := newCenterServer(":0")
	// Default providers: office-openai (high), analysis-anthropic (high)
	req := openAIChatRequest{
		Messages: []openAIChatMessage{{Role: "user", Content: "test"}},
	}
	providers := server.rankProvidersWithTier(req, "high", nil, "")
	if len(providers) != 2 {
		t.Fatalf("providers count = %d, want 2", len(providers))
	}
	for _, p := range providers {
		if p.CostTier != "high" {
			t.Fatalf("provider %q has CostTier %q, want high", p.ID, p.CostTier)
		}
	}
}

func TestRankProvidersWithTier_FallbackWhenNoTierMatch(t *testing.T) {
	setCenterTestHome(t)
	server := newCenterServer(":0")
	req := openAIChatRequest{
		Messages: []openAIChatMessage{{Role: "user", Content: "test"}},
	}
	// No provider has "low" tier in defaults
	providers := server.rankProvidersWithTier(req, "low", nil, "")
	if len(providers) == 0 {
		t.Fatal("expected fallback providers, got none")
	}
	// Should fall back to rankProviders which returns all enabled
	if len(providers) != 2 {
		t.Fatalf("fallback providers count = %d, want 2", len(providers))
	}
}

func TestRankProvidersWithTier_ExplicitModelBypass(t *testing.T) {
	setCenterTestHome(t)
	server := newCenterServer(":0")
	req := openAIChatRequest{
		Model:    "analysis-anthropic",
		Messages: []openAIChatMessage{{Role: "user", Content: "test"}},
	}
	providers := server.rankProvidersWithTier(req, "low", nil, "")
	if len(providers) != 1 {
		t.Fatalf("providers count = %d, want 1", len(providers))
	}
	if providers[0].ID != "analysis-anthropic" {
		t.Fatalf("provider ID = %q, want analysis-anthropic", providers[0].ID)
	}
}

func TestRankProvidersWithTier_RoleBoost(t *testing.T) {
	setCenterTestHome(t)
	server := &centerServer{
		addr: ":0",
		providers: []CenterProvider{
			{ID: "a", Enabled: true, CostTier: "high", Priority: 50},
			{ID: "b", Enabled: true, CostTier: "high", Priority: 50},
		},
		client: nil,
	}
	roleBoost := map[string][]string{
		"office": {"b"},
	}
	req := openAIChatRequest{
		Messages: []openAIChatMessage{{Role: "user", Content: "test"}},
	}
	providers := server.rankProvidersWithTier(req, "high", roleBoost, "office")
	if len(providers) != 2 {
		t.Fatalf("providers count = %d, want 2", len(providers))
	}
	// "b" should be ranked first due to role boost
	if providers[0].ID != "b" {
		t.Fatalf("first provider = %q, want b (role-boosted)", providers[0].ID)
	}
}

// ---------------------------------------------------------------------------
// Provider CostTier normalization tests
// ---------------------------------------------------------------------------

func TestNormalizeCenterProviders_DefaultsCostTierToMedium(t *testing.T) {
	settings := centerSettingsFile{
		Providers: []centerProviderFile{{
			ID:       "test-provider",
			Name:     "Test",
			Protocol: "openai",
			BaseURL:  "http://localhost:8080",
			Model:    "gpt-test",
			Enabled:  true,
			CostTier: "", // empty should default to medium
		}},
	}
	providers := normalizeCenterProviders(settings)
	if len(providers) != 1 {
		t.Fatalf("providers count = %d, want 1", len(providers))
	}
	if providers[0].CostTier != "medium" {
		t.Fatalf("CostTier = %q, want medium", providers[0].CostTier)
	}
}

func TestDefaultCenterProviders_HaveCostTier(t *testing.T) {
	providers := defaultCenterProviders()
	for _, p := range providers {
		if p.CostTier == "" {
			t.Fatalf("provider %q has empty CostTier", p.ID)
		}
	}
	if providers[0].CostTier != "high" {
		t.Fatalf("office-openai CostTier = %q, want high", providers[0].CostTier)
	}
	if providers[1].CostTier != "high" {
		t.Fatalf("analysis-anthropic CostTier = %q, want high", providers[1].CostTier)
	}
}
