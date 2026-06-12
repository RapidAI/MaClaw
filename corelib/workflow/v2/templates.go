package v2

import (
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

// PhaseTemplate defines a phase in a workflow template.
// PhaseExecMode declares how a phase should be executed.
type PhaseExecMode string

const (
	// ExecModeDefault: run as normal agent loop (LLM generates text or calls tools).
	ExecModeDefault PhaseExecMode = ""
	// ExecModeSubAgent: run via CodingSubAgent (task-by-task TDD execution).
	ExecModeSubAgent PhaseExecMode = "subagent"
	// ExecModeAutoFromPrev: auto-complete using the previous phase's output
	// (no separate execution needed — the prior phase already produced the result).
	ExecModeAutoFromPrev PhaseExecMode = "auto_from_prev"
)

type PhaseTemplate struct {
	ID           string
	Name         string
	NeedsConfirm bool
	ToolPolicy   ToolPolicy
	ExecMode     PhaseExecMode // how this phase is executed (default = agent loop)
}

// WorkflowTemplate defines a type of workflow.
type WorkflowTemplate struct {
	Type        string
	Name        string
	Description string
	Keywords    []string // retained for metadata and UI display; not used for routing
	Phases      []PhaseTemplate
}

// TemplateRegistry holds all registered workflow templates.
type TemplateRegistry struct {
	mu        sync.RWMutex
	templates map[string]*WorkflowTemplate
	bm25Index *bm25.Index
	bm25Dirty bool
	version   uint64
}

func NewTemplateRegistry() *TemplateRegistry {
	return &TemplateRegistry{templates: make(map[string]*WorkflowTemplate)}
}

func (r *TemplateRegistry) Register(t *WorkflowTemplate) {
	if r == nil || t == nil || strings.TrimSpace(t.Type) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.templates == nil {
		r.templates = make(map[string]*WorkflowTemplate)
	}
	r.templates[t.Type] = t
	r.bm25Dirty = true
	r.version++
}

func (r *TemplateRegistry) Get(workflowType string) *WorkflowTemplate {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.templates[workflowType]
}

// MatchByText returns the best advisory match using the template's structured
// definition. It intentionally ignores Keywords so routing does not become a
// keyword shortcut.
func (r *TemplateRegistry) MatchByText(text string) *WorkflowTemplate {
	ranked := r.RankedByText(text)
	if !hasStableTopTemplateScore(ranked) {
		return nil
	}
	return r.Get(ranked[0].Type)
}

// MatchByKeywords is kept for older call sites. Despite the historical name it
// no longer performs keyword matching.
func (r *TemplateRegistry) MatchByKeywords(text string) *WorkflowTemplate {
	return r.MatchByText(text)
}

type TemplateScore struct {
	Type  string
	Score float64
}

const stableTemplateScoreLeadRatio = 1.25

func (r *TemplateRegistry) RankedByText(text string) []TemplateScore {
	if r == nil {
		return nil
	}
	idx := r.currentBM25Index()
	if idx == nil {
		return nil
	}
	scores := idx.Score(text)
	ranked := make([]TemplateScore, 0, len(scores))
	for id, score := range scores {
		if score > 0 {
			ranked = append(ranked, TemplateScore{Type: id, Score: score})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].Type < ranked[j].Type
		}
		return ranked[i].Score > ranked[j].Score
	})
	return ranked
}

func (r *TemplateRegistry) currentBM25Index() *bm25.Index {
	for {
		r.mu.Lock()
		if r.bm25Index != nil && !r.bm25Dirty {
			idx := r.bm25Index
			r.mu.Unlock()
			return idx
		}

		version := r.version
		docs := make([]bm25.Doc, 0, len(r.templates))
		for _, tmpl := range r.templates {
			if tmpl == nil || strings.TrimSpace(tmpl.Type) == "" {
				continue
			}
			docs = append(docs, bm25.Doc{ID: tmpl.Type, Text: templateSearchDocument(tmpl)})
		}
		r.mu.Unlock()

		idx := bm25.New()
		idx.Rebuild(docs)

		r.mu.Lock()
		if version == r.version {
			r.bm25Index = idx
			r.bm25Dirty = false
			r.mu.Unlock()
			return idx
		}
		r.mu.Unlock()
	}
}

