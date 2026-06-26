package v2

import "strings"

const (
	GaokaoPhaseProfile          = "gaokao_profile"
	GaokaoPhaseDataSearch       = "gaokao_data_search"
	GaokaoPhaseCandidateRanking = "gaokao_candidate_ranking"
	GaokaoPhaseFinalPlan        = "gaokao_final_plan"
)

func GaokaoApplicationTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        string(WorkflowGaokaoApplication),
		Name:        "高考志愿填报参考",
		Description: "考生信息采集 -> 录取数据检索 -> 候选院校专业排序 -> 冲稳保填报建议。适用于普通高考志愿填报、院校专业选择、普通类、中外合办、中外合作大学和境外校区方案对比，包括宁波诺丁汉大学、西交利物浦大学、上海纽约大学等国外高校中国校区/中外合作大学，以及厦门大学马来西亚分校、河北工业大学芬兰校区等中国高校境外校区。",
		Keywords:    []string{"高考志愿", "志愿填报", "报志愿", "位次", "分数线", "中外合办", "中外合作大学", "国外高校中国校区", "境外校区", "院校专业"},
		Phases: []PhaseTemplate{
			{
				ID:           GaokaoPhaseProfile,
				Name:         "考生信息采集",
				NeedsConfirm: true,
				ToolPolicy:   ToolPolicyDocOnly,
				InputSchema:  gaokaoProfileInputSchema(),
			},
			{
				ID:           GaokaoPhaseDataSearch,
				Name:         "录取数据检索与证据整理",
				NeedsConfirm: true,
				ToolPolicy:   ToolPolicyFull,
			},
			{
				ID:           GaokaoPhaseCandidateRanking,
				Name:         "候选院校专业排序",
				NeedsConfirm: true,
				ToolPolicy:   ToolPolicyFull,
			},
			{
				ID:           GaokaoPhaseFinalPlan,
				Name:         "填报参考资料与建议",
				NeedsConfirm: true,
				ToolPolicy:   ToolPolicyDocOnly,
			},
		},
	}
}

func gaokaoProfileInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "高考志愿填报信息",
		Description: "请填写考生所在地区、选科、性别、分数和位次。位次是核心判断依据，分数仅作辅助参考。",
		Fields: []PhaseInputField{
			{Name: "province", Label: "地区/省份", Type: "select", Required: true, Reusable: true, Options: gaokaoProvinceOptions()},
			{Name: "exam_year", Label: "高考年份", Type: "text", Required: true, Placeholder: "如：2026"},
			{Name: "subject_type", Label: "选科类型", Type: "text", Required: true, Placeholder: "如：文科、理科、物化生、物化地、史政地"},
			{Name: "province_admission_mode", Label: "省份录取规则模式", Type: "select", Reusable: true, Default: "自动判断", Options: []PhaseInputOption{
				{Label: "自动判断", Value: "自动判断"},
				{Label: "新高考专业组模式", Value: "新高考专业组模式"},
				{Label: "院校专业组模式", Value: "院校专业组模式"},
				{Label: "专业+院校模式", Value: "专业+院校模式"},
				{Label: "传统文理模式", Value: "传统文理模式"},
			}},
			{Name: "gender", Label: "性别", Type: "select", Required: true, Reusable: true, Options: []PhaseInputOption{
				{Label: "男", Value: "男"},
				{Label: "女", Value: "女"},
			}},
			{Name: "score", Label: "分数", Type: "number", Placeholder: "如：586"},
			{Name: "rank", Label: "位次", Type: "number", Required: true, Placeholder: "如：32850"},
			{Name: "batch", Label: "批次", Type: "select", Options: []PhaseInputOption{
				{Label: "本科批", Value: "本科批"},
				{Label: "本科一批", Value: "本科一批"},
				{Label: "本科二批", Value: "本科二批"},
				{Label: "其他", Value: "其他"},
			}},
			{Name: "preferred_majors", Label: "偏好专业", Type: "textarea", Reusable: true, Placeholder: "多个专业可换行输入"},
			{Name: "career_intent", Label: "就业行业或发展方向意愿", Type: "textarea", Reusable: true, Placeholder: "如：互联网/AI、芯片半导体、金融、医疗健康、教师、公务员、考研深造、出国、暂不明确"},
			{Name: "future_plan", Label: "未来规划倾向", Type: "select", Reusable: true, Options: []PhaseInputOption{
				{Label: "就业优先", Value: "就业优先"},
				{Label: "考研/保研优先", Value: "考研/保研优先"},
				{Label: "出国深造", Value: "出国深造"},
				{Label: "考公/事业编", Value: "考公/事业编"},
				{Label: "暂不明确", Value: "暂不明确"},
			}},
			{Name: "excluded_majors", Label: "排除专业", Type: "textarea", Reusable: true, Placeholder: "不接受的专业或方向"},
			{Name: "preferred_locations", Label: "偏好城市/地区", Type: "textarea", Reusable: true, Placeholder: "如：长三角、北京、成都、广州"},
			{Name: "accept_joint_program", Label: "是否接受中外合办/境外校区", Type: "select", Required: true, Reusable: true, Options: []PhaseInputOption{
				{Label: "接受", Value: "接受"},
				{Label: "不接受", Value: "不接受"},
				{Label: "可作为备选", Value: "可作为备选"},
			}},
			{Name: "tuition_limit", Label: "学费上限", Type: "text", Reusable: true, Placeholder: "如：每年不超过 8 万"},
			{Name: "strategy", Label: "填报策略", Type: "select", Reusable: true, Options: []PhaseInputOption{
				{Label: "优先专业", Value: "优先专业"},
				{Label: "优先学校", Value: "优先学校"},
				{Label: "均衡", Value: "均衡"},
			}},
			{Name: "notes", Label: "补充说明", Type: "textarea", Placeholder: "家庭偏好、特殊限制、其他需要说明的信息"},
		},
	}
}

func gaokaoProvinceOptions() []PhaseInputOption {
	provinces := []string{
		"北京", "天津", "河北", "山西", "内蒙古",
		"辽宁", "吉林", "黑龙江", "上海", "江苏",
		"浙江", "安徽", "福建", "江西", "山东",
		"河南", "湖北", "湖南", "广东", "广西",
		"海南", "重庆", "四川", "贵州", "云南",
		"西藏", "陕西", "甘肃", "青海", "宁夏",
		"新疆",
	}
	options := make([]PhaseInputOption, 0, len(provinces))
	for _, province := range provinces {
		options = append(options, PhaseInputOption{Label: province, Value: province})
	}
	return options
}

func IsGaokaoApplicationPhase(phaseID string) bool {
	switch phaseID {
	case GaokaoPhaseProfile, GaokaoPhaseDataSearch, GaokaoPhaseCandidateRanking, GaokaoPhaseFinalPlan:
		return true
	default:
		return false
	}
}

func GaokaoPhaseInstruction(phaseID string) string {
	switch phaseID {
	case GaokaoPhaseProfile:
		return gaokaoProfileInstruction()
	case GaokaoPhaseDataSearch:
		return gaokaoDataSearchInstruction()
	case GaokaoPhaseCandidateRanking:
		return gaokaoCandidateRankingInstruction()
	case GaokaoPhaseFinalPlan:
		return gaokaoFinalPlanInstruction()
	default:
		return ""
	}
}

