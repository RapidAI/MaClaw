package main

// btw_subagent.go implements a lightweight SubAgent for /btw side queries.
// It runs in an independent agent loop with a minimal tool set (web search,
// web fetch, read file, memory recall) and does NOT pollute the main
// conversation history with intermediate tool calls.
//
// Architecture mirrors CodingSubAgent — both build focused system prompts
// instead of reusing the main agent's monolithic buildSystemPromptBase.
// BtwSubAgent composes: identity + /btw rules + user facts + memory recall.
// No coding workflow, no session management, no memory write guide.
//
//   Main Agent (conversation owner)     Btw SubAgent (side query)
//   ┌──────────────────────┐           ┌──────────────────────┐
//   │ System Prompt  12K   │           │ Identity + Recall ~2K │
//   │ 40+ Tools     15K   │  /btw     │ 5 Tools        600   │
//   │ Memory/Steering 5K  │ ───────→  │ Empty History         │
//   │ History       20K   │           │                      │
//   │                      │ ←─────── │ Duties: search,      │
//   │ Duties: workflow,    │  result   │ fetch, read, recall, │
//   │ IM, memory, routing  │ (1 msg)  │ agent_status         │
//   └──────────────────────┘           └──────────────────────┘
//
// Mechanism-level design decisions:
//
// 1. Tool allowlist enforced at ExecuteTool level, not just system prompt.
//    The memory tool is restricted to action="recall" — save/delete/list
//    are rejected with an error, regardless of what the LLM requests.
//
// 2. History isolation: /btw runs with empty history (nil). The final result
//    is displayed in the chat UI but NOT appended to the main conversation
//    history — this avoids racing with a concurrent main agent loop's Save.
//
// 3. Cancellation: BtwSubAgent.Cancel() sets an atomic flag checked by
//    ShouldStop(). The handler stores the active SubAgent in activeBtwSubAgent
//    so /cancel can reach it.

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
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
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
	// Build a focused system prompt for /btw by selectively composing only
	// the sections relevant to a single-turn read-only query:
	//   ✅ Identity (role name, self_identity from memory)
	//   ✅ Proactive memory recall (relevant memories for the query)
	//   ✅ User fact summary (who the user is)
	//   ❌ Coding workflow rules (3000+ tokens of noise)
	//   ❌ Memory management guide (save/replace/delete — /btw is read-only)
	//   ❌ Session list, MCP servers, skills, security firewall
	//   ❌ PDF generation rules, file editing strategy
	//
	// This avoids two mechanism-level problems with reusing the full prompt:
	// 1. frozenMemorySnapshots cache pollution (isFirstTurn=true corrupts
	//    the main agent's cached snapshot)
	// 2. Memory-driven tool pinning side effects (appendProactiveRecall
	//    pins tools to the main session based on /btw query content)
	return buildBtwSystemPrompt(c.subagent.handler, userText)
}

// buildBtwSystemPrompt constructs a focused system prompt for /btw.
// It reads identity and memory directly from the handler's stores,
// without calling buildSystemPromptBase (which has side effects).
func buildBtwSystemPrompt(h *IMMessageHandler, userText string) string {
	var b strings.Builder

	// --- Identity (same source as main agent) ---
	roleName := "MaClaw"
	roleDesc := "一个尽心尽责无所不能的软件开发管家"
	if cfg, err := h.loadConfig(); err == nil {
		if cfg.MaclawRoleName != "" {
			roleName = cfg.MaclawRoleName
		}
		if cfg.MaclawRoleDescription != "" {
			roleDesc = cfg.MaclawRoleDescription
		}
	}

	var selfIdentity string
	if h.memoryStore != nil {
		selfIdentity = h.memoryStore.SelfIdentitySummary(600)
	}

	if selfIdentity != "" {
		fmt.Fprintf(&b, "你的自我认知（来自记忆）：%s\n你的底层系统名为 %s。\n", selfIdentity, roleName)
	} else {
		fmt.Fprintf(&b, "你是 %s，%s。\n", roleName, roleDesc)
	}

	// --- /btw mode declaration ---
	b.WriteString(btwSuffix)

	// --- User fact summary (who the user is, no management guide) ---
	if h.memoryStore != nil {
		b.WriteString(h.memoryStore.UserFactSummaryForPrompt(corememory.UserFactPromptOptions("\n## \u7528\u6237\u4fe1\u606f")))
	}

	// --- Proactive memory recall (read-only, no tool pinning side effect) ---
	if h.memoryStore != nil && userText != "" {
		projectPath := ""
		if h.contextResolver != nil {
			projectPath, _ = h.contextResolver.ResolveProject()
		}
		promptContext, _ := h.memoryStore.ProactiveContextForPrompt(userText, corememory.BtwProactivePromptOptions(projectPath, "\n## \u76f8\u5173\u8bb0\u5fc6\uff08\u81ea\u52a8\u53ec\u56de\uff09"))
		b.WriteString(promptContext)
	}

	return b.String()
}

