package main

// remote_coding_subagent.go implements a RemoteCodingSubAgent that executes
// coding tasks on a remote server via SSH. It mirrors the local CodingSubAgent
// architecture — clean context, minimal tools, independent conversation — but
// all file/shell operations target a remote server.
//
// Tool set (5 SSH-wrapped tools):
//   ssh_read_file   → cat remote file
//   ssh_write_file  → write content to remote file (python pathlib)
//   ssh_edit_file   → python string replace in remote file
//   ssh_bash        → execute command on remote server
//   ssh_list_dir    → ls remote directory
//
// The SubAgent reuses corelib/agent.RunLoop and delegates SSH execution to the
// host IMMessageHandler's existing SSH infrastructure (ensureSSHManager, sshExec).

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// RemoteCodingSubAgent executes coding tasks on a remote server via SSH.
type RemoteCodingSubAgent struct {
	handler    *IMMessageHandler
	cfg        corelib.MaclawLLMConfig
	httpClient *http.Client

	// Remote server context
	sessionID  string // SSH session ID (already connected)
	workDir    string // remote working directory
	projectDir string // remote project directory (within workDir)

	// Callbacks
	onToken    func(string)
	onProgress func(string)

	// Cancellation
	loopCtx *LoopContext

	// Knowledge stores (optional, nil = gracefully skipped)
	codingKB  *knowledge.CodingKnowledgeStore
	generalKB *knowledge.SQLiteStore
}

// RemoteCodingSubAgentResult is the outcome of a remote task execution.
type RemoteCodingSubAgentResult struct {
	Status     string // "success", "failed", "cancelled"
	Summary    string
	Error      string
	Iterations int
	ToolCalls  int
}

// NewRemoteCodingSubAgent creates a SubAgent bound to an existing SSH session.
func NewRemoteCodingSubAgent(
	handler *IMMessageHandler,
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
	sessionID, workDir, projectDir string,
	loopCtx *LoopContext,
) *RemoteCodingSubAgent {
	return &RemoteCodingSubAgent{
		handler:    handler,
		cfg:        cfg,
		httpClient: httpClient,
		sessionID:  sessionID,
		workDir:    workDir,
		projectDir: projectDir,
		loopCtx:    loopCtx,
	}
}

// SetCallbacks configures optional streaming and progress callbacks.
func (r *RemoteCodingSubAgent) SetCallbacks(onToken func(string), onProgress func(string)) {
	r.onToken = onToken
	r.onProgress = onProgress
}

// SetKnowledgeStores configures the coding experience store and general knowledge store.
// Both are optional — nil stores are gracefully skipped.
func (r *RemoteCodingSubAgent) SetKnowledgeStores(codingKB *knowledge.CodingKnowledgeStore, generalKB *knowledge.SQLiteStore) {
	if r == nil {
		return
	}
	r.codingKB = codingKB
	r.generalKB = generalKB
}

// SaveExperience saves an experiment experience to the coding knowledge store.
// Called by the orchestrator after each experiment round completes.
// This accumulates experimental knowledge (what worked, what didn't, why).
func (r *RemoteCodingSubAgent) SaveExperience(exp knowledge.CodingExperience) error {
	if r == nil || r.codingKB == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.codingKB.SaveExperience(ctx, exp)
	if err != nil {
		log.Printf("[remote-subagent] failed to save experience: %v", err)
	}
	return err
}

// ExecuteTask runs a single task on the remote server in a clean context.
func (r *RemoteCodingSubAgent) ExecuteTask(taskDescription, taskContext string) *RemoteCodingSubAgentResult {
	cb := &remoteCodingCallbacks{
		agent:       r,
		task:        taskDescription,
		taskContext: taskContext,
	}

	result := agent.RunLoop(cb, taskDescription, nil, r.httpClient)
	return remoteCodingSubAgentResultFromLoopResult(result)
}

func remoteCodingSubAgentResultFromLoopResult(result agent.LoopResult) *RemoteCodingSubAgentResult {
	if result.Error != "" {
		status := "failed"
		if remoteCodingSubAgentLoopErrorIsCancelled(result.Error) {
			status = "cancelled"
		}
		return &RemoteCodingSubAgentResult{
			Status:     status,
			Error:      result.Error,
			Summary:    result.Text,
			Iterations: result.Iterations,
			ToolCalls:  result.ToolCalls,
		}
	}
	if result.HardExit {
		return &RemoteCodingSubAgentResult{
			Status:     "failed",
			Error:      "remote coding subagent hard exit",
			Summary:    result.Text,
			Iterations: result.Iterations,
			ToolCalls:  result.ToolCalls,
		}
	}
	if result.AskUser != nil {
		return &RemoteCodingSubAgentResult{
			Status:     "failed",
			Error:      "remote coding subagent requires user input",
			Summary:    result.Text,
			Iterations: result.Iterations,
			ToolCalls:  result.ToolCalls,
		}
	}
	if strings.TrimSpace(result.Text) == "" {
		return &RemoteCodingSubAgentResult{
			Status:     "failed",
			Error:      "remote coding subagent returned empty summary",
			Summary:    result.Text,
			Iterations: result.Iterations,
			ToolCalls:  result.ToolCalls,
		}
	}

	return &RemoteCodingSubAgentResult{
		Status:     "success",
		Summary:    result.Text,
		Iterations: result.Iterations,
		ToolCalls:  result.ToolCalls,
	}
}

