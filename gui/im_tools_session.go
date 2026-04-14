package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/project"
)

// nonCodingKeywords are phrases that strongly indicate a non-coding task.
// When the user message contains these AND none of the coding keywords,
// we block session creation and guide the LLM to use direct tools instead.
// All entries MUST be lowercase (matched against lowercased user text).
var nonCodingKeywords = []string{
	"搜索论文", "搜论文", "找论文", "查论文", "下载论文",
	"翻译", "全文翻译", "翻译成中文", "翻译成英文",
	"生成pdf", "生成 pdf", "导出pdf", "导出 pdf",
	"ppt", "pptx", "宣传ppt", "宣传 ppt", "演示文稿", "演示稿", "幻灯片", "presentation", "slides",
	"查天气", "天气预报", "今天天气",
	"查快递", "快递单号", "物流查询",
	"搜索新闻", "查新闻", "最新新闻",
	"总结文章", "总结论文", "摘要",
	"发邮件", "写邮件", "发送邮件",
	"提醒我", "设个闹钟",
	"播放音乐", "放首歌",
	"知识库", "放入知识库", "导入知识库", "入库",
	"报告", "评测报告", "文档处理",
	"arxiv",
}

// codingKeywords are phrases that indicate a genuine coding task.
// If any of these appear, the guard does NOT block session creation.
// All entries MUST be lowercase (matched against lowercased user text).
var codingKeywords = []string{
	"写代码", "改代码", "修改代码", "编程", "开发", "修bug", "修 bug", "修复bug", "修复 bug", "bug",
	"重构", "refactor", "实现", "添加功能", "新增功能",
	"写脚本", "写一个脚本", "写个脚本",
	"写函数", "写方法", "写接口", "写api", "写 api",
	"代码", "源码", "源代码",
	"编译", "构建", "build", "compile",
	"测试", "单元测试", "test",
	"部署", "deploy",
	"pull request", "merge request",
	"git commit", "git push",
	"create_session", // explicit tool name = intentional
}

// codingActionPhrases are short action/continuation phrases that indicate
// the user wants to start or continue a coding task. These are NOT added to
// codingKeywords because they are too generic on their own — they only
// indicate coding intent when conversation history already contains coding
// context. Used by checkSessionTaskGuard for context-aware gating.
var codingActionPhrases = []string{
	"开工", "开干", "动手", "搞起来", "搞起", "干吧", "做吧",
	"开始吧", "开始做", "开始干", "开始搞",
	"let's go", "let's do it", "let's start", "let's begin",
}

// checkSessionTaskGuard returns a non-empty hint string when the current
// user message should NOT create a coding session. Returns "" only for
// explicit coding tasks.
func (h *IMMessageHandler) checkSessionTaskGuard() string {
	result := classifyTaskIntent(h.lastUserText)

	// If the current message is clearly a coding task, allow immediately.
	if result.Intent == intentCoding {
		return ""
	}

	// Context-aware gating: when the current message is ambiguous/unknown but
	// contains an action phrase (e.g. "开工", "动手", "start"), check whether
	// the conversation history already established coding context. If so,
	// treat the action phrase as a continuation signal and allow session creation.
	if result.Intent == intentAmbiguous || result.Intent == intentUnknown {
		if hasCodingActionPhrase(h.lastUserText) && h.conversationHasCodingContext() {
			return ""
		}
	}

	switch result.Intent {
	case intentSSH:
		return fmt.Sprintf(`⚠️ 任务类型检测：当前请求更像 SSH/服务器操作任务（检测到特征：%s），不要创建编程会话。

请改用 ssh 工具处理远程操作：
- ssh(action="connect", ...)：连接服务器
- ssh(action="exec", session_id="...", command="...")：执行短命令
- ssh(action="exec_background", session_id="...", command="...")：执行长命令/部署/安装/编译
- ssh(action="upload"/"download", ...)：传输文件

只有明确需要修改项目代码时，才调用 create_session。`, formatIntentEvidence(result))
	case intentNonCoding:
		return fmt.Sprintf(`⚠️ 任务类型检测：当前请求看起来不是编程任务（检测到特征：%s），不需要创建编程会话。

请直接使用以下工具完成任务：
- bash：执行命令行操作（如 curl 下载、脚本执行）
- craft_tool：自动生成并执行脚本（适合数据处理、API 调用、文件转换）
- read_file / write_file / edit_file：读写和局部编辑本地文件
- send_file：将文件发送给用户
- open：打开文件或网址
- memory：保存/检索信息

如果确实需要编程会话，请在下一轮重新调用 create_session。`, formatIntentEvidence(result))
	case intentUnknown, intentAmbiguous:
		return fmt.Sprintf(`⚠️ 任务类型检测：当前请求还不能确定是编程任务还是 SSH/服务器操作（检测到特征：%s）。

现在不要创建编程会话。请先澄清目标：
- 如果是修改项目代码、修 bug、实现功能，再调用 create_session
- 如果是登录服务器、查看日志、重启服务、上传下载文件，请改用 ssh 工具

在任务不明确时，不要自动打开编程工具。`, formatIntentEvidence(result))
	default:
		return ""
	}
}

