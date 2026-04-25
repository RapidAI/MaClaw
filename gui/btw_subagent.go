package main

// btw_subagent.go implements a lightweight SubAgent for /btw side queries.
// It runs in an independent agent loop with a minimal tool set (web search,
// web fetch, read file, memory recall) and does NOT pollute the main
// conversation history with intermediate tool calls.
//
// Architecture mirrors CodingSubAgent:
//
//   Main Agent (conversation owner)     Btw SubAgent (side query)
//   ┌──────────────────────┐           ┌──────────────────────┐
//   │ System Prompt  12K   │           │ Query Prompt    1K   │
//   │ 40+ Tools     15K   │  /btw     │ 4 Tools        500   │
//   │ Memory/Steering 5K  │ ───────→  │ Query Context   ~100K│
//   │ History       20K   │           │                      │
//   │                      │ ←─────── │ Duties: search,      │
//   │ Duties: workflow,    │  result   │ fetch, read, recall  │
//   │ IM, memory, routing  │ (1 msg)  │                      │
//   └──────────────────────┘           └──────────────────────┘
//
// Mechanism-level design decisions:
//
// 1. Tool allowlist enforced at ExecuteTool level, not just system prompt.
//    The memory tool is restricted to action="recall" — save/delete/list
//    are rejected with an error, regardless of what the LLM requests.
//
// 2. History isolation: /btw runs with empty history (nil). The final result
//    is appended to the main history by the caller (handleBtwCommand), not
//    by the SubAgent itself. This keeps the SubAgent stateless.
//
// 3. Cancellation: the caller passes a LoopContext whose Cancel() is wired
//    to the user's cancel action. ShouldStop() checks this context.

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
)

// BtwSubAgent executes a /btw side query in a clean, independent context.
type BtwSubAgent struct {
	handler    *IMMessageHandler
	cfg        corelib.MaclawLLMConfig
	httpClient *http.Client

	// cancelled is set to 1 by Cancel(). ShouldStop() reads it.
	// This is the mechanism-level cancellation — no dependency on LoopContext.
	cancelled atomic.Int32

	onToken    func(string)
	onProgress func(string)
}

// BtwResult is the outcome of a /btw query.
type BtwResult struct {
	Text       string
	Error      string
	Iterations int
	ToolCalls  int
}

// NewBtwSubAgent creates a SubAgent for /btw side queries.
func NewBtwSubAgent(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client) *BtwSubAgent {
	return &BtwSubAgent{
		handler:    handler,
		cfg:        cfg,
		httpClient: httpClient,
	}
}

// Cancel signals the SubAgent to stop at the next iteration.
func (b *BtwSubAgent) Cancel() {
	b.cancelled.Store(1)
}

// SetCallbacks configures optional streaming and progress callbacks.
func (b *BtwSubAgent) SetCallbacks(onToken func(string), onProgress func(string)) {
	b.onToken = onToken
	b.onProgress = onProgress
}

// btwMaxIterations is the iteration cap for /btw side queries.
// Set to MinAgentIterations (30) because EffectiveMaxIterations enforces
// a floor of 30. Side queries typically finish in 3-5 iterations; the cap
// is a safety net, not a target.
const btwMaxIterations = config.MinAgentIterations

// Execute runs the /btw query in an independent agent loop.
// The conversation is independent — no IM rules, no workflow, no 40+ tools.
func (b *BtwSubAgent) Execute(query string) *BtwResult {
	log.Printf("[btw-subagent] starting query: %s", truncateRunesForSubAgent(query, 80))

	if b.onProgress != nil {
		b.onProgress("🔍 /btw 侧查询中...")
	}

	cb := &btwCallbacks{subagent: b}

	// Run with empty history — /btw is a fresh, independent query.
	result := agent.RunLoop(cb, query, nil, b.httpClient)

	if result.Error != "" && result.Text == "" {
		return &BtwResult{
			Error:      result.Error,
			Iterations: result.Iterations,
			ToolCalls:  result.ToolCalls,
		}
	}

	text := result.Text
	if text == "" {
		text = "未找到相关信息。"
	}

	// Prefix with /btw indicator so the user knows this is a side query result.
	text = "🔍 **/btw 查询结果**\n\n" + text

	return &BtwResult{
		Text:       text,
		Iterations: result.Iterations,
		ToolCalls:  result.ToolCalls,
	}
}

// ---------------------------------------------------------------------------
// btwCallbacks implements agent.LoopCallbacks with a minimal query-only
// configuration.
// ---------------------------------------------------------------------------

type btwCallbacks struct {
	subagent *BtwSubAgent

	// cachedTools is built once on first call to BuildTools.
	cachedTools []map[string]interface{}
}

func (c *btwCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.subagent.cfg
}

func (c *btwCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(btwMaxIterations)
}