func remoteCodingSubAgentLoopErrorIsCancelled(errText string) bool {
	lower := strings.ToLower(strings.TrimSpace(errText))
	return lower == "cancelled" || strings.HasPrefix(lower, "cancelled ")
}

// --- LoopCallbacks Implementation ---

type remoteCodingCallbacks struct {
	agent       *RemoteCodingSubAgent
	task        string
	taskContext string
}

func (c *remoteCodingCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	if c == nil || c.agent == nil {
		return corelib.MaclawLLMConfig{}
	}
	return c.agent.cfg
}

func (c *remoteCodingCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(50)
}

func (c *remoteCodingCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	projectDir, workDir, taskContext := "", "", ""
	if c != nil {
		taskContext = c.taskContext
		if c.agent != nil {
			projectDir = c.agent.projectDir
			workDir = c.agent.workDir
		}
	}
	prompt := buildRemoteCodingSystemPrompt(projectDir, workDir, taskContext)

	// Inject knowledge from coding experience store + general knowledge store.
	if sections := c.buildRemoteKnowledgePromptSections(); sections != "" {
		prompt += sections
	}
	return prompt
}

func (c *remoteCodingCallbacks) BuildTools(userText string) []map[string]interface{} {
	tools := remoteCodingToolDefinitions()

	// Append knowledge search tools when stores are available.
	if c != nil && c.agent != nil && c.agent.codingKB != nil {
		tools = append(tools, codingKnowledgeSearchToolDef())
	}
	if c != nil && c.agent != nil && c.agent.generalKB != nil {
		tools = append(tools, knowledgeSearchToolDef())
	}
	return tools
}

func (c *remoteCodingCallbacks) ExecuteTool(name, argsJSON string) string {
	return c.executeRemoteTool(name, argsJSON)
}

func (c *remoteCodingCallbacks) OnToken(delta string) {
	if c != nil && c.agent != nil && c.agent.onToken != nil {
		c.agent.onToken(delta)
	}
}

func (c *remoteCodingCallbacks) OnProgress(text string) {
	if c != nil && c.agent != nil && c.agent.onProgress != nil {
		c.agent.onProgress(text)
	}
}

func (c *remoteCodingCallbacks) OnToolCall(name string) {
	if c != nil && c.agent != nil && c.agent.onProgress != nil {
		// Emit a structured CodingAgentEvent so the frontend renders the same
		// tool activity panel as the local CodingSubAgent.
		event := CodingAgentEvent{
			Version: 1,
			Agent:   codingAgentNameCoding.String(),
			Event:   codingAgentEventKindToolStarted.String(),
			Phase:   codingAgentEventPhaseRunning.String(),
			Detail:  strings.TrimSpace(name),
			Title:   "远程编码",
		}
		emitCodingAgentEvent(c.agent.onProgress, event)
	}
}

func (c *remoteCodingCallbacks) OnToolResult(name string) {}

func (c *remoteCodingCallbacks) ShouldStop() bool {
	if c != nil && c.agent != nil && c.agent.loopCtx != nil {
		return c.agent.loopCtx.IsCancelled()
	}
	return false
}

// LLMRequestContext implements LLMRequestContextProvider for cancellation support.
func (c *remoteCodingCallbacks) LLMRequestContext(iteration int) (context.Context, func(error), error) {
	if c != nil && c.agent != nil && c.agent.loopCtx != nil && c.agent.loopCtx.IsCancelled() {
		return nil, nil, fmt.Errorf("cancelled")
	}
	return context.Background(), func(error) {}, nil
}

// --- Tool Execution ---

