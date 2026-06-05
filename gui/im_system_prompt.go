package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	cworkflow "github.com/RapidAI/CodeClaw/corelib/workflow"
)

func (h *IMMessageHandler) buildSystemPrompt() string {
	return h.buildSystemPromptBase(false)
}

func (h *IMMessageHandler) buildIMEntrySystemPrompt(msg IMUserMessage, history []agent.ConversationEntry, loopCtx *LoopContext, workflowAgentLoop bool, askUserContext, pendingUserReplyContext, capabilityGapContext string) string {
	promptBuildStart := time.Now()

	var systemPrompt string
	if h.memoryStore != nil {
		systemPrompt = h.buildSystemPromptWithMemory(msg.Text, len(history) == 0, loopCtx)
	} else {
		systemPrompt = h.buildSystemPromptBaseWithExperienceContext(false, lifecycle.EventContext{}, loopCtx, msg.Text)
	}
	basePromptElapsed := time.Since(promptBuildStart)

	resumeStart := time.Now()
	systemPrompt += h.buildResumeTraceContextWithLang(msg.UserID, msg.Text, msg.Lang)
	resumeElapsed := time.Since(resumeStart)

	if workflowAgentLoop && h.getWorkflowEngine() != nil {
		if stashed, ok := h.stashedPhasePrompt.LoadAndDelete(msg.UserID); ok {
			systemPrompt += "\n" + stashed.(string)
		} else if phasePrompt := h.getWorkflowEngine().BuildPhasePrompt(msg.UserID); phasePrompt != "" {
			systemPrompt += "\n" + phasePrompt
		}
	} else {
		h.stashedPhasePrompt.Delete(msg.UserID)
		h.workflowOriginalRequest.Delete(msg.UserID)
	}

	if askUserContext != "" {
		systemPrompt += "\n\n" + askUserContext
	}
	if pendingUserReplyContext != "" {
		systemPrompt += "\n\n" + pendingUserReplyContext
	}
	if capabilityGapContext != "" {
		systemPrompt += "\n\n" + capabilityGapContext
	}

	platformKind := normalizeIMMessagePlatformKind(msg.Platform)
	if platformKind.IsDesktop() {
		systemPrompt += desktopWorkflowDocOverride()
	} else if platformKind.IsKnown() || msg.Platform != "" {
		systemPrompt += imWorkflowDocDeliveryRule()
	}

	totalPromptBuild := time.Since(promptBuildStart)
	if totalPromptBuild > 500*time.Millisecond {
		log.Printf("[buildIMEntrySystemPrompt] slow: base_prompt=%v resume_trace=%v total=%v prompt_len=%d user=%s",
			basePromptElapsed, resumeElapsed, totalPromptBuild, len(systemPrompt), msg.UserID)
	}
	imPerfLog("system_prompt", promptBuildStart, imRequestID(msg), msg.UserID, "base_prompt", basePromptElapsed, "resume_trace", resumeElapsed, "prompt_len", len(systemPrompt), "history_len", len(history), "workflow", workflowAgentLoop)
	return systemPrompt
}

func (h *IMMessageHandler) buildSystemPromptBase(includeMemoryGuide bool, userMessage ...string) string {
	return h.buildSystemPromptBaseWithExperienceContext(includeMemoryGuide, lifecycle.EventContext{}, nil, userMessage...)
}

