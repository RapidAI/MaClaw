package v2

// academic_profiles.go — All academic funding type definitions.
//
// Each FundingProfile is a pure data declaration. Adding a new funding type
// (e.g. 博新计划, 万人计划, 教育部新世纪人才) requires ONLY adding a new
// Profile function here + registering it. No other code changes needed.

// ---------------------------------------------------------------------------
// Profile: 长江学者 (Changjiang Scholar)
// ---------------------------------------------------------------------------

func ChangjiangScholarProfile() FundingProfile {
	return FundingProfile{
		Type:        "changjiang_scholar",
		Name:        "长江学者申请书",
		Description: "个人资质梳理 → 学术成就总结 → 聘期研究计划 → 人才培养与团队建设 → 推荐意见与申报书整合。适用于长江学者特聘教授、讲座教授、青年学者等各层次申报。Changjiang Scholar application.",
		Keywords:    []string{"长江学者", "长江学者申报", "长江学者申请书", "长江特聘", "长江讲座", "长江青年", "Changjiang Scholar", "changjiang"},
		Category:    FundingTalent,
		AgeLimit:    "特聘教授无年龄限制，青年学者要求38周岁以下",
		FormTitle:   "长江学者申请人基本信息",
		ExtraFields: []PhaseInputField{
			{Name: "category", Label: "申报类别", Type: "select", Required: true, Options: []PhaseInputOption{
				{Label: "特聘教授", Value: "特聘教授"},
				{Label: "讲座教授", Value: "讲座教授"},
				{Label: "青年学者", Value: "青年学者"},
			}},
			{Name: "key_achievements", Label: "主要学术亮点（3-5项）", Type: "textarea", Reusable: true, Placeholder: "列出最突出的成果，如高影响力论文、国家项目、重要奖项"},
		},
		Emphasis: PhaseEmphasis{
			Phase2Focus: "学术影响力、国际声誉、学科引领能力",
			Phase2Title: "学术成就与代表性成果",
			Phase3Focus: "聘期5年研究蓝图，体现引领性和前瞻性",
			Phase3Title: "聘期研究计划",
			Phase4Focus: "研究生培养实效、团队梯队建设、学科贡献",
			Phase4Title: "人才培养与团队建设",
			Phase5Title: "推荐意见与申报书整合",
		},
		ReviewCriteria: "评审重点：学术水平和国际影响力、研究方向的前沿性、人才培养成效",
	}
}

// ---------------------------------------------------------------------------
// Profile: 杰青 (NSFC Distinguished Youth)
// ---------------------------------------------------------------------------

func NSFCDistinguishedYouthProfile() FundingProfile {
	return FundingProfile{
		Type:        "nsfc_distinguished_youth",
		Name:        "杰青申请书",
		Description: "申请人资质评估 → 研究工作基础与学术贡献 → 研究方案与创新点 → 预期成果与经费预算 → 申请书整合与润色。适用于国家杰出青年科学基金（杰青）申请。NSFC Distinguished Young Scholars Fund application.",
		Keywords:    []string{"杰青", "杰青申请", "杰出青年", "国家杰青", "杰青基金", "NSFC杰青", "distinguished youth", "杰出青年基金"},
		Category:    FundingTalent,
		AgeLimit:    "男性<45岁，女性<48岁",
		FundingInfo: "400万/5年",
		FormTitle:   "杰青申请人基本信息",
		ExtraFields: []PhaseInputField{
			{Name: "nationality", Label: "国籍", Type: "text", Required: true, Reusable: true, Default: "中国"},
			{Name: "degree", Label: "最高学位", Type: "select", Required: true, Reusable: true, Options: []PhaseInputOption{
				{Label: "博士", Value: "博士"}, {Label: "硕士", Value: "硕士"},
			}},
			{Name: "discipline_code", Label: "申报学科代码", Type: "text", Reusable: true, Placeholder: "如：F06 人工智能"},
			{Name: "prior_nsfc", Label: "已获NSFC资助情况", Type: "textarea", Reusable: true, Placeholder: "如：\n面上项目 2020-2023\n优青 2021-2024"},
			{Name: "total_citations", Label: "总引用数", Type: "text", Reusable: true, Placeholder: "如：8500"},
			{Name: "representative_papers", Label: "代表性顶刊论文数", Type: "text", Reusable: true, Placeholder: "如：5篇Nature子刊"},
			{Name: "awards", Label: "主要获奖", Type: "textarea", Reusable: true, Placeholder: "如：\n国家自然科学二等奖 2022\n省部级一等奖 2020"},
		},
		Emphasis: PhaseEmphasis{
			Phase2Focus: "原创性成果的系统性和国际学术影响力",
			Phase2Title: "研究工作基础与学术贡献",
			Phase3Focus: "明确的学术方向、原创性研究方案、重大创新点",
			Phase3Title: "研究方案与创新点",
			Phase4Focus: "预期突破性成果、经费使用计划",
			Phase4Title: "预期成果与经费预算",
			Phase5Title: "申请书整合与润色",
		},
		ReviewCriteria: "评审重点：已有成果的原创性和系统性、国际影响力（引用/受邀报告/学术兼职）、未来研究方向的前沿性",
	}
}

