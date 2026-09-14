package guiapp

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/steering"
)

func (h *IMMessageHandler) buildSystemPrompt() string {
	return h.buildSystemPromptBase(false)
}

func (h *IMMessageHandler) buildIMEntrySystemPrompt(msg IMUserMessage, history []agent.ConversationEntry, loopCtx *LoopContext, workflowAgentLoop bool, phasePromptDirect string, askUserContext, pendingUserReplyContext, capabilityGapContext string) string {
	promptBuildStart := time.Now()
	// File descriptions (and any host-extracted document text) are attached to
	// the user content later in buildAgentLoopConversationStart. Keep the
	// control-plane prompt focused on the user's request, otherwise attachment
	// marker text can re-open Computer Use even after the final tool filter
	// removed it.
	routingText := computerUseRoutingText(msg.Text, msg.Attachments)
	if loopCtx != nil && strings.TrimSpace(loopCtx.ComputerUseRoutingText) == "" {
		loopCtx.ComputerUseRoutingText = routingText
	}
	if loopCtx != nil {
		syncComputerUseTurn(h, loopCtx, msg.UserID, msg.Text)
	}
	intentText := semanticUserIntentText(msg.Text)
	promptMessage := agent.CompactQueryForEmbedding(intentText)
	if strings.TrimSpace(promptMessage) == "" {
		promptMessage = intentText
	}
	profile := ExecutionProfile{}
	if loopCtx != nil {
		profile = loopCtx.Runtime.Execution
	}
	if workflowAgentLoop && profile.IsLight() {
		profile = fullExecutionProfile("workflow agent loop prompt override")
	}

	var systemPrompt string
	if profile.IsLight() {
		promptMsg := msg
		promptMsg.Text = promptMessage
		systemPrompt = buildLightIMSystemPrompt(promptMsg, profile)
	} else if h.memoryStore != nil {
		systemPrompt = h.buildSystemPromptWithMemory(promptMessage, len(history) == 0, loopCtx)
	} else {
		systemPrompt = h.buildSystemPromptBaseWithExperienceContext(false, lifecycle.EventContext{}, loopCtx, promptMessage)
	}
	basePromptElapsed := time.Since(promptBuildStart)

	resumeStart := time.Now()
	if !profile.IsLight() {
		systemPrompt += h.buildResumeTraceContextWithLang(msg.UserID, promptMessage, msg.Lang)
	}
	resumeElapsed := time.Since(resumeStart)

	policyOwnerID := h.workflowPolicyOwnerID(msg.UserID, loopCtx)
	if workflowAgentLoop {
		// Prefer directly-passed phase prompt (synchronous, no race window).
		// Fall back to sync.Map lookup for backward compat (other callers).
		if phasePromptDirect != "" {
			systemPrompt += "\n" + phasePromptDirect
			// Clean up the sync.Map entry since we already have the prompt.
			h.stashedPhasePrompt.Delete(policyOwnerID)
			if policyOwnerID != msg.UserID {
				h.stashedPhasePrompt.Delete(msg.UserID)
			}
		} else if stashed, ok := h.stashedPhasePrompt.LoadAndDelete(policyOwnerID); ok {
			systemPrompt += "\n" + stashed.(string)
		} else {
			// Diagnostic: phase prompt was expected but not found.
			log.Printf("[buildIMEntrySystemPrompt] WARNING: no phase prompt available (direct=%d, stashed key=%q, msg.UserID=%q)", len(phasePromptDirect), policyOwnerID, msg.UserID)
			if policyOwnerID != msg.UserID {
				if stashed2, ok2 := h.stashedPhasePrompt.LoadAndDelete(msg.UserID); ok2 {
					log.Printf("[buildIMEntrySystemPrompt] RECOVERED: found stashed prompt under msg.UserID=%q", msg.UserID)
					systemPrompt += "\n" + stashed2.(string)
				}
			}
		}
	} else {
		h.stashedPhasePrompt.Delete(msg.UserID)
		if policyOwnerID != msg.UserID {
			h.stashedPhasePrompt.Delete(policyOwnerID)
		}
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
	if clientContext := buildClientCapabilityPrompt(msg.ClientCapabilities); clientContext != "" {
		systemPrompt += "\n\n" + clientContext
	}
	if clientToolContext := buildClientToolPrompt(msg.ClientTools, loopCtx); clientToolContext != "" {
		systemPrompt += "\n\n" + clientToolContext
	}
	if bindingPrompt := buildAssistantBindingPrompt(msg.AssistantBinding); bindingPrompt != "" {
		systemPrompt += "\n\n" + bindingPrompt
	}

	// During V2 workflow agent loops, skip the desktop/IM workflow doc delivery
	// overrides. The phase prompt already contains precise output instructions
	// (e.g. "只生成一份文档，输出完毕后立即停止，严禁输出确认提示语").
	// The desktopWorkflowDocOverride says "输出文档后，仍然需要附带确认提示" which
	// directly contradicts the phase prompt and can cause the LLM to self-confirm.
	isV2WorkflowLoop := workflowAgentLoop && h.isWorkflowV2Active(policyOwnerID)
	if !profile.IsLight() && !isV2WorkflowLoop && !loopContextIsSemanticManaged(loopCtx) {
		platformKind := normalizeIMMessagePlatformKind(msg.Platform)
		if platformKind.IsDesktop() {
			systemPrompt += desktopWorkflowDocOverride()
		} else if platformKind.IsKnown() || msg.Platform != "" {
			systemPrompt += imWorkflowDocDeliveryRule()
		}
	}

	totalPromptBuild := time.Since(promptBuildStart)
	if totalPromptBuild > 500*time.Millisecond {
		log.Printf("[buildIMEntrySystemPrompt] slow: base_prompt=%v resume_trace=%v total=%v prompt_len=%d user=%s",
			basePromptElapsed, resumeElapsed, totalPromptBuild, len(systemPrompt), msg.UserID)
	}
	imPerfLog("system_prompt", promptBuildStart, imRequestID(msg), msg.UserID, "base_prompt", basePromptElapsed, "resume_trace", resumeElapsed, "prompt_len", len(systemPrompt), "history_len", len(history), "workflow", workflowAgentLoop, "prompt_profile", profile.PromptProfile)
	return systemPrompt
}

func buildAssistantBindingPrompt(binding *agent.AssistantBinding) string {
	return agentruntime.BuildAssistantBindingPrompt(binding)
}

func buildLightIMSystemPrompt(msg IMUserMessage, profile ExecutionProfile) string {
	// Shared light PromptBundle (identity + short principles + project paths)
	// plus a hard capability fence for the GUI light execution layer.
	roleName := "MaClaw"
	roleDesc := "a careful personal assistant for low-complexity lookup tasks"
	// Expert session: same persona swap as the full prompt path.
	if expertDef := expertDefForUserID(msg.UserID); expertDef != nil {
		if name := strings.TrimSpace(expertDef.Name); name != "" {
			roleName = name
		}
		if sp := strings.TrimSpace(expertDef.SystemPrompt); sp != "" {
			roleDesc = sp
		}
		roleDesc += fmt.Sprintf("\n你是%s，请始终以该专家身份回应，不越界处理无关事务。", roleName)
	}
	return agentruntime.EnsureLightSemanticGrantPromptFence(agentruntime.BuildLightPrompt(agentruntime.LightPromptRequest{
		RoleName:            roleName,
		RoleDescription:     roleDesc,
		UserText:            msg.Text,
		ExecutionLayer:      profile.Layer,
		ExecutionTask:       profile.TaskType,
		ExecutionConfidence: profile.Confidence,
		ExecutionReason:     profile.Reason,
		ClientCapabilities:  msg.ClientCapabilities,
		ClientTools:         msg.ClientTools,
		AssistantBinding:    msg.AssistantBinding,
	}))
}

func buildClientCapabilityPrompt(capabilities *agent.ClientCapabilities) string {
	return agent.BuildClientCapabilityPrompt(capabilities)
}

// buildClientToolPrompt makes device-local actions explicit to the model. Tool
// schemas alone are not enough: otherwise a model can answer "already set"
// without making a call, or prefer a host scheduler when both are available.
func buildClientToolPrompt(clientTools []agent.ClientToolDefinition, loopCtx *LoopContext) string {
	names := make([]string, 0, len(clientTools))
	for _, definition := range clientTools {
		names = append(names, definition.Name)
	}
	clientID := ""
	if loopCtx != nil && loopCtx.ClientToolContext != nil && strings.TrimSpace(loopCtx.ClientToolContext.ClientID) != "" {
		clientID = loopCtx.ClientToolContext.ClientID
	}
	return agentruntime.BuildClientToolPrompt(agentruntime.ClientToolPromptRequest{ToolNames: names, ClientID: clientID})
}

func (h *IMMessageHandler) buildSystemPromptBase(includeMemoryGuide bool, userMessage ...string) string {
	return h.buildSystemPromptBaseWithExperienceContext(includeMemoryGuide, lifecycle.EventContext{}, nil, userMessage...)
}

func (h *IMMessageHandler) buildSystemPromptBaseWithExperienceContext(includeMemoryGuide bool, eventContext lifecycle.EventContext, loopCtx *LoopContext, userMessage ...string) string {
	// Load config once for all decisions.
	roleName := "MaClaw"
	roleDesc := corelib.DefaultMaclawRoleDescription
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

	// Expert session: replace the global role persona with the expert's own
	// name + system prompt (capability sections are appended unchanged below).
	if expertDef := expertDefForUserID(promptUserID); expertDef != nil {
		if name := strings.TrimSpace(expertDef.Name); name != "" {
			roleName = name
		}
		if sp := strings.TrimSpace(expertDef.SystemPrompt); sp != "" {
			roleDesc = sp
		}
		roleDesc += fmt.Sprintf("\n你是%s，请始终以该专家身份回应，不越界处理无关事务。", roleName)
	}

	// Build deps for the shared BuildSystemPrompt.
	// During V2 workflow agent loops, suppress the coding confirmation gate rules
	// from the stable prompt segment — they conflict with phase-specific instructions.
	suppressV2CodingRules := loopCtx != nil && loopCtx.WorkflowAgentLoop && h.isWorkflowV2Active(promptUserID)
	promptProfile := agent.PromptProfileFull
	promptABSample := false
	promptSoftFull := false
	// Light prompt skips the GUI epilogue (user memory + proactive recall). Entry
	// already routes true light turns through buildLightIMSystemPrompt; do not
	// re-apply adaptive light here when a memory store is present, or first-turn
	// / proactive recall sections disappear.
	if loopCtx != nil && loopCtx.Runtime.Execution.IsLight() && h.memoryStore == nil {
		promptProfile = agent.PromptProfileLight
	} else if h.memoryStore == nil && strings.TrimSpace(msg) != "" && !suppressV2CodingRules {
		// Adaptive: short/simple turns use light prompt when memory is not wired.
		var classified llm.ClassifyResult
		promptProfile, classified = agent.ResolvePromptProfile(msg, llm.ClassifyHints{
			ToolHeavy: loopCtx != nil && loopCtx.WorkflowAgentLoop,
		})
		promptABSample = agent.IsQualityABReason(classified.Reason)
		promptSoftFull = agent.IsSoftFullUpgradeReason(classified.Reason)
	}
	// Env override always wins for operator debugging (light|full), except
	// when the semantic plan already declared a mutating or external effect.
	if p, ok := agent.EnvPromptProfileOverride(); ok {
		promptProfile = p
		promptABSample = false
		promptSoftFull = false
	}
	if loopCtx != nil && loopCtx.Runtime.SemanticIntent != nil && semanticIntentRequiresFullProfile(*loopCtx.Runtime.SemanticIntent) {
		promptProfile = agent.PromptProfileFull
		promptABSample = false
		promptSoftFull = false
	}
	// Keep Runtime.PromptProfile in sync so tool filtering + Turn meta observe it.
	if loopCtx != nil {
		if promptProfile.IsLight() {
			loopCtx.Runtime.Execution.PromptProfile = string(agent.PromptProfileLight)
		} else {
			loopCtx.Runtime.Execution.PromptProfile = string(agent.PromptProfileFull)
		}
		loopCtx.Runtime.PromptABSample = promptABSample
		loopCtx.Runtime.PromptSoftFull = promptSoftFull
	}
	deps := agent.SystemPromptDeps{
		Config: agent.SystemPromptConfig{
			RoleName:                roleName,
			RoleDescription:         roleDesc,
			IsProMode:               isProMode,
			Nickname:                currentNickname,
			HasCodingSessions:       true,
			TrialReflect:            trialReflectEnabled,
			SuppressCodingGateRules: suppressV2CodingRules,
			PromptProfile:           promptProfile,
			ManagedSemantic:         loopContextIsSemanticManaged(loopCtx),
		},
		MemoryStore:      h.memoryStore,
		SkipMemoryRecall: true, // GUI writes catalog + pull hint in appendGUIEpilogue; warehouse bodies stay in tools.
		HasKnowledgeBase: true,
		// EffectiveProjectDir: uses the SAME resolution function as tool execution,
		// ensuring the LLM's understanding of "project directory" matches the actual
		// cwd used by bash/write_file/read_file at runtime.
		EffectiveProjectDir: func() string {
			return h.resolveToolWorkDirForOwner("", promptUserID)
		},
		// Scratch must stay under the session workbench so agents do not park
		// downloads / intermediate files in the system TEMP (e.g. maclaw-arxiv).
		ScratchDir: func() string {
			wd := h.resolveToolWorkDirForOwner("", promptUserID)
			if strings.TrimSpace(wd) == "" {
				wd = corelib.EffectiveWorkspaceDir()
			}
			tmp := filepath.Join(wd, ".maclaw-tmp")
			_ = os.MkdirAll(tmp, 0o755)
			return tmp
		},
	}

	// SSH hosts
	if loadedCfg, err := h.loadConfig(); err == nil && len(loadedCfg.SSHHosts) > 0 {
		deps.SSHHostLister = func() []corelib.SSHHostEntry { return loadedCfg.SSHHosts }
	}

	// Steering
	if h.steeringStore != nil {
		deps.SteeringResolver = func(userMessage string, contextTokens int) []steering.File {
			// V2 workflow agent loop: skip ALL steering files.
			// The phase prompt contains complete, self-sufficient instructions.
			// Steering files (coding-workflow rules, maclaw-improvements record, etc.)
			// add conflicting instructions or waste token budget without benefit.
			if loopCtx != nil && loopCtx.WorkflowAgentLoop && h.isWorkflowV2Active(promptUserID) {
				return nil
			}
			ctx := steering.ResolveContext{
				UserMessage:            userMessage,
				EffectiveContextTokens: contextTokens,
			}
			if h.contextResolver != nil {
				ctx.ContextFiles = h.getSteeringContextFiles(promptUserID)
			}
			files := h.steeringStore.Resolve(ctx)
			return files
		}
	}

	// PostCorePrinciples: knowledge base rules are already injected via HasKnowledgeBase.
	// Inject context management + coding workflow contract + passthrough commands.
	// During V2 workflow agent loops, the coding workflow contract is suppressed
	// because it describes the full multi-phase pipeline and causes the LLM to
	// self-confirm and re-emit documents within a single response.
	deps.PostCorePrinciples = func(b *strings.Builder) {
		platform := ""
		if loopCtx != nil {
			platform = runtimePlatformFromLoopContext(loopCtx)
		}
		h.appendGUIPostCorePrinciples(b, isProMode, trialReflectEnabled, suppressV2CodingRules, platform, loopContextIsSemanticManaged(loopCtx))
	}

	// PostSSHRules: inject GUI-specific SSH guidance + skills + MCP + device status etc.
	deps.PostSSHRules = func(b *strings.Builder) {
		h.appendGUIPostSSHRules(b, isProMode, currentNickname, cfg, promptUserID, loopContextIsSemanticManaged(loopCtx))
	}

	// PostCodingWorkflow: inject GUI full coding workflow (pro mode 9-step).
	if isProMode && !loopContextIsSemanticManaged(loopCtx) {
		deps.PostCodingWorkflow = func(b *strings.Builder) {
			h.appendGUIPostCodingWorkflow(b, cfg)
		}
	}

	// Epilogue: memory section + knowledge auto-recall + knowledge skills + repairs + bundle + profile.
	// During V2 workflow agent loops, suppress proactive memory recall — the phase
	// prompt already contains all required context (FormData, previous outputs,
	// phase instructions). Proactive recall of old project memories actively hurts
	// by distracting the LLM from the current task's structured input.
	//
	// We only suppress the DYNAMIC proactive recall, not the full epilogue.
	// The static memory section (user_fact summary) and knowledge/skill sections
	// are still useful for context (e.g. user preferences, device status).
	isV2AgentLoop := loopCtx != nil && loopCtx.WorkflowAgentLoop && h.isWorkflowV2Active(promptUserID)
	deps.Epilogue = func(b *strings.Builder) {
		if isV2AgentLoop {
			// V2 workflow: only inject static user identity section, skip
			// proactive recall (which injects old project memories that distract
			// the LLM from the current phase's FormData/instructions).
			log.Printf("[prompt-bundle] V2 workflow agent loop: suppressing proactive recall for user=%q", promptUserID)
			// Still inject static memory (user_fact) so LLM knows user preferences.
			if h.memoryStore != nil && promptUserID != "" {
				h.appendStaticMemoryOnly(b, promptUserID)
			}
			return
		}
		var history []agent.ConversationEntry
		if loopCtx != nil {
			history = loopCtx.History
		}
		h.appendGUIEpilogue(b, includeMemoryGuide, msg, eventContext, promptUserID, history, loopCtx)
	}

	// User profile
	if model := h.getUserModel(); model != nil {
		deps.UserProfileSection = func() string { return model.FormatForPrompt() }
	}

	bundle := agent.BuildPromptBundle(deps, msg, includeMemoryGuide)
	// Shadow estimate: when light is chosen, measure full-vs-light token delta
	// without a second LLM call (CPU-only dual BuildPromptBundle).
	if promptProfile.IsLight() && loopCtx != nil {
		fullTok, lightTok := agent.EstimatePromptProfileTokens(deps, msg, includeMemoryGuide)
		loopCtx.Runtime.PromptFullTokens = fullTok
		loopCtx.Runtime.PromptLightTokens = lightTok
		if os.Getenv("MACLAW_DEBUG_PROMPT_BUNDLE") == "1" {
			log.Printf("[prompt-bundle] adaptive light savings full=%d light=%d saved=%d",
				fullTok, lightTok, fullTok-lightTok)
		}
	}
	if os.Getenv("MACLAW_DEBUG_PROMPT_BUNDLE") == "1" {
		stats := bundle.TokenStats()
		log.Printf("[prompt-bundle] surface=gui_im stable=%d session=%d retrieved=%d total=%d stable_key=%s profile=%s",
			stats.StableSystemPromptTokens,
			stats.SessionContextTokens,
			stats.RetrievedContextTokens,
			stats.TotalTokens,
			bundle.StableCacheKey(),
			promptProfile,
		)
	}
	return bundle.String()
}

// desktopWorkflowDocOverride returns a system prompt section that overrides
// the PDF generation instructions for the desktop AI assistant panel.
// In the desktop panel, **workflow phase documents** (requirements, design, tasks)
// are displayed as Markdown in the right-side preview panel — no PDF needed.
// PDF generation remains required for IM channels (飞书/微信/QQ/Telegram).
//
// Scope is intentionally limited to coding/product workflow phase docs.
// Meeting-recording products (minutes, transcript archives, MP3) still use
// write_file / generate_pdf / send_file as directed by post-recording context.
func desktopWorkflowDocOverride() string {
	return agentruntime.DesktopWorkflowDocumentDeliveryPrompt()
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
	return agentruntime.IMWorkflowDocumentDeliveryPrompt()
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
- 通过 send_to_im（推荐）或 send_file+destination/forward_to_im 发送给用户。
- PDF 文件名使用 requirements_<feature>.pdf、design_<feature>.pdf、task-plan_<feature>.pdf；文件名保持稳定 ASCII，展示标题可本地化。
- 发送 PDF 时必须附带行动提示或文字摘要，不能只发文件。
- 用户提出修改时，更新文档、重新生成 PDF，并把修订后使用最新版本作为后续阶段输入。
- 收到“回退到需求阶段”或“回到需求阶段”等请求时，告知用户回退信息，并重新生成所有后续阶段文档。

第七步：完成验收。所有任务结束后运行验证并形成验收报告，明确总任务数、成功/失败数、全量测试结果和剩余风险。
## 执行验证与止损契约
- 每个任务完成后运行对应 TDD 测试；失败时最多 3 次重试，仍失败则记录原因并跳到下一个任务。
- 进度格式要能区分完成 和失败 。
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
//   - A catalog/index of the store (counts and tags, not claims)
//   - A hint that warehouse content must be pulled via retrieval tools
//   - Full memory management guide in the session-stable snapshot
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
	// Reuse whenever a snapshot exists — including the first turn — so prewarm
	// hits the critical path. Content is session-stable for KV prefix stability.
	if text, built := h.loadOrBuildStaticMemorySnapshot(userID); text != "" {
		b.WriteString(text)
		if built {
			log.Printf("[frozen_snapshot] generated and cached static memory snapshot for user %q (%d bytes)", userID, len(text))
		}
	}
	_ = isFirstTurn // guide is always included in the session-stable snapshot

	// --- Dynamic part: catalog + pull hint (per message, not frozen) ---
	msg := ""
	if len(userMessage) > 0 {
		msg = userMessage[0]
	}
	// Project Tab owner IDs ("desktop-user:{projectPath}") scope the
	// catalog index to that project. Warehouse bodies stay in tools.
	strictProject := isProjectTabUserID(userID)
	h.appendProactiveRecallForUser(b, msg, strictProject, userID, eventContext)
}

// appendStaticMemoryOnly injects only the static (frozen) user_fact summary
// into the prompt, WITHOUT dynamic proactive recall. Used during V2 workflow
// agent loops where the phase prompt is self-sufficient and proactive recall
// of old project memories would distract the LLM.
func (h *IMMessageHandler) appendStaticMemoryOnly(b *strings.Builder, userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = desktopUserID
	}
	if h.memoryStore != nil {
		if text, _ := h.loadOrBuildStaticMemorySnapshot(userID); text != "" {
			b.WriteString(text)
		}
	}
	h.appendSessionFactsSection(b, userID, nil)
	h.appendMemoryRetractionSection(b)
}

