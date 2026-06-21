package v2

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Parametric Academic Application Template Factory
//
// Design principle: All academic funding application workflows share the same
// 5-phase skeleton. Differences between funding types (长江/杰青/优青/青基/面上/重点)
// are purely DATA-level — different form fields, different evaluation criteria,
// different prompt emphasis. Code-level duplication is eliminated.
//
// A FundingProfile describes WHAT varies. The factory function implements HOW
// to build a WorkflowTemplate from a profile. Adding a new funding type
// (e.g. 博新计划, 万人计划) requires only a new FundingProfile definition.
// ---------------------------------------------------------------------------

// FundingCategory classifies the funding type for prompt generation.
type FundingCategory string

const (
	// FundingTalent: evaluated on the PERSON's achievements/potential (长江/杰青/优青)
	FundingTalent FundingCategory = "talent"
	// FundingProject: evaluated on the RESEARCH QUESTION and methodology (青基/面上/重点)
	FundingProject FundingCategory = "project"
)

// PhaseEmphasis defines what each phase should focus on for a specific funding type.
// This is the core differentiator — same phase structure, different content strategy.
type PhaseEmphasis struct {
	// Phase 1: Information collection (form) — no emphasis needed, it's just data entry.

	// Phase 2: Academic foundation / research accumulation
	Phase2Focus string // e.g. "原创性成果和国际学术影响力" (杰青) vs "成长曲线和发展潜力" (优青)
	Phase2Title string // e.g. "研究工作基础与学术贡献" vs "研究积累与发展潜力"

	// Phase 3: Research plan / proposal
	Phase3Focus string // e.g. "聘期研究蓝图，体现引领性" (长江) vs "关键科学问题和技术路线" (面上)
	Phase3Title string // e.g. "聘期研究计划" vs "研究方案与技术路线"

	// Phase 4: Budget / team / cultivation
	Phase4Focus string // e.g. "人才培养与团队建设" (长江) vs "经费预算与年度计划" (面上)
	Phase4Title string // e.g. "人才培养与团队建设" vs "经费预算与年度计划"

	// Phase 5: Final assembly
	Phase5Title string // e.g. "推荐意见与申报书整合" vs "申请书整合与润色"
}

// FundingProfile is the complete parametric description of an academic funding type.
// One profile = one WorkflowTemplate. All structural logic is in the factory function.
type FundingProfile struct {
	// --- Identity ---
	Type         string   // workflow type (e.g. "nsfc_distinguished_youth")
	Name         string   // display name (e.g. "杰青申请书")
	Description  string   // routing/matching description
	Keywords     []string // intent matching keywords
	SemanticOnly bool     // if true, only activated via LLM classification

	// --- Funding characteristics ---
	Category    FundingCategory // talent vs project (affects prompt structure)
	AgeLimit    string          // e.g. "男性<45岁，女性<48岁" (empty = no age limit)
	FundingInfo string          // e.g. "400万/5年" (shown in form description)

	// --- Form customization (Phase 1) ---
	FormTitle       string            // form title (e.g. "杰青申请人基本信息")
	FormDescription string            // form description text
	ExtraFields     []PhaseInputField // funding-specific fields APPENDED after common fields
	OmitCommon      []string          // common field names to OMIT for this type

	// --- Phase emphasis (the core differentiator) ---
	Emphasis PhaseEmphasis

	// --- Review criteria (injected into all phase prompts) ---
	ReviewCriteria string // what evaluators look for (e.g. "评审重点：原创性、系统性、国际影响力")
}