func templateSearchDocument(tmpl *WorkflowTemplate) string {
	if tmpl == nil {
		return ""
	}
	var b strings.Builder
	appendPart := func(part string) {
		part = strings.TrimSpace(part)
		if part == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(part)
	}
	appendPart(tmpl.Type)
	appendPart(tmpl.Name)
	appendPart(tmpl.Description)
	for _, phase := range tmpl.Phases {
		appendPart(phase.ID)
		appendPart(phase.Name)
	}
	return b.String()
}

func hasStableTopTemplateScore(ranked []TemplateScore) bool {
	if len(ranked) == 0 || ranked[0].Score <= 0 {
		return false
	}
	if len(ranked) == 1 || ranked[1].Score <= 0 {
		return true
	}
	return ranked[0].Score >= ranked[1].Score*stableTemplateScoreLeadRatio
}

// --- Built-in Templates ---

func CodingTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "coding",
		Name:        "编程项目",
		Description: "需求 → 设计 → 任务分解 → 逐任务编码 → 验收。适用于开发应用、游戏、工具、系统、追踪系统。Software coding workflow for building applications, games, tools, systems, C++, backend, frontend, implementation, refactoring, and issue tracking systems.",
		Keywords:    []string{"开发", "编写", "实现", "写代码", "游戏", "应用", "工具", "系统", "重构"},
		Phases: []PhaseTemplate{
			{ID: "requirements", Name: "需求文档", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "design", Name: "技术设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "tasks", Name: "任务分解", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "implementation", Name: "编码执行", NeedsConfirm: false, ToolPolicy: ToolPolicyFull, ExecMode: ExecModeSubAgent},
			{ID: "verification", Name: "验收确认", NeedsConfirm: false, ToolPolicy: ToolPolicyFull, ExecMode: ExecModeAutoFromPrev},
		},
	}
}

func PresentationTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "presentation_design",
		Name:        "PPT 设计",
		Description: "受众分析 → 内容大纲 → 逐页脚本 → 生成 PPT。适用于产品介绍、方案汇报、商业展示、演示文稿、幻灯片和 slide deck 设计。",
		Keywords:    []string{"ppt", "幻灯片", "演示文稿", "slide", "PPT"},
		Phases: []PhaseTemplate{
			{ID: "audience_goal", Name: "受众与目标", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "outline", Name: "内容大纲", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "slide_scripting", Name: "逐页脚本", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "ppt_generation", Name: "PPT 生成", NeedsConfirm: false, ToolPolicy: ToolPolicyFull},
		},
	}
}

func ProductDesignTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "product_design",
		Name:        "产品设计",
		Description: "问题发现 → 用户研究 → 方案设计 → 原型设计",
		Keywords:    []string{"产品设计", "prd", "产品需求", "用户体验"},
		Phases: []PhaseTemplate{
			{ID: "problem_discovery", Name: "问题发现", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "user_research", Name: "用户研究", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "solution_design", Name: "方案设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "prototype", Name: "原型设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func InnovationTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "innovation",
		Name:        "创新方案",
		Description: "趋势分析 → 机会识别 → 方案设计 → 可行性评估 → 路线图",
		Keywords:    []string{"创新", "创意", "头脑风暴", "brainstorm"},
		Phases: []PhaseTemplate{
			{ID: "trend_analysis", Name: "趋势分析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "opportunity", Name: "机会识别", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "solution", Name: "方案设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "feasibility", Name: "可行性评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "roadmap", Name: "路线图", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func BusinessPlanTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "business_plan",
		Name:        "商业计划",
		Description: "市场分析 → 商业模式 → 财务规划 → 运营计划 → 风险评估",
		Keywords:    []string{"商业计划", "business plan", "创业", "融资", "BP"},
		Phases: []PhaseTemplate{
			{ID: "market_analysis", Name: "市场分析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "business_model", Name: "商业模式", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "financial_plan", Name: "财务规划", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "operations", Name: "运营计划", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "risk_assessment", Name: "风险评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func TestingTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "testing",
		Name:        "测试计划",
		Description: "测试策略 → 用例设计 → 环境准备 → 执行测试 → 缺陷报告",
		Keywords:    []string{"测试", "test plan", "QA", "质量保证"},
		Phases: []PhaseTemplate{
			{ID: "test_strategy", Name: "测试策略", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "test_cases", Name: "用例设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "test_environment", Name: "环境准备", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "test_execution", Name: "执行测试", NeedsConfirm: false, ToolPolicy: ToolPolicyFull},
			{ID: "defect_report", Name: "缺陷报告", NeedsConfirm: false, ToolPolicy: ToolPolicyFull, ExecMode: ExecModeAutoFromPrev},
		},
	}
}

func LiteratureReviewTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "literature_review",
		Name:        "文献综述",
		Description: "主题界定 → 文献检索 → 文献筛选 → 内容分析 → 综述撰写",
		Keywords:    []string{"文献综述", "literature review", "论文综述", "学术综述"},
		Phases: []PhaseTemplate{
			{ID: "topic_definition", Name: "主题界定", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "search_strategy", Name: "检索策略", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "screening", Name: "文献筛选", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "analysis", Name: "内容分析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "synthesis", Name: "综述撰写", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func ResearchReportTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "research_report",
		Name:        "研究报告",
		Description: "问题定义 → 方法论 → 数据收集 → 分析论证 → 结论建议",
		Keywords:    []string{"研究报告", "调研报告", "research report", "调研"},
		Phases: []PhaseTemplate{
			{ID: "problem_definition", Name: "问题定义", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "methodology", Name: "方法论", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "data_collection", Name: "数据收集", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "analysis", Name: "分析论证", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "conclusion", Name: "结论建议", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func CompetitiveAnalysisTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "competitive_analysis",
		Name:        "竞品分析",
		Description: "竞品识别 → 维度对比 → SWOT 分析 → 差异化策略 → 行动建议",
		Keywords:    []string{"竞品分析", "competitive analysis", "竞争分析", "SWOT"},
		Phases: []PhaseTemplate{
			{ID: "competitor_id", Name: "竞品识别", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "comparison", Name: "维度对比", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "swot", Name: "SWOT 分析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "differentiation", Name: "差异化策略", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "action_plan", Name: "行动建议", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func ProjectProposalTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "project_proposal",
		Name:        "项目方案",
		Description: "背景分析 → 目标定义 → 方案规划 → 资源预算 → 风险预案",
		Keywords:    []string{"项目方案", "立项", "project proposal", "项目计划"},
		Phases: []PhaseTemplate{
			{ID: "background", Name: "背景分析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "objectives", Name: "目标定义", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "plan", Name: "方案规划", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "budget", Name: "资源预算", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "risk_plan", Name: "风险预案", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func EventPlanningTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "event_planning",
		Name:        "活动策划",
		Description: "目标定位 → 创意策划 → 执行方案 → 预算排期 → 应急预案",
		Keywords:    []string{"活动策划", "event planning", "会议策划", "活动方案"},
		Phases: []PhaseTemplate{
			{ID: "positioning", Name: "目标定位", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "creative", Name: "创意策划", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "execution_plan", Name: "执行方案", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "budget_schedule", Name: "预算排期", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "contingency", Name: "应急预案", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func BidResponseTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "bid_response",
		Name:        "招投标文件",
		Description: "招标解析 → 资质响应 → 技术方案 → 商务报价 → 文件组装",
		Keywords:    []string{"招投标", "投标", "标书", "bid", "tender"},
		Phases: []PhaseTemplate{
			{ID: "tender_analysis", Name: "招标解析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "qualification", Name: "资质响应", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "technical", Name: "技术方案", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "commercial", Name: "商务报价", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "assembly", Name: "文件组装", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func ContractReviewTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "contract_review",
		Name:        "合同审查",
		Description: "合同解析 → 条款风险 → 合规审查 → 修改建议 → 审查意见",
		Keywords:    []string{"合同审查", "合同", "contract review", "法律审查"},
		Phases: []PhaseTemplate{
			{ID: "parsing", Name: "合同解析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "risk_analysis", Name: "条款风险", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "compliance", Name: "合规审查", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "suggestions", Name: "修改建议", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "opinion", Name: "审查意见", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func DueDiligenceTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "due_diligence",
		Name:        "尽职调查",
		Description: "公司画像 → 商业尽调 → 财务尽调 → 法律尽调 → 尽调结论",
		Keywords:    []string{"尽职调查", "尽调", "due diligence", "DD"},
		Phases: []PhaseTemplate{
			{ID: "company_profile", Name: "公司画像", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "business_dd", Name: "商业尽调", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "financial_dd", Name: "财务尽调", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "legal_dd", Name: "法律尽调", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "conclusion", Name: "尽调结论", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func ComplianceAuditTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "compliance_audit",
		Name:        "合规审计",
		Description: "审计范围 → 合规评估 → 风险评级 → 整改计划 → 审计报告",
		Keywords:    []string{"合规审计", "合规", "审计", "compliance audit"},
		Phases: []PhaseTemplate{
			{ID: "scope", Name: "审计范围", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "assessment", Name: "合规评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "risk_rating", Name: "风险评级", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "remediation", Name: "整改计划", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "report", Name: "审计报告", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func PatentAnalysisTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "patent_analysis",
		Name:        "专利分析",
		Description: "技术解析 → 现有技术 → 侵权评估 → 策略建议 → 分析报告",
		Keywords:    []string{"专利分析", "专利", "patent", "知识产权"},
		Phases: []PhaseTemplate{
			{ID: "tech_parsing", Name: "技术解析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "prior_art", Name: "现有技术", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "infringement", Name: "侵权评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "strategy", Name: "策略建议", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "report", Name: "分析报告", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func ExperimentDesignTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "experiment_design",
		Name:        "实验设计",
		Description: "假设提出 → 实验方案 → 变量控制 → 数据采集 → 结果分析",
		Keywords:    []string{"实验设计", "experiment", "实验方案", "A/B测试"},
		Phases: []PhaseTemplate{
			{ID: "hypothesis", Name: "假设提出", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "design", Name: "实验方案", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "variables", Name: "变量控制", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "data_plan", Name: "数据采集", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "analysis_plan", Name: "结果分析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func GrantProposalTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "grant_proposal",
		Name:        "基金申请",
		Description: "选题论证 → 研究基础 → 方案设计 → 预算编制 → 申请书",
		Keywords:    []string{"基金申请", "课题申请", "grant", "国自然", "科研项目"},
		Phases: []PhaseTemplate{
			{ID: "topic", Name: "选题论证", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "foundation", Name: "研究基础", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "methodology", Name: "方案设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "budget", Name: "预算编制", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "proposal", Name: "申请书", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func PaperWritingTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "paper_writing",
		Name:        "论文写作",
		Description: "大纲设计 → 文献梳理 → 正文撰写 → 图表制作 → 润色定稿",
		Keywords:    []string{"论文写作", "写论文", "paper writing", "学术论文"},
		Phases: []PhaseTemplate{
			{ID: "outline", Name: "大纲设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "literature", Name: "文献梳理", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "drafting", Name: "正文撰写", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "figures", Name: "图表制作", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "polishing", Name: "润色定稿", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

// RegisterBuiltinTemplates registers all built-in templates.
func RegisterBuiltinTemplates(r *TemplateRegistry) {
	if r == nil {
		return
	}
	r.Register(CodingTemplate())
	r.Register(PresentationTemplate())
	r.Register(ProductDesignTemplate())
	r.Register(InnovationTemplate())
	r.Register(BusinessPlanTemplate())
	r.Register(TestingTemplate())
	r.Register(LiteratureReviewTemplate())
	r.Register(ResearchReportTemplate())
	r.Register(CompetitiveAnalysisTemplate())
	r.Register(ProjectProposalTemplate())
	r.Register(EventPlanningTemplate())
	r.Register(BidResponseTemplate())
	r.Register(ContractReviewTemplate())
	r.Register(DueDiligenceTemplate())
	r.Register(ComplianceAuditTemplate())
	r.Register(PatentAnalysisTemplate())
	r.Register(ExperimentDesignTemplate())
	r.Register(GrantProposalTemplate())
	r.Register(PaperWritingTemplate())
}
