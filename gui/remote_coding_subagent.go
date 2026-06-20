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
	"strings"
	"time"

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

	if result.Error != "" {
		return &RemoteCodingSubAgentResult{
			Status:     "failed",
			Error:      result.Error,
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

// --- LoopCallbacks Implementation ---

type remoteCodingCallbacks struct {
	agent       *RemoteCodingSubAgent
	task        string
	taskContext string
}

func (c *remoteCodingCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.agent.cfg
}

func (c *remoteCodingCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(50)
}

func (c *remoteCodingCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	prompt := buildRemoteCodingSystemPrompt(c.agent.projectDir, c.agent.workDir, c.taskContext)

	// Inject knowledge from coding experience store + general knowledge store.
	if sections := c.buildRemoteKnowledgePromptSections(); sections != "" {
		prompt += sections
	}
	return prompt
}

func (c *remoteCodingCallbacks) BuildTools(userText string) []map[string]interface{} {
	tools := remoteCodingToolDefinitions()

	// Append knowledge search tools when stores are available.
	if c.agent.codingKB != nil {
		tools = append(tools, codingKnowledgeSearchToolDef())
	}
	if c.agent.generalKB != nil {
		tools = append(tools, knowledgeSearchToolDef())
	}
	return tools
}

func (c *remoteCodingCallbacks) ExecuteTool(name, argsJSON string) string {
	return c.executeRemoteTool(name, argsJSON)
}

func (c *remoteCodingCallbacks) OnToken(delta string) {
	if c.agent.onToken != nil {
		c.agent.onToken(delta)
	}
}

func (c *remoteCodingCallbacks) OnProgress(text string) {
	if c.agent.onProgress != nil {
		c.agent.onProgress(text)
	}
}

func (c *remoteCodingCallbacks) OnToolCall(name string) {
	if c.agent.onProgress != nil {
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
	if c.agent.loopCtx != nil {
		return c.agent.loopCtx.IsCancelled()
	}
	return false
}

// LLMRequestContext implements LLMRequestContextProvider for cancellation support.
func (c *remoteCodingCallbacks) LLMRequestContext(iteration int) (context.Context, func(error), error) {
	if c.agent.loopCtx != nil && c.agent.loopCtx.IsCancelled() {
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
		if c.agent.onProgress != nil {
			duration := time.Since(startedAt)
			outcome := "success"
			if strings.HasPrefix(result, "错误") || strings.Contains(result, "ERROR") ||
				strings.Contains(result, "失败") || strings.Contains(result, "参数解析失败") ||
				strings.Contains(result, "unavailable") || strings.Contains(result, "unknown tool") {
				outcome = "failed"
			}
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

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		result = fmt.Sprintf("参数解析失败: %v", err)
		return result
	}

	h := c.agent.handler
	if h == nil {
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
	case "coding_knowledge_search":
		result = c.executeRemoteCodingKnowledgeSearch(argsJSON)
	case "knowledge_search":
		result = c.executeRemoteKnowledgeSearch(argsJSON)
	default:
		result = fmt.Sprintf("unknown tool: %s (supports: ssh_read_file, ssh_write_file, ssh_edit_file, ssh_bash, ssh_list_dir, ssh_check_task, coding_knowledge_search, knowledge_search)", name)
	}

	return result
}

func (c *remoteCodingCallbacks) sshReadFile(args map[string]interface{}) string {
	path := remoteArgStr(args, "path")
	if path == "" {
		return "错误: 需要 path 参数"
	}
	path = c.resolvePath(path)
	return c.execSSH(fmt.Sprintf("cat %s", remoteShellQuote(path)), 10)
}

func (c *remoteCodingCallbacks) sshWriteFile(args map[string]interface{}) string {
	path := remoteArgStr(args, "path")
	content := remoteArgStr(args, "content")
	if path == "" || content == "" {
		return "错误: 需要 path 和 content 参数"
	}
	path = c.resolvePath(path)

	// For large content (>32KB), write in chunks to avoid PTY buffer overflow.
	if len(content) > 32*1024 {
		return c.sshWriteFileLarge(path, content)
	}

	// Use base64 encoding embedded directly in Python code — no pipes needed.
	// This is PTY-safe since the entire command is a single python3 -c invocation.
	b64 := base64EncodeString(content)
	pyScript := fmt.Sprintf(`python3 -c "
import pathlib, base64
p = pathlib.Path('%s')
p.parent.mkdir(parents=True, exist_ok=True)
p.write_bytes(base64.b64decode('%s'))
print('OK')
"`, strings.ReplaceAll(path, "'", "'\\''"), b64)

	result := c.execSSH(pyScript, 15)
	if strings.Contains(result, "Traceback") || strings.Contains(result, "Error") {
		return fmt.Sprintf("写入失败: %s", result)
	}
	return fmt.Sprintf("✅ 已写入 %s (%d bytes)", path, len(content))
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
		if strings.Contains(result, "Error") {
			return fmt.Sprintf("写入失败（分块传输）: %s", result)
		}
	}

	// Decode and move to target path.
	decodeCmd := fmt.Sprintf(`python3 -c "
import pathlib, base64
p = pathlib.Path('%s')
p.parent.mkdir(parents=True, exist_ok=True)
data = base64.b64decode(open('%s').read())
p.write_bytes(data)
print('OK')
" && rm -f %s`, strings.ReplaceAll(path, "'", "'\\''"), tmpPath, tmpPath)

	result := c.execSSH(decodeCmd, 15)
	if strings.Contains(result, "OK") {
		return fmt.Sprintf("✅ 已写入 %s (%d bytes, chunked)", path, len(content))
	}
	return fmt.Sprintf("写入失败: %s", result)
}

func (c *remoteCodingCallbacks) sshEditFile(args map[string]interface{}) string {
	path := remoteArgStr(args, "path")
	oldStr := remoteArgStr(args, "old_str")
	newStr := remoteArgStr(args, "new_str")
	if path == "" || oldStr == "" {
		return "错误: 需要 path 和 old_str 参数"
	}
	path = c.resolvePath(path)

	// Use base64 to safely transfer old/new strings without heredoc terminator conflicts.
	oldB64 := base64EncodeString(oldStr)
	newB64 := base64EncodeString(newStr)
	pyScript := fmt.Sprintf(`python3 -c "
import pathlib, base64, sys
p = pathlib.Path('%s')
if not p.exists():
    print('ERROR: file not found: ' + str(p))
    sys.exit(0)
text = p.read_text(encoding='utf-8')
old = base64.b64decode('%s').decode('utf-8')
new = base64.b64decode('%s').decode('utf-8')
if old not in text:
    print('ERROR: old_str not found in file')
elif text.count(old) > 1:
    print('ERROR: old_str matches ' + str(text.count(old)) + ' locations (must be unique)')
else:
    p.write_text(text.replace(old, new, 1), encoding='utf-8')
    print('OK: replaced 1 occurrence')
"`, strings.ReplaceAll(path, "'", "'\\''"), oldB64, newB64)

	result := c.execSSH(pyScript, 15)
	if strings.Contains(result, "ERROR:") {
		return result
	}
	if strings.Contains(result, "OK:") {
		return fmt.Sprintf("✅ 已编辑 %s", path)
	}
	return fmt.Sprintf("编辑结果: %s", result)
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
	h := c.agent.handler
	return h.sshExec(map[string]interface{}{
		"session_id":   c.agent.sessionID,
		"command":      command,
		"wait_seconds": float64(waitSec),
	})
}

func (c *remoteCodingCallbacks) execSSHBackground(command string) string {
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
	return c.agent.projectDir + "/" + path
}

// --- System Prompt ---

func buildRemoteCodingSystemPrompt(projectDir, workDir, taskContext string) string {
	var sb strings.Builder
	sb.WriteString("# Remote Coding SubAgent\n\n")
	sb.WriteString("你是一个在远程服务器上执行代码修改和实验的编程 Agent。\n\n")
	sb.WriteString(fmt.Sprintf("## 环境信息\n- 远程项目目录: %s\n- 工作目录: %s\n\n", projectDir, workDir))
	sb.WriteString(`## 可用工具

- ssh_read_file(path): 读取远程文件内容
- ssh_write_file(path, content): 写入/创建远程文件（自动创建父目录）
- ssh_edit_file(path, old_str, new_str): 精确替换远程文件中的文本（old_str 必须唯一匹配）
- ssh_bash(command, working_dir?): 在远程服务器执行命令（长时间命令自动转后台任务，返回 task_id）
- ssh_check_task(task_id): 查询后台任务状态和日志（训练完成后返回 EXIT: 0）
- ssh_list_dir(path?): 列出远程目录内容

## 工作规范

1. 修改文件前先 ssh_read_file 确认当前内容
2. 使用 ssh_edit_file 做精确修改（小改动）或 ssh_write_file 重写文件（大改动）
3. 修改后用 ssh_bash 验证语法（如 "python3 -c 'import module'"）
4. 路径可以是相对路径（相对于项目目录）或绝对路径
5. 长时间训练命令会自动作为后台任务运行，返回 task_id

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
				"path": map[string]interface{}{"type": "string", "description": "文件路径（相对于项目目录或绝对路径）"},
			}, []string{"path"}),
		buildRemoteToolDef("ssh_write_file", "写入内容到远程文件（自动创建父目录）",
			map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "文件路径"},
				"content": map[string]interface{}{"type": "string", "description": "文件内容"},
			}, []string{"path", "content"}),
		buildRemoteToolDef("ssh_edit_file", "精确替换远程文件中的文本（old_str 必须在文件中唯一匹配）",
			map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "文件路径"},
				"old_str": map[string]interface{}{"type": "string", "description": "要被替换的原始文本"},
				"new_str": map[string]interface{}{"type": "string", "description": "替换后的新文本"},
			}, []string{"path", "old_str", "new_str"}),
		buildRemoteToolDef("ssh_bash", "在远程服务器上执行 shell 命令（长时间命令自动转后台任务）",
			map[string]interface{}{
				"command":     map[string]interface{}{"type": "string", "description": "要执行的命令"},
				"working_dir": map[string]interface{}{"type": "string", "description": "工作目录（默认项目目录）"},
			}, []string{"command"}),
		buildRemoteToolDef("ssh_list_dir", "列出远程目录内容",
			map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "目录路径（默认项目目录）"},
			}, nil),
		buildRemoteToolDef("ssh_check_task", "查询后台任务状态（训练/下载等长时间任务）。返回运行状态和日志尾部",
			map[string]interface{}{
				"task_id": map[string]interface{}{"type": "string", "description": "后台任务 ID（由 ssh_bash 长命令自动返回）"},
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

func remoteShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func base64EncodeString(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func isLongRemoteCommand(command string) bool {
	lower := strings.ToLower(command)
	// Only match commands that are clearly long-running.
	// Exclude short Python one-liners (python3 -c "...")
	longPatterns := []string{
		"train", "fit", "epoch",
		"nohup ", "screen ", "tmux ",
		"pip install", "conda install",
		"git clone ", "git pull",
		"wget ", "curl -o", "curl -L",
		"docker build", "docker pull",
		"make ", "cmake ",
		"apt install", "apt-get install",
	}
	for _, p := range longPatterns {
		if strings.Contains(lower, p) {
			// Exclude false positives: "python3 -c" one-liners that mention train/fit
			if strings.Contains(lower, "python3 -c") || strings.Contains(lower, "python -c") {
				return false
			}
			return true
		}
	}
	// Python/shell scripts that run training (not one-liners)
	if (strings.Contains(lower, "python") || strings.Contains(lower, "bash ")) &&
		!strings.Contains(lower, " -c ") &&
		!strings.Contains(lower, " -c\"") {
		// Only if it looks like running a script file
		if strings.Contains(lower, ".py") || strings.Contains(lower, ".sh") {
			return true
		}
	}
	return false
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