// loadOrBuildStaticMemorySnapshot returns the session-stable static memory
// section for userID, generating it once (with memory guide) if missing.
// built is true when this call performed generation (not a cache hit).
//
// Concurrent callers (startup prewarm × N + first chat message) are coalesced
// with a singleflight channel stored in snapshotWarmInflight: one builder runs
// generateStaticMemorySection, waiters block on close then read the cache.
func (h *IMMessageHandler) loadOrBuildStaticMemorySnapshot(userID string) (text string, built bool) {
	if h == nil || h.memoryStore == nil {
		return "", false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = desktopUserID
	}
	if cached := h.cachedStaticMemorySnapshot(userID); cached != "" {
		return cached, false
	}

	// Singleflight: first caller stores a done channel; others wait on it.
	doneCh := make(chan struct{})
	if existing, loaded := h.snapshotWarmInflight.LoadOrStore(userID, doneCh); loaded {
		waitCh, _ := existing.(chan struct{})
		if waitCh != nil {
			<-waitCh
		}
		return h.cachedStaticMemorySnapshot(userID), false
	}
	defer func() {
		h.snapshotWarmInflight.Delete(userID)
		close(doneCh)
	}()

	if cached := h.cachedStaticMemorySnapshot(userID); cached != "" {
		return cached, false
	}
	gen := h.snapshotGeneration(userID)
	return h.buildAndStoreStaticMemorySnapshot(userID, gen)
}