func (c *remoteCodingCallbacks) executeRemoteTool(name, argsJSON string) string {
	startedAt := time.Now()
	canonicalName := strings.ToLower(strings.TrimSpace(name))

	// Defer tool_finished event emission — guarantees pairing with tool_started
	// regardless of early returns (parse errors, nil handler, etc.)
	var result string
	defer func() {
		if c != nil && c.agent != nil && c.agent.onProgress != nil {
			duration := time.Since(startedAt)
			outcome := remoteCodingToolOutcome(result)
			event := CodingAgentEvent{
				Version:    1,
				Agent:      codingAgentNameCoding.String(),
				Event:      codingAgentEventKindToolFinished.String(),
				Phase:      codingAgentEventPhaseRunning.String(),
				Detail:     strings.TrimSpace(name),
				Title:      "远程编码",
				Outcome:    outcome,
				DurationMS: duration.Milliseconds(),
			}
			if outcome != "success" {
				summary := result
				if len([]rune(summary)) > 180 {
					summary = string([]rune(summary)[:180]) + "..."
				}
				event.Summary = summary
			}
			emitCodingAgentEvent(c.agent.onProgress, event)
		}
	}()

	normalizedArgsJSON := normalizeCodingSubAgentToolArguments(argsJSON)
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(normalizedArgsJSON), &args); err != nil {
		result = fmt.Sprintf("参数解析失败: %v", err)
		return result
	}
	if applyRemoteCodingSubAgentToolArgumentAliases(canonicalName, args) {
		if data, err := json.Marshal(args); err == nil {
			normalizedArgsJSON = string(data)
		}
	}

	if c == nil || c.agent == nil {
		result = "remote coding subagent: agent unavailable"
		return result
	}

	switch canonicalName {
	case "coding_knowledge_search":
		result = c.executeRemoteCodingKnowledgeSearch(normalizedArgsJSON)
		return result
	case "knowledge_search":
		result = c.executeRemoteKnowledgeSearch(normalizedArgsJSON)
		return result
	}

	if !remoteCodingToolRequiresSSHHandler(canonicalName) {
		result = fmt.Sprintf("unknown tool: %s (supports: ssh_read_file, ssh_write_file, ssh_edit_file, ssh_bash, ssh_list_dir, ssh_check_task, coding_knowledge_search, knowledge_search)", name)
		return result
	}
	if c.agent.handler == nil {
		result = "remote coding subagent: handler unavailable"
		return result
	}

	switch canonicalName {
	case "ssh_read_file":
		result = c.sshReadFile(args)
	case "ssh_write_file":
		result = c.sshWriteFile(args)
	case "ssh_edit_file":
		result = c.sshEditFile(args)
	case "ssh_bash":
		result = c.sshBash(args)
	case "ssh_list_dir":
		result = c.sshListDir(args)
	case "ssh_check_task":
		result = c.sshCheckTask(args)
	}

	return result
}

func remoteCodingToolRequiresSSHHandler(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ssh_read_file", "ssh_write_file", "ssh_edit_file", "ssh_bash", "ssh_list_dir", "ssh_check_task":
		return true
	default:
		return false
	}
}

func remoteCodingToolOutcome(result string) string {
	if remoteCodingToolResultLooksFailed(result) {
		return "failed"
	}
	return "success"
}

