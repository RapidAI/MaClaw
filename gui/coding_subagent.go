package main

// coding_subagent.go implements a lightweight coding SubAgent that runs in a
// clean context — only coding-related system prompt, only file/shell tools,
// and an independent conversation history. This eliminates the context
// pollution problem where IM rules, 40+ tool definitions, memory recall,
// and steering rules consume 40K+ tokens before any coding work begins.
//
// The SubAgent reuses corelib/agent.RunLoop for the LLM loop and delegates
// tool execution to the host's existing tool implementations (toolReadFile,
// toolWriteFile, etc.) via the IMMessageHandler reference.
//
// Architecture:
//
//   Main Agent (orchestrator)          Coding SubAgent (executor)
//   ┌──────────────────────┐          ┌──────────────────────┐
//   │ System Prompt  12K   │          │ Coding Prompt   2K   │
//   │ 40+ Tools     15K   │          │ 6 Tools         2K   │
//   │ Memory/Steering 5K  │   task   │ Task Context    3K   │
//   │ History       20K   │ ───────→ │ Coding History  ~95K │
//   │                      │          │                      │
//   │ Duties: workflow,    │ ←─────── │ Duties: read/write   │
//   │ IM, memory, routing  │  result  │ files, compile, test │
//   └──────────────────────┘          └──────────────────────┘

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
)

// CodingSubAgent executes a single coding task in a clean context.
type CodingSubAgent struct {
	handler     *IMMessageHandler // host — provides tool implementations
	cfg         corelib.MaclawLLMConfig
	httpClient  *http.Client
	projectPath string

	// onToken streams text deltas to the UI (optional).
	onToken func(delta string)
	// onProgress reports status updates (optional).
	onProgress func(text string)

	// loopCtx is the parent LoopContext from the main agent loop.
	// Used to propagate cancellation: when the user clicks cancel,
	// CancelCurrentSession calls loopCtx.Cancel(), and the SubAgent's
	// ShouldStop checks loopCtx.IsCancelled().
	loopCtx *LoopContext
}

// CodingSubAgentResult is the outcome of a single task execution.
type CodingSubAgentResult struct {
	Status     TaskExecStatus // passed, failed, skipped
	Summary    string         // human-readable summary of what was done
	Error      string         // error message if failed
	Iterations int
	ToolCalls  int

	// FilesModified lists files that were written/edited during execution.
	// Extracted from tool call history for context preservation.
	FilesModified []string
}

// NewCodingSubAgent creates a SubAgent bound to the host's tool implementations.
// loopCtx propagates cancellation from the main agent loop.
func NewCodingSubAgent(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, projectPath string, loopCtx *LoopContext) *CodingSubAgent {
	return &CodingSubAgent{
		handler:     handler,
		cfg:         cfg,
		httpClient:  httpClient,
		projectPath: projectPath,
		loopCtx:     loopCtx,
	}
}

// SetCallbacks configures optional streaming and progress callbacks.
func (s *CodingSubAgent) SetCallbacks(onToken func(string), onProgress func(string)) {
	s.onToken = onToken
	s.onProgress = onProgress
}

// ExecuteTask runs a single task in a clean coding context.
// The conversation is independent — no IM rules, no memory, no 40+ tools.
func (s *CodingSubAgent) ExecuteTask(task *TaskItem, reqCtx, designCtx string, prevOutputs []string) *CodingSubAgentResult {
	log.Printf("[coding-subagent] starting task T%d: %s (project=%s)", task.Index, task.Title, s.projectPath)

	if s.onProgress != nil {
		s.onProgress(fmt.Sprintf("🔧 开始执行任务 T%d: %s", task.Index, task.Title))
	}

	cb := &codingSubAgentCallbacks{
		subagent:    s,
		task:        task,
		reqCtx:      reqCtx,
		designCtx:   designCtx,
		prevOutputs: prevOutputs,
	}

	result := agent.RunLoop(cb, cb.buildTaskUserMessage(), nil, s.httpClient)

	status := TaskExecPassed
	errMsg := ""
	if result.Error != "" {
		status = TaskExecFailed
		errMsg = result.Error
	}
	if result.HardExit {
		status = TaskExecFailed
		errMsg = "模型连续返回空响应，任务中断"
	}

	summary := result.Text
	if summary == "" {
		summary = fmt.Sprintf("任务 T%d 执行完成，%d 轮迭代，%d 次工具调用", task.Index, result.Iterations, result.ToolCalls)
	}

	log.Printf("[coding-subagent] task T%d finished: status=%s iterations=%d tools=%d err=%q",
		task.Index, status, result.Iterations, result.ToolCalls, errMsg)

	return &CodingSubAgentResult{
		Status:        status,
		Summary:       summary,
		Error:         errMsg,
		Iterations:    result.Iterations,
		ToolCalls:     result.ToolCalls,
		FilesModified: cb.getFilesModified(),
	}
}

