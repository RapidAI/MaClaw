package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

type createSessionProviderResolution struct {
	ResolvedProvider string
	Hints            []string
	Error            string
}

func resolveCreateSessionProvider(cfg corelib.AppConfig, toolName, providerOverride string) createSessionProviderResolution {
	toolCfg, err := remoteToolConfig(cfg, toolName)
	if err != nil {
		return createSessionProviderResolution{
			Error: fmt.Sprintf("获取工具配置失败: %s", err.Error()),
		}
	}

	providerOverride, isDefaultOverride := createSessionProviderOverride(cfg, toolName, providerOverride)

	resolver := &ProviderResolver{}
	resolveResult, err := resolver.Resolve(toolCfg, providerOverride)
	if err != nil && isDefaultOverride {
		log.Printf("default provider override %q failed (%v), retrying with auto-resolution", providerOverride, err)
		providerOverride = ""
		resolveResult, err = resolver.Resolve(toolCfg, "")
	}
	if err != nil {
		return createSessionProviderResolution{
			Error: fmt.Sprintf("❌ 无法创建会话：%s\n请在桌面端为 %s 配置至少一个有效的服务商。", err.Error(), toolName),
		}
	}

	resolution := createSessionProviderResolution{
		ResolvedProvider: resolveResult.Provider.ModelName,
	}
	if resolveResult.Fallback {
		resolution.Hints = append(resolution.Hints, fmt.Sprintf("⚡ 服务商已降级: %s → %s", resolveResult.OriginalName, resolveResult.Provider.ModelName))
	}
	return resolution
}

func createSessionProviderOverride(cfg corelib.AppConfig, toolName, providerOverride string) (string, bool) {
	if strings.TrimSpace(providerOverride) != "" {
		return providerOverride, false
	}
	if strings.TrimSpace(cfg.DefaultToolProvider) == "" {
		return "", false
	}
	if !strings.EqualFold(strings.TrimSpace(toolName), strings.TrimSpace(cfg.DefaultTool)) {
		return "", false
	}
	return cfg.DefaultToolProvider, true
}