func gaokaoProfileInstruction() string {
	return `## 阶段指令

基于用户通过 InputSchema 提交的结构化信息，生成一份「高考志愿填报检索边界与考生画像」。

必须包含：
1. **考生基本信息**：地区/省份、高考年份、选科类型、省份录取规则模式、性别、分数、位次、批次。
2. **核心判断依据**：明确声明后续推荐以位次为核心，分数只作辅助参考。
3. **偏好与排除项**：偏好专业、就业行业或发展方向意愿、未来规划倾向、排除专业、偏好城市、是否接受中外合办/中外合作大学/境外校区、学费上限、填报策略。
4. **检索边界**：普通类、中外合办项目、中外合作大学/国外高校中国校区、中国高校境外校区都要检索；如果用户不接受中外合办/中外合作大学/境外校区，则相关方案只作为“不推荐/备选说明”。
5. **风险检查清单**：后续必须检查性别、选科、体检、语种、单科成绩、办学地点、学费等限制。
6. **省份规则清单**：根据省份录取规则模式说明应按“院校专业组”“专业+院校”或“传统文理”等口径检索和比较；若用户选择自动判断，必须在后续检索中从省考试院或官方指南核验。

` + gaokaoCommonConstraints()
}

func gaokaoDataSearchInstruction() string {
	return `## 阶段指令

执行「录取数据检索与证据整理」。必须联网检索，先找官方或可信来源，再整理候选院校/专业数据。

## 检索范围

1. 省级教育考试院、招生考试院、官方志愿填报系统公开信息。
2. 阳光高考等教育部相关平台。
3. 院校本科招生网、招生章程、历年录取分数/位次表。
4. 院校官方发布的专业组、招生计划、办学地点、学费和专业备注。
5. 中外合作大学、国外高校在中国设立或合作运营的校区/机构、以及中国高校境外校区或境外合作办学项目的官方招生信息；必须显式检索宁波诺丁汉大学、西交利物浦大学、上海纽约大学、昆山杜克大学、香港中文大学（深圳）、北京师范大学-香港浸会大学联合国际学院、广东以色列理工学院、深圳北理莫斯科大学等国内中外合作大学/国外高校中国校区，以及厦门大学马来西亚分校、河北工业大学芬兰校区等中国高校境外校区。

## 需要整理的数据

分别整理普通类、中外合办项目、中外合作大学/国外高校中国校区和中国高校境外校区候选项。每条候选项尽量包含：
- 学校
- 专业/专业组
- 办学地点
- 类型：普通 / 中外合办 / 中外合作大学 / 国外高校中国校区 / 境外校区
- 数据年份
- 往年最低分
- 往年最低位次
- 近三年最低位次序列（能找到则必须整理）
- 多年趋势：上升/下降/波动/数据不足
- 招生限制：性别、选科、体检、语种、单科成绩、学费、校区、境外学习地点、签证/出入境、语言环境、学历认证等
- 数据来源名称和 URL

## 输出要求

输出「证据整理表」和「结构化缓存候选记录」，不要直接给最终志愿结论。所有信息都必须给出来源；没有来源 URL、年份或最低位次的数据，必须标记为「待核验」，不能作为强推荐依据。

「结构化缓存候选记录」必须使用稳定字段，方便后续沉淀官方录取位次表：province、exam_year、admission_mode、school、major_or_group、campus、program_type、source_years、min_scores、min_ranks、plan_count、tuition、restrictions、source_name、source_url、verification_status。

` + gaokaoCommonConstraints()
}

