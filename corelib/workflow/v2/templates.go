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
	// ExecModeRemoteSubAgent: run via RemoteCodingSubAgent (iterative experiment loop on remote server).
	ExecModeRemoteSubAgent PhaseExecMode = "remote_subagent"
	// ExecModeAutoFromPrev: auto-complete using the previous phase's output
	// (no separate execution needed — the prior phase already produced the result).
	ExecModeAutoFromPrev PhaseExecMode = "auto_from_prev"
)

// PhaseInputSchema declares a structured AG UI form for a phase's information
// collection. When non-nil on a PhaseTemplate, the state machine signals
// ActionShowForm on first entry into the phase. The form data submitted by the
// user is stored in Phase.FormData and injected into the phase prompt.
type PhaseInputSchema struct {
	Title       string              `json:"title"`
	Description string              `json:"description,omitempty"`
	Fields      []PhaseInputField   `json:"fields"`
	Variants    []PhaseInputVariant `json:"variants,omitempty"`

	// AcceptsResume declares that this form can be auto-populated from a resume/CV
	// document. When true, the frontend renders a "上传简历" file upload entry at the
	// top of the form. If the user uploads a document, the backend parses it via LLM
	// and maps extracted fields to the form schema — the user then reviews and submits.
	// If the user does not upload, the system falls back to memory/knowledge recall.
	//
	// Typical use: academic application forms (长江学者, 国自然, 优青, etc.) where
	// most fields (name, institution, H-index, publications, education) can be
	// extracted from a standard academic CV.
	AcceptsResume bool `json:"accepts_resume,omitempty"`

	// AcceptsSupplementary declares that this form accepts optional supplementary
	// documents as additional context for LLM generation in subsequent phases.
	// Unlike AcceptsResume (which fills form fields), supplementary docs are stored
	// as-is and injected into phase prompts as reference material.
	//
	// Typical use: academic applications — user may upload research proposals,
	// representative paper lists, project summaries, award certificates, or any
	// document relevant to their research direction. 0 to N files, all optional.
	AcceptsSupplementary *SupplementaryDocConfig `json:"accepts_supplementary,omitempty"`
}

// SupplementaryDocConfig declares how a form accepts optional supplementary documents.
type SupplementaryDocConfig struct {
	// Label: UI display label for the upload section (e.g. "研究方向相关材料（可选）")
	Label string `json:"label"`
	// Description: help text explaining what types of documents are accepted
	Description string `json:"description"`
	// MaxFiles: maximum number of supplementary files (0 = unlimited, default: 5)
	MaxFiles int `json:"max_files,omitempty"`
	// AcceptedTypes: file extensions accepted (e.g. [".pdf", ".docx", ".md", ".txt"])
	// Empty = accept all document types
	AcceptedTypes []string `json:"accepted_types,omitempty"`
}

// PhaseInputVariant defines a mutually exclusive field group.
// When Variants is non-empty, the form renders a mode selector allowing the
// user to choose one variant. Only the selected variant's fields are visible
// and submitted. The submitted data includes "_agent_view_variant" = ID.
type PhaseInputVariant struct {
	ID     string            `json:"id"`
	Label  string            `json:"label"`
	Fields []PhaseInputField `json:"fields"`
}

// PhaseInputField defines a single form field.
type PhaseInputField struct {
	Name        string             `json:"name"`
	Label       string             `json:"label"`
	Type        string             `json:"type"` // text|textarea|number|date|select|multiselect|boolean|file|hidden
	Required    bool               `json:"required,omitempty"`
	Sensitive   bool               `json:"sensitive,omitempty"`
	Description string             `json:"description,omitempty"`
	Placeholder string             `json:"placeholder,omitempty"`
	Options     []PhaseInputOption `json:"options,omitempty"`
	Default     interface{}        `json:"default,omitempty"`

	// Reusable declares that this field represents stable personal/institutional
	// information that is consistent across different workflow instances (e.g. name,
	// institution, discipline, H-index). When true:
	//   - After form submission, the value is sedimented to long-term memory as user_fact.
	//   - On future form renders, the prefill system actively recalls this field from memory.
	// When false (default), the field is considered task-specific (e.g. project_title,
	// hypothesis) and is neither sedimented nor recalled.
	// This is the single source of truth for prefill/sediment eligibility — no hardcoded
	// whitelist outside the template definition.
	Reusable bool `json:"reusable,omitempty"`
}

// PhaseInputOption defines a selectable option for select/multiselect fields.
type PhaseInputOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type PhaseTemplate struct {
	ID            string
	Name          string
	NeedsConfirm  bool
	ToolPolicy    ToolPolicy
	Kind          PhaseKind
	MutationScope MutationScope
	ExecMode      PhaseExecMode // how this phase is executed (default = agent loop)

	// InputSchema declares a structured AG UI form for this phase's information
	// collection. When non-nil, the state machine returns ActionShowForm on first
	// entry (before FormData is populated). The GUI emits an AG UI form; the user
	// fills it and submits via SubmitForm. The form data is then injected into the
	// phase prompt as structured context before the LLM generates the deliverable.
	// Nil means the phase uses natural language interaction (default behavior).
	InputSchema *PhaseInputSchema

	// DependsOnFull declares which prior phase(s) this phase requires the FULL
	// (un-truncated) output from. When BuildPhasePrompt constructs the "前序阶段
	// 产出物" section, phases listed here receive a much larger rune budget
	// (up to 30000 runes) instead of the default truncation (500 runes).
	//
	// This is the mechanism-level solution to the "PPT generation only sees 2
	// pages of a 20-page script" problem: the template declares the dependency,
	// and the prompt builder respects it. No hardcoded phase-ID checks needed.
	//
	// Example: ppt_generation depends on slide_scripting's full output to know
	// what content to put on each of the 20 slides.
	DependsOnFull []string
}

// WorkflowTemplate defines a type of workflow.
type WorkflowTemplate struct {
	Type        string
	Name        string
	Description string
	Keywords    []string // retained for metadata and UI display; not used for routing
	Phases      []PhaseTemplate

	// SemanticOnly when true excludes this template from BM25 text matching.
	// The template can only be activated through semantic intent classification
	// (IUM LLM) which returns this type as the category. This prevents
	// accidental BM25 token overlaps from triggering the workflow.
	SemanticOnly bool
}

