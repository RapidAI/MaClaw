package agentservice

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/database"
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
			Name:        "database",
			Description: database.ToolDescription(),
			// A database manager exists for every authenticated service session,
			// including sessions with no configured profiles. Keeping the tool
			// visible makes list_connections and configuration remediation
			// discoverable instead of turning an empty profile set into unknown
			// tool drift. Direct callback tests may omit the manager; execution
			// then returns the same stable initialization error.
			Enabled:    true,
			Parameters: database.ToolParameters(),
		},
		{
			Name:        "database_query",
			Description: database.ToolDescriptionReadOnly(),
			Enabled:     true,
			Parameters:  database.ToolParameters(),
		},
		specFromCoreTool("manage_skill", "", c.skillProvider != nil, func() string {
			if c.skillProvider == nil {
				return "skill system is not configured"
			}
			return ""
		}()),
		specFromCoreTool("goal", "", c.goals != nil, func() string {
			if c.goals == nil {
				return "goal store is not initialized"
			}
			return ""
		}()),
		specFromCoreTool("delegate_task", "Delegate a task to a bound child agent and wait for the finished result. coding_workflow runs the shared coding runtime; help answers product questions.", c.canDelegateCodingWorkflow() || c.delegateSubtask != nil, func() string {
			if !c.canDelegateCodingWorkflow() && c.delegateSubtask == nil {
				return "delegate_task host adapter is not initialized"
			}
			return ""
		}()),
		specFromCoreTool("asr", "", reviewedHostSpeechReady(c.speechTranscriber), func() string {
			if !reviewedHostSpeechReady(c.speechTranscriber) {
				return "speech transcriber is not initialized"
			}
			return ""
		}()),
		specFromCoreTool("tts", "Synthesize speech from text. Headless hosts render an audio artifact; desktop/IM hosts may also play or send it.", reviewedHostSpeechSynthesizerReady(c.speechSynthesizer), func() string {
			if !reviewedHostSpeechSynthesizerReady(c.speechSynthesizer) {
				return "speech synthesizer is not initialized"
			}
			return ""
		}()),
		specFromCoreTool("tts_render", "Render speech as a workspace audio artifact. Does not play or send.", reviewedHostSpeechSynthesizerReady(c.speechSynthesizer), func() string {
			if !reviewedHostSpeechSynthesizerReady(c.speechSynthesizer) {
				return "speech synthesizer is not initialized"
			}
			return ""
		}()),
		specFromCoreTool("FileRead", "", workspaceOK, workspaceReason),
		specFromCoreTool("ripgrep", "", workspaceOK, workspaceReason),
		specFromCoreTool("Glob", "", workspaceOK, workspaceReason),
		specFromCoreTool("edit_lines", "", workspaceOK, workspaceReason),
		specFromCoreTool("read_excel", "", workspaceOK, workspaceReason),
		specFromCoreTool("write_excel", "", workspaceOK, workspaceReason),
		specFromCoreTool("read_pptx", "", workspaceOK, workspaceReason),
		specFromCoreTool("office", "Office/PDF/text document tool. action: read_document/read_excel/write_excel/read_pptx/write_pptx/generate_pdf.", workspaceOK, workspaceReason),
		specFromCoreTool("generate_pdf", "Render Markdown content to a PDF in the instance workspace.", workspaceOK, workspaceReason),
		specFromCoreTool("download_file", "", workspaceOK, workspaceReason),
		specFromCoreTool("list_mcp_tools", "List ready MCP servers and their tools for this user.", c.mcpProvider != nil, func() string {
			if c.mcpProvider == nil {
				return "MCP provider is not initialized"
			}
			return ""
		}()),
		specFromCoreTool("import_mcp_servers", "Import MCP servers from JSON into this user's config. Accepts {\"mcpServers\":{...}} or MaClaw create entries.", c.canImportMCP(), func() string {
			if !c.canImportMCP() {
				return "MCP import persistence is not configured"
			}
			return ""
		}()),
		specFromCoreTool("screenshot", "Capture the operator desktop. Unavailable on a headless host without a display adapter.", reviewedHostDesktopCapturerReady(c.desktopCapturer), func() string {
			if !reviewedHostDesktopCapturerReady(c.desktopCapturer) {
				return "desktop screenshot is unavailable on this headless host"
			}
			return ""
		}()),
		specFromCoreTool("open", "Open a file or URL with the host default handler. Desktop-display-only on hosts without a launcher.", reviewedHostURLLauncherReady(c.urlLauncher) || reviewedHostDocumentLauncherReady(c.documentLauncher), func() string {
			if !reviewedHostURLLauncherReady(c.urlLauncher) && !reviewedHostDocumentLauncherReady(c.documentLauncher) {
				return "OS open is unavailable on this headless host"
			}
			return ""
		}()),
	}
}

