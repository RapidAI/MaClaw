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
		{Message: "打开 Chrome 点购买", ExpectedLabel: LabelBrowser, Note: "web task must not go to computer_use"},
		{Message: "open Chrome and click Buy", ExpectedLabel: LabelBrowser},
		{Message: "打开浏览器帮我在网页上点击购买按钮", ExpectedLabel: LabelBrowser},
		{Message: "用浏览器自动化填写表单", ExpectedLabel: LabelBrowser},
		{Message: "录制浏览器操作步骤", ExpectedLabel: LabelBrowser},
		{Message: "用playwright测试登录流程", ExpectedLabel: LabelBrowser},
		{Message: "登录知乎发表一条码卡龙 6 发布感言", ExpectedLabel: LabelBrowser},
		{Message: "打开网页登录账号然后发布内容", ExpectedLabel: LabelBrowser},
		{Message: "open the browser and navigate to this URL", ExpectedLabel: LabelBrowser},
		{Message: "log into Zhihu and publish a post", ExpectedLabel: LabelBrowser},
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
		{Message: "搜索论文", ExpectedLabel: LabelSearch, Note: "paper lookup is external information retrieval, not local content processing"},

		// =====================================================================
		// Document delivery
		// =====================================================================
		{Message: "把这个文件发送给我", ExpectedLabel: LabelDocumentDelivery},
		{Message: "帮我发送这份报告", ExpectedLabel: LabelDocumentDelivery},
		{Message: "把结果导出为文件发送", ExpectedLabel: LabelDocumentDelivery},

		{Message: "打开桌面上的PDF文件", ExpectedLabel: LabelDocumentOpen},
		{Message: "用默认程序打开这个文档", ExpectedLabel: LabelDocumentOpen},
		{Message: "open the PDF document on my desktop", ExpectedLabel: LabelDocumentOpen},

		// =====================================================================
		// Document generate (single-stage PDF render; not a research workflow)
		// =====================================================================
		{Message: "生成一份PDF文档并发给我", ExpectedLabel: LabelDocumentGenerate},
		{Message: "generate a PDF and send it over", ExpectedLabel: LabelDocumentGenerate},
		{Message: "export pdf", ExpectedLabel: LabelDocumentGenerate},

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

		// =====================================================================
		// S2b1 governed families
		// =====================================================================
		{Message: "读取一下这个文件的内容", ExpectedLabel: LabelFileRead},
		{Message: "列出当前目录下的文件", ExpectedLabel: LabelFileRead},
		{Message: "show me what is in the README file", ExpectedLabel: LabelFileRead},

		{Message: "把这段内容保存到 notes.txt", ExpectedLabel: LabelFileWrite},
		{Message: "在这个文件末尾追加一行", ExpectedLabel: LabelFileWrite},
		{Message: "save this text to a local file", ExpectedLabel: LabelFileWrite},

		{Message: "在本机执行一下这个命令", ExpectedLabel: LabelShellCommand},
		{Message: "帮我运行这个脚本", ExpectedLabel: LabelShellCommand},
		{Message: "run this shell command on my machine", ExpectedLabel: LabelShellCommand},

		{Message: "看看当前 git 状态", ExpectedLabel: LabelGitInspect},
		{Message: "帮我看下现在的 diff", ExpectedLabel: LabelGitInspect},
		{Message: "show me the current diff", ExpectedLabel: LabelGitInspect},

		{Message: "把这些改动提交一下", ExpectedLabel: LabelGitMutate},
		{Message: "把本地提交推送到远程", ExpectedLabel: LabelGitMutate},
		{Message: "commit the current changes", ExpectedLabel: LabelGitMutate},

		{Message: "开始录音", ExpectedLabel: LabelAudioRecord},
		{Message: "帮我录一下这个会议", ExpectedLabel: LabelAudioRecord},
		{Message: "start recording audio", ExpectedLabel: LabelAudioRecord},

		{Message: "把这个音频文件转成文字", ExpectedLabel: LabelAudioTranscribe},
		{Message: "转写一下这段录音", ExpectedLabel: LabelAudioTranscribe},
		{Message: "transcribe this audio file", ExpectedLabel: LabelAudioTranscribe},

		{Message: "把这段话念给我听", ExpectedLabel: LabelAudioSynthesize},
		{Message: "朗读这段文字", ExpectedLabel: LabelAudioSynthesize},
		{Message: "read this paragraph aloud", ExpectedLabel: LabelAudioSynthesize},

		{Message: "用语音发到群里", ExpectedLabel: LabelAudioDeliver},
		{Message: "发成语音消息", ExpectedLabel: LabelAudioDeliver},
		{Message: "send this as a voice message", ExpectedLabel: LabelAudioDeliver},

		{Message: "抓取这个链接的内容", ExpectedLabel: LabelWebFetch, Note: "specific URL supplied, not an open-ended search"},
		{Message: "帮我看看这个网页写了什么", ExpectedLabel: LabelWebFetch},
		{Message: "fetch the content of this URL", ExpectedLabel: LabelWebFetch},

		{Message: "查一下最近的安全审计日志", ExpectedLabel: LabelAuditRead},
		{Message: "搜索我们之前的对话记录", ExpectedLabel: LabelAuditRead},
		{Message: "check the project health status", ExpectedLabel: LabelAuditRead},

		{Message: "在知识库里查一下这个概念的笔记", ExpectedLabel: LabelKnowledgeRead},
		{Message: "检索我的外脑中关于这个项目的资料", ExpectedLabel: LabelKnowledgeRead},
		{Message: "search my knowledge base for this topic", ExpectedLabel: LabelKnowledgeRead},

		// =====================================================================
		// S2b2 governed administration families
		// =====================================================================
		{Message: "用默认浏览器打开这个网址", ExpectedLabel: LabelAppLaunch, Note: "launch via OS handler, not browser automation"},
		{Message: "在资源管理器里打开这个文件夹", ExpectedLabel: LabelAppLaunch},
		{Message: "launch the calculator app", ExpectedLabel: LabelAppLaunch},

		{Message: "把这个链接的文件下载到本地", ExpectedLabel: LabelFileDownload, Note: "save to disk, not read page text"},
		{Message: "帮我把这个安装包下载下来", ExpectedLabel: LabelFileDownload},
		{Message: "download this file to my machine", ExpectedLabel: LabelFileDownload},

		{Message: "每天早上九点提醒我站会", ExpectedLabel: LabelScheduleManage},
		{Message: "列出所有定时任务", ExpectedLabel: LabelScheduleManage},
		{Message: "pause my daily reminder task", ExpectedLabel: LabelScheduleManage},
		{Message: "每天早上发给群里", ExpectedLabel: LabelScheduleDispatch},

		{Message: "把当前的 LLM 服务商切换成智谱", ExpectedLabel: LabelConfigManage},
		{Message: "把最大推理轮数调到 50", ExpectedLabel: LabelConfigManage},
		{Message: "switch to a different LLM provider", ExpectedLabel: LabelConfigManage},

		{Message: "记住我偏好用中文交流", ExpectedLabel: LabelMemoryManage, Note: "agent memory, not knowledge ingestion"},
		{Message: "你都记住了我的哪些偏好", ExpectedLabel: LabelMemoryManage},
		{Message: "what do you remember about me", ExpectedLabel: LabelMemoryManage},

		{Message: "帮我建一个待办清单", ExpectedLabel: LabelTaskTrack},
		{Message: "把这个任务标记为已完成", ExpectedLabel: LabelTaskTrack},
		{Message: "show my current todo list", ExpectedLabel: LabelTaskTrack},

		{Message: "创建一个长期目标：持续监控这个服务的可用性", ExpectedLabel: LabelGoalManage},
		{Message: "查看当前长期目标的进展", ExpectedLabel: LabelGoalManage},
		{Message: "create a long-running goal to keep this documentation up to date", ExpectedLabel: LabelGoalManage},

		{Message: "创建一个用 codex 的会话模板", ExpectedLabel: LabelTemplateManage},
		{Message: "用之前那个模板启动一个新会话", ExpectedLabel: LabelTemplateManage},
		{Message: "list my session templates", ExpectedLabel: LabelTemplateManage},

		{Message: "列出当前所有编码会话", ExpectedLabel: LabelSessionManage, Note: "managing sessions, not doing the coding"},
		{Message: "中断正在运行的会话", ExpectedLabel: LabelSessionManage},
		{Message: "show the recent output of that session", ExpectedLabel: LabelSessionManage},

		{Message: "把这个任务委派给编码子 agent", ExpectedLabel: LabelDelegateTask, Note: "explicit delegation, not a plain build request"},
		{Message: "并行执行这三个独立的任务", ExpectedLabel: LabelDelegateTask},
		{Message: "run these three tasks in parallel", ExpectedLabel: LabelDelegateTask},

		{Message: "刷新一下知识库里的这个来源", ExpectedLabel: LabelKnowledgeAdmin, Note: "admin maintenance, not retrieval"},
		{Message: "禁用这个知识库数据源", ExpectedLabel: LabelKnowledgeAdmin},
		{Message: "run the knowledge quality maintenance plan", ExpectedLabel: LabelKnowledgeAdmin},
	}
}