func (h *IMMessageHandler) snapshotGeneration(userID string) uint64 {
	v, _ := h.snapshotEpoch.LoadOrStore(userID, &atomic.Uint64{})
	return v.(*atomic.Uint64).Load()
}

func (h *IMMessageHandler) bumpSnapshotGeneration(userID string) uint64 {
	v, _ := h.snapshotEpoch.LoadOrStore(userID, &atomic.Uint64{})
	return v.(*atomic.Uint64).Add(1)
}

func (h *IMMessageHandler) buildAndStoreStaticMemorySnapshot(userID string, gen uint64) (text string, built bool) {
	var staticBuf strings.Builder
	h.generateStaticMemorySection(&staticBuf, true, userID)
	snapshot := staticBuf.String()
	if snapshot == "" {
		return "", false
	}
	// Drop stale builds invalidated by RefreshMemorySnapshot while we generated.
	if h.snapshotGeneration(userID) != gen {
		return "", false
	}
	// Publish snapshot before the initialized flag so readers never observe
	// initialized=true with a missing body.
	h.frozenMemorySnapshots.Store(userID, snapshot)
	h.snapshotInitialized.Store(userID, true)
	// Re-check after publish: a Refresh may have raced between gen check and store.
	if h.snapshotGeneration(userID) != gen {
		h.frozenMemorySnapshots.Delete(userID)
		h.snapshotInitialized.Delete(userID)
		return "", false
	}
	return snapshot, true
}

