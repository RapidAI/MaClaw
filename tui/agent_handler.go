package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/clawnet"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// agentMaxIterations 是 Agent 循环的默认最大轮数。
const agentMaxIterations = 300

// llmToolCall 表示 LLM 返回的工具调用。
type llmToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// llmChoice 表示 LLM 返回的一个选择。
type llmChoice struct {
	Message struct {
		Content          string        `json:"content"`
		ReasoningContent string        `json:"reasoning_content,omitempty"`
		ToolCalls        []llmToolCall `json:"tool_calls,omitempty"`
	} `json:"message"`
}

// llmResponse 表示 LLM 的完整响应。
type llmResponse struct {
	Choices []llmChoice `json:"choices"`
}

// TUIAgentHandler 是 TUI 端的 Agent 循环处理器。
// 支持 LLM 工具调用，集成 Firewall + Router + 40+ 工具。
type TUIAgentHandler struct {
	sessionMgr       *TUISessionManager
	httpClient       *http.Client
	firewall         *security.Firewall
	defGenerator     *tool.DefinitionGenerator
	router           *tool.Router
	selector         *tool.Selector
	configMgr        *config.Manager
	memoryStore      *memory.Store
	schedulerMgr     *scheduler.Manager
	clawnetClient    *clawnet.Client
	auditLog         *security.AuditLog
	sshMgr           *remote.SSHSessionManager
	maxIterations    int
	codingToolHealth *codingToolHealthCache // 编程工具健康状态缓存

	// lastScreenshotAt records the time of the last successful screenshot
	// to enforce a cooldown period and prevent accidental rapid-fire captures.
	lastScreenshotAt time.Time
}