// hasCodingActionPhrase checks whether the user message contains a short
// action/continuation phrase that signals "start working" (e.g. "开工",
// "动手", "start"). These phrases are too generic to classify as coding
// on their own, but combined with conversation context they indicate the
// user wants to proceed with a previously discussed coding task.
func hasCodingActionPhrase(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, phrase := range codingActionPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// conversationHasCodingContext checks whether the recent conversation history
// contains evidence of a coding task (e.g. previous messages discussed
// development, code, projects). This allows short follow-up messages like
// "开工" to pass through the session guard when context is established.
func (h *IMMessageHandler) conversationHasCodingContext() bool {
	if h.memory == nil {
		return false
	}
	userID := h.lastUserID
	if userID == "" {
		userID = "desktop-user" // fallback for desktop mode
	}
	entries := h.memory.load(userID)
	if len(entries) == 0 {
		return false
	}
	// Scan the last few entries (up to 10) for coding keywords.
	start := 0
	if len(entries) > 10 {
		start = len(entries) - 10
	}
	for i := start; i < len(entries); i++ {
		text, ok := entries[i].Content.(string)
		if !ok {
			continue
		}
		lower := strings.ToLower(text)
		for _, kw := range codingKeywords {
			if strings.Contains(lower, kw) {
				return true
			}
		}
	}
	return false
}

func (h *IMMessageHandler) toolCreateSession(args map[string]interface{}) string {
	// --- Intent guard ---
	// Only explicit coding tasks may create a coding session. SSH/server tasks
	// must go through the ssh tool, and ambiguous tasks must clarify first.
	if hint := h.checkSessionTaskGuard(); hint != "" {
		return hint
	}

	tool, _ := args["tool"].(string)
	projectPath, _ := args["project_path"].(string)
	projectID, _ := args["project_id"].(string)
	provider, _ := args["provider"].(string)

	var hints []string

	// Smart tool recommendation when tool is empty.
	if tool == "" && h.contextResolver != nil {
		recommended, reason := h.contextResolver.ResolveTool(projectPath, "")
		if recommended != "" {
			tool = recommended
			hints = append(hints, fmt.Sprintf("🔧 自动推荐工具: %s（%s）", tool, reason))
		}
	}
	if tool == "" {
		return "缺少 tool 参数，且无法自动推荐工具"
	}

	// Resolve project_id to project path (takes priority over project_path).
	cfg, cfgErr := h.app.LoadConfig()
	if cfgErr != nil {
		return fmt.Sprintf("加载配置失败: %s", cfgErr.Error())
	}
	if projectID != "" {
		var found bool
		for _, p := range cfg.Projects {
			if p.Id == projectID {
				projectPath = p.Path
				found = true
				hints = append(hints, fmt.Sprintf("📁 通过项目 ID 解析: %s → %s", projectID, p.Path))
				break
			}
		}
		if !found {
			var available []string
			for _, p := range cfg.Projects {
				available = append(available, fmt.Sprintf("%s(%s)", p.Id, p.Name))
			}
			if len(available) == 0 {
				return fmt.Sprintf("项目 ID %q 未找到，当前没有已配置的项目", projectID)
			}
			return fmt.Sprintf("项目 ID %q 未找到，可用项目: %s", projectID, strings.Join(available, ", "))
		}
	}

	// Smart project detection when project_path is empty.
	if projectPath == "" && h.contextResolver != nil {
		detected, reason := h.contextResolver.ResolveProject()
		if detected != "" {
			projectPath = detected
			hints = append(hints, fmt.Sprintf("📁 自动检测项目: %s（%s）", projectPath, reason))
		}
	}

	// Pre-launch environment check.
	if h.sessionPrecheck != nil {
		result := h.sessionPrecheck.Check(tool, projectPath)
		if !result.ToolReady {
			hints = append(hints, fmt.Sprintf("⚠️ 工具预检未通过: %s", result.ToolHint))
		}
		if !result.ProjectReady {
			hints = append(hints, "⚠️ 项目路径不存在或无法访问")
		}
		if !result.ModelReady {
			hints = append(hints, fmt.Sprintf("⚠️ 模型预检未通过: %s", result.ModelHint))
		}
		if result.AllPassed {
			hints = append(hints, "✅ 环境预检全部通过")
		}
		// Block session creation when the tool binary is missing — launching
		// a process that doesn't exist always exits immediately with code 1,
		// wasting a session slot and confusing the user with a cryptic error.
		if !result.ToolReady {
			return strings.Join(hints, "\n") + "\n❌ 工具未安装，无法创建会话。请先在桌面端安装 " + tool + " 后重试。"
		}
	}

	// ProviderResolver integration: resolve provider before starting session.
	toolCfg, tcErr := remoteToolConfig(cfg, tool)
	if tcErr != nil {
		return fmt.Sprintf("获取工具配置失败: %s", tcErr.Error())
	}

	resolver := &ProviderResolver{}
	resolveResult, resolveErr := resolver.Resolve(toolCfg, provider)
	if resolveErr != nil {
		errMsg := fmt.Sprintf("❌ 无法创建会话：%s\n请在桌面端为 %s 配置至少一个有效的服务商。", resolveErr.Error(), tool)
		return errMsg
	}
	if resolveResult.Fallback {
		hints = append(hints, fmt.Sprintf("⚡ 服务商已降级: %s → %s", resolveResult.OriginalName, resolveResult.Provider.ModelName))
	}
	resolvedProvider := resolveResult.Provider.ModelName

	// Pre-launch banner: show the user what tool/provider/project will be used
	// before the session is actually created. This helps users verify the
	// configuration at a glance.
	hints = append(hints, fmt.Sprintf("🚀 即将启动编程会话：\n   🔧 编程工具: %s\n   📦 服务商: %s\n   📁 工作目录: %s", tool, resolvedProvider, projectPath))

	resumeSessionID, _ := args["resume_session_id"].(string)

	starter := h.app.sessionStarter
	if starter == nil {
		h.app.ensureInteractionInfra()
		starter = h.app.sessionStarter
	}
	if starter == nil {
		return "会话启动器未初始化"
	}
	parentRunID := ""
	if h.currentLoopCtx != nil {
		parentRunID = h.currentLoopCtx.RunID
	}
	startResult, err := starter.Start(CodingSessionStartRequest{
		Tool:               tool,
		ProjectID:          projectID,
		ProjectPath:        projectPath,
		Provider:           provider,
		ResumeSessionID:    resumeSessionID,
		InjectResumePrompt: false,
		LaunchSource:       RemoteLaunchSourceAI,
		ParentRunID:        parentRunID,
	})
	if err != nil {
		errMsg := fmt.Sprintf("❌ 创建会话失败: %s", err.Error())
		errMsg += fmt.Sprintf("\n💡 修复建议:\n- 检查 %s 是否已安装并可正常运行\n- 确认项目路径 %s 存在且可访问\n- 使用 list_providers 查看可用服务商配置", tool, projectPath)
		return errMsg
	}
	view := startResult.View
	resolvedProvider = startResult.ResolvedProvider
	if strings.TrimSpace(startResult.ResolvedProjectPath) != "" {
		projectPath = startResult.ResolvedProjectPath
	}
	if len(startResult.Hints) > 0 {
		hints = append(hints, startResult.Hints...)
	}

	// Start monitoring session startup progress in background.
	h.app.ensureStartupFeedback()
	if h.startupFeedback != nil {
		h.startupFeedback.WatchStartup(view.ID, func(msg string) {
			// Progress messages are logged; in a real IM context the
			// onProgress callback from the agent loop would relay these.
			fmt.Fprintf(os.Stderr, "startup_feedback[%s]: %s\n", view.ID, msg)
		})
	}

	var b strings.Builder
	for _, hint := range hints {
		b.WriteString(hint)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("✅ 会话已创建 [%s]\n", view.ID))
	b.WriteString(fmt.Sprintf("\n📋 下一步操作："))
	b.WriteString(fmt.Sprintf("\n1. 调用 get_session_output(session_id=%q) 确认会话已启动（状态为 running）", view.ID))
	b.WriteString(fmt.Sprintf("\n2. 立即调用 send_and_observe(session_id=%q, text=\"编程指令\") 将需求发送给编程工具", view.ID))
	b.WriteString("\n⚠️ 编程工具启动后等待输入，不发送指令不会开始工作。最多检查 2 次 get_session_output，确认 running 后立即发送。")
	b.WriteString("\n🛑 如果会话已退出（exited）且退出码非 0，不要重试，直接告知用户错误信息。")
	return b.String()
}

func (h *IMMessageHandler) toolListProviders(args map[string]interface{}) string {
	toolName, _ := args["tool"].(string)
	if toolName == "" {
		return "缺少 tool 参数"
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return fmt.Sprintf("加载配置失败: %s", err.Error())
	}
	toolCfg, err := remoteToolConfig(cfg, toolName)
	if err != nil {
		return fmt.Sprintf("不支持的工具: %s", toolName)
	}
	valid := validProviders(toolCfg)
	if len(valid) == 0 {
		return fmt.Sprintf("工具 %s 没有可用的服务商，请在桌面端配置", toolName)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("工具 %s 的可用服务商:\n", toolName))
	for _, m := range valid {
		isDefault := ""
		if strings.EqualFold(m.ModelName, toolCfg.CurrentModel) {
			isDefault = " [当前默认]"
		}
		modelId := m.ModelId
		if len(modelId) > 20 {
			modelId = modelId[:20] + "..."
		}
		b.WriteString(fmt.Sprintf("  - %s (model_id=%s)%s\n", m.ModelName, modelId, isDefault))
	}
	return b.String()
}

func (h *IMMessageHandler) toolProjectManage(args map[string]interface{}) string {
	action, _ := args["action"].(string)
	action = strings.TrimSpace(action)
	switch action {
	case "create":
		name, _ := args["name"].(string)
		path, _ := args["path"].(string)
		res, err := project.Create(h.app, name, path)
		if err != nil {
			return fmt.Sprintf("创建项目失败: %v", err)
		}
		data, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "path": res.Path, "status": "created"})
		return string(data)
	case "list":
		items, err := project.List(h.app)
		if err != nil {
			return fmt.Sprintf("加载配置失败: %v", err)
		}
		if len(items) == 0 {
			return "当前没有已配置的项目。请在桌面端添加项目。"
		}
		data, _ := json.Marshal(items)
		return string(data)
	case "delete":
		target, _ := args["target"].(string)
		res, err := project.Delete(h.app, target)
		if err != nil {
			return fmt.Sprintf("删除项目失败: %v", err)
		}
		data, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "status": "deleted"})
		return string(data)
	case "switch":
		target, _ := args["target"].(string)
		res, err := project.Switch(h.app, target)
		if err != nil {
			return fmt.Sprintf("切换项目失败: %v", err)
		}
		data, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "path": res.Path, "status": "switched"})
		return string(data)
	default:
		return fmt.Sprintf("未知 action: %s（支持 create/list/delete/switch）", action)
	}
}

