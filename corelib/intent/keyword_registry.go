package intent

import "strings"

// KeywordMatch represents a keyword hit during classification.
type KeywordMatch struct {
	Entry    KeywordEntry
	Position int // byte offset in text
}

// KeywordRegistry holds all keyword entries organized by IntentLabel.
// This is the single source of truth for all keyword-based classification.
type KeywordRegistry struct {
	entries      []KeywordEntry
	byLabel      map[IntentLabel][]KeywordEntry
	strongIndex  map[string]IntentLabel // keyword → label for strong keywords (conflict-resolved)
	weakByLabel  map[IntentLabel][]string
	lowerEntries []string // pre-lowered keyword strings, parallel to entries
}

// NewKeywordRegistry creates the registry from the consolidated keyword list.
// It builds indexes with conflict resolution priority: ssh > browser(strong) > coding > non_coding > ambiguous.
func NewKeywordRegistry() *KeywordRegistry {
	r := &KeywordRegistry{
		entries:      make([]KeywordEntry, 0, len(defaultKeywords)),
		byLabel:      make(map[IntentLabel][]KeywordEntry),
		strongIndex:  make(map[string]IntentLabel),
		weakByLabel:  make(map[IntentLabel][]string),
		lowerEntries: make([]string, 0, len(defaultKeywords)),
	}

	// Priority order for conflict resolution (higher index = lower priority).
	priority := map[IntentLabel]int{
		LabelSSH:              0,
		LabelBrowser:          1,
		LabelCoding:           2,
		LabelNonCoding:        3,
		LabelAmbiguous:        4,
		LabelSearch:           5,
		LabelDocumentDelivery: 6,
		LabelBugFix:           7,
		LabelContinuation:     8,
		LabelMaintenance:      9,
		LabelOffice:           10,
		LabelUnknown:          11,
	}

	// Deduplicate: track seen keyword+label+strength combos.
	type entryKey struct {
		keyword  string
		label    IntentLabel
		strength KeywordStrength
	}
	seen := make(map[entryKey]bool)

	for _, e := range defaultKeywords {
		key := entryKey{strings.ToLower(e.Keyword), e.Label, e.Strength}
		if seen[key] {
			continue
		}
		seen[key] = true

		r.entries = append(r.entries, e)
		r.byLabel[e.Label] = append(r.byLabel[e.Label], e)

		lowerKW := strings.ToLower(e.Keyword)
		r.lowerEntries = append(r.lowerEntries, lowerKW)
		if e.Strength == Strong {
			// Conflict resolution: only store if no higher-priority label already owns this keyword.
			if existing, ok := r.strongIndex[lowerKW]; ok {
				if priority[e.Label] < priority[existing] {
					r.strongIndex[lowerKW] = e.Label
				}
			} else {
				r.strongIndex[lowerKW] = e.Label
			}
		} else {
			r.weakByLabel[e.Label] = append(r.weakByLabel[e.Label], lowerKW)
		}
	}

	return r
}

// Match returns all matching keyword entries for the given text.
// Matching is case-insensitive substring matching using pre-lowered keywords.
func (r *KeywordRegistry) Match(text string) []KeywordMatch {
	lower := strings.ToLower(text)
	var matches []KeywordMatch

	for i, e := range r.entries {
		pos := strings.Index(lower, r.lowerEntries[i])
		if pos >= 0 {
			matches = append(matches, KeywordMatch{
				Entry:    e,
				Position: pos,
			})
		}
	}

	return matches
}

