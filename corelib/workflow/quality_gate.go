package workflow

import (
	"strings"
	"time"
)

// RunQualityGate checks the phase output against the phase's checklist items.
// It uses lightweight keyword matching — each checklist item is checked by
// looking for related terms in the output text. Returns nil if the phase
// has no checklist items.
func RunQualityGate(phase *PhaseTemplate, output string) *QualityGateResult {
	if phase == nil || len(phase.Checklist) == 0 || strings.TrimSpace(output) == "" {
		return nil
	}

	lower := strings.ToLower(output)
	items := make([]GateCheckItem, 0, len(phase.Checklist))
	passCount := 0

	for _, desc := range phase.Checklist {
		keywords := extractGateKeywords(desc)
		passed := matchGateKeywords(lower, keywords)
		if passed {
			passCount++
		}
		items = append(items, GateCheckItem{
			Description: desc,
			Passed:      passed,
		})
	}

	return &QualityGateResult{
		PhaseID:   phase.ID,
		Passed:    passCount == len(items),
		Items:     items,
		CheckedAt: time.Now(),
	}
}

// gateDomainTerms are meaningful noun phrases commonly found in workflow
// checklist descriptions. Used to extract searchable keywords from Chinese
// text that lacks word boundaries.
var gateDomainTerms = []string{
	// Requirements phase
	"功能需求", "非功能需求", "用户目标", "性能", "安全", "兼容性",
	"边界情况", "异常场景", "验收标准", "可量化", "可验证",
	// Design phase
	"架构设计", "架构", "技术选型", "模块划分", "接口设计", "接口",
	"数据结构", "耦合", "职责",
	// Task breakdown phase
	"任务", "粒度", "依赖关系", "优先级", "排序", "模块",
	// Implementation phase
	"代码", "错误处理", "注释", "文档",
	// Review phase
	"命名规范", "代码风格", "bug", "性能", "安全隐患",
	"优化建议",
	// Product design
	"用户画像", "痛点", "竞品分析", "竞品", "产品定位",
	"核心功能", "用户流程", "信息架构", "交互说明", "交互",
	"原型", "页面", "跳转逻辑", "上线计划",
	// Innovation
	"趋势分析", "趋势", "机会", "需求缺口", "竞争格局",
	"价值主张", "创意", "新颖性", "差异化",
	"技术可行性", "市场可行性", "资源", "可行性",
	"路线图", "里程碑", "风险", "行动计划", "成功指标",
	// Business plan
	"商业模式", "融资", "市场规模", "TAM", "SAM", "SOM",
	"客户细分", "市场进入", "定价策略", "渠道策略",
	"组织架构", "KPI", "风险管理",
	"收入", "成本结构", "盈亏平衡", "现金流", "财务",
	// Testing
	"测试", "测试范围", "测试类型", "测试用例", "测试数据",
	"覆盖率", "通过率", "缺陷", "复现步骤", "严重程度",
	// Research/academic
	"论文", "综述", "文献", "研究", "方法论", "数据来源",
	"引用", "参考文献", "摘要", "结论", "创新点",
	"检索", "关键词", "数据库", "排除标准",
	"PRISMA", "文献矩阵", "研究空白",
	// Research report
	"研报", "行业", "投资", "估值", "盈利", "增长",
	// Experiment design
	"实验", "假设", "变量", "对照组", "样本",
	// Grant proposal
	"基金", "申请", "研究计划", "预算", "进度",
	// Paper writing
	"学术", "论证", "数据分析", "图表",
	// Project proposal
	"立项", "项目", "目标", "范围", "交付物",
	// Event planning
	"活动", "策划", "场地", "日程", "预算", "宣传",
	// Competitive analysis
	"竞品", "对比", "优势", "劣势", "市场份额",
	// PPT/presentation
	"幻灯片", "PPT", "ppt", "演示", "大纲", "视觉",
}

// extractGateKeywords extracts meaningful keywords from a checklist
// description by matching against known domain terms.
func extractGateKeywords(desc string) []string {
	lower := strings.ToLower(desc)
	var keywords []string
	seen := make(map[string]bool)

	for _, term := range gateDomainTerms {
		termLower := strings.ToLower(term)
		if strings.Contains(lower, termLower) && !seen[termLower] {
			keywords = append(keywords, termLower)
			seen[termLower] = true
		}
	}
	return keywords
}

// matchGateKeywords returns true if at least half of the keywords appear
// in the output text. For single-keyword items, requires exact match.
func matchGateKeywords(lowerOutput string, keywords []string) bool {
	if len(keywords) == 0 {
		return true // no keywords to check → pass by default
	}

	matched := 0
	for _, kw := range keywords {
		if strings.Contains(lowerOutput, kw) {
			matched++
		}
	}

	// Require at least half of keywords to match
	threshold := (len(keywords) + 1) / 2
	return matched >= threshold
}