// ---------------------------------------------------------------------------
// Profile: 优青 (NSFC Excellent Youth)
// ---------------------------------------------------------------------------

func NSFCExcellentYouthProfile() FundingProfile {
	return FundingProfile{
		Type:        "nsfc_excellent_youth",
		Name:        "优青申请书",
		Description: "申请人资质评估 → 研究积累与发展潜力 → 研究方案与关键科学问题 → 预期成果与经费预算 → 申请书整合与润色。适用于国家优秀青年科学基金（优青）申请。NSFC Excellent Young Scientists Fund application.",
		Keywords:    []string{"优青", "优青申请", "优秀青年", "国家优青", "优青基金", "NSFC优青", "excellent young"},
		Category:    FundingTalent,
		AgeLimit:    "男性<38岁，女性<40岁",
		FundingInfo: "200万/3年",
		FormTitle:   "优青申请人基本信息",
		ExtraFields: []PhaseInputField{
			{Name: "nationality", Label: "国籍", Type: "text", Required: true, Reusable: true, Default: "中国"},
			{Name: "degree", Label: "最高学位", Type: "select", Required: true, Reusable: true, Options: []PhaseInputOption{
				{Label: "博士", Value: "博士"}, {Label: "硕士", Value: "硕士"},
			}},
			{Name: "phd_year", Label: "博士毕业年份", Type: "text", Reusable: true, Placeholder: "如：2015"},
			{Name: "discipline_code", Label: "申报学科代码", Type: "text", Reusable: true, Placeholder: "如：F06 人工智能"},
			{Name: "prior_nsfc", Label: "已获NSFC资助情况", Type: "textarea", Reusable: true, Placeholder: "如：\n青年基金 2018-2020"},
			{Name: "representative_work", Label: "最有代表性的工作（1-2项）", Type: "textarea", Reusable: true, Placeholder: "简述你最引以为傲的研究成果及其影响"},
		},
		Emphasis: PhaseEmphasis{
			Phase2Focus: "成长曲线、研究活力、发展潜力（而非积累量）",
			Phase2Title: "研究积累与发展潜力",
			Phase3Focus: "未来突破方向、关键科学问题、研究路线图",
			Phase3Title: "研究方案与关键科学问题",
			Phase4Focus: "预期成果、经费使用计划",
			Phase4Title: "预期成果与经费预算",
			Phase5Title: "申请书整合与润色",
		},
		ReviewCriteria: "评审重点：近5年的成长势头和爆发力、研究方向的独立性和前沿性、发展潜力（而非仅看论文数量）",
	}
}

// ---------------------------------------------------------------------------
// Profile: 青基 (NSFC Youth Fund)
// ---------------------------------------------------------------------------