// ---------------------------------------------------------------------------
// codingSubAgentCallbacks implements agent.LoopCallbacks with a minimal
// coding-only configuration.
// ---------------------------------------------------------------------------

type codingSubAgentCallbacks struct {
	subagent    *CodingSubAgent
	task        *TaskItem
	reqCtx      string
	designCtx   string
	prevOutputs []string

	// cachedTools is built once on first call to BuildTools.
	cachedTools []map[string]interface{}

	// filesModified tracks files written/edited during execution.
	mu            sync.Mutex
	filesModified map[string]bool
}

func (c *codingSubAgentCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.subagent.cfg
}

func (c *codingSubAgentCallbacks) GetMaxIterations() int {
	// Single task: 50 iterations is generous. This keeps context bounded.
	return config.EffectiveMaxIterations(50)
}

func (c *codingSubAgentCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	return buildCodingSubAgentSystemPrompt(c.task, c.subagent.projectPath, c.reqCtx, c.designCtx, c.prevOutputs)
}

func (c *codingSubAgentCallbacks) BuildTools(userText string) []map[string]interface{} {
	if c.cachedTools == nil {
		c.cachedTools = buildCodingToolDefinitionsFromRegistry(c.subagent.handler)
	}
	return c.cachedTools
}

func (c *codingSubAgentCallbacks) ExecuteTool(name, argsJSON string) string {
	if !codingSubAgentToolNames[name] {
		return fmt.Sprintf("未知工具: %s（编码 SubAgent 仅支持 %v）", name, codingSubAgentToolNameList())
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}

	h := c.subagent.handler
	switch name {
	case "read_file":
		return h.toolReadFile(args)
	case "write_file":
		if p, _ := args["path"].(string); p != "" {
			c.trackFile(p)
		}
		return h.toolWriteFile(args)
	case "edit_file":
		if p, _ := args["path"].(string); p != "" {
			c.trackFile(p)
		}
		return h.toolEditFile(args)
	case "edit_lines":
		if p, _ := args["path"].(string); p != "" {
			c.trackFile(p)
		}
		return h.toolEditLines(args)
	case "bash":
		return h.toolBash(args, func(text string) {
			if c.subagent.onProgress != nil {
				c.subagent.onProgress(text)
			}
		})
	case "list_directory":
		return h.toolListDirectory(args)
	default:
		return fmt.Sprintf("未知工具: %s", name)
	}
}

func codingSubAgentToolNameList() []string {
	names := make([]string, 0, len(codingSubAgentToolNames))
	for n := range codingSubAgentToolNames {
		names = append(names, n)
	}
	return names
}

func (c *codingSubAgentCallbacks) trackFile(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.filesModified == nil {
		c.filesModified = make(map[string]bool)
	}
	c.filesModified[path] = true
}

func (c *codingSubAgentCallbacks) getFilesModified() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	files := make([]string, 0, len(c.filesModified))
	for f := range c.filesModified {
		files = append(files, f)
	}
	return files
}

func (c *codingSubAgentCallbacks) OnToken(delta string) {
	if c.subagent.onToken != nil {
		c.subagent.onToken(delta)
	}
}

func (c *codingSubAgentCallbacks) OnProgress(text string) {
	if c.subagent.onProgress != nil {
		c.subagent.onProgress(text)
	}
}

func (c *codingSubAgentCallbacks) OnToolCall(name string) {
	if c.subagent.onProgress != nil {
		c.subagent.onProgress(fmt.Sprintf("🔧 %s", name))
	}
}

func (c *codingSubAgentCallbacks) OnToolResult(name string) {}

func (c *codingSubAgentCallbacks) ShouldStop() bool {
	if c.subagent.loopCtx != nil {
		return c.subagent.loopCtx.IsCancelled()
	}
	return false
}

