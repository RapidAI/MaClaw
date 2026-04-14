package im

// builtinCodingTemplate returns the coding workflow template with 5 phases:
// requirements → tech_design → task_breakdown → implementation → review
func builtinCodingTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowCoding,
		Name:        "编程开发",
		Description: "参考 SDLC 标准流程的软件开发工作流，涵盖需求分析、技术设计、任务拆分、编码实现和代码审查五个阶段",
		Keywords:    []string{"编程", "开发", "写代码", "coding", "实现", "功能", "bug", "修复", "重构", "软件", "程序", "应用", "工具", "系统"},
		Phases: []PhaseTemplate{
			{
				ID:          "requirements",
				Name:        "需求分析",
				Description: "梳理功能需求、非功能需求、用户角色、边界情况和验收标准",
				Prompt: `你是一位资深需求分析师。请根据用户的意图描述，输出完整的需求分析文档，包含：
1. 功能需求列表（编号，每条需求一句话描述 + 验收标准）
2. 非功能需求（性能、安全、可用性等）
3. 用户角色与权限
4. 边界情况与异常处理
5. 技术约束（如果用户提到了技术栈、平台等）

输出格式为 Markdown。`,
				Deliverable:  "需求分析文档（Markdown）",
				Checklist:    []string{"所有用户目标都有对应的功能需求", "每条功能需求有明确的验收标准", "非功能需求已覆盖性能和安全", "边界情况和异常处理已列出"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      false,
			},
			{
				ID:          "tech_design",
				Name:        "技术设计",
				Description: "架构设计、技术选型、模块划分、接口设计和数据结构",
				Prompt: `你是一位资深架构师。请根据已确认的需求文档，输出技术设计文档，包含：
1. 整体架构设计（分层/模块划分）
2. 技术选型与理由
3. 核心模块说明（职责、依赖关系）
4. 关键接口设计（输入/输出/错误处理）
5. 数据结构与存储方案

输出格式为 Markdown，关键接口用代码块展示。`,
				Deliverable:  "技术设计文档（Markdown）",
				Checklist:    []string{"架构设计覆盖所有功能模块", "技术选型有明确理由", "接口设计包含错误处理", "数据结构满足需求中的约束"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      false,
			},
			{
				ID:          "task_breakdown",
				Name:        "任务拆分",
				Description: "将技术设计拆分为可执行的任务列表，含优先级和依赖关系",
				Prompt: `你是一位项目经理。请根据技术设计文档，输出任务拆分列表，包含：
1. 任务编号和名称
2. 任务描述（做什么、怎么做）
3. 优先级（P0/P1/P2）
4. 依赖关系（依赖哪些任务先完成）
5. 预估工作量（简单/中等/复杂）

按执行顺序排列，输出格式为 Markdown 表格。`,
				Deliverable:  "任务拆分列表（Markdown 表格）",
				Checklist:    []string{"任务粒度适中，每个任务可独立验证", "依赖关系无循环", "优先级标注合理", "所有设计模块都有对应任务"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      false,
			},
			{
				ID:          "implementation",
				Name:        "编码实现",
				Description: "按任务列表逐个编码实现，路由到设备 Agent 执行",
				Prompt: `请根据任务拆分列表，按优先级和依赖顺序逐个实现。每个任务完成后输出：
1. 实现的文件和关键代码片段
2. 本地测试结果
3. 遇到的问题和解决方案

当前任务完成后，汇报进度并等待指示。`,
				Deliverable:  "代码实现（源代码文件）",
				Checklist:    []string{"代码符合技术设计中的架构", "关键逻辑有单元测试", "命名规范一致"},
				NeedsConfirm: true,
				NeedsDevice:  true,
				CanSkip:      false,
			},
			{
				ID:          "review",
				Name:        "代码审查",
				Description: "审查代码质量、命名、结构、性能和安全",
				Prompt: `你是一位代码审查专家。请对本次开发的所有代码进行审查，输出审查报告，包含：
1. 代码质量评分（1-10）
2. 命名和结构问题
3. 潜在的性能问题
4. 安全风险
5. 改进建议

按严重程度排序，输出格式为 Markdown。`,
				Deliverable:  "代码审查报告（Markdown）",
				Checklist:    []string{"命名规范一致", "无明显性能瓶颈", "无安全漏洞", "代码结构清晰可维护"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      true,
			},
		},
	}
}

// builtinProductDesignTemplate returns the product design workflow template with 4 phases:
// problem_discovery → solution_design → prd → prototype
func builtinProductDesignTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowProductDesign,
		Name:        "产品设计",
		Description: "参考 PRD 标准流程与 Double Diamond 方法论的产品设计工作流，涵盖问题发现、方案设计、产品需求文档和原型设计四个阶段",
		Keywords:    []string{"产品", "设计", "PRD", "需求文档", "用户体验", "交互", "原型", "产品经理", "功能规划"},
		Phases: []PhaseTemplate{
			{
				ID:          "problem_discovery",
				Name:        "问题发现",
				Description: "目标用户画像、核心痛点、竞品分析和问题边界",
				Prompt: `你是一位产品研究员。请根据用户的意图描述，输出问题发现报告，包含：
1. 目标用户画像（角色、场景、痛点）
2. 核心问题定义（用一句话描述要解决的问题）
3. 竞品分析（至少 3 个竞品的优劣对比）
4. 问题边界（做什么、不做什么）

输出格式为 Markdown。`,
				Deliverable:  "问题发现报告（Markdown）",
				Checklist:    []string{"目标用户画像清晰具体", "核心问题定义明确", "竞品分析有数据支撑", "问题边界划分合理"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      false,
			},
			{
				ID:          "solution_design",
				Name:        "方案设计",
				Description: "功能列表、用户故事、信息架构和交互流程",
				Prompt: `你是一位产品设计师。请根据问题发现报告，输出方案设计文档，包含：
1. 功能列表（核心功能 + 增强功能，按优先级排序）
2. 用户故事（As a... I want... So that...）
3. 信息架构（页面/模块层级关系）
4. 核心交互流程（用文字描述关键用户路径）

输出格式为 Markdown。`,
				Deliverable:  "方案设计文档（Markdown）",
				Checklist:    []string{"功能列表覆盖核心痛点", "用户故事可验证", "信息架构层级清晰", "交互流程无死循环"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      false,
			},
			{
				ID:          "prd",
				Name:        "产品需求文档",
				Description: "产品目标、功能规格、非功能需求、发布标准和时间线",
				Prompt: `你是一位资深产品经理。请根据方案设计文档，输出正式的产品需求文档（PRD），包含：
1. 产品目标与成功指标
2. 功能规格（每个功能的详细描述、输入输出、异常处理）
3. 非功能需求（性能、安全、兼容性）
4. 发布标准（MVP 范围、上线条件）
5. 时间线与里程碑

输出格式为 Markdown。`,
				Deliverable:  "产品需求文档 PRD（Markdown）",
				Checklist:    []string{"产品目标有可量化的成功指标", "功能规格足够开发团队理解", "非功能需求已覆盖", "发布标准明确可执行"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      false,
			},
			{
				ID:          "prototype",
				Name:        "原型设计",
				Description: "原型描述、线框图和关键页面流程",
				Prompt: `你是一位交互设计师。请根据 PRD，输出原型设计说明，包含：
1. 关键页面列表及其功能说明
2. 页面间的导航流程
3. 每个页面的核心元素和布局描述
4. 交互细节（点击、滑动、输入等操作的响应）

输出格式为 Markdown，用文字详细描述每个页面的布局和交互。`,
				Deliverable:  "原型设计说明（Markdown）",
				Checklist:    []string{"关键页面覆盖核心用户路径", "页面导航流程完整", "交互细节描述清晰"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      true,
			},
		},
	}
}

// builtinInnovationTemplate returns the innovation workflow template with 5 phases:
// opportunity → ideation → validation → roadmap → action_plan
func builtinInnovationTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowInnovation,
		Name:        "创新制定",
		Description: "参考创新管道框架的创新制定工作流，涵盖机会识别、创意发散、可行性验证、路线图和行动计划五个阶段",
		Keywords:    []string{"创新", "创意", "新产品", "新业务", "机会", "验证", "MVP", "路线图", "商业模式"},
		Phases: []PhaseTemplate{
			{
				ID:          "opportunity",
				Name:        "机会识别",
				Description: "市场趋势、用户需求缺口和技术可行性分析",
				Prompt: `你是一位创新顾问。请根据用户的意图描述，输出机会识别报告，包含：
1. 市场趋势分析（行业动态、技术趋势）
2. 用户需求缺口（未被满足的需求）
3. 技术可行性初步评估
4. 机会窗口判断（为什么是现在）

输出格式为 Markdown。`,
				Deliverable:  "机会识别报告（Markdown）",
				Checklist:    []string{"市场趋势有数据或案例支撑", "需求缺口定义明确", "技术可行性有初步判断", "机会窗口论证合理"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      false,
			},
			{
				ID:          "ideation",
				Name:        "创意发散",
				Description: "多方向创意方案及各自优劣对比",
				Prompt: `你是一位创意总监。请根据机会识别报告，输出创意发散文档，包含：
1. 至少 3 个不同方向的创意方案
2. 每个方案的核心理念和差异化点
3. 各方案的优劣对比（表格形式）
4. 推荐方案及理由

输出格式为 Markdown，对比部分用表格展示。`,
				Deliverable:  "创意发散文档（Markdown）",
				Checklist:    []string{"至少提出 3 个差异化方案", "每个方案有明确的核心理念", "优劣对比客观全面", "推荐理由充分"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      false,
			},
			{
				ID:          "validation",
				Name:        "可行性验证",
				Description: "技术风险、商业可行性、资源评估和 MVP 定义",
				Prompt: `你是一位商业分析师。请根据推荐的创意方案，输出可行性验证报告，包含：
1. 技术风险评估（关键技术难点、解决方案）
2. 商业可行性（目标市场规模、盈利模式）
3. 资源评估（团队、资金、时间）
4. MVP 定义（最小可行产品的功能范围）

输出格式为 Markdown。`,
				Deliverable:  "可行性验证报告（Markdown）",
				Checklist:    []string{"技术风险已识别并有应对方案", "商业模式可行", "资源需求评估合理", "MVP 范围明确且可执行"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      false,
			},
			{
				ID:          "roadmap",
				Name:        "路线图",
				Description: "里程碑、时间线和资源分配",
				Prompt: `你是一位项目规划师。请根据可行性验证报告，输出项目路线图，包含：
1. 关键里程碑（MVP、Beta、正式发布等）
2. 每个里程碑的时间节点和交付物
3. 资源分配计划（人员、预算）
4. 风险缓解措施

输出格式为 Markdown，时间线用表格展示。`,
				Deliverable:  "项目路线图（Markdown）",
				Checklist:    []string{"里程碑划分合理", "时间节点可执行", "资源分配与需求匹配", "风险缓解措施具体"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      true,
			},
			{
				ID:          "action_plan",
				Name:        "行动计划",
				Description: "具体行动项、责任人和完成时间",
				Prompt: `你是一位执行教练。请根据路线图，输出具体的行动计划，包含：
1. 行动项列表（编号、描述、负责人、截止日期）
2. 第一周的具体任务
3. 关键决策点和检查点
4. 沟通机制（汇报频率、会议安排）

输出格式为 Markdown 表格。`,
				Deliverable:  "行动计划（Markdown 表格）",
				Checklist:    []string{"行动项具体可执行", "责任人明确", "时间节点合理", "有定期检查机制"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      true,
			},
		},
	}
}

// builtinBusinessPlanTemplate returns the business plan workflow template with 5 phases:
// executive_summary → market_analysis → product_strategy → operations → financial_projection
func builtinBusinessPlanTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowBusinessPlan,
		Name:        "商业计划",
		Description: "参考标准商业计划书结构的工作流，涵盖执行摘要、市场分析、产品策略、运营计划和财务预测五个阶段",
		Keywords:    []string{"商业计划", "创业", "融资", "BP", "商业模式", "市场分析", "财务", "投资", "盈利"},
		Phases: []PhaseTemplate{
			{
				ID:          "executive_summary",
				Name:        "执行摘要",
				Description: "一页纸商业概述",
				Prompt: `你是一位商业顾问。请根据用户的意图描述，输出执行摘要，包含：
1. 商业概念（一句话描述做什么）
2. 目标市场和客户
3. 核心价值主张
4. 盈利模式概述
5. 团队优势（如果用户提到）
6. 融资需求（如果适用）

控制在一页纸以内，输出格式为 Markdown。`,
				Deliverable:  "执行摘要（一页纸 Markdown）",
				Checklist:    []string{"商业概念一句话说清", "目标市场明确", "价值主张有差异化", "盈利模式可行"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      false,
			},
			{
				ID:          "market_analysis",
				Name:        "市场分析",
				Description: "市场规模、竞争格局和目标客户分析",
				Prompt: `你是一位市场分析师。请根据执行摘要，输出市场分析报告，包含：
1. 市场规模估算（TAM/SAM/SOM）
2. 市场增长趋势
3. 竞争格局分析（主要竞争者、市场份额、竞争壁垒）
4. 目标客户细分（人口统计、行为特征、购买动机）
5. 市场进入策略

输出格式为 Markdown，数据部分用表格展示。`,
				Deliverable:  "市场分析报告（Markdown）",
				Checklist:    []string{"市场规模有估算依据", "竞争分析覆盖主要玩家", "目标客户画像具体", "进入策略可执行"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      false,
			},
			{
				ID:          "product_strategy",
				Name:        "产品策略",
				Description: "产品定位、差异化和定价策略",
				Prompt: `你是一位产品策略师。请根据市场分析报告，输出产品策略文档，包含：
1. 产品定位（目标用户 + 核心价值）
2. 差异化策略（与竞品的关键区别）
3. 产品路线图（MVP → V1 → V2）
4. 定价策略（定价模型、价格区间、依据）
5. 获客策略（渠道、方式、预期成本）

输出格式为 Markdown。`,
				Deliverable:  "产品策略文档（Markdown）",
				Checklist:    []string{"产品定位清晰", "差异化有竞争力", "定价策略有依据", "获客策略可执行"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      false,
			},
			{
				ID:          "operations",
				Name:        "运营计划",
				Description: "团队、流程、供应链和合作伙伴",
				Prompt: `你是一位运营总监。请根据产品策略文档，输出运营计划，包含：
1. 团队架构（核心岗位、职责、招聘计划）
2. 运营流程（从获客到交付的关键流程）
3. 供应链/技术基础设施
4. 合作伙伴策略
5. 关键运营指标（KPI）

输出格式为 Markdown。`,
				Deliverable:  "运营计划（Markdown）",
				Checklist:    []string{"团队架构覆盖关键岗位", "运营流程完整", "KPI 可量化", "合作伙伴策略明确"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      true,
			},
			{
				ID:          "financial_projection",
				Name:        "财务预测",
				Description: "收入预测、成本结构和盈亏平衡分析",
				Prompt: `你是一位财务分析师。请根据前面的分析，输出财务预测报告，包含：
1. 收入预测（月/季度/年，至少预测 3 年）
2. 成本结构（固定成本 + 变动成本）
3. 盈亏平衡分析（何时达到盈亏平衡）
4. 现金流预测
5. 融资需求和资金用途（如果适用）

输出格式为 Markdown，数据部分用表格展示。`,
				Deliverable:  "财务预测报告（Markdown）",
				Checklist:    []string{"收入预测假设合理", "成本结构完整", "盈亏平衡时间点明确", "现金流预测覆盖关键时期"},
				NeedsConfirm: true,
				NeedsDevice:  false,
				CanSkip:      true,
			},
		},
	}
}