func (h *IMMessageHandler) buildSystemPromptBaseWithExperienceContext(includeMemoryGuide bool, eventContext lifecycle.EventContext, loopCtx *LoopContext, userMessage ...string) string {
	// Load config once for all decisions.
	roleName := "MaClaw"
	roleDesc := "一个尽心尽责无所不能的软件开发管家"
	isProMode := false
	currentNickname := ""
	trialReflectEnabled := false
	var cfg corelib.AppConfig
	if loadedCfg, err := h.loadConfig(); err == nil {
		cfg = loadedCfg
		if cfg.MaclawRoleName != "" {
			roleName = cfg.MaclawRoleName
		}
		if cfg.MaclawRoleDescription != "" {
			roleDesc = cfg.MaclawRoleDescription
		}
		isProMode = normalizeUIModeKind(cfg.UIMode).IsProExplicit()
		currentNickname = strings.TrimSpace(cfg.RemoteNickname)
		trialReflectEnabled = isProMode && cfg.TrialReflectEnabled
	}

	msg := ""
	if len(userMessage) > 0 {
		msg = userMessage[0]
	}
	promptUserID := h.promptRuntimeUserID(loopCtx)

	// Build deps for the shared BuildSystemPrompt.
	deps := agent.SystemPromptDeps{
		Config: agent.SystemPromptConfig{
			RoleName:          roleName,
			RoleDescription:   roleDesc,
			IsProMode:         isProMode,
			Nickname:          currentNickname,
			HasCodingSessions: true,
			TrialReflect:      trialReflectEnabled,
		},
		MemoryStore:      h.memoryStore,
		SkipMemoryRecall: true, // GUI handles memory recall in appendGUIEpilogue (with memory index, derived facts, knowledge auto-recall, frozen snapshot caching)
		HasKnowledgeBase: true,
	}

	// SSH hosts
	if loadedCfg, err := h.loadConfig(); err == nil && len(loadedCfg.SSHHosts) > 0 {
		deps.SSHHostLister = func() []corelib.SSHHostEntry { return loadedCfg.SSHHosts }
	}

	// Steering
	if h.steeringStore != nil {
		deps.SteeringResolver = func(userMessage string, contextTokens int) []steering.File {
			ctx := steering.ResolveContext{
				UserMessage:            userMessage,
				EffectiveContextTokens: contextTokens,
			}
			if h.contextResolver != nil {
				ctx.ContextFiles = h.getSteeringContextFiles(promptUserID)
			}
			return h.steeringStore.Resolve(ctx)
		}
	}

	// PostCorePrinciples: knowledge base rules are already injected via HasKnowledgeBase.
	// Inject context management + coding workflow contract + passthrough commands.
	deps.PostCorePrinciples = func(b *strings.Builder) {
		h.appendGUIPostCorePrinciples(b, isProMode, trialReflectEnabled)
	}

	// PostSSHRules: inject GUI-specific SSH guidance + skills + MCP + device status etc.
	deps.PostSSHRules = func(b *strings.Builder) {
		h.appendGUIPostSSHRules(b, isProMode, currentNickname, cfg)
	}

	// PostCodingWorkflow: inject GUI full coding workflow (pro mode 9-step).
	if isProMode {
		deps.PostCodingWorkflow = func(b *strings.Builder) {
			h.appendGUIPostCodingWorkflow(b, cfg)
		}
	}

	// Epilogue: memory section + knowledge auto-recall + knowledge skills + repairs + bundle + profile.
	deps.Epilogue = func(b *strings.Builder) {
		h.appendGUIEpilogue(b, includeMemoryGuide, msg, eventContext, promptUserID)
	}

	// User profile
	if model := h.getUserModel(); model != nil {
		deps.UserProfileSection = func() string { return model.FormatForPrompt() }
	}

	bundle := agent.BuildPromptBundle(deps, msg, includeMemoryGuide)
	if os.Getenv("MACLAW_DEBUG_PROMPT_BUNDLE") == "1" {
		stats := bundle.TokenStats()
		log.Printf("[prompt-bundle] surface=gui_im stable=%d session=%d retrieved=%d total=%d stable_key=%s",
			stats.StableSystemPromptTokens,
			stats.SessionContextTokens,
			stats.RetrievedContextTokens,
			stats.TotalTokens,
			bundle.StableCacheKey(),
		)
	}
	return bundle.String()
}

// desktopWorkflowDocOverride returns a system prompt section that overrides
// the PDF generation instructions for the desktop AI assistant panel.
// In the desktop panel, workflow documents (requirements, design, tasks) are
// displayed as Markdown in the right-side preview panel — no PDF needed.
// PDF generation is only needed for IM channels (飞书/微信/QQ/Telegram).
func desktopWorkflowDocOverride() string {
	return `

### ⚠️ 文档交付方式覆盖（桌面 AI 助手面板专用）
你当前运行在桌面 AI 助手面板中（非 IM 通道）。以下规则覆盖上述 PDF 生成相关的所有指令：

1. **不要使用 office(action="generate_pdf") 或 generate_pdf 工具**——桌面面板不需要 PDF，直接输出 Markdown 文本即可
2. **不要使用 send_file 发送文档**——文档内容直接作为你的回复文本输出
3. 需求文档、技术设计文档、任务列表文档：直接用 Markdown 格式写在回复中
4. 系统会自动将你输出的 Markdown 文档显示在聊天区右侧的预览面板中
5. 输出文档后，仍然需要附带确认提示（如"请查看并确认需求是否准确，或提出修改意见"）
6. 其他规则不变：仍需等待用户确认后才能进入下一阶段
`
}

// imWorkflowDocDeliveryRule returns the IM-channel-specific document delivery
// rule that forces all workflow phase outputs to be delivered as PDF.
// This is the symmetric counterpart of desktopWorkflowDocOverride — desktop
// overrides PDF→Markdown, IM enforces Markdown→PDF.
//
// Injected at the same two system prompt injection points as the desktop
// override, ensuring all 19 workflow templates get the rule without needing
// per-template PhasePrompt changes.
func imWorkflowDocDeliveryRule() string {
	return `

### 📄 IM 通道文档交付规则（所有工作流通用）
你当前运行在 IM 通道中（飞书/微信/QQ/Telegram）。所有工作流（编码、PPT 设计、产品设计、商业计划等）的每个阶段产出文档，必须遵守以下规则：

1. **必须**使用 generate_pdf 工具将本阶段产出物生成 PDF 后发送给用户
2. **严禁**在 IM 聊天窗口中直接输出大段文档文本——IM 中长文本阅读体验极差，用户无法有效审阅
3. 发送 PDF 后必须附带提示："📄 已生成 [阶段名称] 的 PDF 版本，请查看并确认，或提出修改意见。"
4. 短回复（确认提示、澄清问题、进度说明等）可以直接文本输出，不需要 PDF
5. 其他规则不变：仍需等待用户确认后才能进入下一阶段
`
}