func (h *IMMessageHandler) toolSendInput(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	text, _ := args["text"].(string)
	if sessionID == "" || text == "" {
		return "缺少 session_id 或 text 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	if err := h.manager.WriteInput(sessionID, text); err != nil {
		return fmt.Sprintf("发送失败: %s", err.Error())
	}
	return fmt.Sprintf("已发送到会话 %s。⚠️ 你必须立即调用 get_session_output(session_id=%q) 验证命令是否执行成功，不要直接告诉用户已完成。", sessionID, sessionID)
}

// writeAutoResumeHint appends auto-resume instructions to the output buffer
// when a structured session exits with a ResumeContext. The reason parameter
// provides a context-specific description of why the session exited.
func writeAutoResumeHint(b *strings.Builder, rc *SessionResumeContext, reason string) {
	if rc == nil {
		b.WriteString("\n✅ 编程工具已正常退出。如需继续，请创建新会话。")
		return
	}
	if rc.ResumeCount >= 10 {
		b.WriteString("\n✅ 编程工具已退出。已自动续接 10 次，建议告知用户当前进度并询问是否继续。")
		return
	}
	b.WriteString(fmt.Sprintf("\n🔄 %s", reason))
	b.WriteString(fmt.Sprintf("\n📌 自动续接指令（第 %d 次续接）：", rc.ResumeCount+1))
	resumeSessionID := strings.TrimSpace(rc.ResumeSessionID)
	if resumeSessionID == "" {
		resumeSessionID = strings.TrimSpace(rc.ClaudeSessionID)
	}
	if resumeSessionID != "" {
		b.WriteString("\n1. 立即调用 create_session 创建新会话（使用相同的 tool 和 project_path，并传入 resume_session_id）")
		b.WriteString(fmt.Sprintf("\n   例如：create_session(tool=%q, project_path=%q, resume_session_id=%q)", rc.Tool, rc.ProjectPath, resumeSessionID))
	} else {
		b.WriteString("\n1. 立即调用 create_session 创建新会话（使用相同的 tool 和 project_path）")
	}
	b.WriteString("\n2. 用 send_and_observe 发送续接指令：")
	b.WriteString("\n   「请检查项目当前状态，继续完成之前未完成的任务。")
	if rc.OriginalTask != "" {
		b.WriteString(fmt.Sprintf("原始任务：%s。", rc.OriginalTask))
	}
	if rc.LastProgress != "" {
		b.WriteString(fmt.Sprintf("上次进度：%s。", rc.LastProgress))
	}
	if len(rc.CompletedFiles) > 0 {
		b.WriteString(fmt.Sprintf("已完成的文件：%s。", strings.Join(rc.CompletedFiles, ", ")))
	}
	b.WriteString("查看已有文件，补全缺失的部分，确保项目可以正常运行。」")
	b.WriteString("\n⚠️ 不要询问用户是否继续——直接创建新会话续接。不要自己用 write_file 写代码。")
}

