package intent

// ---------------------------------------------------------------------------
// Unified intent definitions — single source of truth for all layers.
//
// Each IntentDefinition feeds:
//   - Layer 1: Keywords → KeywordRegistry
//   - Layer 2: EmbedTexts → intentAnchor vectors
//   - Layer 3: TreeText → Intent Tree LLM prompt
//   - Tool Affinity: ToolNames → tool activation
//
// Adding a new intent requires only adding one IntentDefinition here.
// All layers auto-update via BuildFromDefinitions().
// ---------------------------------------------------------------------------

// DefaultDefinitions returns the complete set of intent definitions for MacLaw.
// This consolidates data previously scattered across keyword_registry.go,
// layer2.go (anchors), tool_affinity.go, and layer3.go (LLM prompt).
func DefaultDefinitions() []IntentDefinition {
	return []IntentDefinition{
		{
			Label:              LabelCoding,
			Domain:             "编码开发 (Coding)",
			MayTriggerWorkflow: true, // coding workflow (needs → design → tasks → implement → review)
			WorkflowTypes:      []string{"coding", "maintenance"},
			TreeText: "用户要从零创建软件/应用/游戏/工具/脚本，需要完整开发流程。" +
				"语义判据：用户目标是创建、实现或修改软件系统、代码、游戏、前端或后端能力。",
			EmbedTexts: []string{
				"开发一个贪吃蛇游戏",
				"写一个爬虫程序",
				"帮我开发一个聊天应用",
				"实现一个REST API服务",
				"创建一个命令行工具",
				"写一个自动化脚本",
				"开发一个数据可视化面板",
				"build a web application",
				"create a CLI tool",
				"develop a REST API",
				"write a Python script for data processing",
				"implement a chat server",
				"build a game in JavaScript",
				"create a file upload service",
			},
			ToolNames: []string{"generate_pdf", "office"},
		},
		{
			Label:  LabelBugFix,
			Domain: "编码开发 (Coding)",
			TreeText: "用户要修复已有代码的 bug、调试崩溃、排查错误、解决异常。" +
				"语义判据：用户目标是定位并修复已有代码或应用的异常、错误、崩溃或不可用状态。",
			EmbedTexts: []string{
				"有bug，一直显示加载中",
				"修复崩溃问题",
				"页面白屏了",
				"程序闪退",
				"调试一下这个问题",
				"排查报错原因",
				"修复登录失败的bug",
				"fix the loading issue",
				"debug this crash",
				"the app keeps crashing on startup",
				"fix the authentication error",
				"there's a bug in the payment flow",
				"troubleshoot the memory leak",
				"resolve the null pointer exception",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelMaintenance,
			Domain: "编码开发 (Coding)",
			TreeText: "用户要重构、优化、清理、升级已有代码，不添加新功能。" +
				"语义判据：用户目标是在已有代码或系统上做结构、性能、依赖或质量改进。",
			EmbedTexts: []string{
				"重构这个函数",
				"优化性能",
				"清理无用代码",
				"升级依赖版本",
				"改善代码结构",
				"优化数据库查询速度",
				"refactor the auth module",
				"clean up dead code",
				"optimize the database queries",
				"upgrade the dependencies",
				"improve code readability",
				"reduce technical debt in the codebase",
			},
			ToolNames: []string{"generate_pdf", "office"},
		},
		{
			Label:  LabelSSH,
			Domain: "远程操作 (Remote)",
			TreeText: "用户要连接远程服务器、执行命令、查看日志、管理服务、操作远端主机。" +
				"语义判据：用户目标需要操作远程主机、服务进程、端口、容器、反向代理或服务器日志。",
			EmbedTexts: []string{
				"登录服务器查看日志",
				"连接远程服务器",
				"SSH到生产环境检查状态",
				"帮我登录服务器重启服务",
				"查看服务器上的GPU占用率",
				"远程执行命令查看磁盘空间",
				"connect to the server via SSH",
				"log into the production server",
				"check the server logs remotely",
				"restart the service on the remote machine",
				"SSH into the GPU server and check usage",
				"run a command on the remote host",
			},
			ToolNames: []string{"ssh"},
		},
		{
			Label:  LabelBrowser,
			Domain: "浏览器自动化 (Browser)",
			TreeText: "用户要自动化浏览器操作：导航网页、点击元素、填写表单、在浏览器中对网页截图、录制/回放浏览器操作。" +
				"语义判据：用户目标需要驱动网页、浏览器会话、录制回放、点击控件或填写网页表单。" +
				"注意：'截屏'/'截图'如果不涉及浏览器/网页，则是桌面截屏操作，不属于本类别。",
			EmbedTexts: []string{
				"打开浏览器访问这个网站",
				"帮我在网页上点击购买按钮",
				"用浏览器自动化填写表单",
				"录制浏览器操作步骤",
				"在Chrome中打开这个页面并截图",
				"自动化浏览器测试这个功能",
				"用playwright测试登录流程",
				"open the browser and navigate to this URL",
				"click the submit button on the web page",
				"automate the browser to fill in the form",
				"record browser actions for this workflow",
				"take a screenshot of this page in Chrome",
				"use playwright to test the login flow",
				"automate web testing with browser tools",
				"log into a website and publish a post",
				"sign in to a social website and submit content",
				"open Zhihu in a browser and publish a pin",
				"登录知乎并发表一条想法",
				"打开网页登录账号然后发布内容",
				"在网页上发帖并验证发布结果",
			},
			ToolNames: []string{
				"browser",
			},
		},
		{
			Label:  LabelComputerUse,
			Domain: "桌面操控 (Computer Use)",
			TreeText: "用户要操作本机桌面图形界面（GUI）：打开软件/程序/应用窗口、点击界面控件、" +
				"用键盘在桌面应用里输入、查看或截取屏幕内容。" +
				"语义判据：用户目标需要驱动本机桌面应用的窗口与控件，而不只是读写文件内容。" +
				"边界：「创建/生成/制作 Word、Excel、PPT 等文档内容」→ office，无需打开真实程序；" +
				"「浏览器/网页操作」→ browser；「纯文件读写或命令行即可完成的任务」→ non_coding 或 coding。",
			EmbedTexts: []string{
				"打开word程序写一份简历",
				"帮我在电脑上打开Excel程序",
				"打开记事本程序输入一段文字",
				"点击窗口上的确定按钮",
				"看看屏幕上现在显示了什么",
				"在桌面上把文件拖进文件夹",
				"操作桌面软件完成设置",
				"控制鼠标点击屏幕上的图标",
				"在应用窗口里输入文字",
				"打开电脑上的软件并操作界面",
				"把当前窗口最小化",
				"用键盘在桌面应用里按快捷键",
				"open the Word app and type a document",
				"click the OK button on the dialog",
				"operate the desktop GUI application",
				"type into the notepad window",
				"look at what is on the screen",
				"open the calculator app and click buttons",
				"drag a file into a folder on the desktop",
				"control the mouse and keyboard in a desktop app",
			},
			ToolNames: []string{
				"computer_observe",
				"computer_click",
				"computer_type",
				"computer_key",
				"computer_scroll",
				"computer_wait",
				"computer_focus",
				"computer_done",
				"computer_playbook",
			},
		},
		{
			Label:  LabelSearch,
			Domain: "内容处理 (Content)",
			TreeText: "用户要在网上搜索信息、文档、论文、解决方案。" +
				"语义判据：用户目标是获取相对稳定的网上资料、文档、论文、背景信息或解决方案。",
			EmbedTexts: []string{
				"搜索一下最新的AI论文",
				"帮我在网上查找这个问题的解决方案",
				"搜索关于机器学习的资料",
				"上网查一下这个API的文档",
				"帮我搜索这个错误信息",
				"网上找一下这个库的用法",
				"search the web for this error message",
				"look up the latest documentation for React",
				"find information about this API online",
				"search for solutions to this problem",
				"google this error and find a fix",
				"look up best practices for Go concurrency",
			},
			ToolNames: []string{"web_search", "web_fetch", "download_file"},
		},
		{
			Label:  LabelNonCoding,
			Domain: "内容处理 (Content)",
			TreeText: "用户要翻译、总结、整理、写文档等非编码内容处理任务，" +
				"或执行步骤明确的技术操作（逆向工程、模型转换、格式提取、数据迁移、环境部署等）。" +
				"共同特征：产出物由输入内容或操作步骤决定，不需要多阶段设计决策。" +
				"语义判据：用户目标是单阶段内容处理、格式转换、信息整理或明确步骤的技术操作。",
			EmbedTexts: []string{
				"翻译文档",
				"整理会议纪要",
				"总结这篇文章",
				"帮我整理资料",
				"生成PDF报告",
				"把这段话翻译成英文",
				"提取模型权重并转换为ONNX格式",
				"帮我把数据从MySQL迁移到PostgreSQL",
				"summarize this article",
				"translate this document",
				"organize meeting notes into a concise summary",
				"organize these notes",
				"help me write a report",
				"draft a project proposal document",
				"extract weights from the DLC model and convert to ONNX",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelLiveData,
			Domain: "Live external data",
			TreeText: "The user asks for current or recently changing external data that must be fetched from an external source, not answered from memory. " +
				"Examples include weather, exchange rates, stock or crypto prices, sports scores, flight or train status, delivery tracking, current news, live rankings, and other real-time public facts. " +
				"Do not use this label for deterministic local clock questions; use current_time for those.",
			EmbedTexts: []string{
				"What's the weather in Beijing today?",
				"Show me the current weather forecast for Shanghai.",
				"What's the USD to CNY exchange rate now?",
				"Check the latest stock price for Apple.",
				"Who won the game today?",
				"Track this flight status.",
				"Find the latest news about this company.",
				"\u5317\u4eac\u5929\u6c14",
				"\u67e5\u4e00\u4e0b\u4eca\u5929\u4e0a\u6d77\u7684\u5929\u6c14",
				"\u7f8e\u5143\u5bf9\u4eba\u6c11\u5e01\u6c47\u7387",
				"\u67e5\u6700\u65b0\u80a1\u4ef7",
				"\u67e5\u822a\u73ed\u72b6\u6001",
			},
			ToolNames: []string{"web_search", "web_fetch", "download_file"},
		},
		{
			Label:  LabelCurrentTime,
			Domain: "Deterministic lookup",
			TreeText: "The user asks for the current local date, time, weekday, or calendar facts that can be answered by a deterministic clock tool. " +
				"Do not use this label for weather, exchange rates, prices, stocks, flights, delivery tracking, or other live external data.",
			EmbedTexts: []string{
				"What time is it now?",
				"What is today's date?",
				"What day of the week is today?",
				"Tell me the current local time.",
				"Show current date and time.",
				"current time and date",
				"\u73b0\u5728\u51e0\u70b9",
				"\u4eca\u5929\u662f\u51e0\u53f7",
				"\u4eca\u5929\u5468\u51e0",
				"\u5f53\u524d\u65e5\u671f\u65f6\u95f4",
			},
			ToolNames: []string{"current_datetime"},
		},
		{
			Label:  LabelDocumentDelivery,
			Domain: "内容处理 (Content)",
			TreeText: "用户要打开、发送、导出文件/文档。" +
				"语义判据：用户目标是交付、打开、导出或发送具体文件/文档产物。",
			EmbedTexts: []string{
				"把这个文件发送给我",
				"打开桌面上的PDF文件",
				"帮我发送这份报告",
				"生成一份PDF文档并发给我",
				"打开这个Excel文件看看内容",
				"把结果导出为文件发送",
				"send me this file",
				"open the PDF document on my desktop",
				"deliver the report to me",
				"generate a PDF and send it over",
				"open this spreadsheet file",
				"export the results and send the file",
			},
			ToolNames: []string{"send_file", "send_to_im", "im_message", "open", "craft_tool", "download_file"},
		},
		{
			Label:  LabelBusinessData,
			Domain: "Business Data",
			TreeText: "The user wants to create, continue, validate, approve, query, or store structured enterprise business data through a runtime form or transaction workspace. " +
				"This includes expense reimbursement, travel reimbursement, purchase requests, leave requests, invoices, contracts, assets, customers, tickets, approvals, and unfinished business entries. " +
				"Do not decide the exact business object by keyword; this label only routes to the semantic MIS runtime, which resolves the object and action.",
			EmbedTexts: []string{
				"continue the unfinished expense reimbursement entry",
				"open my pending business transaction form",
				"submit a travel reimbursement with invoices",
				"create a purchase request and send it for approval",
				"record structured business data for an invoice",
				"validate this business form before committing it",
				"resume the saved approval transaction",
				"store this contract record in the business system",
				"fill in a leave request form",
				"query the local transaction workspace",
				"write structured MIS data from an agent-generated form",
				"collect required fields for an enterprise business object",
			},
			ToolNames: []string{"mis_data"},
		},
		{
			Label:              LabelOffice,
			Domain:             "内容处理 (Content)",
			MayTriggerWorkflow: true, // presentation_design workflow
			WorkflowTypes:      []string{"presentation_design"},
			TreeText: "用户要创建需要设计决策的演示文稿（PPT/幻灯片/slide）。" +
				"判据：产出物是否需要受众定位、内容架构、视觉风格等设计决策。" +
				"需要工作流：「生成/制作/设计 PPT」「基于文档做宣传PPT」「做投资人路演PPT」— 同一素材可产出截然不同的PPT，需要设计决策 → office + workflow_type。" +
				"不需要工作流：「打开/查看/转换/截图 PPT」— 文件操作，无设计决策 → document_delivery 或 non_coding。" +
				"注意：「基于已有素材」不等于「内容处理」。基于文档做PPT仍需要受众定位+内容取舍+风格设计等多阶段决策。" +
				"Excel/Word 等其他办公文档创建不需要工作流，workflow_type 留空。",
			EmbedTexts: []string{
				"帮我制作一个PPT演示文稿",
				"生成一份Excel报表",
				"创建一个Word文档",
				"把数据整理成Excel表格",
				"制作项目汇报PPT",
				"生成一份数据分析的电子表格",
				"create a PowerPoint presentation",
				"generate an Excel spreadsheet report",
				"make a Word document for the proposal",
				"organize the data into an Excel file",
				"build a slide deck for the meeting",
				"create a spreadsheet with the analysis results",
			},
			ToolNames: []string{"office"},
		},
		{
			Label:              LabelWorkflowTask,
			Domain:             "多阶段工作流 (Workflow)",
			MayTriggerWorkflow: true,
			WorkflowTypes: []string{
				"product_design", "innovation", "business_plan", "testing",
				"literature_review", "research_report", "experiment_design",
				"grant_proposal", "paper_writing", "project_proposal",
				"event_planning", "competitive_analysis",
				"bid_response", "contract_review", "due_diligence",
				"compliance_audit", "patent_analysis", "patent_application",
				"us_patent_application",
			},
			TreeText: "用户要启动一个需要多阶段设计决策的复杂项目。" +
				"核心判据：产出物是否需要多阶段设计决策？同一输入能否产出截然不同的结果？" +
				"是 → workflow_task。否 → non_coding。" +
				"workflow_type 选择指引：" +
				"product_design=产品设计/PRD, innovation=创新方案, business_plan=商业计划书/BP, " +
				"testing=测试方案, literature_review=文献综述, research_report=研究报告/研报, " +
				"experiment_design=实验方案, grant_proposal=基金申请, paper_writing=论文撰写, " +
				"project_proposal=项目立项, event_planning=活动策划, " +
				"competitive_analysis=竞品分析, bid_response=招投标, " +
				"contract_review=合同审查, due_diligence=尽职调查(对公司做商业评估), " +
					"compliance_audit=合规审计, patent_analysis=专利分析, " +
					"patent_application=中国专利申请/撰写(按发明、实用新型、外观设计类型准备申请文件), " +
					"us_patent_application=US patent/美国专利申请(USPTO filing, claims+specification in English)。",
			EmbedTexts: []string{
				"帮我做一份产品设计文档",
				"写一份商业计划书",
				"做一份竞品分析",
				"帮我写一篇文献综述",
				"帮我写一份研究报告",
				"帮我写一篇论文",
				"策划一个发布会活动",
				"帮我做一个项目立项方案",
				"对这家公司做个尽职调查",
				"审查一下这个合同",
				"分析一下这个专利的侵权风险",
				"检查一下我们的数据合规情况",
				"帮我分析这个招标文件准备投标",
				"设计一个实验方案",
				"写一份基金申请书",
				"做一个创新方案",
					"写一份测试方案",
					"帮我写一份专利申请书",
					"根据交底书撰写发明专利",
					"帮我准备一份实用新型专利申请文件",
					"帮我整理外观设计专利申请图片和简要说明",
					"file a US patent application",
				"draft USPTO patent claims based on this disclosure",
				"帮我申请美国专利",
				"write a business plan",
				"do a competitive analysis",
				"review this contract for risks",
				"conduct due diligence on this company",
				"write a literature review paper",
				"plan an event for product launch",
				"design a testing strategy",
				"draft a patent application from disclosure",
			},
			ToolNames: []string{"generate_pdf"},
		},
		{
			Label:  LabelContinuation,
			Domain: "特殊 (Special)",
			TreeText: "用户用短语表示继续或开始之前讨论的任务。" +
				"通常是 ≤5 个字的短消息，如「继续」「开工」「go ahead」。",
			EmbedTexts: []string{
				"继续",
				"开工",
				"开干",
				"动手",
				"搞起来",
				"开始吧",
				"let's go",
				"start working",
				"go ahead",
				"continue",
			},
			ToolNames: []string{},
		},
		{
			Label:    LabelKnowledgeWrite,
			Domain:   "Knowledge Base",
			TreeText: "The user is asking to persist supplied text, selected local files, local directories, or URLs into the MaClaw knowledge base / external brain / saved local corpus for future retrieval. This is a durable knowledge ingestion request, not a personal memory update and not a normal file-open/read task.",
			EmbedTexts: []string{
				"将这段材料录入知识库，后面可以检索引用",
				"把用户选择的本地 PDF 导入外脑，作为知识库资料",
				"把这个目录里的文档收录到本地知识库",
				"归档这个网页到知识库供以后查找",
				"save this note into the knowledge base for future retrieval",
				"ingest the selected local document files into the saved corpus",
				"archive this URL into my external brain",
				"store these documents in the local knowledge base",
			},
			ToolNames: []string{
				"knowledge_save_text",
				"knowledge_save_url", "knowledge_save_urls",
				"knowledge_import_files", "knowledge_import_directory",
			},
		},
		{
			Label:      LabelAmbiguous,
			Domain:     "特殊 (Special)",
			TreeText:   "消息不明确，可能属于多个类别，没有主导信号。",
			EmbedTexts: nil, // no anchors for ambiguous
			ToolNames:  []string{},
		},
		{
			Label:      LabelUnknown,
			Domain:     "特殊 (Special)",
			TreeText:   "不属于任何已知类别。",
			EmbedTexts: nil, // no anchors for unknown
			ToolNames:  []string{},
		},
	}
}

// PopulateKeywords attaches keywords from defaultKeywords to each definition
// by matching on Label. This bridges the existing keyword_registry.go data
// into the unified IntentDefinition structure without duplicating the ~200
// keyword entries.
//
// Call this on the result of DefaultDefinitions() to get fully populated defs.
func PopulateKeywords(defs []IntentDefinition) []IntentDefinition {
	// Group defaultKeywords by label.
	byLabel := make(map[IntentLabel][]KeywordEntry)
	for _, kw := range defaultKeywords {
		byLabel[kw.Label] = append(byLabel[kw.Label], kw)
	}

	for i := range defs {
		if kws, ok := byLabel[defs[i].Label]; ok {
			defs[i].Keywords = kws
		}
	}
	return defs
}

// FullDefinitions returns DefaultDefinitions with Keywords populated from
// defaultKeywords. This is the complete unified view of all intent data.
func FullDefinitions() []IntentDefinition {
	return PopulateKeywords(DefaultDefinitions())
}

// BuildKeywordsFromDefinitions extracts all KeywordEntry items from definitions.
func BuildKeywordsFromDefinitions(defs []IntentDefinition) []KeywordEntry {
	var entries []KeywordEntry
	for _, def := range defs {
		entries = append(entries, def.Keywords...)
	}
	return entries
}

// BuildAnchorsFromDefinitions constructs intentAnchor slices from definitions.
func BuildAnchorsFromDefinitions(defs []IntentDefinition) []intentAnchor {
	var anchors []intentAnchor
	for _, def := range defs {
		if len(def.EmbedTexts) == 0 {
			continue
		}
		anchors = append(anchors, intentAnchor{
			Label: def.Label,
			Texts: def.EmbedTexts,
		})
	}
	return anchors
}

// BuildToolAffinityFromDefinitions constructs the tool affinity mapping.
func BuildToolAffinityFromDefinitions(defs []IntentDefinition) map[IntentLabel][]string {
	m := make(map[IntentLabel][]string)
	for _, def := range defs {
		m[def.Label] = def.ToolNames
	}
	return m
}

// NewKeywordRegistryFromDefinitions creates a KeywordRegistry from definitions.
// This is an alternative to NewKeywordRegistry() that uses the unified
// IntentDefinition as the data source instead of the hardcoded defaultKeywords.
func NewKeywordRegistryFromDefinitions(defs []IntentDefinition) *KeywordRegistry {
	allKeywords := BuildKeywordsFromDefinitions(defs)
	return newKeywordRegistryFromEntries(allKeywords)
}

// NewToolAffinityRegistryFromDefinitions creates a ToolAffinityRegistry from definitions.
func NewToolAffinityRegistryFromDefinitions(defs []IntentDefinition) *ToolAffinityRegistry {
	return &ToolAffinityRegistry{
		mapping: BuildToolAffinityFromDefinitions(defs),
	}
}