func appendCodingWorkflowContract(b *strings.Builder) {
	b.WriteString(`
## 编程与非编程任务路由契约
- 编程任务 / Coding_Task：明确要求修改项目代码、修 bug、重构、实现功能、补测试或运行代码级验证时，才进入编程任务工作流。
- 非编程任务：信息检索、翻译、文档生成、文件操作、通信、日常助手、配置查看、截屏/screenshot、简单问答等，优先用现有工具直接完成，不要创建外部编程会话。
- SSH/服务器操作任务：登录服务器、查看远程日志、重启服务、上传下载服务器文件等，优先使用 SSH/服务器工具；如果不能确定是编程任务，不要创建外部编程会话。
- 文件与命令类非编程操作可用 bash、read_file、write_file、edit_file、craft_tool、send_file 等直接处理。

## Spec 驱动编程任务工作流
第一步：识别任务类型，区分编程任务、非编程任务、SSH/服务器操作任务。
第二步：检查跳过确认信号。用户说“直接做”“不用问了”“全力推进”“just do it”“go ahead”等时，跳过三个确认阶段和剩余确认阶段，但仍在内部完成规划并直接执行。
第三步：需求确认。生成需求文档并等待用户明确确认后才进入下一阶段；内容必须包括需求背景与目标、功能需求列表、非功能需求、约束与假设。若 PDF 生成失败，发送 Markdown 纯文本并说明 PDF 生成失败。
第四步：技术设计。基于确认的需求文档生成设计文档，内容必须包括架构设计、接口设计、数据模型变更、实现方案概述。
第五步：任务分解（任务拆分）。基于确认的需求和设计文档生成编号的任务列表、任务的描述和涉及的文件、TDD 验收测试用例。
第六步：任务执行。只有在确认任务列表或收到跳过确认信号后才进入内部 CodingSubAgent 执行；向 CodingSubAgent 传入需求和设计上下文，不要调用外部编程工具会话。

## 文档交付契约
- 需求文档、设计文档、任务列表优先生成 PDF，可使用 craft_tool、bash、pandoc、wkhtmltopdf 或等价工具。
- 通过 send_file 且 forward_to_im=true 发送给用户。
- PDF 文件名使用 requirements_<feature>.pdf、design_<feature>.pdf、task-plan_<feature>.pdf；文件名保持稳定 ASCII，展示标题可本地化。
- 发送 PDF 时必须附带行动提示或文字摘要，不能只发文件。
- 用户提出修改时，更新文档、重新生成 PDF，并把修订后使用最新版本作为后续阶段输入。
- 收到“回退到需求阶段”或“回到需求阶段”等请求时，告知用户回退信息，并重新生成所有后续阶段文档。

第七步：完成验收。所有任务结束后运行验证并形成验收报告，明确总任务数、成功/失败数、全量测试结果和剩余风险。
## 执行验证与止损契约
- 每个任务完成后运行对应 TDD 测试；失败时最多 3 次重试，仍失败则记录原因并跳到下一个任务。
- 进度格式要能区分完成 ✅ 和失败 ❌。
- 所有任务结束后运行全量回归测试，并报告总任务数、成功/失败数、每个任务的执行结果、全量测试运行结果；全部通过时说明全部通过，有失败时列出失败项。
- 会话失败止损原则：同一会话连续失败或无进展时不要无限重试，切换策略、拆小任务或向用户报告阻塞。
- 执行验证原则：没有验证结果不要声称完成；验证不可运行时说明原因和剩余风险。
- 绝对不要终止状态为 busy 的编程会话。
- 自动续接 / Auto-Resume：已有可恢复会话、run_id、resume session id 或执行上下文时，优先续接，不要重复创建会话。
`)
}

