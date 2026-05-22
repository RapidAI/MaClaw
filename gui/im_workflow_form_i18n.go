package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func localizeWorkflowPhaseInputSchema(schema *workflow.PhaseInputSchema, lang string) *workflow.PhaseInputSchema {
	if schema == nil {
		return schema
	}
	localized := schema.Clone()
	localized.Title = localizedWorkflowText(localized.Title, localized.TitleI18N, lang)
	localized.Description = localizedWorkflowText(localized.Description, localized.DescriptionI18N, lang)
	for i := range localized.Fields {
		field := &localized.Fields[i]
		field.Label = localizedWorkflowText(field.Label, field.LabelI18N, lang)
		field.Description = localizedWorkflowText(field.Description, field.DescriptionI18N, lang)
		field.Placeholder = localizedWorkflowText(field.Placeholder, field.PlaceholderI18N, lang)
		for j := range field.Options {
			field.Options[j].Label = localizedWorkflowText(field.Options[j].Label, field.Options[j].LabelI18N, lang)
		}
	}
	return localized
}

func localizedWorkflowText(fallback string, values map[string]string, lang string) string {
	for _, key := range workflowI18NKeys(lang) {
		if text := strings.TrimSpace(values[key]); text != "" {
			return text
		}
	}
	if workflowI18NLanguageKind(lang).IsChinese() {
		return workflowFormZh(fallback)
	}
	return fallback
}

func workflowI18NLanguageKind(lang string) appLanguageKind {
	if strings.TrimSpace(lang) == "" {
		return appLanguageZhHans
	}
	return normalizeAppLanguageKind(lang)

}

func workflowI18NKeys(lang string) []string {
	trimmed := strings.TrimSpace(lang)
	langKind := workflowI18NLanguageKind(trimmed)
	normalized := langKind.TranslationTag()
	keys := []string{}
	if trimmed != "" {
		keys = append(keys, trimmed)
	}
	keys = append(keys, normalized)
	if langKind.IsChinese() {
		keys = append(keys, "zh")
	} else if langKind.IsEnglish() {
		keys = append(keys, "en")
	}
	return keys
}

func workflowFormZh(text string) string {
	if text == "" {
		return ""
	}
	if translated, ok := workflowFormZhText[text]; ok {
		return translated
	}
	return text
}

