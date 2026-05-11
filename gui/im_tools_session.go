package main

import (
	"encoding/json"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"log"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/project"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// checkSessionTaskGuard returns a non-empty hint string when the current
// user message should NOT create a coding session. Returns "" only for
// explicit coding tasks.
func (h *IMMessageHandler) checkSessionTaskGuard() string {
	result := h.classifyTaskIntentForSessionGuard(h.lastUserText)

	// If the current message is clearly a coding task, allow immediately.
	if result.Intent == intentCoding {
		return ""
	}

	// Context-aware gating: when the current message is ambiguous/unknown,
	// check the GateIntentClassifier for semantic classification. The GIC
	// handles continuation phrases ("继续"/"开工"/"直接做") through its
	// gateContPhrases + hasCodingSignals pipeline (or UIC delegation when
	// available), returning GateIntentContinuation for coding continuations.
	//
	// This avoids local text-rule guessing for continuation handling.
	if result.Intent == intentAmbiguous || result.Intent == intentUnknown {
		// GateIntentClassifier: five-category semantic classification for the
		// session guard. Consulted before the generic IntentClassifier because
		// it produces gate-specific categories (new_project, bug_fix,
		// maintenance, non_coding, continuation) that map directly to
		// allow/block decisions.
		if gic := h.getGateIntentClassifier(); gic != nil {
			gResult := gic.Classify(h.lastUserText, h.lastUserID)
			switch gResult.Intent {
			case GateIntentNewProject, GateIntentBugFix, GateIntentMaintenance:
				return "" // coding-related → allow session
			case GateIntentContinuation:
				return "" // continuation → allow session
			case GateIntentNonCoding:
				return nonCodingSessionHint(gResult)
			}
			// GateIntentUnknown — fall through to generic IntentClassifier below.
		}

		// Hybrid intent classifier: use embedding + LLM for semantic classification
		// when the first-pass semantic result is ambiguous.
		if h.app != nil && h.getAppToolRouter() != nil {
			if ic := h.getAppToolRouter().IntentClassifier(); ic != nil {
				icResult := ic.Classify(h.lastUserText)
				switch icResult.Intent {
				case tool.IntentCoding:
					return "" // semantic classifier says coding → allow
				case tool.IntentQuery:
					// Knowledge question, not an action → block session creation
					return "Task intent: semantic classification indicates a knowledge question, not an action. Do not create a coding session; answer the user directly."
				case tool.IntentSSH:
					// Fall through to the SSH hint below
					result.Intent = intentSSH
				case tool.IntentContent:
					result.Intent = intentNonCoding
				}
			}
		}
	}

	return formatSemanticSessionGuardHint(result)
}

func formatSemanticSessionGuardHint(result taskIntentResult) string {
	switch result.Intent {
	case intentSSH:
		return fmt.Sprintf(`Task intent: semantic classification indicates an SSH/server operation (%s). Do not create a coding session.
Use the ssh tool instead:
- ssh(action="connect", ...): connect to the server
- ssh(action="exec", session_id="...", command="..."): run a short command
- ssh(action="exec_background", session_id="...", command="..."): run a long command, deployment, install, or build
- ssh(action="upload"/"download", ...): transfer files
Only call create_session when the task semantically requires modifying project code.`, formatIntentEvidence(result))
	case intentNonCoding:
		return fmt.Sprintf(`Task intent: semantic classification indicates this is not a coding task (%s). Do not create a coding session.
Use direct tools instead:
- bash: run local commands or scripts
- craft_tool: generate and execute a task-specific script
- read_file / write_file / edit_file: read, write, or edit local files
- send_file: send a file to the user
- open: open a file or URL
- memory: save or retrieve information
Only call create_session when the task semantically requires a coding session.`, formatIntentEvidence(result))
	case intentUnknown, intentAmbiguous:
		return fmt.Sprintf(`Task intent is still ambiguous (%s). Do not create a coding session yet.
Clarify the goal first:
- If the user needs project code changes, bug fixes, or feature implementation, create a coding session after clarification.
- If the user needs server login, logs, service restart, upload, or download, use the ssh tool after clarification.
When semantic intent is unavailable or ambiguous, do not open coding tools automatically.`, formatIntentEvidence(result))
	default:
		return ""
	}
}

// conversationHasCodingContext checks whether the recent conversation history
// contains evidence of a coding task (e.g. previous messages discussed
// development, code, projects). This allows short follow-up messages like
// "开工" to pass through the session guard when context is established.
func (h *IMMessageHandler) conversationHasCodingContext() bool {
	if uic := h.getUnifiedClassifier(); uic != nil {
		return h.conversationHasCodingContextUIC(uic)
	}
	return false
}

// conversationHasCodingContextUIC checks recent conversation history for
// coding context using the UIC's semantic classification. Classifies the
// most recent user message — if it's coding-like, the conversation has
// coding context.
//
// Only checks one message (not 5) because:
//  1. Each UIC.Classify() may trigger L2+L3 (embedding + LLM), costing 2-8s
//  2. If the most recent user message is coding-related, that's sufficient
//     context for a follow-up "开工" to be treated as continuation
func (h *IMMessageHandler) conversationHasCodingContextUIC(uic *intent.UnifiedIntentClassifier) bool {
	if h.memory == nil {
		return false
	}
	userID := h.lastUserID
	if userID == "" {
		userID = desktopUserID
	}
	entries := h.memory.Load(userID)
	if len(entries) == 0 {
		return false
	}
	// Find the most recent user message (skip the current one which triggered this check).
	for i := len(entries) - 1; i >= 0; i-- {
		text, ok := entries[i].Content.(string)
		if !ok || strings.TrimSpace(text) == "" {
			continue
		}
		if entries[i].Role != "user" {
			continue
		}
		// Skip if this is the same text as the current message (avoid self-match).
		if strings.TrimSpace(text) == strings.TrimSpace(h.lastUserText) {
			continue
		}
		result := uic.Classify(intent.MessageContext{Text: text})
		return result.IsCodingLike()
	}
	return false
}

// nonCodingSessionHint returns a user-facing hint message when the
// GateIntentClassifier determines the user's request is a non-coding task.
// The hint includes the classifier's reason to help the user understand why
// session creation was blocked.
func nonCodingSessionHint(result GateIntentResult) string {
	reason := strings.TrimSpace(result.Reason)
	if reason == "" {
		reason = "non-coding task detected"
	}
	return fmt.Sprintf(`⚠️ 任务类型检测：当前请求看起来不是编程任务（%s），不需要创建编程会话。

请直接使用以下工具完成任务：
- bash：执行命令行操作（如 curl 下载、脚本执行）
- craft_tool：自动生成并执行脚本（适合数据处理、API 调用、文件转换）
- read_file / write_file / edit_file：读写和局部编辑本地文件
- send_file：将文件发送给用户
- open：打开文件或网址
- memory：保存/检索信息

如果确实需要编程会话，请在下一轮重新调用 create_session。`, reason)
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
	cfg, cfgErr := h.loadConfig()
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

	// Default provider injection: if no explicit provider was given, and the
	// resolved tool matches the user's configured default tool, use the
	// configured default provider as the providerOverride for ProviderResolver.
	isDefaultProviderOverride := false
	if provider == "" && cfg.DefaultTool != "" {
		resolvedNorm := strings.ToLower(strings.TrimSpace(tool))
		defaultNorm := strings.ToLower(strings.TrimSpace(cfg.DefaultTool))
		if resolvedNorm == defaultNorm && strings.TrimSpace(cfg.DefaultToolProvider) != "" {
			provider = cfg.DefaultToolProvider
			isDefaultProviderOverride = true
		}
	}

	resolver := &ProviderResolver{}
	resolveResult, resolveErr := resolver.Resolve(toolCfg, provider)
	if resolveErr != nil && isDefaultProviderOverride {
		// Default provider invalid (not found or no API key), retry without
		// override so ProviderResolver falls back to auto-resolution.
		log.Printf("default provider override %q failed (%v), retrying with auto-resolution", provider, resolveErr)
		provider = ""
		isDefaultProviderOverride = false
		resolveResult, resolveErr = resolver.Resolve(toolCfg, "")
	}
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

	starter := h.getSessionStarter()
	if starter == nil {
		h.ensureInteractionInfra()
		starter = h.getSessionStarter()
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
		Provider:           resolvedProvider,
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

	// Emit code:session_start event for the code preview panel.
	if h.app != nil && h.app.codeEventEmitter != nil {
		h.app.codeEventEmitter.EmitSessionStart(view.ID)
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
	cfg, err := h.loadConfig()
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
	actionText, _ := args["action"].(string)
	action := normalizeProjectToolAction(actionText)
	switch action {
	case projectToolActionCreate:
		name, _ := args["name"].(string)
		path, _ := args["path"].(string)
		res, err := project.Create(h.app, name, path)
		if err != nil {
			return fmt.Sprintf("创建项目失败: %v", err)
		}
		data, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "path": res.Path, "status": "created"})
		return string(data)
	case projectToolActionList:
		items, err := project.List(h.app)
		if err != nil {
			return fmt.Sprintf("加载配置失败: %v", err)
		}
		if len(items) == 0 {
			return "当前没有已配置的项目。请在桌面端添加项目。"
		}
		data, _ := json.Marshal(items)
		return string(data)
	case projectToolActionDelete:
		target, _ := args["target"].(string)
		res, err := project.Delete(h.app, target)
		if err != nil {
			return fmt.Sprintf("删除项目失败: %v", err)
		}
		data, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "status": "deleted"})
		return string(data)
	case projectToolActionSwitch:
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

	maxLines := sessionOutputLineLimit(args)
	waitForSessionStartupOutput(session)

	snapshot := snapshotSessionOutput(session)
	hintFacts := collectSessionOutputHintFacts(session, snapshot.Status, snapshot.RawLines)
	return renderSessionOutput(sessionID, maxLines, snapshot, hintFacts)
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
	userIDForOrch := h.lastUserID
	var taskOrch *TaskExecutionOrchestrator
	if h.taskOrchestratorRegistry != nil && userIDForOrch != "" {
		taskOrch = h.taskOrchestratorRegistry.Get(userIDForOrch)
	}
	if taskOrch != nil && taskOrch.IsActive() {
		task := taskOrch.CurrentTask()
		if task != nil {
			// Record which session is handling this task.
			taskOrch.SetCurrentSessionID(sessionID)

			// If the task is pending, this is the initial send — use the
			// orchestrator's focused prompt instead of the LLM's text.
			if task.Status == TaskExecPending {
				taskPrompt := taskOrch.BuildTaskPrompt()
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
				taskOrch.MarkCurrentStatus(TaskExecInProgress, "")
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

// toolControlSession merges interrupt_session and kill_session into one tool.
func (h *IMMessageHandler) toolControlSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	actionText, _ := args["action"].(string)
	action := normalizeSessionControlAction(actionText)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	switch action {
	case sessionControlActionInterrupt:
		if err := h.manager.Interrupt(sessionID); err != nil {
			return fmt.Sprintf("中断失败: %s", err.Error())
		}
		return fmt.Sprintf("已向会话 %s 发送中断信号", sessionID)
	case sessionControlActionKill:
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
	lower := strings.ToLower(userText)
	idx := strings.Index(lower, strings.ToLower(filePathPromptPrefix))
	if idx < 0 {
		return false
	}
	block := userText[idx+len(filePathPromptPrefix):]
	for _, line := range strings.Split(block, "\n") {
		if classifyLocalImagePathLine(line) == localImagePathLineImagePath {
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
			if ds, err := remote.DownsizeScreenshotBase64(base64Data, 1_200_000); err == nil {
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
				if ds, err := remote.DownsizeScreenshotBase64(base64Data, 1_200_000); err == nil {
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
	if !normalizeIMMessagePlatformKind(platform).IsDesktopPlaybackTarget() {
		captureStart2 := time.Now()
		base64Data, err := h.manager.CaptureScreenshotToBase64(sessionID)
		log.Printf("[screenshot] CaptureScreenshotToBase64 took %v, data_len=%d, err=%v", time.Since(captureStart2), len(base64Data), err)
		if err != nil {
			return fmt.Sprintf("截图失败: %s", err.Error())
		}
		h.lastScreenshotAt = time.Now()
		// Preemptive downsize for IM delivery.
		if len(base64Data) > 1_500_000 {
			if ds, err := remote.DownsizeScreenshotBase64(base64Data, 1_200_000); err == nil {
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