// NewTUIAgentHandler 创建 Agent 处理器。
func NewTUIAgentHandler(sessionMgr *TUISessionManager, opts ...AgentHandlerOption) *TUIAgentHandler {
	h := &TUIAgentHandler{
		sessionMgr:       sessionMgr,
		httpClient:       &http.Client{Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 180 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   20,
			IdleConnTimeout:       90 * time.Second,
			DisableCompression:    true,
		}},
		maxIterations:    agentMaxIterations,
		codingToolHealth: newCodingToolHealthCache(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// AgentHandlerOption 配置 TUIAgentHandler 的选项函数。
type AgentHandlerOption func(*TUIAgentHandler)

func WithFirewall(fw *security.Firewall) AgentHandlerOption {
	return func(h *TUIAgentHandler) { h.firewall = fw }
}
func WithDefGenerator(dg *tool.DefinitionGenerator) AgentHandlerOption {
	return func(h *TUIAgentHandler) { h.defGenerator = dg }
}
func WithRouter(r *tool.Router) AgentHandlerOption {
	return func(h *TUIAgentHandler) { h.router = r }
}
func WithSelector(s *tool.Selector) AgentHandlerOption {
	return func(h *TUIAgentHandler) { h.selector = s }
}
func WithConfigMgr(cm *config.Manager) AgentHandlerOption {
	return func(h *TUIAgentHandler) { h.configMgr = cm }
}
func WithMemoryStore(ms *memory.Store) AgentHandlerOption {
	return func(h *TUIAgentHandler) { h.memoryStore = ms }
}
func WithSchedulerMgr(sm *scheduler.Manager) AgentHandlerOption {
	return func(h *TUIAgentHandler) { h.schedulerMgr = sm }
}
func WithClawnetClient(cc *clawnet.Client) AgentHandlerOption {
	return func(h *TUIAgentHandler) { h.clawnetClient = cc }
}
func WithAuditLog(al *security.AuditLog) AgentHandlerOption {
	return func(h *TUIAgentHandler) { h.auditLog = al }
}
func WithSSHManager(sm *remote.SSHSessionManager) AgentHandlerOption {
	return func(h *TUIAgentHandler) { h.sshMgr = sm }
}
func WithMaxIterations(n int) AgentHandlerOption {
	return func(h *TUIAgentHandler) {
		if n > 0 {
			if n < 30 {
				n = 30
			}
			if n > 300 {
				n = 300
			}
			h.maxIterations = n
		}
	}
}

// AgentResponse 是 Agent 循环的最终响应。
type AgentResponse struct {
	Text  string
	Error string
}

// RunAgentLoop 执行完整的 Agent 循环：LLM 调用 → 工具执行 → 反馈 → 循环。
func (h *TUIAgentHandler) RunAgentLoop(userText string, history []map[string]string) AgentResponse {
	cfg, err := commands.LoadLLMConfig()
	if err != nil {
		return AgentResponse{Error: fmt.Sprintf("加载 LLM 配置失败: %v", err)}
	}
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return AgentResponse{Error: "LLM 未配置，请先设置 maclaw_llm_url 和 maclaw_llm_model"}
	}

	systemPrompt := h.buildSystemPrompt()
	tools := h.buildToolDefinitions()

	// Router: 当工具总数 > MaxToolBudget 时裁剪
	if h.router != nil && len(tools) > tool.MaxToolBudget {
		tools = h.router.Route(userText, tools)
	}

	var conversation []interface{}
	conversation = append(conversation, map[string]string{"role": "system", "content": systemPrompt})
	for _, msg := range history {
		conversation = append(conversation, msg)
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": userText})

	for iteration := 0; iteration < h.maxIterations; iteration++ {
		resp, err := h.doLLMRequestWithTools(cfg, conversation, tools)
		// Retry once on timeout / temporary network errors.
		if err != nil && isRetryableLLMErrorTUI(err) {
			log.Printf("[LLM] 首次请求超时/网络错误，2s 后重试: %v", err)
			time.Sleep(2 * time.Second)
			resp, err = h.doLLMRequestWithTools(cfg, conversation, tools)
		}
		if err != nil {
			return AgentResponse{Error: fmt.Sprintf("LLM 调用失败: %v", err)}
		}
		if len(resp.Choices) == 0 {
			return AgentResponse{Error: "LLM 未返回有效回复"}
		}

		choice := resp.Choices[0]
		tuiContent := choice.Message.Content
		if tuiContent == "" && choice.Message.ReasoningContent != "" {
			tuiContent = choice.Message.ReasoningContent
		}
		assistantMsg := map[string]interface{}{
			"role":    "assistant",
			"content": tuiContent,
		}
		if len(choice.Message.ToolCalls) > 0 {
			assistantMsg["tool_calls"] = choice.Message.ToolCalls
		}
		conversation = append(conversation, assistantMsg)

		// 无工具调用 → 最终回复
		if len(choice.Message.ToolCalls) == 0 {
			return AgentResponse{Text: agent.StripThinkingTags(tuiContent)}
		}

		// 执行工具调用
		for _, tc := range choice.Message.ToolCalls {
			result := h.executeTool(tc.Function.Name, tc.Function.Arguments)
			// 截断过长结果（web_fetch 允许更长内容）
			maxLen := 4000
			if tc.Function.Name == "web_fetch" {
				maxLen = 20000
			}
			if len(result) > maxLen {
				result = result[:maxLen] + "\n...(已截断)"
			}
			conversation = append(conversation, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      result,
			})
		}
	}

	return AgentResponse{Text: "(已达到最大推理轮次)"}
}

func (h *TUIAgentHandler) buildSystemPrompt() string {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	roleTitle := tuiRoleTitle()

	// Override identity from memory self_identity if present.
	var identity string
	if h.memoryStore != nil {
		if si := h.memoryStore.SelfIdentitySummary(600); si != "" {
			identity = fmt.Sprintf("你的自我认知（来自记忆）：%s\n你运行在 TUI 终端中。你可以使用工具来帮助用户完成任务。", si)
		}
	}
	if identity == "" {
		identity = fmt.Sprintf("你是 MaClaw %s，运行在 TUI 终端中。你可以使用工具来帮助用户完成任务。", roleTitle)
	}

	prompt := fmt.Sprintf(`%s
当前系统: %s/%s
用户主目录: %s
当前工作目录: %s
请用简洁的中文回答用户问题。当需要执行操作时，使用提供的工具。`, identity, runtime.GOOS, runtime.GOARCH, home, cwd)

	// 注入编程工具不可用提示
	if h.codingToolHealth != nil {
		if summary := h.codingToolHealth.UnavailableToolsSummary(); summary != "" {
			prompt += fmt.Sprintf(`

🚫 以下编程工具当前不可用：
%s
你不得自行编写代码来替代编程工具。编程任务（除 craft_tool 外）必须通过编程工具完成。
如果编程工具不可用，请立即中止编程任务，告知用户具体原因，让用户自行排查和修复。
不要尝试创建这些工具的会话，也不要使用 bash、write_file 等工具代替编程工具写代码。`, summary)
		}
	}

	// 注入 SSH 远程能力提示
	prompt += `

🖥️ SSH 远程服务器管理：
你有 ssh 工具，可以连接远程服务器并交互式执行命令。
当用户提到"登录"、"服务器"、"远程"、"SSH"、"部署"、"运维"等关键词时，使用 ssh 工具。
用法: ssh(action=connect/exec/list/close)。连接后可多次 exec 执行命令并观察输出。`

	// 注入已配置的 SSH 主机列表
	if sshHosts := h.loadSSHHosts(); len(sshHosts) > 0 {
		prompt += "\n\n已配置的 SSH 主机（用户可用标签名引用）:\n"
		for _, host := range sshHosts {
			port := host.Port
			if port == 0 {
				port = 22
			}
			prompt += fmt.Sprintf("  - %s → %s@%s:%d\n", host.Label, host.User, host.Host, port)
		}
	}

	// 注入主动记忆指令 — 引导 Agent 在会话中主动保存非显而易见的技术发现
	if h.memoryStore != nil {
		prompt += `

## 主动记忆
当你在会话中发现以下类型的非显而易见信息时，应主动使用 memory(action=save) 保存：
- 调试过程中发现的 workaround 或未文档化行为
- 配置细节、环境特殊性
- 用户项目的架构决策或约定
- 重要的错误原因和解决方案

保存时使用 category=project_knowledge 或 instruction，并添加 tag "proactive"。
每次会话最多主动保存 5 条。保存后在回复中简要提示：💾 已主动记录: <摘要>`
	}

	return prompt
}

func (h *TUIAgentHandler) buildToolDefinitions() []map[string]interface{} {
	// 如果有 DefinitionGenerator，使用动态生成（builtin + MCP）
	if h.defGenerator != nil {
		return h.defGenerator.Generate()
	}
	return h.buildBuiltinToolDefinitions()
}

// buildBuiltinToolDefinitions 返回所有内置工具定义（静态回退）。
func (h *TUIAgentHandler) buildBuiltinToolDefinitions() []map[string]interface{} {
	defs := []map[string]interface{}{
		toolDef("bash", "在终端执行命令", map[string]interface{}{
			"command": map[string]interface{}{"type": "string", "description": "要执行的命令"},
		}, []string{"command"}),
		toolDef("read_file", "读取文件内容", map[string]interface{}{
			"path": map[string]interface{}{"type": "string", "description": "文件路径"},
		}, []string{"path"}),
		toolDef("write_file", "写入文件", map[string]interface{}{
			"path":    map[string]interface{}{"type": "string", "description": "文件路径"},
			"content": map[string]interface{}{"type": "string", "description": "文件内容"},
		}, []string{"path", "content"}),
		toolDef("list_directory", "列出目录内容", map[string]interface{}{
			"path": map[string]interface{}{"type": "string", "description": "目录路径"},
		}, []string{"path"}),
		toolDef("list_sessions", "列出当前编程会话", map[string]interface{}{}, nil),
		toolDef("send_input", "向编程会话发送输入", map[string]interface{}{
			"session_id": map[string]interface{}{"type": "string", "description": "会话 ID"},
			"text":       map[string]interface{}{"type": "string", "description": "输入文本"},
		}, []string{"session_id", "text"}),
		// --- 会话管理扩展 ---
		toolDef("create_session", "创建新的编程会话", map[string]interface{}{
			"tool":          map[string]interface{}{"type": "string", "description": "工具名称 (claude/codex/gemini 等)"},
			"project_path":  map[string]interface{}{"type": "string", "description": "项目路径"},
			"template_name": map[string]interface{}{"type": "string", "description": "模板名称（可选）"},
		}, []string{"tool", "project_path"}),
		toolDef("get_session_output", "获取会话输出", map[string]interface{}{
			"session_id": map[string]interface{}{"type": "string", "description": "会话 ID"},
			"tail_lines": map[string]interface{}{"type": "integer", "description": "返回最后 N 行（可选）"},
		}, []string{"session_id"}),
		toolDef("get_session_events", "获取会话重要事件", map[string]interface{}{
			"session_id": map[string]interface{}{"type": "string", "description": "会话 ID"},
		}, []string{"session_id"}),
		toolDef("interrupt_session", "中断会话", map[string]interface{}{
			"session_id": map[string]interface{}{"type": "string", "description": "会话 ID"},
		}, []string{"session_id"}),
		toolDef("kill_session", "终止会话", map[string]interface{}{
			"session_id": map[string]interface{}{"type": "string", "description": "会话 ID"},
		}, []string{"session_id"}),
		toolDef("send_and_observe", "发送输入并等待观察输出", map[string]interface{}{
			"session_id":   map[string]interface{}{"type": "string", "description": "会话 ID"},
			"text":         map[string]interface{}{"type": "string", "description": "输入文本"},
			"wait_seconds": map[string]interface{}{"type": "integer", "description": "等待秒数"},
		}, []string{"session_id", "text"}),
		toolDef("control_session", "控制会话（暂停/恢复/重启）", map[string]interface{}{
			"session_id": map[string]interface{}{"type": "string", "description": "会话 ID"},
			"action":     map[string]interface{}{"type": "string", "description": "操作: pause/resume/restart"},
		}, []string{"session_id", "action"}),
		// --- 配置管理 ---
		toolDef("get_config", "读取配置", map[string]interface{}{
			"section": map[string]interface{}{"type": "string", "description": "配置区段 (all/claude/remote/maclaw_llm 等)"},
		}, []string{"section"}),
		toolDef("update_config", "更新单个配置项", map[string]interface{}{
			"section": map[string]interface{}{"type": "string", "description": "配置区段"},
			"key":     map[string]interface{}{"type": "string", "description": "配置键"},
			"value":   map[string]interface{}{"type": "string", "description": "新值"},
		}, []string{"section", "key", "value"}),
		toolDef("batch_update_config", "批量更新配置", map[string]interface{}{
			"changes": map[string]interface{}{"type": "array", "description": "变更列表 [{section,key,value}]", "items": map[string]interface{}{"type": "object"}},
		}, []string{"changes"}),
		toolDef("list_config_schema", "列出配置模式", map[string]interface{}{}, nil),
		toolDef("export_config", "导出配置为 JSON", map[string]interface{}{}, nil),
		toolDef("import_config", "导入 JSON 配置", map[string]interface{}{
			"json_data": map[string]interface{}{"type": "string", "description": "JSON 配置数据"},
		}, []string{"json_data"}),
		// --- 模板 ---
		toolDef("create_template", "创建会话模板", map[string]interface{}{
			"name":         map[string]interface{}{"type": "string", "description": "模板名称"},
			"tool":         map[string]interface{}{"type": "string", "description": "工具名称"},
			"project_path": map[string]interface{}{"type": "string", "description": "项目路径"},
		}, []string{"name", "tool", "project_path"}),
		toolDef("list_templates", "列出会话模板", map[string]interface{}{}, nil),
		toolDef("launch_template", "从模板启动会话", map[string]interface{}{
			"template_name": map[string]interface{}{"type": "string", "description": "模板名称"},
		}, []string{"template_name"}),
		// --- 定时任务 ---
		toolDef("create_scheduled_task", "创建定时任务", map[string]interface{}{
			"name":             map[string]interface{}{"type": "string", "description": "任务名称"},
			"action":           map[string]interface{}{"type": "string", "description": "任务动作（自然语言）"},
			"hour":             map[string]interface{}{"type": "integer", "description": "小时 (0-23)"},
			"minute":           map[string]interface{}{"type": "integer", "description": "分钟 (0-59)"},
			"interval_minutes": map[string]interface{}{"type": "integer", "description": "重复间隔（分钟），>0 时启用间隔模式，如每4小时=240"},
		}, []string{"name", "action", "hour", "minute"}),
		toolDef("list_scheduled_tasks", "列出定时任务", map[string]interface{}{}, nil),
		toolDef("delete_scheduled_task", "删除定时任务", map[string]interface{}{
			"task_id": map[string]interface{}{"type": "string", "description": "任务 ID"},
		}, []string{"task_id"}),
		toolDef("update_scheduled_task", "更新定时任务", map[string]interface{}{
			"task_id": map[string]interface{}{"type": "string", "description": "任务 ID"},
			"updates": map[string]interface{}{"type": "object", "description": "要更新的字段"},
		}, []string{"task_id", "updates"}),
		// --- 记忆 ---
		toolDef("memory", "记忆管理（save/list/search/delete/pin/unpin/list_archive/restore）", map[string]interface{}{
			"action":   map[string]interface{}{"type": "string", "description": "操作: save/list/search/delete/pin/unpin/list_archive/restore"},
			"content":  map[string]interface{}{"type": "string", "description": "记忆内容（save 时必填）"},
			"category": map[string]interface{}{"type": "string", "description": "分类: user_fact/preference/project_knowledge/instruction"},
			"tags":     map[string]interface{}{"type": "array", "description": "标签列表", "items": map[string]interface{}{"type": "string"}},
			"keyword":  map[string]interface{}{"type": "string", "description": "搜索关键词（search/list 时使用）"},
			"id":       map[string]interface{}{"type": "string", "description": "记忆 ID（delete 时必填）"},
		}, []string{"action"}),
		// --- MCP ---
		toolDef("list_mcp_tools", "列出所有 MCP 服务器及其工具", map[string]interface{}{}, nil),
		toolDef("call_mcp_tool", "调用 MCP 工具", map[string]interface{}{
			"server_id": map[string]interface{}{"type": "string", "description": "MCP 服务器 ID"},
			"tool_name": map[string]interface{}{"type": "string", "description": "工具名称"},
			"arguments": map[string]interface{}{"type": "object", "description": "工具参数"},
		}, []string{"server_id", "tool_name"}),
		// --- 技能 ---
		toolDef("list_skills", "列出本地已安装技能", map[string]interface{}{}, nil),
		toolDef("search_skill_hub", "搜索 SkillHub 技能", map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "搜索关键词"},
		}, []string{"query"}),
		toolDef("install_skill_hub", "从 SkillHub 安装技能", map[string]interface{}{
			"skill_name": map[string]interface{}{"type": "string", "description": "技能名称"},
		}, []string{"skill_name"}),
		toolDef("run_skill", "执行本地技能", map[string]interface{}{
			"skill_name": map[string]interface{}{"type": "string", "description": "技能名称"},
			"input":      map[string]interface{}{"type": "string", "description": "输入参数"},
		}, []string{"skill_name"}),
		// --- ClawNet ---
		toolDef("clawnet_search", "搜索 ClawNet 知识", map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "搜索关键词"},
		}, []string{"query"}),
		toolDef("clawnet_publish", "发布知识到 ClawNet", map[string]interface{}{
			"title": map[string]interface{}{"type": "string", "description": "标题"},
			"body":  map[string]interface{}{"type": "string", "description": "内容"},
		}, []string{"title", "body"}),
		// --- 审计 ---
		toolDef("query_audit_log", "查询审计日志", map[string]interface{}{
			"tool_name":  map[string]interface{}{"type": "string", "description": "工具名称过滤"},
			"risk_level": map[string]interface{}{"type": "string", "description": "风险等级过滤 (low/medium/high/critical)"},
			"start_date": map[string]interface{}{"type": "string", "description": "开始日期 (2006-01-02)"},
			"end_date":   map[string]interface{}{"type": "string", "description": "结束日期 (2006-01-02)"},
		}, nil),
		// --- 项目管理 ---
		toolDef("project_manage", "项目管理（创建/列出/删除/切换项目）", map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "description": "操作: create/list/delete/switch"},
			"name":   map[string]interface{}{"type": "string", "description": "项目名称（create 必填）"},
			"path":   map[string]interface{}{"type": "string", "description": "项目路径（create 必填）"},
			"target": map[string]interface{}{"type": "string", "description": "项目名称或 ID（delete/switch 必填）"},
		}, []string{"action"}),
		// --- 实用工具 ---
		toolDef("send_file", "发送文件内容到会话", map[string]interface{}{
			"session_id": map[string]interface{}{"type": "string", "description": "会话 ID"},
			"file_path":  map[string]interface{}{"type": "string", "description": "文件路径"},
		}, []string{"session_id", "file_path"}),
		toolDef("parallel_execute", "并发执行多个命令", map[string]interface{}{
			"commands": map[string]interface{}{"type": "array", "description": "命令列表", "items": map[string]interface{}{"type": "string"}},
		}, []string{"commands"}),
		toolDef("switch_llm_provider", "切换 LLM 提供商", map[string]interface{}{
			"provider": map[string]interface{}{"type": "string", "description": "提供商名称"},
		}, []string{"provider"}),
		toolDef("set_max_iterations", "设置 Agent 最大推理轮次（30-300）", map[string]interface{}{
			"value": map[string]interface{}{"type": "integer", "description": "最大轮次（30-300）"},
		}, []string{"value"}),
		toolDef("recommend_tool", "推荐最佳编程工具", map[string]interface{}{
			"task_description": map[string]interface{}{"type": "string", "description": "任务描述"},
		}, []string{"task_description"}),
		toolDef("screenshot", "截取屏幕截图。仅在用户明确要求截屏、或需要确认操作结果时使用。最小间隔 30 秒。", map[string]interface{}{}, nil),
		// --- Web search & fetch ---
		toolDef("web_search", "搜索互联网内容，返回搜索结果列表（标题、URL、摘要）", map[string]interface{}{
			"query":       map[string]interface{}{"type": "string", "description": "搜索关键词"},
			"max_results": map[string]interface{}{"type": "integer", "description": "最大结果数（默认 8，最大 20）"},
		}, []string{"query"}),
		toolDef("web_fetch", "抓取指定 URL 的内容并提取正文。支持 HTTP/HTTPS/FTP 协议，编码检测、JS 渲染、文件下载", map[string]interface{}{
			"url":       map[string]interface{}{"type": "string", "description": "要抓取的 URL（支持 http/https/ftp 协议）"},
			"render_js": map[string]interface{}{"type": "boolean", "description": "是否用 Chrome 渲染 JS（可选）"},
			"save_path": map[string]interface{}{"type": "string", "description": "保存文件路径（可选，下载文件用）"},
			"timeout":   map[string]interface{}{"type": "integer", "description": "超时秒数（可选，默认 30）"},
		}, []string{"url"}),
	}
	// SSH 工具
	defs = append(defs, sshToolDefinitions()...)
	return defs
}