func gaokaoCandidateRankingInstruction() string {
	return `## 阶段指令

基于前序「考生画像」和「证据整理表」，生成候选院校专业排序与冲稳保初分档。

## 排序规则

综合考虑：
1. **录取可能性**：用户位次与往年最低位次差值；多年份数据优先，必须分析近三年位次趋势，避免单年波动误判。
2. **专业质量**：专业实力、院校层次、学科口碑、就业/升学适配。
3. **用户偏好适配**：偏好专业、就业行业或发展方向意愿、未来规划倾向、偏好城市、学费上限、是否接受中外合办/境外校区。
4. **限制风险**：性别、选科、体检、语种、单科成绩、办学地点、境外学习地点、签证/出入境、语言环境、学历认证等限制。
5. **数据可信度**：官方来源、多年数据、专业级/专业组级位次优先。

就业行业意愿和未来规划倾向用于专业方向排序，不得作为机械硬筛条件。若用户填写“暂不明确”或未填写，应优先推荐专业口径较宽、转向空间较大、升学/就业弹性较好的方案，并在推荐理由中说明。专业实力、就业/升学路径、产业适配、考公/事业编适配等判断必须有公开来源；没有可靠来源时只能标注为「待核验」或「无法判断」，不得用经验判断替代来源。

## 分档规则

- **冲**：用户位次略低于或接近往年最低位次，有机会但风险较高。
- **稳**：用户位次与往年最低位次匹配度较高，录取概率相对合理。
- **保**：用户位次明显优于往年最低位次，用作兜底，同时专业可接受。

分档阈值不要硬编码为全国统一比例，要结合省份、批次、专业热度、数据年份和位次波动解释原因。

## 输出要求

输出候选排序表，字段必须包含：学校、专业/专业组、办学地点、类型（普通/中外合办/中外合作大学/国外高校中国校区/境外校区）、往年最低位次、近三年位次趋势、年份、与考生位次差、初步档位、录取概率标识、数据可信度标识、学费风险标识、就业/发展方向适配、推荐理由、限制/风险提示、数据来源、依据来源。

` + gaokaoCommonConstraints()
}

func gaokaoFinalPlanInstruction() string {
	var sb strings.Builder
	sb.WriteString(`## 阶段指令

整合前序所有阶段产出，生成最终「高考志愿填报参考资料及填报建议」。

## 必须输出四个主块

1. **总排清单**：按综合推荐顺序混排普通类、中外合办、中外合作大学/国外高校中国校区和境外校区，把最有可能、专业较好的排前面。
2. **冲**：有机会但风险较高的学校/专业。
3. **稳**：录取概率相对合理、专业质量和偏好较匹配的学校/专业。
4. **保**：兜底方案，优先保证录取和专业可接受度。

## 推荐表字段

每一行必须包含：
- 推荐序号
- 学校
- 专业/专业组
- 办学地点
- 类型（普通/中外合办/中外合作大学/国外高校中国校区/境外校区）
- 往年最低位次
- 近三年位次趋势
- 年份
- 与考生位次差
- 档位（冲/稳/保）
- 录取概率标识（高/中/低/待核验）
- 数据可信度标识（高/中/低/待核验）
- 学费风险标识（低/中/高/待核验）
- 就业/发展方向适配
- 推荐理由
- 限制/风险提示
- 数据来源
- 依据来源：该行所有关键判断对应的来源名称和 URL，包含录取数据、招生限制、专业质量、就业/发展方向适配、学费与境外校区风险等

## 结尾必须包含

1. 冲稳保比例建议。
2. 普通类、中外合办、中外合作大学/国外高校中国校区与境外校区取舍建议。
3. 专业优先/学校优先策略建议，并结合用户就业行业意愿和未来规划倾向说明专业选择取舍。
4. 结构化缓存摘要：列出可沉淀为官方录取位次表的数据条数、待核验条数、关键缺口字段。
5. 正式填报前必须复核的事项：当年招生计划、招生章程、专业备注、性别/体检/语种/单科限制、学费和办学地点。

`)
	sb.WriteString(gaokaoCommonConstraints())
	return sb.String()
}

