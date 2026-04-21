package intent

// ---------------------------------------------------------------------------
// Labeled calibration cases — extracted from production bug reports,
// intent_understanding.go examples, and coding_tool_gate tests.
//
// Each case maps a user message to the expected IntentLabel.
// Used by RunGridSearch to find optimal fusion parameters.
// ---------------------------------------------------------------------------

// ProductionCases returns labeled cases from real production scenarios
// documented in maclaw-improvements.md and test files.
func ProductionCases() []CalibrationCase {
	return []CalibrationCase{
		// =====================================================================
		// Coding (creation)
		// =====================================================================
		{Message: "开发一个贪吃蛇游戏", ExpectedLabel: LabelCoding, Note: "classic coding task"},
		{Message: "帮我开发一个聊天应用", ExpectedLabel: LabelCoding},
		{Message: "写一个爬虫程序", ExpectedLabel: LabelCoding},
		{Message: "实现一个REST API服务", ExpectedLabel: LabelCoding},
		{Message: "创建一个命令行工具", ExpectedLabel: LabelCoding},
		{Message: "帮我做一个CRM系统", ExpectedLabel: LabelCoding},
		{Message: "开发一个打飞机游戏，浏览器直接打开即玩，页面上有飞机和子弹", ExpectedLabel: LabelCoding, Note: "#34 browser误激活"},
		{Message: "build a web application", ExpectedLabel: LabelCoding},
		{Message: "create a CLI tool for file management", ExpectedLabel: LabelCoding},
		{Message: "develop a REST API with authentication", ExpectedLabel: LabelCoding},
		{Message: "开发一个bug追踪系统", ExpectedLabel: LabelCoding, Note: "#31 有bug但是创建类"},

		// =====================================================================
		// Bug fix
		// =====================================================================
		{Message: "有bug，一直显示加载中", ExpectedLabel: LabelBugFix, Note: "#31 bug fix bypass"},
		{Message: "修复崩溃问题", ExpectedLabel: LabelBugFix},
		{Message: "页面白屏了", ExpectedLabel: LabelBugFix},
		{Message: "修复加载错误", ExpectedLabel: LabelBugFix},
		{Message: "调试崩溃", ExpectedLabel: LabelBugFix},
		{Message: "排查报错", ExpectedLabel: LabelBugFix},
		{Message: "fix the loading issue", ExpectedLabel: LabelBugFix},
		{Message: "debug this crash", ExpectedLabel: LabelBugFix},
		{Message: "the app keeps crashing on startup", ExpectedLabel: LabelBugFix},

		// =====================================================================
		// Maintenance
		// =====================================================================
		{Message: "重构这个函数", ExpectedLabel: LabelMaintenance},
		{Message: "优化性能", ExpectedLabel: LabelMaintenance},
		{Message: "清理无用代码", ExpectedLabel: LabelMaintenance},
		{Message: "升级依赖版本", ExpectedLabel: LabelMaintenance},
		{Message: "改进优化下这个技能？", ExpectedLabel: LabelMaintenance, Note: "#39 maintenance误触发NeedsConfirm"},
		{Message: "refactor the auth module", ExpectedLabel: LabelMaintenance},
		{Message: "optimize the database queries", ExpectedLabel: LabelMaintenance},

		// =====================================================================
		// SSH
		// =====================================================================
		{Message: "登录服务器查看日志", ExpectedLabel: LabelSSH},
		{Message: "连接远程服务器", ExpectedLabel: LabelSSH},
		{Message: "帮我登录服务器重启服务", ExpectedLabel: LabelSSH},
		{Message: "查看服务器上的GPU占用率", ExpectedLabel: LabelSSH},
		{Message: "登录4090服务器查看GPU", ExpectedLabel: LabelSSH, Note: "#12 条件工具丢失"},
		{Message: "更新api服务器上的 ominiroute", ExpectedLabel: LabelSSH, Note: "#42/#44 服务器操作被劫持"},
		{Message: "SSH into the GPU server and check usage", ExpectedLabel: LabelSSH},
		{Message: "帮我关掉chrome", ExpectedLabel: LabelAmbiguous, Note: "#46 context-dependent: SSH if on server, bash if on desktop"},

		// =====================================================================
		// Browser
		// =====================================================================
		{Message: "打开浏览器帮我在网页上点击购买按钮", ExpectedLabel: LabelBrowser},
		{Message: "用浏览器自动化填写表单", ExpectedLabel: LabelBrowser},
		{Message: "录制浏览器操作步骤", ExpectedLabel: LabelBrowser},
		{Message: "用playwright测试登录流程", ExpectedLabel: LabelBrowser},
		{Message: "open the browser and navigate to this URL", ExpectedLabel: LabelBrowser},
		{Message: "automate web testing with browser tools", ExpectedLabel: LabelBrowser},

		// =====================================================================
		// Search
		// =====================================================================
		{Message: "搜索一下最新的AI论文", ExpectedLabel: LabelSearch},
		{Message: "帮我在网上查找这个问题的解决方案", ExpectedLabel: LabelSearch},
		{Message: "google this error and find a fix", ExpectedLabel: LabelSearch},

		// =====================================================================
		// Non-coding (content processing)
		// =====================================================================
		{Message: "翻译这段英文", ExpectedLabel: LabelNonCoding, Note: "IUM example: simple directive"},
		{Message: "什么是微服务架构", ExpectedLabel: LabelNonCoding, Note: "IUM example: knowledge query"},
		{Message: "帮我写一段Python排序代码", ExpectedLabel: LabelNonCoding, Note: "IUM example: single file snippet"},
		{Message: "怎么配置nginx", ExpectedLabel: LabelNonCoding, Note: "IUM example: how-to"},
		{Message: "看HF论文做摘要", ExpectedLabel: LabelNonCoding, Note: "IUM example: content processing"},
		{Message: "把这份报告翻译成英文", ExpectedLabel: LabelNonCoding},
		{Message: "整理这些会议纪要", ExpectedLabel: LabelNonCoding},
		{Message: "解读这篇论文的核心观点", ExpectedLabel: LabelNonCoding},
		{Message: "summarize this article", ExpectedLabel: LabelNonCoding},
		{Message: "translate this document to Chinese", ExpectedLabel: LabelNonCoding},
		{Message: "截屏桌面文件", ExpectedLabel: LabelNonCoding, Note: "#37 screenshot task"},
		{Message: "搜索论文", ExpectedLabel: LabelNonCoding},

		// =====================================================================
		// Document delivery
		// =====================================================================
		{Message: "把这个文件发送给我", ExpectedLabel: LabelDocumentDelivery},
		{Message: "帮我发送这份报告", ExpectedLabel: LabelDocumentDelivery},
		{Message: "把结果导出为文件发送", ExpectedLabel: LabelDocumentDelivery},

		// =====================================================================
		// Office
		// =====================================================================
		{Message: "帮我制作一个PPT演示文稿", ExpectedLabel: LabelOffice},
		{Message: "生成一份Excel报表", ExpectedLabel: LabelOffice},
		{Message: "制作项目汇报PPT", ExpectedLabel: LabelOffice},

		// =====================================================================
		// File operations — context-dependent, labeled by strongest signal
		// in the message itself. Without conversation context, these are
		// best-effort defaults.
		// =====================================================================
		{Message: "打开桌面上任何一个ppt文件并截图", ExpectedLabel: LabelNonCoding, Note: "#36 截图 is strong non_coding signal"},
		{Message: "打开这个PPT", ExpectedLabel: LabelAmbiguous, Note: "#36 could be doc_delivery or non_coding without context"},
		{Message: "把PPT转换成PDF", ExpectedLabel: LabelNonCoding, Note: "#36 format conversion is content processing"},
		{Message: "查看这个PPT的内容", ExpectedLabel: LabelNonCoding, Note: "#36 查看内容 is content processing"},

		// =====================================================================
		// Continuation
		// =====================================================================
		{Message: "开工", ExpectedLabel: LabelContinuation, Note: "#21 short action phrase"},
		{Message: "开干", ExpectedLabel: LabelContinuation},
		{Message: "继续", ExpectedLabel: LabelContinuation},
		{Message: "let's go", ExpectedLabel: LabelContinuation},
		{Message: "go ahead", ExpectedLabel: LabelContinuation},
		{Message: "直接做", ExpectedLabel: LabelContinuation, Note: "skip signal"},

		// =====================================================================
		// Ambiguous / mixed signals
		// =====================================================================
		{Message: "处理一下线上问题", ExpectedLabel: LabelAmbiguous, Note: "could be SSH or coding"},
		{Message: "看看服务", ExpectedLabel: LabelAmbiguous, Note: "could be SSH or monitoring"},
	}
}
