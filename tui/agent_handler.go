package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// agentMaxIterations 是 Agent 循环的默认最大轮数。
const agentMaxIterations = 20

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
// 支持 LLM 工具调用（bash、read_file、write_file、list_directory、list_sessions、send_input）。
type TUIAgentHandler struct {
	sessionMgr *TUISessionManager
	httpClient *http.Client
}

// NewTUIAgentHandler 创建 Agent 处理器。
func NewTUIAgentHandler(sessionMgr *TUISessionManager) *TUIAgentHandler {
	return &TUIAgentHandler{
		sessionMgr: sessionMgr,
		httpClient: &http.Client{Timeout: 120 * time.Second},
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

	var conversation []interface{}
	conversation = append(conversation, map[string]string{"role": "system", "content": systemPrompt})
	for _, msg := range history {
		conversation = append(conversation, msg)
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": userText})

	for iteration := 0; iteration < agentMaxIterations; iteration++ {
		resp, err := h.doLLMRequestWithTools(cfg, conversation, tools)
		if err != nil {
			return AgentResponse{Error: fmt.Sprintf("LLM 调用失败: %v", err)}
		}
		if len(resp.Choices) == 0 {
			return AgentResponse{Error: "LLM 未返回有效回复"}
		}

		choice := resp.Choices[0]
		assistantMsg := map[string]interface{}{
			"role":    "assistant",
			"content": choice.Message.Content,
		}
		if len(choice.Message.ToolCalls) > 0 {
			assistantMsg["tool_calls"] = choice.Message.ToolCalls
		}
		conversation = append(conversation, assistantMsg)

		// 无工具调用 → 最终回复
		if len(choice.Message.ToolCalls) == 0 {
			return AgentResponse{Text: agent.StripThinkingTags(choice.Message.Content)}
		}

		// 执行工具调用
		for _, tc := range choice.Message.ToolCalls {
			result := h.executeTool(tc.Function.Name, tc.Function.Arguments)
			// 截断过长结果
			if len(result) > 4000 {
				result = result[:4000] + "\n...(已截断)"
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
	return fmt.Sprintf(`你是 MaClaw AI 助手，运行在 TUI 终端中。你可以使用工具来帮助用户完成任务。
当前系统: %s/%s
用户主目录: %s
当前工作目录: %s
请用简洁的中文回答用户问题。当需要执行操作时，使用提供的工具。`, runtime.GOOS, runtime.GOARCH, home, cwd)
}

func (h *TUIAgentHandler) buildToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
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
	}
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

	switch name {
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
	req.Header.Set("User-Agent", "MaClaw-TUI/1.0")
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