func toolDef(name, desc string, props map[string]interface{}, required []string) map[string]interface{} {
	params := map[string]interface{}{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		params["required"] = required
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": desc,
			"parameters":  params,
		},
	}
}

func (h *TUIAgentHandler) executeTool(name, argsJSON string) string {
	var args map[string]interface{}
	if argsJSON != "" {
		_ = json.Unmarshal([]byte(argsJSON), &args)
	}

	// Firewall 安全检查
	if h.firewall != nil {
		allowed, reason := h.firewall.Check(name, args, nil)
		if !allowed {
			return reason
		}
	}

	result := h.dispatchTool(name, args)

	// 审计日志
	if h.auditLog != nil {
		_ = h.auditLog.Log(security.AuditEntry{
			Timestamp: time.Now(),
			ToolName:  name,
			Arguments: args,
			Result:    scheduler.TruncateStr(result, 200),
		})
	}

	return result
}

func (h *TUIAgentHandler) dispatchTool(name string, args map[string]interface{}) string {
	switch name {
	// --- 原有工具 ---
	case "bash":
		return h.toolBash(args)
	case "read_file":
		return h.toolReadFile(args)
	case "write_file":
		return h.toolWriteFile(args)
	case "list_directory":
		return h.toolListDir(args)
	case "list_sessions":
		return h.toolListSessions()
	case "send_input":
		return h.toolSendInput(args)
	// --- 会话管理扩展 ---
	case "create_session":
		return h.toolCreateSession(args)
	case "get_session_output":
		return h.toolGetSessionOutput(args)
	case "get_session_events":
		return h.toolGetSessionEvents(args)
	case "interrupt_session":
		return h.toolInterruptSession(args)
	case "kill_session":
		return h.toolKillSession(args)
	case "send_and_observe":
		return h.toolSendAndObserve(args)
	case "control_session":
		return h.toolControlSession(args)
	// --- 配置管理 ---
	case "get_config":
		return h.toolGetConfig(args)
	case "update_config":
		return h.toolUpdateConfig(args)
	case "batch_update_config":
		return h.toolBatchUpdateConfig(args)
	case "list_config_schema":
		return h.toolListConfigSchema()
	case "export_config":
		return h.toolExportConfig()
	case "import_config":
		return h.toolImportConfig(args)
	// --- 模板 ---
	case "create_template":
		return h.toolCreateTemplate(args)
	case "list_templates":
		return h.toolListTemplates()
	case "launch_template":
		return h.toolLaunchTemplate(args)
	// --- 定时任务 ---
	case "create_scheduled_task":
		return h.toolCreateScheduledTask(args)
	case "list_scheduled_tasks":
		return h.toolListScheduledTasks()
	case "delete_scheduled_task":
		return h.toolDeleteScheduledTask(args)
	case "update_scheduled_task":
		return h.toolUpdateScheduledTask(args)
	// --- 记忆 ---
	case "memory":
		return h.toolMemory(args)
	// --- MCP ---
	case "list_mcp_tools":
		return h.toolListMCPTools()
	case "call_mcp_tool":
		return h.toolCallMCPTool(args)
	// --- 技能 ---
	case "list_skills":
		return h.toolListSkills()
	case "search_skill_hub":
		return h.toolSearchSkillHub(args)
	case "install_skill_hub":
		return h.toolInstallSkillHub(args)
	case "run_skill":
		return h.toolRunSkill(args)
	// --- ClawNet ---
	case "clawnet_search":
		return h.toolClawnetSearch(args)
	case "clawnet_publish":
		return h.toolClawnetPublish(args)
	// --- 审计 ---
	case "query_audit_log":
		return h.toolQueryAuditLog(args)
	// --- 项目管理 ---
	case "project_manage":
		return h.toolProjectManage(args)
	// --- 实用工具 ---
	case "send_file":
		return h.toolSendFile(args)
	case "parallel_execute":
		return h.toolParallelExecute(args)
	case "switch_llm_provider":
		return h.toolSwitchLLMProvider(args)
	case "set_max_iterations":
		return h.toolSetMaxIterations(args)
	case "recommend_tool":
		return h.toolRecommendTool(args)
	case "screenshot":
		return h.toolScreenshot()
	// --- Web search & fetch ---
	case "web_search":
		return h.toolWebSearch(args)
	case "web_fetch":
		return h.toolWebFetch(args)
	// --- SSH ---
	case "ssh":
		return h.toolSSH(args)
	default:
		return fmt.Sprintf("未知工具: %s", name)
	}
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return v
}