// buildSystemPromptWithMemory builds the system prompt with the lightweight
// memory section (user_fact summary + proactive recall + dynamic recall hint).
// The isFirstTurn flag controls whether the full memory management guide is included.
func (h *IMMessageHandler) buildSystemPromptWithMemory(userMessage string, isFirstTurn bool, loopCtx ...*LoopContext) string {
	start := time.Now()
	base := h.buildSystemPromptBaseWithExperienceContext(isFirstTurn, experienceContextFromLoop(loopCtx...), firstLoopContext(loopCtx...), userMessage)
	baseElapsed := time.Since(start)
	if !isFirstTurn {
		if baseElapsed > 200*time.Millisecond {
			log.Printf("[buildSystemPromptWithMemory] base_prompt=%v (not first turn)", baseElapsed)
		}
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteString(h.buildNicknameInstruction())
	totalElapsed := time.Since(start)
	if totalElapsed > 200*time.Millisecond {
		log.Printf("[buildSystemPromptWithMemory] base_prompt=%v total=%v (first turn)", baseElapsed, totalElapsed)
	}
	return b.String()
}

func firstLoopContext(loopCtx ...*LoopContext) *LoopContext {
	if len(loopCtx) == 0 {
		return nil
	}
	return loopCtx[0]
}

func (h *IMMessageHandler) promptRuntimeUserID(loopCtx *LoopContext) string {
	if loopCtx != nil {
		if strings.TrimSpace(loopCtx.Runtime.RequestID) != "" {
			return strings.TrimSpace(loopCtx.Runtime.PolicyOwnerID)
		}
		if ownerID := strings.TrimSpace(loopCtx.Runtime.PolicyOwnerID); ownerID != "" {
			return ownerID
		}
		if userID := strings.TrimSpace(loopCtx.UserID); userID != "" {
			return userID
		}
	}
	if h != nil {
		if ownerID, explicitRuntime := h.currentRuntimePolicyOwnerState(); explicitRuntime {
			return ownerID
		}
	}
	return desktopUserID
}

func experienceContextFromLoop(loopCtx ...*LoopContext) lifecycle.EventContext {
	if len(loopCtx) == 0 || loopCtx[0] == nil {
		return lifecycle.EventContext{}
	}
	return lifecycle.EventContext{TraceID: loopCtx[0].RunID, TaskID: loopCtx[0].ID}
}

// buildNicknameInstruction keeps the Hub nickname in sync without asking the
// LLM to invent one. Empty nicknames are intentionally left to Hub-side
// auto-assignment; set_nickname is only for explicit user rename requests.
func (h *IMMessageHandler) buildNicknameInstruction() string {
	currentNickname := ""
	if cfg, err := h.loadConfig(); err == nil {
		currentNickname = strings.TrimSpace(cfg.RemoteNickname)
	}
	if currentNickname != "" {
		// Nickname already configured — report it directly to Hub in the
		// background instead of asking the LLM to call set_nickname (saves
		// one full LLM round-trip on first message).
		go func() {
			if hc := h.getHubClient(); hc != nil {
				_ = hc.SendNicknameUpdate(currentNickname)
			}
		}()
		return "" // no instruction needed
	}
	return ""
}

// appendMemorySection appends a lightweight "## 用户记忆" section containing:
//   - A compressed one-line summary of user_fact entries (always present)
//   - Proactive recall of relevant memories based on userMessage (if non-empty)
//   - A hint that other memories can be recalled via memory(action: recall)
//   - Full memory management guide only on first turn (isFirstTurn=true)
//
// Frozen snapshot caching (Requirement 5.1, 5.2, 5.8):
// On the first message of a session (per userID), the full memory section is
// generated and cached as a frozen snapshot. Subsequent calls reuse the cached
// snapshot instead of regenerating, keeping the LLM's KV cache prefix stable.
// Mid-session memory writes update persistent storage but do NOT invalidate
// the cached snapshot (Requirement 5.3).
func (h *IMMessageHandler) appendMemorySection(b *strings.Builder, isFirstTurn bool, userID string, eventContext lifecycle.EventContext, userMessage ...string) {
	if h.memoryStore == nil {
		return
	}

	// Determine userID for per-user snapshot keying.
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = desktopUserID
	}

	// --- Static part: user_fact summary + memory guide (frozen per session) ---
	// Only regenerate on first turn or when snapshot is missing.
	staticCached := false
	if !isFirstTurn {
		if initialized, ok := h.snapshotInitialized.Load(userID); ok && initialized.(bool) {
			if snapshot, ok := h.frozenMemorySnapshots.Load(userID); ok {
				b.WriteString(snapshot.(string))
				staticCached = true
				log.Printf("[frozen_snapshot] reusing cached static memory snapshot for user %q", userID)
			}
		}
	}
	if !staticCached {
		var staticBuf strings.Builder
		h.generateStaticMemorySection(&staticBuf, isFirstTurn)
		snapshot := staticBuf.String()
		h.frozenMemorySnapshots.Store(userID, snapshot)
		h.snapshotInitialized.Store(userID, true)
		log.Printf("[frozen_snapshot] generated and cached static memory snapshot for user %q (%d bytes)", userID, len(snapshot))
		b.WriteString(snapshot)
	}

	// --- Dynamic part: proactive recall (executed per message, NOT frozen) ---
	msg := ""
	if len(userMessage) > 0 {
		msg = userMessage[0]
	}
	// Derive strict project mode from the synthesized userID pattern.
	// When a Project Tab sends a message, the userID is synthesized as
	// "desktop-user:{projectPath}" (see SendAIAssistantMessage). This
	// signals that proactive recall should use RecallDynamicStrict to
	// exclude other projects' entries.
	strictProject := isProjectTabUserID(userID)
	h.appendProactiveRecallForUser(b, msg, strictProject, userID, eventContext)
}