// BackfillPhaseDependenciesFromTemplate adds dependencies introduced by a newer
// template to an already-persisted workflow state. It only fills an empty
// DependsOnFull field, preserving explicit dependencies captured when the
// workflow was created.
func BackfillPhaseDependenciesFromTemplate(state *WorkflowState, tmpl *WorkflowTemplate) bool {
	if state == nil || tmpl == nil {
		return false
	}
	changed := false
	for i := range state.Phases {
		if len(state.Phases[i].DependsOnFull) != 0 {
			continue
		}
		for _, spec := range tmpl.Phases {
			if spec.ID != state.Phases[i].ID || len(spec.DependsOnFull) == 0 {
				continue
			}
			state.Phases[i].DependsOnFull = append([]string(nil), spec.DependsOnFull...)
			changed = true
			break
		}
	}
	return changed
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

// AllTypes returns all registered workflow type strings.
// This is the single source of truth for "what templates exist" — consumers
// should call this instead of maintaining their own hardcoded lists.
func (r *TemplateRegistry) AllTypes() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.templates))
	for t := range r.templates {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// MatchByText returns the best advisory match using the template's structured
// definition. It intentionally ignores Keywords so routing does not become a
// keyword shortcut. Templates with SemanticOnly=true are excluded from the
// BM25 index and can only be activated via IUM LLM classification.
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

// minAbsoluteTemplateScore is the minimum BM25 score required for a template
// match to be considered valid. Without this, short/unrelated messages (e.g.
// "你是谁？") can produce low but non-zero scores (e.g. 2.0) due to CJK
// bigram/trigram overlap with long template search documents, causing spurious
// workflow triggers.
//
// Design principle: prefer triggering the choice panel (user can dismiss) over
// missing a valid workflow request (user doesn't know the workflow exists).
// Calibrated via grid search over 88 test cases (42 workflow + 46 skip) using
// F2 score (recall-weighted). Optimal: 2.25 with lead ratio 1.0.
// At 2.25: all noise (max observed: "你是谁？"=2.05) is rejected, all legitimate
// requests (min observed: "写论文"=3.1) pass through to the choice panel.
const minAbsoluteTemplateScore = 2.25

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
			if tmpl.SemanticOnly {
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
	// Reject low-confidence matches: BM25 scores below the absolute minimum
	// are noise from CJK ngram overlap, not meaningful template matches.
	// Calibrated via grid search over 88 test cases: optimal threshold = 2.25
	// with no lead ratio requirement (embedding veto handles ambiguity).
	return ranked[0].Score >= minAbsoluteTemplateScore
}

// --- Built-in Templates ---

func CodingTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "coding",
		Name:        "编程项目",
		Description: "需求 → 设计 → 任务分解 → 逐任务编码 → 验收。适用于开发应用、游戏、工具、系统、追踪系统。Software coding workflow for building applications, games, tools, systems, C++, backend, frontend, implementation, refactoring, and issue tracking systems.",
		Keywords:    []string{"开发", "编写", "实现", "写代码", "游戏", "应用", "工具", "系统", "重构"},
		Phases: []PhaseTemplate{
			{ID: "requirements", Name: "需求文档", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly, Kind: PhaseKindDocumentPlanning, MutationScope: MutationScopeWorkflowDoc,
				InputSchema: &PhaseInputSchema{
					Title: "项目基本信息",
					Fields: []PhaseInputField{
						{Name: "project_name", Label: "项目名称", Type: "text", Required: true, Placeholder: "例如：贪吃蛇游戏"},
						{Name: "tech_stack", Label: "技术栈", Type: "text", Placeholder: "例如：Go + React / C++ / Python"},
						{Name: "description", Label: "项目描述", Type: "textarea", Required: true, Placeholder: "简要描述你想实现的功能与目标"},
						{Name: "project_path", Label: "项目目录", Type: "directory", Placeholder: "选择项目目录（可新建但不会自动创建）"},
					},
				},
			},
			{ID: "design", Name: "技术设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly, Kind: PhaseKindDocumentPlanning, MutationScope: MutationScopeWorkflowDoc},
			{ID: "tasks", Name: "任务分解", NeedsConfirm: true, ToolPolicy: ToolPolicyPlanning, Kind: PhaseKindCodePlanning, MutationScope: MutationScopeWorkflowDoc},
			{ID: "implementation", Name: "编码执行", NeedsConfirm: false, ToolPolicy: ToolPolicyFull, Kind: PhaseKindExecution, MutationScope: MutationScopeProject, ExecMode: ExecModeSubAgent},
			{ID: "verification", Name: "验收确认", NeedsConfirm: false, ToolPolicy: ToolPolicyFull, Kind: PhaseKindReview, MutationScope: MutationScopeProject, ExecMode: ExecModeAutoFromPrev},
		},
	}
}

// CodingSubAgentTemplate is the single-phase "简化编程" workflow. It uses the
// same workflow form pipeline as other templates, then runs CodingSubAgent
// without requirements/design/task phases.
func CodingSubAgentTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:         "coding_subagent",
		Name:         "简化编程",
		Description:  "通过工作流表单收集工作目录和项目描述，提交后执行 CodingSubAgent。Quick coding workflow that collects a working directory and code request in the standard form panel, then runs CodingSubAgent.",
		Keywords:     []string{"简化编程", "快速编程", "快速编码", "quick coding", "coding subagent"},
		SemanticOnly: true,
		Phases: []PhaseTemplate{
			{ID: "coding_subagent_execution", Name: "简化编程", NeedsConfirm: false, ToolPolicy: ToolPolicyFull, Kind: PhaseKindExecution, MutationScope: MutationScopeProject, ExecMode: ExecModeSubAgent,
				InputSchema: &PhaseInputSchema{
					Title:       "简化编程",
					Description: "填写工作目录和要修改的代码需求，提交后启动编程智能体。",
					Fields: []PhaseInputField{
						{Name: "work_dir", Label: "工作目录", Type: "directory", Required: true, Placeholder: "选择或输入项目工作目录"},
						{Name: "project_description", Label: "项目描述 / 代码需求", Type: "textarea", Required: true, Placeholder: "例如：修改用户列表页面的筛选逻辑，并补充单元测试"},
					},
				},
			},
		},
	}
}

// RemoteCodingSubAgentTemplate is the single-phase "远程编程" workflow. It
// collects SSH connection details in the standard workflow form and dispatches
// to RemoteCodingSubAgent.
func RemoteCodingSubAgentTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:         "remote_coding_subagent",
		Name:         "远程编程",
		Description:  "通过工作流表单收集 SSH 主机信息和远程项目描述，连接后执行 RemoteCodingSubAgent。Remote coding workflow that collects SSH details in the standard form panel, connects, then runs RemoteCodingSubAgent.",
		Keywords:     []string{"远程编程", "远程编码", "ssh 编程", "remote coding", "remote subagent"},
		SemanticOnly: true,
		Phases: []PhaseTemplate{
			{ID: "remote_coding_subagent_execution", Name: "远程编程", NeedsConfirm: false, ToolPolicy: ToolPolicyFull, Kind: PhaseKindExecution, MutationScope: MutationScopeProject, ExecMode: ExecModeRemoteSubAgent,
				InputSchema: &PhaseInputSchema{
					Title:       "远程编程",
					Description: "填写 SSH 连接信息、默认工作目录和要修改的代码需求，提交后启动远程编程智能体。",
					Fields: []PhaseInputField{
						{Name: "ssh_host", Label: "主机 IP / 域名", Type: "text", Required: true, Placeholder: "例如：192.168.1.10 或 example.com"},
						{Name: "ssh_port", Label: "端口", Type: "number", Required: true, Default: 22, Placeholder: "22"},
						{Name: "ssh_user", Label: "用户名", Type: "text", Required: true, Placeholder: "例如：root"},
						{Name: "ssh_password", Label: "密码", Type: "text", Required: true, Sensitive: true, Placeholder: "SSH 登录密码"},
						{Name: "work_dir", Label: "默认工作目录", Type: "text", Required: true, Placeholder: "例如：/home/user/project"},
						{Name: "project_description", Label: "项目描述 / 代码需求", Type: "textarea", Required: true, Placeholder: "例如：在远程项目中修复登录接口的超时处理，并补充测试"},
					},
				},
			},
		},
	}
}