func (c *codingSubAgentCallbacks) buildTaskUserMessage() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("请执行以下编码任务：\n\n## T%d: %s\n\n", c.task.Index, c.task.Title))
	if c.task.Description != "" {
		b.WriteString(c.task.Description)
		b.WriteString("\n\n")
	}
	if len(c.task.Files) > 0 {
		b.WriteString("**涉及文件**：")
		b.WriteString(strings.Join(c.task.Files, ", "))
		b.WriteString("\n\n")
	}
	if len(c.task.AcceptanceCriteria) > 0 {
		b.WriteString("**验收标准**：\n")
		for _, ac := range c.task.AcceptanceCriteria {
			b.WriteString("- " + ac + "\n")
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// System prompt — minimal, coding-only. ~1500-2000 tokens.
// ---------------------------------------------------------------------------

func buildCodingSubAgentSystemPrompt(task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string) string {
	var b strings.Builder

	b.WriteString(`你是一个专注的编码执行器。你的唯一职责是完成分配给你的编码任务。

## 工具使用策略（严格遵守）

### 读取：先理解再动手
- 修改已有文件前，必须先 read_file 查看当前内容。不要凭记忆修改。
- 大文件用 read_file(start_line=N, lines=50) 只读相关段落，不要读整个文件。

### 修改已有文件：edit_file / edit_lines（patch 模式）
- 修改已有文件时，必须优先使用 edit_file 或 edit_lines，不要用 write_file 重写整个文件。
- **edit_file**（搜索替换）：提供 old_string 和 new_string。适合改动内容明确、上下文唯一的场景。
- **edit_lines**（行号编辑）：提供行号范围和新内容。适合改动位置明确（先 read_file 看到行号）或文件中有重复内容的场景。
  - replace: edit_lines(path, operation="replace", start_line=10, end_line=12, content="新内容")
  - insert:  edit_lines(path, operation="insert", start_line=5, content="插入的行") — 在第 5 行后插入
  - delete:  edit_lines(path, operation="delete", start_line=10, end_line=15)
- 一个文件需要改多处时，对每处分别调用，不要试图一次替换完。
- edit_file 失败（"未找到要替换的内容"）时，用 read_file 确认当前内容，改用 edit_lines 按行号编辑。

### 创建新文件：write_file
- 只有创建全新文件时才用 write_file(mode=overwrite)。
- 大文件（>3000 字符）分块写入：先 overwrite 第一部分，再 append 后续部分。

### 禁止行为
- 禁止用 write_file 重写已有文件来做小修改——这浪费 token 且容易丢失原文件中的其他内容。
- 禁止不读文件就直接修改——你不知道文件当前的确切内容，edit_file 的 old_string 会匹配失败。

## 编码规范
- 每次修改后运行编译/构建命令验证，确保代码可编译。
- write_file 始终 UTF-8 编码，直接写中文即可。
- 完成后简要总结：列出修改的文件和每个文件改了什么。
- 遇到无法解决的问题，说明具体原因，不要反复重试相同的失败操作。
`)

	b.WriteString(fmt.Sprintf("\n## 项目路径\n%s\n", projectPath))

	// Platform hint so the LLM generates correct shell commands.
	b.WriteString(fmt.Sprintf("平台: %s\n", normalizedRemotePlatform()))
	if normalizedRemotePlatform() == "windows" {
		b.WriteString("注意: bash 工具通过 PowerShell 执行。使用 PowerShell 语法（如 `;` 分隔命令，`Remove-Item` 删除文件）。\n")
	}

	if reqCtx != "" {
		b.WriteString("\n## 需求摘要\n")
		b.WriteString(truncateRunesForSubAgent(reqCtx, 800))
		b.WriteString("\n")
	}

	if designCtx != "" {
		b.WriteString("\n## 设计摘要\n")
		b.WriteString(truncateRunesForSubAgent(designCtx, 800))
		b.WriteString("\n")
	}

	if len(prevOutputs) > 0 {
		b.WriteString("\n## 前置任务产出\n")
		for _, f := range prevOutputs {
			b.WriteString("- " + f + "\n")
		}
	}

	now := time.Now()
	b.WriteString(fmt.Sprintf("\n当前时间: %s\n", now.Format("2006-01-02 15:04")))

	return b.String()
}

// truncateRunesForSubAgent truncates a string to maxRunes, preferring paragraph boundaries.
func truncateRunesForSubAgent(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	// Try to cut at a paragraph boundary.
	cutoff := string(runes[:maxRunes])
	if idx := strings.LastIndex(cutoff, "\n\n"); idx > len(cutoff)/2 {
		return cutoff[:idx] + "\n…（已截断）"
	}
	if idx := strings.LastIndex(cutoff, "\n"); idx > len(cutoff)/2 {
		return cutoff[:idx] + "\n…（已截断）"
	}
	return cutoff + "…（已截断）"
}

// ---------------------------------------------------------------------------
// Tool definitions — only 5 coding tools.
// Extracted from the host's tool registry to avoid duplicate definitions.
// If the registry is not available (e.g. in tests), falls back to minimal
// inline definitions.
// ---------------------------------------------------------------------------

// codingSubAgentToolNames is the single source of truth for which tools
// the SubAgent can use. Adding a tool here automatically makes it available
// in the SubAgent's tool list AND in the ExecuteTool dispatch.
var codingSubAgentToolNames = map[string]bool{
	"read_file":      true,
	"write_file":     true,
	"edit_file":      true,
	"edit_lines":     true,
	"bash":           true,
	"list_directory":  true,
}

// buildCodingToolDefinitionsFromRegistry extracts tool definitions from the
// host's registry, filtered to only coding tools. This ensures parameter
// changes (e.g. adding offset to read_file) are automatically reflected
// in the SubAgent without maintaining a separate copy.
func buildCodingToolDefinitionsFromRegistry(handler *IMMessageHandler) []map[string]interface{} {
	if handler == nil {
		return buildCodingToolDefinitionsFallback()
	}
	allTools := handler.getTools()
	if len(allTools) == 0 {
		return buildCodingToolDefinitionsFallback()
	}

	var filtered []map[string]interface{}
	for _, t := range allTools {
		fn, ok := t["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if codingSubAgentToolNames[name] {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return buildCodingToolDefinitionsFallback()
	}
	return filtered
}

// buildCodingToolDefinitionsFallback provides minimal inline definitions
// for testing or when the registry is unavailable.
func buildCodingToolDefinitionsFallback() []map[string]interface{} {
	tools := []map[string]interface{}{
		buildToolDef("read_file", "读取文件内容",
			map[string]interface{}{
				"path":       map[string]string{"type": "string", "description": "文件路径"},
				"lines":      map[string]string{"type": "integer", "description": "最多读取行数（可选）"},
				"start_line": map[string]string{"type": "integer", "description": "起始行号（可选）"},
				"offset":     map[string]string{"type": "integer", "description": "从文件末尾倒数行数开始读取（可选，类似 tail -n）"},
			}, []string{"path"}),

		buildToolDef("write_file", "写入文件（UTF-8，支持 overwrite/append）",
			map[string]interface{}{
				"path":    map[string]string{"type": "string", "description": "文件路径"},
				"content": map[string]string{"type": "string", "description": "文件内容"},
				"mode":    map[string]string{"type": "string", "description": "overwrite（默认）或 append"},
			}, []string{"path", "content"}),

		buildToolDef("edit_file", "编辑文件（文本替换）",
			map[string]interface{}{
				"path":        map[string]string{"type": "string", "description": "文件路径"},
				"old_string":  map[string]string{"type": "string", "description": "要查找的原始文本"},
				"new_string":  map[string]string{"type": "string", "description": "替换后的文本"},
				"replace_all": map[string]string{"type": "boolean", "description": "是否替换全部匹配"},
			}, []string{"path", "old_string", "new_string"}),

		buildToolDef("edit_lines", "按行号精确编辑文件（替换/插入/删除指定行）",
			map[string]interface{}{
				"path":       map[string]string{"type": "string", "description": "文件路径"},
				"operation":  map[string]string{"type": "string", "description": "操作: replace/insert/delete"},
				"start_line": map[string]string{"type": "integer", "description": "起始行号（1-indexed，insert 时 0=文件开头）"},
				"end_line":   map[string]string{"type": "integer", "description": "结束行号（replace/delete 时必填）"},
				"content":    map[string]string{"type": "string", "description": "新内容（replace/insert 时必填）"},
			}, []string{"path", "operation", "start_line"}),

		buildToolDef("bash", "执行 shell 命令",
			map[string]interface{}{
				"command":     map[string]string{"type": "string", "description": "要执行的命令"},
				"working_dir": map[string]string{"type": "string", "description": "工作目录（可选）"},
				"timeout":     map[string]string{"type": "integer", "description": "超时秒数（可选，默认 30）"},
			}, []string{"command"}),

		buildToolDef("list_directory", "列出目录内容",
			map[string]interface{}{
				"path": map[string]string{"type": "string", "description": "目录路径"},
			}, nil),
	}
	return tools
}

func buildToolDef(name, desc string, props map[string]interface{}, required []string) map[string]interface{} {
	return agent.ToolDef(name, desc, props, required)
}

// ---------------------------------------------------------------------------
// Convenience: run a task through the SubAgent from the orchestrator.
// ---------------------------------------------------------------------------

// RunTaskWithSubAgent creates a SubAgent and executes a single task.
// This is the entry point called by the main agent loop when the orchestrator
// delegates a task to the SubAgent instead of the external coding tool.
func RunTaskWithSubAgent(
	handler *IMMessageHandler,
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
	task *TaskItem,
	projectPath, reqCtx, designCtx string,
	prevOutputs []string,
	loopCtx *LoopContext,
	onToken func(string),
	onProgress func(string),
) *CodingSubAgentResult {
	sa := NewCodingSubAgent(handler, cfg, httpClient, projectPath, loopCtx)
	sa.SetCallbacks(onToken, onProgress)
	return sa.ExecuteTask(task, reqCtx, designCtx, prevOutputs)
}