// generateStaticMemorySection builds the frozen part of the memory section:
// user_fact summary + memory recall hint + memory guide (first turn only).
// This content is stable across messages within a session and can be cached.
func (h *IMMessageHandler) generateStaticMemorySection(b *strings.Builder, isFirstTurn bool) {
	if h.memoryStore == nil {
		return
	}

	b.WriteString(h.memoryStore.StaticMemorySectionForPrompt(corememory.StaticUserMemoryPromptOptions("\n"+corememory.PromptSectionUserMemory, isFirstTurn, corememory.BuildIMMemoryGuidePrompt())))
}

// isProjectTabUserID returns true when the userID was synthesized for a
// Project Tab (format: "desktop-user:{projectPath}"). The local Tab uses
// the plain "desktop-user" constant without a colon suffix.
func isProjectTabUserID(userID string) bool {
	const prefix = desktopUserID + ":"
	return strings.HasPrefix(userID, prefix) && len(userID) > len(prefix)
}

// appendProactiveRecall performs per-message proactive recall and appends
// results to the system prompt. Unlike the static section, this is NOT frozen
// — each user message triggers a fresh recall so the LLM always sees memories
// relevant to the current query.
func (h *IMMessageHandler) appendProactiveRecall(b *strings.Builder, msg string, strictProject bool, eventContext ...lifecycle.EventContext) {
	h.appendProactiveRecallForUser(b, msg, strictProject, h.promptRuntimeUserID(nil), eventContext...)
}

func (h *IMMessageHandler) appendProactiveRecallForUser(b *strings.Builder, msg string, strictProject bool, userID string, eventContext ...lifecycle.EventContext) {
	if h.memoryStore == nil || msg == "" {
		return
	}
	recallStart := time.Now()

	projectPath := ""
	if strictProject {
		// In Project Tab mode, extract the project path directly from the
		// synthesized userID (format "desktop-user:{projectPath}"). This is
		// the authoritative source — contextResolver.ResolveProject() returns
		// the global current project which may differ from the Tab's project.
		projectPath = projectPathFromUserID(userID)
	}
	if projectPath == "" && h.contextResolver != nil {
		projectPath, _ = h.contextResolver.ResolveProject()
	}

	opts := corememory.IMProactivePromptOptions(projectPath, strictProject)
	opts.EventContext = firstLifecycleEventContext(eventContext)
	opts.Recall.Provider = h.proactiveExperienceProviderForUser(userID)
	promptContext, relevant, ok := h.proactiveContextForPromptWithBudget(msg, opts, userID, projectPath, strictProject)
	primaryRecallElapsed := time.Since(recallStart)
	if !ok {
		log.Printf("[proactive_recall] skipped slow recall user=%q userMsg=%d chars projectPath=%q strictProject=%v budget=%s elapsed=%v", userID, len(msg), projectPath, strictProject, imProactiveRecallBudget, primaryRecallElapsed)
		log.Printf("[perf] stage=proactive_recall user=%q elapsed=%s project=%q strict_project=%v recalled=%d status=%q budget=%s", userID, primaryRecallElapsed.Round(time.Millisecond), projectPath, strictProject, 0, "timeout", imProactiveRecallBudget)
		return
	}
	log.Printf("[proactive_recall] userMsg=%d chars, projectPath=%q, strictProject=%v, recalled=%d entries took=%v", len(msg), projectPath, strictProject, len(relevant), primaryRecallElapsed)
	b.WriteString(promptContext)
	if len(relevant) > 0 {
		log.Printf("[proactive_recall] injected %d entries (with index) into system prompt", len(relevant))
	}

	totalRecallElapsed := time.Since(recallStart)
	if totalRecallElapsed > 200*time.Millisecond {
		log.Printf("[proactive_recall] total_elapsed=%v (primary_recall=%v)", totalRecallElapsed, primaryRecallElapsed)
	}
	log.Printf("[perf] stage=proactive_recall user=%q elapsed=%s project=%q strict_project=%v recalled=%d prompt_context_len=%d", userID, totalRecallElapsed.Round(time.Millisecond), projectPath, strictProject, len(relevant), len(promptContext))
}

const imProactiveRecallBudget = 2 * time.Second

type imProactiveRecallResult struct {
	promptContext string
	relevant      []corememory.Entry
}

func (h *IMMessageHandler) proactiveContextForPromptWithBudget(msg string, opts corememory.ProactivePromptOptions, userID string, projectPath string, strictProject bool) (string, []corememory.Entry, bool) {
	if h == nil || h.memoryStore == nil {
		return "", nil, true
	}
	startedAt := time.Now()
	resultC := make(chan imProactiveRecallResult, 1)
	go func() {
		promptContext, relevant := h.memoryStore.ProactiveContextForPrompt(msg, opts)
		resultC <- imProactiveRecallResult{promptContext: promptContext, relevant: relevant}
	}()
	select {
	case result := <-resultC:
		return result.promptContext, result.relevant, true
	case <-time.After(imProactiveRecallBudget):
		go func() {
			result := <-resultC
			log.Printf("[proactive_recall] late result user=%q projectPath=%q strictProject=%v recalled=%d elapsed=%v", userID, projectPath, strictProject, len(result.relevant), time.Since(startedAt).Round(time.Millisecond))
		}()
		return "", nil, false
	}
}

