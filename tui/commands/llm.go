package commands

import (
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// RunLLM 执行 llm 子命令。
func RunLLM(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui llm <test|ping|providers|status|set-provider|set-max-iterations|get-max-iterations|login|usage>")
	}
	switch args[0] {
	case "test":
		return llmTest(args[1:])
	case "ping":
		return llmPing(args[1:])
	case "providers":
		return llmProviders(args[1:])
	case "status":
		return llmStatus(args[1:])
	case "set-provider":
		return llmSetProvider(args[1:])
	case "set-max-iterations":
		return llmSetMaxIterations(args[1:])
	case "get-max-iterations":
		return llmGetMaxIterations(args[1:])
	case "login":
		return llmLogin(args[1:])
	case "usage":
		return llmUsage(args[1:])
	default:
		return NewUsageError("unknown llm action: %s", args[0])
	}
}

// LoadLLMConfig 从本地配置文件加载 LLM 配置（供外部复用）。
func LoadLLMConfig() (corelib.MaclawLLMConfig, error) {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return corelib.MaclawLLMConfig{}, err
	}
	llm := corelib.MaclawLLMConfig{
		URL:           cfg.MaclawLLMUrl,
		Key:           cfg.MaclawLLMKey,
		Model:         cfg.MaclawLLMModel,
		Protocol:      cfg.MaclawLLMProtocol,
		ContextLength: cfg.MaclawLLMContextLength,
	}
	return llm, nil
}

func llmStatus(args []string) error {
	fs := flag.NewFlagSet("llm status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	llm, err := LoadLLMConfig()
	configured := err == nil && strings.TrimSpace(llm.URL) != "" && strings.TrimSpace(llm.Model) != ""

	info := map[string]interface{}{
		"configured": configured,
		"url":        llm.URL,
		"model":      llm.Model,
		"protocol":   llm.Protocol,
	}
	if llm.ContextLength > 0 {
		info["context_length"] = llm.ContextLength
	}
	if *jsonOut {
		return PrintJSON(info)
	}
	if !configured {
		fmt.Println("LLM 状态: 未配置")
		fmt.Println("  请在 GUI 设置或 config set --local 中配置 maclaw_llm_url 和 maclaw_llm_model")
		return nil
	}
	fmt.Println("LLM 状态: 已配置")
	fmt.Printf("  URL:      %s\n", llm.URL)
	fmt.Printf("  Model:    %s\n", llm.Model)
	fmt.Printf("  Protocol: %s\n", orDefault(llm.Protocol, "openai"))
	if llm.ContextLength > 0 {
		fmt.Printf("  Context:  %d tokens\n", llm.ContextLength)
	}
	if llm.Key != "" {
		fmt.Printf("  API Key:  %s****\n", llm.Key[:min(4, len(llm.Key))])
	}
	return nil
}

// ensureTUIoAuthToken 在 TUI 的 LLM 请求前检查并刷新 OAuth token。
func ensureTUIoAuthToken() error {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return nil // no config, nothing to refresh
	}
	if len(cfg.MaclawLLMProviders) == 0 {
		return nil
	}
	for i, p := range cfg.MaclawLLMProviders {
		if p.Name == cfg.MaclawLLMCurrentProvider && p.AuthType == "oauth" {
			oauthCfg := oauth.DefaultConfig()
			updated, err := oauth.EnsureValidToken(p, oauthCfg, func(up corelib.MaclawLLMProvider) error {
				cfg.MaclawLLMProviders[i] = up
				// Sync legacy fields
				cfg.MaclawLLMUrl = up.URL
				cfg.MaclawLLMKey = up.Key
				cfg.MaclawLLMModel = up.Model
				cfg.MaclawLLMProtocol = up.Protocol
				cfg.MaclawLLMContextLength = up.ContextLength
				return store.SaveConfig(cfg)
			})
			if err != nil {
				return err
			}
			cfg.MaclawLLMProviders[i] = updated
			break
		}
	}
	return nil
}