func NSFCYouthProfile() FundingProfile {
	return FundingProfile{
		Type:        "nsfc_youth",
		Name:        "国自然青年基金申请书",
		Description: "立项依据与研究内容 → 研究基础与可行性 → 研究方案与技术路线 → 经费预算 → 申请书整合。适用于国家自然科学基金青年科学基金项目（青基）申请。NSFC Youth Science Fund application.",
		Keywords:    []string{"青基", "青年基金", "青年科学基金", "国自然青年", "NSFC青年", "youth fund", "青年项目"},
		Category:    FundingProject,
		AgeLimit:    "男性<35岁，女性<38岁",
		FundingInfo: "30万/3年",
		FormTitle:   "青基项目基本信息",
		ExtraFields: []PhaseInputField{
			{Name: "phd_year", Label: "博士毕业年份", Type: "text", Required: true, Reusable: true, Placeholder: "如：2020"},
			{Name: "discipline_code", Label: "申报学科代码", Type: "text", Required: true, Reusable: true, Placeholder: "如：F0601 机器学习"},
			{Name: "project_title", Label: "项目名称", Type: "text", Required: true, Placeholder: "拟申报项目的题目"},
			{Name: "core_question", Label: "拟解决的科学问题", Type: "textarea", Required: true, Placeholder: "用1-2句话描述"},
			{Name: "prior_work", Label: "前期研究基础", Type: "textarea", Placeholder: "博士期间和博后期间与本项目相关的工作"},
		},
		Emphasis: PhaseEmphasis{
			Phase2Focus: "与本项目直接相关的前期工作和可行性证据",
			Phase2Title: "研究基础与可行性",
			Phase3Focus: "清晰的技术路线、具体的研究方案、可操作性",
			Phase3Title: "研究方案与技术路线",
			Phase4Focus: "合理的经费预算",
			Phase4Title: "经费预算",
			Phase5Title: "申请书整合与润色",
		},
		ReviewCriteria: "评审重点：科学问题是否有意义、研究思路是否清晰、技术路线是否可行、申请人是否有潜力",
	}
}

// ---------------------------------------------------------------------------
// Profile: 面上 (NSFC General Program)
// ---------------------------------------------------------------------------

func NSFCGeneralProfile() FundingProfile {
	return FundingProfile{
		Type:        "nsfc_general",
		Name:        "国自然面上项目申请书",
		Description: "立项依据与研究内容 → 研究基础与工作条件 → 研究方案与技术路线 → 经费预算与年度计划 → 申请书整合。适用于国家自然科学基金面上项目申请。NSFC General Program application.",
		Keywords:    []string{"面上项目", "国自然面上", "面上", "NSFC面上", "国自然申请", "自然基金", "general program"},
		Category:    FundingProject,
		FundingInfo: "50-80万/4年",
		FormTitle:   "面上项目基本信息",
		OmitCommon:  []string{"birth_date", "education"}, // 面上不审年龄和教育背景
		ExtraFields: []PhaseInputField{
			{Name: "discipline_code", Label: "申报学科代码", Type: "text", Required: true, Reusable: true, Placeholder: "如：F0601 机器学习"},
			{Name: "project_title", Label: "项目名称", Type: "text", Required: true, Placeholder: "拟申报项目的题目"},
			{Name: "core_question", Label: "拟解决的核心科学问题", Type: "textarea", Required: true, Placeholder: "用1-2句话描述本项目要解决什么科学问题"},
			{Name: "prior_work", Label: "前期研究基础", Type: "textarea", Placeholder: "简述与本项目相关的已有工作"},
			{Name: "funding_amount", Label: "申请经费（万元）", Type: "text", Placeholder: "如：58"},
			{Name: "duration", Label: "资助期限", Type: "text", Default: "4年", Placeholder: "如：4年（2027.01-2030.12）"},
		},
		Emphasis: PhaseEmphasis{
			Phase2Focus: "与本项目直接相关的工作基础和实验条件",
			Phase2Title: "研究基础与工作条件",
			Phase3Focus: "针对一个明确的科学问题，提出系统的研究方案和技术路线",
			Phase3Title: "研究方案与技术路线",
			Phase4Focus: "合理的经费预算和年度计划安排",
			Phase4Title: "经费预算与年度计划",
			Phase5Title: "申请书整合与润色",
		},
		ReviewCriteria: "评审重点：科学问题是否明确且有创新性、研究方案是否系统且可行、申请人是否有相关研究基础",
	}
}

// ---------------------------------------------------------------------------
// Profile: 重点项目 (NSFC Key Program)
// ---------------------------------------------------------------------------