func (h *IMMessageHandler) toolGetSessionOutput(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sessionID)
	}

	maxLines := 30
	if n, ok := args["lines"].(float64); ok && n > 0 {
		maxLines = int(n)
		if maxLines > 100 {
			maxLines = 100
		}
	}

	// When the session is still in "starting" state with no output, wait
	// briefly (up to ~5s) for the process to either produce output or exit.
	// This avoids returning an empty "(暂无输出)" result that causes the
	// LLM agent to poll repeatedly, wasting many iterations.
	session.mu.RLock()
	isStarting := session.Status == SessionStarting
	hasOutput := len(session.RawOutputLines) > 0
	session.mu.RUnlock()

	if isStarting && !hasOutput {
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			session.mu.RLock()
			changed := session.Status != SessionStarting || len(session.RawOutputLines) > 0
			session.mu.RUnlock()
			if changed {
				break
			}
		}
	}

	session.mu.RLock()
	status := string(session.Status)
	summary := session.Summary
	rawLines := make([]string, len(session.RawOutputLines))
	copy(rawLines, session.RawOutputLines)
	session.mu.RUnlock()

	var b strings.Builder
	b.WriteString(fmt.Sprintf("会话 %s 状态: %s\n", sessionID, status))
	if summary.CurrentTask != "" {
		b.WriteString(fmt.Sprintf("当前任务: %s\n", summary.CurrentTask))
	}
	if summary.ProgressSummary != "" {
		b.WriteString(fmt.Sprintf("进度: %s\n", summary.ProgressSummary))
	}
	if summary.LastResult != "" {
		b.WriteString(fmt.Sprintf("最近结果: %s\n", summary.LastResult))
	}
	if summary.LastCommand != "" {
		b.WriteString(fmt.Sprintf("最近命令: %s\n", summary.LastCommand))
	}
	if summary.WaitingForUser {
		b.WriteString("⚠️ 会话正在等待用户输入\n")
	}
	if summary.SuggestedAction != "" {
		b.WriteString(fmt.Sprintf("建议操作: %s\n", summary.SuggestedAction))
	}
	if len(rawLines) > 0 {
		start := 0
		if len(rawLines) > maxLines {
			start = len(rawLines) - maxLines
		}
		b.WriteString(fmt.Sprintf("\n--- 最近 %d 行输出 ---\n", len(rawLines)-start))
		for _, line := range rawLines[start:] {
			b.WriteString(line)
			b.WriteString("\n")
		}
	} else {
		b.WriteString("\n(暂无输出)\n")
		// When the session is running but has no output, it's likely waiting
		// for the first user message (SDK mode tools like Claude Code wait
		// for input after init). Hint the AI to send the task.
		if status == string(SessionRunning) {
			b.WriteString(fmt.Sprintf("\n📌 会话已就绪但暂无输出——编程工具在等待输入。请立即调用 send_and_observe(session_id=%q, text=\"编程指令\") 发送任务。", sessionID))
		} else if status == string(SessionStarting) {
			b.WriteString("\n⏳ 会话正在启动中，请稍后再次调用 get_session_output 检查状态（最多再检查 1 次）。")
		}
	}

	// When the session is busy (coding tool actively executing), hint the
	// Agent based on the current stall detection state.
	if status == string(SessionBusy) {
		// Check if recent output contains API retry messages — this means
		// the upstream API is rate-limiting or temporarily unavailable, and
		// Claude Code is automatically retrying. The agent should wait
		// patiently instead of giving up or sending new messages.
		hasAPIRetry := false
		for i := len(rawLines) - 1; i >= 0 && i >= len(rawLines)-10; i-- {
			if strings.Contains(rawLines[i], "API retry") || strings.Contains(rawLines[i], "api_retry") {
				hasAPIRetry = true
				break
			}
		}

		session.mu.RLock()
		stallState := session.StallState
		session.mu.RUnlock()

		if hasAPIRetry {
			b.WriteString("\n⏳ 上游 API 限流/暂时不可用，编程工具正在自动重试中。这是正常现象，请耐心等待（可能需要 30-60 秒）。")
			b.WriteString("\n⚠️ 不要放弃或创建新会话——API 重试通常会成功。稍后再调用 get_session_output 检查进度。")
		} else {
			switch stallState {
			case StallStateSuspected:
				b.WriteString("\n⏳ 编程工具输出暂停，系统正在尝试恢复，请稍后再检查")
			case StallStateStuck:
				b.WriteString("\n⚠️ 编程工具可能已卡住，建议发送具体指令或终止会话")
			default: // StallStateNormal
				b.WriteString("\n⏳ 编程工具正在工作中，请等待后再检查进度")
			}
		}
	}

	// When the session is waiting for input, hint the Agent based on the
	// semantic completion analysis result.
	if status == string(SessionWaitingInput) {
		// Check if the session just recovered from an API error — the last
		// few lines may contain error messages. If so, hint the agent to
		// retry the instruction instead of giving up.
		hasRecentAPIError := false
		for i := len(rawLines) - 1; i >= 0 && i >= len(rawLines)-5; i-- {
			if strings.Contains(rawLines[i], "API retry") || strings.Contains(rawLines[i], "❌") {
				hasRecentAPIError = true
				break
			}
		}

		session.mu.RLock()
		completionLevel := session.CompletionLevel
		session.mu.RUnlock()

		if hasRecentAPIError && completionLevel != CompletionCompleted {
			b.WriteString("\n⚠️ 编程工具遇到 API 错误后恢复，任务可能未完成。")
			b.WriteString(fmt.Sprintf("\n📌 请重新发送指令让编程工具继续工作：send_and_observe(session_id=%q, text=\"继续完成之前的任务\")", sessionID))
			b.WriteString("\n不要放弃——API 错误是暂时的，重试通常会成功。")
		} else {
			switch completionLevel {
			case CompletionCompleted:
				b.WriteString("\n✅ 任务似乎已完成，可以查看结果")
			case CompletionIncomplete:
				if session.isStructuredSession() {
					session.mu.RLock()
					continueCount := session.AutoContinueCount
					session.mu.RUnlock()

					if continueCount >= 10 {
						b.WriteString("\n⚠️ 已自动续接 10 次，建议告知用户当前进度并询问是否继续。")
					} else {
						b.WriteString("\n🔄 编程工具因 token/turn 限制暂停，任务未完成。")
						b.WriteString(fmt.Sprintf("\n📌 立即调用 send_and_observe(session_id=%q, text=\"继续完成之前的任务\") 让编程工具继续工作。", sessionID))
						b.WriteString("\n⚠️ 不要询问用户是否继续——直接发送续接指令。")
					}
				} else {
					b.WriteString("\n⚠️ 任务似乎未完成，建议发送「继续」让编程工具继续工作")
				}
			default:
				// CompletionUncertain: for structured sessions, default to
				// suggesting continuation — it's better to send one extra
				// "continue" than to leave the task half-done.
				if session.isStructuredSession() {
					b.WriteString(fmt.Sprintf("\n📌 编程工具已暂停。调用 send_and_observe(session_id=%q, text=\"继续\") 让编程工具继续工作，或查看输出判断任务是否已完成。", sessionID))
				}
				// Non-structured sessions: keep existing default hint ("⚠️ 会话正在等待用户输入")
			}
		}
	}

	// When the session has exited with a non-zero code, append a strong
	// stop-loss hint so the LLM agent stops retrying and reports the
	// failure to the user immediately.
	session.mu.RLock()
	var exitCodeVal *int
	if session.ExitCode != nil {
		cp := *session.ExitCode
		exitCodeVal = &cp
	}
	sessionStatus := session.Status
	sessionTool := session.Tool
	session.mu.RUnlock()

	if (sessionStatus == SessionExited || sessionStatus == SessionError) && exitCodeVal != nil && *exitCodeVal != 0 {
		// Structured sessions (Claude Code SDK, Gemini ACP, Codex, iFlow)
		// normally exit with code 1 — this is NOT a failure.
		if session.isStructuredSession() && *exitCodeVal == 1 {
			// Check if there's a resume context indicating unfinished work.
			session.mu.RLock()
			rc := session.ResumeContext
			session.mu.RUnlock()

			writeAutoResumeHint(&b, rc, "编程工具因 token 耗尽正常退出，但任务可能未完成。")
		} else if session.isStructuredSession() && *exitCodeVal > 1 {
			// Determine whether the error is fatal (unrecoverable) or
			// transient (worth retrying). Fatal errors include missing API
			// keys, authentication failures, and tool-not-found — retrying
			// these will just waste iterations. Everything else (rate
			// limits, network timeouts, server errors) is retryable.
			session.mu.RLock()
			rc := session.ResumeContext
			session.mu.RUnlock()

			isFatal := isFatalSessionError(rawLines)
			resumeCount := 0
			if rc != nil {
				resumeCount = rc.ResumeCount
			}

			if isFatal {
				b.WriteString(fmt.Sprintf("\n🛑 编程工具遇到不可恢复的错误退出（退出码 %d）。", *exitCodeVal))
				b.WriteString(fmt.Sprintf("\n请立即将错误信息告知用户，并建议检查 %s 的 API Key 配置和安装。", sessionTool))
				b.WriteString("\n不要重试——这是配置问题，重试不会解决。")
			} else if resumeCount < 3 {
				b.WriteString(fmt.Sprintf("\n⚠️ 编程工具异常退出（退出码 %d），可能是 API 限流、网络波动或临时服务故障。", *exitCodeVal))
				b.WriteString(fmt.Sprintf("\n📌 自动重试（第 %d/3 次）：立即创建新会话重试，使用相同参数。", resumeCount+1))
				b.WriteString("\n不要询问用户——直接重试。这类错误通常是暂时的，重试大概率会成功。")
			} else {
				b.WriteString(fmt.Sprintf("\n🛑 编程工具已连续失败 %d 次（退出码 %d）。", resumeCount, *exitCodeVal))
				b.WriteString(fmt.Sprintf("\n请将错误信息告知用户，建议检查 %s 的上游 API 状态或稍后再试。", sessionTool))
			}
		} else {
			// Non-structured sessions (PTY mode) — also apply retry logic
			// instead of immediately giving up.
			isFatal := isFatalSessionError(rawLines)
			if isFatal {
				b.WriteString(fmt.Sprintf("\n🛑 会话遇到不可恢复的错误退出（退出码 %d）。", *exitCodeVal))
				b.WriteString(fmt.Sprintf("\n请立即将错误信息告知用户，并建议检查 %s 的安装和配置。", sessionTool))
			} else {
				b.WriteString(fmt.Sprintf("\n⚠️ 会话异常退出（退出码 %d），可能是临时错误。", *exitCodeVal))
				b.WriteString("\n📌 建议创建新会话重试。如果连续失败，请将错误信息告知用户。")
			}
		}
	}

	// Structured sessions that exit with code 0 may also have unfinished
	// work (e.g. Claude Code completed its max-turns but the task isn't
	// done). Check ResumeContext for these sessions too.
	if (sessionStatus == SessionExited) && exitCodeVal != nil && *exitCodeVal == 0 && session.isStructuredSession() {
		session.mu.RLock()
		rc := session.ResumeContext
		session.mu.RUnlock()

		writeAutoResumeHint(&b, rc, "编程工具已正常退出（可能达到 max-turns 限制），任务可能未完成。")
	}

	return b.String()
}

