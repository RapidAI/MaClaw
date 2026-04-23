package main

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/proxyutil"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// buildProxyConfig builds a proxyutil.Config from the current AppConfig.
func (a *App) buildProxyConfig() proxyutil.Config {
	cfg, err := a.LoadConfig()
	if err != nil {
		return proxyutil.Config{}
	}
	return proxyutil.Config{
		Enabled:  cfg.DefaultProxyEnabled,
		Protocol: cfg.DefaultProxyProtocol,
		Host:     cfg.DefaultProxyHost,
		Port:     cfg.DefaultProxyPort,
		Username: cfg.DefaultProxyUsername,
		Password: cfg.DefaultProxyPassword,
		Bypass:   cfg.DefaultProxyBypass,
	}
}

// applyAgentProxy applies the proxy configuration to the websearch (agent) HTTP client
// if the agent scope is enabled.
func (a *App) applyAgentProxy() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	pc := proxyutil.Config{
		Enabled:  cfg.DefaultProxyEnabled,
		Protocol: cfg.DefaultProxyProtocol,
		Host:     cfg.DefaultProxyHost,
		Port:     cfg.DefaultProxyPort,
		Username: cfg.DefaultProxyUsername,
		Password: cfg.DefaultProxyPassword,
		Bypass:   cfg.DefaultProxyBypass,
	}
	if !pc.Enabled || !cfg.DefaultProxyScopeAgent {
		websearch.SetProxy(proxyutil.Config{Enabled: false})
		return
	}
	websearch.SetProxy(pc)
}

// GetProxyConfig returns the current proxy configuration for the frontend.
func (a *App) GetProxyConfig() map[string]interface{} {
	cfg, err := a.LoadConfig()
	if err != nil {
		return map[string]interface{}{"enabled": false}
	}
	return map[string]interface{}{
		"enabled":            cfg.DefaultProxyEnabled,
		"protocol":           cfg.DefaultProxyProtocol,
		"host":               cfg.DefaultProxyHost,
		"port":               cfg.DefaultProxyPort,
		"username":           cfg.DefaultProxyUsername,
		"password":           cfg.DefaultProxyPassword,
		"bypass":             cfg.DefaultProxyBypass,
		"scope_maclaw":       cfg.DefaultProxyScopeMaclaw,
		"scope_coding_tools": cfg.DefaultProxyScopeCodingTools,
		"scope_agent":        cfg.DefaultProxyScopeAgent,
	}
}

// SaveProxyConfig saves the proxy configuration from the frontend and applies it.
func (a *App) SaveProxyConfig(data map[string]interface{}) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}

	if v, ok := data["enabled"].(bool); ok {
		cfg.DefaultProxyEnabled = v
	}
	if v, ok := data["protocol"].(string); ok {
		cfg.DefaultProxyProtocol = v
	}
	if v, ok := data["host"].(string); ok {
		cfg.DefaultProxyHost = v
	}
	if v, ok := data["port"].(string); ok {
		cfg.DefaultProxyPort = v
	}
	if v, ok := data["username"].(string); ok {
		cfg.DefaultProxyUsername = v
	}
	if v, ok := data["password"].(string); ok {
		cfg.DefaultProxyPassword = v
	}
	if v, ok := data["bypass"].(string); ok {
		cfg.DefaultProxyBypass = v
	}
	if v, ok := data["scope_maclaw"].(bool); ok {
		cfg.DefaultProxyScopeMaclaw = v
	}
	if v, ok := data["scope_coding_tools"].(bool); ok {
		cfg.DefaultProxyScopeCodingTools = v
	}
	if v, ok := data["scope_agent"].(bool); ok {
		cfg.DefaultProxyScopeAgent = v
	}

	if err := a.SaveConfig(cfg); err != nil {
		return err
	}

	// Apply proxy changes immediately
	a.applyAgentProxy()
	return nil
}

// injectProxyEnv injects HTTP_PROXY/HTTPS_PROXY/NO_PROXY into the env map
// for coding tool subprocess launches.
func (a *App) injectProxyEnv(env map[string]string, config corelib.AppConfig, projectDir string, useProxy bool) {
	if !useProxy {
		return
	}
	proxyURL := a.resolveProjectProxyURL(config, projectDir)
	if proxyURL == "" {
		return
	}
	env["HTTP_PROXY"] = proxyURL
	env["HTTPS_PROXY"] = proxyURL
	env["http_proxy"] = proxyURL
	env["https_proxy"] = proxyURL
	// Add NO_PROXY from bypass list (convert semicolons to commas for env var)
	if config.DefaultProxyBypass != "" {
		noProxy := strings.ReplaceAll(config.DefaultProxyBypass, ";", ",")
		env["NO_PROXY"] = noProxy
		env["no_proxy"] = noProxy
	}
}
