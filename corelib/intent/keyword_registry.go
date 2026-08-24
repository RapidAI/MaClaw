package intent

import "strings"

// KeywordMatch represents a keyword hit during diagnostic recall.
type KeywordMatch struct {
	Entry    KeywordEntry
	Position int // byte offset in text
}

// KeywordRegistry holds keyword evidence organized by IntentLabel.
// Keyword evidence is not an execution-route authority.
type KeywordRegistry struct {
	entries      []KeywordEntry
	byLabel      map[IntentLabel][]KeywordEntry
	strongIndex  map[string]IntentLabel
	weakByLabel  map[IntentLabel][]string
	lowerEntries []string
}

// NewKeywordRegistry creates the registry from diagnostic keyword evidence.
func NewKeywordRegistry() *KeywordRegistry {
	return newKeywordRegistryFromEntries(defaultKeywords)
}

// newKeywordRegistryFromEntries builds a KeywordRegistry from an arbitrary
// keyword entry list. Shared by NewKeywordRegistry and definition-derived data.
func newKeywordRegistryFromEntries(keywords []KeywordEntry) *KeywordRegistry {
	r := &KeywordRegistry{
		entries:      make([]KeywordEntry, 0, len(keywords)),
		byLabel:      make(map[IntentLabel][]KeywordEntry),
		strongIndex:  make(map[string]IntentLabel),
		weakByLabel:  make(map[IntentLabel][]string),
		lowerEntries: make([]string, 0, len(keywords)),
	}

	priority := map[IntentLabel]int{
		LabelSSH:              0,
		LabelBrowser:          1,
		LabelCoding:           2,
		LabelNonCoding:        3,
		LabelAmbiguous:        4,
		LabelSearch:           5,
		LabelDocumentDelivery: 6,
		LabelDocumentGenerate: 6,
		LabelBusinessData:     7,
		LabelBugFix:           8,
		LabelContinuation:     9,
		LabelMaintenance:      10,
		LabelOffice:           11,
		LabelUnknown:          12,
		// S2b1 labels rank after every pre-existing label so a shared strong
		// keyword can never steal diagnostic recall from an older family.
		LabelFileRead:        13,
		LabelFileWrite:       14,
		LabelShellCommand:    15,
		LabelGitInspect:      16,
		LabelAudioRecord:     17,
		LabelAudioTranscribe: 18,
		LabelWebFetch:        19,
		LabelAuditRead:       20,
		LabelKnowledgeRead:   21,
		// S2b2 labels rank after every earlier label for the same reason.
		LabelAppLaunch:        22,
		LabelFileDownload:     23,
		LabelScheduleManage:   24,
		LabelScheduleDispatch: 33,
		LabelAudioSynthesize:  34,
		LabelAudioDeliver:     35,
		LabelDocumentOpen:     36,
		LabelConfigManage:     25,
		LabelMemoryManage:     26,
		LabelTaskTrack:        27,
		LabelGoalManage:       28,
		LabelTemplateManage:   29,
		LabelSessionManage:    30,
		LabelDelegateTask:     31,
		LabelKnowledgeAdmin:   32,
		// git_mutate ranks last for the same reason: it shares "git" wording
		// with git_inspect and "提交" wording with several older families, and
		// must never outrank them on a keyword they already own.
		LabelGitMutate: 37,
	}

	type entryKey struct {
		keyword  string
		label    IntentLabel
		strength KeywordStrength
	}
	seen := make(map[entryKey]bool)

	for _, e := range keywords {
		lowerKW := strings.ToLower(e.Keyword)
		key := entryKey{lowerKW, e.Label, e.Strength}
		if seen[key] {
			continue
		}
		seen[key] = true

		r.entries = append(r.entries, e)
		r.byLabel[e.Label] = append(r.byLabel[e.Label], e)
		r.lowerEntries = append(r.lowerEntries, lowerKW)
		if e.Strength == Strong {
			if existing, ok := r.strongIndex[lowerKW]; ok {
				if priority[e.Label] < priority[existing] {
					r.strongIndex[lowerKW] = e.Label
				}
				continue
			}
			r.strongIndex[lowerKW] = e.Label
		} else {
			r.weakByLabel[e.Label] = append(r.weakByLabel[e.Label], lowerKW)
		}
	}

	return r
}