func llmTest(args []string) error {
	fs := flag.NewFlagSet("llm test", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if err := ensureTUIoAuthToken(); err != nil {
		return fmt.Errorf("OAuth token 刷新失败: %w", err)
	}

	llm, err := LoadLLMConfig()
	if err != nil {
		return fmt.Errorf("加载 LLM 配置失败: %w", err)
	}
	if strings.TrimSpace(llm.URL) == "" || strings.TrimSpace(llm.Model) == "" {
		return fmt.Errorf("LLM 未配置")
	}

	fmt.Printf("测试 LLM: %s (%s)...\n", llm.Model, llm.URL)
	msgs := []interface{}{
		map[string]string{"role": "user", "content": "请回复 OK"},
	}
	client := &http.Client{Timeout: 30 * time.Second}
	start := time.Now()
	resp, err := agent.DoSimpleLLMRequest(llm, msgs, client, 30*time.Second)
	elapsed := time.Since(start)

	if err != nil {
		if *jsonOut {
			return PrintJSON(map[string]interface{}{"success": false, "error": err.Error(), "elapsed_ms": elapsed.Milliseconds()})
		}
		return fmt.Errorf("LLM 测试失败 (%v): %w", elapsed.Round(time.Millisecond), err)
	}
	if *jsonOut {
		return PrintJSON(map[string]interface{}{"success": true, "response": resp.Content, "elapsed_ms": elapsed.Milliseconds()})
	}
	fmt.Printf("✓ 成功 (%v)\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  响应: %s\n", TruncateDisplay(resp.Content, 80))
	return nil
}

func llmPing(args []string) error {
	fs := flag.NewFlagSet("llm ping", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if err := ensureTUIoAuthToken(); err != nil {
		return fmt.Errorf("OAuth token 刷新失败: %w", err)
	}

	llm, err := LoadLLMConfig()
	if err != nil {
		return fmt.Errorf("加载 LLM 配置失败: %w", err)
	}
	if strings.TrimSpace(llm.URL) == "" {
		return fmt.Errorf("LLM URL 未配置")
	}

	// 简单 HTTP GET 检测端点可达性
	client := &http.Client{Timeout: 10 * time.Second}
	endpoint := strings.TrimRight(llm.URL, "/") + "/models"
	if llm.Protocol == "anthropic" {
		endpoint = strings.TrimRight(llm.URL, "/") + "/v1/messages"
	}

	start := time.Now()
	req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
	if llm.Key != "" {
		if llm.Protocol == "anthropic" {
			req.Header.Set("x-api-key", llm.Key)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+llm.Key)
		}
	}
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		if *jsonOut {
			return PrintJSON(map[string]interface{}{"reachable": false, "error": err.Error()})
		}
		return fmt.Errorf("LLM 端点不可达 (%v): %w", elapsed.Round(time.Millisecond), err)
	}
	resp.Body.Close()

	if *jsonOut {
		return PrintJSON(map[string]interface{}{"reachable": true, "status": resp.StatusCode, "elapsed_ms": elapsed.Milliseconds()})
	}
	fmt.Printf("✓ 端点可达 (HTTP %d, %v)\n", resp.StatusCode, elapsed.Round(time.Millisecond))
	return nil
}

