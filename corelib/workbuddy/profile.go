// Package workbuddy speaks the WorkBuddy international and CodeBuddy China
// upstream protocol used by RapidProxy: browser login, token refresh, and
// OpenAI chat translation onto /v2/chat/completions.
package workbuddy

import (
	"net"
	"net/url"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	NameChina  = "WorkBuddy 国内版"
	NameGlobal = "WorkBuddy 国际版"

	StoreChina  = "workbuddy-cn"
	StoreGlobal = "workbuddy-global"

	UserAgent = "CLI/2.63.2 CodeBuddy/2.63.2"
	Product   = "SaaS"
	Platform  = "CLI"

	// MarkerHeader tells the local transport to translate this chat request.
	// It is removed before the request leaves the process.
	MarkerHeader = "X-Maclaw-WorkBuddy"
)

// Profile is one upstream edition.
type Profile struct {
	ID           string
	Name         string
	StoreID      string
	APIRoot      string
	ChatURL      string
	Origin       string
	DefaultModel string
}

// ChinaProfile is the domestic edition (copilot.tencent.com / CodeBuddy).
func ChinaProfile() Profile {
	return Profile{
		ID:           "codebuddy",
		Name:         NameChina,
		StoreID:      StoreChina,
		APIRoot:      "https://copilot.tencent.com",
		ChatURL:      "https://copilot.tencent.com/v2",
		Origin:       "https://www.codebuddy.cn",
		DefaultModel: "glm-5.3",
	}
}

// GlobalProfile is the international edition (www.workbuddy.ai).
func GlobalProfile() Profile {
	return Profile{
		ID:           "workbuddy",
		Name:         NameGlobal,
		StoreID:      StoreGlobal,
		APIRoot:      "https://www.workbuddy.ai",
		ChatURL:      "https://www.workbuddy.ai/v2",
		Origin:       "https://www.workbuddy.ai",
		DefaultModel: "gpt-5.4",
	}
}

const (
	EditionChina  = "china"
	EditionGlobal = "global"
)

// ProfileByEdition returns the domestic or international edition.
// Accepted values are china/cn/codebuddy and global/intl/workbuddy.
func ProfileByEdition(edition string) (Profile, bool) {
	switch strings.ToLower(strings.TrimSpace(edition)) {
	case EditionChina, "cn", "codebuddy", StoreChina:
		return ChinaProfile(), true
	case EditionGlobal, "intl", "international", "workbuddy", StoreGlobal:
		return GlobalProfile(), true
	default:
		return Profile{}, false
	}
}

// ProfileByName returns the edition for a MaClaw provider display name.
func ProfileByName(name string) (Profile, bool) {
	switch strings.TrimSpace(name) {
	case NameChina:
		return ChinaProfile(), true
	case NameGlobal:
		return GlobalProfile(), true
	default:
		return Profile{}, false
	}
}

// ProfileByStoreID returns the edition for a credential-store id.
func ProfileByStoreID(id string) (Profile, bool) {
	switch strings.TrimSpace(id) {
	case StoreChina:
		return ChinaProfile(), true
	case StoreGlobal:
		return GlobalProfile(), true
	default:
		return Profile{}, false
	}
}

// ProfileByURL returns the edition for an API root or chat base URL.
func ProfileByURL(raw string) (Profile, bool) {
	switch requestHost(raw) {
	case "www.workbuddy.ai", "workbuddy.ai":
		return GlobalProfile(), true
	case "copilot.tencent.com", "www.codebuddy.cn", "codebuddy.cn":
		return ChinaProfile(), true
	default:
		return Profile{}, false
	}
}

// Matches reports whether a runtime LLM config targets either edition.
func Matches(cfg corelib.MaclawLLMConfig) bool {
	if _, ok := ProfileByName(cfg.ProviderName); ok {
		return true
	}
	_, ok := ProfileByURL(cfg.URL)
	return ok
}

// DefaultContext is the built-in context window of the edition's default model.
func (p Profile) DefaultContext() int {
	for _, spec := range Allowlist(p.ID) {
		if spec.ID == p.DefaultModel && spec.ContextLength > 0 {
			return int(spec.ContextLength)
		}
	}
	return 200000
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
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host
}