func (c *btwCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	return buildBtwSystemPrompt()
}

func (c *btwCallbacks) BuildTools(userText string) []map[string]interface{} {
	if c.cachedTools == nil {
		c.cachedTools = buildBtwToolDefinitions()
	}
	return c.cachedTools
}

func (c *btwCallbacks) ExecuteTool(name, argsJSON string) string {
	if !btwToolNames[name] {
		return fmt.Sprintf("未知工具: %s（/btw 仅支持 web_search, web_fetch, read_file, memory）", name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}

	// Mechanism-level enforcement: memory tool is recall-only in /btw.
	// This is not a prompt-level suggestion — the LLM cannot bypass it.
	if name == "memory" {
		action, _ := args["action"].(string)
		if action != "recall" {
			return "错误: /btw 侧查询中 memory 工具仅支持 action=\"recall\"（只读查询）"
		}
	}

	h := c.subagent.handler
	switch name {
	case "web_search":
		return h.toolWebSearch(args)
	case "web_fetch":
		return h.toolWebFetch(args)
	case "read_file":
		return h.toolReadFile(args)
	case "memory":
		return h.toolMemory(args)
	default:
		return fmt.Sprintf("未知工具: %s", name)
	}
}

func (c *btwCallbacks) OnToken(delta string) {
	if c.subagent.onToken != nil {
		c.subagent.onToken(delta)
	}
}

func (c *btwCallbacks) OnProgress(text string) {
	if c.subagent.onProgress != nil {
		c.subagent.onProgress(text)
	}
}

func (c *btwCallbacks) OnToolCall(name string) {
	if c.subagent.onProgress != nil {
		c.subagent.onProgress(fmt.Sprintf("🔧 %s", name))
	}
}

func (c *btwCallbacks) OnToolResult(name string) {}

func (c *btwCallbacks) ShouldStop() bool {
	return c.subagent.cancelled.Load() != 0
}

// ---------------------------------------------------------------------------
// Tool set
// ---------------------------------------------------------------------------

// btwToolNames is the allowlist of tools available to /btw queries.
var btwToolNames = map[string]bool{
	"web_search": true,
	"web_fetch":  true,
	"read_file":  true,
	"memory":     true,
}

// buildBtwToolDefinitions constructs the minimal tool definitions for /btw.
func buildBtwToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		btwToolDef("web_search", "搜索互联网获取最新信息",
			map[string]interface{}{
				"query":       map[string]string{"type": "string", "description": "搜索关键词"},
				"max_results": map[string]string{"type": "integer", "description": "最大结果数（默认 8）"},
			}, []string{"query"}),
		btwToolDef("web_fetch", "抓取指定 URL 的网页内容并提取正文",
			map[string]interface{}{
				"url":       map[string]string{"type": "string", "description": "要抓取的 URL"},
				"max_chars": map[string]string{"type": "integer", "description": "最多返回字符数（可选）"},
			}, []string{"url"}),
		btwToolDef("read_file", "读取本地文件内容",
			map[string]interface{}{
				"path":   map[string]string{"type": "string", "description": "文件路径"},
				"lines":  map[string]string{"type": "integer", "description": "读取行数（可选）"},
				"offset": map[string]string{"type": "integer", "description": "从末尾倒数行数开始读取（可选）"},
			}, []string{"path"}),
		btwToolDef("memory", "查询长期记忆（仅支持 recall）",
			map[string]interface{}{
				"action": map[string]string{"type": "string", "description": "操作: recall（仅支持 recall）"},
				"query":  map[string]string{"type": "string", "description": "查询关键词"},
			}, []string{"action", "query"}),
	}
}

func btwToolDef(name, desc string, props map[string]interface{}, required []string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": desc,
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": props,
				"required":   required,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// System prompt
// ---------------------------------------------------------------------------

func buildBtwSystemPrompt() string {
	var b strings.Builder
	b.WriteString("你是一个高效的信息查询助手。用户通过 /btw 命令发起了一个侧查询（side query），请快速、准确地回答。\n\n")
	b.WriteString("## 规则\n\n")
	b.WriteString("1. 优先使用 web_search 搜索最新信息，然后用 web_fetch 获取详细内容\n")
	b.WriteString("2. 如果问题涉及本地项目文件，使用 read_file 查看\n")
	b.WriteString("3. 如果问题涉及之前的对话或记忆，使用 memory(action=\"recall\", query=\"...\") 召回\n")
	b.WriteString("4. 回答要简洁、结构化，直接给出关键信息\n")
	b.WriteString("5. 引用网络来源时附上 URL\n")
	b.WriteString("6. 这是一个只读查询——不要修改任何文件\n")
	b.WriteString("7. 尽量在 2-3 轮工具调用内完成查询，不要过度搜索\n")
	return b.String()
}
