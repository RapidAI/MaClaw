package main

// RoutingRules holds all configurable routing parameters.
type RoutingRules struct {
	WorkTypeKeywords  map[string][]string // work_type → keyword list
	WorkTypeTier      map[string]string   // work_type → cost_tier
	RoleProviderBoost map[string][]string // role_code → preferred provider IDs
	DefaultWorkType   string              // fallback work type (default: "simple_qa")
	DefaultCostTier   string              // fallback cost tier (default: "medium")
}

// DefaultRoutingRules returns the built-in default rules.
func DefaultRoutingRules() RoutingRules {
	return RoutingRules{
		WorkTypeKeywords: map[string][]string{
			WorkTypeDocumentWriting:  {"公文", "通知", "纪要", "日报", "周报", "报告", "文档", "起草", "撰写", "正式"},
			WorkTypeDataAnalysis:     {"分析", "数据", "统计", "趋势", "对比", "指标", "报表", "归因"},
			WorkTypeQualityReport:    {"质量", "异常", "整改", "不良", "缺陷", "质检", "品控", "根因"},
			WorkTypeProductionReport: {"生产", "产量", "排产", "工单", "产线", "良率"},
			WorkTypeTableFormatting:  {"表格", "格式化", "排版", "整理", "列表"},
			WorkTypeLongTextSummary:  {"总结", "摘要", "概括", "提炼", "要点"},
			WorkTypeSimpleQA:         {},
		},
		WorkTypeTier: map[string]string{
			WorkTypeDocumentWriting:  CostTierHigh,
			WorkTypeDataAnalysis:     CostTierHigh,
			WorkTypeQualityReport:    CostTierHigh,
			WorkTypeProductionReport: CostTierMedium,
			WorkTypeTableFormatting:  CostTierMedium,
			WorkTypeLongTextSummary:  CostTierMedium,
			WorkTypeSimpleQA:         CostTierLow,
		},
		RoleProviderBoost: map[string][]string{},
		DefaultWorkType:   WorkTypeSimpleQA,
		DefaultCostTier:   CostTierMedium,
	}
}

// MergeWithDefaults fills in any missing fields from defaults.
// For map fields, missing keys are filled from defaults (partial override supported).
func (r RoutingRules) MergeWithDefaults() RoutingRules {
	defaults := DefaultRoutingRules()

	if r.WorkTypeKeywords == nil {
		r.WorkTypeKeywords = defaults.WorkTypeKeywords
	} else {
		for k, v := range defaults.WorkTypeKeywords {
			if _, exists := r.WorkTypeKeywords[k]; !exists {
				r.WorkTypeKeywords[k] = v
			}
		}
	}
	if r.WorkTypeTier == nil {
		r.WorkTypeTier = defaults.WorkTypeTier
	} else {
		for k, v := range defaults.WorkTypeTier {
			if _, exists := r.WorkTypeTier[k]; !exists {
				r.WorkTypeTier[k] = v
			}
		}
	}
	if r.RoleProviderBoost == nil {
		r.RoleProviderBoost = defaults.RoleProviderBoost
	}
	if r.DefaultWorkType == "" {
		r.DefaultWorkType = defaults.DefaultWorkType
	}
	if r.DefaultCostTier == "" {
		r.DefaultCostTier = defaults.DefaultCostTier
	}
	return r
}

// LookupTier returns the Cost Tier for a given Work Type.
// If the work type has no explicit mapping, returns "medium" as fallback.
func (r RoutingRules) LookupTier(workType string) string {
	if tier, ok := r.WorkTypeTier[workType]; ok {
		return tier
	}
	return CostTierMedium
}
