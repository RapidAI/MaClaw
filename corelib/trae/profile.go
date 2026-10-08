// Package trae implements Trae's browser device login (国内/国际 realms) and
// its SOLO agent chat. The login rides the console's native_ide flow: a
// browser authorization page redirects back to a local callback with a
// refresh token (new pages may also hand an AuthCode + PKCE verifier pair),
// which exchanges for a JWT access token on the account host. Chat itself is
// a non-OpenAI protocol on the per-realm chat host, so Transport translates
// OpenAI requests into SOLO llm_utils_chat calls and its SSE back.
package trae

import (
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	// NameCN is 国内版 provider label shown in 服务商管理.
	NameCN = "Trae 国内版"
	// NameGlobal is 国际版 provider label.
	NameGlobal = "Trae 国际版"

	// StoreCN / StoreGlobal are credential-store ids.
	StoreCN     = "trae-cn"
	StoreGlobal = "trae-global"

	// SOLOClientID is the public SOLO product-line OAuth client. It carries no
	// secret and aligns auth_from=solo logins (both realms) with token refresh.
	SOLOClientID = "en1oxy7wnw8j9n"

	// AppID is the SOLO app identity sent as x-app-id on chat requests.
	AppID = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"

	// UserAgentPrefix builds the "Trae/<ideVersion>" request identity the
	// upstream expects on chat traffic.
	UserAgentPrefix = "Trae/"

	// LoginPluginVersion matches the client's tronBuildVersion used by the
	// community captures of the authorization URL.
	LoginPluginVersion = "2.3.62834"

	// AuthorizePath is the console authorization page.
	AuthorizePath = "/authorization"

	// CallbackPath is the local redirect endpoint baked into auth_callback_url.
	CallbackPath = "/authorize"

	// CallbackPort is the default local redirect port. The production client
	// form is negotiated per login; the site only probes its fixed port.
	CallbackPort = 18080
	// ProbePort is the fixed local port the console authorization page probes
	// to decide a native client is online, per 2026-09 captures.
	ProbePort = 17388

	// LoginPluginVersionTimeoutExpr etc are not needed; login window is bounded by context.
	LoginLifetime = 10 * time.Minute

	// ExchangePath is the account host endpoint that turns a refresh token
	// into a fresh access token (the legacy exchange contract). The newer
	// AuthCode contract ({host}/trae/api/v3/oauth/ExchangeToken) is
	// deliberately not implemented: its exchange demands a device proof this
	// client cannot produce — see docs/providers-trae-lobsterai-zh.md.
	ExchangePath = "/cloudide/api/v3/trae/oauth/ExchangeToken"
	// UserInfoPath reads the account profile behind a fresh JWT.
	UserInfoPath = "/cloudide/api/v3/trae/GetUserInfo"

	// ChatPath is the SOLO agent chat endpoint on the chat host.
	ChatPath = "/api/agent/v3/llm_utils_chat"
	// ModelsPath lists the SOLO model catalog on the chat host.
	ModelsPath = "/api/ide/v1/get_detail_param"

	// DefaultContextLength backs the provider row before a catalog read; the
	// SOLO catalog's 1M-window models cover the biggest tier.
	DefaultContextLength = 1000000
)

// Profile is one upstream realm.
type Profile struct {
	ID             string
	Name           string
	StoreID        string
	ConsoleBase    string // browser authorization page origin
	AuthBase       string // account/token API (exchange & refresh & userinfo)
	AuthAltBase    string // CN's exchange+userinfo lives on a second host too
	ChatHost       string // SOLO agent chat host (llm_utils_chat)
	ClientID       string
	IDEVersion     string
	IDEVersionCode string
	DefaultModel   string
}

// AuthHosts lists the account hosts a token exchange/userinfo may ride, in
// probe order. The country realm publishes the legacy exchange on two hosts
// (api.trae.cn and api.trae.com.cn); community runners use both.
func (p Profile) AuthHosts() []string {
	hosts := make([]string, 0, 2)
	if strings.TrimSpace(p.AuthBase) != "" {
		hosts = append(hosts, p.AuthBase)
	}
	if strings.TrimSpace(p.AuthAltBase) != "" {
		case2 := strings.TrimSpace(p.AuthAltBase)
		dup := false
		for _, host := range hosts {
			if requestHostCase(host) == requestHostCase(case2) {
				dup = true
			}
		}
		if !dup {
			hosts = append(hosts, case2)
		}
	}
	return hosts
}

func requestHostCase(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// CNProfile is the domestic realm.
func CNProfile() Profile {
	return Profile{
		ID:             "cn",
		Name:           NameCN,
		StoreID:        StoreCN,
		ConsoleBase:    "https://www.trae.cn",
		AuthBase:       "https://api.trae.cn",
		AuthAltBase:    "https://api.trae.com.cn",
		ChatHost:       "https://trae-api-cn.mchost.guru",
		ClientID:       SOLOClientID,
		IDEVersion:     "3.3.67",
		IDEVersionCode: "20260401",
		DefaultModel:   "glm-5.2",
	}
}

// GlobalProfile is the international realm (Singapore deployment).
func GlobalProfile() Profile {
	return Profile{
		ID:             "global",
		Name:           NameGlobal,
		StoreID:        StoreGlobal,
		ConsoleBase:    "https://www.trae.ai",
		AuthBase:       "https://growsg-normal.trae.ai",
		AuthAltBase:    "",
		ChatHost:       "https://coresg-normal.trae.ai",
		ClientID:       SOLOClientID,
		IDEVersion:     "3.5.51",
		IDEVersionCode: "20260401",
		DefaultModel:   "gpt-5",
	}
}

// ProfileByEdition resolves a 国内/国际 edition selector.
func ProfileByEdition(edition string) (Profile, bool) {
	switch strings.ToLower(strings.TrimSpace(edition)) {
	case "cn", "china", "domestic", "国内", "国内版", StoreCN:
		return CNProfile(), true
	case "global", "intl", "international", "sg", "v2", "国际", "国际版", StoreGlobal:
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

// ProfileByChatHost resolves an edition for a chat API row URL, so request
// adapters can pin the realm by the configured endpoint.
func ProfileByChatHost(rawURL string) (Profile, bool) {
	switch requestHost(rawURL) {
	case requestHost(CNProfile().ChatHost), requestHost(CNProfile().AuthBase):
		return CNProfile(), true
	case requestHost(GlobalProfile().ChatHost), requestHost(GlobalProfile().AuthBase):
		return GlobalProfile(), true
	default:
		return Profile{}, false
	}
}

// Matches reports whether a runtime LLM config targets either realm by name
// or by chat host.
func Matches(name, rawURL string) bool {
	if _, ok := ProfileByName(name); ok {
		return true
	}
	_, ok := ProfileByChatHost(rawURL)
	return ok
}

// CanonicalChatURL keeps the provider chat base pinned to the realm chat host.
func CanonicalChatURL(profile Profile, raw string) string {
	if requestHost(raw) == requestHost(profile.ChatHost) {
		return profile.ChatHost
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