// btwSuffix defines the /btw mode constraints.
const btwSuffix = `
## /btw 侧查询模式（当前生效）

你正在处理一个 /btw 侧查询。这是一个独立的单轮快速查询，不是主任务的一部分。

规则：
1. 如果用户询问任务进度、运行状态等问题，优先使用 agent_status 工具查询实际运行时状态
2. 使用 web_search 搜索最新信息，然后用 web_fetch 获取详细内容
3. 如果问题涉及本地项目文件，使用 read_file 查看
4. 如果问题涉及之前的对话或记忆，使用 memory(action="recall") 召回
5. 回答要简洁、结构化，直接给出关键信息
6. 引用网络来源时附上 URL
7. 这是一个只读查询——不要修改任何文件，不要执行任何写操作
8. 尽量在 2-3 轮工具调用内完成查询，不要过度搜索
`

func (c *btwCallbacks) BuildTools(userText string) []map[string]interface{} {
	if c.cachedTools == nil {
		c.cachedTools = buildBtwToolDefinitions()
	}
	return c.cachedTools
}

func (c *btwCallbacks) ExecuteTool(name, argsJSON string) string {
	if !btwToolNames[name] {
		return fmt.Sprintf("未知工具: %s（/btw 仅支持 web_search, web_fetch, read_file, memory, agent_status）", name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}

	// Mechanism-level enforcement: memory tool is read-only in /btw.
	// This is not a prompt-level suggestion — the LLM cannot bypass it.
	if name == "memory" {
		action, _ := args["action"].(string)
		if !normalizeMemoryToolAction(action).IsRecallOnlyAllowed() {
			return "错误: /btw 侧查询中 memory 工具仅支持只读操作（recall/themes/scenes/trace/candidates/derived）"
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
	case "agent_status":
		return h.toolAgentStatus(args)
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
	"web_search":   true,
	"web_fetch":    true,
	"read_file":    true,
	"memory":       true,
	"agent_status": true,
}

// buildBtwToolDefinitions constructs the minimal tool definitions for /btw.
func buildBtwToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		btwToolDef("web_search", "Search the web for fresh information.",
			map[string]interface{}{
				"query":       map[string]string{"type": "string", "description": "Search query"},
				"max_results": map[string]string{"type": "integer", "description": "Maximum number of results"},
			}, []string{"query"}),
		btwToolDef("web_fetch", "Fetch and extract text from a URL.",
			map[string]interface{}{
				"url":       map[string]string{"type": "string", "description": "URL to fetch"},
				"max_chars": map[string]string{"type": "integer", "description": "Maximum returned characters"},
			}, []string{"url"}),
		btwToolDef("read_file", "Read a local file.",
			map[string]interface{}{
				"path":   map[string]string{"type": "string", "description": "File path"},
				"lines":  map[string]string{"type": "integer", "description": "Number of lines to read"},
				"offset": map[string]string{"type": "integer", "description": "Line offset"},
			}, []string{"path"}),
		btwToolDef("memory", "Recall long-term memory entries.",
			map[string]interface{}{
				"action": map[string]string{"type": "string", "description": "Action: recall"},
				"query":  map[string]string{"type": "string", "description": "Recall query"},
			}, []string{"action", "query"}),
		btwToolDef("agent_status", "Inspect local agent task, coding session, and SSH session status.",
			map[string]interface{}{
				"category": map[string]string{"type": "string", "description": "Category: all, local_tasks, ssh_tasks, sessions, or ssh_sessions"},
				"task_id":  map[string]string{"type": "string", "description": "Optional task ID"},
			}, nil),
	}
}
func btwToolDef(name, desc string, props map[string]interface{}, required []string) map[string]interface{} {
	return agent.ToolDef(name, desc, props, required)
}

// ---------------------------------------------------------------------------
// System prompt — see buildBtwSystemPrompt() above.
// Selectively composes identity + memory recall. Does NOT call the main
// agent's buildSystemPromptBase to avoid side effects and noise.
//
// agent_status tool implementation is in im_tool_agent_status.go — it's a
// handler-level capability, not /btw-specific.
// ---------------------------------------------------------------------------