func remoteCodingToolResultLooksFailed(result string) bool {
	text := strings.TrimSpace(result)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(text, "错误") || strings.Contains(text, "失败") || strings.Contains(text, "参数解析失败") {
		return true
	}
	for _, pattern := range []string{
		"error:",
		"traceback",
		"exception",
		"panic:",
		"exit status",
		"command exited with code",
		"no such file or directory",
		"command not found",
		"file not found",
		"permission denied",
		"unavailable",
		"unknown tool",
	} {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func (c *remoteCodingCallbacks) sshReadFile(args map[string]interface{}) string {
	path := remoteArgStr(args, "path")
	if path == "" {
		return "错误: 需要 path 参数"
	}
	path = c.resolvePath(path)
	offset := remoteArgInt(args, 0, 0, 1000000, "offset", "start_line", "start")
	limit := remoteArgInt(args, 0, 0, 2000, "limit", "num_lines", "line_count")
	return c.execSSH(remoteReadFileRangePythonCommand(path, offset, limit), 10)
}

func remotePythonCommand(script string) string {
	return "python3 -c " + remoteShellQuote(script)
}

func remoteReadFileRangePythonCommand(path string, offset, limit int) string {
	pathB64 := base64EncodeString(path)
	if offset <= 0 {
		offset = 1
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	script := fmt.Sprintf(strings.Join([]string{
		"import pathlib, base64, sys",
		"p = pathlib.Path(base64.b64decode('%s').decode('utf-8'))",
		"start = %d",
		"limit = %d",
		"try:",
		"    lines = p.read_text(encoding='utf-8').splitlines(True)",
		"except UnicodeDecodeError:",
		"    size = p.stat().st_size",
		"    sys.stdout.write('[remote read_file binary/non-UTF8: %%d bytes; text line range unavailable for offset=%%d limit=%%d]\\n' %% (size, start, limit))",
		"    sys.exit(0)",
		"if start < 1:",
		"    start = 1",
		"begin = start - 1",
		"if begin >= len(lines):",
		"    sys.stdout.write('[remote read_file EOF: offset %%d is beyond file length %%d]\\n' %% (start, len(lines)))",
		"    sys.exit(0)",
		"end = min(len(lines), begin + limit)",
		"for lineno, line in enumerate(lines[begin:end], start=start):",
		"    sys.stdout.write(f'{lineno}\\t{line}')",
		"if end < len(lines):",
		"    sys.stdout.write('\\n[remote read_file truncated: showing lines %%d-%%d of %%d; call again with offset=%%d]\\n' %% (start, end, len(lines), end + 1))",
	}, "\n"), pathB64, offset, limit)
	return remotePythonCommand(script)
}

func (c *remoteCodingCallbacks) sshWriteFile(args map[string]interface{}) string {
	path := remoteArgStr(args, "path")
	content, hasContent := remoteArgRawStr(args, "content")
	if path == "" || !hasContent {
		return "错误: 需要 path 和 content 参数"
	}
	path = c.resolvePath(path)

	// For large content (>32KB), write in chunks to avoid PTY buffer overflow.
	if len(content) > 32*1024 {
		return c.sshWriteFileLarge(path, content)
	}

	// Use base64 encoding embedded directly in Python code — no pipes needed.
	// This is PTY-safe since the entire command is a single python3 -c invocation.
	pyScript := remoteWriteFilePythonCommand(path, content)

	result := c.execSSH(pyScript, 15)
	return remoteWriteFileResult(path, len(content), result, false)
}

func remoteWriteFileResult(path string, contentLen int, commandResult string, chunked bool) string {
	if remoteCodingToolResultLooksFailed(commandResult) || !strings.Contains(commandResult, "OK") {
		return fmt.Sprintf("写入失败: %s", commandResult)
	}
	if chunked {
		return fmt.Sprintf("✅ 已写入 %s (%d bytes, chunked)", path, contentLen)
	}
	return fmt.Sprintf("✅ 已写入 %s (%d bytes)", path, contentLen)
}

func remoteWriteFilePythonCommand(path, content string) string {
	pathB64 := base64EncodeString(path)
	contentB64 := base64EncodeString(content)
	script := fmt.Sprintf(strings.Join([]string{
		"import pathlib, base64",
		"p = pathlib.Path(base64.b64decode('%s').decode('utf-8'))",
		"p.parent.mkdir(parents=True, exist_ok=True)",
		"p.write_bytes(base64.b64decode('%s'))",
		"print('OK')",
	}, "\n"), pathB64, contentB64)
	return remotePythonCommand(script)
}

// sshWriteFileLarge handles files >32KB by writing in base64 chunks.
func (c *remoteCodingCallbacks) sshWriteFileLarge(path, content string) string {
	// Write full base64 to a temp file, then decode it.
	b64 := base64EncodeString(content)
	tmpPath := "/tmp/maclaw_write_" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Write base64 data in chunks of 48KB using append mode.
	chunkSize := 48 * 1024
	for i := 0; i < len(b64); i += chunkSize {
		end := i + chunkSize
		if end > len(b64) {
			end = len(b64)
		}
		chunk := b64[i:end]
		op := ">"
		if i > 0 {
			op = ">>"
		}
		cmd := fmt.Sprintf("echo -n '%s' %s %s", chunk, op, tmpPath)
		result := c.execSSH(cmd, 10)
		if remoteCodingToolResultLooksFailed(result) {
			return fmt.Sprintf("写入失败（分块传输）: %s", result)
		}
	}

	// Decode and move to target path.
	decodeCmd := remoteWriteFileLargeDecodeCommand(path, tmpPath)

	result := c.execSSH(decodeCmd, 15)
	return remoteWriteFileResult(path, len(content), result, true)
}

func remoteWriteFileLargeDecodeCommand(path, tmpPath string) string {
	pathB64 := base64EncodeString(path)
	tmpPathB64 := base64EncodeString(tmpPath)
	script := fmt.Sprintf(strings.Join([]string{
		"import pathlib, base64",
		"p = pathlib.Path(base64.b64decode('%s').decode('utf-8'))",
		"p.parent.mkdir(parents=True, exist_ok=True)",
		"tmp = base64.b64decode('%s').decode('utf-8')",
		"data = base64.b64decode(open(tmp).read())",
		"p.write_bytes(data)",
		"print('OK')",
	}, "\n"), pathB64, tmpPathB64)
	return fmt.Sprintf("%s && rm -f %s", remotePythonCommand(script), remoteShellQuote(tmpPath))
}

func (c *remoteCodingCallbacks) sshEditFile(args map[string]interface{}) string {
	path := remoteArgStr(args, "path")
	oldStr, hasOldStr := remoteArgRawStr(args, "old_str")
	newStr, hasNewStr := remoteArgRawStr(args, "new_str")
	if path == "" || !hasOldStr || oldStr == "" || !hasNewStr {
		return "错误: 需要 path、old_str 和 new_str 参数"
	}
	path = c.resolvePath(path)

	// Use base64 to safely transfer old/new strings without heredoc terminator conflicts.
	pyScript := remoteEditFilePythonCommand(path, oldStr, newStr)

	result := c.execSSH(pyScript, 15)
	return remoteEditFileResult(path, result)
}

func remoteEditFileResult(path string, commandResult string) string {
	if remoteCodingToolResultLooksFailed(commandResult) || !strings.Contains(commandResult, "OK:") {
		return fmt.Sprintf("编辑失败: %s", commandResult)
	}
	return fmt.Sprintf("✅ 已编辑 %s", path)
}

func remoteEditFilePythonCommand(path, oldStr, newStr string) string {
	pathB64 := base64EncodeString(path)
	oldB64 := base64EncodeString(oldStr)
	newB64 := base64EncodeString(newStr)
	script := fmt.Sprintf(strings.Join([]string{
		"import pathlib, base64, sys",
		"p = pathlib.Path(base64.b64decode('%s').decode('utf-8'))",
		"if not p.exists():",
		"    print('ERROR: file not found: ' + str(p))",
		"    sys.exit(0)",
		"text = p.read_text(encoding='utf-8')",
		"old = base64.b64decode('%s').decode('utf-8')",
		"new = base64.b64decode('%s').decode('utf-8')",
		"if old not in text:",
		"    print('ERROR: old_str not found in file')",
		"elif text.count(old) > 1:",
		"    print('ERROR: old_str matches ' + str(text.count(old)) + ' locations (must be unique)')",
		"else:",
		"    p.write_text(text.replace(old, new, 1), encoding='utf-8')",
		"    print('OK: replaced 1 occurrence')",
	}, "\n"), pathB64, oldB64, newB64)
	return remotePythonCommand(script)
}

func (c *remoteCodingCallbacks) sshBash(args map[string]interface{}) string {
	command := remoteArgStr(args, "command")
	if command == "" {
		return "错误: 需要 command 参数"
	}
	workDir := remoteArgStr(args, "working_dir")
	if workDir == "" {
		workDir = c.agent.projectDir
	} else {
		workDir = c.resolvePath(workDir)
	}

	// Prepend cd to working directory
	fullCmd := fmt.Sprintf("cd %s && %s", remoteShellQuote(workDir), command)

	// Long commands → use SSH background task
	if isLongRemoteCommand(command) {
		log.Printf("[remote-subagent] long command detected, using background task: %.80s", command)
		return c.execSSHBackground(fullCmd)
	}

	return c.execSSH(fullCmd, 60)
}

func (c *remoteCodingCallbacks) sshListDir(args map[string]interface{}) string {
	path := remoteArgStr(args, "path")
	if path == "" {
		path = c.agent.projectDir
	} else {
		path = c.resolvePath(path)
	}
	return c.execSSH(fmt.Sprintf("ls -la %s", remoteShellQuote(path)), 10)
}

func (c *remoteCodingCallbacks) sshCheckTask(args map[string]interface{}) string {
	taskID := remoteArgStr(args, "task_id")
	if taskID == "" {
		return "错误: 需要 task_id 参数"
	}
	if c == nil || c.agent == nil || c.agent.handler == nil {
		return "remote coding subagent: handler unavailable"
	}
	// Delegate to the main SSH tool's check_task action.
	h := c.agent.handler
	return h.toolSSH(map[string]interface{}{
		"action":     "check_task",
		"session_id": c.agent.sessionID,
		"task_id":    taskID,
	})
}

// --- SSH Execution Helpers ---

func (c *remoteCodingCallbacks) execSSH(command string, waitSec int) string {
	if c == nil || c.agent == nil || c.agent.handler == nil {
		return "remote coding subagent: handler unavailable"
	}
	h := c.agent.handler
	return h.sshExec(map[string]interface{}{
		"session_id":   c.agent.sessionID,
		"command":      command,
		"wait_seconds": float64(waitSec),
	})
}

func (c *remoteCodingCallbacks) execSSHBackground(command string) string {
	if c == nil || c.agent == nil || c.agent.handler == nil {
		return "remote coding subagent: handler unavailable"
	}
	h := c.agent.handler
	return h.toolSSH(map[string]interface{}{
		"action":     "exec_background",
		"session_id": c.agent.sessionID,
		"command":    command,
	})
}

// resolvePath makes relative paths relative to the project directory.
func (c *remoteCodingCallbacks) resolvePath(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	if c == nil || c.agent == nil || strings.TrimSpace(c.agent.projectDir) == "" {
		return path
	}
	return strings.TrimRight(c.agent.projectDir, "/") + "/" + path
}

// --- System Prompt ---

func buildRemoteCodingSystemPrompt(projectDir, workDir, taskContext string) string {
	var sb strings.Builder
	sb.WriteString("# Remote Coding SubAgent\n\n")
	sb.WriteString("你是一个在远程服务器上执行代码修改和实验的编程 Agent。\n\n")
	sb.WriteString(fmt.Sprintf("## 环境信息\n- 远程项目目录: %s\n- 工作目录: %s\n\n", projectDir, workDir))
	sb.WriteString(`## 可用工具

- ssh_read_file(path, offset?, limit?): 读取远程文件内容；默认读取前 200 行，大文件用 offset/limit 分片读取
- ssh_write_file(path, content): 写入/创建远程文件（自动创建父目录）
- ssh_edit_file(path, old_str, new_str): 精确替换远程文件中的文本（old_str 必须唯一匹配）
- ssh_bash(command, working_dir?): 在远程服务器执行命令（长时间命令自动转后台任务，返回 task_id）
- ssh_check_task(task_id): 查询后台任务状态和日志（训练完成后返回 EXIT: 0）
- ssh_list_dir(path?): 列出远程目录内容

参数兼容: 路径可用 file/file_path/filename/target_path 代替 path；ssh_edit_file 也接受 old_string/old_content/find/search -> old_str 和 new_string/new_content/replace/replacement -> new_str；ssh_bash 接受 cwd/work_dir -> working_dir；ssh_check_task 接受 id/task -> task_id。

## 工作规范

1. 修改文件前先 ssh_read_file 确认当前内容
2. 使用 ssh_edit_file 做精确修改（小改动）或 ssh_write_file 重写文件（大改动）
3. 修改后再次 ssh_read_file 读取关键片段，确认远程文件确实变成预期内容
4. 修改后用 ssh_bash 运行匹配任务的验证命令（如 "python3 -c 'import module'"、pytest/go test/npm test 等）
5. 路径可以是相对路径（相对于项目目录）或绝对路径
6. ssh_read_file 默认只返回前 200 行；继续读取时用返回提示里的 offset 分片查看
7. 长时间训练命令会自动作为后台任务运行，返回 task_id
8. 最终回复必须说明：修改/创建的文件、实际运行的验证命令及结果、剩余风险或未验证项

## 严禁行为
- 不要删除项目根目录或关键系统文件
- 不要修改 /etc、/usr 等系统目录
- 不要在未读取文件的情况下盲目覆盖
`)
	if taskContext != "" {
		sb.WriteString("\n## 任务上下文\n\n")
		sb.WriteString(taskContext)
		sb.WriteString("\n")
	}
	return sb.String()
}

// --- Tool Definitions ---

func remoteCodingToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		buildRemoteToolDef("ssh_read_file", "读取远程服务器上的文件内容",
			map[string]interface{}{
				"path":   map[string]interface{}{"type": "string", "description": "文件路径（相对于项目目录或绝对路径；也接受 file/file_path/filename/target_path）"},
				"offset": map[string]interface{}{"type": "number", "description": "可选，1-based 起始行；也接受 start/start_line"},
				"limit":  map[string]interface{}{"type": "number", "description": "可选，最多读取的行数；默认 200，也接受 num_lines/line_count，最大 2000"},
			}, []string{"path"}),
		buildRemoteToolDef("ssh_write_file", "写入内容到远程文件（自动创建父目录）",
			map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "文件路径（也接受 file/file_path/filename/target_path）"},
				"content": map[string]interface{}{"type": "string", "description": "文件内容"},
			}, []string{"path", "content"}),
		buildRemoteToolDef("ssh_edit_file", "精确替换远程文件中的文本（old_str 必须在文件中唯一匹配；也接受 old_string/old_content/find/search 和 new_string/new_content/replace/replacement）",
			map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "文件路径（也接受 file/file_path/filename/target_path）"},
				"old_str": map[string]interface{}{"type": "string", "description": "要被替换的原始文本（也接受 old_string/old_content/find/search）"},
				"new_str": map[string]interface{}{"type": "string", "description": "替换后的新文本（也接受 new_string/new_content/replace/replacement）"},
			}, []string{"path", "old_str", "new_str"}),
		buildRemoteToolDef("ssh_bash", "在远程服务器上执行 shell 命令（长时间命令自动转后台任务）",
			map[string]interface{}{
				"command":     map[string]interface{}{"type": "string", "description": "要执行的命令"},
				"working_dir": map[string]interface{}{"type": "string", "description": "工作目录（默认项目目录；也接受 cwd/work_dir）"},
			}, []string{"command"}),
		buildRemoteToolDef("ssh_list_dir", "列出远程目录内容",
			map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "目录路径（默认项目目录；也接受 dir/directory/root/file/file_path/filename/target_path）"},
			}, nil),
		buildRemoteToolDef("ssh_check_task", "查询后台任务状态（训练/下载等长时间任务）。返回运行状态和日志尾部",
			map[string]interface{}{
				"task_id": map[string]interface{}{"type": "string", "description": "后台任务 ID（由 ssh_bash 长命令自动返回；也接受 id/task）"},
			}, []string{"task_id"}),
	}
}

