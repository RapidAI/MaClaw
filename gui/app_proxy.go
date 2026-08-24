package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/proxyutil"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

func proxyConfigFromApp(cfg corelib.AppConfig) proxyutil.Config {
	pc := proxyutil.Config{
		Enabled:  cfg.DefaultProxyEnabled,
		Protocol: cfg.DefaultProxyProtocol,
		Host:     cfg.DefaultProxyHost,
		Port:     cfg.DefaultProxyPort,
		Username: cfg.DefaultProxyUsername,
		Password: cfg.DefaultProxyPassword,
		Bypass:   cfg.DefaultProxyBypass,
	}
	if !pc.Enabled {
		return pc
	}
	return pc.Normalized()
}

func maclawProxyConfig(cfg corelib.AppConfig) proxyutil.Config {
	pc := proxyConfigFromApp(cfg)
	pc.Enabled = pc.Enabled && cfg.DefaultProxyScopeMaclaw
	return proxyutil.EnabledNormalized(pc)
}

var (
	maclawLLMProxyMu  sync.RWMutex
	maclawLLMProxyCfg proxyutil.Config
)

func setMaclawLLMProxy(pc proxyutil.Config) {
	pc = proxyutil.EnabledNormalized(pc)
	maclawLLMProxyMu.Lock()
	maclawLLMProxyCfg = pc
	maclawLLMProxyMu.Unlock()
}

func currentMaclawLLMProxy() proxyutil.Config {
	maclawLLMProxyMu.RLock()
	defer maclawLLMProxyMu.RUnlock()
	return maclawLLMProxyCfg
}

// llmOrEnvProxy sends MaClaw-scope LLM HTTP through the saved proxy.
// When that scope is off, it falls back to HTTP_PROXY/HTTPS_PROXY.
func llmOrEnvProxy(req *http.Request) (*url.URL, error) {
	if req == nil {
		return nil, nil
	}
	if pc := currentMaclawLLMProxy(); pc.Enabled {
		if fn := pc.ProxyFunc(); fn != nil {
			return fn(req)
		}
	}
	return http.ProxyFromEnvironment(req)
}

func (a *App) llmHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 llmOrEnvProxy,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: timeout,
		},
	}
}

