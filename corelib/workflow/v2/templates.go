package v2

import (
	"strings"
	"sync"
)

// PhaseTemplate defines a phase in a workflow template.
type PhaseTemplate struct {
	ID           string
	Name         string
	NeedsConfirm bool
	ToolPolicy   ToolPolicy
}

// WorkflowTemplate defines a type of workflow.
type WorkflowTemplate struct {
	Type        string
	Name        string
	Description string
	Keywords    []string // used for quick matching
	Phases      []PhaseTemplate
}

// TemplateRegistry holds all registered workflow templates.
type TemplateRegistry struct {
	mu        sync.RWMutex
	templates map[string]*WorkflowTemplate
}

func NewTemplateRegistry() *TemplateRegistry {
	return &TemplateRegistry{templates: make(map[string]*WorkflowTemplate)}
}

func (r *TemplateRegistry) Register(t *WorkflowTemplate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.templates[t.Type] = t
}

func (r *TemplateRegistry) Get(workflowType string) *WorkflowTemplate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.templates[workflowType]
}

// MatchByKeywords returns the best-matching template based on keyword hits.
// A keyword is "strong" if it has ≥2 runes (Chinese: 2 chars, English: 2+ chars).
// Requires at least one strong keyword hit to match.
func (r *TemplateRegistry) MatchByKeywords(text string) *WorkflowTemplate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	lower := strings.ToLower(text)

	var bestTemplate *WorkflowTemplate
	bestScore := 0

	for _, tmpl := range r.templates {
		score := 0
		hasStrong := false
		for _, kw := range tmpl.Keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				kwLen := len([]rune(kw))
				if kwLen >= 2 {
					score += 2
					hasStrong = true
				} else {
					score += 1
				}
			}
		}
		if hasStrong && score > bestScore {
			bestScore = score
			bestTemplate = tmpl
		}
	}
	return bestTemplate
}

// --- Built-in Templates ---

func CodingTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "coding",
		Name:        "编程项目",
		Description: "需求 → 设计 → 任务分解 → 逐任务编码",
		Keywords:    []string{"开发", "编写", "实现", "写代码", "游戏", "应用", "工具", "系统", "重构"},
		Phases: []PhaseTemplate{
			{ID: "requirements", Name: "需求文档", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "design", Name: "技术设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "tasks", Name: "任务分解", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "implementation", Name: "编码执行", NeedsConfirm: false, ToolPolicy: ToolPolicyFull},
		},
	}
}

func PresentationTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "presentation_design",
		Name:        "PPT 设计",
		Description: "受众分析 → 内容大纲 → 逐页脚本 → 生成 PPT",
		Keywords:    []string{"ppt", "幻灯片", "演示文稿", "slide"},
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
		Keywords:    []string{"产品设计", "prd", "产品需求"},
		Phases: []PhaseTemplate{
			{ID: "problem_discovery", Name: "问题发现", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "user_research", Name: "用户研究", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "solution_design", Name: "方案设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "prototype", Name: "原型设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

// RegisterBuiltinTemplates registers all built-in templates.
func RegisterBuiltinTemplates(r *TemplateRegistry) {
	r.Register(CodingTemplate())
	r.Register(PresentationTemplate())
	r.Register(ProductDesignTemplate())
}
