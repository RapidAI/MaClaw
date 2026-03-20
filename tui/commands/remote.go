package commands

import (
	"flag"
	"fmt"
	"strings"
)

// RunRemote 执行 remote 子命令。
func RunRemote(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui remote <status|set-hub|set-email|deactivate>")
	}
	switch args[0] {
	case "status":
		return remoteStatus(args[1:])
	case "set-hub":
		return remoteSetHub(args[1:])
	case "set-email":
		return remoteSetEmail(args[1:])
	case "deactivate":
		return remoteDeactivate(args[1:])
	default:
		return NewUsageError("unknown remote action: %s", args[0])
	}
}

func remoteStatus(args []string) error {
	fs := flag.NewFlagSet("remote status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	info := map[string]interface{}{
		"enabled":    cfg.RemoteEnabled,
		"hub_url":    cfg.RemoteHubURL,
		"email":      cfg.RemoteEmail,
		"machine_id": cfg.RemoteMachineID,
		"sn":         cfg.RemoteSN,
		"user_id":    cfg.RemoteUserID,
	}

	if *jsonOut {
		return PrintJSON(info)
	}

	activated := cfg.RemoteMachineID != "" && cfg.RemoteMachineToken != ""
	status := "未激活"
	if activated {
		status = "已激活"
	}

	fmt.Printf("远程模式: %s\n", status)
	fmt.Printf("  启用:     %v\n", cfg.RemoteEnabled)
	fmt.Printf("  Hub URL:  %s\n", orDefault(cfg.RemoteHubURL, "(未设置)"))
	fmt.Printf("  邮箱:     %s\n", orDefault(cfg.RemoteEmail, "(未设置)"))
	fmt.Printf("  机器 ID:  %s\n", orDefault(cfg.RemoteMachineID, "(未激活)"))
	fmt.Printf("  SN:       %s\n", orDefault(cfg.RemoteSN, "(未激活)"))
	if cfg.RemoteMachineToken != "" {
		fmt.Printf("  Token:    %s****\n", cfg.RemoteMachineToken[:min(4, len(cfg.RemoteMachineToken))])
	}
	return nil
}

func remoteSetHub(args []string) error {
	fs := flag.NewFlagSet("remote set-hub", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: remote set-hub <hub-url>")
	}
	hubURL := strings.TrimRight(fs.Arg(0), "/")

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	cfg.RemoteHubURL = hubURL
	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("Hub URL 已设为: %s\n", hubURL)
	return nil
}

func remoteSetEmail(args []string) error {
	fs := flag.NewFlagSet("remote set-email", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: remote set-email <email>")
	}
	email := fs.Arg(0)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	cfg.RemoteEmail = email
	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("远程邮箱已设为: %s\n", email)
	return nil
}

func remoteDeactivate(args []string) error {
	fs := flag.NewFlagSet("remote deactivate", flag.ExitOnError)
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if cfg.RemoteMachineID == "" {
		fmt.Println("远程模式未激活，无需取消。")
		return nil
	}

	cfg.RemoteMachineID = ""
	cfg.RemoteMachineToken = ""
	cfg.RemoteEmail = ""
	cfg.RemoteSN = ""
	cfg.RemoteUserID = ""
	cfg.RemoteEnabled = false

	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Println("远程模式已取消激活。")
	return nil
}