// Match returns all diagnostic keyword evidence found in the text.
func (r *KeywordRegistry) Match(text string) []KeywordMatch {
	lower := strings.ToLower(text)
	var matches []KeywordMatch
	for i, e := range r.entries {
		pos := strings.Index(lower, r.lowerEntries[i])
		if pos >= 0 {
			matches = append(matches, KeywordMatch{Entry: e, Position: pos})
		}
	}
	return matches
}

// defaultKeywords is a compact diagnostic vocabulary. It feeds metadata,
// calibration, and prompts, but never directly authorizes workflow transitions
// or tool activation.
var defaultKeywords = []KeywordEntry{
	{Keyword: "ssh", Label: LabelSSH, Strength: Weak},
	{Keyword: "ssh into", Label: LabelSSH, Strength: Strong},
	{Keyword: "remote server", Label: LabelSSH, Strength: Strong},
	{Keyword: "server logs", Label: LabelSSH, Strength: Strong},
	{Keyword: "restart service", Label: LabelSSH, Strength: Strong},

	{Keyword: "打开 Chrome", Label: LabelBrowser, Strength: Strong},
	{Keyword: "打开网页", Label: LabelBrowser, Strength: Strong},
	{Keyword: "open chrome", Label: LabelBrowser, Strength: Strong},
	{Keyword: "browser", Label: LabelBrowser, Strength: Strong},
	{Keyword: "playwright", Label: LabelBrowser, Strength: Strong},
	{Keyword: "登录知乎", Label: LabelBrowser, Strength: Strong},
	{Keyword: "发表", Label: LabelBrowser, Strength: Weak},
	{Keyword: "发布", Label: LabelBrowser, Strength: Weak},
	{Keyword: "发帖", Label: LabelBrowser, Strength: Strong},
	{Keyword: "publish", Label: LabelBrowser, Strength: Weak},
	{Keyword: "sign in", Label: LabelBrowser, Strength: Strong},
	{Keyword: "web page", Label: LabelBrowser, Strength: Weak},
	{Keyword: "click", Label: LabelBrowser, Strength: Weak},

	{Keyword: "search", Label: LabelSearch, Strength: Strong},
	{Keyword: "paper", Label: LabelSearch, Strength: Strong},
	{Keyword: "news", Label: LabelSearch, Strength: Strong},
	{Keyword: "全网搜索", Label: LabelSearch, Strength: Strong},
	{Keyword: "上网搜索", Label: LabelSearch, Strength: Strong},
	{Keyword: "网上搜索", Label: LabelSearch, Strength: Strong},

	{Keyword: "send file", Label: LabelDocumentDelivery, Strength: Strong},
	{Keyword: "attachment", Label: LabelDocumentDelivery, Strength: Strong},
	{Keyword: "export pdf", Label: LabelDocumentGenerate, Strength: Strong},
	{Keyword: "generate pdf", Label: LabelDocumentGenerate, Strength: Strong},
	{Keyword: "生成pdf", Label: LabelDocumentGenerate, Strength: Strong},

	{Keyword: "business transaction", Label: LabelBusinessData, Strength: Strong},
	{Keyword: "structured business data", Label: LabelBusinessData, Strength: Strong},
	{Keyword: "expense reimbursement", Label: LabelBusinessData, Strength: Weak},
	{Keyword: "invoice approval", Label: LabelBusinessData, Strength: Weak},

	{Keyword: "write code", Label: LabelCoding, Strength: Strong},
	{Keyword: "implement feature", Label: LabelCoding, Strength: Strong},
	{Keyword: "build app", Label: LabelCoding, Strength: Strong},

	{Keyword: "fix bug", Label: LabelBugFix, Strength: Strong},
	{Keyword: "debug crash", Label: LabelBugFix, Strength: Strong},

	{Keyword: "refactor", Label: LabelMaintenance, Strength: Strong},
	{Keyword: "optimize", Label: LabelMaintenance, Strength: Strong},

	{Keyword: "summarize", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "translate", Label: LabelNonCoding, Strength: Strong},

	{Keyword: "ppt", Label: LabelOffice, Strength: Strong},
	{Keyword: "spreadsheet", Label: LabelOffice, Strength: Strong},

	{Keyword: "continue", Label: LabelContinuation, Strength: Weak},
	{Keyword: "go ahead", Label: LabelContinuation, Strength: Weak},
	{Keyword: "切换到完整模式", Label: LabelContinuation, Strength: Weak},
	{Keyword: "切换到完整agent模式", Label: LabelContinuation, Strength: Weak},
	{Keyword: "用完整能力再做一次", Label: LabelContinuation, Strength: Weak},
	{Keyword: "switch to full agent", Label: LabelContinuation, Strength: Weak},
	{Keyword: "switch to full agent mode", Label: LabelContinuation, Strength: Weak},

	// S2b1 governed families. These are diagnostic recall evidence only; the
	// semantic rule set, not a keyword hit, decides managed routing.
	{Keyword: "读取文件", Label: LabelFileRead, Strength: Strong},
	{Keyword: "查看文件内容", Label: LabelFileRead, Strength: Strong},
	{Keyword: "列出目录", Label: LabelFileRead, Strength: Weak},
	{Keyword: "read file", Label: LabelFileRead, Strength: Weak},
	{Keyword: "list directory", Label: LabelFileRead, Strength: Weak},

	{Keyword: "写入文件", Label: LabelFileWrite, Strength: Strong},
	{Keyword: "保存到文件", Label: LabelFileWrite, Strength: Strong},
	{Keyword: "修改文件", Label: LabelFileWrite, Strength: Weak},
	{Keyword: "write file", Label: LabelFileWrite, Strength: Weak},

	{Keyword: "执行命令", Label: LabelShellCommand, Strength: Strong},
	{Keyword: "运行脚本", Label: LabelShellCommand, Strength: Weak},
	{Keyword: "run command", Label: LabelShellCommand, Strength: Weak},
	{Keyword: "shell command", Label: LabelShellCommand, Strength: Weak},

	{Keyword: "git status", Label: LabelGitInspect, Strength: Strong},
	{Keyword: "git diff", Label: LabelGitInspect, Strength: Strong},

	// Only wordings that name the VCS write itself. A bare "提交" is left out:
	// it is the ordinary verb for submitting a form, a report, or an order.
	{Keyword: "git commit", Label: LabelGitMutate, Strength: Strong},
	{Keyword: "git push", Label: LabelGitMutate, Strength: Strong},
	{Keyword: "提交代码", Label: LabelGitMutate, Strength: Strong},
	{Keyword: "推送到远程", Label: LabelGitMutate, Strength: Strong},
	{Keyword: "commit changes", Label: LabelGitMutate, Strength: Weak},

	{Keyword: "开始录音", Label: LabelAudioRecord, Strength: Strong},
	{Keyword: "会议录音", Label: LabelAudioRecord, Strength: Strong},
	{Keyword: "record audio", Label: LabelAudioRecord, Strength: Weak},

	{Keyword: "转写", Label: LabelAudioTranscribe, Strength: Strong},
	{Keyword: "语音识别", Label: LabelAudioTranscribe, Strength: Strong},
	{Keyword: "transcribe", Label: LabelAudioTranscribe, Strength: Strong},

	{Keyword: "念给我听", Label: LabelAudioSynthesize, Strength: Strong},
	{Keyword: "朗读这段", Label: LabelAudioSynthesize, Strength: Strong},
	{Keyword: "用语音播放", Label: LabelAudioSynthesize, Strength: Strong},
	{Keyword: "read this aloud", Label: LabelAudioSynthesize, Strength: Strong},
	{Keyword: "speak this text", Label: LabelAudioSynthesize, Strength: Weak},
	{Keyword: "play this as speech", Label: LabelAudioSynthesize, Strength: Weak},

	{Keyword: "用语音发到群里", Label: LabelAudioDeliver, Strength: Strong},
	{Keyword: "发成语音消息", Label: LabelAudioDeliver, Strength: Strong},
	{Keyword: "send this as a voice message", Label: LabelAudioDeliver, Strength: Strong},
	{Keyword: "deliver this as speech", Label: LabelAudioDeliver, Strength: Weak},

	{Keyword: "打开桌面上的PDF", Label: LabelDocumentOpen, Strength: Strong},
	{Keyword: "用默认程序打开这个文档", Label: LabelDocumentOpen, Strength: Strong},
	{Keyword: "open the PDF on my desktop", Label: LabelDocumentOpen, Strength: Strong},
	{Keyword: "open the PDF document on my desktop", Label: LabelDocumentOpen, Strength: Strong},
	{Keyword: "open this document with the default app", Label: LabelDocumentOpen, Strength: Strong},

	{Keyword: "抓取网页", Label: LabelWebFetch, Strength: Strong},
	{Keyword: "抓取这个链接", Label: LabelWebFetch, Strength: Strong},
	{Keyword: "fetch url", Label: LabelWebFetch, Strength: Weak},

	{Keyword: "审计日志", Label: LabelAuditRead, Strength: Strong},
	{Keyword: "audit log", Label: LabelAuditRead, Strength: Strong},
	{Keyword: "历史对话", Label: LabelAuditRead, Strength: Weak},

	{Keyword: "检索知识库", Label: LabelKnowledgeRead, Strength: Strong},
	{Keyword: "知识库检索", Label: LabelKnowledgeRead, Strength: Strong},
	{Keyword: "search knowledge base", Label: LabelKnowledgeRead, Strength: Weak},

	// S2b2 governed administration families. Diagnostic recall evidence only;
	// the semantic rule set, not a keyword hit, decides managed routing.
	{Keyword: "打开应用", Label: LabelAppLaunch, Strength: Weak},
	{Keyword: "打开网址", Label: LabelAppLaunch, Strength: Weak},
	{Keyword: "open url", Label: LabelAppLaunch, Strength: Weak},
	{Keyword: "launch app", Label: LabelAppLaunch, Strength: Weak},

	{Keyword: "下载文件", Label: LabelFileDownload, Strength: Strong},
	{Keyword: "下载到本地", Label: LabelFileDownload, Strength: Strong},
	{Keyword: "download file", Label: LabelFileDownload, Strength: Weak},

	{Keyword: "定时任务", Label: LabelScheduleManage, Strength: Strong},
	{Keyword: "定时提醒", Label: LabelScheduleManage, Strength: Strong},
	{Keyword: "scheduled task", Label: LabelScheduleManage, Strength: Weak},
	{Keyword: "发给群", Label: LabelScheduleDispatch, Strength: Strong},
	{Keyword: "推送到群", Label: LabelScheduleDispatch, Strength: Strong},
	{Keyword: "每天发给", Label: LabelScheduleDispatch, Strength: Strong},
	{Keyword: "send to the group every", Label: LabelScheduleDispatch, Strength: Weak},

	{Keyword: "修改配置", Label: LabelConfigManage, Strength: Strong},
	{Keyword: "切换模型", Label: LabelConfigManage, Strength: Strong},
	{Keyword: "switch provider", Label: LabelConfigManage, Strength: Weak},
	{Keyword: "用户画像", Label: LabelConfigManage, Strength: Weak},

	{Keyword: "记住我", Label: LabelMemoryManage, Strength: Strong},
	{Keyword: "长期记忆", Label: LabelMemoryManage, Strength: Strong},
	{Keyword: "remember that", Label: LabelMemoryManage, Strength: Weak},

	{Keyword: "待办清单", Label: LabelTaskTrack, Strength: Strong},
	{Keyword: "任务列表", Label: LabelTaskTrack, Strength: Weak},
	{Keyword: "todo list", Label: LabelTaskTrack, Strength: Weak},

	{Keyword: "长期目标", Label: LabelGoalManage, Strength: Strong},
	{Keyword: "long-running goal", Label: LabelGoalManage, Strength: Weak},

	{Keyword: "会话模板", Label: LabelTemplateManage, Strength: Strong},
	{Keyword: "session template", Label: LabelTemplateManage, Strength: Weak},

	{Keyword: "编码会话", Label: LabelSessionManage, Strength: Strong},
	{Keyword: "中断会话", Label: LabelSessionManage, Strength: Strong},
	{Keyword: "coding session", Label: LabelSessionManage, Strength: Weak},

	{Keyword: "委派给子", Label: LabelDelegateTask, Strength: Strong},
	{Keyword: "并行执行", Label: LabelDelegateTask, Strength: Weak},
	{Keyword: "delegate", Label: LabelDelegateTask, Strength: Weak},

	{Keyword: "知识库维护", Label: LabelKnowledgeAdmin, Strength: Strong},
	{Keyword: "禁用知识库", Label: LabelKnowledgeAdmin, Strength: Strong},
	{Keyword: "knowledge maintenance", Label: LabelKnowledgeAdmin, Strength: Weak},
}
