package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticToolsSearchName is the read-only discovery meta-tool rendered on
// every governed surface. The closed surface assumes the model can guess the
// exact stable spelling of an unlisted tool; weaker models cannot (the
// 2026-08-26 PPT turn hallucinated "generate_ppt" and stalled). Discovery
// turns that guess into a lookup: the model asks in natural language, gets
// back exact names and whether each is listed or petitionable. Discovery is
// not authorization — grants still come only from the plan or a petition.
const semanticToolsSearchName = "tools_search"

func semanticToolsSearchDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticToolsSearchName,
			"description": "Find the exact name of an unlisted capability. Read-only.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			},
		},
	}
}

// semanticToolsSearchInventory is the host-owned discovery inventory: stable
// model name → keywords (zh+en) and a one-line summary. It deliberately
// covers the names a managed chat surface can ever render or petition plus
// the well-known model names it must honestly mark unavailable (legacy
// file-tool aliases, quarantined sends), not every registered tool —
// discovering a name the host can never grant would send the model down a
// dead end.
type semanticToolsSearchEntry struct {
	name     string
	keywords []string
	summary  string
}

var semanticToolsSearchInventory = []semanticToolsSearchEntry{
	{"web_search", []string{"搜索", "搜", "查", "网页", "网上", "检索", "天气", "新闻", "search", "web", "lookup", "weather", "forecast", "news", "photo", "image", "picture", "song", "music", "movie", "wiki"}, "Search the public web."},
	{"web_fetch", []string{"抓取", "网页内容", "链接", "打开网页", "fetch", "url", "page", "wikipedia"}, "Fetch the content of one web page."},
	{"generate_pdf", []string{"pdf", "报告", "文档", "导出", "report"}, "Render Markdown content into a PDF and deliver it."},
	{"office", []string{"ppt", "幻灯片", "演示", "表格", "excel", "xlsx", "pptx", "办公", "slide", "spreadsheet", "presentation", "deck"}, "Write a spreadsheet (.xlsx) or presentation (.pptx) into the workspace."},
	{"bash", []string{"命令", "脚本", "运行", "执行", "安装", "python", "shell", "command", "script", "run"}, "Run one local command in the bound workspace."},
	{"delegate_task", []string{"编程", "写代码", "子任务", "开发", "coding", "delegate", "subtask", "code"}, "Delegate one self-contained subtask to the coding agent."},
	{"send_file", []string{"发送", "发文件", "发给", "附件", "send", "deliver", "file"}, "Deliver the produced file to the current channel."},
	{"read_file", []string{"读文件", "查看文件", "读取", "read"}, "Read a local file."},
	{"write_file", []string{"写文件", "保存", "创建文件", "write", "save"}, "Create or modify a local file."},
	{"edit_file", []string{"编辑", "修改文件", "edit"}, "Edit a local file."},
	{"list_directory", []string{"列目录", "列表", "ls", "dir", "list"}, "List a local directory."},
	{"search_files", []string{"找文件", "搜索文件", "grep", "find"}, "Search local files."},
	{"download_file", []string{"下载", "download", "图片", "照片", "附件下载", "image", "photo", "picture", "img"}, "Download a remote resource into a local artifact."},
	{"open", []string{"打开", "启动", "open", "launch"}, "Open a file, application, or URL with the system handler."},
	{"screenshot", []string{"截图", "截屏", "screenshot"}, "Capture the desktop screenshot and deliver it."},
	{"current_datetime", []string{"时间", "日期", "几点", "time", "date", "clock"}, "Read the current date and time."},
	{"git_status", []string{"git", "状态", "diff", "变更"}, "Inspect repository status and diffs."},
	{"git_commit", []string{"提交", "commit", "push", "推送"}, "Commit and push repository changes."},
	{"build_verify", []string{"构建", "编译", "测试", "build", "test", "lint"}, "Run a reviewed build, test, or lint task."},
	{"browser", []string{"浏览器", "browser"}, "Drive a web browser session."},
	{"computer_use", []string{"桌面", "鼠标", "键盘", "desktop", "computer"}, "Observe or drive the local desktop."},
	{"ssh", []string{"ssh", "远程", "remote"}, "Run a command on a remote host over SSH."},
	{"memory_recall", []string{"记忆", "回忆", "memory", "recall"}, "Recall agent memory."},
	{"memory", []string{"记住", "偏好", "memory", "remember", "preference"}, "Read or update agent memory."},
	{"knowledge_search", []string{"知识库", "knowledge"}, "Search the local knowledge base."},
	{"knowledge_save_text", []string{"保存知识", "记住", "ingest"}, "Save text into the knowledge base."},
	{"knowledge_maintain", []string{"知识库维护", "维护知识", "刷新知识", "maintain", "refresh knowledge"}, "Administer knowledge-base sources and maintenance."},
	{"manage_schedule", []string{"日程", "提醒", "定时", "schedule", "cron", "remind"}, "Administer local schedules and reminders."},
	{"task", []string{"任务", "待办", "task", "todo"}, "Track local tasks."},
	{"goal", []string{"目标", "goal"}, "Manage long-running goals."},
	{"record_audio", []string{"录音", "record", "audio"}, "Record microphone audio."},
	{"asr", []string{"转写", "语音识别", "transcribe", "asr"}, "Transcribe speech audio."},
	{"session_search", []string{"会话", "历史", "审计", "session", "audit"}, "Search session history and audit records."},
	{"manage_template", []string{"模板", "template"}, "Manage session templates."},
	{"manage_config", []string{"配置", "设置", "config", "settings"}, "Read or update assistant configuration."},
	{"send_to_im", []string{"发到", "指定对象", "im", "send_to"}, "Deliver a file to a specified IM target."},
	{"send_im_text", []string{"发消息", "文本消息", "text message"}, "Send a text message to a specified IM target."},
	{"list_sessions", []string{"会话列表", "sessions"}, "List sessions."},
	{"mis_data", []string{"业务", "mis", "business"}, "Query or administer business data."},
}

