package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// RunSSH 执行 ssh 子命令。
// 用法:
//
//	maclaw-tui ssh list                     — 列出已配置的 SSH 主机
//	maclaw-tui ssh add <label> <user@host>  — 添加 SSH 主机
//	maclaw-tui ssh remove <label>           — 删除 SSH 主机
//	maclaw-tui ssh connect <label>          — 通过 agent 连接（触发 LLM agent loop）
func RunSSH(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui ssh <list|add|remove>")
	}
	switch args[0] {
	case "list":
		return sshListHosts()
	case "add":
		return sshAddHost(args[1:])
	case "remove":
		return sshRemoveHost(args[1:])
	default:
		return NewUsageError("unknown ssh action: %s (支持: list/add/remove)", args[0])
	}
}

func sshListHosts() error {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if len(cfg.SSHHosts) == 0 {
		Println("未配置 SSH 主机。使用 'maclaw-tui ssh add <label> <user@host>' 添加。")
		return nil
	}

	Printf("SSH 主机列表（%d 个）:\n", len(cfg.SSHHosts))
	for _, h := range cfg.SSHHosts {
		port := h.Port
		if port == 0 {
			port = 22
		}
		auth := h.AuthMethod
		if auth == "" {
			auth = "key"
		}
		Printf("  %-20s %s@%s:%d  (auth: %s)\n", h.Label, h.User, h.Host, port, auth)
	}
	return nil
}

func sshAddHost(args []string) error {
	fs := flag.NewFlagSet("ssh add", flag.ExitOnError)
	port := fs.Int("port", 22, "SSH 端口")
	auth := fs.String("auth", "key", "认证方式 (key/password/agent)")
	keyPath := fs.String("key", "", "私钥路径")
	fs.Parse(args)

	if fs.NArg() < 2 {
		return NewUsageError("usage: maclaw-tui ssh add [flags] <label> <user@host>")
	}

	label := fs.Arg(0)
	userHost := fs.Arg(1)

	user, host, err := parseUserHost(userHost)
	if err != nil {
		return err
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 检查标签是否已存在
	for _, h := range cfg.SSHHosts {
		if h.Label == label {
			return fmt.Errorf("标签 %q 已存在，请使用其他名称", label)
		}
	}

	entry := corelib.SSHHostEntry{
		Label:      label,
		Host:       host,
		Port:       *port,
		User:       user,
		AuthMethod: *auth,
		KeyPath:    *keyPath,
	}
	cfg.SSHHosts = append(cfg.SSHHosts, entry)

	if err := store.SaveConfig(cfg); err != nil {
		return err
	}

	data, _ := json.Marshal(entry)
	Printf("SSH 主机已添加: %s\n%s\n", label, string(data))
	return nil
}

func sshRemoveHost(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui ssh remove <label>")
	}
	label := args[0]

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	found := false
	filtered := make([]corelib.SSHHostEntry, 0, len(cfg.SSHHosts))
	for _, h := range cfg.SSHHosts {
		if h.Label == label {
			found = true
			continue
		}
		filtered = append(filtered, h)
	}

	if !found {
		return fmt.Errorf("SSH 主机 %q 不存在", label)
	}

	cfg.SSHHosts = filtered
	if err := store.SaveConfig(cfg); err != nil {
		return err
	}

	Printf("SSH 主机 %q 已删除\n", label)
	return nil
}

// parseUserHost 解析 "user@host" 格式。
func parseUserHost(s string) (user, host string, err error) {
	user, host, ok := strings.Cut(s, "@")
	if !ok || user == "" || host == "" {
		return "", "", fmt.Errorf("格式错误: %q（应为 user@host）", s)
	}
	return user, host, nil
}
