package commands

import (
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// RunLLM 执行 llm 子命令。
func RunLLM(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui llm <test|ping|providers|status|set-provider|set-max-iterations|get-max-iterations>")
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

func llmTest(args []string) error {
	fs := flag.NewFlagSet("llm test", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

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
	fmt.Printf("%-20s %-10s %-30s %s\n", "NAME", "PROTOCOL", "URL", "MODEL")
	fmt.Println(strings.Repeat("-", 80))
	for _, p := range cfg.MaclawLLMProviders {
		marker := "  "
		if p.Name == cfg.MaclawLLMCurrentProvider {
			marker = "→ "
		}
		fmt.Printf("%s%-18s %-10s %-30s %s\n", marker, p.Name, orDefault(p.Protocol, "openai"), TruncateDisplay(p.URL, 30), p.Model)
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