func (h *IMMessageHandler) toolGetSessionEvents(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sessionID)
	}
	session.mu.RLock()
	events := make([]ImportantEvent, len(session.Events))
	copy(events, session.Events)
	session.mu.RUnlock()
	if len(events) == 0 {
		return fmt.Sprintf("会话 %s 暂无重要事件。", sessionID)
	}
	var b strings.Builder
	for _, ev := range events {
		b.WriteString(fmt.Sprintf("- [%s] %s: %s", ev.Severity, ev.Type, ev.Title))
		if ev.Summary != "" {
			b.WriteString(fmt.Sprintf(" — %s", ev.Summary))
		}
		if ev.RelatedFile != "" {
			b.WriteString(fmt.Sprintf(" (文件: %s)", ev.RelatedFile))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolInterruptSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	if err := h.manager.Interrupt(sessionID); err != nil {
		return fmt.Sprintf("中断失败: %s", err.Error())
	}
	return fmt.Sprintf("已向会话 %s 发送中断信号", sessionID)
}

func (h *IMMessageHandler) toolKillSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	if err := h.manager.Kill(sessionID); err != nil {
		return fmt.Sprintf("终止失败: %s", err.Error())
	}
	return fmt.Sprintf("已终止会话 %s", sessionID)
}

// toolSendAndObserve combines send_input + get_session_output into a single
// tool call. It sends text to a session, waits briefly for output to
// accumulate, then returns the session output — saving one LLM round-trip.
//
// When the TaskExecutionOrchestrator is active, the text is automatically
// enriched with per-task context (task description, acceptance criteria,
// dependency outputs) to prevent the LLM from dumping the entire project
// description into a single session.
func (h *IMMessageHandler) toolSendAndObserve(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	text, _ := args["text"].(string)
	timeoutSeconds, _ := args["timeout_seconds"].(float64)

	// Task orchestrator enrichment: when active, prepend per-task context
	// to the text being sent to the coding session.
	if h.taskOrchestrator != nil && h.taskOrchestrator.IsActive() {
		task := h.taskOrchestrator.CurrentTask()
		if task != nil {
			// Record which session is handling this task.
			h.taskOrchestrator.SetCurrentSessionID(sessionID)

			// If the task is pending, this is the initial send — use the
			// orchestrator's focused prompt instead of the LLM's text.
			if task.Status == TaskExecPending {
				taskPrompt := h.taskOrchestrator.BuildTaskPrompt()
				if taskPrompt != "" {
					// Prepend the structured task prompt; append the LLM's
					// original text as supplementary context (it may contain
					// useful details the orchestrator doesn't know about).
					if strings.TrimSpace(text) != "" {
						text = taskPrompt + "\n\n---\n补充说明：\n" + text
					} else {
						text = taskPrompt
					}
					log.Printf("[task-orchestrator] enriched send_and_observe for task %d: %s",
						task.Index+1, task.Title)
				}
				h.taskOrchestrator.MarkCurrentStatus(TaskExecInProgress, "")
			}
		}
	}

	return SendAndObserveSession(h.manager, sessionID, text, SessionObserveOptions{
		TimeoutSeconds: timeoutSeconds,
		Lines:          40,
	}, func(renderArgs map[string]interface{}) string {
		return h.toolGetSessionOutput(renderArgs)
	})
}