func (h *TUIAgentHandler) toolBash(args map[string]interface{}) string {
	command := stringArg(args, "command")
	if command == "" {
		return "错误: 缺少 command 参数"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	out, err := cmd.CombinedOutput()
	result := string(out)
	if err != nil {
		result += "\n错误: " + err.Error()
	}
	return result
}

func (h *TUIAgentHandler) toolReadFile(args map[string]interface{}) string {
	path := stringArg(args, "path")
	if path == "" {
		return "错误: 缺少 path 参数"
	}
	path = resolvePath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("读取失败: %v", err)
	}
	return string(data)
}

func (h *TUIAgentHandler) toolWriteFile(args map[string]interface{}) string {
	path := stringArg(args, "path")
	content := stringArg(args, "content")
	if path == "" {
		return "错误: 缺少 path 参数"
	}
	path = resolvePath(path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Sprintf("写入失败: %v", err)
	}
	return fmt.Sprintf("已写入 %s (%d 字节)", path, len(content))
}

func (h *TUIAgentHandler) toolListDir(args map[string]interface{}) string {
	path := stringArg(args, "path")
	if path == "" {
		path = "."
	}
	path = resolvePath(path)
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Sprintf("读取目录失败: %v", err)
	}
	var sb strings.Builder
	for _, e := range entries {
		info, _ := e.Info()
		if info != nil {
			sb.WriteString(fmt.Sprintf("%s  %8d  %s\n", info.Mode(), info.Size(), e.Name()))
		} else {
			sb.WriteString(e.Name() + "\n")
		}
	}
	return sb.String()
}

