package commands

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// RunRemote 执行 remote 子命令。
func RunRemote(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui remote <status|activate|set-hub|set-email|deactivate>")
	}
	switch args[0] {
	case "status":
		return remoteStatus(args[1:])
	case "activate":
		return remoteActivate(args[1:])
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

// remoteActivate performs the full HubCenter discovery → Hub resolve → Enroll flow.
// This is the TUI equivalent of GUI's ActivateRemote.
func remoteActivate(args []string) error {
	fs := flag.NewFlagSet("remote activate", flag.ExitOnError)
	email := fs.String("email", "", "注册邮箱")
	invCode := fs.String("invitation-code", "", "邀请码（如需要）")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// Use flag value, fall back to saved config.
	activateEmail := strings.TrimSpace(*email)
	if activateEmail == "" {
		activateEmail = strings.TrimSpace(cfg.RemoteEmail)
	}
	if activateEmail == "" {
		return fmt.Errorf("邮箱未配置。请使用 --email 参数或先运行: maclaw-tui remote set-email <email>")
	}

	// Check if already activated.
	if cfg.RemoteMachineID != "" && cfg.RemoteMachineToken != "" {
		fmt.Printf("已激活 (machine_id=%s)。如需重新注册，请先运行: maclaw-tui remote deactivate\n", cfg.RemoteMachineID)
		return nil
	}

	fmt.Printf("正在注册 %s ...\n", activateEmail)

	// Build machine profile with TUI version.
	profile := remote.BuildMachineProfile(resolveAppVersion())
	profile.Email = activateEmail
	profile.InvitationCode = strings.TrimSpace(*invCode)
	profile.ClientID = cfg.RemoteClientID
	profile.HubURL = strings.TrimSpace(cfg.RemoteHubURL)
	profile.HubCenterURL = strings.TrimSpace(cfg.RemoteHubCenterURL)
	profile.HubCenterURLs = cfg.HubCenterBaseURLs(remote.DefaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs)

	client := remote.NewEnrollmentClient()
	result, err := client.Enroll(context.Background(), profile)
	if err != nil {
		return fmt.Errorf("注册失败: %w", err)
	}

	// Persist credentials to config.json (shared with GUI).
	cfg.RemoteEmail = result.Email
	cfg.RemoteSN = result.SN
	cfg.RemoteUserID = result.UserID
	cfg.RemoteMachineID = result.MachineID
	cfg.RemoteMachineToken = result.MachineToken
	cfg.RemoteHubURL = result.HubURL
	cfg.RemoteEnabled = true
	if result.ViewerToken != "" {
		cfg.RemoteViewerToken = result.ViewerToken
	}
	if result.ClientID != "" && cfg.RemoteClientID == "" {
		cfg.RemoteClientID = result.ClientID
	}
	if result.HubCenterURL != "" {
		cfg.RemoteHubCenterURL = result.HubCenterURL
	}
	if len(result.DiscoveredURLs) > 0 {
		cfg.RemoteHubCenterURLs = remote.NormalizeHubCenterURLs(result.DiscoveredURLs)
	}

	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	fmt.Println("✅ 注册成功")
	fmt.Printf("  Hub URL:    %s\n", result.HubURL)
	fmt.Printf("  Machine ID: %s\n", result.MachineID)
	fmt.Printf("  Email:      %s\n", result.Email)
	if result.SN != "" {
		fmt.Printf("  SN:         %s\n", result.SN)
	}
	return nil
}

// resolveAppVersion returns the TUI app version string.
func resolveAppVersion() string {
	// The version variable is set by the linker at build time in main.go.
	// We can't access it from the commands package, so return a generic value.
	// The actual version is injected by the caller if needed.
	return "tui"
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
	cfg.RemoteViewerToken = ""
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
