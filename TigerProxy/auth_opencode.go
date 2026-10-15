package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/codegenproxy"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

// OpenCodeLoginInfo is returned when the OpenCode key page has been opened.
type OpenCodeLoginInfo struct {
	AuthURL       string `json:"auth_url"`
	BrowserOpened bool   `json:"browser_opened"`
}

// StartOpenCodeLogin opens the official OpenCode key page. The key is pasted
// afterwards, or imported from the local OpenCode auth file.
func (a *App) StartOpenCodeLogin() (OpenCodeLoginInfo, error) {
	authURL := configfile.OpenCodeZenAuthURL
	if !openCodeAuthURLAllowed(authURL) {
		return OpenCodeLoginInfo{}, fmt.Errorf("OpenCode 登录链接不受信任")
	}
	info := OpenCodeLoginInfo{AuthURL: authURL}
	if err := openExternalURL(a.ctx, authURL); err != nil {
		log.Printf("[codexproxy] opencode browser open failed: %s", browserOpenReason(err))
	} else {
		info.BrowserOpened = true
	}
	return info, nil
}

// CompleteOpenCodeLogin saves an API key copied from the OpenCode auth page.
func (a *App) CompleteOpenCodeLogin(key string) (Status, error) {
	normalized, err := normalizeOpenCodeKey(key)
	if err != nil {
		return Status{}, err
	}
	return a.applyOpenCodeKey(normalized)
}

// UseLocalOpenCodeLogin imports the key already saved by the OpenCode CLI.
func (a *App) UseLocalOpenCodeLogin() (Status, error) {
	key := strings.TrimSpace(configfile.ReadOpenCodeZenKey())
	if key == "" {
		return Status{}, fmt.Errorf("本机未找到 OpenCode 登录")
	}
	normalized, err := normalizeOpenCodeKey(key)
	if err != nil {
		return Status{}, fmt.Errorf("本机 OpenCode 密钥无法使用")
	}
	return a.applyOpenCodeKey(normalized)
}

func (a *App) applyOpenCodeKey(key string) (Status, error) {
	a.setRelaunchAfterAuth(true)
	status, err := a.applyVendorLogin(vendorCredential{
		Mode:        AuthModeOpenCode,
		BaseURL:     openCodeBaseURL(),
		AccessToken: key,
		ModelID:     configfile.OpenCodePreferredModel(),
	})
	if err != nil {
		a.setRelaunchAfterAuth(false)
		return Status{}, err
	}
	return status, nil
}

func openCodeBaseURL() string {
	return strings.TrimRight(configfile.OpenCodeZenBaseURL, "/")
}

func openCodeDefaultModel() string {
	return configfile.OpenCodeZenDefaultModel
}

func openCodeModelHeaders() map[string]string {
	return map[string]string{
		"User-Agent":                  corelib.OpenCodeClientUserAgent("opencode"),
		corelib.OpenCodeSessionHeader: corelib.OpenCodeClientSessionID("codexproxy", "models"),
	}
}

func normalizeOpenCodeKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	key = strings.Trim(key, `"'`)
	if strings.HasPrefix(key, "{") {
		if extracted := openCodeKeyFromJSON(key); extracted != "" {
			key = extracted
		}
	}
	if len(key) >= 7 && strings.EqualFold(key[:7], "bearer ") {
		key = strings.TrimSpace(key[7:])
		key = strings.Trim(key, `"'`)
	}
	if len(key) < 12 || strings.ContainsAny(key, " \t\r\n") {
		return "", fmt.Errorf("OpenCode 密钥无效")
	}
	return key, nil
}

func openCodeKeyFromJSON(raw string) string {
	var payload map[string]any
	if json.Unmarshal([]byte(raw), &payload) != nil || payload == nil {
		return ""
	}
	if nested, ok := payload["opencode"].(map[string]any); ok {
		if key := firstOpenCodeKey(nested); key != "" {
			return key
		}
	}
	return firstOpenCodeKey(payload)
}

func firstOpenCodeKey(fields map[string]any) string {
	for _, name := range []string{"key", "apiKey", "access", "access_token", "OPENCODE_API_KEY"} {
		value, _ := fields[name].(string)
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func openCodeAuthURLAllowed(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	if !strings.EqualFold(parsed.Hostname(), "opencode.ai") {
		return false
	}
	path := parsed.EscapedPath()
	return path == "/auth" || strings.HasPrefix(path, "/auth/")
}

type openCodeUpstream struct {
	client *http.Client
}

func newOpenCodeUpstream() openCodeUpstream {
	// No Client.Timeout: that limit includes the response body and cuts off a long stream.
	// ResponseHeaderTimeout still fails a connection that never sends headers.
	transport := openCodeTransport()
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = 10 * time.Minute
	return openCodeUpstream{client: &http.Client{Transport: transport}}
}

func openCodeTransport() *http.Transport {
	if base, ok := http.DefaultTransport.(*http.Transport); ok && base != nil {
		return base.Clone()
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		ExpectContinueTimeout: time.Second,
	}
}

func (u openCodeUpstream) DoModels(ctx context.Context, endpoint, apiKey, clientName string) (*http.Response, error) {
	client := u.client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", corelib.OpenCodeClientUserAgent(clientName))
	req.Header.Set(corelib.OpenCodeSessionHeader, corelib.OpenCodeClientSessionID("codexproxy", "models"))
	return client.Do(req)
}

func (u openCodeUpstream) DoChatCompletions(ctx context.Context, endpoint, apiKey, clientName string, body []byte, accept string, stream bool) (*http.Response, error) {
	body = codegenproxy.PrepareOpenAICompatChatBody(body)
	session := openCodeSessionForBody(body)
	return postChat(ctx, u.client, endpoint, apiKey, clientName, body, accept, stream, func(h http.Header) {
		h.Set("User-Agent", corelib.OpenCodeClientUserAgent(clientName))
		h.Set(corelib.OpenCodeSessionHeader, session)
	})
}

func openCodeSessionForBody(body []byte) string {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		payload = nil
	}
	seed := corelib.OpenCodeConversationSeed(payload)
	if seed == "" {
		return corelib.OpenCodeClientSessionID("codexproxy", "chat")
	}
	return corelib.OpenCodeClientSessionID("codexproxy", seed)
}