func (h *IMMessageHandler) cachedStaticMemorySnapshot(userID string) string {
	if h == nil {
		return ""
	}
	// Prefer the snapshot body; initialized is a secondary gate for Refresh clears.
	snapshot, ok := h.frozenMemorySnapshots.Load(userID)
	if !ok {
		return ""
	}
	text, _ := snapshot.(string)
	if text == "" {
		return ""
	}
	if initialized, ok := h.snapshotInitialized.Load(userID); ok && initialized.(bool) {
		return text
	}
	// Snapshot present but flag cleared mid-Refresh — treat as miss.
	return ""
}

// generateStaticMemorySection builds the frozen part of the memory section:
// user_fact summary + memory recall hint + memory guide (first turn only).
// This content is stable across messages within a session and can be cached.
func (h *IMMessageHandler) generateStaticMemorySection(b *strings.Builder, isFirstTurn bool, userID string) {
	if h.memoryStore == nil {
		return
	}
	strictOwner := isIsolatedAssistantSessionUserID(userID)
	b.WriteString("\n")
	b.WriteString(corememory.PromptSectionUserMemory)
	b.WriteByte('\n')
	opts := corememory.RecallHintAndGuidePromptOptions(isFirstTurn && !strictOwner, corememory.BuildIMMemoryGuidePrompt())
	b.WriteString(h.memoryStore.StaticMemorySectionForPrompt(opts))
}