func NSFCKeyProfile() FundingProfile {
	return FundingProfile{
		Type:        "nsfc_key",
		Name:        "国自然重点项目申请书",
		Description: "战略需求与科学问题凝练 → 研究团队与工作基础 → 研究方案与课题设置 → 经费预算与管理计划 → 申请书整合。适用于国家自然科学基金重点项目申请。NSFC Key Program application.",
		Keywords:    []string{"重点项目", "国自然重点", "NSFC重点", "重点基金", "key program"},
		Category:    FundingProject,
		FundingInfo: "250-350万/5年",
		FormTitle:   "重点项目基本信息",
		OmitCommon:  []string{"birth_date", "education", "h_index", "total_papers"}, // 重点项目审项目不审个人指标
		ExtraFields: []PhaseInputField{
			{Name: "discipline_code", Label: "申报学科代码", Type: "text", Required: true, Reusable: true, Placeholder: "如：F06 人工智能"},
			{Name: "project_title", Label: "项目名称", Type: "text", Required: true, Placeholder: "拟申报项目的题目"},
			{Name: "core_question", Label: "拟解决的重大科学问题", Type: "textarea", Required: true, Placeholder: "描述核心科学问题及国家战略意义"},
			{Name: "sub_projects", Label: "拟设课题数", Type: "text", Placeholder: "如：3-4个子课题"},
			{Name: "team_members", Label: "核心团队成员", Type: "textarea", Placeholder: "课题负责人列表"},
			{Name: "funding_amount", Label: "申请经费（万元）", Type: "text", Placeholder: "如：300"},
			{Name: "duration", Label: "资助期限", Type: "text", Default: "5年"},
			{Name: "prior_key_projects", Label: "前期相关重大项目", Type: "textarea", Reusable: true, Placeholder: "如：973计划、重点研发计划等"},
		},
		Emphasis: PhaseEmphasis{
			Phase2Focus: "团队整体实力、互补性、协作基础",
			Phase2Title: "研究团队与工作基础",
			Phase3Focus: "课题设置的逻辑性、分工协作的合理性",
			Phase3Title: "研究方案与课题设置",
			Phase4Focus: "各课题经费分配合理性、组织管理计划",
			Phase4Title: "经费预算与管理计划",
			Phase5Title: "申请书整合与润色",
		},
		ReviewCriteria: "评审重点：科学问题的重大性和战略意义、团队的综合实力和互补性、课题设置的系统性、是否有可能取得重大突破",
	}
}

// ---------------------------------------------------------------------------
// Profile Registry — maps workflow type to its FundingProfile.
// Used by phaseInstruction() to generate parametric prompts.
//
// This is a static registry initialized at package load time (no dynamic
// registration). Safe for concurrent read access without synchronization.
// To add a new funding type: define a Profile function above, add an entry here,
// and register in RegisterBuiltinTemplates.
// ---------------------------------------------------------------------------

var academicProfiles = map[string]FundingProfile{
	"changjiang_scholar":       ChangjiangScholarProfile(),
	"nsfc_distinguished_youth": NSFCDistinguishedYouthProfile(),
	"nsfc_excellent_youth":     NSFCExcellentYouthProfile(),
	"nsfc_youth":               NSFCYouthProfile(),
	"nsfc_general":             NSFCGeneralProfile(),
	"nsfc_key":                 NSFCKeyProfile(),
}

// GetAcademicProfile returns the FundingProfile for a given workflow type.
// Returns nil if the type is not an academic application workflow.
func GetAcademicProfile(workflowType string) *FundingProfile {
	if p, ok := academicProfiles[workflowType]; ok {
		return &p
	}
	return nil
}

// IsAcademicApplicationPhase checks if a phaseID belongs to an academic application
// workflow. Uses a pre-computed lookup map for O(1) performance (called on every
// phaseInstruction invocation).
func IsAcademicApplicationPhase(phaseID string) (*FundingProfile, bool) {
	p, ok := academicPhaseIDIndex[phaseID]
	return p, ok
}

// academicPhaseIDIndex is a pre-computed map of all valid academic phase IDs → their profile.
// Built at init time from academicProfiles × known suffixes. O(1) lookup in phaseInstruction.
var academicPhaseIDIndex = buildAcademicPhaseIDIndex()

func buildAcademicPhaseIDIndex() map[string]*FundingProfile {
	suffixes := []string{"_profile", "_foundation", "_plan", "_phase4", "_assembly"}
	index := make(map[string]*FundingProfile, len(academicProfiles)*len(suffixes))
	for wfType, profile := range academicProfiles {
		prefix := inferPhasePrefix(wfType)
		p := profile // copy for stable pointer
		for _, suffix := range suffixes {
			index[prefix+suffix] = &p
		}
	}
	return index
}