func llmProviders(args []string) error {
	fs := flag.NewFlagSet("llm providers", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(map[string]interface{}{
			"providers": cfg.MaclawLLMProviders,
			"current":   cfg.MaclawLLMCurrentProvider,
		})
	}
	if len(cfg.MaclawLLMProviders) == 0 {
		fmt.Println("未配置 LLM 提供商。")
		if cfg.MaclawLLMUrl != "" {
			fmt.Printf("  当前直接配置: %s (%s)\n", cfg.MaclawLLMModel, cfg.MaclawLLMUrl)
		}
		return nil
	}
	fmt.Printf("%-20s %-10s %-30s %-16s %s\n", "NAME", "PROTOCOL", "URL", "AUTH", "MODEL")
	fmt.Println(strings.Repeat("-", 96))
	for _, p := range cfg.MaclawLLMProviders {
		marker := "  "
		if p.Name == cfg.MaclawLLMCurrentProvider {
			marker = "→ "
		}
		auth := "-"
		if p.AuthType == "oauth" {
			if p.Key == "" {
				auth = "未认证"
			} else if p.TokenExpiresAt > 0 && time.Now().Unix() >= p.TokenExpiresAt {
				auth = "已过期"
			} else {
				auth = "已认证"
			}
		}
		fmt.Printf("%s%-18s %-10s %-30s %-16s %s\n", marker, p.Name, orDefault(p.Protocol, "openai"), TruncateDisplay(p.URL, 30), auth, p.Model)
	}
	return nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func llmSetProvider(args []string) error {
	fs := flag.NewFlagSet("llm set-provider", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() == 0 {
		return NewUsageError("usage: llm set-provider <provider-name>")
	}
	name := fs.Arg(0)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 查找匹配的 provider
	var found *corelib.MaclawLLMProvider
	for i := range cfg.MaclawLLMProviders {
		if cfg.MaclawLLMProviders[i].Name == name {
			found = &cfg.MaclawLLMProviders[i]
			break
		}
	}
	if found == nil {
		// 列出可用的
		var names []string
		for _, p := range cfg.MaclawLLMProviders {
			names = append(names, p.Name)
		}
		if len(names) == 0 {
			return fmt.Errorf("未配置任何 LLM 提供商，请先在 GUI 中添加")
		}
		return fmt.Errorf("提供商 '%s' 不存在，可用: %s", name, strings.Join(names, ", "))
	}

	// 更新当前 provider 和 LLM 配置
	cfg.MaclawLLMCurrentProvider = name
	cfg.MaclawLLMUrl = found.URL
	cfg.MaclawLLMKey = found.Key
	cfg.MaclawLLMModel = found.Model
	cfg.MaclawLLMProtocol = found.Protocol
	cfg.MaclawLLMContextLength = found.ContextLength

	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	fmt.Printf("已切换到 LLM 提供商: %s (%s, %s)\n", name, found.Model, found.URL)
	return nil
}

func llmSetMaxIterations(args []string) error {
	fs := flag.NewFlagSet("llm set-max-iterations", flag.ExitOnError)
	value := fs.Int("value", 0, "最大推理轮次（必填，正整数）")
	fs.Parse(args)

	if *value <= 0 {
		return NewUsageError("usage: llm set-max-iterations --value <N> (N > 0)")
	}
	if *value > 300 {
		*value = 300
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	cfg.MaclawAgentMaxIterations = *value
	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	fmt.Printf("Agent 最大推理轮次已设置为 %d\n", *value)
	return nil
}

func llmGetMaxIterations(args []string) error {
	fs := flag.NewFlagSet("llm get-max-iterations", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	value := cfg.MaclawAgentMaxIterations
	if value <= 0 {
		value = 20 // default
	}
	if *jsonOut {
		return PrintJSON(map[string]int{"max_iterations": value})
	}
	fmt.Printf("Agent 最大推理轮次: %d\n", value)
	return nil
}

func llmLogin(args []string) error {
	if len(args) == 0 || args[0] != "openai" {
		return NewUsageError("usage: llm login openai")
	}

	fmt.Println("正在启动 OpenAI OAuth 登录，请在浏览器中完成授权...")

	cfg := oauth.DefaultConfig()
	result, err := oauth.RunOAuthFlow(cfg)
	if err != nil {
		return fmt.Errorf("OAuth 登录失败: %w", err)
	}

	store := NewFileConfigStore(ResolveDataDir())
	appCfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// Find or create the OpenAI provider
	found := false
	for i, p := range appCfg.MaclawLLMProviders {
		if p.Name == "OpenAI" && p.AuthType == "oauth" {
			appCfg.MaclawLLMProviders[i] = oauth.ApplyTokenResult(p, result)
			found = true
			break
		}
	}
	if !found {
		p := corelib.MaclawLLMProvider{
			Name: "OpenAI", URL: "https://api.openai.com/v1",
			Model: "gpt-4o", AuthType: "oauth", ContextLength: 128000,
		}
		p = oauth.ApplyTokenResult(p, result)
		appCfg.MaclawLLMProviders = append([]corelib.MaclawLLMProvider{p}, appCfg.MaclawLLMProviders...)
	}

	// Set OpenAI as current and sync legacy fields
	appCfg.MaclawLLMCurrentProvider = "OpenAI"
	for _, p := range appCfg.MaclawLLMProviders {
		if p.Name == "OpenAI" {
			appCfg.MaclawLLMUrl = p.URL
			appCfg.MaclawLLMKey = p.Key
			appCfg.MaclawLLMModel = p.Model
			appCfg.MaclawLLMProtocol = p.Protocol
			appCfg.MaclawLLMContextLength = p.ContextLength
			break
		}
	}

	if err := store.SaveConfig(appCfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	fmt.Println("✓ OpenAI OAuth 登录成功，已设为当前 LLM 提供商")
	return nil
}

func llmUsage(args []string) error {
	fs := flag.NewFlagSet("llm usage", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if err := ensureTUIoAuthToken(); err != nil {
		return fmt.Errorf("OAuth token 刷新失败: %w", err)
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// Find the current OAuth provider
	var accessToken string
	for _, p := range cfg.MaclawLLMProviders {
		if p.Name == cfg.MaclawLLMCurrentProvider && p.AuthType == "oauth" {
			accessToken = p.Key
			break
		}
	}
	if accessToken == "" {
		return fmt.Errorf("当前 provider 不支持用量查询（非 OAuth 类型）")
	}

	info, err := oauth.QueryUsage(accessToken)
	if err != nil {
		return fmt.Errorf("查询用量失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(info)
	}

	fmt.Println("OpenAI 用量信息:")
	fmt.Printf("  总额度:   $%.2f\n", info.TotalGranted)
	fmt.Printf("  已使用:   $%.2f\n", info.TotalUsed)
	fmt.Printf("  剩余额度: $%.2f\n", info.TotalAvailable)
	return nil
}