func gaokaoCommonConstraints() string {
	return `## 重要约束（违反将导致严重错误）

### 数据真实性约束
- 【严禁幻觉】不得编造学校、专业、专业组、往年最低位次、分数、学费、招生计划、招生章程内容或数据来源。
- 【所有信息必须有来源】所有事实性信息和关键判断都必须有来源，包括学校/专业/专业组、办学地点、项目类型、往年分数/位次、招生计划、学费、招生限制、境外校区信息、专业实力、就业/升学路径、行业适配、录取概率、数据可信度和学费风险。不能找到来源时必须写「待核验」或「未找到可靠来源」，不得脑补，不得用经验判断替代来源。
- 【位次优先】所有录取可能性判断必须以位次为核心，分数只作辅助参考。
- 【来源要求】推荐依据必须尽量给出数据年份、来源名称和 URL。官方来源优先：省考试院、阳光高考、院校本科招生网、招生章程、官方历年录取表、中外合作大学/国外高校中国校区官方网站、中国高校境外校区或境外合作办学项目官方网站。
- 【第三方来源】第三方聚合站只能作为线索；若无法找到官方来源，必须标记「待官方复核」。
- 【数据不足】没有年份、来源 URL 或往年最低位次的数据，不能作为强推荐依据，只能列为「待核验」或风险提示。
- 【判断也要溯源】推荐理由、冲/稳/保分档、录取概率标识、数据可信度标识、学费风险标识和专业方向适配必须说明依据来自哪条数据或哪类来源；依据不足时必须降级为「待核验/无法判断」。
- 【结构化缓存】检索到的官方录取位次表必须整理为结构化缓存候选记录；第三方或缺字段数据只能标记为待核验，不得沉淀为强依据。
- 【多年趋势】能获取多年份数据时，必须优先使用近三年位次序列判断稳定性；若只有单年数据，必须降低数据可信度并解释波动风险。

### 限制检查约束
- 必须检查用户的选科类型是否满足专业/专业组要求。
- 必须结合省份录取规则模式检查比较口径，避免把“院校专业组”“专业+院校”“传统文理”混为一谈。
- 必须使用用户性别检查可能存在的性别限制或建议要求，例如军警、航海、定向、护理、助产等专业。
- 必须检查并提示体检、色盲色弱、身高视力、外语语种、单科成绩、办学地点、学费、中外合办/中外合作大学/国外高校中国校区培养模式，以及境外校区的境外学习地点、签证/出入境、语言环境、学历认证等风险。
- 若来源没有明确说明，不得断言「无限制」，应写「未见明确限制，填报前需以当年招生计划和招生章程为准」。

### 专业方向适配约束
- 若用户填写就业行业或发展方向意愿，必须在专业排序和推荐理由中解释该专业与行业/方向的匹配关系，例如产业方向、典型岗位、升学路径或考公/事业编适配度。
- 若用户填写未来规划倾向，必须据此调整专业评价侧重：就业优先看产业与岗位，考研/保研优先看学科平台和深造路径，出国深造看国际化和学科通用性，考公/事业编看岗位兼容性与稳定性。
- 专业方向适配只能基于招生章程、培养方案、院校/学院官网、教育部学科/专业目录、公开就业质量报告、官方专业介绍等来源；缺少来源时必须标注「待核验」，不得用泛泛常识冒充依据。
- 不得把行业意愿当作唯一筛选条件；录取可能性、专业质量、限制风险和数据可信度仍必须同时考虑。
- 若用户未填写或填写暂不明确，应提示“方向未明确”，并优先推荐口径较宽、可转向空间较大的专业组合。

### 输出约束
- 必须区分普通、中外合办、中外合作大学/国外高校中国校区和境外校区；中外合作大学/国外高校中国校区包括宁波诺丁汉大学、西交利物浦大学、上海纽约大学、昆山杜克大学、香港中文大学（深圳）、北京师范大学-香港浸会大学联合国际学院、广东以色列理工学院、深圳北理莫斯科大学等；境外校区包括中国高校在国外开设或合作运营的校区/项目，例如厦门大学马来西亚分校、河北工业大学芬兰校区等。
- 最终建议必须包含总排清单和冲/稳/保三档。
- 推荐理由要同时解释录取可能性、专业质量、多年趋势、就业/发展方向适配、用户偏好适配和限制风险。
- 每条最终推荐必须给出录取概率、数据可信度、学费风险三个可视化风险标识；风险标识必须有依据，不能只给符号。
- 每条最终推荐必须附「依据来源」字段；若某个判断没有可靠来源，必须在该字段中逐项标注「待核验」，不能省略。
- 只生成本阶段文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
`
}