// defaultKeywords consolidates ALL keywords from the legacy scattered keyword lists.
// Each entry maps a keyword to an IntentLabel with a strength indicator.
//
// Sources:
//   - corelib/tool/router.go: sshIntentKeywords, searchIntentKeywords, documentDeliveryKeywords,
//     browserIntentKeywords, browserPageKeywords, browserActionKeywords, excelKeywords,
//     pptxReadKeywords, codingWorkflowDocKeywords
//   - gui/im_intent_classifier.go: sshKeywords, ambiguousKeywords
//   - gui/im_tools_session.go: nonCodingKeywords, codingKeywords, codingActionPhrases
//   - gui/coding_tool_gate.go: bugFixKeywords, creationCodingKeywords, skipSignalsChinese, skipSignalsEnglish
//   - gui/gate_intent_classifier.go: gateContPhrases, gateMaintenanceKeywords
var defaultKeywords = []KeywordEntry{
	// =========================================================================
	// LabelSSH (Strong) — from router.go sshIntentKeywords + im_intent_classifier.go sshKeywords
	// =========================================================================
	{Keyword: "ssh", Label: LabelSSH, Strength: Strong},
	{Keyword: "服务器", Label: LabelSSH, Strength: Strong},
	{Keyword: "服务端", Label: LabelSSH, Strength: Strong},
	{Keyword: "主机", Label: LabelSSH, Strength: Strong},
	{Keyword: "远程机器", Label: LabelSSH, Strength: Strong},
	{Keyword: "远程主机", Label: LabelSSH, Strength: Strong},
	{Keyword: "云服务器", Label: LabelSSH, Strength: Strong},
	{Keyword: "线上机器", Label: LabelSSH, Strength: Strong},
	{Keyword: "登录服务器", Label: LabelSSH, Strength: Strong},
	{Keyword: "连上服务器", Label: LabelSSH, Strength: Strong},
	{Keyword: "连接服务器", Label: LabelSSH, Strength: Strong},
	{Keyword: "远程登录", Label: LabelSSH, Strength: Strong},
	{Keyword: "看日志", Label: LabelSSH, Strength: Strong},
	{Keyword: "查看日志", Label: LabelSSH, Strength: Strong},
	{Keyword: "日志", Label: LabelSSH, Strength: Strong},
	{Keyword: "tail -f", Label: LabelSSH, Strength: Strong},
	{Keyword: "journalctl", Label: LabelSSH, Strength: Strong},
	{Keyword: "systemctl", Label: LabelSSH, Strength: Strong},
	{Keyword: "service ", Label: LabelSSH, Strength: Strong},
	{Keyword: "nginx", Label: LabelSSH, Strength: Strong},
	{Keyword: "docker", Label: LabelSSH, Strength: Strong},
	{Keyword: "docker compose", Label: LabelSSH, Strength: Strong},
	{Keyword: "k8s", Label: LabelSSH, Strength: Strong},
	{Keyword: "kubectl", Label: LabelSSH, Strength: Strong},
	{Keyword: "pm2", Label: LabelSSH, Strength: Strong},
	{Keyword: "supervisor", Label: LabelSSH, Strength: Strong},
	{Keyword: "重启服务", Label: LabelSSH, Strength: Strong},
	{Keyword: "重启 nginx", Label: LabelSSH, Strength: Strong},
	{Keyword: "重启进程", Label: LabelSSH, Strength: Strong},
	{Keyword: "上传到服务器", Label: LabelSSH, Strength: Strong},
	{Keyword: "下载服务器文件", Label: LabelSSH, Strength: Strong},
	{Keyword: "sftp", Label: LabelSSH, Strength: Strong},
	{Keyword: "scp", Label: LabelSSH, Strength: Strong},
	{Keyword: "rsync", Label: LabelSSH, Strength: Strong},
	{Keyword: "端口", Label: LabelSSH, Strength: Strong},
	{Keyword: "进程", Label: LabelSSH, Strength: Strong},
	{Keyword: "服务器文件", Label: LabelSSH, Strength: Strong},
	{Keyword: "服务器上", Label: LabelSSH, Strength: Strong},
	{Keyword: "远程执行", Label: LabelSSH, Strength: Strong},
	{Keyword: "host", Label: LabelSSH, Strength: Strong},
	{Keyword: "user", Label: LabelSSH, Strength: Strong},
	{Keyword: "label", Label: LabelSSH, Strength: Strong},
	{Keyword: "initial_command", Label: LabelSSH, Strength: Strong},

	// =========================================================================
	// LabelSearch (Strong) — from router.go searchIntentKeywords
	// =========================================================================
	{Keyword: "搜索", Label: LabelSearch, Strength: Strong},
	{Keyword: "search", Label: LabelSearch, Strength: Strong},
	{Keyword: "查找", Label: LabelSearch, Strength: Strong},
	{Keyword: "网页", Label: LabelSearch, Strength: Strong},
	{Keyword: "web", Label: LabelSearch, Strength: Strong},
	{Keyword: "google", Label: LabelSearch, Strength: Strong},
	{Keyword: "papers", Label: LabelSearch, Strength: Strong},
	{Keyword: "paper", Label: LabelSearch, Strength: Strong},
	{Keyword: "huggingface", Label: LabelSearch, Strength: Strong},

	// =========================================================================
	// LabelDocumentDelivery (Strong) — from router.go documentDeliveryKeywords
	// =========================================================================
	{Keyword: "pdf", Label: LabelDocumentDelivery, Strength: Strong},
	{Keyword: "报告", Label: LabelDocumentDelivery, Strength: Strong},
	{Keyword: "综述", Label: LabelDocumentDelivery, Strength: Strong},
	{Keyword: "附件", Label: LabelDocumentDelivery, Strength: Strong},
	{Keyword: "发送文件", Label: LabelDocumentDelivery, Strength: Strong},
	{Keyword: "文件发我", Label: LabelDocumentDelivery, Strength: Strong},
	{Keyword: "发给我", Label: LabelDocumentDelivery, Strength: Strong},
	{Keyword: "导出", Label: LabelDocumentDelivery, Strength: Strong},

	// =========================================================================
	// LabelBrowser (Strong) — from router.go browserIntentKeywords
	// =========================================================================
	{Keyword: "浏览器", Label: LabelBrowser, Strength: Strong},
	{Keyword: "browser", Label: LabelBrowser, Strength: Strong},
	{Keyword: "chrome", Label: LabelBrowser, Strength: Strong},
	{Keyword: "chromium", Label: LabelBrowser, Strength: Strong},
	{Keyword: "playwright", Label: LabelBrowser, Strength: Strong},
	{Keyword: "录制", Label: LabelBrowser, Strength: Strong},
	{Keyword: "回放", Label: LabelBrowser, Strength: Strong},
	{Keyword: "replay", Label: LabelBrowser, Strength: Strong},
	{Keyword: "record", Label: LabelBrowser, Strength: Strong},
	{Keyword: "browser_", Label: LabelBrowser, Strength: Strong},

	// =========================================================================
	// LabelBrowser (Weak) — from router.go browserPageKeywords + browserActionKeywords
	// =========================================================================
	{Keyword: "页面", Label: LabelBrowser, Strength: Weak},
	{Keyword: "网站", Label: LabelBrowser, Strength: Weak},
	{Keyword: "url", Label: LabelBrowser, Strength: Weak},
	{Keyword: "page", Label: LabelBrowser, Strength: Weak},
	{Keyword: "site", Label: LabelBrowser, Strength: Weak},
	{Keyword: "访问", Label: LabelBrowser, Strength: Weak},
	{Keyword: "导航", Label: LabelBrowser, Strength: Weak},
	{Keyword: "点击", Label: LabelBrowser, Strength: Weak},
	{Keyword: "观察", Label: LabelBrowser, Strength: Weak},
	{Keyword: "打开", Label: LabelBrowser, Strength: Weak},
	// "截图" moved to LabelNonCoding Strong — screenshot is a generic desktop operation, not browser-specific.
	{Keyword: "输入", Label: LabelBrowser, Strength: Weak},
	{Keyword: "填写", Label: LabelBrowser, Strength: Weak},

	// =========================================================================
	// LabelOffice (Strong) — from router.go excelKeywords + pptxReadKeywords
	// =========================================================================
	{Keyword: "xlsx", Label: LabelOffice, Strength: Strong},
	{Keyword: "csv", Label: LabelOffice, Strength: Strong},
	{Keyword: "spreadsheet", Label: LabelOffice, Strength: Strong},
	{Keyword: "表格", Label: LabelOffice, Strength: Strong},
	{Keyword: "电子表格", Label: LabelOffice, Strength: Strong},
	{Keyword: "excel", Label: LabelOffice, Strength: Strong},
	{Keyword: "pptx", Label: LabelOffice, Strength: Strong},
	{Keyword: "幻灯片", Label: LabelOffice, Strength: Strong},
	{Keyword: "演示文稿", Label: LabelOffice, Strength: Strong},
	{Keyword: "powerpoint", Label: LabelOffice, Strength: Strong},
	{Keyword: "ppt", Label: LabelOffice, Strength: Strong},
	{Keyword: "读取ppt", Label: LabelOffice, Strength: Strong},

	// =========================================================================
	// LabelCoding (Strong) — from router.go codingWorkflowDocKeywords
	// =========================================================================
	{Keyword: "需求文档", Label: LabelCoding, Strength: Strong},
	{Keyword: "设计文档", Label: LabelCoding, Strength: Strong},
	{Keyword: "任务文档", Label: LabelCoding, Strength: Strong},
	{Keyword: "任务拆分", Label: LabelCoding, Strength: Strong},
	{Keyword: "任务计划", Label: LabelCoding, Strength: Strong},
	{Keyword: "技术设计", Label: LabelCoding, Strength: Strong},
	{Keyword: "需求分析", Label: LabelCoding, Strength: Strong},
	{Keyword: "架构设计", Label: LabelCoding, Strength: Strong},
	{Keyword: "模块设计", Label: LabelCoding, Strength: Strong},
	{Keyword: "接口设计", Label: LabelCoding, Strength: Strong},
	{Keyword: "生成需求", Label: LabelCoding, Strength: Strong},
	{Keyword: "生成设计", Label: LabelCoding, Strength: Strong},
	{Keyword: "生成任务", Label: LabelCoding, Strength: Strong},
	{Keyword: "开发游戏", Label: LabelCoding, Strength: Strong},
	{Keyword: "开发应用", Label: LabelCoding, Strength: Strong},
	{Keyword: "开发工具", Label: LabelCoding, Strength: Strong},
	{Keyword: "开发系统", Label: LabelCoding, Strength: Strong},
	{Keyword: "开发程序", Label: LabelCoding, Strength: Strong},
	{Keyword: "开发项目", Label: LabelCoding, Strength: Strong},
	{Keyword: "写代码", Label: LabelCoding, Strength: Strong},
	{Keyword: "改代码", Label: LabelCoding, Strength: Strong},
	{Keyword: "编程", Label: LabelCoding, Strength: Strong},
	{Keyword: "实现功能", Label: LabelCoding, Strength: Strong},
	{Keyword: "新增功能", Label: LabelCoding, Strength: Strong},
	{Keyword: "添加功能", Label: LabelCoding, Strength: Strong},
	{Keyword: "修 bug", Label: LabelCoding, Strength: Strong},
	{Keyword: "修bug", Label: LabelCoding, Strength: Strong},
	{Keyword: "修复bug", Label: LabelCoding, Strength: Strong},
	{Keyword: "重构代码", Label: LabelCoding, Strength: Strong},

	// =========================================================================
	// LabelCoding (Strong) — from im_tools_session.go codingKeywords (deduplicated)
	// =========================================================================
	{Keyword: "修改代码", Label: LabelCoding, Strength: Strong},
	{Keyword: "开发", Label: LabelCoding, Strength: Strong},
	{Keyword: "重构", Label: LabelCoding, Strength: Strong},
	{Keyword: "refactor", Label: LabelCoding, Strength: Strong},
	{Keyword: "实现", Label: LabelCoding, Strength: Strong},
	{Keyword: "写脚本", Label: LabelCoding, Strength: Strong},
	{Keyword: "写一个脚本", Label: LabelCoding, Strength: Strong},
	{Keyword: "写个脚本", Label: LabelCoding, Strength: Strong},
	{Keyword: "写函数", Label: LabelCoding, Strength: Strong},
	{Keyword: "写方法", Label: LabelCoding, Strength: Strong},
	{Keyword: "写接口", Label: LabelCoding, Strength: Strong},
	{Keyword: "写api", Label: LabelCoding, Strength: Strong},
	{Keyword: "写 api", Label: LabelCoding, Strength: Strong},
	{Keyword: "编译", Label: LabelCoding, Strength: Strong},
	{Keyword: "构建", Label: LabelCoding, Strength: Strong},
	{Keyword: "build", Label: LabelCoding, Strength: Strong},
	{Keyword: "compile", Label: LabelCoding, Strength: Strong},
	{Keyword: "pull request", Label: LabelCoding, Strength: Strong},
	{Keyword: "merge request", Label: LabelCoding, Strength: Strong},
	{Keyword: "git commit", Label: LabelCoding, Strength: Strong},
	{Keyword: "git push", Label: LabelCoding, Strength: Strong},
	{Keyword: "create_session", Label: LabelCoding, Strength: Strong},
	{Keyword: "游戏", Label: LabelCoding, Strength: Strong},
	{Keyword: "game", Label: LabelCoding, Strength: Strong},
	{Keyword: "开发一个", Label: LabelCoding, Strength: Strong},
	{Keyword: "开发个", Label: LabelCoding, Strength: Strong},
	{Keyword: "实现一个", Label: LabelCoding, Strength: Strong},
	{Keyword: "实现个", Label: LabelCoding, Strength: Strong},
	{Keyword: "创建一个", Label: LabelCoding, Strength: Strong},
	{Keyword: "创建个", Label: LabelCoding, Strength: Strong},
	{Keyword: "前端", Label: LabelCoding, Strength: Strong},
	{Keyword: "后端", Label: LabelCoding, Strength: Strong},
	{Keyword: "frontend", Label: LabelCoding, Strength: Strong},
	{Keyword: "backend", Label: LabelCoding, Strength: Strong},

	// =========================================================================
	// LabelCoding (Weak) — from im_tools_session.go codingKeywords (weaker signals)
	// =========================================================================
	{Keyword: "代码", Label: LabelCoding, Strength: Weak},
	{Keyword: "源码", Label: LabelCoding, Strength: Weak},
	{Keyword: "源代码", Label: LabelCoding, Strength: Weak},
	{Keyword: "测试", Label: LabelCoding, Strength: Weak},
	{Keyword: "单元测试", Label: LabelCoding, Strength: Weak},
	{Keyword: "test", Label: LabelCoding, Strength: Weak},
	{Keyword: "部署", Label: LabelCoding, Strength: Weak},
	{Keyword: "deploy", Label: LabelCoding, Strength: Weak},
	{Keyword: "bug", Label: LabelCoding, Strength: Weak},
	{Keyword: "修复 bug", Label: LabelCoding, Strength: Weak},

	// =========================================================================
	// LabelCoding (Strong) — from coding_tool_gate.go creationCodingKeywords (deduplicated)
	// =========================================================================
	{Keyword: "写一个脚本", Label: LabelCoding, Strength: Strong},
	{Keyword: "写个脚本", Label: LabelCoding, Strength: Strong},
	{Keyword: "写api", Label: LabelCoding, Strength: Strong},
	{Keyword: "写 api", Label: LabelCoding, Strength: Strong},

	// =========================================================================
	// LabelNonCoding (Strong) — from im_intent_classifier.go / im_tools_session.go nonCodingKeywords
	// =========================================================================
	{Keyword: "搜索论文", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "搜论文", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "找论文", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "查论文", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "下载论文", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "翻译", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "全文翻译", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "翻译成中文", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "翻译成英文", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "生成pdf", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "生成 pdf", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "导出pdf", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "导出 pdf", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "宣传ppt", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "宣传 ppt", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "演示稿", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "presentation", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "slides", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "查天气", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "天气预报", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "今天天气", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "查快递", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "快递单号", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "物流查询", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "搜索新闻", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "查新闻", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "最新新闻", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "总结文章", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "总结论文", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "摘要", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "发邮件", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "写邮件", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "发送邮件", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "提醒我", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "设个闹钟", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "播放音乐", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "放首歌", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "知识库", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "放入知识库", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "导入知识库", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "入库", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "评测报告", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "文档处理", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "arxiv", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "截屏", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "截图", Label: LabelNonCoding, Strength: Strong},

	// =========================================================================
	// LabelAmbiguous (Weak) — from im_intent_classifier.go ambiguousKeywords
	// =========================================================================
	{Keyword: "上线", Label: LabelAmbiguous, Strength: Weak},
	{Keyword: "线上问题", Label: LabelAmbiguous, Strength: Weak},
	{Keyword: "线上故障", Label: LabelAmbiguous, Strength: Weak},
	{Keyword: "服务挂了", Label: LabelAmbiguous, Strength: Weak},
	{Keyword: "服务异常", Label: LabelAmbiguous, Strength: Weak},
	{Keyword: "环境问题", Label: LabelAmbiguous, Strength: Weak},
	{Keyword: "处理一下线上问题", Label: LabelAmbiguous, Strength: Weak},
	{Keyword: "看看服务", Label: LabelAmbiguous, Strength: Weak},
	{Keyword: "看下服务", Label: LabelAmbiguous, Strength: Weak},
	{Keyword: "排查一下", Label: LabelAmbiguous, Strength: Weak},
	{Keyword: "处理一下这个项目", Label: LabelAmbiguous, Strength: Weak},

	// =========================================================================
	// LabelContinuation (Weak) — from im_tools_session.go codingActionPhrases
	// =========================================================================
	{Keyword: "开工", Label: LabelContinuation, Strength: Weak},
	{Keyword: "开干", Label: LabelContinuation, Strength: Weak},
	{Keyword: "动手", Label: LabelContinuation, Strength: Weak},
	{Keyword: "搞起来", Label: LabelContinuation, Strength: Weak},
	{Keyword: "搞起", Label: LabelContinuation, Strength: Weak},
	{Keyword: "干吧", Label: LabelContinuation, Strength: Weak},
	{Keyword: "做吧", Label: LabelContinuation, Strength: Weak},
	{Keyword: "开始吧", Label: LabelContinuation, Strength: Weak},
	{Keyword: "开始做", Label: LabelContinuation, Strength: Weak},
	{Keyword: "开始干", Label: LabelContinuation, Strength: Weak},
	{Keyword: "开始搞", Label: LabelContinuation, Strength: Weak},
	{Keyword: "let's go", Label: LabelContinuation, Strength: Weak},
	{Keyword: "let's do it", Label: LabelContinuation, Strength: Weak},
	{Keyword: "let's start", Label: LabelContinuation, Strength: Weak},
	{Keyword: "let's begin", Label: LabelContinuation, Strength: Weak},

	// =========================================================================
	// LabelContinuation (Weak) — from gate_intent_classifier.go gateContPhrases
	// =========================================================================
	{Keyword: "继续", Label: LabelContinuation, Strength: Weak},
	{Keyword: "start working", Label: LabelContinuation, Strength: Weak},
	{Keyword: "go ahead", Label: LabelContinuation, Strength: Weak},
	{Keyword: "continue", Label: LabelContinuation, Strength: Weak},
	{Keyword: "start", Label: LabelContinuation, Strength: Weak},
	{Keyword: "begin", Label: LabelContinuation, Strength: Weak},
	{Keyword: "go", Label: LabelContinuation, Strength: Weak},
	{Keyword: "ok", Label: LabelContinuation, Strength: Weak},
	{Keyword: "好的", Label: LabelContinuation, Strength: Weak},
	{Keyword: "嗯", Label: LabelContinuation, Strength: Weak},

	// =========================================================================
	// LabelContinuation (Strong) — from coding_tool_gate.go skipSignalsChinese/English
	// =========================================================================
	{Keyword: "直接做", Label: LabelContinuation, Strength: Strong},
	{Keyword: "直接用", Label: LabelContinuation, Strength: Strong},
	{Keyword: "不用问了", Label: LabelContinuation, Strength: Strong},
	{Keyword: "按你的想法来", Label: LabelContinuation, Strength: Strong},
	{Keyword: "直接开始", Label: LabelContinuation, Strength: Strong},
	{Keyword: "不用确认", Label: LabelContinuation, Strength: Strong},
	{Keyword: "马上做", Label: LabelContinuation, Strength: Strong},
	{Keyword: "赶紧做", Label: LabelContinuation, Strength: Strong},
	{Keyword: "跳过文档", Label: LabelContinuation, Strength: Strong},
	{Keyword: "不需要文档", Label: LabelContinuation, Strength: Strong},
	{Keyword: "just do it", Label: LabelContinuation, Strength: Strong},
	{Keyword: "skip confirmation", Label: LabelContinuation, Strength: Strong},
	{Keyword: "do it now", Label: LabelContinuation, Strength: Strong},

	// =========================================================================
	// LabelBugFix (Strong) — from coding_tool_gate.go bugFixKeywords
	// =========================================================================
	{Keyword: "修bug", Label: LabelBugFix, Strength: Strong},
	{Keyword: "修 bug", Label: LabelBugFix, Strength: Strong},
	{Keyword: "修复bug", Label: LabelBugFix, Strength: Strong},
	{Keyword: "修复 bug", Label: LabelBugFix, Strength: Strong},
	{Keyword: "fix", Label: LabelBugFix, Strength: Strong},
	{Keyword: "修复", Label: LabelBugFix, Strength: Strong},
	{Keyword: "修正", Label: LabelBugFix, Strength: Strong},
	{Keyword: "调试", Label: LabelBugFix, Strength: Strong},
	{Keyword: "debug", Label: LabelBugFix, Strength: Strong},
	{Keyword: "排查", Label: LabelBugFix, Strength: Strong},
	{Keyword: "排错", Label: LabelBugFix, Strength: Strong},
	{Keyword: "报错", Label: LabelBugFix, Strength: Strong},
	{Keyword: "出错", Label: LabelBugFix, Strength: Strong},
	{Keyword: "错误", Label: LabelBugFix, Strength: Strong},
	{Keyword: "异常", Label: LabelBugFix, Strength: Strong},
	{Keyword: "加载中", Label: LabelBugFix, Strength: Strong},
	{Keyword: "卡住", Label: LabelBugFix, Strength: Strong},
	{Keyword: "崩溃", Label: LabelBugFix, Strength: Strong},
	{Keyword: "crash", Label: LabelBugFix, Strength: Strong},
	{Keyword: "白屏", Label: LabelBugFix, Strength: Strong},
	{Keyword: "闪退", Label: LabelBugFix, Strength: Strong},
	{Keyword: "不工作", Label: LabelBugFix, Strength: Strong},
	{Keyword: "不生效", Label: LabelBugFix, Strength: Strong},
	{Keyword: "失败", Label: LabelBugFix, Strength: Strong},
	{Keyword: "不显示", Label: LabelBugFix, Strength: Strong},
	{Keyword: "显示异常", Label: LabelBugFix, Strength: Strong},

	// =========================================================================
	// LabelMaintenance (Strong) — from gate_intent_classifier.go gateMaintenanceKeywords
	// =========================================================================
	{Keyword: "优化", Label: LabelMaintenance, Strength: Strong},
	{Keyword: "清理", Label: LabelMaintenance, Strength: Strong},
	{Keyword: "升级", Label: LabelMaintenance, Strength: Strong},
	{Keyword: "改善", Label: LabelMaintenance, Strength: Strong},
	{Keyword: "clean up", Label: LabelMaintenance, Strength: Strong},
	{Keyword: "optimize", Label: LabelMaintenance, Strength: Strong},
	{Keyword: "upgrade", Label: LabelMaintenance, Strength: Strong},
	{Keyword: "improve", Label: LabelMaintenance, Strength: Strong},
}