func isLansengerGroupConversationUserID(userID string) bool {
	return strings.HasPrefix(strings.TrimSpace(userID), "lansenger-group:")
}

// isProjectTabUserID returns true when the userID was synthesized for a
// Project Tab (format: "desktop-user:{projectPath}"). The local Tab uses
// the plain "desktop-user" constant without a colon suffix.
func isProjectTabUserID(userID string) bool {
	const prefix = desktopUserID + ":"
	return strings.HasPrefix(userID, prefix) && len(userID) > len(prefix) &&
		!strings.HasPrefix(userID, expertSessionUserIDPrefix) &&
		!isACPAssistantSessionUserID(userID)
}

// appendProactiveRecall writes the per-message catalog (index + pull hint).
// Warehouse memory bodies stay in tools; this is not frozen with the static snapshot.
func (h *IMMessageHandler) appendProactiveRecall(b *strings.Builder, msg string, strictProject bool, eventContext ...lifecycle.EventContext) {
	h.appendProactiveRecallForUser(b, msg, strictProject, h.promptRuntimeUserID(nil), eventContext...)
}

func (h *IMMessageHandler) appendProactiveRecallForUser(b *strings.Builder, msg string, strictProject bool, userID string, eventContext ...lifecycle.EventContext) {
	if h.memoryStore == nil || msg == "" {
		return
	}
	_ = eventContext

	projectPath := ""
	if strictProject {
		// In Project Tab mode, extract the project path directly from the
		// synthesized userID (format "desktop-user:{projectPath}"). This is
		// the authoritative source — contextResolver.ResolveProject() returns
		// the global current project which may differ from the Tab's project.
		projectPath = projectPathFromUserID(userID)
	}
	// Local tab (and any non-strict owner): align catalog scope with the same
	// working directory tools/system-prompt use. Do NOT fall back to
	// ResolveProject()/Projects list or user home — that is what made agents
	// "ignore" the top-bar directory and chase Pictures from memory.
	if projectPath == "" && h.app != nil {
		projectPath = strings.TrimSpace(h.app.EffectiveWorkingDirForOwner(userID))
	}
	if projectPath == "" && h.contextResolver != nil {
		projectPath, _ = h.contextResolver.ResolveProject()
	}

	opts := corememory.IMProactivePromptOptions(projectPath, strictProject)
	opts.Recall.OwnerID = userID
	// Active project, expert, and group conversations have an exact owner
	// boundary. The core memory layer may still admit an explicitly archived
	// global experience; no ordinary/shared/raw memory crosses this boundary.
	opts.Recall.StrictOwner = isIsolatedAssistantSessionUserID(userID)
	opts.Recall.AllowArchivedExperience = isIsolatedAssistantSessionUserID(userID)
	// CatalogOnly: index + pull hint only. Skip BM25/embedding and the 2.5s
	// budgeted goroutine that used to dump warehouse bodies.
	promptContext, _ := h.memoryStore.ProactiveContextForPrompt("", opts)
	b.WriteString(promptContext)
}