// commonAcademicFields returns the shared personal/academic fields present in
// all academic application forms. Each field is marked Reusable:true for
// cross-workflow memory recall and sediment.
func commonAcademicFields(ageHint string) []PhaseInputField {
	birthPlaceholder := "如：1980年5月"
	if ageHint != "" {
		birthPlaceholder = fmt.Sprintf("如：1980年5月（%s）", ageHint)
	}
	return []PhaseInputField{
		{Name: "name", Label: "姓名", Type: "text", Required: true, Reusable: true},
		{Name: "gender", Label: "性别", Type: "select", Required: true, Reusable: true, Options: []PhaseInputOption{
			{Label: "男", Value: "男"}, {Label: "女", Value: "女"},
		}},
		{Name: "birth_date", Label: "出生日期", Type: "text", Required: true, Reusable: true, Placeholder: birthPlaceholder},
		{Name: "institution", Label: "依托单位", Type: "text", Required: true, Reusable: true, Placeholder: "如：XX大学 XX学院"},
		{Name: "title", Label: "职称", Type: "text", Required: true, Reusable: true, Placeholder: "如：教授/研究员"},
		{Name: "discipline", Label: "学科领域", Type: "text", Required: true, Reusable: true, Placeholder: "如：计算机科学与技术"},
		{Name: "research_direction", Label: "主要研究方向", Type: "textarea", Required: true, Reusable: true, Placeholder: "2-3个核心研究方向"},
		{Name: "h_index", Label: "H指数", Type: "text", Reusable: true, Placeholder: "如：35"},
		{Name: "total_papers", Label: "SCI/SSCI论文总数", Type: "text", Reusable: true, Placeholder: "如：120"},
		{Name: "education", Label: "教育背景", Type: "textarea", Reusable: true, Placeholder: "按时间顺序列出：\n本科→硕士→博士"},
	}
}

// BuildAcademicApplicationTemplate generates a complete WorkflowTemplate from a FundingProfile.
// This is the SINGLE implementation of the academic application workflow structure.
func BuildAcademicApplicationTemplate(p FundingProfile) *WorkflowTemplate {
	// --- Build Phase 1 form fields ---
	common := commonAcademicFields(p.AgeLimit)

	// Filter out omitted fields
	omitSet := make(map[string]bool, len(p.OmitCommon))
	for _, name := range p.OmitCommon {
		omitSet[name] = true
	}
	var fields []PhaseInputField
	for _, f := range common {
		if !omitSet[f.Name] {
			fields = append(fields, f)
		}
	}

	// Append funding-specific extra fields
	fields = append(fields, p.ExtraFields...)

	// Build form description
	formDesc := p.FormDescription
	if formDesc == "" {
		formDesc = "请填写申请人基本信息。"
		if p.FundingInfo != "" {
			formDesc += fmt.Sprintf("（%s）", p.FundingInfo)
		}
		formDesc += "\n💡 支持上传简历/CV自动填充：上传后系统自动提取信息，您只需微调后提交。"
	}

	// --- Build phase ID prefix from type (e.g. "nsfc_distinguished_youth" → "dy") ---
	prefix := inferPhasePrefix(p.Type)

	// --- Assemble 5 phases ---
	phases := []PhaseTemplate{
		// Phase 1: Information collection (form)
		{
			ID: prefix + "_profile", Name: "申请人基本信息采集",
			NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
			InputSchema: &PhaseInputSchema{
				Title:         p.FormTitle,
				Description:   formDesc,
				AcceptsResume: true,
				Fields:        fields,
			},
		},
		// Phase 2: Academic foundation
		{
			ID: prefix + "_foundation", Name: p.Emphasis.Phase2Title,
			NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
		},
		// Phase 3: Research plan
		{
			ID: prefix + "_plan", Name: p.Emphasis.Phase3Title,
			NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
		},
		// Phase 4: Budget / team
		{
			ID: prefix + "_phase4", Name: p.Emphasis.Phase4Title,
			NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
		},
		// Phase 5: Final assembly
		{
			ID: prefix + "_assembly", Name: p.Emphasis.Phase5Title,
			NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
		},
	}

	return &WorkflowTemplate{
		Type:         p.Type,
		Name:         p.Name,
		Description:  p.Description,
		Keywords:     p.Keywords,
		SemanticOnly: p.SemanticOnly,
		Phases:       phases,
	}
}

// AcademicPhaseInstruction generates the phase-specific prompt instruction
// for an academic application workflow phase, parametrized by FundingProfile.
//
// This replaces the hardcoded switch cases in phaseInstruction() for academic phases.
// Called by phaseInstruction() when the phaseID matches an academic pattern.
func AcademicPhaseInstruction(phaseID string, profile *FundingProfile) string {
	if profile == nil {
		return ""
	}
	prefix := inferPhasePrefix(profile.Type)

	switch {
	case phaseID == prefix+"_profile":
		return academicPhase1Instruction(profile)
	case phaseID == prefix+"_foundation":
		return academicPhase2Instruction(profile)
	case phaseID == prefix+"_plan":
		return academicPhase3Instruction(profile)
	case phaseID == prefix+"_phase4":
		return academicPhase4Instruction(profile)
	case phaseID == prefix+"_assembly":
		return academicPhase5Instruction(profile)
	}
	return ""
}