func (h *IMMessageHandler) proactiveExperienceProvider() lifecycle.Provider {
	return h.proactiveExperienceProviderForUser(h.promptRuntimeUserID(nil))
}

func (h *IMMessageHandler) proactiveExperienceProviderForUser(userID string) lifecycle.Provider {
	if h == nil || h.memoryStore == nil {
		return nil
	}
	providers := []lifecycle.Provider{corememory.NewExperienceProvider(h.memoryStore)}
	if exec := h.getSkillExecutor(); exec != nil {
		exec.mu.RLock()
		skills := exec.loadSkills()
		exec.mu.RUnlock()
		if len(skills) > 0 {
			providers = append(providers, cskill.NewExperienceProvider(skills))
			providers = append(providers, cskill.NewGovernanceDraftProvider(skills, cskill.SkillMaintenancePlanOptions{MaxActions: 12}))
		}
	}
	if engine := h.getWorkflowEngine(); engine != nil {
		if ws := engine.GetActiveWorkflow(strings.TrimSpace(userID)); ws != nil {
			providers = append(providers, cworkflow.NewExperienceProvider(ws))
		}
	}
	return lifecycle.NewCompositeProvider(providers...)
}

func firstLifecycleEventContext(values []lifecycle.EventContext) lifecycle.EventContext {
	if len(values) == 0 {
		return lifecycle.EventContext{}
	}
	return values[0]
}

// RefreshMemorySnapshot regenerates the cached memory snapshot for the given
// user from current persistent storage. Called when the user issues /new,
// starts a new topic, or on application restart (first message of new session).
// (Requirement 5.4, 5.5, 5.7)
func (h *IMMessageHandler) RefreshMemorySnapshot(userID string) {
	h.frozenMemorySnapshots.Delete(userID)
	h.snapshotInitialized.Delete(userID)
	log.Printf("[frozen_snapshot] refreshed (invalidated) memory snapshot for user %q", userID)
}

// ---------------------------------------------------------------------------
// Knowledge Skill Injection (Requirements 1.5, 1.6, 1.7, 1.8, 8.1–8.5)
// ---------------------------------------------------------------------------

// defaultKnowledgeSkillTokenBudget is the combined token budget for all
// injected knowledge skills. Configurable via config.json field
// "knowledge_skill_token_budget". (Requirements 1.7, 8.5)
const defaultKnowledgeSkillTokenBudget = 2000

// matchedKnowledgeSkill holds a knowledge skill that matched the user message
// along with its relevance score (number of trigger matches).
type matchedKnowledgeSkill struct {
	Name        string
	Content     string
	ParamSchema string // formatted parameter schema for LLM context
	Score       int    // number of triggers that matched
}

