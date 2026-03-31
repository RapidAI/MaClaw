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
	"查天气", "天气预报", "今天天气",
	"查快递", "快递单号", "物流查询",
	"搜索新闻", "查新闻", "最新新闻",
	"总结文章", "总结论文", "摘要",
	"发邮件", "写邮件", "发送邮件",
	"提醒我", "设个闹钟",
	"播放音乐", "放首歌",
	"arxiv",
}

// codingKeywords are phrases that indicate a genuine coding task.
// If any of these appear, the guard does NOT block session creation.
// All entries MUST be lowercase (matched against lowercased user text).
var codingKeywords = []string{
	"写代码", "编程", "开发", "修bug", "修 bug", "修复bug", "修复 bug",
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

// checkNonCodingTaskGuard returns a non-empty hint string when the current
// user message looks like a non-coding task. Returns "" to allow session creation.
func (h *IMMessageHandler) checkNonCodingTaskGuard() string {
	msg := strings.ToLower(h.lastUserText)
	if msg == "" {
		return "" // no context available, allow
	}

	// If any coding keyword is present, always allow.
	for _, kw := range codingKeywords {
		if strings.Contains(msg, kw) {
			return ""
		}
	}

	// Check for non-coding keywords.
	matched := ""
	for _, kw := range nonCodingKeywords {
		if strings.Contains(msg, kw) {
			matched = kw
			break
		}
	}
	if matched == "" {
		return "" // no non-coding signal, allow
	}

	return fmt.Sprintf(`⚠️ 任务类型检测：当前请求看起来不是编程任务（检测到关键词：%q），不需要创建编程会话。

请直接使用以下工具完成任务：
- bash：执行命令行操作（如 curl 下载、脚本执行）
- craft_tool：自动生成并执行脚本（适合数据处理、API 调用、文件转换）
- read_file / write_file：读写本地文件
- send_file：将文件发送给用户
- open：打开文件或网址
- memory：保存/检索信息

如果确实需要编程会话，请在下一轮重新调用 create_session。`, matched)
}

func (h *IMMessageHandler) toolCreateSession(args map[string]interface{}) string {
	// --- Non-coding task guard ---
	// Detect when the LLM incorrectly tries to create a coding session for
	// tasks that don't require one (e.g. search, translate, generate PDF).
	if hint := h.checkNonCodingTaskGuard(); hint != "" {
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

	resumeSessionID, _ := args["resume_session_id"].(string)

	view, err := h.app.StartRemoteSessionForProject(RemoteStartSessionRequest{
		Tool: tool, ProjectPath: projectPath, Provider: resolvedProvider,
		LaunchSource:    RemoteLaunchSourceAI,
		ResumeSessionID: resumeSessionID,
	})
	if err != nil {
		errMsg := fmt.Sprintf("❌ 创建会话失败: %s", err.Error())
		errMsg += fmt.Sprintf("\n💡 修复建议:\n- 检查 %s 是否已安装并可正常运行\n- 确认项目路径 %s 存在且可访问\n- 使用 list_providers 查看可用服务商配置", tool, projectPath)
		return errMsg
	}

	// Start monitoring session startup progress in background.
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
	b.WriteString(fmt.Sprintf("🔧 工具: %s | 📦 服务商: %s | 📁 项目: %s\n", view.Tool, resolvedProvider, projectPath))
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
	b.WriteString("\n1. 立即调用 create_session 创建新会话（使用相同的 tool 和 project_path）")
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
		session.mu.RLock()
		stallState := session.StallState
		session.mu.RUnlock()

		switch stallState {
		case StallStateSuspected:
			b.WriteString("\n⏳ 编程工具输出暂停，系统正在尝试恢复，请稍后再检查")
		case StallStateStuck:
			b.WriteString("\n⚠️ 编程工具可能已卡住，建议发送具体指令或终止会话")
		default: // StallStateNormal
			b.WriteString("\n⏳ 编程工具正在工作中，请等待后再检查进度")
		}
	}

	// When the session is waiting for input, hint the Agent based on the
	// semantic completion analysis result.
	if status == string(SessionWaitingInput) {
		session.mu.RLock()
		completionLevel := session.CompletionLevel
		session.mu.RUnlock()

		switch completionLevel {
		case CompletionCompleted:
			b.WriteString("\n✅ 任务似乎已完成，可以查看结果")
		case CompletionIncomplete:
			b.WriteString("\n⚠️ 任务似乎未完成，建议发送「继续」让编程工具继续工作")
			// CompletionUncertain: 保持现有默认提示（"⚠️ 会话正在等待用户输入"）
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
			// API error or unexpected crash — allow retry.
			session.mu.RLock()
			rc := session.ResumeContext
			session.mu.RUnlock()

			if rc != nil && rc.ExitReason == "api_error" && rc.ResumeCount < 2 {
				b.WriteString(fmt.Sprintf("\n⚠️ 编程工具异常退出（退出码 %d），可能是上游 API 不稳定。", *exitCodeVal))
				b.WriteString(fmt.Sprintf("\n📌 自动重试（第 %d 次）：立即创建新会话重试，使用相同参数。", rc.ResumeCount+1))
				b.WriteString("\n不要询问用户——直接重试。")
			} else {
				b.WriteString(fmt.Sprintf("\n🛑 会话已失败退出（退出码 %d）。不要再对此会话调用任何工具。", *exitCodeVal))
				b.WriteString(fmt.Sprintf("\n请立即将错误信息告知用户，并建议检查 %s 的安装和配置。", sessionTool))
				b.WriteString("\n不要重复创建新会话重试——同样的环境问题会导致同样的失败。")
			}
		} else {
			b.WriteString(fmt.Sprintf("\n🛑 会话已失败退出（退出码 %d）。不要再对此会话调用任何工具。", *exitCodeVal))
			b.WriteString(fmt.Sprintf("\n请立即将错误信息告知用户，并建议检查 %s 的安装和配置。", sessionTool))
			b.WriteString("\n不要重复创建新会话重试——同样的环境问题会导致同样的失败。")
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
func (h *IMMessageHandler) toolSendAndObserve(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	text, _ := args["text"].(string)
	if sessionID == "" || text == "" {
		return "缺少 session_id 或 text 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}

	// Snapshot line count and image count BEFORE sending so we can detect new output/images.
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sessionID)
	}
	session.mu.RLock()
	baseLineCount := len(session.RawOutputLines)
	baseImageCount := len(session.OutputImages)
	session.mu.RUnlock()

	if err := h.manager.WriteInput(sessionID, text); err != nil {
		return fmt.Sprintf("发送失败: %s", err.Error())
	}

	// Poll up to ~30s with increasing intervals, waiting for meaningful output.
	// We use newLines > 1 (not > 0) because the PTY echo of the sent text
	// typically produces 1 line immediately; real output starts after that.
	//
	// If the caller provides a positive timeout_seconds (capped at 120s),
	// we build a custom polling array that totals approximately that many
	// seconds, starting with the same ramp-up pattern and then filling
	// with 3 000 ms intervals.
	waitMs := []int{500, 500, 1000, 1000, 1500, 1500, 2000, 2000, 3000, 3000, 3000, 3000, 3000, 3000, 3000}
	if ts, ok := args["timeout_seconds"].(float64); ok && ts > 0 {
		if ts > 120.0 {
			ts = 120.0
		}
		targetMs := int(ts * 1000)
		base := []int{500, 500, 1000, 1000, 1500, 1500, 2000}
		sum := 0
		for _, v := range base {
			sum += v
		}
		custom := make([]int, len(base))
		copy(custom, base)
		for sum < targetMs {
			custom = append(custom, 3000)
			sum += 3000
		}
		waitMs = custom
	}
	for _, ms := range waitMs {
		time.Sleep(time.Duration(ms) * time.Millisecond)
		session.mu.RLock()
		newLines := len(session.RawOutputLines) - baseLineCount
		waiting := session.Summary.WaitingForUser
		status := session.Status
		session.mu.RUnlock()
		// Stop early if: meaningful new output, session waiting for input, or session ended.
		if newLines > 1 || waiting || status == SessionExited || status == SessionError {
			break
		}
	}

	// Check if session is still busy after polling — read stall state for precise hint.
	session.mu.RLock()
	stillBusy := session.Status == SessionBusy
	stallState := session.StallState
	session.mu.RUnlock()

	// Check if new images were produced during the command execution.
	// Images from SDK sessions are already delivered to the user via the
	// session.image WebSocket channel (Hub → Feishu notifier), so we do NOT
	// return [screenshot_base64] here (that would cause duplicate delivery).
	// Instead, append a note to the text output so the AI knows an image
	// was sent and can reference it in its response.
	session.mu.RLock()
	newImageCount := len(session.OutputImages) - baseImageCount
	session.mu.RUnlock()

	output := h.toolGetSessionOutput(map[string]interface{}{
		"session_id": sessionID,
		"lines":      float64(40),
	})

	// NOTE: busy/stall hints are already appended by toolGetSessionOutput above,
	// so we do NOT duplicate them here. The stillBusy/stallState variables are
	// retained for potential future use (e.g. deciding whether to retry).
	_ = stillBusy
	_ = stallState

	if newImageCount > 0 {
		output += fmt.Sprintf("\n\n📷 会话产生了 %d 张图片，已自动通过 IM 发送给用户。", newImageCount)
	}

	return output
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
func (h *IMMessageHandler) toolManageConfig(args map[string]interface{}) string {
	action, _ := args["action"].(string)
	switch action {
	case "get":
		return h.toolGetConfig(args)
	case "update":
		return h.toolUpdateConfig(args)
	case "batch_update":
		return h.toolBatchUpdateConfig(args)
	case "list_schema":
		return h.toolListConfigSchema()
	case "export":
		return h.toolExportConfig()
	case "import":
		return h.toolImportConfig(args)
	default:
		return "action 参数无效，可选值: get, update, batch_update, list_schema, export, import"
	}
}

// screenshotCooldown is the minimum interval between consecutive screenshots
// to prevent accidental rapid-fire captures by the LLM.
const screenshotCooldown = 30 * time.Second

func (h *IMMessageHandler) toolScreenshot(args map[string]interface{}) string {
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
