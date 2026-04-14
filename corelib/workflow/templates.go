package workflow

// RegisterBuiltinTemplates registers all built-in workflow templates
// into the given registry. Called automatically by NewWorkflowRegistry.
func RegisterBuiltinTemplates(r *WorkflowRegistry) {
	r.Register(codingTemplate())
	r.Register(productDesignTemplate())
	r.Register(innovationTemplate())
	r.Register(businessPlanTemplate())
	r.Register(testingTemplate())
	r.Register(literatureReviewTemplate())
	r.Register(researchReportTemplate())
	r.Register(experimentDesignTemplate())
	r.Register(grantProposalTemplate())
	r.Register(paperWritingTemplate())
	r.Register(projectProposalTemplate())
	r.Register(eventPlanningTemplate())
	r.Register(competitiveAnalysisTemplate())
	r.Register(presentationDesignTemplate())
}

// codingTemplate returns the coding workflow template (5 phases).
func codingTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowCoding,
		Name:        "编程开发",
		Description: "适用于软件开发任务的标准工作流，涵盖需求分析、技术设计、任务拆分、编码实现和代码审查五个阶段。",
		Keywords:    []string{"开发", "编程", "编写", "实现", "代码", "软件", "应用", "系统", "功能", "模块", "重构", "修bug"},
		Phases: []PhaseTemplate{
			{
				ID:           "requirements",
				Name:         "需求分析",
				Description:  "梳理和明确项目需求，输出结构化的需求文档。",
				Prompt:       "你现在处于【需求分析】阶段。请根据用户意图，输出一份完整的需求文档，包含：功能需求列表、非功能需求、边界情况、验收标准。使用 Markdown 格式，条理清晰，每条需求有编号。不要开始编码，不要开始技术设计，专注于需求梳理。输出需求文档后立即停止，等待用户确认或修改意见。",
				Deliverable:  "需求文档（Markdown 格式，含功能需求、非功能需求、边界情况、验收标准）",
				Checklist:    []string{"功能需求是否完整覆盖用户目标", "非功能需求是否包含性能、安全、兼容性", "边界情况和异常场景是否已识别", "验收标准是否可量化可验证"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "tech_design",
				Name:         "技术设计",
				Description:  "基于需求文档进行架构设计和技术选型，输出技术设计文档。",
				Prompt:       "你现在处于【技术设计】阶段。请基于已确认的需求文档，输出技术设计文档，包含：架构设计、技术选型及理由、模块划分、接口设计、数据结构定义。使用 Markdown 格式，必要时用 Mermaid 图表辅助说明。不要开始编码。",
				Deliverable:  "技术设计文档（Markdown 格式，含架构图、技术选型、模块划分、接口定义、数据结构）",
				Checklist:    []string{"架构设计是否满足需求文档中的所有功能需求", "技术选型是否有明确理由", "模块划分是否职责清晰、耦合度低", "接口设计是否完整且一致"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "task_breakdown",
				Name:         "任务拆分",
				Description:  "将技术设计拆分为可执行的开发任务列表，明确优先级和依赖关系。",
				Prompt:       "你现在处于【任务拆分】阶段。请基于技术设计文档，将开发工作拆分为具体的任务列表，每个任务包含：编号、描述、优先级、依赖关系、预估工作量。按执行顺序排列，标注可并行的任务。使用 Markdown 格式。",
				Deliverable:  "任务拆分文档（Markdown 格式，含编号、描述、优先级、依赖关系、预估工作量）",
				Checklist:    []string{"任务粒度是否适中（单个任务可在一次迭代内完成）", "依赖关系是否正确标注", "优先级排序是否合理", "是否覆盖技术设计中的所有模块"},
				NeedsConfirm: true,
				CanSkip:      true,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "implementation",
				Name:         "编码实现",
				Description:  "按任务列表逐项编码实现，使用完整工具集进行开发。",
				Prompt:       "你现在处于【编码实现】阶段。请按照任务列表逐项实现，遵循技术设计文档的架构和接口定义。每完成一个任务，简要说明实现要点。确保代码质量，包含必要的错误处理和注释。",
				Deliverable:  "可运行的代码实现（按任务列表逐项完成）",
				Checklist:    []string{"代码是否遵循技术设计文档的架构", "错误处理是否完善", "是否包含必要的注释和文档"},
				NeedsConfirm: false,
				CanSkip:      false,
				ToolPolicy:   ToolFilterFull,
			},
			{
				ID:           "review",
				Name:         "代码审查",
				Description:  "对实现代码进行质量审查，检查潜在问题并提出优化建议。",
				Prompt:       "你现在处于【代码审查】阶段。请对已完成的代码进行全面审查，检查：代码质量、命名规范、潜在 bug、性能问题、安全隐患。输出审查报告，列出发现的问题和优化建议，按严重程度排序。",
				Deliverable:  "代码审查报告（Markdown 格式，含问题列表、严重程度、优化建议）",
				Checklist:    []string{"是否检查了命名规范和代码风格", "是否识别了潜在的 bug 和边界问题", "是否评估了性能和安全隐患", "优化建议是否具体可操作"},
				NeedsConfirm: true,
				CanSkip:      true,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// productDesignTemplate returns the product design workflow template (4 phases, all doc_only).
func productDesignTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowProductDesign,
		Name:        "产品设计",
		Description: "适用于产品设计任务的工作流，涵盖问题发现、方案设计、PRD 编写和原型设计四个阶段。",
		Keywords:    []string{"产品", "设计", "PRD", "需求", "用户体验", "原型", "功能规划", "产品经理"},
		Phases: []PhaseTemplate{
			{
				ID:           "problem_discovery",
				Name:         "问题发现",
				Description:  "深入分析目标用户和场景，识别核心问题和痛点。",
				Prompt:       "你现在处于【问题发现】阶段。请分析目标用户群体、使用场景和核心痛点，输出问题分析报告，包含：用户画像、场景描述、痛点列表（按严重程度排序）、竞品分析摘要。使用 Markdown 格式。",
				Deliverable:  "问题分析报告（Markdown 格式，含用户画像、场景描述、痛点列表、竞品分析）",
				Checklist:    []string{"用户画像是否清晰具体", "痛点是否来自真实场景而非臆测", "是否进行了竞品分析", "问题优先级排序是否合理"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "solution_design",
				Name:         "方案设计",
				Description:  "针对核心问题设计解决方案，明确产品定位和核心功能。",
				Prompt:       "你现在处于【方案设计】阶段。请基于问题分析报告，设计产品解决方案，包含：产品定位、核心功能列表、用户流程图、信息架构。重点说明方案如何解决已识别的痛点。使用 Markdown 格式。",
				Deliverable:  "方案设计文档（Markdown 格式，含产品定位、核心功能、用户流程、信息架构）",
				Checklist:    []string{"方案是否针对性地解决了已识别的痛点", "核心功能是否聚焦而非贪多", "用户流程是否简洁直观", "信息架构是否层次清晰"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "prd",
				Name:         "产品需求文档",
				Description:  "编写完整的产品需求文档（PRD），作为开发团队的交付依据。",
				Prompt:       "你现在处于【产品需求文档】阶段。请基于方案设计，编写完整的 PRD，包含：产品概述、功能需求（含优先级）、非功能需求、交互说明、数据需求、上线计划。每条需求有编号，验收标准明确。使用 Markdown 格式。",
				Deliverable:  "产品需求文档 PRD（Markdown 格式，含功能需求、非功能需求、交互说明、上线计划）",
				Checklist:    []string{"功能需求是否有明确的优先级和验收标准", "交互说明是否覆盖主要用户流程", "非功能需求是否包含性能和兼容性要求", "上线计划是否可执行"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "prototype",
				Name:         "原型设计",
				Description:  "基于 PRD 描述交互原型的关键页面和流程。",
				Prompt:       "你现在处于【原型设计】阶段。请基于 PRD，描述关键页面的布局、交互流程和状态变化。输出原型说明文档，包含：页面列表、每页的布局描述、页面间跳转逻辑、关键交互细节。使用 Markdown 格式。",
				Deliverable:  "原型说明文档（Markdown 格式，含页面布局描述、跳转逻辑、交互细节）",
				Checklist:    []string{"关键页面是否全部覆盖", "页面间跳转逻辑是否完整", "交互细节是否足够指导开发"},
				NeedsConfirm: true,
				CanSkip:      true,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// innovationTemplate returns the innovation workflow template (5 phases, all doc_only).
func innovationTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowInnovation,
		Name:        "创新制定",
		Description: "适用于创新项目的工作流，涵盖机会识别、创意发散、可行性验证、路线图和行动计划五个阶段。",
		Keywords:    []string{"创新", "创意", "机会", "验证", "路线图", "行动计划", "头脑风暴", "可行性"},
		Phases: []PhaseTemplate{
			{
				ID:           "opportunity",
				Name:         "机会识别",
				Description:  "分析市场趋势和用户需求，识别创新机会。",
				Prompt:       "你现在处于【机会识别】阶段。请分析相关领域的市场趋势、技术发展和用户未满足需求，输出机会分析报告，包含：行业趋势、技术机会、用户需求缺口、竞争格局分析。使用 Markdown 格式。",
				Deliverable:  "机会分析报告（Markdown 格式，含行业趋势、技术机会、需求缺口、竞争格局）",
				Checklist:    []string{"趋势分析是否有数据或案例支撑", "机会是否与用户真实需求关联", "竞争格局分析是否客观全面"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "ideation",
				Name:         "创意发散",
				Description:  "基于机会分析进行创意发散，生成多个候选方案。",
				Prompt:       "你现在处于【创意发散】阶段。请基于机会分析报告，进行创意发散，生成至少 3 个候选方案。每个方案包含：核心创意、目标用户、价值主张、初步实现思路。使用 Markdown 格式，鼓励大胆创新。",
				Deliverable:  "创意方案集（Markdown 格式，含多个候选方案的核心创意、价值主张、实现思路）",
				Checklist:    []string{"是否生成了至少 3 个差异化方案", "每个方案的价值主张是否清晰", "创意是否具有新颖性和差异化", "目标用户是否明确"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "validation",
				Name:         "可行性验证",
				Description:  "对候选方案进行技术、市场和资源可行性评估。",
				Prompt:       "你现在处于【可行性验证】阶段。请对每个候选方案进行可行性评估，包含：技术可行性（实现难度、技术风险）、市场可行性（市场规模、竞争壁垒）、资源可行性（团队能力、时间成本）。给出综合评分和推荐方案。使用 Markdown 格式。",
				Deliverable:  "可行性评估报告（Markdown 格式，含技术/市场/资源可行性分析、综合评分、推荐方案）",
				Checklist:    []string{"技术可行性评估是否考虑了实现难度和风险", "市场可行性是否有规模和竞争分析", "资源评估是否切合实际", "是否给出了明确的推荐方案"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "roadmap",
				Name:         "路线图",
				Description:  "为推荐方案制定分阶段实施路线图。",
				Prompt:       "你现在处于【路线图】阶段。请为推荐方案制定实施路线图，包含：阶段划分（短期/中期/长期）、每阶段的里程碑和交付物、关键依赖和风险点、资源需求。使用 Markdown 格式。",
				Deliverable:  "实施路线图（Markdown 格式，含阶段划分、里程碑、依赖关系、资源需求）",
				Checklist:    []string{"阶段划分是否合理（短期可验证、长期有愿景）", "里程碑是否具体可衡量", "关键风险是否已识别并有应对策略"},
				NeedsConfirm: true,
				CanSkip:      true,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "action_plan",
				Name:         "行动计划",
				Description:  "制定具体的执行行动计划，明确责任人和时间节点。",
				Prompt:       "你现在处于【行动计划】阶段。请制定第一阶段的具体行动计划，包含：任务列表、负责人/角色、时间节点、所需资源、成功指标。确保计划可立即执行。使用 Markdown 格式。",
				Deliverable:  "行动计划（Markdown 格式，含任务列表、负责人、时间节点、资源需求、成功指标）",
				Checklist:    []string{"任务是否具体到可立即执行", "时间节点是否合理", "成功指标是否可量化", "所需资源是否已明确"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// businessPlanTemplate returns the business plan workflow template (5 phases, all doc_only).
func businessPlanTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowBusinessPlan,
		Name:        "商业计划",
		Description: "适用于商业计划编写的工作流，涵盖执行摘要、市场分析、产品策略、运营计划和财务预测五个阶段。",
		Keywords:    []string{"商业", "计划", "市场", "财务", "运营", "融资", "商业模式", "盈利"},
		Phases: []PhaseTemplate{
			{
				ID:           "executive_summary",
				Name:         "执行摘要",
				Description:  "撰写商业计划的执行摘要，概述项目核心价值和商业模式。",
				Prompt:       "你现在处于【执行摘要】阶段。请撰写商业计划的执行摘要，包含：项目愿景、核心价值主张、目标市场概述、商业模式简述、团队优势、融资需求概要。控制在 1-2 页，语言精炼有说服力。使用 Markdown 格式。",
				Deliverable:  "执行摘要（Markdown 格式，含愿景、价值主张、目标市场、商业模式、团队、融资需求）",
				Checklist:    []string{"价值主张是否清晰有说服力", "商业模式是否简明易懂", "融资需求是否有明确数字", "篇幅是否精炼（1-2 页）"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "market_analysis",
				Name:         "市场分析",
				Description:  "深入分析目标市场规模、竞争格局和增长趋势。",
				Prompt:       "你现在处于【市场分析】阶段。请进行深入的市场分析，包含：目标市场定义和规模（TAM/SAM/SOM）、市场增长趋势、竞争格局（主要竞争者及其优劣势）、目标客户细分、市场进入策略。使用 Markdown 格式，尽量引用数据支撑。",
				Deliverable:  "市场分析报告（Markdown 格式，含市场规模、增长趋势、竞争格局、客户细分、进入策略）",
				Checklist:    []string{"市场规模是否有 TAM/SAM/SOM 分层", "竞争分析是否覆盖主要竞争者", "客户细分是否清晰可操作", "市场进入策略是否具体可行"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "product_strategy",
				Name:         "产品策略",
				Description:  "定义产品策略、差异化定位和发展路径。",
				Prompt:       "你现在处于【产品策略】阶段。请制定产品策略，包含：产品定位和差异化优势、核心功能和产品路线图、定价策略、销售渠道策略、合作伙伴策略。说明产品如何在竞争中脱颖而出。使用 Markdown 格式。",
				Deliverable:  "产品策略文档（Markdown 格式，含产品定位、功能路线图、定价策略、渠道策略）",
				Checklist:    []string{"差异化优势是否明确且可持续", "定价策略是否有竞争力分析支撑", "产品路线图是否分阶段可执行", "渠道策略是否匹配目标客户"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "operations",
				Name:         "运营计划",
				Description:  "制定运营计划，包括团队组建、流程设计和关键指标。",
				Prompt:       "你现在处于【运营计划】阶段。请制定运营计划，包含：组织架构和关键岗位、运营流程设计、关键绩效指标（KPI）、风险管理计划、里程碑时间表。使用 Markdown 格式。",
				Deliverable:  "运营计划（Markdown 格式，含组织架构、运营流程、KPI、风险管理、里程碑）",
				Checklist:    []string{"组织架构是否匹配业务需求", "KPI 是否可量化可追踪", "风险管理是否覆盖主要风险场景", "里程碑时间表是否切合实际"},
				NeedsConfirm: true,
				CanSkip:      true,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "financial_projection",
				Name:         "财务预测",
				Description:  "编制财务预测，包括收入模型、成本结构和盈亏分析。",
				Prompt:       "你现在处于【财务预测】阶段。请编制 3-5 年财务预测，包含：收入模型和假设、成本结构（固定/可变）、盈亏平衡分析、现金流预测、融资计划和资金用途。数字需有明确假设支撑。使用 Markdown 格式。",
				Deliverable:  "财务预测报告（Markdown 格式，含收入模型、成本结构、盈亏分析、现金流、融资计划）",
				Checklist:    []string{"收入假设是否合理且有依据", "成本结构是否区分固定和可变成本", "盈亏平衡点是否明确", "融资计划是否与业务阶段匹配"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// testingTemplate returns the testing workflow template (5 phases).
func testingTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowTesting,
		Name:        "测试方案",
		Description: "适用于软件测试任务的工作流，涵盖测试策略、测试用例设计、测试环境规划、测试执行和缺陷报告五个阶段。",
		Keywords:    []string{"测试", "QA", "质量", "用例", "缺陷", "回归", "自动化测试", "测试计划"},
		Phases: []PhaseTemplate{
			{
				ID:           "test_strategy",
				Name:         "测试策略",
				Description:  "制定整体测试策略，明确测试范围、方法和资源。",
				Prompt:       "你现在处于【测试策略】阶段。请制定测试策略文档，包含：测试目标和范围、测试类型（单元/集成/系统/验收）、测试方法（手动/自动化）、风险评估和优先级、资源和时间估算。使用 Markdown 格式。",
				Deliverable:  "测试策略文档（Markdown 格式，含测试目标、范围、类型、方法、风险评估、资源估算）",
				Checklist:    []string{"测试范围是否覆盖所有关键功能", "测试类型选择是否合理", "风险评估是否识别了高优先级测试项", "资源和时间估算是否切合实际"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "test_design",
				Name:         "测试用例设计",
				Description:  "设计详细的测试用例，覆盖正常流程和异常场景。",
				Prompt:       "你现在处于【测试用例设计】阶段。请设计测试用例集，每个用例包含：用例编号、测试场景、前置条件、测试步骤、预期结果、优先级。覆盖正常流程、边界条件和异常场景。使用 Markdown 表格格式。",
				Deliverable:  "测试用例文档（Markdown 格式，含用例编号、场景、步骤、预期结果、优先级）",
				Checklist:    []string{"用例是否覆盖所有测试策略中的测试项", "是否包含正常流程和异常场景", "边界条件是否充分考虑", "用例步骤是否清晰可执行"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "test_environment",
				Name:         "测试环境规划",
				Description:  "规划测试环境配置，包括硬件、软件和测试数据准备。",
				Prompt:       "你现在处于【测试环境规划】阶段。请规划测试环境，包含：环境配置需求（硬件/软件/网络）、测试数据准备方案、环境搭建步骤、环境验证检查项。使用 Markdown 格式。",
				Deliverable:  "测试环境规划文档（Markdown 格式，含环境配置、测试数据方案、搭建步骤、验证检查项）",
				Checklist:    []string{"环境配置是否匹配生产环境关键特征", "测试数据是否覆盖各种场景", "搭建步骤是否可重复执行"},
				NeedsConfirm: true,
				CanSkip:      true,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "test_execution",
				Name:         "测试执行",
				Description:  "执行测试用例，使用完整工具集运行测试并记录结果。",
				Prompt:       "你现在处于【测试执行】阶段。请按照测试用例逐项执行测试，记录每个用例的实际结果、通过/失败状态。对失败的用例记录详细的复现步骤和环境信息。可使用所有可用工具辅助测试。",
				Deliverable:  "测试执行记录（含每个用例的执行结果、通过/失败状态、失败详情）",
				Checklist:    []string{"所有高优先级用例是否已执行", "失败用例是否有详细的复现步骤", "测试覆盖率是否达到策略要求"},
				NeedsConfirm: false,
				CanSkip:      false,
				ToolPolicy:   ToolFilterFull,
			},
			{
				ID:           "defect_report",
				Name:         "缺陷跟踪与报告",
				Description:  "汇总测试结果，编写缺陷报告和测试总结。",
				Prompt:       "你现在处于【缺陷跟踪与报告】阶段。请汇总测试执行结果，编写缺陷报告，包含：测试总结（通过率、覆盖率）、缺陷列表（编号、描述、严重程度、复现步骤、状态）、风险评估、改进建议。使用 Markdown 格式。",
				Deliverable:  "缺陷报告（Markdown 格式，含测试总结、缺陷列表、风险评估、改进建议）",
				Checklist:    []string{"缺陷描述是否清晰可复现", "严重程度分级是否合理", "测试总结是否包含关键指标（通过率、覆盖率）", "改进建议是否具体可操作"},
				NeedsConfirm: true,
				CanSkip:      true,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// literatureReviewTemplate returns the literature review workflow template (5 phases, all doc_only).
func literatureReviewTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowLiteratureReview,
		Name:        "论文综述整理",
		Description: "适用于某领域学术论文综述整理的工作流，涵盖选题定义、文献检索、文献筛选与分类、核心内容提取和综述撰写五个阶段。",
		Keywords:    []string{"论文", "综述", "文献", "学术", "研究", "review", "survey", "期刊", "引用", "摘要", "学科", "领域"},
		Phases: []PhaseTemplate{
			{
				ID:           "topic_definition",
				Name:         "选题与范围界定",
				Description:  "明确综述的研究领域、核心问题和时间范围，确定检索策略。",
				Prompt:       "你现在处于【选题与范围界定】阶段。请根据用户意图，输出选题报告，包含：研究领域定义、核心研究问题（1-3 个）、时间范围（如近 5 年/10 年）、目标期刊/会议/数据库列表、检索关键词组合（中英文）、排除标准。使用 Markdown 格式。",
				Deliverable:  "选题报告（Markdown 格式，含研究问题、时间范围、检索策略、排除标准）",
				Checklist:    []string{"研究问题是否聚焦且可回答", "检索关键词是否覆盖中英文同义词", "时间范围和数据库选择是否合理", "排除标准是否明确"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "literature_search",
				Name:         "文献检索与收集",
				Description:  "按检索策略系统收集相关文献，记录检索过程和初步结果。",
				Prompt:       "你现在处于【文献检索与收集】阶段。请基于选题报告的检索策略，模拟系统检索过程，输出检索记录，包含：各数据库检索结果数量、检索式记录、初步筛选后的文献列表（标题、作者、年份、来源、摘要摘录）。按相关性排序，标注高相关文献。使用 Markdown 格式。",
				Deliverable:  "检索记录与初步文献列表（Markdown 格式，含检索式、各库结果、文献条目）",
				Checklist:    []string{"检索是否覆盖了所有目标数据库", "检索式是否与选题报告一致", "文献列表是否包含必要的元数据", "是否标注了高相关文献"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "screening_classification",
				Name:         "文献筛选与分类",
				Description:  "对收集的文献进行筛选、去重和主题分类，建立文献矩阵。",
				Prompt:       "你现在处于【文献筛选与分类】阶段。请对文献列表进行筛选和分类，输出：纳入/排除统计（PRISMA 流程图描述）、按主题/方法/时间的分类体系、文献矩阵表（文献 × 研究问题/方法/结论的交叉对照）、每篇纳入文献的一句话核心贡献。使用 Markdown 格式。",
				Deliverable:  "文献筛选报告与分类矩阵（Markdown 格式，含 PRISMA 统计、分类体系、文献矩阵）",
				Checklist:    []string{"筛选标准是否与选题报告的排除标准一致", "分类体系是否逻辑清晰且互斥", "文献矩阵是否覆盖所有纳入文献", "每篇文献的核心贡献是否准确概括"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "content_extraction",
				Name:         "核心内容提取与分析",
				Description:  "深入阅读纳入文献，提取关键发现、方法论和研究空白。",
				Prompt:       "你现在处于【核心内容提取与分析】阶段。请对每个主题分类下的文献进行深入分析，输出：各主题的研究现状综合（共识、争议、趋势）、主要方法论对比、关键发现汇总表、研究空白和未来方向识别。使用 Markdown 格式，必要时用表格对比。",
				Deliverable:  "内容分析报告（Markdown 格式，含各主题综合分析、方法对比、发现汇总、研究空白）",
				Checklist:    []string{"是否识别了领域内的共识和争议", "方法论对比是否客观全面", "研究空白是否有文献证据支撑", "分析是否覆盖了所有主题分类"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "review_writing",
				Name:         "综述撰写",
				Description:  "基于分析结果撰写完整的文献综述文档。",
				Prompt:       "你现在处于【综述撰写】阶段。请基于前序阶段的所有产出物，撰写完整的文献综述，包含：引言（研究背景、综述目的、范围说明）、主体（按主题组织的文献分析、对比讨论）、研究空白与未来方向、结论、参考文献列表。使用学术写作风格，Markdown 格式。",
				Deliverable:  "完整文献综述文档（Markdown 格式，含引言、主体、研究空白、结论、参考文献）",
				Checklist:    []string{"综述结构是否完整（引言/主体/结论）", "主体部分是否按主题而非按文献逐篇罗列", "是否明确指出了研究空白和未来方向", "参考文献格式是否统一"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// researchReportTemplate returns the research report collection workflow template (5 phases, all doc_only).
func researchReportTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowResearchReport,
		Name:        "研报收集整理",
		Description: "适用于行业研究报告收集与整理的工作流，涵盖需求定义、信息源梳理、研报收集摘要、核心观点提炼和整合报告撰写五个阶段。",
		Keywords:    []string{"研报", "研究报告", "行业报告", "券商", "分析", "投研", "市场研究", "行业分析", "调研", "数据", "趋势"},
		Phases: []PhaseTemplate{
			{
				ID:           "requirement_scoping",
				Name:         "需求定义与范围",
				Description:  "明确研报收集的行业/主题、时间范围、关注维度和输出要求。",
				Prompt:       "你现在处于【需求定义与范围】阶段。请根据用户意图，输出研报收集需求文档，包含：目标行业/主题、关注的子领域或公司、时间范围、关注维度（市场规模、竞争格局、技术趋势、政策影响等）、期望的输出格式和深度、优先关注的信息源类型（券商研报、咨询公司、政府报告等）。使用 Markdown 格式。",
				Deliverable:  "研报收集需求文档（Markdown 格式，含行业/主题、维度、时间范围、信息源偏好）",
				Checklist:    []string{"目标行业/主题是否明确", "关注维度是否覆盖用户核心需求", "时间范围是否合理", "输出格式和深度要求是否清晰"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "source_mapping",
				Name:         "信息源梳理",
				Description:  "梳理和评估可用的研报来源，建立信息源清单。",
				Prompt:       "你现在处于【信息源梳理】阶段。请梳理该行业/主题的主要研报来源，输出信息源清单，包含：券商研究所（国内/国际）、咨询公司（麦肯锡/BCG/贝恩等）、行业协会、政府机构、专业数据库、公开渠道。每个来源标注：覆盖范围、更新频率、可获取性、权威性评级。使用 Markdown 格式。",
				Deliverable:  "信息源清单（Markdown 格式，含来源名称、覆盖范围、权威性评级、获取方式）",
				Checklist:    []string{"信息源是否覆盖了主流券商和咨询公司", "是否包含中英文来源", "权威性评级是否有依据", "获取方式是否可操作"},
				NeedsConfirm: true,
				CanSkip:      true,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "report_collection",
				Name:         "研报收集与摘要",
				Description:  "按需求收集研报，提取每份报告的核心摘要信息。",
				Prompt:       "你现在处于【研报收集与摘要】阶段。请模拟收集过程，为每份研报输出结构化摘要卡片，包含：报告标题、发布机构、发布日期、核心观点（3-5 条）、关键数据点、评级/目标（如适用）、与需求维度的关联标签。按时间倒序排列。使用 Markdown 格式。",
				Deliverable:  "研报摘要集（Markdown 格式，每份研报含标题、机构、日期、核心观点、关键数据）",
				Checklist:    []string{"摘要是否覆盖了需求文档中的所有维度", "核心观点提取是否准确", "关键数据点是否有具体数字", "是否按时间倒序排列"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "insight_extraction",
				Name:         "核心观点提炼与对比",
				Description:  "跨研报提炼共识观点、分歧观点和独特洞察，进行横向对比。",
				Prompt:       "你现在处于【核心观点提炼与对比】阶段。请跨所有收集的研报进行横向分析，输出：各维度的共识观点（多数机构一致的判断）、分歧观点（机构间的不同看法及理由）、独特洞察（仅少数机构提出但有价值的观点）、关键数据对比表（不同机构对同一指标的预测对比）、趋势判断汇总。使用 Markdown 格式。",
				Deliverable:  "观点提炼报告（Markdown 格式，含共识/分歧/独特洞察、数据对比表、趋势判断）",
				Checklist:    []string{"共识观点是否有多个来源支撑", "分歧观点是否列出了各方理由", "数据对比表是否对齐了同一指标", "趋势判断是否有逻辑推导"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "synthesis_report",
				Name:         "整合报告撰写",
				Description:  "将所有分析结果整合为一份结构化的研究整合报告。",
				Prompt:       "你现在处于【整合报告撰写】阶段。请基于前序阶段的所有产出物，撰写完整的研究整合报告，包含：执行摘要（一页纸核心结论）、行业/主题概览、各维度深度分析（市场/竞争/技术/政策等）、关键数据图表描述、风险与机会、结论与建议、附录（研报来源列表）。使用专业研究报告风格，Markdown 格式。",
				Deliverable:  "研究整合报告（Markdown 格式，含执行摘要、各维度分析、风险机会、结论建议、附录）",
				Checklist:    []string{"执行摘要是否在一页内概括核心结论", "各维度分析是否有数据支撑", "风险与机会是否具体可操作", "附录是否列出了所有引用的研报来源"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// experimentDesignTemplate returns the experiment design workflow template (5 phases, all doc_only).
func experimentDesignTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowExperimentDesign,
		Name:        "实验方案设计",
		Description: "适用于科学实验方案设计的工作流，涵盖假设提出、实验设计、变量控制、数据采集和数据分析计划五个阶段。",
		Keywords:    []string{"实验", "假设", "变量", "对照", "样本", "实验设计", "控制组", "随机", "重复性"},
		Phases: []PhaseTemplate{
			{
				ID:           "hypothesis_formulation",
				Name:         "假设提出与研究问题",
				Description:  "明确研究问题，提出可验证的科学假设。",
				Prompt:       "你现在处于【假设提出与研究问题】阶段。请根据用户的研究意图，输出研究问题与假设文档，包含：研究背景概述、核心研究问题（1-3 个）、对应的科学假设（零假设与备择假设）、假设的理论依据、预期结果方向。使用 Markdown 格式。",
				Deliverable:  "研究问题与假设文档（Markdown 格式，含研究背景、研究问题、科学假设、理论依据）",
				Checklist:    []string{"研究问题是否明确且可操作", "假设是否可证伪", "零假设与备择假设是否清晰对应", "理论依据是否有文献支撑"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "experiment_design",
				Name:         "实验设计与方法选择",
				Description:  "选择实验类型和方法，设计实验整体方案。",
				Prompt:       "你现在处于【实验设计与方法选择】阶段。请基于研究假设，设计实验方案，包含：实验类型选择（随机对照/准实验/前后对比等）及理由、实验组与对照组设置、实验流程步骤、所需仪器设备和材料清单、伦理考量（如适用）。使用 Markdown 格式。",
				Deliverable:  "实验设计方案（Markdown 格式，含实验类型、分组设置、流程步骤、设备材料、伦理考量）",
				Checklist:    []string{"实验类型是否匹配研究问题", "实验组与对照组设置是否合理", "实验流程是否可重复执行", "所需设备和材料是否已列明"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "variable_control",
				Name:         "变量控制与样本规划",
				Description:  "定义自变量、因变量和控制变量，规划样本量和抽样方法。",
				Prompt:       "你现在处于【变量控制与样本规划】阶段。请详细定义实验变量和样本方案，包含：自变量定义与操作化、因变量定义与测量方式、控制变量列表及控制方法、样本量估算（含效应量和统计功效）、抽样方法和纳入/排除标准。使用 Markdown 格式。",
				Deliverable:  "变量与样本规划文档（Markdown 格式，含变量定义、测量方式、样本量估算、抽样方法）",
				Checklist:    []string{"自变量操作化是否具体可执行", "因变量测量方式是否可靠有效", "控制变量是否充分识别", "样本量估算是否有统计依据", "抽样方法是否避免选择偏倚"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "data_collection",
				Name:         "数据采集方案",
				Description:  "设计数据采集流程、工具和质量控制措施。",
				Prompt:       "你现在处于【数据采集方案】阶段。请设计数据采集方案，包含：数据采集工具和仪器、采集流程和时间节点、数据记录格式和模板、质量控制措施（盲法、随机化等）、数据存储和备份方案。使用 Markdown 格式。",
				Deliverable:  "数据采集方案（Markdown 格式，含采集工具、流程、记录格式、质量控制、存储方案）",
				Checklist:    []string{"采集工具是否经过验证", "采集流程是否标准化", "质量控制措施是否覆盖关键环节", "数据存储方案是否安全可靠"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "analysis_plan",
				Name:         "数据分析计划",
				Description:  "制定数据分析策略，明确统计方法和结果呈现方式。",
				Prompt:       "你现在处于【数据分析计划】阶段。请制定数据分析计划，包含：数据清洗和预处理步骤、描述性统计方案、推断性统计方法选择及理由、显著性水平设定、结果呈现方式（图表类型）、敏感性分析计划。使用 Markdown 格式。",
				Deliverable:  "数据分析计划（Markdown 格式，含预处理步骤、统计方法、显著性水平、结果呈现、敏感性分析）",
				Checklist:    []string{"统计方法是否匹配数据类型和研究设计", "显著性水平是否合理设定", "是否包含敏感性分析", "结果呈现方式是否清晰直观"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// grantProposalTemplate returns the grant proposal workflow template (5 phases, all doc_only).
func grantProposalTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowGrantProposal,
		Name:        "基金申请书",
		Description: "适用于科研基金申请书撰写的工作流，涵盖选题论证、研究现状、研究方案、预期成果和经费预算五个阶段。",
		Keywords:    []string{"基金", "课题", "申请", "申报", "国自然", "NSFC", "项目书", "立项", "经费", "资助"},
		Phases: []PhaseTemplate{
			{
				ID:           "topic_justification",
				Name:         "选题论证",
				Description:  "论证选题的科学意义、应用价值和研究必要性。",
				Prompt:       "你现在处于【选题论证】阶段。请撰写选题论证部分，包含：研究背景与科学问题、选题的理论意义和应用价值、国内外研究差距、本项目的切入点和独特视角、拟解决的关键科学问题。使用 Markdown 格式，语言严谨学术化。",
				Deliverable:  "选题论证文档（Markdown 格式，含研究背景、科学意义、应用价值、研究差距、关键问题）",
				Checklist:    []string{"科学问题是否明确且有研究价值", "理论意义和应用价值是否充分论证", "国内外研究差距是否有文献支撑", "切入点是否体现创新性"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "research_status",
				Name:         "研究现状与文献基础",
				Description:  "综述国内外研究现状，建立文献基础和理论框架。",
				Prompt:       "你现在处于【研究现状与文献基础】阶段。请撰写研究现状综述，包含：国际研究进展（按主题分类）、国内研究进展、现有研究的不足和空白、申请人及团队的前期研究基础、本项目与前期工作的关联。使用 Markdown 格式，引用关键文献。",
				Deliverable:  "研究现状综述（Markdown 格式，含国际/国内进展、研究空白、前期基础、关键文献）",
				Checklist:    []string{"文献综述是否覆盖近五年核心文献", "国内外进展是否分类清晰", "研究空白是否与选题直接关联", "前期基础是否体现团队能力"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "research_plan",
				Name:         "研究方案与技术路线",
				Description:  "设计详细的研究方案、技术路线和实施计划。",
				Prompt:       "你现在处于【研究方案与技术路线】阶段。请设计研究方案，包含：研究目标（总目标和分目标）、研究内容（分课题/模块）、技术路线图（流程描述）、关键技术和难点分析、实施计划和年度安排。使用 Markdown 格式，技术路线可用流程描述。",
				Deliverable:  "研究方案文档（Markdown 格式，含研究目标、内容、技术路线、难点分析、年度计划）",
				Checklist:    []string{"研究目标是否具体可考核", "研究内容是否覆盖所有关键问题", "技术路线是否逻辑清晰可行", "关键技术难点是否有应对方案", "年度安排是否合理"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "expected_outcomes",
				Name:         "预期成果与创新点",
				Description:  "明确预期研究成果和项目的创新点。",
				Prompt:       "你现在处于【预期成果与创新点】阶段。请撰写预期成果和创新点，包含：预期研究成果（论文、专利、软件著作权等）、成果的学术贡献和应用前景、项目的创新点（理论创新、方法创新、应用创新）、与同类研究的差异化优势。使用 Markdown 格式。",
				Deliverable:  "预期成果与创新点文档（Markdown 格式，含预期成果、学术贡献、创新点、差异化优势）",
				Checklist:    []string{"预期成果是否具体可量化", "创新点是否有充分论据支撑", "学术贡献是否与研究目标对应", "应用前景是否切合实际"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "budget_plan",
				Name:         "经费预算与进度安排",
				Description:  "编制经费预算和详细的研究进度安排。",
				Prompt:       "你现在处于【经费预算与进度安排】阶段。请编制经费预算和进度安排，包含：经费预算明细（设备费、材料费、测试费、差旅费、劳务费、间接费用等）、各项经费的测算依据、年度经费分配、研究进度甘特图描述、各阶段的考核指标。使用 Markdown 格式。",
				Deliverable:  "经费预算与进度文档（Markdown 格式，含预算明细、测算依据、年度分配、进度安排、考核指标）",
				Checklist:    []string{"预算科目是否符合基金管理规定", "测算依据是否合理", "年度经费分配是否与研究进度匹配", "考核指标是否可量化"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// paperWritingTemplate returns the paper writing workflow template (5 phases, all doc_only).
func paperWritingTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowPaperWriting,
		Name:        "学术论文撰写",
		Description: "适用于学术论文撰写的工作流，涵盖大纲构思、方法论撰写、结果呈现、讨论分析和投稿准备五个阶段。",
		Keywords:    []string{"论文", "撰写", "写论文", "投稿", "期刊", "SCI", "摘要", "引言", "方法", "结果", "讨论"},
		Phases: []PhaseTemplate{
			{
				ID:           "outline_design",
				Name:         "大纲构思与结构设计",
				Description:  "构思论文整体框架，设计章节结构和逻辑主线。",
				Prompt:       "你现在处于【大纲构思与结构设计】阶段。请设计论文大纲，包含：论文标题（中英文）、摘要框架（背景-目的-方法-结果-结论）、关键词列表、章节结构（引言、相关工作、方法、实验/结果、讨论、结论）、每章节的核心论点和逻辑关系、目标期刊/会议建议。使用 Markdown 格式。",
				Deliverable:  "论文大纲（Markdown 格式，含标题、摘要框架、章节结构、核心论点、目标期刊）",
				Checklist:    []string{"论文标题是否准确反映研究内容", "章节结构是否符合目标期刊要求", "各章节逻辑关系是否连贯", "核心论点是否支撑研究结论"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "methodology",
				Name:         "方法论撰写",
				Description:  "撰写研究方法部分，确保可重复性和严谨性。",
				Prompt:       "你现在处于【方法论撰写】阶段。请撰写方法论部分，包含：研究设计概述、数据来源和采集方法、实验/分析方法详细描述、算法或模型说明（如适用）、评估指标和基准选择、实验环境和参数设置。使用学术写作风格，Markdown 格式。",
				Deliverable:  "方法论章节（Markdown 格式，含研究设计、数据来源、方法描述、评估指标、实验设置）",
				Checklist:    []string{"方法描述是否足够详细以支持重复实验", "数据来源是否清晰可追溯", "评估指标选择是否合理且有依据", "实验参数是否完整记录"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "results_presentation",
				Name:         "结果呈现",
				Description:  "组织和呈现研究结果，设计图表和数据展示。",
				Prompt:       "你现在处于【结果呈现】阶段。请组织研究结果的呈现，包含：主要实验结果描述、结果图表设计建议（图表类型、标注要素）、统计分析结果（显著性、置信区间等）、与基准方法的对比分析、补充实验或消融实验结果。使用 Markdown 格式，图表用文字描述。",
				Deliverable:  "结果章节（Markdown 格式，含实验结果、图表设计、统计分析、对比分析、消融实验）",
				Checklist:    []string{"结果是否客观呈现未选择性报告", "图表设计是否清晰易读", "统计分析是否完整（含效应量和置信区间）", "与基准的对比是否公平合理"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "discussion_analysis",
				Name:         "讨论与分析",
				Description:  "深入讨论结果的意义、局限性和未来方向。",
				Prompt:       "你现在处于【讨论与分析】阶段。请撰写讨论部分，包含：主要发现的解读和意义、与已有研究的对比讨论、结果的理论和实践启示、研究局限性分析、未来研究方向建议、结论总结。使用学术写作风格，Markdown 格式。",
				Deliverable:  "讨论与结论章节（Markdown 格式，含发现解读、对比讨论、启示、局限性、未来方向、结论）",
				Checklist:    []string{"讨论是否紧扣研究问题和结果", "与已有研究的对比是否客观", "局限性分析是否诚实全面", "未来方向是否具体可行"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "submission_prep",
				Name:         "投稿准备",
				Description:  "完成投稿前的最终准备工作，包括格式检查和材料整理。",
				Prompt:       "你现在处于【投稿准备】阶段。请完成投稿准备工作，包含：摘要定稿（中英文）、关键词最终确认、参考文献格式检查、作者信息和贡献声明、Cover Letter 草稿、投稿清单（目标期刊要求逐项核对）、补充材料清单。使用 Markdown 格式。",
				Deliverable:  "投稿准备文档（Markdown 格式，含摘要定稿、参考文献、Cover Letter、投稿清单）",
				Checklist:    []string{"摘要是否符合目标期刊字数要求", "参考文献格式是否统一正确", "Cover Letter 是否突出论文贡献", "投稿清单是否逐项核对完成"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// projectProposalTemplate returns the project proposal workflow template (5 phases, all doc_only).
func projectProposalTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowProjectProposal,
		Name:        "项目立项方案",
		Description: "适用于项目立项方案编写的工作流，涵盖背景分析、目标定义、方案设计、资源评估和风险预案五个阶段。",
		Keywords:    []string{"立项", "方案", "项目方案", "提案", "可行性", "评审", "审批", "预算"},
		Phases: []PhaseTemplate{
			{
				ID:           "background_analysis",
				Name:         "背景分析与问题定义",
				Description:  "分析项目背景，明确要解决的核心问题。",
				Prompt:       "你现在处于【背景分析与问题定义】阶段。请分析项目背景，包含：行业/业务背景概述、当前存在的问题和痛点、问题的影响范围和严重程度、利益相关方分析、项目发起的驱动因素。使用 Markdown 格式。",
				Deliverable:  "背景分析报告（Markdown 格式，含行业背景、问题痛点、影响分析、利益相关方、驱动因素）",
				Checklist:    []string{"问题描述是否基于事实和数据", "影响范围是否量化评估", "利益相关方是否全面识别", "驱动因素是否清晰有说服力"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "goal_definition",
				Name:         "目标定义与范围界定",
				Description:  "定义项目目标，明确项目范围和边界。",
				Prompt:       "你现在处于【目标定义与范围界定】阶段。请定义项目目标和范围，包含：项目愿景和总目标、SMART 分目标列表、项目范围（包含和不包含的内容）、成功标准和验收条件、项目约束条件（时间、预算、技术等）。使用 Markdown 格式。",
				Deliverable:  "目标与范围文档（Markdown 格式，含项目目标、SMART 分目标、范围界定、成功标准、约束条件）",
				Checklist:    []string{"目标是否符合 SMART 原则", "范围边界是否清晰明确", "成功标准是否可量化验证", "约束条件是否全面考虑"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "solution_design",
				Name:         "方案设计",
				Description:  "设计项目实施方案，包括技术方案和实施路径。",
				Prompt:       "你现在处于【方案设计】阶段。请设计项目实施方案，包含：整体解决方案概述、技术方案或实施路径、方案对比分析（如有多个候选方案）、关键里程碑和交付物、实施步骤和阶段划分。使用 Markdown 格式。",
				Deliverable:  "方案设计文档（Markdown 格式，含解决方案、技术路径、方案对比、里程碑、实施步骤）",
				Checklist:    []string{"方案是否直接解决已定义的问题", "技术路径是否可行", "里程碑是否具体可衡量", "实施步骤是否逻辑清晰"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "resource_assessment",
				Name:         "资源评估与排期",
				Description:  "评估项目所需资源，制定详细的时间排期。",
				Prompt:       "你现在处于【资源评估与排期】阶段。请评估项目资源需求，包含：人力资源需求（角色、人数、技能要求）、预算估算（分类明细）、技术资源和基础设施需求、项目时间线和甘特图描述、关键路径分析。使用 Markdown 格式。",
				Deliverable:  "资源与排期文档（Markdown 格式，含人力需求、预算估算、技术资源、时间线、关键路径）",
				Checklist:    []string{"人力资源是否匹配项目需求", "预算估算是否有明确依据", "时间线是否考虑了依赖关系", "关键路径是否已识别"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "risk_contingency",
				Name:         "风险预案",
				Description:  "识别项目风险，制定应对策略和应急预案。",
				Prompt:       "你现在处于【风险预案】阶段。请制定风险管理计划，包含：风险识别清单（技术风险、人员风险、进度风险、外部风险）、风险评估矩阵（概率 × 影响）、每项风险的应对策略（规避/转移/缓解/接受）、应急预案和触发条件、风险监控机制。使用 Markdown 格式。",
				Deliverable:  "风险预案文档（Markdown 格式，含风险清单、评估矩阵、应对策略、应急预案、监控机制）",
				Checklist:    []string{"风险是否覆盖技术、人员、进度等维度", "风险评估是否有概率和影响分级", "应对策略是否具体可执行", "应急预案是否有明确触发条件"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// eventPlanningTemplate returns the event planning workflow template (5 phases, all doc_only).
func eventPlanningTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowEventPlanning,
		Name:        "活动策划",
		Description: "适用于各类活动策划的工作流，涵盖需求确认、方案策划、流程设计、物料清单和执行手册五个阶段。",
		Keywords:    []string{"活动", "策划", "会议", "年会", "发布会", "沙龙", "论坛", "团建", "庆典", "展会"},
		Phases: []PhaseTemplate{
			{
				ID:           "requirement_confirm",
				Name:         "需求确认与目标设定",
				Description:  "确认活动需求，明确活动目标和基本参数。",
				Prompt:       "你现在处于【需求确认与目标设定】阶段。请确认活动需求，包含：活动类型和主题、活动目标（品牌曝光/客户转化/团队凝聚等）、目标受众和预期人数、时间和地点要求、预算范围、特殊需求和限制条件。使用 Markdown 格式。",
				Deliverable:  "活动需求文档（Markdown 格式，含活动类型、目标、受众、时间地点、预算、特殊需求）",
				Checklist:    []string{"活动目标是否明确可衡量", "目标受众是否清晰定义", "预算范围是否合理", "时间地点要求是否可行"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "scheme_planning",
				Name:         "方案策划",
				Description:  "设计活动整体方案，包括主题创意和核心环节。",
				Prompt:       "你现在处于【方案策划】阶段。请设计活动方案，包含：活动主题和视觉概念、核心环节设计（开场/主体/互动/收尾）、嘉宾和演讲安排、互动环节设计（游戏/抽奖/问答等）、宣传推广计划、预期效果和评估指标。使用 Markdown 格式。",
				Deliverable:  "活动方案（Markdown 格式，含主题概念、核心环节、嘉宾安排、互动设计、宣传计划）",
				Checklist:    []string{"主题是否与活动目标一致", "核心环节是否有吸引力", "互动设计是否适合目标受众", "宣传计划是否覆盖目标渠道"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "process_design",
				Name:         "流程设计与时间线",
				Description:  "设计活动详细流程和时间安排。",
				Prompt:       "你现在处于【流程设计与时间线】阶段。请设计活动流程，包含：活动当天详细时间表（精确到分钟）、各环节负责人和职责、场地布置方案和动线设计、技术保障需求（音响/灯光/投影/网络）、签到和离场流程。使用 Markdown 格式。",
				Deliverable:  "活动流程文档（Markdown 格式，含时间表、负责人、场地方案、技术需求、签到流程）",
				Checklist:    []string{"时间安排是否留有缓冲", "各环节衔接是否顺畅", "技术保障是否覆盖所有需求", "动线设计是否合理"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "material_checklist",
				Name:         "物料清单与供应商",
				Description:  "列出所需物料清单，确定供应商和采购计划。",
				Prompt:       "你现在处于【物料清单与供应商】阶段。请编制物料清单，包含：物料分类清单（场地布置/宣传物料/礼品/餐饮/技术设备）、每项物料的规格数量和预算、供应商推荐和联系方式、采购时间节点、物料验收标准。使用 Markdown 格式。",
				Deliverable:  "物料清单文档（Markdown 格式，含物料分类、规格数量、预算、供应商、采购时间）",
				Checklist:    []string{"物料是否覆盖活动所有环节", "预算是否在总预算范围内", "采购时间是否留有余量", "是否有备选供应商"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "execution_manual",
				Name:         "执行手册",
				Description:  "编制活动执行手册，确保团队可按手册独立执行。",
				Prompt:       "你现在处于【执行手册】阶段。请编制执行手册，包含：团队分工和通讯录、活动前准备检查清单（D-7/D-3/D-1）、活动当天执行 SOP（标准操作流程）、应急预案（天气/设备故障/人员缺席/安全事故）、活动后复盘和总结模板。使用 Markdown 格式。",
				Deliverable:  "执行手册（Markdown 格式，含团队分工、准备清单、执行 SOP、应急预案、复盘模板）",
				Checklist:    []string{"分工是否明确到具体人员", "准备清单是否按时间倒排", "应急预案是否覆盖常见风险", "SOP 是否足够详细可独立执行"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// competitiveAnalysisTemplate returns the competitive analysis workflow template (5 phases, all doc_only).
func competitiveAnalysisTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowCompetitiveAnalysis,
		Name:        "竞品分析",
		Description: "适用于竞品分析的工作流，涵盖分析目标定义、竞品识别、多维度对比、差异分析和策略建议五个阶段。",
		Keywords:    []string{"竞品", "竞争", "对比", "分析", "竞争对手", "市场份额", "差异化", "SWOT"},
		Phases: []PhaseTemplate{
			{
				ID:           "target_definition",
				Name:         "分析目标与维度定义",
				Description:  "明确竞品分析的目标、范围和评估维度。",
				Prompt:       "你现在处于【分析目标与维度定义】阶段。请定义竞品分析框架，包含：分析目的（产品定位/功能规划/市场策略等）、分析范围（直接竞品/间接竞品/潜在竞品）、评估维度定义（产品功能/用户体验/定价策略/市场表现/技术架构等）、数据来源和收集方法、输出要求。使用 Markdown 格式。",
				Deliverable:  "分析框架文档（Markdown 格式，含分析目的、范围、评估维度、数据来源、输出要求）",
				Checklist:    []string{"分析目的是否与业务决策直接关联", "评估维度是否全面且可量化", "竞品范围是否合理界定", "数据来源是否可靠可获取"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "competitor_identification",
				Name:         "竞品识别与信息收集",
				Description:  "识别目标竞品，系统收集各竞品的基础信息。",
				Prompt:       "你现在处于【竞品识别与信息收集】阶段。请识别和收集竞品信息，包含：竞品列表（名称、公司、成立时间、融资阶段）、每个竞品的产品概述和定位、目标用户群体、核心功能列表、定价模式、市场份额或用户规模估算。使用 Markdown 格式，建议用表格对比。",
				Deliverable:  "竞品信息卡片集（Markdown 格式，含竞品列表、产品概述、功能、定价、市场数据）",
				Checklist:    []string{"竞品选择是否覆盖直接和间接竞争者", "信息收集是否基于可靠来源", "产品定位描述是否准确客观", "市场数据是否标注了来源和时间"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "dimension_comparison",
				Name:         "多维度对比分析",
				Description:  "按定义的维度对各竞品进行系统对比分析。",
				Prompt:       "你现在处于【多维度对比分析】阶段。请进行多维度对比，包含：功能对比矩阵（功能 × 竞品的支持情况）、用户体验对比（交互设计、易用性、性能）、定价策略对比（价格区间、计费模式、性价比）、技术架构对比（如可获取）、市场表现对比（增长趋势、用户评价）。使用 Markdown 格式，多用表格。",
				Deliverable:  "多维度对比报告（Markdown 格式，含功能矩阵、体验对比、定价对比、技术对比、市场对比）",
				Checklist:    []string{"功能对比矩阵是否覆盖核心功能", "对比标准是否统一客观", "定价对比是否考虑了不同套餐", "数据是否标注了获取时间"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "gap_analysis",
				Name:         "差异分析与洞察提炼",
				Description:  "分析竞品间的差异，提炼关键洞察和机会点。",
				Prompt:       "你现在处于【差异分析与洞察提炼】阶段。请进行差异分析，包含：SWOT 分析（自身产品 vs 主要竞品）、竞争优势和劣势总结、市场空白和机会点识别、用户未满足需求分析、竞品发展趋势预判。使用 Markdown 格式。",
				Deliverable:  "差异分析报告（Markdown 格式，含 SWOT 分析、优劣势总结、机会点、未满足需求、趋势预判）",
				Checklist:    []string{"SWOT 分析是否客观有据", "机会点是否有市场验证潜力", "劣势分析是否诚实不回避", "趋势预判是否有逻辑支撑"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "strategy_recommendation",
				Name:         "策略建议",
				Description:  "基于分析结果提出具体的竞争策略和行动建议。",
				Prompt:       "你现在处于【策略建议】阶段。请提出竞争策略建议，包含：差异化定位建议、功能优先级建议（应加强/应新增/可放弃的功能）、定价策略调整建议、市场进入或扩张策略、短期快赢行动项和长期战略方向、持续监控指标和竞品跟踪计划。使用 Markdown 格式。",
				Deliverable:  "策略建议文档（Markdown 格式，含差异化定位、功能优先级、定价建议、市场策略、行动项）",
				Checklist:    []string{"建议是否直接基于分析结论", "功能优先级是否有明确依据", "短期行动项是否可立即执行", "长期策略是否与公司愿景一致"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
		},
	}
}

// presentationDesignTemplate returns the PPT design & generation workflow template.
// The last phase (ppt_generation) uses ToolFilterFull to enable actual file generation
// via pptx-generator skill or generate_pdf tool.
func presentationDesignTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Type:        WorkflowPresentationDesign,
		Name:        "PPT 设计与生成",
		Description: "适用于演示文稿设计与生成的工作流，涵盖受众目标定义、内容大纲、风格规范、逐页脚本和 PPT 生成五个阶段。前四阶段确保内容和风格可控，最后阶段调用工具实际生成 PPT 文件。",
		Keywords:    []string{"PPT", "ppt", "演示", "幻灯片", "slide", "presentation", "汇报", "演讲", "路演", "keynote", "模板"},
		Phases: []PhaseTemplate{
			{
				ID:           "audience_goal",
				Name:         "受众与目标定义",
				Description:  "明确演示的目标受众、使用场景和核心信息。",
				Prompt:       "你现在处于【受众与目标定义】阶段。请明确 PPT 的定位，包含：目标受众（身份、专业程度、关注点）、演示场景（会议汇报/路演/培训/学术答辩等）、核心目标（说服/教育/汇报/展示）、关键信息（希望受众记住的 3-5 个要点）、时间限制和页数建议。使用 Markdown 格式。",
				Deliverable:  "受众与目标文档（Markdown 格式，含受众画像、场景、核心目标、关键信息、时间页数）",
				Checklist:    []string{"受众画像是否具体到可指导内容深度", "核心目标是否单一明确", "关键信息是否控制在 3-5 个", "时间和页数建议是否合理"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "content_outline",
				Name:         "内容大纲与逻辑线",
				Description:  "设计 PPT 的章节结构、逻辑主线和每页核心论点。",
				Prompt:       "你现在处于【内容大纲与逻辑线】阶段。请设计 PPT 内容大纲，包含：整体叙事逻辑（问题→方案→证据→行动 或 背景→现状→分析→建议等）、章节划分和每章标题、每页的核心论点（一页一个观点）、数据/案例/图表支撑计划、开场和结尾设计。使用 Markdown 格式，按页编号。",
				Deliverable:  "内容大纲（Markdown 格式，含叙事逻辑、章节结构、逐页论点、数据支撑计划）",
				Checklist:    []string{"叙事逻辑是否连贯有说服力", "每页是否只有一个核心论点", "数据和案例是否有来源", "开场是否能抓住注意力"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "style_specification",
				Name:         "风格与视觉规范",
				Description:  "定义 PPT 的视觉风格、配色方案、字体和版式规范。",
				Prompt:       "你现在处于【风格与视觉规范】阶段。请定义 PPT 的视觉规范，包含：整体风格定位（商务简约/科技感/学术严谨/创意活泼等）、配色方案（主色/辅色/强调色，含色值）、字体选择（标题字体/正文字体/中英文搭配）、版式规则（标题位置/正文区域/留白比例）、图表风格（柱状图/折线图/饼图的配色和样式）、品牌元素（Logo 位置、公司色等，如适用）。使用 Markdown 格式。",
				Deliverable:  "视觉规范文档（Markdown 格式，含风格定位、配色方案、字体、版式规则、图表风格、品牌元素）",
				Checklist:    []string{"配色方案是否和谐且有对比度", "字体选择是否兼顾可读性和美观", "版式规则是否统一一致", "风格是否匹配受众和场景"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "slide_scripting",
				Name:         "逐页脚本",
				Description:  "为每一页编写详细的内容脚本，包括标题、正文、图表描述和演讲备注。",
				Prompt:       "你现在处于【逐页脚本】阶段。请为每一页编写详细脚本，每页包含：页码和章节归属、页面标题、正文内容（控制字数，标题页≤20字，内容页≤80字）、图表/图片描述（类型、数据、标注）、动画/转场建议（如需要）、演讲备注（口头补充说明，100-200字）。使用 Markdown 格式，按页编号。",
				Deliverable:  "逐页脚本（Markdown 格式，每页含标题、正文、图表描述、动画建议、演讲备注）",
				Checklist:    []string{"每页文字量是否控制在规范内", "图表描述是否足够指导生成", "演讲备注是否补充了页面未展示的信息", "页面总数是否与目标一致"},
				NeedsConfirm: true,
				CanSkip:      false,
				ToolPolicy:   ToolFilterDocOnly,
			},
			{
				ID:           "ppt_generation",
				Name:         "PPT 生成",
				Description:  "基于逐页脚本和视觉规范，调用工具实际生成 PPT 文件。",
				Prompt:       "你现在处于【PPT 生成】阶段。请基于前序阶段的逐页脚本和视觉规范，使用可用的 PPT 生成工具（如 pptx-generator skill 或 generate_pdf）实际生成演示文稿文件。确保：遵循视觉规范的配色和字体、每页内容与脚本一致、图表按描述生成、文件格式为 PPTX 或 PDF。生成后发送给用户。",
				Deliverable:  "PPT 文件（PPTX 或 PDF 格式，内容和风格与前序文档一致）",
				Checklist:    []string{"配色和字体是否遵循视觉规范", "每页内容是否与逐页脚本一致", "图表是否按描述正确生成", "文件是否已发送给用户"},
				NeedsConfirm: false,
				CanSkip:      false,
				ToolPolicy:   ToolFilterFull,
			},
		},
	}
}