// isFatalSessionError scans the recent output lines for patterns that
// indicate an unrecoverable configuration error (missing API key,
// authentication failure, tool not installed, etc.). These errors will
// not be fixed by retrying — the user must fix the configuration first.
// Returns false for transient errors (rate limits, network timeouts,
// server 5xx, etc.) which are worth retrying.
func isFatalSessionError(lines []string) bool {
	// Only scan the last 20 lines — error messages are near the end.
	start := 0
	if len(lines) > 20 {
		start = len(lines) - 20
	}
	for _, line := range lines[start:] {
		lower := strings.ToLower(line)
		// API key / authentication issues
		if strings.Contains(lower, "api key") && (strings.Contains(lower, "missing") || strings.Contains(lower, "invalid") || strings.Contains(lower, "not set") || strings.Contains(lower, "not found") || strings.Contains(lower, "not configured")) {
			return true
		}
		if strings.Contains(lower, "authentication failed") {
			return true
		}
		if strings.Contains(lower, "invalid_api_key") || strings.Contains(lower, "invalid api key") {
			return true
		}
		// HTTP 401 — only match explicit status code patterns, not random numbers
		if strings.Contains(lower, "status 401") || strings.Contains(lower, "http 401") || strings.Contains(lower, "error 401") || strings.Contains(lower, "code 401") || strings.Contains(lower, "401 unauthorized") {
			return true
		}
		// Tool not installed
		if strings.Contains(lower, "command not found") || strings.Contains(lower, "not recognized as") || strings.Contains(lower, "is not recognized") {
			return true
		}
		if strings.Contains(lower, "no such file or directory") && (strings.Contains(lower, "claude") || strings.Contains(lower, "codex") || strings.Contains(lower, "gemini")) {
			return true
		}
		// Permission denied at OS level (not API rate-limit "permission")
		if strings.Contains(lower, "permission denied") && !strings.Contains(lower, "rate") && !strings.Contains(lower, "api") {
			return true
		}
	}
	return false
}

