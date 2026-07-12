package commands

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// RunSkillMarketAuth 执行 skillmarket 认证相关子命令。
// 从 RunSkillMarket 中路由过来。
func RunSkillMarketAuth(action string, args []string) error {
	switch action {
	case "login":
		return smAuthLogin(args)
	case "register":
		return smAuthRegister(args)
	case "lookup":
		return smAuthLookup(args)
	case "verify":
		return smAuthVerify(args)
	case "whoami":
		return smAuthWhoami(args)
	default:
		return NewUsageError("unknown skillmarket auth action: %s\nAvailable: login, register, lookup, verify, whoami", action)
	}
}

func smAuthLogin(args []string) error {
	fs := flag.NewFlagSet("skillmarket login", flag.ExitOnError)
	email := fs.String("email", "", "SkillMarket 账号邮箱")
	password := fs.String("password", "", "密码")
	fs.Parse(args)

	if *email == "" {
		*email = resolveEmail()
	}
	if *email == "" {
		return fmt.Errorf("请指定 --email 参数或在配置中设置 remote_email")
	}
	if *password == "" {
		return fmt.Errorf("请指定 --password 参数")
	}

	baseURL := resolveHubCenterURL()
	client := remote.NewSkillMarketAuthClient()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := client.Login(ctx, baseURL, *email, *password)
	if err != nil {
		return fmt.Errorf("登录失败: %w", err)
	}

	// 保存 token 到 config
	if err := saveSkillMarketToken(result.SessionToken); err != nil {
		return fmt.Errorf("登录成功但保存 token 失败: %w", err)
	}

	fmt.Printf("登录成功！\n")
	fmt.Printf("   邮箱: %s\n", result.Email)
	fmt.Printf("   Token 已保存到配置文件\n")
	return nil
}

func smAuthRegister(args []string) error {
	fs := flag.NewFlagSet("skillmarket register", flag.ExitOnError)
	email := fs.String("email", "", "注册邮箱")
	password := fs.String("password", "", "密码（至少 6 位）")
	fs.Parse(args)

	if *email == "" {
		*email = resolveEmail()
	}
	if *email == "" {
		return fmt.Errorf("请指定 --email 参数")
	}
	if *password == "" {
		return fmt.Errorf("请指定 --password 参数（至少 6 位）")
	}

	baseURL := resolveHubCenterURL()
	client := remote.NewSkillMarketAuthClient()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.Register(ctx, baseURL, *email, *password); err != nil {
		return fmt.Errorf("注册失败: %w", err)
	}

	fmt.Printf("注册成功！激活邮件已发送到 %s\n", *email)
	fmt.Printf("   请检查邮箱并点击激活链接，然后使用 login 命令登录。\n")
	return nil
}

func smAuthLookup(args []string) error {
	fs := flag.NewFlagSet("skillmarket lookup", flag.ExitOnError)
	email := fs.String("email", "", "邮箱")
	fs.Parse(args)

	if *email == "" {
		*email = resolveEmail()
	}
	if *email == "" {
		return fmt.Errorf("请指定 --email 参数")
	}

	baseURL := resolveHubCenterURL()
	client := remote.NewSkillMarketAuthClient()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.SendLookup(ctx, baseURL, *email); err != nil {
		return fmt.Errorf("发送验证邮件失败: %w", err)
	}

	fmt.Printf("验证邮件已发送到 %s\n", *email)
	fmt.Printf("   请检查邮箱，点击链接后使用 verify 命令完成登录。\n")
	return nil
}

func smAuthVerify(args []string) error {
	fs := flag.NewFlagSet("skillmarket verify", flag.ExitOnError)
	token := fs.String("token", "", "邮件中的验证 token")
	fs.Parse(args)

	if *token == "" && len(fs.Args()) > 0 {
		*token = fs.Args()[0]
	}
	if *token == "" {
		return fmt.Errorf("请指定 --token 参数（从邮件链接中获取）")
	}

	baseURL := resolveHubCenterURL()
	client := remote.NewSkillMarketAuthClient()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := client.VerifyIdentity(ctx, baseURL, *token)
	if err != nil {
		return fmt.Errorf("验证失败: %w", err)
	}

	if err := saveSkillMarketToken(result.SessionToken); err != nil {
		return fmt.Errorf("验证成功但保存 token 失败: %w", err)
	}

	fmt.Printf("验证成功！已登录为 %s\n", result.Email)
	fmt.Printf("   Token 已保存到配置文件\n")
	return nil
}

func smAuthWhoami(args []string) error {
	_ = args
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}

	token := strings.TrimSpace(cfg.SkillMarketSessionToken)
	email := strings.TrimSpace(cfg.RemoteEmail)

	if token == "" {
		fmt.Printf("未登录 SkillMarket\n")
		if email != "" {
			fmt.Printf("   配置邮箱: %s（上传时使用 email 模式）\n", email)
		}
		return nil
	}

	// Validate token
	baseURL := resolveHubCenterURL()
	client := remote.NewSkillMarketAuthClient()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	valid, err := client.ValidateToken(ctx, baseURL, token)
	if err != nil {
		fmt.Printf(" 无法验证 token（网络错误）: %v\n", err)
		fmt.Printf("   配置邮箱: %s\n", email)
		return nil
	}

	if valid {
		fmt.Printf("已登录 SkillMarket\n")
		fmt.Printf("   邮箱: %s\n", email)
		fmt.Printf("   Token: %s...（有效）\n", token[:min(20, len(token))])
	} else {
		fmt.Printf("Token 已过期\n")
		fmt.Printf("   邮箱: %s\n", email)
		fmt.Printf("   请重新登录: maclaw-tui skillmarket login --email %s --password <密码>\n", email)
	}
	return nil
}

// saveSkillMarketToken persists the session token to config.json.
// Uses load-then-merge pattern to avoid overwriting concurrent config changes.
func saveSkillMarketToken(token string) error {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		cfg = corelib.AppConfig{}
	}
	cfg.SkillMarketSessionToken = token
	return store.SaveConfig(cfg)
}
