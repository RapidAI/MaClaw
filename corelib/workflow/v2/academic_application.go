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
		{Name: "birth_date", Label: "出生日期", Type: "date", Required: true, Reusable: true, Placeholder: birthPlaceholder},
		{Name: "institution", Label: "依托单位", Type: "text", Required: true, Reusable: true, Placeholder: "如：XX大学 XX学院"},
		{Name: "title", Label: "职称", Type: "text", Required: true, Reusable: true, Placeholder: "如：教授/研究员"},
		{Name: "discipline", Label: "学科领域", Type: "text", Required: true, Reusable: true, Placeholder: "如：计算机科学与技术"},
		{Name: "research_direction", Label: "主要研究方向", Type: "textarea", Required: true, Reusable: true, Placeholder: "2-3个核心研究方向"},
		{Name: "google_scholar_url", Label: "Google Scholar 主页", Type: "text", Reusable: true, Placeholder: "如：https://scholar.google.com/citations?user=XXXXX"},
		{Name: "orcid_url", Label: "ORCID 主页", Type: "text", Reusable: true, Placeholder: "如：https://orcid.org/0000-0002-XXXX-XXXX"},
		{Name: "dblp_url", Label: "DBLP 主页（计算机类）", Type: "text", Reusable: true, Placeholder: "如：https://dblp.org/pid/xx/xxxx.html"},
		{Name: "h_index", Label: "H指数", Type: "text", Reusable: true, Placeholder: "如：35"},
		{Name: "total_papers", Label: "SCI/SSCI论文总数", Type: "text", Reusable: true, Placeholder: "如：120"},
		{Name: "education", Label: "教育背景", Type: "textarea", Reusable: true, Placeholder: "按时间顺序列出：\n本科→硕士→博士"},
	}
}