// MaintenanceTemplate is a lightweight coding workflow for maintenance,
// refactoring, and incremental feature changes on existing projects.
// It has only 3 phases (vs coding's 5): a combined analysis+plan phase,
// execution via SubAgent, and auto-verification.
func MaintenanceTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:         "maintenance",
		Name:         "维护/重构",
		Description:  "影响分析 → 实施方案 → 执行验证。适用于现有项目的架构重构、技术栈迁移、模式改造。Lightweight coding workflow for refactoring architecture, migrating technology stacks, and transforming existing codebases.",
		Keywords:     []string{"维护", "重构", "改造", "迁移", "改为", "改成", "换成", "升级", "refactor", "migrate", "maintenance"},
		SemanticOnly: true, // Only activated via IUM LLM classification, not BM25 text matching
		Phases: []PhaseTemplate{
			{ID: "maint_analysis", Name: "影响分析与方案", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly, Kind: PhaseKindDocumentPlanning, MutationScope: MutationScopeWorkflowDoc},
			{ID: "maint_execution", Name: "重构执行", NeedsConfirm: false, ToolPolicy: ToolPolicyFull, Kind: PhaseKindExecution, MutationScope: MutationScopeProject, ExecMode: ExecModeSubAgent},
			{ID: "maint_verification", Name: "验证", NeedsConfirm: false, ToolPolicy: ToolPolicyFull, Kind: PhaseKindReview, MutationScope: MutationScopeProject, ExecMode: ExecModeAutoFromPrev},
		},
	}
}

// OpsMaintenanceTemplate is the server-ops / SRE controlled-change workflow.
// Early phases produce intake, read-only environment reports, maintenance
// artifacts, and a risk policy; only controlled_execution may run approved
// bash/ssh commands from that risk-policy manifest.
func OpsMaintenanceTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "ops_maintenance",
		Name:        "运维维护",
		Description: "运维 intake → 只读采集 → 维护工件 → 风险策略 → 受控执行。适用于服务器运维、巡检、排障、变更、回滚与审批后执行。Ops maintenance workflow for server ops, inspection, incident response, change, rollback, and controlled execution.",
		Keywords:    []string{"ops", "operations", "maintenance", "server", "ssh", "linux", "sre", "devops", "运维", "服务器", "巡检", "排障", "应急", "变更", "回滚", "风险", "审批", "执行"},
		Phases: []PhaseTemplate{
			{ID: "ops_intake", Name: "Ops Intake", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly, Kind: PhaseKindIntake, MutationScope: MutationScopeWorkflowDoc},
			{ID: "readonly_collection", Name: "Read-only Collection", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly, Kind: PhaseKindDocumentPlanning, MutationScope: MutationScopeWorkflowDoc},
			{ID: "artifact_plan", Name: "Maintenance Artifacts", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly, Kind: PhaseKindDocumentPlanning, MutationScope: MutationScopeWorkflowDoc},
			{ID: "risk_policy", Name: "Risk Policy Gate", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly, Kind: PhaseKindOpsRiskPolicy, MutationScope: MutationScopeWorkflowDoc},
			{ID: "controlled_execution", Name: "Controlled Execution", NeedsConfirm: false, ToolPolicy: ToolPolicyOpsControlled, Kind: PhaseKindOpsExecution, MutationScope: MutationScopeProject},
		},
	}
}

func PresentationTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "presentation_design",
		Name:        "PPT 设计",
		Description: "受众分析 → 内容大纲 → 逐页脚本 → 生成 PPT。适用于产品介绍、方案汇报、商业展示、演示文稿、幻灯片和 slide deck 设计。",
		Keywords:    []string{"ppt", "powerpoint", "幻灯片", "演示文稿", "slide", "slide deck", "PPT", "PowerPoint"},
		Phases: []PhaseTemplate{
			{ID: "audience_goal", Name: "受众与目标", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly, Kind: PhaseKindDocumentPlanning, MutationScope: MutationScopeWorkflowDoc,
				InputSchema: &PhaseInputSchema{
					Title: "PPT基本信息",
					Fields: []PhaseInputField{
						{Name: "topic", Label: "主题", Type: "text", Required: true, Placeholder: "PPT演示的主题"},
						{Name: "audience", Label: "目标受众", Type: "text", Required: true, Placeholder: "如：公司高管、客户、学生"},
						{Name: "purpose", Label: "演示目的", Type: "text", Placeholder: "如：产品介绍、方案汇报"},
						{Name: "page_count", Label: "期望页数", Type: "text", Placeholder: "如：20页"},
						{Name: "style", Label: "风格偏好", Type: "text", Placeholder: "如：商务简约、科技感"},
						{Name: "key_points", Label: "要点", Type: "textarea", Placeholder: "列出希望涵盖的核心内容要点"},
					},
				},
			},
			{ID: "outline", Name: "内容大纲", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly, Kind: PhaseKindDocumentPlanning, MutationScope: MutationScopeWorkflowDoc},
			// A slide script is an expansion of the approved outline.  This is a
			// data dependency, not merely helpful background, so it must receive the
			// complete outline rather than the default short previous-phase summary.
			{ID: "slide_scripting", Name: "逐页脚本", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly, Kind: PhaseKindDocumentPlanning, MutationScope: MutationScopeWorkflowDoc,
				DependsOnFull: []string{"outline"}},
			{ID: "ppt_generation", Name: "PPT 生成", NeedsConfirm: false, ToolPolicy: ToolPolicyFull, Kind: PhaseKindArtifactGeneration, MutationScope: MutationScopeArtifact,
				DependsOnFull: []string{"slide_scripting", "outline"}},
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
			{ID: "problem_discovery", Name: "问题发现", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "产品基本信息",
					Fields: []PhaseInputField{
						{Name: "product_name", Label: "产品名称", Type: "text", Required: true, Placeholder: "产品或项目名称"},
						{Name: "target_users", Label: "目标用户群", Type: "text", Required: true, Placeholder: "如：25-35岁职场白领"},
						{Name: "problem_desc", Label: "核心问题", Type: "textarea", Required: true, Placeholder: "描述用户面临的核心痛点"},
						{Name: "current_solutions", Label: "现有方案", Type: "textarea", Placeholder: "目前市场上的解决方案及不足"},
					},
				},
			},
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
			{ID: "trend_analysis", Name: "趋势分析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "创新方向信息",
					Fields: []PhaseInputField{
						{Name: "domain", Label: "行业/领域", Type: "text", Required: true, Placeholder: "如：智能制造、医疗健康"},
						{Name: "challenge", Label: "挑战/痛点", Type: "textarea", Required: true, Placeholder: "描述当前面临的挑战或痛点"},
						{Name: "constraints", Label: "约束条件", Type: "textarea", Placeholder: "如：预算、时间、技术限制"},
						{Name: "inspiration", Label: "灵感参考", Type: "textarea", Placeholder: "已有的创意方向或参考案例"},
					},
				},
			},
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
			{ID: "market_analysis", Name: "市场分析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "商业计划基本信息",
					Fields: []PhaseInputField{
						{Name: "company_name", Label: "公司名称", Type: "text", Required: true, Placeholder: "公司或项目名称"},
						{Name: "industry", Label: "行业", Type: "text", Required: true, Placeholder: "如：SaaS、电商、医疗"},
						{Name: "product_service", Label: "产品/服务描述", Type: "textarea", Required: true, Placeholder: "描述核心产品或服务"},
						{Name: "target_market", Label: "目标市场", Type: "text", Placeholder: "如：中国中小企业"},
						{Name: "funding_goal", Label: "融资目标", Type: "text", Placeholder: "如：500万天使轮"},
						{Name: "stage", Label: "当前阶段", Type: "text", Placeholder: "如：种子期、A轮前"},
					},
				},
			},
			{ID: "business_model", Name: "商业模式", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "financial_plan", Name: "财务规划", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "operations", Name: "运营计划", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "risk_assessment", Name: "风险评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				DependsOnFull: []string{"market_analysis", "business_model", "financial_plan"},
			},
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
			{ID: "test_strategy", Name: "测试策略", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "测试项目信息",
					Fields: []PhaseInputField{
						{Name: "test_target", Label: "测试对象", Type: "text", Required: true, Placeholder: "如：XX系统 v2.0"},
						{Name: "test_scope", Label: "测试范围", Type: "text", Required: true, Placeholder: "如：功能测试、性能测试"},
						{Name: "test_env", Label: "测试环境", Type: "text", Placeholder: "如：Chrome/Firefox、iOS/Android"},
					},
				},
			},
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
			{ID: "topic_definition", Name: "主题界定", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "文献综述信息",
					Fields: []PhaseInputField{
						{Name: "research_topic", Label: "研究主题", Type: "text", Required: true, Placeholder: "如：深度学习在医学影像中的应用"},
						{Name: "time_range", Label: "时间范围", Type: "text", Placeholder: "如：2020-2026"},
						{Name: "databases", Label: "检索数据库", Type: "text", Placeholder: "如：PubMed、Web of Science、知网"},
						{Name: "language", Label: "语种", Type: "text", Placeholder: "如：中文、英文"},
					},
				},
			},
			{ID: "search_strategy", Name: "检索策略", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "screening", Name: "文献筛选", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "analysis", Name: "内容分析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "synthesis", Name: "综述撰写", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				DependsOnFull: []string{"analysis", "screening"},
			},
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
			{ID: "problem_definition", Name: "问题定义", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "研究报告信息",
					Fields: []PhaseInputField{
						{Name: "research_topic", Label: "研究主题", Type: "text", Required: true, Placeholder: "如：中国新能源汽车市场分析"},
						{Name: "purpose", Label: "研究目的", Type: "textarea", Required: true, Placeholder: "描述研究的目的和预期产出"},
						{Name: "scope", Label: "研究范围", Type: "text", Placeholder: "如：2023-2026年中国市场"},
					},
				},
			},
			{ID: "methodology", Name: "方法论", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "data_collection", Name: "数据收集", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "analysis", Name: "分析论证", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "conclusion", Name: "结论建议", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				DependsOnFull: []string{"data_collection", "analysis"},
			},
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
			{ID: "competitor_id", Name: "竞品识别", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "竞品分析信息",
					Fields: []PhaseInputField{
						{Name: "our_product", Label: "我方产品", Type: "text", Required: true, Placeholder: "我方产品或服务名称"},
						{Name: "competitors", Label: "已知竞品", Type: "textarea", Required: true, Placeholder: "列出主要竞争对手，每行一个"},
						{Name: "dimensions", Label: "分析维度", Type: "text", Placeholder: "如：功能、价格、用户体验、市场份额"},
					},
				},
			},
			{ID: "comparison", Name: "维度对比", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "swot", Name: "SWOT 分析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "differentiation", Name: "差异化策略", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "action_plan", Name: "行动建议", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				DependsOnFull: []string{"comparison", "swot", "differentiation"},
			},
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
			{ID: "background", Name: "背景分析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "项目方案信息",
					Fields: []PhaseInputField{
						{Name: "project_name", Label: "项目名称", Type: "text", Required: true, Placeholder: "项目名称"},
						{Name: "organization", Label: "所属组织", Type: "text", Placeholder: "如：XX公司/XX部门"},
						{Name: "objective", Label: "项目目标", Type: "textarea", Required: true, Placeholder: "描述项目要达成的目标"},
						{Name: "timeline", Label: "时间线", Type: "text", Placeholder: "如：2026年Q3-Q4"},
					},
				},
			},
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
			{ID: "positioning", Name: "目标定位", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "活动策划信息",
					Fields: []PhaseInputField{
						{Name: "event_name", Label: "活动名称", Type: "text", Required: true, Placeholder: "活动名称"},
						{Name: "event_type", Label: "活动类型", Type: "text", Required: true, Placeholder: "如：年会、产品发布会、团建"},
						{Name: "expected_date", Label: "预计时间", Type: "text", Placeholder: "如：2026年8月"},
						{Name: "expected_scale", Label: "预计规模", Type: "text", Placeholder: "如：200人"},
						{Name: "budget", Label: "预算", Type: "text", Placeholder: "如：50万"},
					},
				},
			},
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
			{ID: "tender_analysis", Name: "招标解析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "招投标信息",
					Fields: []PhaseInputField{
						{Name: "bid_doc_path", Label: "招标文件路径", Type: "file", Placeholder: "如：D:\\投标\\招标文件.pdf"},
						{Name: "bid_doc_text", Label: "或粘贴招标文件内容", Type: "textarea", Placeholder: "将招标文件核心内容粘贴到这里"},
						{Name: "our_company", Label: "投标公司", Type: "text", Required: true, Placeholder: "我方公司名称"},
						{Name: "focus_areas", Label: "重点关注", Type: "textarea", Placeholder: "如：技术要求、资质门槛、评分标准"},
					},
				},
			},
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
			{ID: "parsing", Name: "合同解析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "合同审查信息",
					Fields: []PhaseInputField{
						{Name: "contract_path", Label: "合同文件路径", Type: "file", Placeholder: "如：D:\\合同\\采购合同.pdf"},
						{Name: "contract_text", Label: "或粘贴合同文本", Type: "textarea", Placeholder: "将合同主要条款内容粘贴到这里"},
						{Name: "review_purpose", Label: "审查目的", Type: "text", Required: true, Placeholder: "如：签约前风险评估"},
						{Name: "focus_areas", Label: "重点关注", Type: "textarea", Placeholder: "如：付款条款、违约责任、知识产权"},
					},
				},
			},
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
			{ID: "company_profile", Name: "公司画像", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "尽调信息",
					Fields: []PhaseInputField{
						{Name: "target_company", Label: "目标公司", Type: "text", Required: true, Placeholder: "被调查公司名称"},
						{Name: "dd_type", Label: "尽调类型", Type: "text", Required: true, Placeholder: "如：投资尽调、并购尽调"},
						{Name: "industry", Label: "行业", Type: "text", Placeholder: "如：互联网、新能源"},
						{Name: "key_concerns", Label: "重点关注", Type: "textarea", Placeholder: "如：财务真实性、知识产权、竞业限制"},
					},
				},
			},
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
			{ID: "scope", Name: "审计范围", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "合规审计信息",
					Fields: []PhaseInputField{
						{Name: "audit_target", Label: "审计对象", Type: "text", Required: true, Placeholder: "如：XX公司数据隐私合规"},
						{Name: "audit_scope", Label: "审计范围", Type: "text", Required: true, Placeholder: "如：个人信息处理全流程"},
						{Name: "regulations", Label: "适用法规", Type: "textarea", Placeholder: "如：GDPR、个人信息保护法、数据安全法"},
						{Name: "period", Label: "审计期间", Type: "text", Placeholder: "如：2025年1月-2026年6月"},
					},
				},
			},
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
			{ID: "tech_parsing", Name: "技术解析", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "专利分析信息",
					Fields: []PhaseInputField{
						{Name: "technology_field", Label: "技术领域", Type: "text", Required: true, Placeholder: "如：锂电池正极材料"},
						{Name: "analysis_purpose", Label: "分析目的", Type: "text", Required: true, Placeholder: "如：侵权风险评估、专利布局规划"},
						{Name: "patent_numbers", Label: "相关专利号", Type: "textarea", Placeholder: "如：CN1234567A，每行一个"},
						{Name: "competitor_companies", Label: "竞争对手", Type: "textarea", Placeholder: "如：XX公司、YY公司"},
					},
				},
			},
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
			{ID: "hypothesis", Name: "假设提出", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "实验设计信息",
					Fields: []PhaseInputField{
						{Name: "research_question", Label: "研究问题", Type: "text", Required: true, Placeholder: "如：新算法是否显著提升检测精度"},
						{Name: "hypothesis", Label: "研究假设", Type: "textarea", Required: true, Placeholder: "描述你的假设"},
						{Name: "field", Label: "学科领域", Type: "text", Placeholder: "如：计算机视觉、药理学"},
						{Name: "resources", Label: "可用资源", Type: "textarea", Placeholder: "如：GPU服务器、实验室设备、数据集"},
					},
				},
			},
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
		Keywords:    []string{"基金申请", "课题申请", "grant", "科研项目", "项目申请书"},
		Phases: []PhaseTemplate{
			{ID: "topic", Name: "选题论证", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "基金项目信息",
					Fields: []PhaseInputField{
						{Name: "name", Label: "申请人", Type: "text", Required: true},
						{Name: "institution", Label: "单位", Type: "text", Required: true, Placeholder: "如：XX大学"},
						{Name: "title", Label: "职称", Type: "text", Placeholder: "如：教授/副教授"},
						{Name: "fund_type", Label: "基金类型", Type: "text", Required: true, Placeholder: "如：省自然科学基金、教育部人文社科"},
						{Name: "research_field", Label: "研究领域", Type: "text", Required: true, Placeholder: "如：人工智能、材料科学"},
						{Name: "project_title", Label: "项目名称", Type: "text", Required: true, Placeholder: "拟申报项目的题目"},
						{Name: "core_question", Label: "核心问题", Type: "textarea", Required: true, Placeholder: "用1-2句话描述拟解决的科学问题"},
						{Name: "prior_work", Label: "前期基础", Type: "textarea", Placeholder: "简述与本项目相关的已有研究基础"},
					},
				},
			},
			{ID: "foundation", Name: "研究基础", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "methodology", Name: "方案设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "budget", Name: "预算编制", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "proposal", Name: "申请书", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				DependsOnFull: []string{"topic", "foundation", "methodology"},
			},
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
			{ID: "outline", Name: "大纲设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title: "论文写作信息",
					Fields: []PhaseInputField{
						{Name: "paper_topic", Label: "论文主题", Type: "text", Required: true, Placeholder: "如：基于Transformer的多模态情感分析"},
						{Name: "target_journal", Label: "目标期刊", Type: "text", Placeholder: "如：IEEE TPAMI、计算机学报"},
						{Name: "paper_type", Label: "论文类型", Type: "text", Placeholder: "如：研究论文、综述论文"},
						{Name: "key_contribution", Label: "核心贡献", Type: "textarea", Required: true, Placeholder: "描述本文的主要创新点和贡献"},
					},
				},
			},
			{ID: "literature", Name: "文献梳理", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "drafting", Name: "正文撰写", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "figures", Name: "图表制作", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "polishing", Name: "润色定稿", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				DependsOnFull: []string{"drafting", "literature"},
			},
		},
	}
}

func PaperReproductionTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "paper_reproduction",
		Name:        "论文复现",
		Description: "论文解读 → 复现规划（源码/数据集搜索） → 环境与数据 → 基线复现 → 迭代改进 → 实验报告。适用于阅读学术论文后搜索源码和数据集，在远程服务器上搭建环境、下载数据、复现基线实验，然后迭代改进直到结果显著超越论文，最终生成包含对比实验、消融实验、超参数分析的完整实验报告。Paper reproduction: read paper, search for source code and datasets, set up remote environment, reproduce baseline, iteratively improve, generate comprehensive reports with ablation studies.",
		Keywords:    []string{"论文复现", "复现", "reproduce", "replication", "实验复现", "跑实验", "复现实验"},
		Phases: []PhaseTemplate{
			{ID: "paper_analysis", Name: "论文深度解读", NeedsConfirm: true, ToolPolicy: ToolPolicyFull,
				InputSchema: &PhaseInputSchema{
					Title: "论文复现信息",
					Fields: []PhaseInputField{
						{Name: "paper_title", Label: "论文标题", Type: "text", Required: true, Placeholder: "论文的完整标题"},
						{Name: "paper_url", Label: "论文链接", Type: "text", Placeholder: "如：https://arxiv.org/abs/xxxx.xxxxx"},
						{Name: "ssh_host", Label: "GPU服务器", Type: "text", Placeholder: "如：user@192.168.1.100:22"},
						{Name: "ssh_password", Label: "密码", Type: "text", Sensitive: true, Placeholder: "SSH登录密码", Description: "仅存储在本机，不会上传到任何服务器"},
						{Name: "work_dir", Label: "工作目录", Type: "text", Placeholder: "如：/home/user/experiments"},
					},
				},
			},
			{ID: "reproduction_plan", Name: "复现规划", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
			{ID: "env_and_data", Name: "环境搭建与数据准备", NeedsConfirm: false, ToolPolicy: ToolPolicyFull},
			{ID: "baseline_reproduction", Name: "基线实验复现", NeedsConfirm: false, ToolPolicy: ToolPolicyFull},
			{ID: "iterative_improvement", Name: "迭代改进", NeedsConfirm: false, ToolPolicy: ToolPolicyFull, ExecMode: ExecModeRemoteSubAgent},
			{ID: "experiment_report", Name: "实验报告", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
		},
	}
}