func (h *IMMessageHandler) isCurrentMemoryPromptGeneration(userID string, generation uint64) bool {
	return h != nil && h.snapshotGeneration(userID) == generation
}

// RefreshMemorySnapshot regenerates the cached memory snapshot for the given
// user from current persistent storage. Called when the user issues /new,
// starts a new topic, or on application restart (first message of new session).
// (Requirement 5.4, 5.5, 5.7)
func (h *IMMessageHandler) RefreshMemorySnapshot(userID string) {
	h.invalidateMemorySnapshot(userID, true)
}

// invalidateMemorySnapshot clears one user's frozen prompt-memory state. When
// waitForBuild is false the generation bump still prevents a stale builder
// from publishing, but the caller does not block on it.
func (h *IMMessageHandler) invalidateMemorySnapshot(userID string, waitForBuild bool) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = desktopUserID
	}
	// Invalidate first so any in-flight builder discards its result on publish.
	h.bumpSnapshotGeneration(userID)
	h.frozenMemorySnapshots.Delete(userID)
	h.snapshotInitialized.Delete(userID)
	if waitForBuild {
		// Wait for the builder to finish (close done ch) so waiters are not orphaned
		// and so a late publish-under-old-gen cannot race past the bump above.
		if v, ok := h.snapshotWarmInflight.Load(userID); ok {
			if ch, ok := v.(chan struct{}); ok {
				select {
				case <-ch:
				case <-time.After(3 * time.Second):
					log.Printf("[frozen_snapshot] refresh wait timed out for user %q (build still in flight)", userID)
				}
			}
		}
	}
	// Clear again after the wait — covers a publish that slipped in before gen check.
	h.frozenMemorySnapshots.Delete(userID)
	h.snapshotInitialized.Delete(userID)
	log.Printf("[frozen_snapshot] refreshed (invalidated) memory snapshot for user %q", userID)
}

// RefreshAllMemorySnapshots invalidates every cached prompt-memory view owned
// by this handler. GUI memory edits are global to the handler's store, so
// refreshing only desktopUserID would leave active project or expert Agents
// with a stale frozen user-fact section.
func (h *IMMessageHandler) RefreshAllMemorySnapshots() {
	if h == nil {
		return
	}
	userIDs := make(map[string]struct{})
	collect := func(key, _ any) bool {
		if userID, ok := key.(string); ok && strings.TrimSpace(userID) != "" {
			userIDs[userID] = struct{}{}
		}
		return true
	}
	// Existing snapshots contain stale text. In-flight builders must also be
	// invalidated, otherwise one that read before the deletion could publish it
	// after this method returns. Do not wait here: the generation bump makes a
	// stale publish impossible, and GUI deletion must not wait on prompt work.
	h.frozenMemorySnapshots.Range(collect)
	h.snapshotWarmInflight.Range(collect)
	for userID := range userIDs {
		h.invalidateMemorySnapshot(userID, false)
	}
}

// WarmFrozenMemorySnapshot precomputes the static memory section so the first
// user message does not pay StaticMemorySectionForPrompt on the critical path.
// Safe to call from a background goroutine; no-ops when already warm or store is nil.
// Concurrent warms (and races with first-message build) are coalesced inside
// loadOrBuildStaticMemorySnapshot.
func (h *IMMessageHandler) WarmFrozenMemorySnapshot(userID string) {
	if h == nil || h.memoryStore == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = desktopUserID
	}
	started := time.Now()
	snapshot, built := h.loadOrBuildStaticMemorySnapshot(userID)
	if snapshot == "" || !built {
		return
	}
	log.Printf("[frozen_snapshot] prewarmed for user %q (%d bytes, took=%s)",
		userID, len(snapshot), time.Since(started).Round(time.Millisecond))
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
	AgentGuided bool
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
//
// Imported agent-guided workflows (Book-PDF) store SKILL.md in the single
// craft_tool step when no SkillDir exists; that blob is the documentation
// the unmanaged turn needs in order to run the workflow with host tools.
func (h *IMMessageHandler) appendKnowledgeSkillSection(b *strings.Builder, userMessage, ownerID string) {
	if h.app == nil || h.getSkillExecutor() == nil || userMessage == "" {
		return
	}

	skills := h.getSkillExecutor().List()
	// Documentation for a skill learned in another experience pool is the same
	// context bleed as advertising the skill itself, so it is filtered here too.
	skills = filterSkillsForExperienceDomain(h.skillExperienceDomainForOwner(ownerID), skills)
	if len(skills) == 0 {
		return
	}

	matched := collectMatchedSkillDocs(skills, userMessage)
	matched = mergeStickyAgentGuidedDocs(matched, skills, h.recallAgentGuidedSkill(ownerID, h.getSkillExecutor()))
	if len(matched) == 0 {
		return
	}

	tokenBudget := defaultKnowledgeSkillTokenBudget
	if cfg, err := h.loadConfig(); err == nil && cfg.KnowledgeSkillTokenBudget > 0 {
		tokenBudget = cfg.KnowledgeSkillTokenBudget
	}
	writeMatchedSkillDocs(b, matched, tokenBudget)
}

