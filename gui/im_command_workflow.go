package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// workflowTypeAliases maps short user-friendly aliases to workflow type strings.
// Users can type `/workflow coding` or `/workflow 编程` to start a coding workflow.
var workflowTypeAliases = map[string]string{
	// English aliases
	"coding":       string(v2.WorkflowCoding),
	"code":         string(v2.WorkflowCoding),
	"ppt":          string(v2.WorkflowPresentationDesign),
	"presentation": string(v2.WorkflowPresentationDesign),
	"product":      string(v2.WorkflowProductDesign),
	"business":     string(v2.WorkflowBusinessPlan),
	"test":         string(v2.WorkflowTesting),
	"testing":      string(v2.WorkflowTesting),
	"research":     string(v2.WorkflowResearchReport),
	"paper":        string(v2.WorkflowPaperWriting),
	"patent":       string(v2.WorkflowPatentApplication),
	"bid":          string(v2.WorkflowBidResponse),
	"contract":     string(v2.WorkflowContractReview),
	"event":        string(v2.WorkflowEventPlanning),
	"competitive":  string(v2.WorkflowCompetitiveAnalysis),
	"ops":          string(v2.WorkflowOpsMaintenance),
	"maintenance":  string(v2.WorkflowMaintenance),

	// Chinese aliases
	"编程":   string(v2.WorkflowCoding),
	"开发":   string(v2.WorkflowCoding),
	"编码":   string(v2.WorkflowCoding),
	"ppt设计": string(v2.WorkflowPresentationDesign),
	"演示":   string(v2.WorkflowPresentationDesign),
	"产品":   string(v2.WorkflowProductDesign),
	"产品设计": string(v2.WorkflowProductDesign),
	"商业":   string(v2.WorkflowBusinessPlan),
	"商业计划": string(v2.WorkflowBusinessPlan),
	"测试":   string(v2.WorkflowTesting),
	"研究":   string(v2.WorkflowResearchReport),
	"论文":   string(v2.WorkflowPaperWriting),
	"专利":   string(v2.WorkflowPatentApplication),
	"招投标":  string(v2.WorkflowBidResponse),
	"合同":   string(v2.WorkflowContractReview),
	"活动":   string(v2.WorkflowEventPlanning),
	"竞品":   string(v2.WorkflowCompetitiveAnalysis),
	"运维":   string(v2.WorkflowOpsMaintenance),
}

// handleWorkflowCommand processes the /workflow slash command.
// - /workflow         → list available workflows
// - /workflow <type>  → force-start the specified workflow (bypasses toggle)
func (h *IMMessageHandler) handleWorkflowCommand(msg IMUserMessage, trimmed, lang string) (*IMAgentResponse, bool) {
	arg := ""
	if len(trimmed) > len("/workflow") {
		arg = strings.TrimSpace(trimmed[len("/workflow"):])
	}

	// No argument → list available workflows
	if arg == "" {
		return &IMAgentResponse{Text: h.buildWorkflowListText(lang)}, true
	}

	// Resolve workflow type from alias or exact type string
	workflowType := resolveWorkflowType(arg, h)
	if workflowType == "" {
		text := localizeWorkflowText(lang,
			fmt.Sprintf("Unknown workflow type: %q\nUse `/workflow` to see available types.", arg),
			fmt.Sprintf("未知的工作流类型：%q\n输入 `/workflow` 查看可用类型。", arg),
			fmt.Sprintf("未知的工作流類型：%q\n輸入 `/workflow` 查看可用類型。", arg),
		)
		return &IMAgentResponse{Text: text}, true
	}

	// Force-start the workflow (bypasses workflow_enabled toggle)
	return h.forceStartWorkflow(msg, workflowType, lang), true
}