func (c *coreAgentCallbacks) executeSharedHostTool(name string, args map[string]interface{}) (agent.ToolExecutionResult, bool) {
	switch strings.TrimSpace(name) {
	case "database", "database_query":
		if c == nil || c.databaseManager == nil {
			return toolTextResult("数据库连接工具未初始化。请先配置数据源 profile。"), true
		}
		if name == "database_query" {
			if msg, refused := database.RefuseWriteForReadOnlyTool(args); refused {
				return toolTextResult(msg), true
			}
		}
		// Bind connection IDs to the authenticated principal/session through the
		// trusted request context; transport-private fields never enter model
		// arguments or the shared tool schema.
		// Managed semantic turns carry a stable task operation scope. Opt the
		// database manager into the strict query-success gate only for that
		// governed surface; legacy turns remain on the compatibility path.
		c.databaseManager.SetStrictOperationGate(c.dynamicSemanticManaged)
		scope := database.RequestScope{
			OwnerID: memoryOwnerIDForPrincipal(c.principal), SessionID: strings.TrimSpace(c.session.ID),
		}
		if c.dynamicSemanticManaged {
			scope.OperationID = strings.TrimSpace(c.dynamicOperationScope)
		}
		execCtx := database.WithRequestScope(c.ctx, scope)
		if database.ContextHasApproval(c.ctx) {
			execCtx = c.ctx
			execCtx = database.WithRequestScope(execCtx, scope)
		}
		return toolTextResult(database.HandleTool(execCtx, c.databaseManager, args)), true
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
		return c.executeListMCPTools(args), true
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
	return agentruntime.ToolTextResult(out)
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
	return agentruntime.ToolTextFailure(out)
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

func (c *coreAgentCallbacks) executeWritePPTX(args map[string]interface{}) agent.ToolExecutionResult {
	scoped, err := c.scopeToolPaths(args, "file_path", "path")
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	text, writeErr := agent.WritePPTXDetailed(scoped)
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
	case "write_pptx", "generate_pptx":
		return c.executeWritePPTX(args)
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
	result, err := websearch.FetchWithStrategyCtx(c.parentContext(), rawURL, opts, c.webSearchStrategy())
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

func (c *coreAgentCallbacks) executeListMCPTools(args map[string]interface{}) agent.ToolExecutionResult {
	if c.mcpProvider == nil {
		return agent.ToolExecutionResult{Result: "Error: MCP provider is not initialized", Outcome: agent.ToolExecutionOutcomeError}
	}
	query := strings.ToLower(strings.TrimSpace(stringArg(args, "query")))
	serverID := strings.TrimSpace(stringArg(args, "server_id"))
	tools := c.mcpProvider.ListAvailableTools(c.parentContext(), c.principal)
	matched := 0
	var b strings.Builder
	for _, tool := range tools {
		if serverID != "" && !strings.EqualFold(tool.ServerID, serverID) && !strings.EqualFold(tool.ServerName, serverID) {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(strings.TrimSpace(tool.ServerName + " " + tool.ServerID + " " + tool.ToolName + " " + tool.Description))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		matched++
		b.WriteString(fmt.Sprintf("  - %s / %s", tool.ServerName, tool.ToolName))
		if strings.TrimSpace(tool.Description) != "" {
			b.WriteString(": ")
			b.WriteString(tool.Description)
		}
		b.WriteByte('\n')
	}
	if matched == 0 {
		if len(tools) == 0 {
			return agent.ToolExecutionResult{Result: "No MCP tools are ready.", Outcome: agent.ToolExecutionOutcomeOK}
		}
		return agent.ToolExecutionResult{Result: "No MCP tools matched the filter.", Outcome: agent.ToolExecutionOutcomeOK}
	}
	return agent.ToolExecutionResult{Result: fmt.Sprintf("Ready MCP tools (%d):\n%s", matched, b.String()), Outcome: agent.ToolExecutionOutcomeOK}
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
	display := 0
	if raw, ok := args["display"]; ok {
		parsed, err := agentruntime.ParseDesktopDisplayIndex(raw)
		if err != nil {
			return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
		}
		display = parsed
	}
	if !reviewedHostDesktopCapturerReady(c.desktopCapturer) {
		return capabilityUnavailableToolResult("desktop_capture")
	}
	var png []byte
	var err error
	if capturer, ok := c.desktopCapturer.(interface {
		CaptureDisplay(context.Context, int) ([]byte, error)
	}); ok {
		png, err = capturer.CaptureDisplay(c.parentContext(), display)
	} else {
		if display > 1 || display < 0 {
			return agent.ToolExecutionResult{Result: "Error: this host can only capture the primary display", Outcome: agent.ToolExecutionOutcomeError}
		}
		png, err = c.desktopCapturer.CapturePrimary(c.parentContext())
	}
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
			return capabilityUnavailableToolResult("url_launcher")
		}
		out, err := c.OpenReviewedHostURL(c.parentContext(), c.principal, target)
		if err != nil {
			return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
		}
		return agent.ToolExecutionResult{Result: out, Outcome: agent.ToolExecutionOutcomeOK}
	}
	if !reviewedHostDocumentLauncherReady(c.documentLauncher) {
		return capabilityUnavailableToolResult("document_launcher")
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