// appendKnowledgeSkillSection injects matched skill documentation into the
// system prompt. This is the single mechanism that bridges the gap between
// a skill's semantic layer (SKILL.md) and the LLM's decision-making context.
//
// Two categories of skills are injected:
//
//  1. Knowledge skills (type: "knowledge"): inline Content from skill.yaml.
//     These are pure documentation skills with no executable steps.
//
//  2. Executable skills with SKILL.md: documentation loaded from the skill
//     directory. These skills have steps that the Runner executes, but the
//     SKILL.md describes the full workflow including prerequisites that the
//     LLM must handle (e.g. "generate drawio XML before running run.js").
//     Without this injection, the LLM only sees a one-line description in
//     the skill listing and has no way to know the skill's prerequisites.
//
// Both categories use the same trigger-matching and token-budget mechanism.
// The section is placed after the memory section and before tool definitions.
// When no skills match the current user message, the section is omitted.
func (h *IMMessageHandler) appendKnowledgeSkillSection(b *strings.Builder, userMessage string) {
	if h.app == nil || h.getSkillExecutor() == nil || userMessage == "" {
		return
	}

	skills := h.getSkillExecutor().List()
	if len(skills) == 0 {
		return
	}

	msgLower := strings.ToLower(userMessage)

	var matched []matchedKnowledgeSkill
	for _, s := range skills {
		if normalizeSkillEntryStatus(s.Status) != skillEntryStatusActive {
			continue
		}
		if isShellBrowserAutomationSkill(s) {
			continue
		}

		// Determine the content to inject.
		var content string
		skillKind := normalizeSkillTypeKind(s.Type)
		switch {
		case skillKind.IsKnowledge() && s.Content != "":
			// Category 1: knowledge skill with inline content.
			content = s.Content
		case !skillKind.IsKnowledge() && s.SkillDir != "":
			// Category 2: executable skill with SKILL.md.
			// Load documentation directly from the skill directory.
			// Only loaded when triggers match (lazy), so no wasted IO.
			content = loadSkillDocContent(s.SkillDir)
		}
		if content == "" {
			continue
		}

		// Match triggers against user message.
		triggers := s.Triggers
		if len(triggers) == 0 {
			continue
		}
		score := countTriggerMatches(triggers, msgLower)
		// Also match by skill name (covers "用 drawio-skill 画..." pattern).
		if score == 0 && strings.Contains(msgLower, strings.ToLower(s.Name)) {
			score = 1
		}
		if score == 0 {
			continue
		}

		matched = append(matched, matchedKnowledgeSkill{
			Name:        s.Name,
			Content:     content,
			ParamSchema: buildParamSchemaForSkill(s),
			Score:       score,
		})
	}

	if len(matched) == 0 {
		return
	}

	// Sort by relevance: higher score first, then alphabetically by name for stability.
	sortMatchedKnowledgeSkills(matched)

	// Determine token budget from config or use default.
	tokenBudget := defaultKnowledgeSkillTokenBudget
	if cfg, err := h.loadConfig(); err == nil && cfg.KnowledgeSkillTokenBudget > 0 {
		tokenBudget = cfg.KnowledgeSkillTokenBudget
	}

	totalTokensUsed := 0

	b.WriteString("\n## Skill 使用文档\n")
	for _, m := range matched {
		// If the total budget is exhausted, skip remaining skills.
		if totalTokensUsed >= tokenBudget {
			log.Printf("[skill_doc_inject] token budget exhausted (%d/%d), skipping skill %q", totalTokensUsed, tokenBudget, m.Name)
			break
		}

		content := m.Content
		contentTokens := estimateTokens(content)
		remaining := tokenBudget - totalTokensUsed

		if contentTokens > remaining {
			// Truncate content to fit within remaining budget.
			content = truncateToTokenBudget(content, remaining)
			contentTokens = estimateTokens(content)
			log.Printf("[skill_doc_inject] truncated skill %q to fit budget (remaining=%d tokens)", m.Name, remaining)
		}

		totalTokensUsed += contentTokens

		b.WriteString(fmt.Sprintf("\n### Skill: %s\n", m.Name))

		// Inject parameter schema if available (explicit or synthesized).
		if m.ParamSchema != "" {
			b.WriteString(m.ParamSchema)
		}

		b.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n---\n")
	}
}

// appendSkillRepairNotifications injects notifications about recently
// auto-repaired skills into the system prompt. This closes the signal gap
// between background self-repair and LLM awareness: when a skill's steps
// are modified by the repair system, the LLM needs to know so it can
// adjust its calling strategy.
//
// Notifications are consumed (one-shot) — each repair is injected exactly
// once into the next LLM turn, then cleared.
func (h *IMMessageHandler) appendSkillRepairNotifications(b *strings.Builder) {
	runner := h.getSkillRunner()
	if runner == nil {
		return
	}

	repairs := runner.ConsumeRepairNotifications()
	if len(repairs) == 0 {
		return
	}

	b.WriteString("\n\n## 最近自动修复的 Skill\n")
	for name, explanation := range repairs {
		b.WriteString(fmt.Sprintf("- Skill「%s」已自动修复：%s。下次调用将使用修复后的版本。\n", name, explanation))
	}
}

// appendBundleContextBanner injects a bundle context banner into the system
// prompt when a namespaced skill (one with a Publisher) is currently running.
// The banner lists sibling skills from the same publisher to provide context.
// (Requirement 5.5)
func (h *IMMessageHandler) appendBundleContextBanner(b *strings.Builder) {
	if h.app == nil || h.getSkillRunner() == nil || h.getSkillExecutor() == nil {
		return
	}

	// Find the most recent active skill run that has a publisher.
	runs := h.getSkillRunner().ListRuns()
	var activePublisher string
	var activeSkillName string
	for _, run := range runs {
		if !run.IsRunning() {
			continue
		}
		// Look up the skill to check its publisher.
		h.getSkillExecutor().mu.RLock()
		for _, s := range h.getSkillExecutor().loadSkills() {
			if s.MatchesName(run.Skill) && s.Publisher != "" && !isShellBrowserAutomationSkillEntry(s) {
				activePublisher = s.Publisher
				activeSkillName = s.Name
				break
			}
		}
		h.getSkillExecutor().mu.RUnlock()
		if activePublisher != "" {
			break
		}
	}

	if activePublisher == "" {
		return
	}

	// Find sibling skills from the same publisher.
	h.getSkillExecutor().mu.RLock()
	var siblings []string
	for _, s := range h.getSkillExecutor().loadSkills() {
		if s.Publisher == activePublisher && s.Name != activeSkillName && normalizeSkillEntryStatus(s.Status) == skillEntryStatusActive && !isShellBrowserAutomationSkillEntry(s) {
			siblings = append(siblings, s.Name)
		}
	}
	h.getSkillExecutor().mu.RUnlock()

	// Build the banner.
	b.WriteString(fmt.Sprintf("\n## Bundle Context\nThis skill is part of the '%s' bundle.", activePublisher))
	if len(siblings) > 0 {
		b.WriteString(fmt.Sprintf(" Related skills: %s", strings.Join(siblings, ", ")))
	}
	b.WriteString("\n")
}