// resolveWorkflowType resolves a user-provided string to a valid workflow type.
// Checks aliases first, then exact type match against registry.
func resolveWorkflowType(input string, h *IMMessageHandler) string {
	lower := strings.ToLower(strings.TrimSpace(input))

	// Check aliases
	if wfType, ok := workflowTypeAliases[lower]; ok {
		return wfType
	}

	// Check exact type string against registry
	wf := h.getWorkflowV2()
	if wf != nil && wf.registry != nil {
		if tmpl := wf.registry.Get(lower); tmpl != nil {
			return tmpl.Type
		}
	}

	return ""
}

// forceStartWorkflow starts a workflow directly, bypassing the workflow_enabled toggle.
// Reuses the same pendingWorkflowChoice mechanism as StartWorkflowDirect.
//
// For desktop platform: sends a synthetic choice command through SendAIAssistantMessage
// (same mechanism as workflow panel click).
// For IM platforms: sends through HandleIMMessage directly with the IM user's identity.
func (h *IMMessageHandler) forceStartWorkflow(msg IMUserMessage, workflowType, lang string) *IMAgentResponse {
	wf := h.getWorkflowV2()
	if wf == nil {
		return &IMAgentResponse{Error: localizeWorkflowText(lang,
			"Workflow engine not initialized.",
			"工作流引擎未初始化。",
			"工作流引擎未初始化。",
		)}
	}

	tmpl := wf.registry.Get(workflowType)
	if tmpl == nil {
		return &IMAgentResponse{Error: localizeWorkflowText(lang,
			fmt.Sprintf("Workflow template %q not found.", workflowType),
			fmt.Sprintf("找不到工作流模板 %q。", workflowType),
			fmt.Sprintf("找不到工作流模板 %q。", workflowType),
		)}
	}

	userID := msg.UserID

	// Cancel any active agent loop first — starting a workflow is a new task.
	if ctx := h.getSessionLoopCtx(userID); ctx != nil {
		ctx.Cancel()
	}

	// Cancel any active workflow for this user before starting a new one.
	h.cancelWorkflowForUser(userID)

	projectPath := ""
	if h.app != nil {
		projectPath = strings.TrimSpace(h.app.GetCurrentProjectPath())
	}
	if projectPath == "" {
		projectPath = "."
	}

	choiceID := fmt.Sprintf("slash-cmd-%d", time.Now().UnixNano())
	h.pendingWorkflowChoice.Store(userID, &pendingWorkflowChoice{
		Msg: msg,
		RouteResult: &v2.RouteResult{
			Target:       "workflow",
			WorkflowType: workflowType,
			ProjectPath:  projectPath,
		},
		ChoiceID: choiceID,
	})

	// Route the choice command through the normal message path for proper
	// session serialization. The subsequent handler call picks up the
	// pendingWorkflowChoice stored above and starts the workflow.
	choiceCommand := buildWorkflowChoiceCommand(workflowChoiceComplex, choiceID)

	isDesktop := normalizeIMMessagePlatformKind(msg.Platform).IsDesktop() || msg.Platform == "" || msg.Platform == desktopPlatform
	if isDesktop && h.app != nil {
		// Desktop: use SendAIAssistantMessage (same as workflow panel click).
		requestID := fmt.Sprintf("desktop-ai-%d-workflow-cmd", time.Now().UnixNano())
		go func() {
			if _, err := h.app.SendAIAssistantMessage(AIAssistantSendRequest{
				Text:         choiceCommand,
				RequestID:    requestID,
				ProjectPath:  "",
				EventScopeID: "local",
			}); err != nil {
				log.Printf("[/workflow] SendAIAssistantMessage failed: type=%s err=%v", workflowType, err)
				h.pendingWorkflowChoice.Delete(userID)
			}
		}()
	} else {
		// IM channel: send through HandleIMMessage with the original user identity.
		go func() {
			h.HandleIMMessage(IMUserMessage{
				UserID:   userID,
				Platform: msg.Platform,
				Text:     choiceCommand,
				Lang:     msg.Lang,
			})
		}()
	}

	displayName := tmpl.Name
	if displayName == "" {
		displayName = workflowType
	}
	return &IMAgentResponse{Text: localizeWorkflowText(lang,
		fmt.Sprintf("🚀 Starting workflow: %s", displayName),
		fmt.Sprintf("🚀 正在启动工作流：%s", displayName),
		fmt.Sprintf("🚀 正在啟動工作流：%s", displayName),
	)}
}