// semanticToolsSearchPlanCapabilities maps inventory names onto the
// capability their plan selections would carry, so discovery can tell "in
// this turn's plan" apart from "not routed this turn at all". Three kinds of
// names live here: petitionable names that can also be planned outright (the
// planned status is more accurate when the turn already routes them); legacy aliases the managed catalog unpublished in
// favor of a trusted adapter (list_directory, search_files, edit_file — the
// capability may be planned, but the alias itself never renders, so they are
// never petitionable); and capability-backed names no rule label can ever
// route (memory_recall rides ambient retrieval, build_verify rides the coding
// rule, send_im_text stays quarantined), which always report unavailable.
var semanticToolsSearchPlanCapabilities = map[string]tool.CapabilityID{
	"web_search":    "information.search.web",
	"web_fetch":     tool.CapabilityInformationFetchWeb,
	"bash":          tool.CapabilityShellExecuteLocal,
	"delegate_task": tool.CapabilityAgentDelegateSubtask,
	"download_file": tool.CapabilityArtifactAcquireRemote,
	"office":        tool.CapabilityDocumentWriteOffice,
	"generate_pdf":  "document.generate.file",
	"send_file":     "artifact.deliver.current_channel",
	// Legacy aliases for the trusted file adapters (never rendered managed).
	"list_directory": tool.CapabilityFSReadLocal,
	"search_files":   tool.CapabilityFSReadLocal,
	"edit_file":      tool.CapabilityFSWriteLocal,
	// Capability-backed but never rule-routed on a managed chat surface.
	"memory_recall": tool.CapabilityMemoryRecallAgent,
	"build_verify":  tool.CapabilityBuildVerifyLocal,
	"send_im_text":  tool.CapabilityMessageSendIM,
}

// semanticToolsSearchNameCapability resolves the capability a managed surface
// would plan or petition for an inventory name, from whichever map backs it.
func semanticToolsSearchNameCapability(name string) (tool.CapabilityID, bool) {
	if capability, ok := semanticPetitionableCapabilities[name]; ok {
		return capability, true
	}
	capability, ok := semanticToolsSearchPlanCapabilities[name]
	return capability, ok
}