// applyConfiguredProxies applies the given config to websearch (agent scope),
// OAuth HTTP, and MaClaw-scope LLM HTTP (chat, ping, provider test).
func (a *App) applyConfiguredProxies(cfg corelib.AppConfig) {
	pc := proxyConfigFromApp(cfg)
	if !pc.Enabled || !cfg.DefaultProxyScopeAgent {
		websearch.SetProxy(proxyutil.Config{Enabled: false})
	} else {
		websearch.SetProxy(pc)
	}
	oauth.ApplyProxyFromAppConfig(cfg)
	setMaclawLLMProxy(maclawProxyConfig(cfg))
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
	patch := make(map[string]interface{})
	if v, ok := data["enabled"].(bool); ok {
		patch["default_proxy_enabled"] = v
	}
	if _, ok := data["protocol"]; ok {
		protocol := proxyutil.Config{Protocol: formString(data, "protocol")}.Normalized().Protocol
		patch["default_proxy_protocol"] = protocol
	}
	if _, ok := data["host"]; ok {
		patch["default_proxy_host"] = formString(data, "host")
	}
	if _, ok := data["port"]; ok {
		port := formString(data, "port")
		if err := validateProxyPort(port); err != nil {
			return err
		}
		patch["default_proxy_port"] = port
	}
	if _, ok := data["username"]; ok {
		patch["default_proxy_username"] = formString(data, "username")
	}
	if _, ok := data["password"]; ok {
		patch["default_proxy_password"] = formString(data, "password")
	}
	if _, ok := data["bypass"]; ok {
		patch["default_proxy_bypass"] = formString(data, "bypass")
	}
	if v, ok := data["scope_maclaw"].(bool); ok {
		patch["default_proxy_scope_maclaw"] = v
	}
	if v, ok := data["scope_coding_tools"].(bool); ok {
		patch["default_proxy_scope_coding_tools"] = v
	}
	if v, ok := data["scope_agent"].(bool); ok {
		patch["default_proxy_scope_agent"] = v
	}
	if _, err := a.PatchConfigFields(patch); err != nil {
		return err
	}
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

// proxyProbeURL is Google's connectivity check: small response, typically
// unreachable from mainland networks without a working outbound proxy.
var proxyProbeURL = "https://www.google.com/generate_204"

// proxyTraceURL reports the egress IP through the same proxied client.
var proxyTraceURL = "https://1.1.1.1/cdn-cgi/trace"

func formString(data map[string]interface{}, key string) string {
	switch v := data[key].(type) {
	case string:
		if key == "password" {
			return v
		}
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case float64:
		if key == "port" {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return ""
	}
}

func proxyConfigFromForm(data map[string]interface{}) proxyutil.Config {
	enabled, _ := data["enabled"].(bool)
	pc := proxyutil.Config{
		Enabled:  enabled,
		Protocol: formString(data, "protocol"),
		Host:     formString(data, "host"),
		Port:     formString(data, "port"),
		Username: formString(data, "username"),
		Password: formString(data, "password"),
		Bypass:   formString(data, "bypass"),
	}
	if !enabled {
		return pc
	}
	return pc.Normalized()
}

func annotateProxyProbeError(msg string) string {
	if strings.Contains(msg, "certificate is valid for") || strings.Contains(msg, "certificate is not valid for") {
		return "TLS 证书与目标域名不匹配（网络劫持或代理异常）: " + msg
	}
	return msg
}

func redactProxySecrets(msg string, pc proxyutil.Config) string {
	for _, secret := range []string{pc.Password, pc.Username} {
		if secret == "" {
			continue
		}
		msg = strings.ReplaceAll(msg, secret, "***")
		msg = strings.ReplaceAll(msg, url.QueryEscape(secret), "***")
	}
	return msg
}

func validateProxyPort(port string) error {
	if port == "" {
		return nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("代理端口无效")
	}
	return nil
}

func fetchProxyEgressIP(ctx context.Context, client *http.Client) string {
	if proxyTraceURL == "" || client == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyTraceURL, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ip=") {
			continue
		}
		ip := strings.TrimSpace(strings.TrimPrefix(line, "ip="))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}

// TestProxyConfig probes an overseas HTTPS endpoint through the form's proxy
// settings (they do not have to be saved first).
func (a *App) TestProxyConfig(data map[string]interface{}) (map[string]interface{}, error) {
	pc := proxyConfigFromForm(data)
	if !pc.Enabled {
		return nil, fmt.Errorf("请先启用代理")
	}
	if pc.Host == "" || pc.Port == "" {
		return nil, fmt.Errorf("请填写代理主机和端口")
	}
	if err := validateProxyPort(pc.Port); err != nil {
		return nil, err
	}

	transport := &http.Transport{
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		DisableKeepAlives:     true,
	}
	if err := proxyutil.ApplyToTransport(transport, pc); err != nil {
		return nil, err
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Timeout:   12 * time.Second,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	ipCh := make(chan string, 1)
	go func() { ipCh <- fetchProxyEgressIP(ctx, client) }()

	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyProbeURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw-Proxy-Test/1.0")
	resp, err := client.Do(req)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		return map[string]interface{}{
			"ok":         false,
			"target":     proxyProbeURL,
			"latency_ms": latency,
			"message":    annotateProxyProbeError(redactProxySecrets(err.Error(), pc)),
		}, nil
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32*1024))

	ok := resp.StatusCode >= 200 && resp.StatusCode < 400
	result := map[string]interface{}{
		"ok":         ok,
		"target":     proxyProbeURL,
		"status":     resp.StatusCode,
		"latency_ms": latency,
	}
	if !ok {
		result["message"] = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return result, nil
	}
	select {
	case ip := <-ipCh:
		if ip != "" {
			result["egress_ip"] = ip
		}
	case <-time.After(250 * time.Millisecond):
	}
	result["message"] = "ok"
	return result, nil
}