var workflowFormZhText = map[string]string{
	"Project intake": "项目需求采集",
	"Capture the product, platform, and implementation constraints before drafting requirements.": "在编写需求前，先收集产品、平台和实现约束。",
	"Project name": "项目名称",
	"Example: snake game, user management system": "例如：贪吃蛇游戏、用户管理系统",
	"Primary tech stack":                          "主要技术栈",
	"Target platform":                             "目标平台",
	"Build tool":                                  "构建工具",
	"Feature description":                         "功能描述",
	"Describe the expected features, gameplay, UI, interactions, and behavior.":       "描述预期功能、玩法、界面、交互和行为。",
	"Be specific about what the software should do and how success should be judged.": "请具体说明软件要做什么，以及如何判断结果成功。",
	"Special requirements": "特殊要求",
	"Performance, dependency limits, UI style, compatibility, security, or deployment requirements.": "性能、依赖限制、界面风格、兼容性、安全或部署要求。",
	"Project directory":                         "项目目录",
	"Example: D:\\workprj\\my-game":             "例如：D:\\workprj\\my-game",
	"Leave empty to use the current workspace.": "留空则使用当前工作区。",

	"Presentation brief": "演示文稿简报",
	"Capture the audience, goal, and content boundaries before planning slides.": "在规划幻灯片前，先收集受众、目标和内容边界。",
	"Presentation topic":                         "演示主题",
	"Example: Q3 product launch, thesis defense": "例如：Q3 产品发布、论文答辩",
	"Audience":                             "受众",
	"Talk length":                          "演讲时长",
	"Expected slide count":                 "预计页数",
	"Suggested: 10-30":                     "建议：10-30",
	"Style preference":                     "风格偏好",
	"Key points":                           "关键要点",
	"List 3-5 points the deck must cover.": "列出 3-5 个必须覆盖的要点。",
	"These points become the backbone of the presentation.": "这些要点会作为演示文稿主线。",
	"Additional notes": "补充说明",
	"Brand colors, logo, references, tone, forbidden content, or source material.": "品牌色、Logo、参考资料、语气、禁忌内容或源材料。",

	"Innovation brief": "创新方案简报",
	"Capture the domain, pain points, and available resources before opportunity analysis.": "在机会分析前，先收集领域、痛点和可用资源。",
	"Target domain or industry":                         "目标领域或行业",
	"Example: smart home, online education, new energy": "例如：智能家居、在线教育、新能源",
	"Innovation type":                                   "创新类型",
	"Known pain points or opportunities":                "已知痛点或机会",
	"Describe observed customer needs, market gaps, or technology opportunities.": "描述观察到的客户需求、市场空白或技术机会。",
	"Available resources": "可用资源",
	"Constraints":         "约束条件",
	"Budget, timeline, technical, compliance, channel, or operational constraints.": "预算、时间、技术、合规、渠道或运营约束。",

	"Business plan brief": "商业计划简报",
	"Capture the business plan context before market and financial planning.": "在市场和财务规划前，先收集商业计划背景。",
	"Project or company name":                    "项目或公司名称",
	"Example: AI customer support SaaS platform": "例如：AI 客服 SaaS 平台",
	"Target reader":                              "目标读者",
	"Company stage":                              "公司阶段",
	"Industry":                                   "所属行业",
	"Example: artificial intelligence, healthcare, renewable energy": "例如：人工智能、医疗健康、可再生能源",
	"Funding amount": "融资金额",
	"Example: 5M RMB, 10M RMB, or leave empty if not applicable": "例如：500 万人民币、1000 万人民币；不适用可留空",
	"Document depth":  "文档深度",
	"Project summary": "项目摘要",
	"In 2-3 sentences, explain what it does, what problem it solves, and who it serves.": "用 2-3 句话说明它做什么、解决什么问题、服务谁。",

	"Testing brief": "测试方案简报",
	"Capture the system under test, scope, and risk focus before creating the test strategy.": "在制定测试策略前，先收集被测系统、范围和风险重点。",
	"Project or system name":                "项目或系统名称",
	"Example: user management system v2.0":  "例如：用户管理系统 v2.0",
	"Test scope":                            "测试范围",
	"Test method":                           "测试方式",
	"Tech stack":                            "技术栈",
	"Example: React + Node.js + PostgreSQL": "例如：React + Node.js + PostgreSQL",
	"Testing focus":                         "测试重点",
	"Describe important modules, known issues, risky flows, and special scenarios.": "描述重要模块、已知问题、风险流程和特殊场景。",

	"Literature review brief": "文献综述简报",
	"Capture the research topic, questions, and search boundaries before review planning.": "在规划综述前，先收集研究主题、问题和检索边界。",
	"Research topic or field":                            "研究主题或领域",
	"Example: large language models for code generation": "例如：面向代码生成的大语言模型",
	"Research questions":                                 "研究问题",
	"List 1-3 questions the review should answer.":       "列出综述需要回答的 1-3 个问题。",
	"Time range":      "时间范围",
	"Source language": "来源语言",
	"Search keywords": "检索关键词",
	"List English and Chinese keywords, separated by commas. Leave empty to let the system derive them.": "列出中英文关键词，用逗号分隔；留空则由系统推导。",

	"Research report brief": "研究报告简报",
	"Capture the industry, dimensions, and desired depth before research planning.": "在研究规划前，先收集行业、分析维度和期望深度。",
	"Target industry or topic": "目标行业或主题",
	"Example: new energy vehicles, semiconductors, AI foundation models": "例如：新能源汽车、半导体、AI 基础模型",
	"Focus areas":        "关注维度",
	"Companies to watch": "重点关注公司",
	"List important companies to analyze, if any.": "如有，请列出需要重点分析的公司。",
	"Output depth": "输出深度",

	"Experiment design brief": "实验设计简报",
	"Capture the research question, experiment type, and constraints before design.": "在设计实验前，先收集研究问题、实验类型和约束。",
	"Research field": "研究领域",
	"Example: materials science, psychology, computer vision": "例如：材料科学、心理学、计算机视觉",
	"Research question": "研究问题",
	"Describe the question or hypothesis the experiment should test.": "描述实验要检验的问题或假设。",
	"Experiment type": "实验类型",
	"Describe equipment, samples, datasets, budget, collaborators, or facilities.":   "描述设备、样本、数据集、预算、合作方或设施。",
	"Timeline, ethics, sample size, data access, compliance, or safety constraints.": "时间、伦理、样本量、数据访问、合规或安全约束。",

	"Grant proposal brief": "基金申请简报",
	"Capture the grant type, field, background, and budget before proposal drafting.": "在撰写申请书前，先收集基金类型、领域、背景和预算。",
	"Project title": "项目题目",
	"Example: deep learning methods for protein structure prediction": "例如：面向蛋白质结构预测的深度学习方法",
	"Grant type": "基金类型",
	"Briefly describe the current foundation, scientific problem, and motivation.": "简要描述现有基础、科学问题和研究动机。",
	"Project duration (years)": "项目周期（年）",
	"Usually 3-5 years":        "通常 3-5 年",
	"Requested budget":         "申请预算",
	"Example: 300k RMB for youth fund, 800k RMB for general program": "例如：青年基金 30 万人民币，面上项目 80 万人民币",

	"Competitive analysis brief": "竞品分析简报",
	"Capture your product, competitors, and decision purpose before analysis.": "在分析前，先收集己方产品、竞品和决策目的。",
	"Our product or project":                      "己方产品或项目",
	"Example: our online collaboration tool":      "例如：我们的在线协作工具",
	"Main competitors":                            "主要竞品",
	"List competitors, one per line.":             "每行列出一个竞品。",
	"Provide at least 2-3 important competitors.": "请至少提供 2-3 个重要竞品。",
	"Analysis dimensions":                         "分析维度",
	"Purpose":                                     "目的",

	"Event planning brief": "活动策划简报",
	"Capture event goals, audience size, time, and budget before planning.": "在策划前，先收集活动目标、受众规模、时间和预算。",
	"Event name":                             "活动名称",
	"Example: 2026 annual technology summit": "例如：2026 年度技术峰会",
	"Event type":                             "活动类型",
	"Expected attendees":                     "预计人数",
	"Example: 200":                           "例如：200",
	"Planned time":                           "计划时间",
	"Example: mid May 2026, two days":        "例如：2026 年 5 月中旬，两天",
	"Budget range":                           "预算范围",
	"Example: 100k-200k RMB":                 "例如：10 万-20 万人民币",
	"Event goals":                            "活动目标",
	"Describe goals such as brand exposure, customer conversion, training, or team cohesion.": "描述品牌曝光、客户转化、培训或团队凝聚等目标。",

	"Project proposal brief": "项目建议书简报",
	"Capture the project problem, scope, stakeholders, budget, and timeline before proposal planning.": "在规划建议书前，先收集项目问题、范围、干系人、预算和时间。",
	"Project type":     "项目类型",
	"Problem to solve": "待解决问题",
	"Describe the current issue, pain point, or business need.": "描述当前问题、痛点或业务需求。",
	"Expected duration":                            "预计周期",
	"Example: 3 months, half a year":               "例如：3 个月、半年",
	"Budget estimate":                              "预算估算",
	"Example: 500k-1M RMB":                         "例如：50 万-100 万人民币",
	"Key stakeholders":                             "关键干系人",
	"List stakeholders and what each cares about.": "列出干系人及其关注点。",

	"Paper writing brief": "论文写作简报",
	"Capture the paper type, venue, contribution, and language before outline design.": "在设计大纲前，先收集论文类型、目标 venue、贡献和语言。",
	"Working title": "暂定标题",
	"Example: Transformer-based multimodal sentiment analysis": "例如：基于 Transformer 的多模态情感分析",
	"Paper type":   "论文类型",
	"Target venue": "目标期刊/会议",
	"Example: IEEE TPAMI, ACL 2026, Nature Communications": "例如：IEEE TPAMI、ACL 2026、Nature Communications",
	"A clear venue helps match format, rigor, and length.": "明确目标 venue 有助于匹配格式、严谨度和篇幅。",
	"Research type":     "研究类型",
	"Core contribution": "核心贡献",
	"Summarize the paper's main novelty and contribution in 1-3 sentences.": "用 1-3 句话概括论文主要创新点和贡献。",
	"Writing language":   "写作语言",
	"Existing materials": "已有材料",
	"Describe datasets, experiments, code, preliminary results, or notes already available.": "描述已有数据集、实验、代码、初步结果或笔记。",

	"Operations request brief": "运维请求简报",
	"Capture the target, environment, risk, and desired execution mode before planning operations work.": "在规划运维工作前，先收集目标、环境、风险和期望执行方式。",
	"Operation description": "操作描述",
	"Describe the action, such as cleaning /tmp, restarting nginx, or updating a Docker image.": "描述操作，例如清理 /tmp、重启 nginx 或更新 Docker 镜像。",
	"Target host": "目标主机",
	"Example: api.example.com, 192.168.1.100":        "例如：api.example.com, 192.168.1.100",
	"Use comma-separated values for multiple hosts.": "多个主机请用逗号分隔。",
	"Environment":        "环境",
	"Execution mode":     "执行方式",
	"Urgency":            "紧急程度",
	"Additional context": "补充上下文",
	"Relevant logs, alerts, previous actions, rollback expectations, or approvals.": "相关日志、告警、之前操作、回滚预期或审批信息。",

	"Changjiang Scholar applicant profile":                                                         "长江学者申请人信息",
	"Capture the applicant profile and academic highlights before drafting application materials.": "在起草申请材料前，先收集申请人信息和学术亮点。",
	"Applicant name":                    "申请人姓名",
	"Applicant full name":               "申请人全名",
	"Gender":                            "性别",
	"Birth date":                        "出生日期",
	"Example: May 1985":                 "例如：1985 年 5 月",
	"Application category":              "申报类别",
	"Discipline category":               "学科类别",
	"Research direction":                "研究方向",
	"Current institution":               "现任单位",
	"Example: XX University, XX School": "例如：XX 大学 XX 学院",
	"Current title":                     "现任职称",
	"Education background":              "教育背景",
	"List in chronological order:\nBachelor: XX University, major, years\nMaster: XX University, major, years\nPhD: XX University, major, years": "按时间顺序列出：\n本科：XX 大学，专业，年份\n硕士：XX 大学，专业，年份\n博士：XX 大学，专业，年份",
	"Include undergraduate, master's, and doctoral education when available.":                                                                    "如有，请包含本科、硕士和博士教育经历。",
	"Key academic achievements": "主要学术成果",
	"List 3-5 major achievements, such as high-impact papers, national projects, awards, or field contributions.": "列出 3-5 项主要成果，如高影响力论文、国家项目、奖项或领域贡献。",
	"Later phases can expand these; this field should capture the strongest highlights.":                          "后续阶段可展开，此处应提炼最强亮点。",
	"H-index":               "H 指数",
	"Example: 35":           "例如：35",
	"Total SCI/SSCI papers": "SCI/SSCI 论文总数",
	"Example: 120":          "例如：120",

	"Product design brief": "产品设计简报",
	"Capture the product direction, users, problem, and current stage before product design.": "在产品设计前，先收集产品方向、用户、问题和当前阶段。",
	"Product name or direction":                              "产品名称或方向",
	"Example: online whiteboard, AI customer support system": "例如：在线白板、AI 客服系统",
	"Product type": "产品类型",
	"Target users": "目标用户",
	"Describe who the users are, their characteristics, and their usage scenarios.": "描述用户是谁、有什么特征、使用场景是什么。",
	"Core problem": "核心问题",
	"What pain point do users face, and why are current solutions insufficient?": "用户面临什么痛点，为什么现有方案不够好？",
	"Known competitors": "已知竞品",
	"List known competitors or substitute solutions.": "列出已知竞品或替代方案。",
	"Current stage": "当前阶段",

	"C/C++":                                  "C/C++",
	"Python":                                 "Python",
	"Go":                                     "Go",
	"JavaScript/TypeScript":                  "JavaScript/TypeScript",
	"Java":                                   "Java",
	"Rust":                                   "Rust",
	"C#/.NET":                                "C#/.NET",
	"Other":                                  "其他",
	"Windows":                                "Windows",
	"macOS":                                  "macOS",
	"Linux":                                  "Linux",
	"Web browser":                            "Web 浏览器",
	"Mobile (Android/iOS)":                   "移动端（Android/iOS）",
	"Cross-platform":                         "跨平台",
	"Auto-select (recommended)":              "自动选择（推荐）",
	"CMake":                                  "CMake",
	"Makefile":                               "Makefile",
	"npm/yarn/pnpm":                          "npm/yarn/pnpm",
	"Gradle/Maven":                           "Gradle/Maven",
	"Cargo":                                  "Cargo",
	"go mod":                                 "go mod",
	"Internal team":                          "内部团队",
	"Clients or partners":                    "客户或合作伙伴",
	"Investors or executives":                "投资人或高管",
	"Academic committee":                     "学术委员会",
	"Public or media":                        "公众或媒体",
	"Training or teaching":                   "培训或教学",
	"5 minutes":                              "5 分钟",
	"10-15 minutes":                          "10-15 分钟",
	"20-30 minutes":                          "20-30 分钟",
	"45-60 minutes":                          "45-60 分钟",
	"Unknown":                                "未知",
	"Concise business":                       "简洁商务",
	"Technology-forward":                     "科技感",
	"Academic and rigorous":                  "学术严谨",
	"Creative and energetic":                 "创意活力",
	"No preference / auto":                   "无偏好 / 自动",
	"Product innovation":                     "产品创新",
	"Technology innovation":                  "技术创新",
	"Business model innovation":              "商业模式创新",
	"Process or efficiency innovation":       "流程或效率创新",
	"Service innovation":                     "服务创新",
	"Mixed or unknown":                       "混合或未知",
	"Individual or small team (1-3 people)":  "个人或小团队（1-3 人）",
	"Medium team (4-10 people)":              "中型团队（4-10 人）",
	"Large team or company":                  "大型团队或公司",
	"Investors (angel/VC/PE)":                "投资人（天使/VC/PE）",
	"Bank or loan reviewer":                  "银行或贷款审核方",
	"Government grant reviewer":              "政府补贴评审方",
	"Internal decision maker":                "内部决策者",
	"Partner or channel":                     "合作伙伴或渠道",
	"Concept only":                           "仅概念阶段",
	"Seed / MVP":                             "种子期 / MVP",
	"Growth with users or revenue":           "已有用户或收入的成长期",
	"Mature and profitable":                  "成熟盈利期",
	"Brief version (10-15 pages)":            "简版（10-15 页）",
	"Standard version (20-30 pages)":         "标准版（20-30 页）",
	"Detailed version (40+ pages)":           "详细版（40 页以上）",
	"Functional testing":                     "功能测试",
	"Performance testing":                    "性能测试",
	"Security testing":                       "安全测试",
	"Compatibility testing":                  "兼容性测试",
	"Regression testing":                     "回归测试",
	"API testing":                            "API 测试",
	"UI/UX testing":                          "UI/UX 测试",
	"Mostly manual":                          "以手工为主",
	"Mostly automated":                       "以自动化为主",
	"Manual and automated mix":               "手工与自动化结合",
	"Last 3 years":                           "近 3 年",
	"Last 5 years":                           "近 5 年",
	"Last 10 years":                          "近 10 年",
	"No limit":                               "不限",
	"English":                                "英文",
	"Chinese":                                "中文",
	"Last 1 month":                           "近 1 个月",
	"Last 3 months":                          "近 3 个月",
	"Last 6 months":                          "近 6 个月",
	"Last 1 year":                            "近 1 年",
	"Market size and growth":                 "市场规模与增长",
	"Competitive landscape":                  "竞争格局",
	"Technology trends":                      "技术趋势",
	"Policy impact":                          "政策影响",
	"Investment opportunities":               "投资机会",
	"Supply chain analysis":                  "供应链分析",
	"Overview with key conclusions and data": "概览版，包含关键结论和数据",
	"Standard with detailed analysis and comparisons": "标准版，包含详细分析和比较",
	"Deep with full data and chart plan":              "深度版，包含完整数据和图表规划",
	"Randomized controlled trial":                     "随机对照试验",
	"Quasi-experimental design":                       "准实验设计",
	"Pre/post comparison":                             "前后测对比",
	"Observational study":                             "观察性研究",
	"Simulation experiment":                           "仿真实验",
	"Other or unknown":                                "其他或未知",
	"NSFC Youth Fund":                                 "国家自然科学基金青年项目",
	"NSFC General Program":                            "国家自然科学基金面上项目",
	"NSFC Key Program":                                "国家自然科学基金重点项目",
	"Provincial or city research fund":                "省市级科研基金",
	"Enterprise collaboration project":                "企业合作项目",
	"Feature comparison":                              "功能对比",
	"Pricing strategy":                                "定价策略",
	"User experience":                                 "用户体验",
	"Technical architecture":                          "技术架构",
	"Market positioning":                              "市场定位",
	"Operations strategy":                             "运营策略",
	"Funding and team":                                "融资与团队",
	"Product planning reference":                      "产品规划参考",
	"Investment decision":                             "投资决策",
	"Market entry strategy":                           "市场进入策略",
	"Differentiation positioning":                     "差异化定位",
	"Conference or summit":                            "会议或峰会",
	"Product launch":                                  "产品发布会",
	"Team building or annual meeting":                 "团建或年会",
	"Training or workshop":                            "培训或工作坊",
	"Exhibition or roadshow":                          "展会或路演",
	"Online event or livestream":                      "线上活动或直播",
	"New product or system development":               "新产品或系统开发",
	"Existing system upgrade":                         "现有系统升级",
	"Infrastructure construction":                     "基础设施建设",
	"Process optimization or digital transformation":  "流程优化或数字化转型",
	"Research or exploratory project":                 "研究或探索型项目",
	"Journal paper":                                   "期刊论文",
	"Conference paper":                                "会议论文",
	"Thesis":                                          "学位论文",
	"Survey or review":                                "综述论文",
	"Short paper or letter":                           "短文或快报",
	"Experimental method and evaluation":              "实验方法与评估",
	"Theoretical proof or formalization":              "理论证明或形式化",
	"System design and engineering evaluation":        "系统设计与工程评估",
	"Survey or literature analysis":                   "综述或文献分析",
	"Case study":                                      "案例研究",
	"Development":                                     "开发环境",
	"Testing":                                         "测试环境",
	"Staging":                                         "预发布环境",
	"Production":                                      "生产环境",
	"Critical infrastructure":                         "关键基础设施",
	"Generate documentation or scripts only":          "仅生成文档或脚本",
	"Generate a plan and execute only after approval": "生成计划，审批后执行",
	"Auto-execute low-risk operations":                "自动执行低风险操作",
	"Routine maintenance":                             "例行维护",
	"Planned change":                                  "计划变更",
	"Urgent fix":                                      "紧急修复",
	"Incident response":                               "故障响应",
	"Male":                                            "男",
	"Female":                                          "女",
	"Distinguished professor":                         "特聘教授",
	"Young scholar":                                   "青年学者",
	"Chair professor":                                 "讲座教授",
	"Natural sciences":                                "自然科学",
	"Engineering":                                     "工程技术",
	"Humanities and social sciences":                  "人文社会科学",
	"Medicine":                                        "医学",
	"Professor / researcher":                          "教授 / 研究员",
	"Associate professor / associate researcher":      "副教授 / 副研究员",
	"Overseas associate professor or above":           "海外副教授及以上",
	"Web app":                                         "Web 应用",
	"Mobile app":                                      "移动应用",
	"Desktop software":                                "桌面软件",
	"Mini program":                                    "小程序",
	"SaaS platform":                                   "SaaS 平台",
	"Hardware product":                                "硬件产品",
	"Starting from concept":                           "从概念开始",
	"Initial idea exists":                             "已有初步想法",
	"MVP or prototype exists":                         "已有 MVP 或原型",
	"Launched and iterating":                          "已上线并迭代",
}