func (h *TUIAgentHandler) toolListSessions() string {
	if h.sessionMgr == nil {
		return "会话管理器未初始化"
	}
	sessions := h.sessionMgr.List()
	if len(sessions) == 0 {
		return "当前无活跃会话"
	}
	var sb strings.Builder
	for _, s := range sessions {
		s.mu.Lock()
		sb.WriteString(fmt.Sprintf("ID: %s  工具: %s  状态: %s  标题: %s\n", s.ID, s.Spec.Tool, s.Status, s.Spec.Title))
		s.mu.Unlock()
	}
	return sb.String()
}

func (h *TUIAgentHandler) toolSendInput(args map[string]interface{}) string {
	sid := stringArg(args, "session_id")
	text := stringArg(args, "text")
	if sid == "" || text == "" {
		return "错误: 缺少 session_id 或 text 参数"
	}
	if h.sessionMgr == nil {
		return "会话管理器未初始化"
	}
	if err := h.sessionMgr.WriteInput(sid, text); err != nil {
		return fmt.Sprintf("发送失败: %v", err)
	}
	return "已发送"
}

func resolvePath(p string) string {
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[1:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// isRetryableLLMErrorTUI returns true for timeout and temporary network errors
// that are worth retrying once.
func isRetryableLLMErrorTUI(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "Client.Timeout") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "connection refused")
}

// doLLMRequestWithTools 发送带工具定义的 LLM 请求。
func (h *TUIAgentHandler) doLLMRequestWithTools(cfg corelib.MaclawLLMConfig, conversation []interface{}, tools []map[string]interface{}) (*llmResponse, error) {
	endpoint := strings.TrimRight(cfg.URL, "/") + "/chat/completions"
	reqBody := map[string]interface{}{
		"model":    cfg.Model,
		"messages": conversation,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
	}
	data, _ := json.Marshal(reqBody)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	var result llmResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return &result, nil
}