// buildWorkflowListText builds the help text listing available workflow types.
// Shows common workflows grouped by category, not the full 30+ template list.
func (h *IMMessageHandler) buildWorkflowListText(lang string) string {
	wf := h.getWorkflowV2()

	var sb strings.Builder

	header := localizeWorkflowText(lang,
		"Use `/workflow <type>` to force-start a workflow.\n\nCommon workflows:\n",
		"输入 `/workflow <类型>` 强制启动工作流。\n\n常用工作流：\n",
		"輸入 `/workflow <類型>` 強制啟動工作流。\n\n常用工作流：\n",
	)
	sb.WriteString(header)

	if wf == nil || wf.registry == nil {
		sb.WriteString(localizeWorkflowText(lang,
			"  (workflow engine not initialized)",
			"  （工作流引擎未初始化）",
			"  （工作流引擎未初始化）",
		))
		return sb.String()
	}

	// Show curated common workflows grouped by category
	type aliasEntry struct {
		alias   string
		typeStr string
	}
	commonWorkflows := []struct {
		category string
		items    []aliasEntry
	}{
		{
			category: localizeWorkflowText(lang, "Development", "开发", "開發"),
			items: []aliasEntry{
				{"coding", string(v2.WorkflowCoding)},
				{"testing", string(v2.WorkflowTesting)},
				{"ops", string(v2.WorkflowOpsMaintenance)},
			},
		},
		{
			category: localizeWorkflowText(lang, "Business", "商业", "商業"),
			items: []aliasEntry{
				{"product", string(v2.WorkflowProductDesign)},
				{"business", string(v2.WorkflowBusinessPlan)},
				{"competitive", string(v2.WorkflowCompetitiveAnalysis)},
			},
		},
		{
			category: localizeWorkflowText(lang, "Document & Presentation", "文档演示", "文檔演示"),
			items: []aliasEntry{
				{"ppt", string(v2.WorkflowPresentationDesign)},
				{"paper", string(v2.WorkflowPaperWriting)},
				{"patent", string(v2.WorkflowPatentApplication)},
				{"bid", string(v2.WorkflowBidResponse)},
				{"contract", string(v2.WorkflowContractReview)},
			},
		},
		{
			category: localizeWorkflowText(lang, "Research", "学术研究", "學術研究"),
			items: []aliasEntry{
				{"research", string(v2.WorkflowResearchReport)},
				{"paper_reproduction", string(v2.WorkflowPaperReproduction)},
			},
		},
	}

	for _, group := range commonWorkflows {
		sb.WriteString(fmt.Sprintf("\n  [%s]\n", group.category))
		for _, item := range group.items {
			tmpl := wf.registry.Get(item.typeStr)
			name := item.typeStr
			if tmpl != nil && tmpl.Name != "" {
				name = tmpl.Name
			}
			sb.WriteString(fmt.Sprintf("    /workflow %-20s %s\n", item.alias, name))
		}
	}

	sb.WriteString(localizeWorkflowText(lang,
		"\nYou can also use the exact type name (e.g. `/workflow presentation_design`).\nSee all types in the Workflow panel on the left.",
		"\n也可以使用精确类型名（如 `/workflow presentation_design`）。\n完整列表请查看左侧「工作流」面板。",
		"\n也可以使用精確類型名（如 `/workflow presentation_design`）。\n完整列表請查看左側「工作流」面板。",
	))

	return sb.String()
}

// localizeWorkflowText is a helper for localizing /workflow command text.
func localizeWorkflowText(lang, en, zhHans, zhHant string) string {
	if strings.HasPrefix(lang, "zh") {
		if strings.Contains(lang, "TW") || strings.Contains(lang, "Hant") {
			return zhHant
		}
		return zhHans
	}
	return en
}