func buildRemoteToolDef(name, description string, properties map[string]interface{}, required []string) map[string]interface{} {
	params := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		params["required"] = required
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": description,
			"parameters":  params,
		},
	}
}

// --- Utility ---

func remoteArgStr(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func remoteArgRawStr(args map[string]interface{}, key string) (string, bool) {
	v, ok := args[key].(string)
	return v, ok
}

func remoteArgInt(args map[string]interface{}, defaultValue, minValue, maxValue int, keys ...string) int {
	value := defaultValue
	for _, key := range keys {
		raw, ok := args[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case int:
			value = v
		case int64:
			value = int(v)
		case float64:
			value = int(v)
		case json.Number:
			if n, err := v.Int64(); err == nil {
				value = int(n)
			}
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				value = n
			}
		}
		break
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func applyRemoteCodingSubAgentToolArgumentAliases(name string, args map[string]interface{}) bool {
	if len(args) == 0 {
		return false
	}
	changed := false
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ssh_read_file", "ssh_write_file":
		changed = applyCodingSubAgentPathArgumentAliases(args) || changed
	case "ssh_edit_file":
		changed = applyCodingSubAgentPathArgumentAliases(args) || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "old_string", "old_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "old_content", "old_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "find", "old_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "search", "old_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "new_string", "new_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "new_content", "new_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "replace", "new_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "replacement", "new_str") || changed
	case "ssh_bash":
		changed = applyCodingSubAgentToolArgumentAlias(args, "work_dir", "working_dir") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "cwd", "working_dir") || changed
	case "ssh_list_dir":
		changed = applyCodingSubAgentPathArgumentAliases(args) || changed
		changed = applyCodingSubAgentDirectoryArgumentAliases(args) || changed
	case "ssh_check_task":
		changed = applyCodingSubAgentToolArgumentAlias(args, "task", "task_id") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "id", "task_id") || changed
	case "coding_knowledge_search", "knowledge_search":
		changed = applyCodingSubAgentQueryArgumentAliases(args) || changed
	}
	return changed
}

func remoteShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func base64EncodeString(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func isLongRemoteCommand(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	if lower == "" || remoteCommandIsPythonOneLiner(lower) {
		return false
	}
	for _, pattern := range []string{
		"nohup ",
		"screen ",
		"tmux ",
		"pip install",
		"conda install",
		"apt install",
		"apt-get install",
		"git clone ",
		"wget ",
		"curl -o",
		"curl -l",
		"docker build",
		"docker pull",
	} {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	if strings.Contains(lower, "git pull") {
		return true
	}
	if strings.HasPrefix(lower, "make ") || strings.Contains(lower, " make ") {
		return remoteCommandHasLongTrainingIntent(lower) || strings.Contains(lower, " build") || strings.Contains(lower, " install")
	}
	if strings.Contains(lower, "cmake ") {
		return strings.Contains(lower, "--build") || strings.Contains(lower, " --install")
	}
	if (strings.Contains(lower, "python") || strings.Contains(lower, "bash ") || strings.Contains(lower, "sh ")) &&
		(strings.Contains(lower, ".py") || strings.Contains(lower, ".sh")) {
		return remoteCommandHasLongTrainingIntent(lower)
	}
	return remoteCommandHasLongTrainingIntent(lower) && remoteCommandLooksExplicitlyLongRunning(lower)
}

func remoteCommandIsPythonOneLiner(lower string) bool {
	return strings.Contains(lower, "python3 -c") ||
		strings.Contains(lower, "python -c") ||
		strings.Contains(lower, "python3 - <<") ||
		strings.Contains(lower, "python - <<")
}

func remoteCommandLooksExplicitlyLongRunning(lower string) bool {
	return strings.Contains(lower, "train ") ||
		strings.Contains(lower, " train") ||
		strings.Contains(lower, "epoch") ||
		strings.Contains(lower, "--epochs") ||
		strings.Contains(lower, "--max-steps")
}

func remoteCommandHasLongTrainingIntent(lower string) bool {
	tokens := remoteCommandTokens(lower)
	for i, token := range tokens {
		if !remoteCommandTokenIsTrainingIntent(token) {
			continue
		}
		if i > 0 && remoteCommandTokenSuppressesTrainingIntent(tokens[i-1]) {
			continue
		}
		return true
	}
	return false
}

func remoteCommandTokenIsTrainingIntent(token string) bool {
	switch token {
	case "train", "training", "fit", "epoch", "epochs", "finetune", "finetuning":
		return true
	default:
		return false
	}
}

func remoteCommandTokenSuppressesTrainingIntent(token string) bool {
	switch token {
	case "check", "test", "tests", "validate", "validation", "lint", "verify":
		return true
	default:
		return false
	}
}

func remoteCommandTokens(command string) []string {
	return strings.FieldsFunc(command, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})
}

// --- Knowledge Store Integration ---

// buildRemoteKnowledgePromptSections generates knowledge-related system prompt
// sections. Returns empty string if no relevant knowledge found.
func (c *remoteCodingCallbacks) buildRemoteKnowledgePromptSections() string {
	if c == nil || c.agent == nil {
		return ""
	}
	if c.ShouldStop() {
		return ""
	}

	var b strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	taskQuery := c.task
	if taskQuery == "" {
		return ""
	}

	// 1. Coding knowledge (experiences)
	if c.agent.codingKB != nil {
		pack, err := c.agent.codingKB.ContextPackForTask(ctx, knowledge.CodingContextPackOptions{
			Query:    taskQuery,
			Language: "python", // paper reproduction is predominantly Python
			MaxItems: 4,
			MaxChars: 1500,
		})
		if err == nil && len(pack.Items) > 0 {
			b.WriteString("\n## 相关编码经验（来自编程知识库）\n")
			b.WriteString("以下经验来自历史编码任务积累，供参考：\n")
			for _, item := range pack.Items {
				text := item.Text
				if len([]rune(text)) > 300 {
					text = string([]rune(text)[:300]) + "..."
				}
				b.WriteString(fmt.Sprintf("- **%s**: %s\n", item.Title, text))
			}
		}
	}

	// 2. General knowledge (project docs)
	if c.agent.generalKB != nil {
		searchOpts := knowledge.SearchOptions{
			Query: taskQuery,
			Limit: 10,
		}
		pack, err := c.agent.generalKB.ContextPack(ctx, knowledge.ContextPackOptions{
			SearchOptions: searchOpts,
			MaxItems:      3,
			MaxChars:      2000,
		})
		if err == nil && len(pack.Items) > 0 {
			b.WriteString("\n## 项目参考资料（来自通用知识库）\n")
			b.WriteString("以下是与当前任务相关的项目文档：\n")
			b.WriteString(knowledge.FormatContextPackForLLM(pack))
		}
	}

	return b.String()
}

// executeRemoteCodingKnowledgeSearch handles coding_knowledge_search tool call.
func (c *remoteCodingCallbacks) executeRemoteCodingKnowledgeSearch(argsJSON string) string {
	if c.agent.codingKB == nil {
		return "编程知识库未配置。暂无可用的编码经验。"
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}
	query, _ := args["query"].(string)
	if query == "" {
		return "Error: query parameter is required"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	experiences, err := c.agent.codingKB.SearchExperiences(ctx, knowledge.CodingSearchOptions{
		Query:    query,
		Language: "python",
		Status:   []string{knowledge.CodingStatusActive, knowledge.CodingStatusVerified},
		Limit:    5,
	})
	if err != nil {
		return fmt.Sprintf("编程知识库搜索失败: %v", err)
	}
	if len(experiences) == 0 {
		return fmt.Sprintf("未找到与 %q 相关的编码经验。", query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条相关编码经验：\n\n", len(experiences)))
	for i, exp := range experiences {
		b.WriteString(fmt.Sprintf("%d. **%s** [%s/%s] (置信度: %.1f)\n", i+1, exp.Title, exp.Scope, exp.Category, exp.Confidence))
		if exp.TriggerCondition != "" {
			b.WriteString(fmt.Sprintf("   触发条件: %s\n", exp.TriggerCondition))
		}
		if exp.Content != "" {
			content := exp.Content
			if len([]rune(content)) > 400 {
				content = string([]rune(content)[:400]) + "..."
			}
			b.WriteString(fmt.Sprintf("   %s\n", content))
		}
		if exp.CodeSnippet != "" {
			snippet := exp.CodeSnippet
			if len([]rune(snippet)) > 300 {
				snippet = string([]rune(snippet)[:300]) + "..."
			}
			b.WriteString(fmt.Sprintf("   代码片段:\n   ```\n   %s\n   ```\n", snippet))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// executeRemoteKnowledgeSearch handles knowledge_search tool call (general knowledge).
func (c *remoteCodingCallbacks) executeRemoteKnowledgeSearch(argsJSON string) string {
	if c.agent.generalKB == nil {
		return "项目知识库未配置。请使用 ssh_read_file 直接查看项目文件获取信息。"
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}
	query, _ := args["query"].(string)
	if query == "" {
		return "Error: query parameter is required"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	results, err := c.agent.generalKB.Search(ctx, knowledge.SearchOptions{
		Query: query,
		Limit: 5,
	})
	if err != nil {
		return fmt.Sprintf("项目知识库搜索失败: %v", err)
	}
	if len(results) == 0 {
		return fmt.Sprintf("未找到与 %q 相关的项目资料。", query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条相关项目资料：\n\n", len(results)))
	for i, r := range results {
		source := r.Source.Title
		if source == "" {
			source = r.Source.RelativePath
		}
		if source == "" {
			source = r.Source.URI
		}
		text := remoteKnowledgeSnippet(r)
		b.WriteString(fmt.Sprintf("%d. [%.1f] **%s**\n   %s\n\n", i+1, r.Score, source, text))
	}
	return b.String()
}

func remoteKnowledgeSnippet(r knowledge.SearchResult) string {
	text := knowledge.BestContentText(r)
	if text == "" {
		return "(no content)"
	}
	if len([]rune(text)) > 300 {
		text = string([]rune(text)[:300]) + "..."
	}
	return text
}
