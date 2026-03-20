package commands

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

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

	type mcpView struct {
		Remote []corelib.MCPServerEntry      `json:"remote"`
		Local  []corelib.LocalMCPServerEntry `json:"local"`
	}
	view := mcpView{Remote: cfg.MCPServers, Local: cfg.LocalMCPServers}

	if *jsonOut {
		return PrintJSON(view)
	}

	if len(cfg.MCPServers) == 0 && len(cfg.LocalMCPServers) == 0 {
		fmt.Println("未配置 MCP 服务器。")
		return nil
	}

	if len(cfg.MCPServers) > 0 {
		fmt.Println("远程 MCP 服务器:")
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
		fmt.Println("本地 MCP 服务器:")
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
	return nil
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
		return NewUsageError("usage: mcp add --name <name> (--url <endpoint> | --command <cmd>)")
	}
	if *endpoint == "" && *command == "" {
		return NewUsageError("必须指定 --url（远程）或 --command（本地）")
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if *command != "" {
		// 本地 MCP
		entry := corelib.LocalMCPServerEntry{
			ID:        fmt.Sprintf("local-%s-%d", *name, time.Now().UnixMilli()),
			Name:      *name,
			Command:   *command,
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		if *mcpArgs != "" {
			entry.Args = strings.Split(*mcpArgs, ",")
		}
		cfg.LocalMCPServers = append(cfg.LocalMCPServers, entry)
		fmt.Printf("✓ 本地 MCP 服务器 '%s' 已添加 (command: %s)\n", *name, *command)
	} else {
		// 远程 MCP
		entry := corelib.MCPServerEntry{
			ID:          fmt.Sprintf("remote-%s-%d", *name, time.Now().UnixMilli()),
			Name:        *name,
			EndpointURL: *endpoint,
			AuthType:    *authType,
			AuthSecret:  *authSecret,
			CreatedAt:   time.Now().Format(time.RFC3339),
			Source:      corelib.MCPSourceManual,
		}
		cfg.MCPServers = append(cfg.MCPServers, entry)
		fmt.Printf("✓ 远程 MCP 服务器 '%s' 已添加 (url: %s)\n", *name, *endpoint)
	}

	return store.SaveConfig(cfg)
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
		start := time.Now()
		req, _ := http.NewRequest(http.MethodGet, s.EndpointURL, nil)
		if s.AuthType == "bearer" && s.AuthSecret != "" {
			req.Header.Set("Authorization", "Bearer "+s.AuthSecret)
		} else if s.AuthType == "api_key" && s.AuthSecret != "" {
			req.Header.Set("X-API-Key", s.AuthSecret)
		}
		resp, err := client.Do(req)
		elapsed := time.Since(start)
		r := healthResult{Name: s.Name, Type: "remote", Endpoint: s.EndpointURL}
		if err != nil {
			r.Status = "unreachable"
			r.Error = err.Error()
		} else {
			resp.Body.Close()
			r.Status = fmt.Sprintf("HTTP %d", resp.StatusCode)
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
	}

	var tools []toolInfo

	// For remote MCP servers, try to fetch tool list from /tools endpoint
	client := &http.Client{Timeout: 10 * time.Second}
	for _, s := range cfg.MCPServers {
		if *server != "" && s.Name != *server {
			continue
		}
		endpoint := strings.TrimRight(s.EndpointURL, "/") + "/tools"
		req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
		if s.AuthType == "bearer" && s.AuthSecret != "" {
			req.Header.Set("Authorization", "Bearer "+s.AuthSecret)
		} else if s.AuthType == "api_key" && s.AuthSecret != "" {
			req.Header.Set("X-API-Key", s.AuthSecret)
		}
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
		var toolList []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&toolList); err != nil {
			resp.Body.Close()
			tools = append(tools, toolInfo{Server: s.Name, Name: "(parse error)", Desc: err.Error()})
			continue
		}
		resp.Body.Close()
		for _, t := range toolList {
			tools = append(tools, toolInfo{Server: s.Name, Name: t.Name, Desc: t.Description})
		}
	}

	if *jsonOut {
		return PrintJSON(tools)
	}

	if len(tools) == 0 {
		fmt.Println("未发现 MCP 工具。")
		return nil
	}

	fmt.Printf("%-20s %-30s %s\n", "SERVER", "TOOL", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 80))
	for _, t := range tools {
		fmt.Printf("%-20s %-30s %s\n",
			TruncateDisplay(t.Server, 20), TruncateDisplay(t.Name, 30), TruncateDisplay(t.Desc, 40))
	}
	return nil
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

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// Find the server
	var endpoint, authType, authSecret string
	for _, s := range cfg.MCPServers {
		if s.Name == *server {
			endpoint = s.EndpointURL
			authType = s.AuthType
			authSecret = s.AuthSecret
			break
		}
	}
	if endpoint == "" {
		return fmt.Errorf("MCP 服务器 '%s' 不存在或不是远程服务器", *server)
	}

	// Parse args
	var parsedArgs map[string]interface{}
	if err := json.Unmarshal([]byte(*toolArgs), &parsedArgs); err != nil {
		return fmt.Errorf("解析工具参数失败: %w", err)
	}

	// Call the tool
	callURL := strings.TrimRight(endpoint, "/") + "/tools/" + *tool + "/call"
	body, _ := json.Marshal(parsedArgs)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodPost, callURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if authType == "bearer" && authSecret != "" {
		req.Header.Set("Authorization", "Bearer "+authSecret)
	} else if authType == "api_key" && authSecret != "" {
		req.Header.Set("X-API-Key", authSecret)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("调用工具失败: %w", err)
	}
	defer resp.Body.Close()

	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	return PrintJSON(result)
}