// semanticToolsSearchRun executes one deterministic discovery query against
// the host inventory. Status is derived from the live surface and the turn's
// petition budget: listed names are callable now, petitionable names are
// reachable by calling them once, planned names unlock as earlier steps
// complete, and everything else must be stated as unavailable — an ambiguous
// status invites the model to call names the petition gate will reject
// (production 2026-08-27: "按计划路由提供" read as "call it", eight wasted
// iterations on office while the turn rode the coding surface).
func semanticToolsSearchRun(cb *sharedAgentLoopCallbacks, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "[system rejected] tools_search_arguments_invalid"
	}
	query, _ := args["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return "[system rejected] tools_search_query_required"
	}
	lower := strings.ToLower(query)
	type hit struct {
		entry semanticToolsSearchEntry
		score int
	}
	hits := make([]hit, 0, 8)
	for _, entry := range semanticToolsSearchInventory {
		score := 0
		// The entry's own name is its strongest keyword: a model that already
		// guessed the exact spelling ("office") must get that entry back —
		// answering "(no matching capability)" to the correct name tells the
		// model the tool does not exist, and it stops petitioning a capability
		// the plan may even have scheduled (2026-08-27 birthday-deck turn:
		// office was planned, the agent asked for it by name, heard "nothing",
		// and burned the discovery budget panicking).
		if lower == entry.name {
			score += 4
		} else if strings.Contains(lower, entry.name) || strings.Contains(entry.name, lower) {
			score += 2
		}
		for _, keyword := range entry.keywords {
			if keyword != "" && strings.Contains(lower, strings.ToLower(keyword)) {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, hit{entry, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].entry.name < hits[j].entry.name
	})
	if len(hits) > 8 {
		hits = hits[:8]
	}
	var out strings.Builder
	fmt.Fprintf(&out, "tools_search results for %q:\n", query)
	if len(hits) == 0 {
		out.WriteString("(no matching capability)\n")
	}
	for _, h := range hits {
		fmt.Fprintf(&out, "- %s — %s %s\n", h.entry.name, h.entry.summary, semanticToolsSearchStatus(cb, h.entry.name))
	}
	out.WriteString("只有标记「可请愿」的未列出名字才可以直接调用一次请愿（每轮每类限一次）；标记「已列入本轮计划/已用尽/不可用」的名字调用必被拒绝，不要尝试。")
	return out.String()
}

func semanticToolsSearchStatus(cb *sharedAgentLoopCallbacks, name string) string {
	surface := cb.semanticSurface
	if surface != nil {
		if _, ok := surface.grants[name]; ok {
			return "[已在当前工具面]"
		}
		if _, ok := surface.retiredGrants[name]; ok {
			return "[本轮授权已用尽，不要调用]"
		}
	}
	// A capability the turn already routes is planned, not petitionable: the
	// petition expansion would add nothing and fail after spending the budget,
	// so the planned status must win for names in both maps.
	if capability, ok := semanticToolsSearchNameCapability(name); ok && semanticSurfacePlansCapability(surface, name, capability) {
		return "[已列入本轮计划：前置步骤完成后自动出现在列表，不要直接调用]"
	}
	if _, ok := semanticPetitionableCapabilities[name]; ok {
		consumed := cb.semanticPetitionConsumed
		if semanticPetitionIsEffectful(name) {
			consumed = cb.semanticEffectfulPetitionConsumed
		}
		if consumed {
			return "[本轮请愿机会已用完，不要调用]"
		}
		return "[可请愿：直接调用一次]"
	}
	return "[本轮不可用：不要调用，用已列出的工具完成]"
}

func semanticSurfacePlansCapability(surface *semanticCallSurface, name string, capability tool.CapabilityID) bool {
	if surface == nil {
		return false
	}
	for _, selection := range surface.plan.Selections {
		if selection.AdapterName == name || selection.FitProof.MatchedCapability == capability {
			return true
		}
	}
	return false
}
