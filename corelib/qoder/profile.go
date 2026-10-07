// Package qoder implements the Qoder platform login published by the official
// qoder CLI: a browser device login carrying an S256 PKCE challenge, then a
// token poll against the OpenAPI host. The chat API itself is
// OpenAI-compatible, so chats ride the normal openai provider path.
package qoder

import (
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	// NameCN is 国内版 provider label shown in 服务商管理.
	NameCN = "Qoder 国内版"
	// NameGlobal is 国际版 provider label.
	NameGlobal = "Qoder 国际版"

	// StoreCN / StoreGlobal are credential-store ids.
	StoreCN     = "qoder-cn"
	StoreGlobal = "qoder-global"

	// The two public device-login clients. They carry no secret; the CLI
	// hardcodes them in its worker runtime (qoder-worker-runtime 1.1.64).
	ClientIDCN     = "e93fe488-5778-4c35-a6fc-0f54ed7b3139"
	ClientIDGlobal = "e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb"

	// ClientVersion matches an official CLI release so upstream can tell the
	// client family apart. MaClaw reports itself as maclaw where a platform
	// identity is requested; the version string alone stays honest.
	ClientVersion = "1.1.64"
	UserAgent     = "qoder/" + ClientVersion

	// DefaultModel is the hardcoded fallback model of the official CLI
	// (Qwen 3.8 Max, 1M context). The login flow replaces it with the
	// server catalog default when that catalog is readable.
	DefaultModel          = "qwen3.8-max"
	DefaultContextWindows = 1000000

	// ChatBase is the shared model server behind both editions. The official
	// CLI picks it from a fixed table (api2-v2.qoder.sh for prod) rather than
	// from the per-site hosts.
	ChatBase = "https://api2-v2.qoder.sh/model/v1"

	pollInterval    = time.Second     // the CLI re-polls once per second
	pollLifetime    = 5 * time.Minute // server-side approval window (Yac=3e5)
	refreshAttempts = 3
)

// LoginLifetime is how long a browser approval may stay pending.
func LoginLifetime() time.Duration { return pollLifetime }

// Profile is one upstream edition.
type Profile struct {
	ID          string
	Name        string
	StoreID     string
	WebOrigin   string // browser login page origin
	OpenAPIBase string // account/token API (device poll & refresh)
	InferBase   string // model catalog host (falls back to plain Bearer)
	ClientID    string
	ChatURL     string // OpenAI-compatible chat base (protocol-openai URL)
}

// CNProfile is the domestic edition (qoder.cn).
func CNProfile() Profile {
	return Profile{
		ID:          "cn",
		Name:        NameCN,
		StoreID:     StoreCN,
		WebOrigin:   "https://qoder.cn",
		OpenAPIBase: "https://openapi.qoder.com.cn",
		InferBase:   "https://gateway.qoder.com.cn",
		ClientID:    ClientIDCN,
		ChatURL:     ChatBase,
	}
}

// GlobalProfile is the international edition (qoder.com).
func GlobalProfile() Profile {
	return Profile{
		ID:          "global",
		Name:        NameGlobal,
		StoreID:     StoreGlobal,
		WebOrigin:   "https://qoder.com",
		OpenAPIBase: "https://openapi.qoder.sh",
		InferBase:   "https://api2.qoder.sh",
		ClientID:    ClientIDGlobal,
		ChatURL:     ChatBase,
	}
}

// ProfileByEdition resolves a 国内/国际 edition selector.
func ProfileByEdition(edition string) (Profile, bool) {
	switch strings.ToLower(strings.TrimSpace(edition)) {
	case "cn", "china", "国内", "国内版", StoreCN:
		return CNProfile(), true
	case "global", "intl", "international", "国际", "国际版", StoreGlobal:
		return GlobalProfile(), true
	default:
		return Profile{}, false
	}
}

// ProfileByName resolves an edition from a MaClaw provider display name.
func ProfileByName(name string) (Profile, bool) {
	switch strings.TrimSpace(name) {
	case NameCN:
		return CNProfile(), true
	case NameGlobal:
		return GlobalProfile(), true
	default:
		return Profile{}, false
	}
}

// ProfileByStoreID resolves an edition for a credential-store id.
func ProfileByStoreID(id string) (Profile, bool) {
	switch strings.TrimSpace(id) {
	case StoreCN:
		return CNProfile(), true
	case StoreGlobal:
		return GlobalProfile(), true
	default:
		return Profile{}, false
	}
}

// ProfileByURL resolves the edition for a saved chat base URL.
func ProfileByURL(raw string) bool {
	return requestHost(raw) == requestHost(ChatBase)
}

// Matches reports whether a runtime LLM config targets an edition.
func Matches(name, rawURL string) bool {
	if _, ok := ProfileByName(name); ok {
		return true
	}
	return ProfileByURL(rawURL)
}

// CanonicalChatURL keeps the provider chat base pinned to the shared model
// server regardless of schema/host typing drift.
func CanonicalChatURL(raw string) string {
	if requestHost(raw) == requestHost(ChatBase) {
		return ChatBase
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
