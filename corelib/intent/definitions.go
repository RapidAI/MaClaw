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
			Label:  LabelCoding,
			Domain: "编码开发 (Coding)",
			MayTriggerWorkflow: true, // coding workflow (needs → design → tasks → implement → review)
			TreeText: "用户要从零创建软件/应用/游戏/工具/脚本，需要完整开发流程。" +
				"关键信号：开发、创建、实现、写代码、编程、游戏、前端、后端。",
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
				"关键信号：修复、调试、debug、报错、崩溃、白屏、闪退、卡住。",
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
				"关键信号：重构、优化、清理、升级、改善、refactor、optimize。",
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
				"关键信号：服务器、SSH、日志、docker、nginx、systemctl、远程、端口、进程。",
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
			TreeText: "用户要自动化浏览器操作：导航网页、点击元素、填写表单、截图网页、录制/回放浏览器操作。" +
				"关键信号：浏览器、browser、chrome、playwright、录制、回放、点击按钮、填写表单。",
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
			},
			ToolNames: []string{
				"browser_navigate", "browser_click", "browser_type",
				"browser_screenshot", "browser_scroll", "browser_wait",
				"browser_execute_js", "browser_select", "browser_hover",
				"browser_drag", "browser_upload", "browser_download",
				"browser_tab_new", "browser_tab_close", "browser_tab_switch",
				"browser_cookie_get", "browser_cookie_set", "browser_dialog_handle",
				"browser_pdf", "browser_network_intercept", "browser_evaluate",
				"browser_close", "browser_back", "browser_forward",
				"browser_refresh",
				"gui_record_start", "gui_record_stop",
			},
		},
		{
			Label:  LabelSearch,
			Domain: "内容处理 (Content)",
			TreeText: "用户要在网上搜索信息、文档、论文、解决方案。" +
				"关键信号：搜索、search、查找、google、论文、paper。",
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
			ToolNames: []string{"web_search"},
		},
		{
			Label:  LabelNonCoding,
			Domain: "内容处理 (Content)",
			TreeText: "用户要翻译、总结、整理、写文档、查天气等非编码任务。" +
				"这是一步完成的内容处理任务，不需要多阶段工作流。" +
				"关键信号：翻译、总结、摘要、整理、查天气、写邮件、截图。",
			EmbedTexts: []string{
				"翻译文档",
				"搜索论文",
				"总结这篇文章",
				"帮我整理资料",
				"生成PDF报告",
				"把这段话翻译成英文",
				"summarize this article",
				"translate this document",
				"search for papers on AI",
				"organize these notes",
				"help me write a report",
				"draft a project proposal document",
			},
			ToolNames: []string{},
		},
		{
			Label:  LabelDocumentDelivery,
			Domain: "内容处理 (Content)",
			TreeText: "用户要打开、发送、导出文件/文档。" +
				"关键信号：发送文件、打开文件、导出、附件、发给我。",
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
			ToolNames: []string{"send_file", "open", "craft_tool"},
		},
		{
			Label:  LabelOffice,
			Domain: "内容处理 (Content)",
			MayTriggerWorkflow: true, // presentation_design workflow
			TreeText: "用户要创建新的 PPT/Excel/Word 等办公文档。" +
				"关键信号：制作PPT、生成Excel、创建Word、做一份报表。",
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
			Label:  LabelAmbiguous,
			Domain: "特殊 (Special)",
			TreeText: "消息不明确，可能属于多个类别，没有主导信号。",
			EmbedTexts: nil, // no anchors for ambiguous
			ToolNames:  []string{},
		},
		{
			Label:  LabelUnknown,
			Domain: "特殊 (Special)",
			TreeText: "不属于任何已知类别。",
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