const agentGuidedSkillDocsLeadIn = "以下是已安装且匹配当前请求的 agent 工作流。它不是工具名：不要 discover_tool / search_and_install_skill 寻找同名工具，不要用 generate_pdf 替代，不要 manage_skill。按文档用 bash、read_file、write_file、edit_file 执行各阶段。\n"

func writeMatchedSkillDocs(b *strings.Builder, matched []matchedKnowledgeSkill, tokenBudget int) {
	if b == nil || len(matched) == 0 {
		return
	}
	if tokenBudget <= 0 {
		tokenBudget = defaultKnowledgeSkillTokenBudget
	}
	b.WriteString("\n## Skill 使用文档\n")
	if matchedSkillDocsIncludeAgentGuided(matched) {
		b.WriteString(agentGuidedSkillDocsLeadIn)
	}
	totalTokensUsed := 0
	for _, m := range matched {
		if totalTokensUsed >= tokenBudget {
			log.Printf("[skill_doc_inject] token budget exhausted (%d/%d), skipping skill %q", totalTokensUsed, tokenBudget, m.Name)
			break
		}
		content := m.Content
		contentTokens := estimateTokens(content)
		remaining := tokenBudget - totalTokensUsed
		if contentTokens > remaining {
			content = agentruntime.TruncateToTokenBudget(content, remaining)
			contentTokens = estimateTokens(content)
			log.Printf("[skill_doc_inject] truncated skill %q to fit budget (remaining=%d tokens)", m.Name, remaining)
		}
		totalTokensUsed += contentTokens
		b.WriteString(fmt.Sprintf("\n### Skill: %s\n", m.Name))
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

func mergeStickyAgentGuidedDocs(matched []matchedKnowledgeSkill, skills []NLSkillDefinition, name string) []matchedKnowledgeSkill {
	name = strings.TrimSpace(name)
	if name == "" {
		return matched
	}
	for _, m := range matched {
		if m.Name == name {
			return matched
		}
	}
	for _, s := range skills {
		if s.Name != name {
			continue
		}
		if normalizeSkillEntryStatus(s.Status) != skillEntryStatusActive && normalizeSkillEntryStatus(s.Status) != skillEntryStatusUnknown {
			return matched
		}
		content := skillDocContentForInjection(s)
		if content == "" {
			return matched
		}
		extra := matchedKnowledgeSkill{
			Name:        s.Name,
			Content:     content,
			ParamSchema: buildParamSchemaForSkill(s),
			Score:       1,
			AgentGuided: true,
		}
		return append([]matchedKnowledgeSkill{extra}, matched...)
	}
	return matched
}

func matchedSkillDocsIncludeAgentGuided(matched []matchedKnowledgeSkill) bool {
	for _, m := range matched {
		if m.AgentGuided {
			return true
		}
	}
	return false
}

func collectMatchedSkillDocs(skills []NLSkillDefinition, userMessage string) []matchedKnowledgeSkill {
	msgLower := strings.ToLower(userMessage)
	var matched []matchedKnowledgeSkill
	for _, s := range skills {
		if normalizeSkillEntryStatus(s.Status) != skillEntryStatusActive {
			continue
		}
		if cskill.IsInstructionOnlySkillType(s.Type) {
			continue
		}
		if isShellBrowserAutomationSkill(s) {
			continue
		}
		guided := nlSkillIsAgentGuided(s)
		if len(s.Triggers) == 0 && !guided {
			continue
		}
		score := skillDocMatchScore(s.Name, s.Triggers, msgLower)
		if score == 0 {
			continue
		}
		content := skillDocContentForInjection(s)
		if content == "" {
			continue
		}
		matched = append(matched, matchedKnowledgeSkill{
			Name:        s.Name,
			Content:     content,
			ParamSchema: buildParamSchemaForSkill(s),
			Score:       score,
			AgentGuided: guided,
		})
	}
	sortMatchedKnowledgeSkills(matched)
	return matched
}

func (h *IMMessageHandler) agentGuidedNamedSkillTurn(ownerID, userMessage string) bool {
	if h == nil || strings.TrimSpace(userMessage) == "" || h.getSkillExecutor() == nil {
		return false
	}
	skills := filterSkillsForExperienceDomain(h.skillExperienceDomainForOwner(ownerID), h.getSkillExecutor().List())
	return agentGuidedSkillDocsMatch(skills, userMessage)
}

func (h *IMMessageHandler) releaseNamedSkillOnLoopIntent(ctx *LoopContext, userID, userText string) {
	if ctx == nil || ctx.Runtime.SemanticIntent == nil {
		return
	}
	h.releaseNamedSkillIntercept(ctx.Runtime.SemanticIntent, userID, userText)
}

func (h *IMMessageHandler) releaseNamedSkillIntercept(result *intent.ClassificationResult, userID, userText string) {
	if result == nil {
		return
	}
	intentText := semanticUserIntentText(userText)
	injected := false
	if !intent.ExplicitSkillInvocation(intentText) && intent.NamedSkillInterceptCandidate(*result) {
		injected = h.agentGuidedNamedSkillTurn(userID, intentText)
	}
	intent.ReleaseNamedSkillIntercept(intentText, injected, result)
}

func agentGuidedSkillDocsMatch(skills []NLSkillDefinition, userMessage string) bool {
	if len(skills) == 0 || strings.TrimSpace(userMessage) == "" {
		return false
	}
	msgLower := strings.ToLower(userMessage)
	for _, s := range skills {
		if normalizeSkillEntryStatus(s.Status) != skillEntryStatusActive {
			continue
		}
		if cskill.IsInstructionOnlySkillType(s.Type) || isShellBrowserAutomationSkill(s) {
			continue
		}
		if skillDocMatchScore(s.Name, s.Triggers, msgLower) == 0 {
			continue
		}
		if cskill.AgentGuidedWorkflowInstructions(skillDefinitionAsEntry(s)) != "" {
			return true
		}
	}
	return false
}

func skillDocContentForInjection(s NLSkillDefinition) string {
	skillKind := normalizeSkillTypeKind(s.Type)
	if skillKind.IsKnowledge() && strings.TrimSpace(s.Content) != "" {
		return s.Content
	}
	if !skillKind.IsKnowledge() && strings.TrimSpace(s.SkillDir) != "" {
		if content := loadSkillDocContent(s.SkillDir); content != "" {
			return content
		}
	}
	// Imported agent-guided workflows store SKILL.md in the craft_tool step.
	return cskill.AgentGuidedWorkflowInstructions(skillDefinitionAsEntry(s))
}

func skillDefinitionAsEntry(s NLSkillDefinition) *corelib.NLSkillEntry {
	return &corelib.NLSkillEntry{
		Name:         s.Name,
		Description:  s.Description,
		Type:         s.Type,
		Source:       s.Source,
		Params:       s.Params,
		Steps:        s.Steps,
		RequiredArgs: s.RequiredArgs,
		SkillDir:     s.SkillDir,
		Content:      s.Content,
	}
}

// skillDocMatchScore delegates to the shared agentruntime skill-doc matcher
// so GUI and headless hosts inject skill docs with identical hit semantics.
func skillDocMatchScore(name string, triggers []string, msgLower string) int {
	return agentruntime.SkillDocMatchScore(name, triggers, msgLower)
}

func skillDocMatchAliases(name string, triggers []string) []string {
	return agentruntime.SkillDocMatchAliases(name, triggers)
}

func skillDocPhraseOccurs(msgLower, phrase string) bool {
	return agentruntime.SkillDocPhraseOccurs(msgLower, phrase)
}

func skillDocMatchLooksLikePathOperand(msg []rune, start, end int) bool {
	return agentruntime.SkillDocMatchLooksLikePathOperand(msg, start, end)
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

// appendMaintenanceExperienceHints injects high-value curator recommendations
// so the next dialogue turn can proactively suggest review/draft flows.
func (h *IMMessageHandler) appendMaintenanceExperienceHints(b *strings.Builder) {
	if h == nil || b == nil {
		return
	}
	exec := h.getSkillExecutor()
	if exec == nil {
		return
	}
	skills := exec.loadSkills()
	if section := cskill.BuildMaintenanceExperiencePromptSection(skills, 5); section != "" {
		b.WriteString(section)
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
		for _, s := range h.getSkillExecutor().loadSkills() {
			if s.MatchesName(run.Skill) && s.Publisher != "" && !isShellBrowserAutomationSkillEntry(s) {
				activePublisher = s.Publisher
				activeSkillName = s.Name
				break
			}
		}
		if activePublisher != "" {
			break
		}
	}

	if activePublisher == "" {
		return
	}

	// Find sibling skills from the same publisher.
	var siblings []string
	for _, s := range h.getSkillExecutor().loadSkills() {
		if s.Publisher == activePublisher && s.Name != activeSkillName && normalizeSkillEntryStatus(s.Status) == skillEntryStatusActive && !cskill.IsInstructionOnlySkillType(s.Type) && !isShellBrowserAutomationSkillEntry(s) {
			siblings = append(siblings, s.Name)
		}
	}

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

// countTriggerMatches counts how many of the skill's triggers match the user
// message as bounded phrases (hyphen/space/dash variants allowed). Returns 0
// if none match. Substring matching would treat trigger "git" as a hit inside
// "github".
func countTriggerMatches(triggers []string, msgLower string) int {
	return agentruntime.CountTriggerMatches(triggers, msgLower)
}

// sortMatchedKnowledgeSkills sorts matched skills by descending relevance
// score, with alphabetical name as tiebreaker for deterministic ordering.
func sortMatchedKnowledgeSkills(matched []matchedKnowledgeSkill) {
	slices.SortFunc(matched, func(a, b matchedKnowledgeSkill) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		return strings.Compare(a.Name, b.Name)
	})
}

// buildParamSchemaForSkill returns a formatted parameter schema string for
// a skill, preserving explicit params and completing any template placeholders
// that the author omitted from the declared schema. Missing descriptions are
// filled from SKILL.md when SkillDir is available.
func buildParamSchemaForSkill(s NLSkillDefinition) string {
	params := cskill.CompleteParamsForSkill(skillDefinitionAsEntry(s))
	if len(params) == 0 {
		return ""
	}
	return cskill.FormatParamSchema(params)
}

// ---------------------------------------------------------------------------
// Tool Definitions
// ---------------------------------------------------------------------------