// --- Phase instruction generators (parametrized by profile) ---

func academicPhase1Instruction(p *FundingProfile) string {
	var sb strings.Builder
	sb.WriteString("## 阶段指令\n\n")
	sb.WriteString(fmt.Sprintf("基于用户提交的表单信息，生成「%s」的申请人资质梳理文档。\n\n", p.Name))

	if p.AgeLimit != "" {
		sb.WriteString(fmt.Sprintf("**年龄要求**：%s\n\n", p.AgeLimit))
	}
	if p.ReviewCriteria != "" {
		sb.WriteString(fmt.Sprintf("**评审重点**：%s\n\n", p.ReviewCriteria))
	}

	sb.WriteString("生成文档内容：\n")
	sb.WriteString("1. **基本信息表**：个人信息、学历经历\n")
	sb.WriteString("2. **申报条件对照**：逐项对照申报条件，标注满足/不满足/待确认\n")
	sb.WriteString("3. **学科竞争态势分析**\n")
	sb.WriteString("4. **资质评估结论与建议**\n\n")
	sb.WriteString(academicConstraints())
	return sb.String()
}

func academicPhase2Instruction(p *FundingProfile) string {
	var sb strings.Builder
	sb.WriteString("## 阶段指令\n\n")
	sb.WriteString(fmt.Sprintf("撰写「%s」的%s部分。\n\n", p.Name, p.Emphasis.Phase2Title))
	sb.WriteString(fmt.Sprintf("**本阶段侧重点**：%s\n\n", p.Emphasis.Phase2Focus))
	if p.ReviewCriteria != "" {
		sb.WriteString(fmt.Sprintf("**%s**\n\n", p.ReviewCriteria))
	}

	if p.Category == FundingTalent {
		sb.WriteString(`生成文档内容：
1. **研究方向凝练**：2-3 个主要方向，每个用一句话概括核心贡献
2. **代表性成果**（按方向分类）：
   - 代表性论文（10篇以内）：标题、期刊、引用、贡献
   - 代表性项目：名称、来源、经费、角色
3. **学术影响力**：H-index、引用、高被引论文、学术兼职、受邀报告
4. **获奖情况**：省部级以上奖励

**写作要点**：
- 突出"原创性"和"系统性"
- 每个成果要说清"解决了什么科学问题"
- 体现研究的连贯性和递进关系
`)
	} else {
		sb.WriteString(`生成文档内容：
1. **研究基础**：与本项目直接相关的前期工作
2. **已有成果**：相关论文、专利、软件著作权
3. **工作条件**：实验平台、计算资源、数据资源
4. **团队基础**：主要参与人员及分工

**写作要点**：
- 证明你有能力完成本项目
- 前期工作要与拟研究内容紧密关联
- 工作条件要说明能支撑本项目实施
`)
	}
	sb.WriteString("\n")
	sb.WriteString(academicConstraints())
	return sb.String()
}

func academicPhase3Instruction(p *FundingProfile) string {
	var sb strings.Builder
	sb.WriteString("## 阶段指令\n\n")
	sb.WriteString(fmt.Sprintf("撰写「%s」的%s部分。\n\n", p.Name, p.Emphasis.Phase3Title))
	sb.WriteString(fmt.Sprintf("**本阶段侧重点**：%s\n\n", p.Emphasis.Phase3Focus))
	if p.ReviewCriteria != "" {
		sb.WriteString(fmt.Sprintf("**%s**\n\n", p.ReviewCriteria))
	}

	if p.Category == FundingTalent {
		sb.WriteString(`生成文档内容：
1. **研究背景与意义**：国际前沿、关键科学问题、突破口
2. **研究目标**：总体目标 + 3-5 个具体目标（可量化）
3. **研究内容**：3-4 个相互关联的研究方向
4. **创新点**：3-4 个明确的创新之处
5. **年度安排**：逐年里程碑计划
6. **预期成果**：论文、项目、人才培养、成果转化

**写作要点**：
- 体现"引领性"而非"跟踪性"
- 目标有挑战性但可实现
- 内容之间有内在逻辑联系
- 与前一阶段形成"过去→未来"的连贯叙事
`)
	} else {
		sb.WriteString(`生成文档内容：
1. **研究目标**：总体目标和具体目标
2. **研究内容**：拟解决的关键科学问题及研究内容
3. **拟采取的研究方案**：技术路线和方法
4. **可行性分析**：为什么这个方案能成功
5. **项目特色与创新之处**
6. **年度计划与预期结果**

**写作要点**：
- 科学问题要明确、聚焦
- 技术路线要具体、可操作
- 创新点不宜过多（2-3个即可），但每个要说透
- 方案要有针对性地解决提出的科学问题
`)
	}
	sb.WriteString("\n")
	sb.WriteString(academicConstraints())
	return sb.String()
}

