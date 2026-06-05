package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// applyMCPAuth sets authentication and custom headers on an MCP HTTP request.
// Custom headers are applied first (lower precedence), then AuthType/AuthSecret
// (higher precedence). Protocol-level headers (Content-Type, Accept) are protected.
func applyMCPAuth(req *http.Request, entry corelib.MCPServerEntry) {
	for k, v := range entry.Headers {
		if k == "" || v == "" {
			continue
		}
		lk := strings.ToLower(k)
		if lk == "content-type" || lk == "accept" {
			continue
		}
		req.Header.Set(k, v)
	}
	if entry.AuthSecret == "" {
		return
	}
	switch entry.AuthType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+entry.AuthSecret)
	case "api_key":
		req.Header.Set("X-API-Key", entry.AuthSecret)
	}
}

// RunMCP 执行 mcp 子命令。
func RunMCP(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui mcp <list|add|remove|health-check|tools|call-tool>")
	}
	switch args[0] {
	case "list":
		return mcpList(args[1:])
	case "add":
		return mcpAdd(args[1:])
	case "remove":
		return mcpRemove(args[1:])
	case "health-check":
		return mcpHealthCheck(args[1:])
	case "tools":
		return mcpTools(args[1:])
	case "call-tool":
		return mcpCallTool(args[1:])
	default:
		return NewUsageError("unknown mcp action: %s", args[0])
	}
}

