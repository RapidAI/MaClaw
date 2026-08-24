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
				"打开 Chrome 点购买",
				"open Chrome and click Buy",
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
				"computer_select",
				"computer_scroll_into_view",
				"computer_drag",
				"computer_wait",
				"computer_focus",
				"computer_find",
				"computer_done",
				"computer_playbook",
			},
		},
		{
			Label:  LabelScreenshot,
			Domain: "Desktop capture",
			TreeText: "The user wants a still image of the current local desktop, a display, a monitor, or a screen region. " +
				"This is a capture-only request: use the screenshot tool rather than the full computer_use surface. " +
				"A request to capture a browser page in a browser remains browser; reading or sending an existing image file is document_delivery.",
			EmbedTexts: []string{
				"take a screenshot of my desktop",
				"capture the current screen",
				"grab the primary monitor",
				"take a screen capture and send it to me",
				"screenshot display one",
				"截图",
				"截屏",
				"截主屏",
				"截主屏幕",
				"截取主屏",
				"给我截一下当前桌面",
				"截取当前显示器画面",
			},
			ToolNames: []string{"screenshot"},
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
				"全网搜索这个人的资料",
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
				"What's the weather for a given location today?",
				"Show me the current weather forecast for a location.",
				"Retrieve live weather information for an arbitrary location.",
				"What's the USD to CNY exchange rate now?",
				"Check the latest stock price for Apple.",
				"Who won the game today?",
				"Track this flight status.",
				"Find the latest news about this company.",
				"\u67e5\u8be2\u67d0\u4e2a\u57ce\u5e02\u5f53\u524d\u5929\u6c14",
				"\u67e5\u8be2\u4efb\u610f\u57ce\u5e02\u4eca\u65e5\u5929\u6c14",
				"\u83b7\u53d6\u67d0\u5730\u5b9e\u65f6\u5929\u6c14\u4fe1\u606f",
				"\u7f8e\u5143\u5bf9\u4eba\u6c11\u5e01\u6c47\u7387",
				"\u67e5\u6700\u65b0\u80a1\u4ef7",
				"\u67e5\u822a\u73ed\u72b6\u6001",
			},
			ToolNames: []string{"web_search", "web_fetch", "download_file"},
		},
		{
			Label:  LabelLiveDataVisual,
			Domain: "实时数据可视化 (Live data visualization)",
			TreeText: "用户要求将当前实时外部事实渲染为一张图片或信息图。" +
				"语义判据：先取得天气、汇率、行情等当前数据，再依据这些可信事实生成 PNG 并交付当前会话。" +
				"排除：截取本机桌面 → screenshot；寻找或下载现成图片 → search/web_fetch；自由创作或文生图不是本能力。",
			EmbedTexts: []string{
				"生成一张实时天气实况图",
				"把当前汇率制作成信息图",
				"render a live weather graphic",
				"create an infographic from the current stock price",
			},
			ToolNames: []string{},
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
			TreeText: "用户要把已有文件发送到指定目标、邮箱、路径或另一会话。" +
				"语义判据：目标是交付到当前会话以外的收件人/位置，而不是本机打开或阅读附件。" +
				"边界：「用系统程序打开本地文档」→ document_open；「阅读已上传附件」→ document_read；" +
				"「把本轮附件发回当前渠道」→ attachment_delivery；「生成 PDF」→ document_generate。",
			EmbedTexts: []string{
				"把这个文件发送给我",
				"帮我发送这份报告",
				"把结果导出为文件发送",
				"send me this file",
				"deliver the report to me",
				"export the results and send the file",
			},
			ToolNames: []string{"send_file", "send_to_im", "im_message"},
		},
		{
			Label:  LabelDocumentGenerate,
			Domain: "内容处理 (Content)",
			TreeText: "用户要把当前已有事实渲染成一份 PDF 文件。" +
				"语义判据：目标是单阶段排版/导出 PDF，产出物由已收集的事实或用户给出的正文决定，不需要多阶段设计决策。" +
				"排除：撰写商业计划、研究报告、论文、竞品分析等需要受众/结构/论证设计的多阶段任务 → workflow_task；" +
				"打开已有本地文档 → document_open；发送到指定目标 → document_delivery；把本轮已上传附件原样发回 → attachment_delivery。",
			EmbedTexts: []string{
				"生成一份PDF文档并发给我",
				"生成pdf报告",
				"把这些内容生成PDF",
				"将已查得的实时信息输出为格式化PDF报告",
				"将已查到的公开数据保存为PDF文件",
				"以PDF格式导出当前已获取的信息",
				"将实时查询结果导出为格式化PDF报告",
				"把获取到的公开信息整理成PDF文件",
				"export pdf",
				"generate a PDF and send it over",
				"generate a PDF document",
				"render this as a PDF file",
				"make a PDF from these facts",
				"export current retrieved information as a formatted PDF report",
				"save fetched public data as a PDF file",
				"export live lookup results as a formatted PDF report",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelDocumentRead,
			Domain: "内容处理 (Content)",
			TreeText: "用户要读取、检查、提取、总结或回答其已提供的一个具体文档的内容。" +
				"语义判据：目标是对已有输入做只读理解；不包括创建、编辑、导出、打开桌面应用或向他人发送文件。" +
				"若没有受当前会话授权的文档输入，必须要求用户附上或明确选择文档，不能把路径文本当作文件授权。",
			EmbedTexts: []string{
				"读取我刚发的 PDF",
				"看看这个附件里写了什么",
				"提取这份 Word 文档的要点",
				"分析附件中的 Excel 表格",
				"打开这个Excel文件看看内容",
				"总结这份 PPT 的内容",
				"read the attached document",
				"summarize this PDF attachment",
				"inspect the spreadsheet I uploaded",
				"extract the text from this Word file",
				"review the contents of the attached presentation",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelDocumentOpen,
			Domain: "内容处理 (Content)",
			TreeText: "用户要用操作系统默认程序打开一份已有的本地文档。" +
				"语义判据：目标是让系统 handler 打开 PDF/Word/Excel/PPT 等文档本身，而不是阅读内容、发送文件或启动无关应用。" +
				"边界：「阅读附件内容」→ document_read；「发送到指定目标」→ document_delivery；" +
				"「打开应用/网址/文件夹」→ app_launch；「生成 PDF」→ document_generate。",
			EmbedTexts: []string{
				"打开桌面上的PDF文件",
				"用默认程序打开这个文档",
				"用系统看图软件打开这个PPT",
				"open the PDF document on my desktop",
				"open this document with the default app",
				"open the spreadsheet with the system handler",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelAttachmentDelivery,
			Domain: "Content",
			TreeText: "The user wants the assistant to deliver exactly one document or file already attached to the current conversation back through that same current channel. " +
				"This is a channel delivery outcome, not opening a local path, creating/exporting a document, choosing another recipient, or sending a file mentioned only in text.",
			EmbedTexts: []string{
				"把我刚发的附件回传给我",
				"把这个附件发回当前聊天",
				"把这份上传的报告发给我",
				"send back the file I attached",
				"deliver this uploaded attachment in this chat",
				"return the attached report to this conversation",
			},
			ToolNames: []string{},
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
				"bid_response", "bid_review", "contract_review", "due_diligence",
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
				"competitive_analysis=竞品分析, bid_response=招投标/写标书/生成投标文件(从招标文件起草), " +
				"bid_review=标书检查/审查已写好的标书并给修改建议(不是写新标书; 需对照招标标准), " +
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
				"检查一下我们的标书是否符合招标要求",
				"对照招标文件审查投标文件并给出修改建议",
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
				"切换到完整模式",
				"切换到完整agent模式",
				"用完整能力再做一次",
				"switch to full agent",
				"switch to full agent mode",
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
			Label:  LabelFileRead,
			Domain: "本地文件 (Local Files)",
			TreeText: "用户要查看、读取、搜索本机文件或目录的内容，目标就是获得这些本地内容本身。" +
				"语义判据：用户目标是对本机文件/目录做只读查看、读取、列目录或内容搜索。" +
				"边界：「开发/修改软件、读取代码是为了改代码」→ coding/bug_fix/maintenance；" +
				"「读取用户刚发的文档附件」→ document_read；「检索知识库」→ knowledge_read；" +
				"「查看 git 状态或差异」→ git_inspect；「读取指定 URL 的网页」→ web_fetch。",
			EmbedTexts: []string{
				"读取一下这个文件的内容",
				"看看 config.yaml 里写了什么",
				"列出当前目录下的文件",
				"在项目里搜索这个函数名出现在哪些文件",
				"帮我看一下日志文件的最后几行",
				"read the contents of this file",
				"show me what is in the README file",
				"list the files in this directory",
				"search the project files for this error message",
				"print the last lines of the local log file",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelFileWrite,
			Domain: "本地文件 (Local Files)",
			TreeText: "用户要把指定内容写入、保存到本机文件，或直接修改某个已有的本地文件。" +
				"语义判据：用户目标是创建/覆盖/追加/编辑一个具体的本地文件，产出物就是该文件本身。" +
				"边界：「从零开发软件/功能」→ coding；「生成 Word/Excel/PPT 办公文档」→ office；" +
				"「生成 PDF」→ document_generate；「把内容录入知识库」→ knowledge_write。",
			EmbedTexts: []string{
				"把这段内容保存到 notes.txt",
				"帮我在桌面创建一个文本文件",
				"修改 hosts 文件添加一条记录",
				"把这串配置写进 .env 文件",
				"在这个文件末尾追加一行",
				"save this text to a local file",
				"create a file called todo.md with this content",
				"edit the config file and change the port",
				"append this line to the end of the file",
				"overwrite the old notes file with this",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelShellCommand,
			Domain: "本地命令 (Local Shell)",
			TreeText: "用户要在本机直接执行 shell/命令行命令、运行脚本或管理直通任务。" +
				"语义判据：用户目标是在 MaClaw 所在设备上执行一条具体命令或脚本。" +
				"边界：「登录/操作远程服务器」→ ssh；「命令是手段、开发软件是目的」→ coding；" +
				"「查看 git 状态/差异」优先 → git_inspect。",
			EmbedTexts: []string{
				"在本机执行一下这个命令",
				"帮我运行这个脚本",
				"用命令行创建一个目录",
				"在本机跑一下 npm install",
				"执行 ping 测试看网络通不通",
				"run this shell command on my machine",
				"execute the script locally",
				"create a folder using the command line",
				"run the build command on this computer",
				"check the local disk usage with df",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelGitInspect,
			Domain: "版本控制 (VCS)",
			TreeText: "用户要查看本地 Git 仓库的只读状态：工作区状态、未提交变更、diff 差异。" +
				"语义判据：用户目标是只读检查版本控制状态或差异，不提交、不推送、不改历史。" +
				"边界：「提交/推送代码」→ git_mutate；「看代码文件内容」→ file_read。",
			EmbedTexts: []string{
				"看看当前 git 状态",
				"这个仓库有哪些未提交的改动",
				"帮我看下现在的 diff",
				"查看暂存区有什么变更",
				"check the git status of this repo",
				"show me the current diff",
				"what files have uncommitted changes",
				"review the staged changes",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelGitMutate,
			Domain: "版本控制 (VCS)",
			TreeText: "用户要把当前仓库的改动提交，或把已有提交推送到远程。" +
				"语义判据：用户目标就是这次版本控制写入本身（commit / push），改动已经存在，不需要再写代码。" +
				"边界：「先改代码再顺手提交」→ coding/bug_fix/maintenance（提交只是收尾）；" +
				"「只看状态或 diff」→ git_inspect；「回滚、改历史、切分支、合并」不在本类别（无对应受管能力）。",
			EmbedTexts: []string{
				"把这些改动提交一下",
				"提交代码，说明写修复登录问题",
				"帮我 commit 一下当前的修改",
				"把本地提交推送到远程",
				"commit the current changes",
				"push my commits to the remote",
				"commit these edits with a message",
				"git push this branch",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelAudioRecord,
			Domain: "音频 (Audio)",
			TreeText: "用户要开始用本机麦克风录音、录制会议或讨论。" +
				"语义判据：用户目标是采集新的音频（打开录音界面/开始录音）。" +
				"边界：「转写已有音频文件」→ audio_transcribe；「把文字在本机朗读」→ audio_synthesize。",
			EmbedTexts: []string{
				"开始录音",
				"帮我录一下这个会议",
				"打开录音功能",
				"录制一段语音",
				"start recording audio",
				"record this meeting",
				"open the microphone recorder",
				"capture a voice memo",
			},
			ToolNames: []string{"record_audio"},
		},
		{
			Label:  LabelAudioTranscribe,
			Domain: "音频 (Audio)",
			TreeText: "用户要把一个已有的音频文件转写成文字、做语音识别或生成会议纪要。" +
				"语义判据：用户目标是对已有音频文件做只读转写/识别。" +
				"边界：「开始新的录音」→ audio_record；「把文字在本机朗读」→ audio_synthesize；「总结文字内容」→ non_coding。",
			EmbedTexts: []string{
				"把这个音频文件转成文字",
				"转写一下这段录音",
				"识别这个语音文件里说了什么",
				"给这个会议录音生成文字稿",
				"transcribe this audio file",
				"convert the recording to text",
				"run speech recognition on this wav file",
				"generate a transcript of the meeting audio",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelAudioSynthesize,
			Domain: "音频 (Audio)",
			TreeText: "用户要把已有文字在本机朗读、合成为语音并在桌面或终端播放。" +
				"语义判据：用户目标是本机播放语音，而不是把语音发到群/人或转写已有音频。" +
				"边界：「开始新的录音」→ audio_record；「转写已有音频」→ audio_transcribe；" +
				"「把语音发到群/当前会话」→ audio_deliver；「发送已有文件」→ document_delivery，不是本机朗读。",
			EmbedTexts: []string{
				"把这段话念给我听",
				"朗读这段文字",
				"用语音把这段话播放出来",
				"在桌面上读出这段说明",
				"read this paragraph aloud",
				"speak this text on the desktop",
				"play this message as speech locally",
				"read the summary out loud",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelAudioDeliver,
			Domain: "Audio",
			TreeText: "The user wants spoken audio delivered to the current authenticated group or person." +
				"This is a voice-message send, not local playback and not sending an existing file." +
				"Boundaries: local read-aloud -> audio_synthesize; send an existing file -> document_delivery;" +
				"start recording -> audio_record. Destination comes only from the current session.",
			EmbedTexts: []string{
				"send this as a voice message",
				"deliver this text as speech to the group",
				"send a voice bubble with this text",
				"speak this and send it to the chat",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelWebFetch,
			Domain: "网页抓取 (Web Fetch)",
			TreeText: "用户给出了具体 URL，要抓取、读取该网页的正文内容。" +
				"语义判据：用户目标是读取一个明确给定的网页/链接的内容。" +
				"边界：「上网搜索/查找资料但没有具体 URL」→ search；「查天气股价等实时数据」→ live_data；" +
				"「用浏览器操作网页（点击/登录/填表）」→ browser。",
			EmbedTexts: []string{
				"抓取这个链接的内容",
				"帮我看看这个网页写了什么",
				"读取这个 URL 的正文",
				"把这个网址的内容提取出来",
				"fetch the content of this URL",
				"read this web page for me",
				"grab the article text from this link",
				"extract the main content of this page",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelAuditRead,
			Domain: "审计与诊断 (Audit)",
			TreeText: "用户要查询安全审计日志、搜索历史会话记录，或检查项目/系统健康状态。" +
				"语义判据：用户目标是只读查看审计、历史对话或健康诊断信息。" +
				"边界：「检索知识库内容」→ knowledge_read；「查看服务器日志」→ ssh；「读本地日志文件」→ file_read。",
			EmbedTexts: []string{
				"查一下最近的安全审计日志",
				"搜索我们之前的对话记录",
				"检查这个项目能否正常编译",
				"看看有没有高风险的工具调用记录",
				"show the audit log for today",
				"search my past conversation history",
				"check the project health status",
				"find when this tool was called before",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelKnowledgeRead,
			Domain: "Knowledge Base",
			TreeText: "The user wants to search, retrieve, or explain content already stored in the local knowledge base / external brain. " +
				"语义判据：用户目标是从知识库中只读检索、召回、查看已有内容或引用出处。" +
				"边界：「把新材料录入/导入知识库」→ knowledge_write；「读本地文件」→ file_read；" +
				"「搜索历史对话」→ audit_read；「上网搜索」→ search。",
			EmbedTexts: []string{
				"在知识库里查一下这个概念的笔记",
				"检索我的外脑中关于这个项目的资料",
				"知识库里有没有相关的引用出处",
				"从我的本地知识库找相关内容",
				"search my knowledge base for this topic",
				"look up the stored notes about this concept",
				"find relevant entries in my external brain",
				"recall what the knowledge base says about this",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelAppLaunch,
			Domain: "本机操作 (Local Host)",
			TreeText: "用户要用操作系统默认程序启动一个应用程序、打开一个网址或打开一个文件夹。" +
				"语义判据：用户目标是让系统 handler 启动某个程序/链接/目录本身。" +
				"边界：「用默认程序打开已有文档」→ document_open；「阅读附件内容」→ document_read；" +
				"「在浏览器里操作网页（点击/登录/填表）」→ browser；「下载 URL 到本地」→ file_download。",
			EmbedTexts: []string{
				"打开计算器应用",
				"用默认浏览器打开这个网址",
				"在资源管理器里打开这个文件夹",
				"帮我启动本地开发工具",
				"open this URL in the default browser",
				"launch the calculator app",
				"open this folder in the file explorer",
				"start the application with the system handler",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelFileDownload,
			Domain: "网页抓取 (Web Fetch)",
			TreeText: "用户要把一个远程 URL（文件、PDF、安装包等）下载保存到本机磁盘。" +
				"语义判据：用户目标是把远程资源落盘成本地文件，产出物是下载得到的文件。" +
				"边界：「只读取网页正文内容」→ web_fetch；「没有具体 URL 的查找」→ search；" +
				"「把已有文件发送给用户」→ document_delivery。",
			EmbedTexts: []string{
				"把这个链接的文件下载到本地",
				"下载这个 PDF 到工作目录",
				"帮我把这个安装包下载下来",
				"将这个网址的附件保存到磁盘",
				"download this file to my machine",
				"save the PDF from this URL to disk",
				"fetch this installer and store it locally",
				"download the attachment from this link",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelScheduleManage,
			Domain: "任务调度 (Schedule)",
			TreeText: "用户要创建、查看、修改、暂停、恢复或删除本机定时任务/提醒，且不要求到点向群或人推送。" +
				"语义判据：用户目标是管理按计划时间自动执行的本机任务项。" +
				"边界：「到点发给群/人/微信/蓝信」→ schedule_dispatch；「维护当前工作的待办任务清单」→ task_track；" +
				"「长期持续推进的目标」→ goal_manage；「问现在几点/今天几号」→ current_time。",
			EmbedTexts: []string{
				"每天早上九点提醒我站会",
				"创建一个每小时执行一次的定时任务",
				"暂停昨天的那个定时任务",
				"列出所有定时任务",
				"create a scheduled job that runs every morning",
				"pause my daily reminder task",
				"list all scheduled tasks",
				"delete the weekly backup schedule",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelScheduleDispatch,
			Domain: "任务调度外发 (Schedule Dispatch)",
			TreeText: "用户要创建或修改定时任务，并且要求到点把结果发给群、人、微信、蓝信或其他 IM 通道。" +
				"语义判据：用户目标包含按计划时间向会话外发，而不只是在本机留下任务。" +
				"边界：「只查看/创建/删除本地定时任务、不提发送对象」→ schedule_manage；" +
				"「现在立刻发到群/人」→ document_delivery 或即时消息，不是定时外发。",
			EmbedTexts: []string{
				"每天早上发给群里",
				"每天九点把日报推送到蓝信群",
				"创建一个定时任务到点发给张三",
				"每周一提醒并发送到微信",
				"send the standup to the group every morning",
				"schedule a daily push to the lansenger group",
				"create a reminder that messages the team at 9am",
				"deliver this report to the group on a schedule",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelConfigManage,
			Domain: "自身配置 (Agent Config)",
			TreeText: "用户要查看或修改 MaClaw 自身的配置、切换自己使用的 LLM 服务商/模型、调整推理轮数上限，或修正用户画像。" +
				"语义判据：用户目标是改变 agent 自身的设置、模型来源、运行上限或对用户的建模。" +
				"边界：「查看编码工具的 provider/会话」→ session_manage；「修改项目代码里的配置文件」→ file_write 或 coding。",
			EmbedTexts: []string{
				"把当前的 LLM 服务商切换成智谱",
				"修改一下配置里的主题设置",
				"你现在用的什么模型",
				"把最大推理轮数调到 50",
				"修正一下你的用户画像里我的偏好",
				"switch to a different LLM provider",
				"show the current configuration",
				"raise the max iteration limit",
				"correct my user profile preferences",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelMemoryManage,
			Domain: "记忆 (Memory)",
			TreeText: "用户要让 agent 记住、更新、查看或删除关于用户/项目的长期记忆条目。" +
				"语义判据：用户目标是维护 agent 记忆库中的条目（偏好、事实、决定）。" +
				"边界：「把材料录入知识库供检索」→ knowledge_write；「检索知识库」→ knowledge_read；" +
				"「维护定时提醒」→ schedule_manage。",
			EmbedTexts: []string{
				"记住我偏好用中文交流",
				"把刚才这个决定记到长期记忆里",
				"你都记住了我的哪些偏好",
				"删掉那条关于我地址的记忆",
				"remember that I prefer dark mode",
				"update your memory about this project",
				"what do you remember about me",
				"forget the note about my old address",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelTaskTrack,
			Domain: "任务跟踪 (Task Tracking)",
			TreeText: "用户要创建、更新、完成、列出或删除当前工作的本地任务清单条目（待办事项）。" +
				"语义判据：用户目标是维护一个任务/待办列表的状态本身，而不是执行某个任务。" +
				"边界：「定时自动执行的任务」→ schedule_manage；「长期自主推进的目标」→ goal_manage；" +
				"「直接动手做编码任务」→ coding。",
			EmbedTexts: []string{
				"帮我建一个待办清单",
				"把修复登录问题加到任务列表里",
				"把这个任务标记为已完成",
				"列出当前所有待办任务",
				"add this item to the task list",
				"mark this task as completed",
				"show my current todo list",
				"create a task to track the migration work",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelGoalManage,
			Domain: "长期目标 (Long-running Goal)",
			TreeText: "用户明确要求创建、查看或结束一个需要系统长期自主持续推进的目标（带预算/验收条件）。" +
				"语义判据：用户目标是管理一个持久化、跨多轮自动推进的长期目标，且用户明确提出。" +
				"边界：「多阶段设计类项目（PRD/论文/标书等）」→ workflow_task；「待办清单条目」→ task_track；" +
				"「定时执行的任务」→ schedule_manage；「普通编码任务」→ coding。",
			EmbedTexts: []string{
				"创建一个长期目标：持续监控这个服务的可用性",
				"设一个目标，每天自动整理我的收藏直到整理完",
				"查看当前长期目标的进展",
				"终止那个长期目标",
				"create a long-running goal to keep this documentation up to date",
				"show the status of my persistent goal",
				"mark this long-running goal as complete",
				"set up an autonomous goal with a token budget",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelTemplateManage,
			Domain: "会话模板 (Session Template)",
			TreeText: "用户要创建、列出或用模板启动会话模板（预设工具+项目路径+模型配置）。" +
				"语义判据：用户目标是管理可复用的会话模板本身。" +
				"边界：「管理正在运行的编码会话」→ session_manage；「修改 agent 自身配置」→ config_manage。",
			EmbedTexts: []string{
				"创建一个用 codex 的会话模板",
				"列出所有会话模板",
				"用之前那个模板启动一个新会话",
				"把这套配置保存成模板",
				"create a session template for code review",
				"list my session templates",
				"launch a session from the template",
				"save this setup as a reusable template",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelSessionManage,
			Domain: "会话管理 (Coding Session)",
			TreeText: "用户要列出、查看、驱动、中断或终止外部编码会话，查看编码工具的 provider，或管理项目列表。" +
				"语义判据：用户目标是管理编码会话/项目的生命周期与状态，而不是亲自完成编码工作。" +
				"边界：「让它开发/修改软件」→ coding；「委派子任务给子 agent」→ delegate_task；" +
				"「操作远程服务器」→ ssh；「切换 MaClaw 自己的模型」→ config_manage。",
			EmbedTexts: []string{
				"列出当前所有编码会话",
				"看看那个会话最近的输出",
				"给会话发一条输入让它继续",
				"中断正在运行的会话",
				"切换当前项目",
				"list my running coding sessions",
				"show the recent output of that session",
				"interrupt the stuck coding session",
				"which providers does this coding tool have",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelDelegateTask,
			Domain: "任务委派 (Delegation)",
			TreeText: "用户明确要求把任务委派给子 agent、并行执行多个任务，或组织多专家讨论。" +
				"语义判据：用户目标是通过委派/并行/会诊的编排方式完成工作，且用户明确提出这种编排。" +
				"边界：「直接开发/修改软件」→ coding；「管理编码会话」→ session_manage；" +
				"「把子任务加进待办清单」→ task_track。",
			EmbedTexts: []string{
				"把这个任务委派给编码子 agent",
				"并行执行这三个独立的任务",
				"组织一次专家讨论评估这个方案",
				"派一个子 agent 去处理这个子任务",
				"delegate this subtask to a specialized agent",
				"run these three tasks in parallel",
				"start a group discussion among experts about this design",
				"hand this piece off to a coding sub-agent",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelKnowledgeAdmin,
			Domain: "Knowledge Base",
			TreeText: "The user wants to administer or maintain the local knowledge base: refresh sources, run quality maintenance, enable/disable/delete sources, manage labels/links, suppress duplicates, or inspect store health/stats. " +
				"语义判据：用户目标是对知识库做管理、维护、治理或健康检查操作，而不是检索或录入内容。" +
				"边界：「检索知识库内容」→ knowledge_read；「把新材料录入知识库」→ knowledge_write；" +
				"「维护 agent 记忆」→ memory_manage。",
			EmbedTexts: []string{
				"刷新一下知识库里的这个来源",
				"禁用这个知识库数据源",
				"删除知识库里的这个来源",
				"跑一下知识库质量维护计划",
				"检查知识库的健康状况和统计",
				"refresh this knowledge source",
				"disable a stale knowledge base source",
				"run the knowledge quality maintenance plan",
				"show knowledge base stats and health",
			},
			ToolNames: []string{},
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