// BuildAcademicApplicationTemplate generates a complete WorkflowTemplate from a FundingProfile.
// This is the SINGLE implementation of the academic application workflow structure.
func BuildAcademicApplicationTemplate(p FundingProfile) *WorkflowTemplate {
	// --- Build Phase 1 form fields for manual mode ---
	common := commonAcademicFields(p.AgeLimit)

	// Filter out omitted fields
	omitSet := make(map[string]bool, len(p.OmitCommon))
	for _, name := range p.OmitCommon {
		omitSet[name] = true
	}
	var manualFields []PhaseInputField
	for _, f := range common {
		if !omitSet[f.Name] {
			manualFields = append(manualFields, f)
		}
	}

	// Append funding-specific extra fields
	manualFields = append(manualFields, p.ExtraFields...)

	// Build form description
	formDesc := p.FormDescription
	if formDesc == "" {
		formDesc = "请选择信息输入方式：上传简历/CV由系统自动提取，或手动逐项填写。"
		if p.FundingInfo != "" {
			formDesc += fmt.Sprintf("\n资助额度：%s", p.FundingInfo)
		}
	}

	// --- Build phase ID prefix from type (e.g. "nsfc_distinguished_youth" → "dy") ---
	prefix := inferPhasePrefix(p.Type)

	// --- Assemble 5 phases ---
	phases := []PhaseTemplate{
		// Phase 1: Information collection (form with two mutually exclusive input modes)
		{
			ID: prefix + "_profile", Name: "申请人基本信息采集",
			NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly,
			InputSchema: &PhaseInputSchema{
				Title:         p.FormTitle,
				Description:   formDesc,
				AcceptsResume: true,
				AcceptsSupplementary: &SupplementaryDocConfig{
					Label:         "研究方向相关材料（可选）",
					Description:   "可上传研究计划初稿、代表性论文列表、课题组简介、获奖证书扫描件等，系统将在后续阶段参考这些材料生成更精准的内容。支持 PDF、Word、PowerPoint、Excel、Markdown、TXT 格式，可上传 0~5 份。",
					MaxFiles:      5,
					AcceptedTypes: []string{".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".md", ".txt"},
				},
				// Common fields shared across both variants (always visible)
				Fields: []PhaseInputField{},
				// Two mutually exclusive input modes
				Variants: []PhaseInputVariant{
					{
						ID:    "resume_mode",
						Label: "上传简历/CV（自动提取填充）",
						Fields: []PhaseInputField{
							{Name: "resume_file", Label: "简历文件", Type: "file", Required: true,
								Description: "支持 PDF、Word、PowerPoint、Excel、Markdown、TXT 格式的简历或 CV",
								Placeholder: "选择简历文件"},
						},
					},
					{
						ID:     "manual_mode",
						Label:  "手动填写",
						Fields: manualFields,
					},
				},
			},
		},
		// Phase 2: Academic foundation — ToolPolicyFull to allow web_fetch for
		// Google Scholar / ORCID / DBLP data collection when URLs are provided.
		{
			ID: prefix + "_foundation", Name: p.Emphasis.Phase2Title,
			NeedsConfirm: true, ToolPolicy: ToolPolicyFull,
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

	// Data collection guidance — always present for Phase 2
	sb.WriteString(`## 第零步：数据收集（必须先执行，再写文档）

在生成文档之前，必须先从用户提供的学术主页URL中收集真实数据。

**执行步骤**：
1. 检查用户表单中是否提供了以下URL：google_scholar_url、orcid_url、dblp_url
2. 对于每个非空的URL，使用 web_fetch 工具抓取页面内容
3. 从抓取结果中提取：论文列表（标题、期刊、年份、引用数）、H-index、总引用数、合作者信息
4. 如果 web_fetch 失败（网络问题、页面变化），在文档中标注「[数据源不可用：URL]，以下为用户手动提供的数据」
5. 如果用户没有提供任何URL且简历中没有具体论文列表，对具体成果使用占位符

**重要**：
- 只有从URL实际抓取到的数据才能作为"已验证"信息写入文档
- web_fetch 失败时不要编造替代数据
- Google Scholar 的引用数可能与简历中不同，以 Google Scholar 为准（注明查询日期）

`)

	if p.Category == FundingTalent {
		sb.WriteString(`生成文档内容：
1. **研究方向凝练**：2-3 个主要方向，每个用一句话概括核心贡献
2. **代表性成果**（按方向分类）：
   - 代表性论文（10篇以内）：标题、期刊、引用、贡献
   - 代表性项目：名称、来源、经费、角色
3. **学术影响力**：H-index、引用、高被引论文、学术兼职、受邀报告
4. **获奖情况**：省部级以上奖励

**数据来源规则**：
- H-index、论文总数等数值：优先使用从 Google Scholar 抓取的实时数据，其次使用表单填写的数字
- 研究方向：基于用户表单中填写的"主要研究方向"展开
- 具体论文列表：优先使用从 Google Scholar/ORCID/DBLP 抓取的真实论文数据；其次使用简历中提供的列表；均无时使用占位符「[待补充：...]」
- 具体项目/奖项：只有用户在简历或表单中明确提供才能写入；未提供时使用占位符
- 教育背景：使用用户表单中的"教育背景"字段内容

**写作要点**：
- 突出"原创性"和"系统性"
- 每个成果要说清"解决了什么科学问题"
- 体现研究的连贯性和递进关系
- 宁可留占位符也不能编造具体论文标题或项目编号
`)
	} else {
		sb.WriteString(`生成文档内容：
1. **研究基础**：与本项目直接相关的前期工作
2. **已有成果**：相关论文、专利、软件著作权
3. **工作条件**：实验平台、计算资源、数据资源
4. **团队基础**：主要参与人员及分工

**数据来源规则**：
- 优先使用从 Google Scholar/ORCID/DBLP 抓取的真实数据（如果第零步成功抓取）
- 其次使用用户在表单和前序阶段中提供的信息
- 具体成果（论文、专利）：未从任何来源获取到具体列表时使用占位符「[待补充：...]」
- 工作条件：可基于用户所在单位做合理推断，但不能编造具体设备型号或数据库名称

**写作要点**：
- 证明你有能力完成本项目
- 前期工作要与拟研究内容紧密关联
- 工作条件要说明能支撑本项目实施
- 宁可留占位符也不能编造不存在的专利号或论文标题
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

**数据来源规则**：
- "已培养人才"中的具体数字和学生信息：只使用用户在简历或前序阶段中提供的数据
- 未提供具体人数时使用占位符「[待补充：已毕业硕士X人/博士X人]」
- 培养计划和团队建设可以是规划性内容（属于AI可生成的框架）
- 具体学生姓名、去向：未提供时不列出，用"代表性毕业生去向包括[待补充]"

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
	return `## 重要约束（违反将导致严重错误）

### 内容真实性约束（最高优先级）
- 【严禁幻觉】绝对禁止编造任何具体事实：论文标题、期刊名、引用数、项目编号、经费金额、获奖名称、年份等。
- 【严禁杜撰】不得编造不存在的学术成果、数据或引用来填充文档。
- 【信息来源】所有具体事实必须来自以下来源之一：
  1. 用户通过表单提交的信息（上方"用户提供的结构化信息"section）
  2. 用户上传的简历/CV 中提取的内容
  3. 从用户提供的学术主页 URL（Google Scholar / ORCID / DBLP）通过 web_fetch 抓取的真实数据
  4. 前序阶段产出物中已确认的内容
- 【数据不足时的处理】：对于没有数据来源的具体信息，使用占位符格式：
  - 论文：「[待补充：代表性论文1 - 标题/期刊/年份/引用数]」
  - 项目：「[待补充：主持项目1 - 名称/来源/经费/年份]」
  - 获奖：「[待补充：奖励1 - 名称/等级/年份]」
  - 数据：「[待补充：具体数值]」
- 【允许生成的内容】：文档结构、章节标题、写作框架、通用表述（不含具体事实的描述性文字）、研究计划（规划性内容）可以由 AI 生成。
- 【论文信息规则】：除非从 Google Scholar/ORCID 抓取到了具体列表或用户提供了论文列表，否则只能写"申请人在XX领域发表SCI论文N篇"（N来自表单填写的total_papers），不能列出具体论文标题。

### 格式约束
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