func academicPhase4Instruction(p *FundingProfile) string {
	var sb strings.Builder
	sb.WriteString("## 阶段指令\n\n")
	sb.WriteString(fmt.Sprintf("撰写「%s」的%s部分。\n\n", p.Name, p.Emphasis.Phase4Title))
	sb.WriteString(fmt.Sprintf("**本阶段侧重点**：%s\n\n", p.Emphasis.Phase4Focus))

	if p.Category == FundingTalent {
		sb.WriteString(`生成文档内容：
1. **已培养人才**：已毕业硕博人数、代表性学生去向和成果
2. **在读学生**：当前指导的研究生概况
3. **聘期培养计划**：计划招生数量、培养模式、质量目标
4. **团队建设**：现有团队结构、拟引进人才、梯队规划
5. **教学工作**：承担课程、教材编写、教学改革

**写作要点**：
- 用数据说话（已培养X名博士，其中N人获奖）
- 培养计划要与研究方向紧密结合
- 团队建设要有梯度和互补性
`)
	} else {
		sb.WriteString(`生成文档内容：
1. **经费预算**：
   - 设备费、材料费、测试化验费、差旅费、会议费
   - 劳务费、专家咨询费、间接费用
   - 按年度分解预算
2. **预算说明**：各项费用的用途和合理性论证
3. **年度计划**：逐年的研究安排和阶段目标

**写作要点**：
- 预算要与研究方案匹配（做计算的不要列大量试剂费）
- 单项费用不宜过于集中
- 年度计划要有递进逻辑，不要前松后紧
`)
	}
	sb.WriteString("\n")
	sb.WriteString(academicConstraints())
	return sb.String()
}

func academicPhase5Instruction(p *FundingProfile) string {
	var sb strings.Builder
	sb.WriteString("## 阶段指令\n\n")
	sb.WriteString(fmt.Sprintf("整合前序所有阶段产出物，生成完整的「%s」终稿。\n\n", p.Name))
	sb.WriteString(`整合要求：
1. 统一术语、格式、编号
2. 确保各部分之间逻辑连贯
3. 检查数据一致性（如引用数、论文数等在各处保持一致）
4. 润色语言表达（学术规范、简洁有力）
5. 补全可能遗漏的常规内容（如诚信声明、签名栏等）

`)
	sb.WriteString(academicConstraints())
	return sb.String()
}

func academicConstraints() string {
	return `## 重要约束（违反将导致错误）
- 只生成一份文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
- 你只负责输出文档本身，系统会自动提示用户确认。
`
}

// --- Utilities ---

// inferPhasePrefix generates a short phase ID prefix from the workflow type.
// e.g. "nsfc_distinguished_youth" → "dy", "changjiang_scholar" → "cj", "nsfc_youth" → "yf"
//
// All known academic types MUST have an explicit case in the switch — the default
// branch is a last resort for truly unknown types and should never be reached for
// registered academic profiles. If you add a new profile, add its prefix here.
func inferPhasePrefix(workflowType string) string {
	switch workflowType {
	case "changjiang_scholar":
		return "cj"
	case "nsfc_distinguished_youth":
		return "dy"
	case "nsfc_excellent_youth":
		return "ey"
	case "nsfc_youth":
		return "yf"
	case "nsfc_general":
		return "gp"
	case "nsfc_key":
		return "kp"
	default:
		// Fallback for unregistered types — generate from initials.
		// WARNING: if you get here for a registered academic profile, add an explicit
		// case above to avoid potential prefix collisions.
		parts := strings.Split(workflowType, "_")
		if len(parts) >= 2 {
			p1 := parts[len(parts)-2]
			p2 := parts[len(parts)-1]
			if len(p1) > 0 && len(p2) > 0 {
				return string(p1[0]) + string(p2[0])
			}
		}
		return "ac"
	}
}
