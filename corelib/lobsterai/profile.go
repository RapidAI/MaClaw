// Package lobsterai speaks LobsterAI's (网易有道 龙虾) desktop client protocol:
// an Electron browser login returns an authorization code to a local
// callback, /api/auth/exchange turns it into a Bearer account, and the chat
// API is standard OpenAI on /api/proxy/v1/chat/completions with Bearer +
// client headers. The package ships the OAuth exchange, token refresh, the
// model catalog, and a small http.Transport that re-routes OpenAI paths onto
// the proxy endpoint.
package lobsterai

import (
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	// Name is the built-in provider label shown in 服务商管理.
	Name = "LobsterAI"
	// StoreID is the credential-store id.
	StoreID = "lobsterai"

	// PortalBase hosts the browser login SPA.
	PortalBase = "https://lobsterai.youdao.com"
	// APIBase hosts the account + chat backend.
	APIBase = "https://lobsterai-server.youdao.com"

	// LoginPath is the SPA route the desktop client opens.
	LoginPath = "/portal#/login?source=electron"
	// ExchangePath trades an authorization code for the account token pair.
	ExchangePath = "/api/auth/exchange"
	// RefreshPath renews the access token.
	RefreshPath = "/api/auth/refresh"
	// ChatPath is the OpenAI-compatible chat endpoint.
	ChatPath = "/api/proxy/v1/chat/completions"
	// ModelsPath lists the chat-capable models.
	ModelsPath = "/api/models/available"

	// ClientCapabilities mirrors the desktop client's capability declaration;
	// the model catalog filters by it.
	ClientCapabilities = "kimi-k3-agentic-v1"
	// ClientFeatures additionally enables thinking level control.
	ClientFeatures = "thinking-level-control-v1"

	// ClientVersion is the chat-version header the desktop build sends. Chat
	// endpoints accept a stale version; only check-in activities need the
	// live one.
	ClientVersion = "0.1.0"

	// UserAgent identifies the desktop client family.
	UserAgent = "LobsterAI/" + ClientVersion

	// DefaultModel is the fallback before the remote catalog answers.
	DefaultModel = "glm-5.3"
	// DefaultContextLength backs the provider row before a catalog read.
	DefaultContextLength = 1000000

	// LoginLifetime bounds one pending browser approval.
	LoginLifetime = 10 * 60 * time.Second

	// CallbackPath is the local redirect endpoint the desktop client registers.
	CallbackPath = "/auth/callback"
)

// Profile carries the static endpoints (compile-time constants upstream).
type Profile struct {
	Name       string
	StoreID    string
	PortalBase string
	APIBase    string
}

// ProductProfile is the single edition descriptor.
func ProductProfile() Profile {
	return Profile{Name: Name, StoreID: StoreID, PortalBase: PortalBase, APIBase: APIBase}
}

// IsProviderName reports whether name is the built-in LobsterAI provider.
func IsProviderName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), Name)
}

// IsAPIBase reports whether raw points at the LobsterAI API host.
func IsAPIBase(raw string) bool {
	return requestHost(raw) == requestHost(APIBase)
}

// Matches reports whether a runtime LLM config targets LobsterAI by name or
// by the configured endpoint.
func Matches(name, rawURL string) bool {
	return IsProviderName(name) || IsAPIBase(rawURL)
}

// CanonicalChatURL keeps the provider chat base pinned to the API host.
func CanonicalChatURL(raw string) string {
	if IsAPIBase(raw) {
		return APIBase
	}
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func requestHost(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if !strings.Contains(text, "://") {
		text = "https://" + text
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		host = parsed.Host
	}
	if h, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		host = h
	}
	return host
}