// toolControlSession merges interrupt_session and kill_session into one tool.
func (h *IMMessageHandler) toolControlSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	action, _ := args["action"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	switch action {
	case "interrupt":
		if err := h.manager.Interrupt(sessionID); err != nil {
			return fmt.Sprintf("中断失败: %s", err.Error())
		}
		return fmt.Sprintf("已向会话 %s 发送中断信号", sessionID)
	case "kill":
		if err := h.manager.Kill(sessionID); err != nil {
			return fmt.Sprintf("终止失败: %s", err.Error())
		}
		return fmt.Sprintf("已终止会话 %s", sessionID)
	default:
		return "action 参数无效，可选值: interrupt, kill"
	}
}

// toolManageConfig merges all config operations into a single tool.
// screenshotCooldown is the minimum interval between consecutive screenshots
// to prevent accidental rapid-fire captures by the LLM.
const screenshotCooldown = 30 * time.Second

func hasSelectedLocalImagePath(userText string) bool {
	const filePathPromptPrefix = "[用户选择的本地文件路径]"
	lower := strings.ToLower(userText)
	idx := strings.Index(lower, strings.ToLower(filePathPromptPrefix))
	if idx < 0 {
		return false
	}
	block := userText[idx+len(filePathPromptPrefix):]
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if trimmed == "" {
			continue
		}
		lowerLine := strings.ToLower(trimmed)
		if strings.HasPrefix(lowerLine, "这是用户已经提供的本地图片文件") || strings.HasPrefix(lowerLine, "请直接使用这些路径") {
			continue
		}
		if strings.HasSuffix(lowerLine, ".png") || strings.HasSuffix(lowerLine, ".jpg") || strings.HasSuffix(lowerLine, ".jpeg") || strings.HasSuffix(lowerLine, ".gif") || strings.HasSuffix(lowerLine, ".bmp") || strings.HasSuffix(lowerLine, ".webp") || strings.HasSuffix(lowerLine, ".svg") || strings.HasSuffix(lowerLine, ".ico") || strings.HasSuffix(lowerLine, ".tif") || strings.HasSuffix(lowerLine, ".tiff") {
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) toolScreenshot(args map[string]interface{}) string {
	if hasSelectedLocalImagePath(h.lastUserText) {
		return "用户消息里已经提供了本地图片文件路径。不要调用 screenshot 或重新截图；请直接使用这些路径，并优先用 read_file 或 open 查看图片内容。"
	}

	// Enforce cooldown to prevent accidental repeated screenshots.
	if !h.lastScreenshotAt.IsZero() && time.Since(h.lastScreenshotAt) < screenshotCooldown {
		remaining := screenshotCooldown - time.Since(h.lastScreenshotAt)
		return fmt.Sprintf("截屏冷却中，请等待 %d 秒后再试", int(remaining.Seconds())+1)
	}

	// Check for display parameter — capture a specific monitor directly.
	if displayRaw, ok := args["display"]; ok {
		var displayIndex int
		switch v := displayRaw.(type) {
		case float64:
			displayIndex = int(v)
		case int:
			displayIndex = v
		case string:
			if _, err := fmt.Sscanf(v, "%d", &displayIndex); err != nil {
				return fmt.Sprintf("display 参数无效: %s", v)
			}
		default:
			return fmt.Sprintf("display 参数类型无效: %T", displayRaw)
		}
		if h.manager == nil {
			return "会话管理器未初始化"
		}
		captureStart := time.Now()
		base64Data, err := h.manager.CaptureScreenshotDirectForDisplay(displayIndex)
		log.Printf("[screenshot] CaptureScreenshotDirectForDisplay(%d) took %v, data_len=%d, err=%v",
			displayIndex, time.Since(captureStart), len(base64Data), err)
		if err != nil {
			return fmt.Sprintf("截取显示器 %d 失败: %s", displayIndex, err.Error())
		}
		h.lastScreenshotAt = time.Now()
		if len(base64Data) > 1_500_000 {
			if ds, err := downsizeScreenshotBase64(base64Data, 1_200_000); err == nil {
				base64Data = ds
			}
		}
		return fmt.Sprintf("[screenshot_base64]%s", base64Data)
	}

	sessionID, _ := args["session_id"].(string)

	// 如果未指定 session_id，自动选择唯一活跃会话
	if sessionID == "" && h.manager != nil {
		sessions := h.manager.List()
		if len(sessions) == 1 {
			sessionID = sessions[0].ID
		} else if len(sessions) > 1 {
			var lines []string
			lines = append(lines, "有多个活跃会话，请指定 session_id：")
			for _, s := range sessions {
				s.mu.RLock()
				status := string(s.Status)
				s.mu.RUnlock()
				lines = append(lines, fmt.Sprintf("- %s (工具=%s, 状态=%s)", s.ID, s.Tool, status))
			}
			return strings.Join(lines, "\n")
		} else {
			// 没有活跃会话时，直接截屏本机屏幕（不依赖 session）
			captureStart := time.Now()
			base64Data, err := h.manager.CaptureScreenshotDirect()
			log.Printf("[screenshot] CaptureScreenshotDirect took %v, data_len=%d, err=%v", time.Since(captureStart), len(base64Data), err)
			if err != nil {
				return fmt.Sprintf("截图失败: %s", err.Error())
			}
			h.lastScreenshotAt = time.Now()
			// Preemptive downsize for IM delivery (multi-monitor can be huge).
			if len(base64Data) > 1_500_000 {
				if ds, err := downsizeScreenshotBase64(base64Data, 1_200_000); err == nil {
					base64Data = ds
				}
			}
			return fmt.Sprintf("[screenshot_base64]%s", base64Data)
		}
	}

	if sessionID == "" {
		return "缺少 session_id 参数，且无法自动选择会话"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}

	// Non-desktop platforms (WeChat, QQ, etc.) cannot receive session.image
	// WebSocket pushes, so capture and return base64 data directly.
	platform := ""
	if h.currentLoopCtx != nil {
		platform = h.currentLoopCtx.Platform
	}
	if platform != "" && platform != "desktop" {
		captureStart2 := time.Now()
		base64Data, err := h.manager.CaptureScreenshotToBase64(sessionID)
		log.Printf("[screenshot] CaptureScreenshotToBase64 took %v, data_len=%d, err=%v", time.Since(captureStart2), len(base64Data), err)
		if err != nil {
			return fmt.Sprintf("截图失败: %s", err.Error())
		}
		h.lastScreenshotAt = time.Now()
		// Preemptive downsize for IM delivery.
		if len(base64Data) > 1_500_000 {
			if ds, err := downsizeScreenshotBase64(base64Data, 1_200_000); err == nil {
				base64Data = ds
			}
		}
		return fmt.Sprintf("[screenshot_base64]%s", base64Data)
	}

	if err := h.manager.CaptureScreenshot(sessionID); err != nil {
		return fmt.Sprintf("截图失败: %s", err.Error())
	}
	// 截图已通过 session.image 通道直接发送给用户，
	// 返回特殊标记让 runAgentLoop 立即终止，避免 Agent 继续推理导致重复发图。
	h.lastScreenshotAt = time.Now()
	return "[screenshot_sent]"
}
