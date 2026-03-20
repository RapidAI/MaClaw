package commands

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// RunMCP 执行 mcp 子命令。
func RunMCP(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui mcp <list|add|remove>")
	}
	switch args[0] {
	case "list":
		return mcpList(args[1:])
	case "add":
		return mcpAdd(args[1:])
	case "remove":
		return mcpRemove(args[1:])
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