// estimateTokens delegates to corelib.EstimateTextTokens.
func estimateTokens(s string) int {
	return corelib.EstimateTextTokens(s)
}

// truncateToTokenBudget truncates content to fit within the given token budget,
// cutting at a smart boundary (paragraph break "\n\n", or sentence-ending
// punctuation followed by whitespace/newline). Appends "[truncated]" notice.
// Uses rune-safe operations to avoid splitting multi-byte UTF-8 characters.
// Uses conservative rune budget (1.5 runes/token) to ensure the truncated
// result never exceeds the token budget under CJK-aware estimation.
// (Requirement 1.8)
func truncateToTokenBudget(content string, tokenBudget int) string {
	// Convert to runes for safe truncation of multi-byte characters.
	runes := []rune(content)
	// Conservative: assume worst case (all CJK, ~1.5 chars/token).
	maxRunes := tokenBudget * 3 / 2
	if maxRunes <= 0 {
		return "[truncated]"
	}
	if len(runes) <= maxRunes {
		return content
	}

	// Reserve space for the truncation notice.
	const truncNotice = "\n[truncated]"
	truncNoticeRunes := len([]rune(truncNotice))
	cutoff := maxRunes - truncNoticeRunes
	if cutoff <= 0 {
		return truncNotice
	}
	if cutoff > len(runes) {
		return content
	}

	snippet := string(runes[:cutoff])

	// Try to find a smart boundary working backwards from the cutoff point.
	halfLen := len(snippet) / 2

	// Priority 1: paragraph break ("\n\n")
	if idx := strings.LastIndex(snippet, "\n\n"); idx > halfLen {
		return snippet[:idx] + truncNotice
	}

	// Priority 2: sentence-ending punctuation (., 。, !, ?, ！, ？) followed
	// by whitespace or newline, or at end of snippet.
	bestSentEnd := -1
	for i := len(snippet) - 1; i > halfLen; i-- {
		ch := snippet[i]
		if ch == '.' || ch == '!' || ch == '?' {
			// Check that the next char (if any) is whitespace/newline or end of snippet.
			if i+1 >= len(snippet) || snippet[i+1] == ' ' || snippet[i+1] == '\n' || snippet[i+1] == '\r' || snippet[i+1] == '\t' {
				bestSentEnd = i + 1
				break
			}
		}
		// Handle multi-byte sentence-ending punctuation (。！？).
		// These are 3-byte UTF-8 sequences.
		if i >= 2 {
			triple := snippet[i-2 : i+1]
			if triple == "。" || triple == "！" || triple == "？" {
				bestSentEnd = i + 1
				break
			}
		}
	}
	if bestSentEnd > 0 {
		return snippet[:bestSentEnd] + truncNotice
	}

	// Priority 3: newline break
	if idx := strings.LastIndex(snippet, "\n"); idx > halfLen {
		return snippet[:idx] + truncNotice
	}

	// Fallback: hard cut (already rune-safe from the runes[:cutoff] above).
	return snippet + truncNotice
}

// countTriggerMatches counts how many of the skill's triggers match the user
// message via case-insensitive substring matching. Returns 0 if none match.
func countTriggerMatches(triggers []string, msgLower string) int {
	count := 0
	for _, t := range triggers {
		if t == "" {
			continue
		}
		if strings.Contains(msgLower, strings.ToLower(t)) {
			count++
		}
	}
	return count
}

// sortMatchedKnowledgeSkills sorts matched skills by descending relevance
// score, with alphabetical name as tiebreaker for deterministic ordering.
func sortMatchedKnowledgeSkills(matched []matchedKnowledgeSkill) {
	for i := 1; i < len(matched); i++ {
		for j := i; j > 0; j-- {
			if matched[j].Score > matched[j-1].Score ||
				(matched[j].Score == matched[j-1].Score && matched[j].Name < matched[j-1].Name) {
				matched[j], matched[j-1] = matched[j-1], matched[j]
			} else {
				break
			}
		}
	}
}

// buildParamSchemaForSkill returns a formatted parameter schema string for
// a skill, preserving explicit params and completing any template placeholders
// that the author omitted from the declared schema.
func buildParamSchemaForSkill(s NLSkillDefinition) string {
	params := cskill.CompleteParamsForRunner(s.Params, s.Steps, s.RequiredArgs)
	if len(params) == 0 {
		return ""
	}
	return cskill.FormatParamSchema(params)
}

// ---------------------------------------------------------------------------
// Tool Definitions
// ---------------------------------------------------------------------------