// ChangjiangScholarReviewTemplate defines the Changjiang Scholar review/evaluation workflow.
// Used by reviewers or applicants for self-assessment before submission.
func ChangjiangScholarReviewTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:         "changjiang_scholar_review",
		Name:         "长江学者申报书评审",
		Description:  "完整性检测 → 学术成果评估 → 研究计划评估 → 撰写质量评估 → 综合评估报告。适用于长江学者申报材料的自审或他审，从多维度评估已有材料质量并给出改进建议。Changjiang Scholar application review: completeness check, achievement evaluation, plan feasibility, narrative quality, improvement report.",
		Keywords:     []string{"长江学者评审", "申报书评审", "长江评审", "申报书审查"},
		SemanticOnly: true,
		Phases: []PhaseTemplate{
			{ID: "cj_completeness_check", Name: "基本信息完整性检测", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title:       "提供申报材料",
					Description: "请提供待评审的长江学者申报材料。支持上传文件（PDF/Word）、粘贴文本内容、或指定本机文件路径。",
					Fields: []PhaseInputField{
						{Name: "material_path", Label: "申报材料文件路径", Type: "file", Placeholder: "如：D:\\申报材料\\长江学者申报书.pdf（如果文件在本机）"},
						{Name: "material_text", Label: "或粘贴申报材料文本", Type: "textarea", Placeholder: "将申报书的主要内容粘贴到这里"},
						{Name: "focus_areas", Label: "重点关注方面（可选）", Type: "textarea", Placeholder: "如：学术成果是否突出、研究计划是否可行"},
					},
				},
			},
			{ID: "cj_achievement_evaluation", Name: "学术成果质量评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "cj_plan_feasibility", Name: "研究计划可行性评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "cj_narrative_quality", Name: "材料撰写质量评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "cj_improvement_report", Name: "综合评估与修改建议报告", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

// NSFCDistinguishedYouthReviewTemplate defines the review workflow for NSFC Distinguished Youth Fund applications.
func NSFCDistinguishedYouthReviewTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:         "nsfc_distinguished_youth_review",
		Name:         "杰青申请书评审",
		Description:  "完整性检测 → 学术成果评估 → 研究计划评估 → 撰写质量评估 → 综合评估报告。适用于杰青申请书材料的评审与改进建议。",
		Keywords:     []string{"杰青评审", "杰青审查", "杰青材料评审"},
		SemanticOnly: true,
		Phases: []PhaseTemplate{
			{ID: "dy_review_completeness", Name: "完整性检测", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title:       "提供申报材料",
					Description: "请提供待评审的杰青申请书材料。支持上传文件、粘贴文本、或指定本机文件路径。",
					Fields: []PhaseInputField{
						{Name: "material_path", Label: "申请书文件路径", Type: "file", Placeholder: "如：D:\\申报材料\\杰青申请书.pdf"},
						{Name: "material_text", Label: "或粘贴申请书文本", Type: "textarea", Placeholder: "将申请书的主要内容粘贴到这里"},
						{Name: "focus_areas", Label: "重点关注方面（可选）", Type: "textarea", Placeholder: "如：学术贡献是否突出、研究方案创新性"},
					},
				},
			},
			{ID: "dy_review_achievements", Name: "学术成果评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "dy_review_plan", Name: "研究计划评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "dy_review_quality", Name: "撰写质量评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "dy_review_report", Name: "综合评估报告", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

// NSFCExcellentYouthReviewTemplate defines the review workflow for NSFC Excellent Young Scientists Fund applications.
func NSFCExcellentYouthReviewTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:         "nsfc_excellent_youth_review",
		Name:         "优青申请书评审",
		Description:  "完整性检测 → 学术成果评估 → 研究计划评估 → 撰写质量评估 → 综合评估报告。适用于优青申请书材料的评审与改进建议。",
		Keywords:     []string{"优青评审", "优青审查", "优青材料评审"},
		SemanticOnly: true,
		Phases: []PhaseTemplate{
			{ID: "ey_review_completeness", Name: "完整性检测", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title:       "提供申报材料",
					Description: "请提供待评审的优青申请书材料。支持上传文件、粘贴文本、或指定本机文件路径。",
					Fields: []PhaseInputField{
						{Name: "material_path", Label: "申请书文件路径", Type: "file", Placeholder: "如：D:\\申报材料\\优青申请书.pdf"},
						{Name: "material_text", Label: "或粘贴申请书文本", Type: "textarea", Placeholder: "将申请书的主要内容粘贴到这里"},
						{Name: "focus_areas", Label: "重点关注方面（可选）", Type: "textarea", Placeholder: "如：发展潜力是否突出、研究方案可行性"},
					},
				},
			},
			{ID: "ey_review_achievements", Name: "学术成果评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "ey_review_plan", Name: "研究计划评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "ey_review_quality", Name: "撰写质量评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "ey_review_report", Name: "综合评估报告", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

// NSFCYouthReviewTemplate defines the review workflow for NSFC Youth Science Fund applications.
func NSFCYouthReviewTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:         "nsfc_youth_review",
		Name:         "青基申请书评审",
		Description:  "完整性检测 → 学术成果评估 → 研究计划评估 → 撰写质量评估 → 综合评估报告。适用于青年基金申请书材料的评审与改进建议。",
		Keywords:     []string{"青基评审", "青基审查", "青年基金评审"},
		SemanticOnly: true,
		Phases: []PhaseTemplate{
			{ID: "yf_review_completeness", Name: "完整性检测", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title:       "提供申报材料",
					Description: "请提供待评审的青基申请书材料。支持上传文件、粘贴文本、或指定本机文件路径。",
					Fields: []PhaseInputField{
						{Name: "material_path", Label: "申请书文件路径", Type: "file", Placeholder: "如：D:\\申报材料\\青基申请书.pdf"},
						{Name: "material_text", Label: "或粘贴申请书文本", Type: "textarea", Placeholder: "将申请书的主要内容粘贴到这里"},
						{Name: "focus_areas", Label: "重点关注方面（可选）", Type: "textarea", Placeholder: "如：立项依据是否充分、技术路线可行性"},
					},
				},
			},
			{ID: "yf_review_achievements", Name: "学术成果评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "yf_review_plan", Name: "研究计划评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "yf_review_quality", Name: "撰写质量评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "yf_review_report", Name: "综合评估报告", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

// NSFCGeneralReviewTemplate defines the review workflow for NSFC General Program applications.
func NSFCGeneralReviewTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:         "nsfc_general_review",
		Name:         "面上项目申请书评审",
		Description:  "完整性检测 → 学术成果评估 → 研究计划评估 → 撰写质量评估 → 综合评估报告。适用于面上项目申请书材料的评审与改进建议。",
		Keywords:     []string{"面上评审", "面上审查", "面上项目评审"},
		SemanticOnly: true,
		Phases: []PhaseTemplate{
			{ID: "gen_review_completeness", Name: "完整性检测", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title:       "提供申报材料",
					Description: "请提供待评审的面上项目申请书材料。支持上传文件、粘贴文本、或指定本机文件路径。",
					Fields: []PhaseInputField{
						{Name: "material_path", Label: "申请书文件路径", Type: "file", Placeholder: "如：D:\\申报材料\\面上项目申请书.pdf"},
						{Name: "material_text", Label: "或粘贴申请书文本", Type: "textarea", Placeholder: "将申请书的主要内容粘贴到这里"},
						{Name: "focus_areas", Label: "重点关注方面（可选）", Type: "textarea", Placeholder: "如：科学问题是否明确、创新点是否突出"},
					},
				},
			},
			{ID: "gen_review_achievements", Name: "学术成果评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "gen_review_plan", Name: "研究计划评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "gen_review_quality", Name: "撰写质量评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "gen_review_report", Name: "综合评估报告", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

// NSFCKeyReviewTemplate defines the review workflow for NSFC Key Program applications.
func NSFCKeyReviewTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:         "nsfc_key_review",
		Name:         "重点项目申请书评审",
		Description:  "完整性检测 → 学术成果评估 → 研究计划评估 → 撰写质量评估 → 综合评估报告。适用于重点项目申请书材料的评审与改进建议。",
		Keywords:     []string{"重点项目评审", "重点审查", "重点项目材料评审"},
		SemanticOnly: true,
		Phases: []PhaseTemplate{
			{ID: "key_review_completeness", Name: "完整性检测", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
				InputSchema: &PhaseInputSchema{
					Title:       "提供申报材料",
					Description: "请提供待评审的重点项目申请书材料。支持上传文件、粘贴文本、或指定本机文件路径。",
					Fields: []PhaseInputField{
						{Name: "material_path", Label: "申请书文件路径", Type: "file", Placeholder: "如：D:\\申报材料\\重点项目申请书.pdf"},
						{Name: "material_text", Label: "或粘贴申请书文本", Type: "textarea", Placeholder: "将申请书的主要内容粘贴到这里"},
						{Name: "focus_areas", Label: "重点关注方面（可选）", Type: "textarea", Placeholder: "如：战略意义是否明确、课题设置是否合理"},
					},
				},
			},
			{ID: "key_review_achievements", Name: "学术成果评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "key_review_plan", Name: "研究计划评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "key_review_quality", Name: "撰写质量评估", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: "key_review_report", Name: "综合评估报告", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	}
}

func PatentApplicationTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "patent_application",
		Name:        "中国专利申请",
		Description: "面向中国国家知识产权局（CNIPA）的专利申请文件准备流程：材料解析 → 查新/近似检索 → 权利要求或保护要点 → 附图/图片整理 → 说明书或简要说明 → 申请文件组装",
		Keywords:    []string{"专利申请", "专利撰写", "发明专利", "实用新型", "外观设计", "专利交底书", "权利要求书", "中国专利", "CNIPA", "patent application"},
		Phases: []PhaseTemplate{
			{ID: "pa_disclosure_parsing", Name: "申请材料解析", NeedsConfirm: true, ToolPolicy: ToolPolicyFull,
				InputSchema: &PhaseInputSchema{
					Title:       "专利申请信息",
					Description: "选择输入方式：提供交底书文件、手工输入技术内容，或提供外观设计图片/照片。",
					Fields: []PhaseInputField{
						{Name: "patent_type", Label: "专利类型", Type: "select", Required: true, Options: []PhaseInputOption{
							{Label: "发明专利", Value: "invention"},
							{Label: "实用新型", Value: "utility_model"},
							{Label: "外观设计", Value: "design"},
						}, Default: "invention"},
						{Name: "applicant", Label: "申请人（单位/个人）", Type: "text", Required: true, Placeholder: "如：XX科技有限公司"},
						{Name: "inventors", Label: "发明人/设计人", Type: "text", Required: true, Placeholder: "如：张三、李四（逗号分隔）"},
						{Name: "tech_field", Label: "技术领域/产品类别", Type: "text", Required: true, Placeholder: "如：人工智能、新能源电池、机械加工、智能音箱"},
						{Name: "output_dir", Label: "文档输出目录", Type: "directory", Placeholder: "选择文档输出目录（留空则保存到材料或图片同目录）"},
					},
					Variants: []PhaseInputVariant{
						{
							ID:    "file_mode",
							Label: "交底书/申请材料文件",
							Fields: []PhaseInputField{
								{Name: "disclosure_path", Label: "交底书/申请材料文件路径", Type: "file", Required: true, Placeholder: "选择交底书或申请材料文件（Word/PDF）"},
							},
						},
						{
							ID:    "manual_mode",
							Label: "手工输入",
							Fields: []PhaseInputField{
								{Name: "technical_problem", Label: "要解决的技术问题", Type: "textarea", Required: true, Placeholder: "描述现有技术的不足，以及本发明要解决的核心技术问题"},
								{Name: "technical_solution", Label: "技术方案", Type: "textarea", Required: true, Placeholder: "详细描述本发明的技术方案（结构、步骤、连接关系、参数等）"},
								{Name: "beneficial_effects", Label: "有益效果", Type: "textarea", Placeholder: "描述本发明相比现有技术的改进效果，尽量量化"},
								{Name: "figures_paths", Label: "附图文件路径（每行一个）", Type: "textarea", Placeholder: "如：\nD:\\专利\\图1-系统架构.png\nD:\\专利\\图2-流程图.png"},
								{Name: "figures_descriptions", Label: "附图说明（每行对应一张图）", Type: "textarea", Placeholder: "如：\n图1：系统整体架构示意图，展示各模块连接关系\n图2：数据处理流程图"},
								{Name: "prior_art", Label: "已知的现有技术/对比文件（可选）", Type: "textarea", Placeholder: "列出已知的相关专利号或文献，每行一个"},
							},
						},
						{
							ID:    "design_mode",
							Label: "外观设计图片或照片",
							Fields: []PhaseInputField{
								{Name: "design_product_name", Label: "产品名称", Type: "text", Required: true, Placeholder: "如：智能音箱、包装盒、显示屏幕面板"},
								{Name: "design_product_use", Label: "产品用途", Type: "textarea", Required: true, Placeholder: "简要说明使用该外观设计的产品用途"},
								{Name: "design_images_paths", Label: "外观设计图片或照片路径（每行一个）", Type: "textarea", Required: true, Placeholder: "如：\nD:\\专利\\主视图.png\nD:\\专利\\立体图.jpg"},
								{Name: "design_brief_description", Label: "简要说明", Type: "textarea", Required: true, Placeholder: "说明设计要点、最能表明设计要点的图片或照片、是否请求保护色彩等"},
							},
						},
					},
				},
			},
			{ID: "pa_prior_art_search", Name: "查新/近似检索分析", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
			{ID: "pa_claims_drafting", Name: "权利要求/保护要点", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
			{ID: "pa_figures_organization", Name: "附图/图片整理", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
			{ID: "pa_description_writing", Name: "说明书/简要说明", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
			{ID: "pa_document_assembly", Name: "申请文件组装与检查", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
		},
	}
}

// USPatentApplicationTemplate defines the US patent (USPTO) drafting workflow.
func USPatentApplicationTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        "us_patent_application",
		Name:        "US Patent (USPTO)",
		Description: "USPTO utility/provisional patent application workflow: Disclosure Analysis → Prior Art Search → Claims Drafting → Drawings → Specification Writing → Application Assembly",
		Keywords:    []string{"美国专利", "USPTO", "US patent", "utility patent", "provisional patent", "non-provisional", "patent claims", "specification", "美国专利申请"},
		Phases: []PhaseTemplate{
			{ID: "us_disclosure_analysis", Name: "Disclosure Analysis / 交底书解析", NeedsConfirm: true, ToolPolicy: ToolPolicyFull,
				InputSchema: &PhaseInputSchema{
					Title:       "US Patent Application Information",
					Description: "Provide invention disclosure (file or manual input). The disclosure can be in Chinese or English.",
					Fields: []PhaseInputField{
						{Name: "patent_type", Label: "Patent Type / 专利类型", Type: "select", Required: true, Options: []PhaseInputOption{
							{Label: "Utility Patent (实用专利)", Value: "utility"},
							{Label: "Provisional Application (临时申请)", Value: "provisional"},
						}, Default: "utility"},
						{Name: "applicant", Label: "Applicant / 申请人", Type: "text", Required: true, Placeholder: "e.g. Acme Corp. / XX科技有限公司"},
						{Name: "inventors", Label: "Inventor(s) / 发明人", Type: "text", Required: true, Placeholder: "e.g. John Smith, Jane Doe（comma separated）"},
						{Name: "tech_field", Label: "Technical Field / 技术领域", Type: "text", Required: true, Placeholder: "e.g. Machine Learning, Battery Technology"},
						{Name: "output_dir", Label: "Output Directory / 文档输出目录", Type: "directory", Placeholder: "Select output directory (leave empty for same directory as disclosure)"},
					},
					Variants: []PhaseInputVariant{
						{
							ID:    "file_mode",
							Label: "Disclosure File / 交底书文件",
							Fields: []PhaseInputField{
								{Name: "disclosure_path", Label: "Disclosure File Path / 交底书文件路径", Type: "file", Required: true, Placeholder: "Select disclosure file (Word/PDF, Chinese or English)"},
							},
						},
						{
							ID:    "manual_mode",
							Label: "Manual Input / 手工输入",
							Fields: []PhaseInputField{
								{Name: "technical_problem", Label: "Problem to be Solved / 要解决的技术问题", Type: "textarea", Required: true, Placeholder: "Describe the technical problem and limitations of prior art"},
								{Name: "technical_solution", Label: "Technical Solution / 技术方案", Type: "textarea", Required: true, Placeholder: "Describe the invention in detail (structure, steps, parameters)"},
								{Name: "beneficial_effects", Label: "Advantages / 有益效果", Type: "textarea", Placeholder: "Describe advantages over prior art, quantify where possible"},
								{Name: "figures_paths", Label: "Drawing file paths (one per line) / 附图路径", Type: "textarea", Placeholder: "e.g.\nD:\\Patent\\Fig1-Architecture.png\nD:\\Patent\\Fig2-Flowchart.png"},
								{Name: "figures_descriptions", Label: "Drawing descriptions / 附图说明", Type: "textarea", Placeholder: "e.g.\nFig.1: System architecture showing module connections\nFig.2: Data processing flowchart"},
								{Name: "prior_art", Label: "Known Prior Art (optional) / 已知现有技术", Type: "textarea", Placeholder: "List known patent numbers or publications, one per line"},
							},
						},
					},
				},
			},
			{ID: "us_prior_art_search", Name: "Prior Art Search / 查新检索", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
			{ID: "us_claims_drafting", Name: "Claims Drafting / 权利要求撰写", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
			{ID: "us_drawings", Name: "Drawings / 附图生成与整理", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
			{ID: "us_specification_writing", Name: "Specification Writing / 说明书撰写", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
			{ID: "us_application_assembly", Name: "Application Assembly / 申请文件组装", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
		},
	}
}

// RegisterBuiltinTemplates registers all built-in templates.
func RegisterBuiltinTemplates(r *TemplateRegistry) {
	if r == nil {
		return
	}
	r.Register(CodingTemplate())
	r.Register(CodingSubAgentTemplate())
	r.Register(RemoteCodingSubAgentTemplate())
	r.Register(MaintenanceTemplate())
	r.Register(OpsMaintenanceTemplate())
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
	r.Register(PaperReproductionTemplate())
	// Academic application templates — generated from parametric FundingProfiles.
	// Adding a new funding type only requires defining a FundingProfile in academic_profiles.go.
	r.Register(BuildAcademicApplicationTemplate(ChangjiangScholarProfile()))
	r.Register(BuildAcademicApplicationTemplate(NSFCDistinguishedYouthProfile()))
	r.Register(BuildAcademicApplicationTemplate(NSFCExcellentYouthProfile()))
	r.Register(BuildAcademicApplicationTemplate(NSFCYouthProfile()))
	r.Register(BuildAcademicApplicationTemplate(NSFCGeneralProfile()))
	r.Register(BuildAcademicApplicationTemplate(NSFCKeyProfile()))

	// Academic review templates (kept as-is — different structure, not parametrizable with application factory)
	r.Register(ChangjiangScholarReviewTemplate())
	r.Register(NSFCDistinguishedYouthReviewTemplate())
	r.Register(NSFCExcellentYouthReviewTemplate())
	r.Register(NSFCYouthReviewTemplate())
	r.Register(NSFCGeneralReviewTemplate())
	r.Register(NSFCKeyReviewTemplate())

	r.Register(PatentApplicationTemplate())
	r.Register(USPatentApplicationTemplate())
	r.Register(GaokaoApplicationTemplate())
}
