package agentservice

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// sharedHostToolSpecs advertises GUI-shared capabilities that the historical
// srv catalog omitted. Desktop-display-only tools stay visible with an honest
// DisabledReason when the host cannot provide I/O.
func (c *coreAgentCallbacks) sharedHostToolSpecs() []coreToolSpec {
	workspaceOK := strings.TrimSpace(c.workspace) != ""
	workspaceReason := ""
	if !workspaceOK {
		workspaceReason = "no workspace configured for this instance"
	}
	return []coreToolSpec{
		{
			Name:        "manage_skill",
			Description: skill.ManageSkillDescription(),
			Enabled:     c.skillProvider != nil,
			DisabledReason: func() string {
				if c.skillProvider == nil {
					return "skill system is not configured"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action":   map[string]interface{}{"type": "string", "description": "list, search, install, run, or maintenance_plan"},
					"query":    map[string]interface{}{"type": "string"},
					"name":     map[string]interface{}{"type": "string"},
					"skill_id": map[string]interface{}{"type": "string"},
					"hub_url":  map[string]interface{}{"type": "string"},
					"args":     map[string]interface{}{"type": "object"},
				},
				"required": []string{"action"},
			},
		},
		{
			Name:        "goal",
			Description: "Manage a persistent long-running goal (action: create/complete/fail/get).",
			Enabled:     c.goals != nil,
			DisabledReason: func() string {
				if c.goals == nil {
					return "goal store is not initialized"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action":              map[string]interface{}{"type": "string"},
					"objective":           map[string]interface{}{"type": "string"},
					"token_budget":        map[string]interface{}{"type": "integer"},
					"max_turns":           map[string]interface{}{"type": "integer"},
					"acceptance_criteria": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"summary":             map[string]interface{}{"type": "string"},
					"reason":              map[string]interface{}{"type": "string"},
				},
				"required": []string{"action"},
			},
		},
		{
			Name:        "delegate_task",
			Description: "Delegate a task to a bound child agent and wait for the finished result. coding_workflow runs the shared coding runtime; help answers product questions.",
			Enabled:     c.canDelegateCodingWorkflow() || c.delegateSubtask != nil,
			DisabledReason: func() string {
				if !c.canDelegateCodingWorkflow() && c.delegateSubtask == nil {
					return "delegate_task host adapter is not initialized"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent":        map[string]interface{}{"type": "string", "description": "coding_workflow or help"},
					"request":      map[string]interface{}{"type": "string"},
					"task":         map[string]interface{}{"type": "string", "description": "Alias for request"},
					"project_path": map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			Name:        "asr",
			Description: "Transcribe a local audio file in the instance workspace.",
			Enabled:     reviewedHostSpeechReady(c.speechTranscriber),
			DisabledReason: func() string {
				if !reviewedHostSpeechReady(c.speechTranscriber) {
					return "speech transcriber is not initialized"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":   map[string]interface{}{"type": "string"},
					"format": map[string]interface{}{"type": "string"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "tts",
			Description: "Synthesize speech from text. Headless hosts render an audio artifact; desktop/IM hosts may also play or send it.",
			Enabled:     reviewedHostSpeechSynthesizerReady(c.speechSynthesizer),
			DisabledReason: func() string {
				if !reviewedHostSpeechSynthesizerReady(c.speechSynthesizer) {
					return "speech synthesizer is not initialized"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"text": map[string]interface{}{"type": "string"}},
				"required":   []string{"text"},
			},
		},
		{
			Name:        "tts_render",
			Description: "Render speech as a workspace audio artifact. Does not play or send.",
			Enabled:     reviewedHostSpeechSynthesizerReady(c.speechSynthesizer),
			DisabledReason: func() string {
				if !reviewedHostSpeechSynthesizerReady(c.speechSynthesizer) {
					return "speech synthesizer is not initialized"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"text": map[string]interface{}{"type": "string"}},
				"required":   []string{"text"},
			},
		},
		{
			Name:           "FileRead",
			Description:    "Read a precise UTF-8 line range from a workspace file.",
			Enabled:        workspaceOK,
			DisabledReason: workspaceReason,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":       map[string]interface{}{"type": "string"},
					"file_path":  map[string]interface{}{"type": "string", "description": "Alias for path"},
					"start_line": map[string]interface{}{"type": "integer"},
					"end_line":   map[string]interface{}{"type": "integer"},
					"lines":      map[string]interface{}{"type": "integer"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:           "ripgrep",
			Description:    "Search workspace files with a regular expression.",
			Enabled:        workspaceOK,
			DisabledReason: workspaceReason,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern":     map[string]interface{}{"type": "string"},
					"path":        map[string]interface{}{"type": "string"},
					"glob":        map[string]interface{}{"type": "string"},
					"max_results": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:           "Glob",
			Description:    "Find workspace files by glob pattern.",
			Enabled:        workspaceOK,
			DisabledReason: workspaceReason,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{"type": "string"},
					"path":    map[string]interface{}{"type": "string"},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:           "edit_lines",
			Description:    "Edit a workspace file by line number (replace, insert, or delete).",
			Enabled:        workspaceOK,
			DisabledReason: workspaceReason,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":       map[string]interface{}{"type": "string"},
					"operation":  map[string]interface{}{"type": "string"},
					"start_line": map[string]interface{}{"type": "integer"},
					"end_line":   map[string]interface{}{"type": "integer"},
					"content":    map[string]interface{}{"type": "string"},
				},
				"required": []string{"path", "operation", "start_line"},
			},
		},
		{
			Name:           "read_excel",
			Description:    "Read an Excel file (.xlsx/.xls/.csv) in the instance workspace.",
			Enabled:        workspaceOK,
			DisabledReason: workspaceReason,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{"type": "string"},
					"path":      map[string]interface{}{"type": "string"},
					"sheet":     map[string]interface{}{"type": "string"},
					"range":     map[string]interface{}{"type": "string"},
					"max_rows":  map[string]interface{}{"type": "integer"},
				},
			},
		},
		{
			Name:           "write_excel",
			Description:    "Write an XLSX file in the instance workspace.",
			Enabled:        workspaceOK,
			DisabledReason: workspaceReason,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{"type": "string"},
					"data":      map[string]interface{}{"type": "object"},
				},
				"required": []string{"file_path", "data"},
			},
		},
		{
			Name:           "read_pptx",
			Description:    "Read a PowerPoint PPTX file in the instance workspace.",
			Enabled:        workspaceOK,
			DisabledReason: workspaceReason,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path":    map[string]interface{}{"type": "string"},
					"path":         map[string]interface{}{"type": "string"},
					"max_slides":   map[string]interface{}{"type": "integer"},
					"slide_offset": map[string]interface{}{"type": "integer"},
				},
			},
		},
		{
			Name:           "office",
			Description:    "Office/PDF/text document tool. action: read_document/read_excel/write_excel/read_pptx/generate_pdf.",
			Enabled:        workspaceOK,
			DisabledReason: workspaceReason,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action":    map[string]interface{}{"type": "string"},
					"file_path": map[string]interface{}{"type": "string"},
					"path":      map[string]interface{}{"type": "string"},
					"content":   map[string]interface{}{"type": "string"},
					"title":     map[string]interface{}{"type": "string"},
					"data":      map[string]interface{}{"type": "object"},
				},
				"required": []string{"action"},
			},
		},
		{
			Name:           "generate_pdf",
			Description:    "Render Markdown content to a PDF in the instance workspace.",
			Enabled:        workspaceOK,
			DisabledReason: workspaceReason,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{"type": "string"},
					"title":   map[string]interface{}{"type": "string"},
				},
				"required": []string{"content"},
			},
		},
		{
			Name:           "download_file",
			Description:    "Download an HTTP/HTTPS URL into the instance workspace.",
			Enabled:        workspaceOK,
			DisabledReason: workspaceReason,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url":       map[string]interface{}{"type": "string"},
					"save_path": map[string]interface{}{"type": "string"},
					"output":    map[string]interface{}{"type": "string"},
				},
				"required": []string{"url"},
			},
		},
		{
			Name:        "list_mcp_tools",
			Description: "List ready MCP servers and their tools for this user.",
			Enabled:     c.mcpProvider != nil,
			DisabledReason: func() string {
				if c.mcpProvider == nil {
					return "MCP provider is not initialized"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			Name:        "import_mcp_servers",
			Description: "Import MCP servers from JSON into this user's config. Accepts {\"mcpServers\":{...}} or MaClaw create entries.",
			Enabled:     c.canImportMCP(),
			DisabledReason: func() string {
				if !c.canImportMCP() {
					return "MCP import persistence is not configured"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"json_config": map[string]interface{}{"type": "string"},
					"target":      map[string]interface{}{"type": "string"},
				},
				"required": []string{"json_config"},
			},
		},
		{
			Name:        "screenshot",
			Description: "Capture the operator desktop. Unavailable on a headless host without a display adapter.",
			Enabled:     reviewedHostDesktopCapturerReady(c.desktopCapturer),
			DisabledReason: func() string {
				if !reviewedHostDesktopCapturerReady(c.desktopCapturer) {
					return "desktop screenshot is unavailable on this headless host"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
			},
		},
		{
			Name:        "open",
			Description: "Open a file or URL with the host default handler. Desktop-display-only on hosts without a launcher.",
			Enabled:     reviewedHostURLLauncherReady(c.urlLauncher) || reviewedHostDocumentLauncherReady(c.documentLauncher),
			DisabledReason: func() string {
				if !reviewedHostURLLauncherReady(c.urlLauncher) && !reviewedHostDocumentLauncherReady(c.documentLauncher) {
					return "OS open is unavailable on this headless host"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"target": map[string]interface{}{"type": "string"}},
				"required":   []string{"target"},
			},
		},
	}
}

func (c *coreAgentCallbacks) executeSharedHostTool(name string, args map[string]interface{}) (agent.ToolExecutionResult, bool) {
	switch strings.TrimSpace(name) {
	case "goal":
		out := agent.ToolGoal(c.goals, args)
		return toolTextResult(out), true
	case "delegate_task":
		return c.executeDelegateTask(args), true
	case "asr":
		return c.executeASR(args), true
	case "tts", "tts_render":
		return c.executeTTS(name, args), true
	case "FileRead":
		return c.executeScopedAgentFileTool(args, []string{"path", "file_path", "file"}, func(scoped map[string]interface{}) string {
			if stringArg(scoped, "path") == "" {
				scoped["path"] = firstNonEmpty(stringArg(scoped, "file_path"), stringArg(scoped, "file"))
			}
			return agent.ToolFileRead(scoped)
		}), true
	case "ripgrep":
		return c.executeScopedSearchTool(args, agent.ToolRipgrepDetailed), true
	case "Glob":
		return c.executeScopedSearchTool(args, agent.ToolGlobDetailed), true
	case "edit_lines":
		return c.executeEditLines(args), true
	case "read_excel":
		return c.executeScopedAgentFileTool(args, []string{"file_path", "path"}, agent.ToolReadExcel), true
	case "write_excel":
		return c.executeWriteExcel(args), true
	case "read_pptx":
		return c.executeScopedAgentFileTool(args, []string{"file_path", "path"}, agent.ToolReadPPTX), true
	case "office":
		return c.executeOffice(args), true
	case "generate_pdf":
		return c.executeGeneratePDF(args), true
	case "download_file":
		return c.executeDownloadFile(args), true
	case "list_mcp_tools":
		return c.executeListMCPTools(), true
	case "import_mcp_servers":
		return c.executeImportMCPServers(args), true
	case "screenshot":
		return c.executeScreenshot(args), true
	case "open":
		return c.executeOpen(args), true
	default:
		return agent.ToolExecutionResult{}, false
	}
}

func toolTextResult(out string) agent.ToolExecutionResult {
	outcome := agent.ToolExecutionOutcomeOK
	if sharedHostToolTextFailed(out) {
		outcome = agent.ToolExecutionOutcomeError
	}
	return agent.ToolExecutionResult{Result: out, Outcome: outcome}
}

func commandOutputToolResult(out string) agent.ToolExecutionResult {
	trimmed := strings.TrimSpace(out)
	if strings.Contains(out, "[错误] 命令超时") {
		return agent.ToolExecutionResult{Result: out, Outcome: agent.ToolExecutionOutcomeTimeout}
	}
	if strings.Contains(out, "\n[错误]") || strings.Contains(out, "\n[error] command cancelled") {
		return agent.ToolExecutionResult{Result: out, Outcome: agent.ToolExecutionOutcomeError}
	}
	if strings.HasPrefix(trimmed, "[错误]") || strings.HasPrefix(trimmed, "[system rejected]") || strings.HasPrefix(trimmed, "缺少 ") {
		return agent.ToolExecutionResult{Result: out, Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: out, Outcome: agent.ToolExecutionOutcomeOK}
}

func sshToolLayerErrorLine(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "错误:") ||
		strings.HasPrefix(line, "未知 SSH") ||
		strings.HasPrefix(line, "发送命令失败") ||
		strings.HasPrefix(line, "SSH 会话已断开")
}

func sshToolResult(out string) agent.ToolExecutionResult {
	trimmed := strings.TrimSpace(out)
	firstLine, rest, _ := strings.Cut(trimmed, "\n")
	if sshToolLayerErrorLine(firstLine) {
		return agent.ToolExecutionResult{Result: out, Outcome: agent.ToolExecutionOutcomeError}
	}
	// ToolSSH prefixes reconnect-then-fail as "连接已断开并自动重连\n发送命令失败: ...".
	if firstLine == "连接已断开并自动重连" {
		secondLine, _, _ := strings.Cut(strings.TrimSpace(rest), "\n")
		if sshToolLayerErrorLine(secondLine) {
			return agent.ToolExecutionResult{Result: out, Outcome: agent.ToolExecutionOutcomeError}
		}
	}
	return agent.ToolExecutionResult{Result: out, Outcome: agent.ToolExecutionOutcomeOK}
}

func sharedHostToolTextFailed(out string) bool {
	out = strings.TrimSpace(out)
	if out == "" {
		return false
	}
	firstLine, _, _ := strings.Cut(out, "\n")
	lower := strings.ToLower(firstLine)
	if strings.HasPrefix(lower, "error:") || strings.HasPrefix(firstLine, "错误:") || strings.HasPrefix(firstLine, "[错误]") || strings.HasPrefix(firstLine, "目标管理器未初始化") || strings.HasPrefix(firstLine, "任务管理器未初始化") || strings.HasPrefix(firstLine, "long-term memory is not initialized") || strings.HasPrefix(firstLine, "未知 task action") || strings.HasPrefix(firstLine, "未知 goal action") || strings.HasPrefix(firstLine, "创建目标失败") || strings.HasPrefix(firstLine, "未知 SSH") || strings.HasPrefix(firstLine, "unknown memory action") || strings.HasPrefix(firstLine, "save memory failed") || strings.HasPrefix(firstLine, "delete memory failed") || strings.HasPrefix(firstLine, "memory candidate rejected") || strings.HasPrefix(firstLine, "derived surgery failed") || strings.HasPrefix(firstLine, "unsupported derived surgery") {
		return true
	}
	if _, failed := agent.DocumentReadFailure(out); failed {
		return true
	}
	switch {
	case strings.HasPrefix(firstLine, "缺少 "),
		strings.HasPrefix(firstLine, "文件不存在或无法访问"),
		strings.HasPrefix(firstLine, "读取失败"),
		strings.HasPrefix(firstLine, "missing pattern"),
		strings.HasPrefix(firstLine, "invalid regex"),
		strings.HasPrefix(firstLine, "search cancelled"),
		strings.HasPrefix(firstLine, "Glob cancelled"),
		strings.HasPrefix(firstLine, "data 参数格式错误"),
		strings.HasPrefix(firstLine, "missing query parameter"),
		strings.HasPrefix(firstLine, "missing content parameter"),
		strings.HasPrefix(firstLine, "missing id parameter"),
		strings.HasPrefix(firstLine, "cannot combine "),
		strings.HasPrefix(firstLine, "pagination not available"),
		strings.HasPrefix(firstLine, "scroll sessions not available"),
		strings.HasPrefix(firstLine, "未知 "),
		strings.HasPrefix(firstLine, "发送失败"),
		strings.HasPrefix(firstLine, "定时任务管理器未初始化"),
		strings.HasPrefix(firstLine, "请提供 "),
		strings.Contains(firstLine, "必须在 "):
		return true
	}
	if strings.Contains(firstLine, "失败:") {
		return true
	}
	if strings.Contains(firstLine, " in this isolated conversation") {
		return true
	}
	if strings.Contains(firstLine, " 是目录，请使用") {
		return true
	}
	if strings.HasPrefix(firstLine, "start_line=") || strings.HasPrefix(firstLine, "end_line=") {
		return true
	}
	return false
}

func (c *coreAgentCallbacks) codingRuntimeParent() *CoreAgentExecutor {
	if c == nil {
		return nil
	}
	if c.executor != nil {
		return c.executor
	}
	return c.runtimeParentExecutor
}

func (c *coreAgentCallbacks) canDelegateCodingWorkflow() bool {
	parent := c.codingRuntimeParent()
	return parent != nil && parent.getCodingRuntimeStore() != nil
}

func (c *coreAgentCallbacks) canImportMCP() bool {
	if c == nil || c.mcpProvider == nil {
		return false
	}
	_, ok := c.mcpProvider.(mcpJSONImporter)
	return ok
}

func (c *coreAgentCallbacks) executeDelegateTask(args map[string]interface{}) agent.ToolExecutionResult {
	agentName := strings.ToLower(strings.TrimSpace(stringArg(args, "agent")))
	taskText := firstNonEmpty(stringArg(args, "request"), stringArg(args, "task"), stringArg(args, "description"))
	if agentName == "" {
		return agent.ToolExecutionResult{Result: "Available sub-agents:\n- coding_workflow: runs the shared coding runtime for coding tasks\n- help: product usage help", Outcome: agent.ToolExecutionOutcomeOK}
	}
	if agentName == "coding_workflow" {
		return c.executeCodingWorkflowDelegate(taskText, stringArg(args, "project_path"))
	}
	if strings.TrimSpace(taskText) == "" {
		return agent.ToolExecutionResult{Result: "Error: delegate_task requires request or task", Outcome: agent.ToolExecutionOutcomeError}
	}
	if c.delegateSubtask == nil {
		return agent.ToolExecutionResult{Result: "Error: delegate_task host adapter is not initialized", Outcome: agent.ToolExecutionOutcomeError}
	}
	out, err := c.delegateSubtask(c.parentContext(), c.principal, taskText)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	if strings.TrimSpace(out) == "" {
		return agent.ToolExecutionResult{Result: "Error: delegate_task returned an empty result", Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: out, Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) executeCodingWorkflowDelegate(request, projectPath string) agent.ToolExecutionResult {
	if strings.TrimSpace(request) == "" {
		return agent.ToolExecutionResult{Result: "Error: delegate_task(coding_workflow) requires a non-empty request", Outcome: agent.ToolExecutionOutcomeError}
	}
	if c.runtimeStore != nil && c.runtimeAttempt != nil {
		return agent.ToolExecutionResult{Result: "Error: nested coding_workflow is not allowed from an active coding runtime attempt", Outcome: agent.ToolExecutionOutcomeError}
	}
	parent := c.codingRuntimeParent()
	if parent == nil {
		return agent.ToolExecutionResult{Result: "Error: coding runtime host is unavailable", Outcome: agent.ToolExecutionOutcomeError}
	}
	if parent.getCodingRuntimeStore() == nil {
		return agent.ToolExecutionResult{Result: "Error: coding runtime is unavailable", Outcome: agent.ToolExecutionOutcomeError}
	}
	inst := c.instance
	if strings.TrimSpace(inst.Workspace) == "" {
		inst.Workspace = strings.TrimSpace(c.workspace)
	}
	if project := strings.TrimSpace(projectPath); project != "" {
		resolved, err := c.resolveWorkspacePath(project)
		if err != nil {
			return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
		}
		inst.Workspace = resolved
	}
	if strings.TrimSpace(inst.Workspace) == "" {
		return agent.ToolExecutionResult{Result: "Error: coding runtime requires an instance workspace", Outcome: agent.ToolExecutionOutcomeError}
	}
	req := ExecuteRequest{
		Principal: c.principal,
		Tenant:    c.tenant,
		User:      c.user,
		Instance:  inst,
		Session:   c.session,
		Config:    c.appCfg,
		DataDir:   c.dataDir,
		Message: Message{
			Content: request,
			Metadata: map[string]string{
				metaCodingRuntimeMode:       "local_workflow",
				metaCodingRuntimeWorkflowID: "delegate_coding",
				metaCodingRuntimePhaseID:    "implementation",
			},
		},
		MutationScope:       v2.MutationScopeProject,
		ToolPolicy:          c.toolPolicy,
		OpsApprovedCommands: append([]v2.OpsApprovedCommand(nil), c.opsApprovedCommands...),
	}
	parentSession := strings.TrimSpace(req.Session.ID)
	if parentSession == "" {
		parentSession = "sess"
	}
	req.Session.ID = fmt.Sprintf("delegate-%s-%d", parentSession, time.Now().UnixNano())
	result, err := parent.Execute(c.parentContext(), req)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	if result == nil {
		return agent.ToolExecutionResult{Result: "Error: coding_workflow returned no result", Outcome: agent.ToolExecutionOutcomeError}
	}
	text := strings.TrimSpace(result.Content)
	id, status := "", ""
	if result.Metadata != nil {
		id = strings.TrimSpace(result.Metadata[metaCodingRuntimeTaskID])
		status = strings.TrimSpace(result.Metadata[metaCodingRuntimeTaskStatus])
	}
	if id != "" {
		if text != "" {
			text += "\n"
		}
		text += "coding_runtime_task_id=" + id
		if status != "" {
			text += " status=" + status
		}
	}
	if text == "" {
		return agent.ToolExecutionResult{Result: "Error: coding_workflow returned an empty result", Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: text, Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) executeASR(args map[string]interface{}) agent.ToolExecutionResult {
	if !reviewedHostSpeechReady(c.speechTranscriber) {
		return agent.ToolExecutionResult{Result: "Error: speech transcriber is not initialized", Outcome: agent.ToolExecutionOutcomeError}
	}
	rawPath := firstNonEmpty(stringArg(args, "path"), stringArg(args, "file_path"))
	absPath, err := c.resolveWorkspacePath(rawPath)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: read audio failed: %v", err), Outcome: agent.ToolExecutionOutcomeError}
	}
	mime := stringArg(args, "format")
	if mime == "" {
		switch strings.ToLower(filepath.Ext(absPath)) {
		case ".wav":
			mime = "audio/wav"
		case ".mp3":
			mime = "audio/mpeg"
		case ".ogg", ".opus", ".oga":
			mime = "audio/ogg"
		case ".m4a":
			mime = "audio/mp4"
		default:
			mime = "application/octet-stream"
		}
	}
	text, err := c.speechTranscriber.TranscribeSpeech(c.parentContext(), mime, data)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	if strings.TrimSpace(text) == "" {
		return agent.ToolExecutionResult{Result: "Error: transcription was empty", Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: text, Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) executeTTS(name string, args map[string]interface{}) agent.ToolExecutionResult {
	text := strings.TrimSpace(stringArg(args, "text"))
	if text == "" {
		return agent.ToolExecutionResult{Result: "Error: missing text", Outcome: agent.ToolExecutionOutcomeError}
	}
	if !reviewedHostSpeechSynthesizerReady(c.speechSynthesizer) {
		return agent.ToolExecutionResult{Result: "Error: speech synthesizer is not initialized", Outcome: agent.ToolExecutionOutcomeError}
	}
	wav, err := c.speechSynthesizer.RenderSpeech(c.parentContext(), text)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	if len(wav) == 0 {
		return agent.ToolExecutionResult{Result: "Error: speech synthesizer returned empty audio", Outcome: agent.ToolExecutionOutcomeError}
	}
	if name == "tts" && c.speechPlayer != nil {
		if playErr := c.speechPlayer.PlaySpeech(c.parentContext(), wav); playErr != nil {
			return agent.ToolExecutionResult{Result: "Error: " + playErr.Error(), Outcome: agent.ToolExecutionOutcomeError}
		}
		return agent.ToolExecutionResult{Result: "Speech synthesized and played.", Outcome: agent.ToolExecutionOutcomeOK}
	}
	if strings.TrimSpace(c.workspace) == "" {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Speech rendered (%d bytes). No workspace to save the artifact.", len(wav)), Outcome: agent.ToolExecutionOutcomeOK}
	}
	outPath := filepath.Join(c.workspace, fmt.Sprintf("tts-render-%d.wav", time.Now().UnixNano()))
	if err := os.WriteFile(outPath, wav, 0o644); err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: "Rendered speech artifact: " + outPath, Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) executeScopedAgentFileTool(args map[string]interface{}, keys []string, fn func(map[string]interface{}) string) agent.ToolExecutionResult {
	scoped, err := c.scopeToolPaths(args, keys...)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	return toolTextResult(fn(scoped))
}

func (c *coreAgentCallbacks) executeScopedSearchTool(args map[string]interface{}, fn func(map[string]interface{}) agent.SearchToolResult) agent.ToolExecutionResult {
	scoped := cloneToolArgs(args)
	rawPath := firstNonEmpty(stringArg(args, "path"), stringArg(args, "file_path"))
	if rawPath == "" {
		if strings.TrimSpace(c.workspace) == "" {
			return agent.ToolExecutionResult{Result: "Error: no workspace configured for this instance", Outcome: agent.ToolExecutionOutcomeError}
		}
		scoped["path"] = c.workspace
	} else {
		absPath, err := c.resolveWorkspacePath(rawPath)
		if err != nil {
			return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
		}
		scoped["path"] = absPath
	}
	result := fn(scoped)
	outcome := agent.ToolExecutionOutcomeOK
	if result.Outcome == agent.SearchToolOutcomeError {
		outcome = agent.ToolExecutionOutcomeError
	}
	return agent.ToolExecutionResult{Result: result.Text, Outcome: outcome}
}

func (c *coreAgentCallbacks) executeWriteExcel(args map[string]interface{}) agent.ToolExecutionResult {
	scoped, err := c.scopeToolPaths(args, "file_path", "path")
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	text, writeErr := agent.WriteExcelDetailed(scoped)
	if writeErr != nil {
		return agent.ToolExecutionResult{Result: text, Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: text, Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) scopeToolPaths(args map[string]interface{}, keys ...string) (map[string]interface{}, error) {
	scoped := cloneToolArgs(args)
	for _, key := range keys {
		raw := stringArg(args, key)
		if raw == "" {
			continue
		}
		absPath, err := c.resolveWorkspacePath(raw)
		if err != nil {
			return nil, err
		}
		scoped[key] = absPath
	}
	return scoped, nil
}

func (c *coreAgentCallbacks) executeOffice(args map[string]interface{}) agent.ToolExecutionResult {
	action := strings.TrimSpace(strings.ToLower(stringArg(args, "action")))
	switch action {
	case "read_document", "read_doc", "read_docx", "read_pdf":
		return c.executeReadDocument(args)
	case "read_excel":
		return c.executeScopedAgentFileTool(args, []string{"file_path", "path"}, agent.ToolReadExcel)
	case "write_excel":
		return c.executeWriteExcel(args)
	case "read_pptx":
		return c.executeScopedAgentFileTool(args, []string{"file_path", "path"}, agent.ToolReadPPTX)
	case "generate_pdf":
		return c.executeGeneratePDF(args)
	default:
		return agent.ToolExecutionResult{Result: "Error: unknown office action " + action, Outcome: agent.ToolExecutionOutcomeError}
	}
}

func (c *coreAgentCallbacks) executeReadDocument(args map[string]interface{}) agent.ToolExecutionResult {
	rawPath := firstNonEmpty(stringArg(args, "file_path"), stringArg(args, "path"))
	filePath, err := c.resolveWorkspacePath(rawPath)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	scoped := cloneToolArgs(args)
	scoped["file_path"] = filePath
	// Bind the trusted request config at this final execution boundary so one
	// user's OfficeRead policy cannot affect another principal in-process.
	return readDocumentToolResult(agent.ToolReadDocumentWithOfficeReadConfigAndContext(scoped, officeReadConfigFromAppConfig(c.appCfg), c.llmCfg.EffectiveContextTokens()))
}

func (c *coreAgentCallbacks) executeGeneratePDF(args map[string]interface{}) agent.ToolExecutionResult {
	content := strings.TrimSpace(stringArg(args, "content"))
	if content == "" {
		return agent.ToolExecutionResult{Result: "Error: missing content", Outcome: agent.ToolExecutionOutcomeError}
	}
	pdf, err := reviewedHostRenderGeneratedPDF(c.reviewedHostPDFRenderer, content)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	if len(pdf) == 0 {
		return agent.ToolExecutionResult{Result: "Error: PDF renderer returned empty output", Outcome: agent.ToolExecutionOutcomeError}
	}
	title := strings.TrimSpace(stringArg(args, "title"))
	name := sanitizePDFFileStem(title)
	if title == "" {
		name = fmt.Sprintf("document-%d", time.Now().UnixNano())
	}
	outPath, err := c.resolveWorkspacePath(name + ".pdf")
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	if err := os.WriteFile(outPath, pdf, 0o644); err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: "Wrote PDF " + outPath, Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) executeDownloadFile(args map[string]interface{}) agent.ToolExecutionResult {
	if strings.TrimSpace(c.workspace) == "" {
		return agent.ToolExecutionResult{Result: "Error: no workspace configured for this instance", Outcome: agent.ToolExecutionOutcomeError}
	}
	rawURL := stringArg(args, "url")
	if rawURL == "" {
		return agent.ToolExecutionResult{Result: "Error: missing url parameter", Outcome: agent.ToolExecutionOutcomeError}
	}
	savePath := firstNonEmpty(stringArg(args, "save_path"), stringArg(args, "output"))
	if savePath == "" {
		savePath = downloadFileNameFromURL(rawURL)
	}
	absPath, err := c.resolveWorkspacePath(savePath)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	opts := &websearch.FetchOptions{
		SavePath: absPath,
		SaveRoot: c.workspace,
		MaxBytes: 25 * 1024 * 1024,
		TimeoutS: 60,
	}
	result, err := websearch.FetchCtx(c.parentContext(), rawURL, opts)
	if err != nil {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: download failed: %v", err), Outcome: agent.ToolExecutionOutcomeError}
	}
	saved := absPath
	if result != nil && strings.TrimSpace(result.SavedTo) != "" {
		saved = result.SavedTo
		if err := ensurePathWithinBase(saved, c.workspace); err != nil {
			return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
		}
	}
	return agent.ToolExecutionResult{Result: "Downloaded to " + saved, Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) executeListMCPTools() agent.ToolExecutionResult {
	if c.mcpProvider == nil {
		return agent.ToolExecutionResult{Result: "Error: MCP provider is not initialized", Outcome: agent.ToolExecutionOutcomeError}
	}
	tools := c.mcpProvider.ListAvailableTools(c.parentContext(), c.principal)
	if len(tools) == 0 {
		return agent.ToolExecutionResult{Result: "No MCP tools are ready.", Outcome: agent.ToolExecutionOutcomeOK}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Ready MCP tools (%d):\n", len(tools)))
	for _, tool := range tools {
		b.WriteString(fmt.Sprintf("  - %s / %s", tool.ServerName, tool.ToolName))
		if strings.TrimSpace(tool.Description) != "" {
			b.WriteString(": ")
			b.WriteString(tool.Description)
		}
		b.WriteByte('\n')
	}
	return agent.ToolExecutionResult{Result: b.String(), Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) executeImportMCPServers(args map[string]interface{}) agent.ToolExecutionResult {
	raw := mcpJSONConfigArg(args)
	if raw == "" {
		return agent.ToolExecutionResult{Result: "Error: missing json_config", Outcome: agent.ToolExecutionOutcomeError}
	}
	inputs, err := parseMCPServerCreateInputs(raw, stringArg(args, "target"))
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	importer, ok := c.mcpProvider.(mcpJSONImporter)
	if !ok || importer == nil {
		names := make([]string, 0, len(inputs))
		for _, in := range inputs {
			names = append(names, in.Name)
		}
		return agent.ToolExecutionResult{
			Result:  "Error: parsed MCP servers (" + strings.Join(names, ", ") + ") but this host cannot persist them. Use the Skills/MCP workbench or POST /api/v1/mcp/servers.",
			Outcome: agent.ToolExecutionOutcomeError,
		}
	}
	created, err := importer.ImportMCPServers(c.parentContext(), c.principal, inputs)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{
		Result:  fmt.Sprintf("Imported MCP servers (%d): %s", len(created), strings.Join(created, ", ")),
		Outcome: agent.ToolExecutionOutcomeOK,
	}
}

func (c *coreAgentCallbacks) executeScreenshot(args map[string]interface{}) agent.ToolExecutionResult {
	if display := intArg(args, "display", 0); display > 1 || display < 0 {
		return agent.ToolExecutionResult{Result: "Error: this host can only capture the primary display", Outcome: agent.ToolExecutionOutcomeError}
	}
	if !reviewedHostDesktopCapturerReady(c.desktopCapturer) {
		return agent.ToolExecutionResult{Result: "Error: desktop screenshot is unavailable on this headless host", Outcome: agent.ToolExecutionOutcomeError}
	}
	png, err := c.desktopCapturer.CapturePrimary(c.parentContext())
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	if len(png) == 0 {
		return agent.ToolExecutionResult{Result: "Error: desktop capturer returned empty image", Outcome: agent.ToolExecutionOutcomeError}
	}
	if strings.TrimSpace(c.workspace) == "" {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Captured screenshot (%d bytes). No workspace to save the artifact.", len(png)), Outcome: agent.ToolExecutionOutcomeOK}
	}
	outPath := filepath.Join(c.workspace, fmt.Sprintf("screenshot-%d.png", time.Now().UnixNano()))
	if err := os.WriteFile(outPath, png, 0o644); err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: "Saved screenshot " + outPath, Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) executeOpen(args map[string]interface{}) agent.ToolExecutionResult {
	target := firstNonEmpty(stringArg(args, "target"), stringArg(args, "url"), stringArg(args, "path"))
	if target == "" {
		return agent.ToolExecutionResult{Result: "Error: missing target", Outcome: agent.ToolExecutionOutcomeError}
	}
	if path, ok := fileURLPath(target); ok {
		target = path
	} else if looksLikeOpenURL(target) {
		if !reviewedHostURLLauncherReady(c.urlLauncher) {
			return agent.ToolExecutionResult{Result: "Error: OS open is unavailable on this headless host", Outcome: agent.ToolExecutionOutcomeError}
		}
		out, err := c.OpenReviewedHostURL(c.parentContext(), c.principal, target)
		if err != nil {
			return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
		}
		return agent.ToolExecutionResult{Result: out, Outcome: agent.ToolExecutionOutcomeOK}
	}
	if !reviewedHostDocumentLauncherReady(c.documentLauncher) {
		return agent.ToolExecutionResult{Result: "Error: OS open is unavailable on this headless host", Outcome: agent.ToolExecutionOutcomeError}
	}
	out, err := c.OpenReviewedHostDocument(c.parentContext(), c.principal, target)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: out, Outcome: agent.ToolExecutionOutcomeOK}
}

func looksLikeOpenURL(target string) bool {
	lower := strings.ToLower(strings.TrimSpace(target))
	if strings.HasPrefix(lower, "file:") {
		return false
	}
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:")
}

func fileURLPath(target string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil || parsed == nil || !strings.EqualFold(parsed.Scheme, "file") {
		return "", false
	}
	path := parsed.Path
	if parsed.Opaque != "" && path == "" {
		path = parsed.Opaque
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		path = "//" + parsed.Host + path
	}
	if len(path) >= 3 && path[0] == '/' && ((path[1] >= 'A' && path[1] <= 'Z') || (path[1] >= 'a' && path[1] <= 'z')) && path[2] == ':' {
		path = path[1:]
	}
	if path == "" {
		return "", false
	}
	return filepath.FromSlash(path), true
}

func (c *coreAgentCallbacks) executeEditLines(args map[string]interface{}) agent.ToolExecutionResult {
	absPath, err := c.resolveWorkspacePath(firstNonEmpty(stringArg(args, "path"), stringArg(args, "file_path"), stringArg(args, "file")))
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	op := normalizeEditLinesOp(firstNonEmpty(stringArg(args, "operation"), stringArg(args, "op"), stringArg(args, "action")))
	switch op {
	case coretool.EditLineReplace, coretool.EditLineInsert, coretool.EditLineDelete:
	default:
		return agent.ToolExecutionResult{Result: "Error: operation must be replace, insert, or delete", Outcome: agent.ToolExecutionOutcomeError}
	}
	start := intArg(args, "start_line", 0)
	if _, ok := args["start_line"]; !ok {
		start = intArg(args, "start", 0)
	}
	end := intArg(args, "end_line", start)
	if _, ok := args["end_line"]; !ok {
		if _, hasEnd := args["end"]; hasEnd {
			end = intArg(args, "end", start)
		}
	}
	res, err := coretool.EditFileByLine(absPath, op, start, end, stringArg(args, "content"))
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: fmt.Sprintf("Edited %s (%s lines %d, now %d lines)", absPath, op, start, res.TotalLines), Outcome: agent.ToolExecutionOutcomeOK}
}

func normalizeEditLinesOp(op string) coretool.EditLineOperation {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "insert", "add", "append", "insert_line", "insert_lines":
		return coretool.EditLineInsert
	case "replace", "update", "modify", "replace_line", "replace_lines":
		return coretool.EditLineReplace
	case "delete", "remove", "rm", "delete_line", "delete_lines":
		return coretool.EditLineDelete
	default:
		return coretool.EditLineOperation(strings.ToLower(strings.TrimSpace(op)))
	}
}

func sanitizePDFFileStem(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name = strings.TrimSpace(name[:len(name)-4])
	}
	var b strings.Builder
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`<>:"|?*`, r) {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "document"
	}
	return out
}

func downloadFileNameFromURL(rawURL string) string {
	u := strings.TrimSpace(rawURL)
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	base := filepath.Base(strings.ReplaceAll(u, "\\", "/"))
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == "/" || base == "\\" {
		return "download.bin"
	}
	var b strings.Builder
	for _, r := range base {
		if r < 32 || strings.ContainsRune(`<>:"|?*`, r) {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." {
		return "download.bin"
	}
	return out
}