func mcpList(args []string) error {
	fs := flag.NewFlagSet("mcp list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	lang := i18n.NormalizeLang(cfg.Language)

	type mcpView struct {
		Remote         []corelib.MCPServerEntry      `json:"remote"`
		Local          []corelib.LocalMCPServerEntry `json:"local"`
		NextAction     string                        `json:"next_action,omitempty"`
		NextTUICommand string                        `json:"next_tui_command,omitempty"`
	}
	view := mcpView{
		Remote:         cfg.MCPServers,
		Local:          cfg.LocalMCPServers,
		NextAction:     mcpNextAction(cfg, lang),
		NextTUICommand: mcpNextTUICommand(cfg),
	}

	if *jsonOut {
		return PrintJSON(view)
	}

	if len(cfg.MCPServers) == 0 && len(cfg.LocalMCPServers) == 0 {
		if lang == "en" {
			fmt.Println("No MCP servers configured.")
			fmt.Printf("Next: %s\n", mcpNextAction(cfg, lang))
			fmt.Printf("TUI add: %s\n", mcpNextTUICommand(cfg))
			return nil
		}
		fmt.Println("未配置 MCP 服务器。")
		fmt.Printf("下一步: %s\n", mcpNextAction(cfg, lang))
		fmt.Printf("TUI 添加: %s\n", mcpNextTUICommand(cfg))
		return nil
	}

	if len(cfg.MCPServers) > 0 {
		if lang == "en" {
			fmt.Println("Remote MCP servers:")
		} else {
			fmt.Println("远程 MCP 服务器:")
		}
		fmt.Printf("  %-20s %-10s %-8s %s\n", "NAME", "AUTH", "SOURCE", "URL")
		fmt.Println("  " + strings.Repeat("-", 70))
		for _, s := range cfg.MCPServers {
			fmt.Printf("  %-20s %-10s %-8s %s\n",
				TruncateDisplay(s.Name, 20),
				s.AuthType,
				string(s.Source),
				TruncateDisplay(s.EndpointURL, 40))
		}
	}

	if len(cfg.LocalMCPServers) > 0 {
		if len(cfg.MCPServers) > 0 {
			fmt.Println()
		}
		if lang == "en" {
			fmt.Println("Local MCP servers:")
		} else {
			fmt.Println("本地 MCP 服务器:")
		}
		fmt.Printf("  %-20s %-8s %s\n", "NAME", "DISABLED", "COMMAND")
		fmt.Println("  " + strings.Repeat("-", 60))
		for _, s := range cfg.LocalMCPServers {
			disabled := "no"
			if s.Disabled {
				disabled = "yes"
			}
			cmd := s.Command
			if len(s.Args) > 0 {
				cmd += " " + strings.Join(s.Args, " ")
			}
			fmt.Printf("  %-20s %-8s %s\n",
				TruncateDisplay(s.Name, 20),
				disabled,
				TruncateDisplay(cmd, 50))
		}
	}
	if lang == "en" {
		fmt.Printf("\nNext: %s\n", mcpNextAction(cfg, lang))
	} else {
		fmt.Printf("\n下一步: %s\n", mcpNextAction(cfg, lang))
	}
	return nil
}

func mcpNextAction(cfg corelib.AppConfig, lang string) string {
	cliName := mcpTUIName()
	if lang == "en" {
		if len(cfg.MCPServers) == 0 && len(cfg.LocalMCPServers) == 0 {
			return fmt.Sprintf("Run %s mcp to add MCP from TUI templates; use %s mcp remote for remote endpoints.", cliName, cliName)
		}
		return fmt.Sprintf("Run %s mcp to view/add MCP in the TUI; scripted checks can use %s mcp health-check.", cliName, cliName)
	}
	if len(cfg.MCPServers) == 0 && len(cfg.LocalMCPServers) == 0 {
		return fmt.Sprintf("运行 %s mcp 从模板添加 MCP；远程端点可用 %s mcp remote。", cliName, cliName)
	}
	return fmt.Sprintf("运行 %s mcp 在 TUI 中查看和添加 MCP；脚本检查可用 %s mcp health-check。", cliName, cliName)
}

func mcpNextTUICommand(cfg corelib.AppConfig) string {
	cliName := mcpTUIName()
	if len(cfg.MCPServers) == 0 && len(cfg.LocalMCPServers) == 0 {
		return cliName + " mcp"
	}
	return cliName + " mcp"
}

func mcpTUIName() string {
	return strings.ToLower(brand.Current().DisplayName) + "-tui"
}

func mcpAdd(args []string) error {
	fs := flag.NewFlagSet("mcp add", flag.ExitOnError)
	name := fs.String("name", "", "服务器名称（必填）")
	endpoint := fs.String("url", "", "远程端点 URL")
	command := fs.String("command", "", "本地启动命令")
	authType := fs.String("auth", "none", "认证类型 (none/api_key/bearer)")
	authSecret := fs.String("secret", "", "认证密钥")
	mcpArgs := fs.String("args", "", "命令参数（逗号分隔）")
	fs.Parse(args)

	if *name == "" {
		return NewUsageError("usage: mcp add --name <name> (--url <endpoint> | --command <cmd>)\n推荐: 运行 maclaw-tui mcp，在 TUI 中从模板选择。")
	}
	if *endpoint == "" && *command == "" {
		return NewUsageError("必须指定 --url（远程）或 --command（本地）。推荐运行 maclaw-tui mcp，在 TUI 中从模板选择。")
	}
	if *endpoint != "" && *command != "" {
		return NewUsageError("--url 和 --command 只能二选一。推荐运行 maclaw-tui mcp local 或 maclaw-tui mcp remote，在 TUI 中选择模板。")
	}
	auth := strings.TrimSpace(*authType)
	if auth == "" {
		auth = "none"
	}
	secret := strings.TrimSpace(*authSecret)
	if *endpoint != "" {
		switch auth {
		case "none":
			secret = ""
		case "api_key", "bearer":
			if secret == "" {
				return NewUsageError("认证类型 %s 需要 --secret。推荐运行 maclaw-tui mcp remote，在 TUI 中选择认证方式并填写密钥。", auth)
			}
		default:
			return NewUsageError("不支持的 MCP 认证类型 %q（可用: none/api_key/bearer）。推荐运行 maclaw-tui mcp remote。", auth)
		}
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if *command != "" {
		entryArgs := splitMCPArgs(*mcpArgs)
		cmdText := strings.Join(append([]string{*command}, entryArgs...), " ")
		if err := enforceMCPClientSecurity(cfg, "bash", map[string]interface{}{"command": cmdText}); err != nil {
			return err
		}
		// 本地 MCP
		entry := corelib.LocalMCPServerEntry{
			ID:        fmt.Sprintf("local-%s-%d", *name, time.Now().UnixMilli()),
			Name:      *name,
			Command:   *command,
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		entry.Args = entryArgs
		cfg.LocalMCPServers = append(cfg.LocalMCPServers, entry)
		fmt.Printf("✓ 本地 MCP 服务器 '%s' 已添加 (command: %s)\n", *name, *command)
	} else {
		if err := enforceMCPClientSecurity(cfg, "web_fetch", map[string]interface{}{"url": *endpoint}); err != nil {
			return err
		}
		// 远程 MCP
		entry := corelib.MCPServerEntry{
			ID:          fmt.Sprintf("remote-%s-%d", *name, time.Now().UnixMilli()),
			Name:        *name,
			EndpointURL: *endpoint,
			AuthType:    auth,
			AuthSecret:  secret,
			CreatedAt:   time.Now().Format(time.RFC3339),
			Source:      corelib.MCPSourceManual,
		}
		cfg.MCPServers = append(cfg.MCPServers, entry)
		fmt.Printf("✓ 远程 MCP 服务器 '%s' 已添加 (url: %s)\n", *name, *endpoint)
	}

	return store.SaveConfig(cfg)
}

func splitMCPArgs(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func enforceMCPClientSecurity(cfg corelib.AppConfig, name string, args map[string]interface{}) error {
	if ok, reason := clientsecurity.EnforceConfig(cfg, name, args); !ok {
		if reason == "" {
			reason = "blocked by Hub security policy"
		}
		return fmt.Errorf("%s", reason)
	}
	return nil
}

func mcpRemove(args []string) error {
	fs := flag.NewFlagSet("mcp remove", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: mcp remove <name>")
	}
	name := fs.Arg(0)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	found := false
	// 从远程列表移除
	for i, s := range cfg.MCPServers {
		if s.Name == name {
			cfg.MCPServers = append(cfg.MCPServers[:i], cfg.MCPServers[i+1:]...)
			found = true
			break
		}
	}
	// 从本地列表移除
	if !found {
		for i, s := range cfg.LocalMCPServers {
			if s.Name == name {
				cfg.LocalMCPServers = append(cfg.LocalMCPServers[:i], cfg.LocalMCPServers[i+1:]...)
				found = true
				break
			}
		}
	}

	if !found {
		return fmt.Errorf("MCP 服务器 '%s' 不存在", name)
	}

	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("MCP 服务器 '%s' 已移除。\n", name)
	return nil
}

// ---------- Health Check ----------

func mcpHealthCheck(args []string) error {
	fs := flag.NewFlagSet("mcp health-check", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	type healthResult struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Endpoint string `json:"endpoint,omitempty"`
		Command  string `json:"command,omitempty"`
		Status   string `json:"status"`
		Error    string `json:"error,omitempty"`
		Latency  string `json:"latency,omitempty"`
	}

	var results []healthResult

	client := &http.Client{Timeout: 5 * time.Second}
	for _, s := range cfg.MCPServers {
		if err := enforceMCPClientSecurity(cfg, "web_fetch", map[string]interface{}{"url": s.EndpointURL}); err != nil {
			results = append(results, healthResult{Name: s.Name, Type: "remote", Endpoint: s.EndpointURL, Status: "blocked", Error: err.Error()})
			continue
		}
		start := time.Now()
		// Send a JSON-RPC tools/list request (POST) per MCP Streamable HTTP spec.
		reqBody, _ := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/list",
			"params":  map[string]interface{}{},
		})
		req, _ := http.NewRequest(http.MethodPost, s.EndpointURL, bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		applyMCPAuth(req, s)
		resp, err := client.Do(req)
		elapsed := time.Since(start)
		r := healthResult{Name: s.Name, Type: "remote", Endpoint: s.EndpointURL}
		if err != nil {
			r.Status = "unreachable"
			r.Error = err.Error()
		} else {
			// Check HTTP status first, then validate response is parseable.
			if resp.StatusCode != http.StatusOK {
				errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
				resp.Body.Close()
				r.Status = fmt.Sprintf("HTTP %d", resp.StatusCode)
				if len(errBody) > 0 {
					detail := string(errBody)
					if len(detail) > 200 {
						detail = detail[:200] + "..."
					}
					r.Error = detail
				}
			} else {
				// Validate response is parseable (handles SSE / Streamable HTTP).
				ct := resp.Header.Get("Content-Type")
				_, parseErr := corelib.ParseMCPResponse(resp.Body, ct, 64*1024)
				resp.Body.Close()
				if parseErr != nil {
					r.Status = "parse_error"
					r.Error = parseErr.Error()
				} else {
					r.Status = "healthy"
				}
			}
			r.Latency = elapsed.Round(time.Millisecond).String()
		}
		results = append(results, r)
	}

	for _, s := range cfg.LocalMCPServers {
		r := healthResult{Name: s.Name, Type: "local", Command: s.Command}
		if s.Disabled {
			r.Status = "disabled"
		} else {
			r.Status = "configured"
		}
		results = append(results, r)
	}

	if *jsonOut {
		return PrintJSON(results)
	}

	if len(results) == 0 {
		fmt.Println("未配置 MCP 服务器。")
		return nil
	}

	fmt.Printf("%-20s %-8s %-15s %-10s %s\n", "NAME", "TYPE", "STATUS", "LATENCY", "ENDPOINT")
	fmt.Println(strings.Repeat("-", 80))
	for _, r := range results {
		ep := r.Endpoint
		if ep == "" {
			ep = r.Command
		}
		latency := r.Latency
		if latency == "" {
			latency = "-"
		}
		fmt.Printf("%-20s %-8s %-15s %-10s %s\n",
			TruncateDisplay(r.Name, 20), r.Type, r.Status, latency, TruncateDisplay(ep, 40))
	}
	return nil
}

// ---------- Tools ----------

func mcpTools(args []string) error {
	fs := flag.NewFlagSet("mcp tools", flag.ExitOnError)
	server := fs.String("server", "", "按服务器名称过滤")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	type toolInfo struct {
		Server string `json:"server"`
		Name   string `json:"name"`
		Desc   string `json:"description,omitempty"`
		Params string `json:"params,omitempty"`
	}

	var tools []toolInfo

	// For remote MCP servers, fetch tool list via JSON-RPC tools/list.
	client := &http.Client{Timeout: 10 * time.Second}
	for _, s := range cfg.MCPServers {
		if *server != "" && s.Name != *server {
			continue
		}
		if err := enforceMCPClientSecurity(cfg, "web_fetch", map[string]interface{}{"url": s.EndpointURL}); err != nil {
			tools = append(tools, toolInfo{Server: s.Name, Name: "(blocked)", Desc: err.Error()})
			continue
		}
		reqBody, _ := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/list",
			"params":  map[string]interface{}{},
		})
		req, _ := http.NewRequest(http.MethodPost, s.EndpointURL, bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		applyMCPAuth(req, s)
		resp, err := client.Do(req)
		if err != nil {
			tools = append(tools, toolInfo{Server: s.Name, Name: "(unreachable)", Desc: err.Error()})
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			tools = append(tools, toolInfo{Server: s.Name, Name: "(error)", Desc: fmt.Sprintf("HTTP %d", resp.StatusCode)})
			continue
		}
		// Parse response — handles both plain JSON and SSE (Streamable HTTP).
		ct := resp.Header.Get("Content-Type")
		parsed, parseErr := corelib.ParseMCPResponse(resp.Body, ct, 256*1024)
		resp.Body.Close()
		if parseErr != nil {
			tools = append(tools, toolInfo{Server: s.Name, Name: "(parse error)", Desc: parseErr.Error()})
			continue
		}
		var rpcResp struct {
			Result struct {
				Tools []struct {
					Name        string                 `json:"name"`
					Description string                 `json:"description"`
					InputSchema map[string]interface{} `json:"inputSchema"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(parsed, &rpcResp); err != nil {
			tools = append(tools, toolInfo{Server: s.Name, Name: "(parse error)", Desc: err.Error()})
			continue
		}
		for _, t := range rpcResp.Result.Tools {
			params := formatMCPToolParams(t.InputSchema)
			tools = append(tools, toolInfo{Server: s.Name, Name: t.Name, Desc: t.Description, Params: params})
		}
	}

	// For local MCP servers, start the process briefly to discover tools.
	for _, s := range cfg.LocalMCPServers {
		if s.Disabled {
			continue
		}
		if *server != "" && s.Name != *server {
			continue
		}
		if err := enforceMCPClientSecurity(cfg, "bash", map[string]interface{}{"command": strings.Join(append([]string{s.Command}, s.Args...), " ")}); err != nil {
			tools = append(tools, toolInfo{Server: s.Name + " (local)", Name: "(blocked)", Desc: err.Error()})
			continue
		}
		discovered, err := discoverLocalMCPTools(s)
		if err != nil {
			tools = append(tools, toolInfo{Server: s.Name + " (local)", Name: "(error)", Desc: err.Error()})
			continue
		}
		for _, t := range discovered {
			params := formatMCPToolParams(t.InputSchema)
			tools = append(tools, toolInfo{Server: s.Name + " (local)", Name: t.Name, Desc: t.Description, Params: params})
		}
	}

	if *jsonOut {
		return PrintJSON(tools)
	}

	if len(tools) == 0 {
		fmt.Println("未发现 MCP 工具。")
		return nil
	}

	fmt.Printf("%-20s %-30s %-40s %s\n", "SERVER", "TOOL", "DESCRIPTION", "PARAMS")
	fmt.Println(strings.Repeat("-", 100))
	for _, t := range tools {
		params := t.Params
		if params == "" {
			params = "(no parameters)"
		}
		fmt.Printf("%-20s %-30s %-40s %s\n",
			TruncateDisplay(t.Server, 20), TruncateDisplay(t.Name, 30), TruncateDisplay(t.Desc, 40), params)
	}
	return nil
}

// formatMCPToolParams extracts parameter names from an MCP inputSchema and
// returns a compact summary string like "search_query*, content_size".
// Required parameters are marked with "*".
func formatMCPToolParams(schema map[string]interface{}) string {
	if schema == nil {
		return ""
	}
	props, _ := schema["properties"].(map[string]interface{})
	if len(props) == 0 {
		return ""
	}

	// Collect required parameter names into a set for quick lookup.
	requiredSet := map[string]bool{}
	if reqList, ok := schema["required"].([]interface{}); ok {
		for _, r := range reqList {
			if s, ok := r.(string); ok {
				requiredSet[s] = true
			}
		}
	}

	// Sort parameter names for stable output.
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		if requiredSet[name] {
			parts = append(parts, name+"*")
		} else {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ", ")
}

// localMCPToolInfo holds a tool discovered from a local MCP server.
type localMCPToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// discoverLocalMCPTools starts a local MCP server process, performs the
// initialize handshake, calls tools/list, and shuts down. The entire
// operation is bounded by a 15-second timeout.
func discoverLocalMCPTools(entry corelib.LocalMCPServerEntry) ([]localMCPToolInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, entry.Command, entry.Args...)
	cmd.Env = os.Environ()
	for k, v := range entry.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %q: %w", entry.Command, err)
	}
	defer func() {
		stdinPipe.Close()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait() //nolint:errcheck
	}()

	reader := bufio.NewReaderSize(stdoutPipe, 64*1024)
	var nextID atomic.Int64

	// Single goroutine reads lines from stdout; shared across all sendRPC calls.
	type rpcLine struct {
		line string
		err  error
	}
	lineCh := make(chan rpcLine, 4)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			lineCh <- rpcLine{strings.TrimSpace(line), err}
			if err != nil {
				return
			}
		}
	}()

	sendRPC := func(method string, params interface{}) (json.RawMessage, error) {
		id := nextID.Add(1)
		req := struct {
			JSONRPC string      `json:"jsonrpc"`
			ID      int64       `json:"id"`
			Method  string      `json:"method"`
			Params  interface{} `json:"params,omitempty"`
		}{"2.0", id, method, params}

		data, _ := json.Marshal(req)
		data = append(data, '\n')
		if _, err := stdinPipe.Write(data); err != nil {
			return nil, fmt.Errorf("write: %w", err)
		}

		deadline := time.After(10 * time.Second)
		for {
			select {
			case <-deadline:
				return nil, fmt.Errorf("timeout waiting for %s response", method)
			case <-ctx.Done():
				return nil, ctx.Err()
			case r := <-lineCh:
				if r.err != nil {
					return nil, fmt.Errorf("read: %w", r.err)
				}
				if r.line == "" {
					continue
				}
				var resp struct {
					ID     *int64          `json:"id"`
					Result json.RawMessage `json:"result,omitempty"`
					Error  *struct {
						Code    int    `json:"code"`
						Message string `json:"message"`
					} `json:"error,omitempty"`
				}
				if err := json.Unmarshal([]byte(r.line), &resp); err != nil {
					continue // skip non-JSON lines (server logs)
				}
				if resp.ID == nil || *resp.ID != id {
					continue // skip notifications or mismatched IDs
				}
				if resp.Error != nil {
					return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
				}
				return resp.Result, nil
			}
		}
	}

	// 1. Initialize handshake.
	_, err = sendRPC("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "maclaw-cli", "version": "1.0.0"},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	// Send initialized notification (fire-and-forget).
	notif, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	notif = append(notif, '\n')
	stdinPipe.Write(notif) //nolint:errcheck

	// 2. Discover tools.
	result, err := sendRPC("tools/list", map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	var listResult struct {
		Tools []localMCPToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(result, &listResult); err != nil {
		return nil, fmt.Errorf("parse tools: %w", err)
	}

	return listResult.Tools, nil
}

// ---------- Call Tool ----------

func mcpCallTool(args []string) error {
	fs := flag.NewFlagSet("mcp call-tool", flag.ExitOnError)
	server := fs.String("server", "", "MCP 服务器名称（必填）")
	tool := fs.String("tool", "", "工具名称（必填）")
	toolArgs := fs.String("args", "{}", "工具参数（JSON 格式）")
	fs.Parse(args)

	if *server == "" || *tool == "" {
		return NewUsageError("usage: mcp call-tool --server <name> --tool <name> [--args '{...}']")
	}
	if coretool.IsDisabledExternalCodingSessionTool(*tool) {
		return fmt.Errorf("external coding-session tool %q is disabled", *tool)
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// Find the server
	var serverEntry corelib.MCPServerEntry
	found := false
	for _, s := range cfg.MCPServers {
		if s.Name == *server {
			serverEntry = s
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("MCP 服务器 '%s' 不存在或不是远程服务器", *server)
	}

	if err := enforceMCPClientSecurity(cfg, "web_fetch", map[string]interface{}{"url": serverEntry.EndpointURL}); err != nil {
		return err
	}

	// Parse args
	var parsedArgs map[string]interface{}
	if err := json.Unmarshal([]byte(*toolArgs), &parsedArgs); err != nil {
		return fmt.Errorf("解析工具参数失败: %w", err)
	}

	if err := enforceMCPClientSecurity(cfg, "call_mcp_tool", map[string]interface{}{"tool_name": *tool, "arguments": parsedArgs}); err != nil {
		return err
	}

	// Call the tool via JSON-RPC
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      *tool,
			"arguments": parsedArgs,
		},
	}
	body, _ := json.Marshal(reqBody)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodPost, serverEntry.EndpointURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	applyMCPAuth(req, serverEntry)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("调用工具失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return fmt.Errorf("MCP HTTP %d: %s", resp.StatusCode, string(errBody))
	}

	// Parse response — handles both plain JSON and SSE (Streamable HTTP).
	ct := resp.Header.Get("Content-Type")
	parsed, parseErr := corelib.ParseMCPResponse(resp.Body, ct, 256*1024)
	if parseErr != nil {
		return fmt.Errorf("解析响应失败: %w", parseErr)
	}

	var result interface{}
	if err := json.Unmarshal(parsed, &result); err != nil {
		return fmt.Errorf("解析 JSON 失败: %w", err)
	}
	return PrintJSON(result)
}
